package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	for i := 0; i < 5; i++ {
		if !rl.Allow("test-key") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		rl.Allow("test-key")
	}
	if rl.Allow("test-key") {
		t.Fatal("4th request should be blocked")
	}
}

func TestRateLimiter_SeparateKeys(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	rl.Allow("key-a")
	rl.Allow("key-a")

	// key-a is exhausted
	if rl.Allow("key-a") {
		t.Fatal("key-a should be blocked")
	}
	// key-b is independent
	if !rl.Allow("key-b") {
		t.Fatal("key-b should be allowed")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	rl.Allow("test-key")
	rl.Allow("test-key")

	// Should be blocked
	if rl.Allow("test-key") {
		t.Fatal("should be blocked at limit")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow("test-key") {
		t.Fatal("should be allowed after window expires")
	}
}

func TestRateLimitMiddleware_AllowsLoopback(t *testing.T) {
	cfg := &RateLimitConfig{
		Auth: NewRateLimiter(1, time.Minute),
		API:  NewRateLimiter(1, time.Minute),
	}

	handler := RateLimitMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Loopback should always be allowed, even after limit is exhausted
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/settings/config", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("loopback request %d: got %d, want 200", i+1, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_BlocksAuthEndpoint(t *testing.T) {
	cfg := &RateLimitConfig{
		Auth: NewRateLimiter(2, time.Minute),
		API:  NewRateLimiter(100, time.Minute),
	}

	handler := RateLimitMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 should pass
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/auth/code", nil)
		req.RemoteAddr = "192.168.1.100:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("auth request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	// 3rd should be blocked
	req := httptest.NewRequest("GET", "/api/auth/code", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("auth request 3: got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("missing Retry-After header")
	}
}

func TestRateLimitMiddleware_BlocksAPIEndpoint(t *testing.T) {
	cfg := &RateLimitConfig{
		Auth: NewRateLimiter(100, time.Minute),
		API:  NewRateLimiter(2, time.Minute),
	}

	handler := RateLimitMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 should pass
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/studio/sessions", nil)
		req.RemoteAddr = "10.0.0.1:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("API request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	// 3rd should be blocked
	req := httptest.NewRequest("GET", "/api/studio/sessions", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("API request 3: got %d, want 429", rec.Code)
	}
}

func TestRateLimitMiddleware_SkipsNonAPIPaths(t *testing.T) {
	cfg := &RateLimitConfig{
		Auth: NewRateLimiter(1, time.Minute),
		API:  NewRateLimiter(1, time.Minute),
	}

	handler := RateLimitMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Non-API paths should never be rate-limited
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/index.html", nil)
		req.RemoteAddr = "10.0.0.1:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("non-API request %d: got %d, want 200", i+1, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_AuthAndAPIAreSeparate(t *testing.T) {
	cfg := &RateLimitConfig{
		Auth: NewRateLimiter(1, time.Minute),
		API:  NewRateLimiter(1, time.Minute),
	}

	handler := RateLimitMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust auth limit
	req := httptest.NewRequest("GET", "/api/auth/code", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first auth request: got %d, want 200", rec.Code)
	}

	// Auth is now blocked
	req = httptest.NewRequest("GET", "/api/auth/code", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second auth request: got %d, want 429", rec.Code)
	}

	// But API should still work (different limiter)
	req = httptest.NewRequest("GET", "/api/studio/sessions", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first API request: got %d, want 200", rec.Code)
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:8080", "ip:192.168.1.1"},
		{"[::1]:8080", "ip:::1"},
		{"127.0.0.1:0", "ip:127.0.0.1"},
		{"invalid", "invalid"}, // no port, returns as-is
	}
	for _, tt := range tests {
		got := extractIP(tt.input)
		if got != tt.want {
			t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
