package api

import (
	"context"
	"net/http"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

// healthBackend holds a reference to the platform backend for readiness checks.
// Set during route registration when in platform mode.
var healthBackend store.PlatformBackend

// SetHealthBackend sets the backend reference for health checks.
func SetHealthBackend(b store.PlatformBackend) {
	healthBackend = b
}

// SetHealthPGStore is a backward-compatible alias for SetHealthBackend.
// Deprecated: use SetHealthBackend.
func SetHealthPGStore(b store.PlatformBackend) {
	healthBackend = b
}

// HealthzHandler is the liveness probe endpoint.
// Returns 200 if the HTTP server is alive and responsive.
// GET /api/healthz
func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadyzHandler is the readiness probe endpoint.
// In platform mode, verifies database connectivity via PlatformDB().Ping().
// In personal mode, always returns 200.
// GET /api/readyz
func ReadyzHandler(w http.ResponseWriter, _ *http.Request) {
	if healthBackend == nil {
		// Personal mode or backend not configured — always ready
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Platform mode: check DB connectivity via PlatformDB ping
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	db := healthBackend.PlatformDB()
	if db == nil {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if err := db.PingContext(ctx); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"error":  "database unreachable",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
