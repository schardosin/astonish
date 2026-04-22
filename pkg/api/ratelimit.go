package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter provides per-key sliding-window rate limiting.
// Each key (IP address or session token) is tracked independently.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*slidingWindow
	limit   int           // max requests per window
	window  time.Duration // window size
	done    chan struct{} // closed by Close() to stop the cleanup goroutine
}

// slidingWindow tracks request timestamps for a single key.
type slidingWindow struct {
	timestamps []time.Time
	lastAccess time.Time
}

// NewRateLimiter creates a rate limiter that allows limit requests per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		windows: make(map[string]*slidingWindow),
		limit:   limit,
		window:  window,
		done:    make(chan struct{}),
	}
	// Periodic cleanup of expired entries
	go rl.cleanup()
	return rl
}

// Close stops the background cleanup goroutine. Safe to call multiple times.
func (rl *RateLimiter) Close() {
	select {
	case <-rl.done:
		// already closed
	default:
		close(rl.done)
	}
}

// Allow checks whether a request from the given key should be allowed.
// Returns true if the request is within the rate limit.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, ok := rl.windows[key]
	if !ok {
		w = &slidingWindow{}
		rl.windows[key] = w
	}
	w.lastAccess = now

	// Evict timestamps outside the current window
	cutoff := now.Add(-rl.window)
	start := 0
	for start < len(w.timestamps) && w.timestamps[start].Before(cutoff) {
		start++
	}
	w.timestamps = w.timestamps[start:]

	if len(w.timestamps) >= rl.limit {
		return false
	}

	w.timestamps = append(w.timestamps, now)
	return true
}

// cleanup periodically removes stale entries from the map.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-2 * rl.window)
			for key, w := range rl.windows {
				if w.lastAccess.Before(cutoff) {
					delete(rl.windows, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.done:
			return
		}
	}
}

// RateLimitConfig holds the rate limiters for different endpoint tiers.
type RateLimitConfig struct {
	// Auth protects unauthenticated endpoints (auth code, status): 10 req/min per IP.
	Auth *RateLimiter
	// API protects authenticated API endpoints: 120 req/min per IP.
	API *RateLimiter
}

// Close shuts down the background cleanup goroutines for all rate limiters.
func (c *RateLimitConfig) Close() {
	if c.Auth != nil {
		c.Auth.Close()
	}
	if c.API != nil {
		c.API.Close()
	}
}

// NewDefaultRateLimitConfig creates rate limiters with sensible defaults.
// This is a single-user desktop app behind auth, not a public API.
// The API limit is intentionally high (3000/min = 50 req/sec) because:
//  - The React UI fires 10-15 parallel requests on every page load
//  - Each app preview iframe loads 3 assets, with many previews per chat
//  - SSE streams, state queries, and polling add continuous load
//  - Remote access via reverse proxy means requests are NOT loopback-exempt
//
// The auth limit stays strict to prevent brute-force code guessing.
func NewDefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		Auth: NewRateLimiter(10, time.Minute),
		API:  NewRateLimiter(3000, time.Minute),
	}
}

// RateLimitMiddleware returns HTTP middleware that enforces per-IP rate limits.
// Loopback addresses are exempt (CLI and local tools should never be throttled).
// Auth endpoints use stricter limits; all other API endpoints use the general limit.
func RateLimitMiddleware(cfg *RateLimitConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Loopback bypass — same reasoning as auth middleware.
		if isLoopback(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path

		// Only rate-limit API paths
		if len(path) < 4 || path[:4] != "/api" {
			next.ServeHTTP(w, r)
			return
		}

		// VNC proxy is exempt — KasmVNC's web client loads 40+ sub-resources
		// (JS bundles, CSS, images, sounds) simultaneously on page load, which
		// would exhaust the API rate budget in a single burst.
		if strings.HasPrefix(path, "/api/browser/vnc/") {
			next.ServeHTTP(w, r)
			return
		}

		// App preview sandbox assets are exempt — every iframe loads sandbox HTML,
		// runtime.js, and tailwind.js. Multiple app previews on screen multiply this.
		if strings.HasPrefix(path, "/api/app-preview/") {
			next.ServeHTTP(w, r)
			return
		}

		// SSE stream endpoints are exempt — they're long-lived connections (one
		// per session), not repeated requests. Counting them would waste budget.
		if strings.HasSuffix(path, "/stream") {
			next.ServeHTTP(w, r)
			return
		}

		ip := extractIP(r.RemoteAddr)

		// Auth endpoints get stricter limits (brute-force protection)
		if len(path) >= 9 && path[:9] == "/api/auth" {
			if !cfg.Auth.Allow(ip) {
				slog.Warn("rate limit exceeded on auth endpoint", "ip", ip, "path", path)
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// All other API endpoints
		if !cfg.API.Allow(ip) {
			slog.Warn("rate limit exceeded on API endpoint", "ip", ip, "path", path)
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractIP strips the port from a remote address, returning just the IP.
func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return fmt.Sprintf("ip:%s", host)
}
