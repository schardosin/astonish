package llmerror

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// LLMError is a structured error returned by LLM providers when an API call
// fails. It preserves the HTTP status code and provider name so callers can
// make retry/display decisions without parsing error strings.
type LLMError struct {
	StatusCode int           // HTTP status code (429, 400, 500, etc.)
	Provider   string        // Provider name ("anthropic", "openai", etc.)
	Message    string        // Human-readable error message
	Body       string        // Raw response body (for debugging)
	Retryable  bool          // Whether this error is worth retrying
	RetryAfter time.Duration // Suggested wait before retry (from Retry-After header)
}

func (e *LLMError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %d %s", e.Provider, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s: %d %s", e.Provider, e.StatusCode, http.StatusText(e.StatusCode))
}

// NewLLMError creates an LLMError with automatic retryable classification.
func NewLLMError(provider string, statusCode int, message, body string) *LLMError {
	return &LLMError{
		StatusCode: statusCode,
		Provider:   provider,
		Message:    message,
		Body:       body,
		Retryable:  isRetryableStatus(statusCode),
	}
}

// NewFromResponse creates an LLMError from an HTTP response, parsing
// the Retry-After header if present.
func NewFromResponse(provider string, resp *http.Response, body []byte) *LLMError {
	e := NewLLMError(provider, resp.StatusCode, resp.Status, string(body))
	e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	return e
}

// IsRetryable returns true if the error is an LLMError that should be retried.
// This includes 429 (rate limit), 500 (server error / proxy failure),
// 502/503 (server overload), 504 (timeout), and 408 (request timeout).
func IsRetryable(err error) bool {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.Retryable
	}
	return false
}

// IsRateLimited returns true if the error is specifically a 429 rate limit.
func IsRateLimited(err error) bool {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.StatusCode == http.StatusTooManyRequests
	}
	return false
}

// IsAuthError returns true if the error is a 401 or 403 authentication failure.
func IsAuthError(err error) bool {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.StatusCode == http.StatusUnauthorized || llmErr.StatusCode == http.StatusForbidden
	}
	return false
}

// IsServerError returns true if the error is a 5xx server error.
func IsServerError(err error) bool {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.StatusCode >= 500
	}
	return false
}

// GetRetryAfter extracts the RetryAfter duration from an LLMError, if present.
func GetRetryAfter(err error) time.Duration {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.RetryAfter
	}
	return 0
}

// StatusCode extracts the HTTP status code from an LLMError. Returns 0 if
// the error is not an LLMError.
func StatusCode(err error) int {
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr.StatusCode
	}
	return 0
}

// isRetryableStatus classifies HTTP status codes as retryable or not.
// 500 is included because LLM provider proxies (e.g., Bifrost) commonly
// return 500 for transient upstream failures (timeouts, connection resets)
// that succeed on retry. The retry budget (3 attempts with backoff) limits
// the cost of retrying genuine server bugs.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout,      // 504
		http.StatusRequestTimeout:      // 408
		return true
	default:
		return false
	}
}

// parseRetryAfter parses the Retry-After header value. It supports both
// delay-seconds (e.g., "30") and HTTP-date formats.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try as seconds first (most common for rate limiting)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}
