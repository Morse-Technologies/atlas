// Package models holds what the HTTP clients of the model services share: streaming multipart uploads
// and mapping HTTP status codes to the error classes of platform/errs.
package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/morse/atlas/server/internal/platform/errs"
)

// File is a local file sent to a service. Name carries the extension services use to pick a converter.
type File struct {
	Name string
	Path string
}

// Upload is one multipart POST: a file under FileField plus plain form fields.
type Upload struct {
	URL       string
	FileField string
	File      File
	Fields    map[string][]string
}

// Post streams the upload without loading the file in memory and checks the response status.
func Post(ctx context.Context, client *http.Client, up Upload) (*http.Response, error) {
	body, contentType := streamMultipart(up)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, up.URL, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	return Do(client, req)
}

// Get performs a GET and checks the response status.
func Get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	return Do(client, req)
}

// Do sends req; network failures are transient, non-2xx statuses are classified by StatusError.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, errs.NewTransient(fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err))
	}
	if err := StatusError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// StatusError classifies a non-2xx response: overload and server errors are retried; a file the service
// refuses as too large or unreadable is permanent.
func StatusError(resp *http.Response) error {
	code := resp.StatusCode
	if code >= http.StatusOK && code < http.StatusMultipleChoices {
		return nil
	}
	err := fmt.Errorf("%s %s: status %d", resp.Request.Method, resp.Request.URL.Path, code)
	switch {
	case isRetryableStatus(code):
		return errs.NewTransient(err)
	case code == http.StatusRequestEntityTooLarge:
		return errs.NewPermanent(errs.ReasonTooLarge, err)
	default:
		return errs.NewPermanent(errs.ReasonCorrupt, err)
	}
}

func isRetryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

func streamMultipart(up Upload) (io.Reader, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() { pw.CloseWithError(writeMultipart(mw, up)) }()
	return pr, mw.FormDataContentType()
}

func writeMultipart(mw *multipart.Writer, up Upload) error {
	for name, values := range up.Fields {
		for _, v := range values {
			if err := mw.WriteField(name, v); err != nil {
				return err
			}
		}
	}
	if err := copyFilePart(mw, up); err != nil {
		return err
	}
	return mw.Close()
}

func copyFilePart(mw *multipart.Writer, up Upload) error {
	part, err := mw.CreateFormFile(up.FileField, filepath.Base(up.File.Name))
	if err != nil {
		return err
	}
	f, err := os.Open(up.File.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(part, f)
	return err
}

// Endpoint is a URL and the client that calls it.
type Endpoint struct {
	Client *http.Client
	URL    string
}

// PostJSON sends call.In as JSON and decodes the answer into call.Out. Network failures and 429/5xx are transient.
func PostJSON(ctx context.Context, ep Endpoint, call JSONCall) error {
	resp, err := OpenJSON(ctx, ep, call.In)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(call.Out); err != nil {
		return fmt.Errorf("decode %s: %w", resp.Request.URL.Path, err)
	}
	return nil
}

// JSONCall is a request body and where to put the decoded answer.
type JSONCall struct {
	In  any
	Out any
}

// OpenJSON sends in as JSON and returns the response with its status already checked. The caller closes it.
func OpenJSON(ctx context.Context, ep Endpoint, in any) (*http.Response, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return Do(ep.Client, req)
}
