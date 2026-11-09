package main

import (
	"fmt"
	"net/http"
	"net/url"
)

// nextPage reads one page of documents after cursor and returns the cursor of the next, empty at the end.
func (l *load) nextPage(cursor string) (string, error) {
	path := fmt.Sprintf("/documents?limit=%d", listPageSize)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequest(http.MethodGet, l.c.base+apiPath+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := l.c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != httpOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	return resp.Header.Get("Next-Cursor"), nil
}
