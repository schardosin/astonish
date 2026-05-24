//go:build e2e

package e2eboot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Client is a per-user authenticated HTTP client for E2E tests.
// Each user gets their own Client carrying the correct JWT and team header.
type Client struct {
	baseURL  string
	token    string
	teamSlug string
}

// Get sends an authenticated GET request.
func (c *Client) Get(t *testing.T, path string) *http.Response {
	t.Helper()
	return c.do(t, http.MethodGet, path, nil, 30*time.Second)
}

// Post sends an authenticated POST request.
func (c *Client) Post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return c.do(t, http.MethodPost, path, body, 30*time.Second)
}

// PostWithTimeout sends an authenticated POST request with a custom timeout.
func (c *Client) PostWithTimeout(t *testing.T, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()
	return c.do(t, http.MethodPost, path, body, timeout)
}

// Put sends an authenticated PUT request.
func (c *Client) Put(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return c.do(t, http.MethodPut, path, body, 30*time.Second)
}

// Delete sends an authenticated DELETE request.
func (c *Client) Delete(t *testing.T, path string) *http.Response {
	t.Helper()
	return c.do(t, http.MethodDelete, path, nil, 10*time.Second)
}

// SSE sends an authenticated POST to an SSE endpoint and returns parsed events.
func (c *Client) SSE(t *testing.T, path string, body any, timeout time.Duration) []SSEEvent {
	t.Helper()
	resp := c.doWithHeaders(t, http.MethodPost, path, body, timeout, map[string]string{
		"Accept": "text/event-stream",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("[client] SSE %s: %d %s", path, resp.StatusCode, string(respBody))
	}
	return ParseSSEStream(t, resp.Body)
}

// SSERaw sends an SSE POST and returns the raw response for streaming.
func (c *Client) SSERaw(t *testing.T, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()
	resp := c.doWithHeaders(t, http.MethodPost, path, body, timeout, map[string]string{
		"Accept": "text/event-stream",
	})
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("[client] SSE %s: %d %s", path, resp.StatusCode, string(respBody))
	}
	return resp
}

// Token returns the raw JWT for this client.
func (c *Client) Token() string {
	return c.token
}

// TeamSlug returns the team slug used in requests.
func (c *Client) TeamSlug() string {
	return c.teamSlug
}

func (c *Client) do(t *testing.T, method, path string, body any, timeout time.Duration) *http.Response {
	t.Helper()
	return c.doWithHeaders(t, method, path, body, timeout, nil)
}

func (c *Client) doWithHeaders(t *testing.T, method, path string, body any, timeout time.Duration, headers map[string]string) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("[client] marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("[client] create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Astonish-Team", c.teamSlug)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Check if this is a connection refused (expected for negative auth tests)
		if strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("[client] %s %s: server not running: %v", method, path, err)
		}
		t.Fatalf("[client] %s %s: %v", method, path, err)
	}
	return resp
}
