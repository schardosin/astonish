//go:build e2e

package e2eboot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// Post sends an authenticated POST request and returns the response.
// The caller must close the response body.
func (h *Harness) Post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return h.do(t, http.MethodPost, path, body, 30*time.Second)
}

// PostWithTimeout sends an authenticated POST request with a custom timeout.
func (h *Harness) PostWithTimeout(t *testing.T, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()
	return h.do(t, http.MethodPost, path, body, timeout)
}

// Get sends an authenticated GET request and returns the response.
func (h *Harness) Get(t *testing.T, path string) *http.Response {
	t.Helper()
	return h.do(t, http.MethodGet, path, nil, 30*time.Second)
}

// Delete sends an authenticated DELETE request and returns the response.
func (h *Harness) Delete(t *testing.T, path string) *http.Response {
	t.Helper()
	return h.do(t, http.MethodDelete, path, nil, 10*time.Second)
}

// ReadBody reads the entire response body, closes it, and returns it as a string.
func ReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("[e2eboot] read body: %v", err)
	}
	return string(data)
}

// DecodeJSON reads the response body and decodes it into dest.
func DecodeJSON(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatalf("[e2eboot] decode JSON: %v", err)
	}
}

func (h *Harness) do(t *testing.T, method, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("[e2eboot] marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := h.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("[e2eboot] create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("X-Astonish-Team", "general")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("[e2eboot] %s %s: %v", method, path, err)
	}
	return resp
}
