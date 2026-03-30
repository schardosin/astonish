// Package httpool provides a shared, pool-aware HTTP client for LLM providers.
// Using a single transport with tuned connection pool settings avoids
// connection churn in fleet scenarios where many providers run concurrently.
package httpool

import (
	"net"
	"net/http"
	"time"
)

// sharedTransport is a pool-aware HTTP transport shared by all LLM providers.
// It overrides Go's conservative defaults to handle concurrent fleet workloads
// where multiple agents call different provider endpoints simultaneously.
var sharedTransport = &http.Transport{
	// Connection pooling
	MaxIdleConns:        200,               // total across all hosts (default: 100)
	MaxIdleConnsPerHost: 20,                // per-host idle pool (default: 2)
	MaxConnsPerHost:     0,                 // unlimited active per host
	IdleConnTimeout:     120 * time.Second, // keep idle connections longer (default: 90s)

	// Timeouts for connection establishment
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second, // TCP connect timeout
		KeepAlive: 30 * time.Second, // TCP keepalive interval
	}).DialContext,

	// TLS handshake timeout
	TLSHandshakeTimeout: 15 * time.Second,

	// Expect: 100-continue timeout (for large POST bodies)
	ExpectContinueTimeout: 1 * time.Second,

	// Response header timeout (time to wait for response headers after request is sent)
	ResponseHeaderTimeout: 120 * time.Second,

	// Force HTTP/2 (already the default when using TLS, but explicit for clarity)
	ForceAttemptHTTP2: true,
}

// Client returns an HTTP client backed by the shared transport pool.
// The provided timeout applies to the overall request lifecycle (connect +
// send + response headers + body). For streaming LLM responses, pass 0 to
// disable the timeout (the caller controls cancellation via context).
func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: sharedTransport,
		Timeout:   timeout,
	}
}

// StreamingClient returns an HTTP client suitable for SSE / streaming
// responses. It uses the shared transport but has no overall timeout since
// streaming responses can run indefinitely.
func StreamingClient() *http.Client {
	return &http.Client{
		Transport: sharedTransport,
		Timeout:   0, // no timeout — caller uses context
	}
}

// Transport returns the shared pool-aware transport for use as the base
// round-tripper inside custom transports (e.g. auth-injecting wrappers).
func Transport() http.RoundTripper {
	return sharedTransport
}
