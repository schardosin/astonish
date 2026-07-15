package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSPMiddleware_AllowsBlobMedia(t *testing.T) {
	handler := CSPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("missing Content-Security-Policy header")
	}
	if !strings.Contains(csp, "media-src 'self' blob:") {
		t.Fatalf("CSP must allow blob: media for recording playback, got %q", csp)
	}
	// Ensure img-src blob: was not regressed.
	if !strings.Contains(csp, "img-src 'self' data: blob:") {
		t.Fatalf("CSP missing img-src blob: allowance, got %q", csp)
	}
}
