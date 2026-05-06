package astonish

import (
	"io"
	"net/http"

	"github.com/schardosin/astonish/pkg/client"
)

// addRemoteAuthHeaders adds Authorization and X-Astonish-Team headers to a
// request when in remote mode. Called by getDaemonBaseURL's callers after
// creating their http.Request. For local mode this is a no-op.
func addRemoteAuthHeaders(req *http.Request) {
	if !client.IsRemoteMode() {
		return
	}

	ts, err := client.NewTokenStore()
	if err != nil {
		return
	}
	tokens, err := ts.Load()
	if err != nil || tokens == nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	cfg, err := client.LoadRemoteConfig()
	if err == nil && cfg != nil && cfg.Team != "" {
		req.Header.Set("X-Astonish-Team", cfg.Team)
	}
}

// newAPIRequest creates an HTTP request with proper auth headers for remote mode.
// For local mode, returns a plain request (daemon uses loopback bypass).
func newAPIRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	addRemoteAuthHeaders(req)
	return req, nil
}
