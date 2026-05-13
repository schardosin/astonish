package api

import (
	"context"
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// healthPGStore holds a reference to the PG store for readiness checks.
// Set during route registration when in platform mode.
var healthPGStore *pgstore.PGStore

// SetHealthPGStore sets the PG store reference for health checks.
func SetHealthPGStore(pg *pgstore.PGStore) {
	healthPGStore = pg
}

// HealthzHandler is the liveness probe endpoint.
// Returns 200 if the HTTP server is alive and responsive.
// GET /api/healthz
func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadyzHandler is the readiness probe endpoint.
// In platform mode, verifies PostgreSQL connectivity.
// In personal mode, always returns 200.
// GET /api/readyz
func ReadyzHandler(w http.ResponseWriter, _ *http.Request) {
	if healthPGStore == nil {
		// Personal mode or PG not configured — always ready
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Platform mode: check PG connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := healthPGStore.PoolManager().PlatformPool(ctx)
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"error":  "database pool error",
		})
		return
	}

	if err := pool.Ping(ctx); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"error":  "database unreachable",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
