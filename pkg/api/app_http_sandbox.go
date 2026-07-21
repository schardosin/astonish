package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/SAP/astonish/pkg/sandbox"
)

const appHTTPStatusMarker = "\nASTONISH_HTTP_STATUS:"

// appHTTPFetch performs an HTTP request via the App sandbox (curl Exec).
// Overridable in tests.
var appHTTPFetch = fetchHTTPViaSandbox

func fetchHTTPViaSandbox(ctx context.Context, r *http.Request, method, rawURL string, headers map[string]string, body []byte) (status int, respBody []byte, err error) {
	userID := "anonymous"
	if r != nil {
		userID = effectiveUserID(r)
	}

	backend, sessionID, cleanup, err := ensureAppSandboxSession(ctx, r, userID)
	if err != nil {
		return 0, nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	cmd := []string{
		"curl", "-sS",
		"-X", method,
		"--max-redirs", "5",
		"--max-time", "30",
		"-w", appHTTPStatusMarker + "%{http_code}",
	}
	for k, v := range headers {
		cmd = append(cmd, "-H", k+": "+v)
	}

	var stdin io.Reader
	if len(body) > 0 {
		cmd = append(cmd, "--data-binary", "@-")
		stdin = bytes.NewReader(body)
	}
	cmd = append(cmd, rawURL)

	slog.Debug("app HTTP sandbox fetch", "method", method, "url", rawURL, "session", sessionID)
	result, err := backend.Exec(ctx, sessionID, sandbox.ExecSpec{
		Command: cmd,
		Stdin:   stdin,
	})
	appMCPIdleTracker.touch(sessionID)
	if err != nil {
		return 0, nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	// curl non-zero exit often still includes status/body (e.g. connection errors).
	respBody, status, parseErr := parseCurlHTTPOutput(result.Stdout)
	if parseErr != nil {
		stderr := strings.TrimSpace(string(result.Stderr))
		if stderr == "" {
			stderr = strings.TrimSpace(string(result.Stdout))
		}
		if result.ExitCode != 0 {
			return 0, nil, fmt.Errorf("HTTP request failed: curl exit %d: %s", result.ExitCode, stderr)
		}
		return 0, nil, fmt.Errorf("HTTP request failed: %w", parseErr)
	}
	if len(respBody) > maxResponseBodySize {
		respBody = respBody[:maxResponseBodySize]
	}
	return status, respBody, nil
}

func parseCurlHTTPOutput(stdout []byte) (body []byte, status int, err error) {
	idx := bytes.LastIndex(stdout, []byte(appHTTPStatusMarker))
	if idx < 0 {
		return nil, 0, fmt.Errorf("curl did not report HTTP status")
	}
	body = stdout[:idx]
	statusStr := strings.TrimSpace(string(stdout[idx+len(appHTTPStatusMarker):]))
	status, err = strconv.Atoi(statusStr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid HTTP status %q from curl: %w", statusStr, err)
	}
	return body, status, nil
}

// validateAppHTTPURL rejects non-http(s) schemes and hard-blocked IP literals
// (loopback / link-local / metadata). Soft-private hosts are allowed here —
// sandbox L7 + Network Policy PreSeed enforce egress. Hostnames are not
// resolved on the Studio host.
func validateAppHTTPURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsed.Scheme)
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isHardBlockedIP(ip) {
			return fmt.Errorf("requests to private/internal networks are not allowed (%s)", ip)
		}
	}
	return nil
}
