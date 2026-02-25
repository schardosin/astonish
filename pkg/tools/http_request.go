package tools

import (
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"
	"time"

	"google.golang.org/adk/tool"
)

const (
	httpReqDefaultTimeout  = 30 * time.Second
	httpReqMaxTimeout      = 120 * time.Second
	httpReqMaxRedirects    = 5
	httpReqDefaultMaxBytes = 2 * 1024 * 1024 // 2MB
	httpReqUserAgent       = "Astonish/1.0"
)

// httpReqSkipSSRF is a test hook to bypass SSRF checks for httptest servers.
var httpReqSkipSSRF bool

var httpReqAllowedMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"PATCH":   true,
	"DELETE":  true,
	"HEAD":    true,
	"OPTIONS": true,
}

type HttpRequestArgs struct {
	URL        string            `json:"url" jsonschema:"The URL to send the request to (http or https)"`
	Method     string            `json:"method,omitempty" jsonschema:"HTTP method: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS. Default: GET"`
	Headers    map[string]string `json:"headers,omitempty" jsonschema:"Additional HTTP headers as key-value pairs"`
	Body       string            `json:"body,omitempty" jsonschema:"Request body (for POST, PUT, PATCH). Use JSON string for JSON APIs."`
	Credential string            `json:"credential,omitempty" jsonschema:"Name of a stored credential for authentication. The credential's auth header is added automatically."`
	Timeout    int               `json:"timeout,omitempty" jsonschema:"Request timeout in seconds. Default 30, max 120."`
	MaxBytes   int               `json:"max_bytes,omitempty" jsonschema:"Maximum response body size in bytes. Default 2MB."`
}

type HttpRequestResult struct {
	StatusCode  int               `json:"status_code"`
	Status      string            `json:"status"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	DurationMs  int64             `json:"duration_ms"`
	Truncated   bool              `json:"truncated,omitempty"`
}

// HttpRequest makes an HTTP request with optional credential injection.
func HttpRequest(_ tool.Context, args HttpRequestArgs) (HttpRequestResult, error) {
	// Validate URL
	if args.URL == "" {
		return HttpRequestResult{}, fmt.Errorf("url is required")
	}

	parsedURL, err := nurl.ParseRequestURI(args.URL)
	if err != nil {
		return HttpRequestResult{}, fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return HttpRequestResult{}, fmt.Errorf("only http and https URLs are supported, got %q", parsedURL.Scheme)
	}

	// SSRF protection
	if !httpReqSkipSSRF {
		if err := checkSSRF(parsedURL.Hostname()); err != nil {
			return HttpRequestResult{}, err
		}
	}

	// Validate and default method
	method := strings.ToUpper(args.Method)
	if method == "" {
		method = "GET"
	}
	if !httpReqAllowedMethods[method] {
		return HttpRequestResult{}, fmt.Errorf("unsupported HTTP method %q: allowed methods are GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS", args.Method)
	}

	// Timeout bounds
	timeout := httpReqDefaultTimeout
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
		if timeout > httpReqMaxTimeout {
			timeout = httpReqMaxTimeout
		}
	}

	// Max bytes bounds
	maxBytes := int64(httpReqDefaultMaxBytes)
	if args.MaxBytes > 0 {
		maxBytes = int64(args.MaxBytes)
	}

	// Build request body
	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequest(method, args.URL, bodyReader)
	if err != nil {
		return HttpRequestResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default User-Agent
	req.Header.Set("User-Agent", httpReqUserAgent)

	// Auto-detect JSON body
	if args.Body != "" && req.Header.Get("Content-Type") == "" {
		trimmed := strings.TrimSpace(args.Body)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	// Apply user-provided headers
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	// Resolve and inject credential (after user headers — credential takes precedence for auth)
	if args.Credential != "" {
		if credentialStoreVar == nil {
			return HttpRequestResult{}, fmt.Errorf("credential store is not available — cannot resolve credential %q", args.Credential)
		}
		headerKey, headerValue, err := credentialStoreVar.Resolve(args.Credential)
		if err != nil {
			return HttpRequestResult{}, fmt.Errorf("failed to resolve credential %q: %w", args.Credential, err)
		}
		req.Header.Set(headerKey, headerValue)
	}

	// Build HTTP client
	redirectCount := 0
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			redirectCount++
			if redirectCount > httpReqMaxRedirects {
				return fmt.Errorf("too many redirects (max %d)", httpReqMaxRedirects)
			}
			return nil
		},
	}

	// Execute request
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return HttpRequestResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	durationMs := time.Since(start).Milliseconds()

	// Read response body with size limit
	// Read maxBytes+1 to detect truncation
	limitedReader := io.LimitReader(resp.Body, maxBytes+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return HttpRequestResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	truncated := false
	if int64(len(bodyBytes)) > maxBytes {
		bodyBytes = bodyBytes[:maxBytes]
		truncated = true
	}

	bodyStr := string(bodyBytes)

	// Pretty-print JSON responses
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") || strings.Contains(ct, "+json") {
		if pretty, err := prettyJSON(bodyStr); err == nil {
			bodyStr = pretty
		}
	}

	// Collect all response headers (first value per key)
	respHeaders := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			respHeaders[k] = vals[0]
		}
	}

	return HttpRequestResult{
		StatusCode:  resp.StatusCode,
		Status:      resp.Status,
		Headers:     respHeaders,
		Body:        bodyStr,
		ContentType: ct,
		DurationMs:  durationMs,
		Truncated:   truncated,
	}, nil
}


