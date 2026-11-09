// Package docling parses documents with docling-serve's asynchronous API: submit, poll, fetch (ATLAS.md §8).
// The synchronous endpoint stops waiting after 120 s, too short for long scanned documents.
package docling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models"
)

const (
	submitPath   = "/v1/convert/file/async"
	pollPath     = "/v1/status/poll/"
	resultPath   = "/v1/result/"
	healthPath   = "/health"
	fileField    = "files"
	pollInterval = 2 * time.Second

	statusSuccess = "success"
	statusFailure = "failure"
)

// Client talks to one docling-serve instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client. The overall deadline is the job timeout carried by the context.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

// convertOptions asks for the DoclingDocument JSON only, with OCR on pages without a text layer.
var convertOptions = map[string][]string{"to_formats": {"json"}, "do_ocr": {"true"}}

type taskStatus struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
}

type convertResult struct {
	Status   string `json:"status"`
	Document struct {
		JSONContent json.RawMessage `json:"json_content"`
	} `json:"document"`
}

// Convert parses file and returns the DoclingDocument JSON.
func (c *Client) Convert(ctx context.Context, file models.File) (json.RawMessage, error) {
	taskID, err := c.submit(ctx, file)
	if err != nil {
		return nil, err
	}
	if err := c.waitForTask(ctx, taskID); err != nil {
		return nil, err
	}
	return c.fetchResult(ctx, taskID)
}

// Health reports whether docling-serve answers.
func (c *Client) Health(ctx context.Context) error {
	resp, err := models.Get(ctx, c.http, c.baseURL+healthPath)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (c *Client) submit(ctx context.Context, file models.File) (string, error) {
	up := models.Upload{URL: c.baseURL + submitPath, FileField: fileField, File: file, Fields: convertOptions}
	resp, err := models.Post(ctx, c.http, up)
	if err != nil {
		return "", fmt.Errorf("docling submit: %w", err)
	}
	var status taskStatus
	if err := decodeAndClose(resp, &status); err != nil {
		return "", err
	}
	return status.TaskID, nil
}

// waitForTask polls until the task succeeds or fails, or the context deadline (job timeout) passes.
func (c *Client) waitForTask(ctx context.Context, taskID string) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		done, err := c.taskFinished(ctx, taskID)
		if done || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return errs.NewTransient(fmt.Errorf("docling task %s: %w", taskID, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (c *Client) taskFinished(ctx context.Context, taskID string) (bool, error) {
	resp, err := models.Get(ctx, c.http, c.baseURL+pollPath+taskID)
	if err != nil {
		return false, fmt.Errorf("docling poll: %w", err)
	}
	var status taskStatus
	if err := decodeAndClose(resp, &status); err != nil {
		return false, err
	}
	switch status.TaskStatus {
	case statusSuccess:
		return true, nil
	case statusFailure:
		return true, errs.NewPermanent(errs.ReasonCorrupt, fmt.Errorf("docling could not parse the file (task %s)", taskID))
	default:
		return false, nil
	}
}

func (c *Client) fetchResult(ctx context.Context, taskID string) (json.RawMessage, error) {
	resp, err := models.Get(ctx, c.http, c.baseURL+resultPath+taskID)
	if err != nil {
		return nil, fmt.Errorf("docling result: %w", err)
	}
	var result convertResult
	if err := decodeAndClose(resp, &result); err != nil {
		return nil, err
	}
	if result.Status == statusFailure || len(result.Document.JSONContent) == 0 {
		return nil, errs.NewPermanent(errs.ReasonCorrupt, errors.New("docling returned no document"))
	}
	return result.Document.JSONContent, nil
}

func decodeAndClose(resp *http.Response, into any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return errs.NewTransient(fmt.Errorf("docling decode: %w", err))
	}
	return nil
}
