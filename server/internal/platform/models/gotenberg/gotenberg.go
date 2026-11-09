// Package gotenberg converts Office documents to PDF through Gotenberg's LibreOffice route (ATLAS.md §8).
package gotenberg

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models"
)

const (
	libreOfficePath = "/forms/libreoffice/convert"
	healthPath      = "/health"
	fileField       = "files"
)

// Client talks to one Gotenberg instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client. Per-call deadlines come from the context (the job timeout).
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

// OfficeToPDF converts file and streams the PDF into dst.
func (c *Client) OfficeToPDF(ctx context.Context, file models.File, dst io.Writer) error {
	resp, err := models.Post(ctx, c.http, models.Upload{URL: c.baseURL + libreOfficePath, FileField: fileField, File: file})
	if err != nil {
		return fmt.Errorf("gotenberg convert: %w", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return errs.NewTransient(fmt.Errorf("gotenberg read: %w", err))
	}
	return nil
}

// Health reports whether Gotenberg answers.
func (c *Client) Health(ctx context.Context) error {
	resp, err := models.Get(ctx, c.http, c.baseURL+healthPath)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
