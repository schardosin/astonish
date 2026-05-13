package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// PlatformInitRequest is the request body for POST /api/platform/init.
type PlatformInitRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
	OrgName  string `json:"org_name"`
	OrgSlug  string `json:"org_slug"`
}

// PlatformInitResponse is the response for POST /api/platform/init.
type PlatformInitResponse struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	RestartRequired bool   `json:"restart_required"`
	Error           string `json:"error,omitempty"`
}

// PlatformInitHandler handles POST /api/platform/init.
//
// It accepts PostgreSQL connection parameters, creates the platform database,
// runs migrations, generates a JWT secret, and saves the config. This endpoint
// only works when the system is NOT already running in platform mode.
func PlatformInitHandler(w http.ResponseWriter, r *http.Request) {
	// Guard: refuse if already in platform mode.
	cfg, err := config.LoadAppConfig()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to load config: " + err.Error(),
		})
		return
	}
	if cfg.Storage.IsPostgres() {
		respondJSON(w, http.StatusConflict, PlatformInitResponse{
			Error: "Platform mode is already configured. To reconfigure, edit config.yaml directly.",
		})
		return
	}

	var req PlatformInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Validate required fields.
	if req.Host == "" {
		req.Host = "localhost"
	}
	if req.Port == 0 {
		req.Port = 5432
	}
	if req.User == "" {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "PostgreSQL username is required",
		})
		return
	}
	if req.Password == "" {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "PostgreSQL password is required",
		})
		return
	}
	if req.SSLMode == "" {
		req.SSLMode = "prefer"
	}
	if req.OrgName == "" {
		req.OrgName = "My Organization"
	}
	if req.OrgSlug == "" {
		req.OrgSlug = "default"
	}

	// Validate slug format.
	for _, ch := range req.OrgSlug {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
				Error: "Organization slug must be lowercase alphanumeric with hyphens/underscores",
			})
			return
		}
	}

	// Generate a unique instance suffix for this deployment.
	// This namespaces all databases so multiple Astonish instances can share a PG host.
	suffix := config.GenerateInstanceSuffix()

	// Build a temporary DSN (pointing to any DB on the host — we'll create the real one).
	tempDSN := pgstore.BuildDSN(req.Host, req.Port, req.User, req.Password, "postgres", req.SSLMode)

	// Check for suffix collision (extremely unlikely, but handle it)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	for attempts := 0; attempts < 5; attempts++ {
		exists, checkErr := pgstore.PlatformDBExists(ctx, tempDSN, suffix)
		if checkErr != nil {
			respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
				Error: "Failed to check database existence: " + cleanPGError(checkErr.Error()),
			})
			return
		}
		if !exists {
			break
		}
		suffix = config.GenerateInstanceSuffix()
	}

	// Build the platform DSN with the actual platform DB name.
	platformDBName := config.PlatformDBName(suffix)
	platformDSN := pgstore.BuildDSN(req.Host, req.Port, req.User, req.Password, platformDBName, req.SSLMode)

	// Bootstrap: create DB, roles, and run migrations.
	if err := pgstore.BootstrapPlatform(ctx, platformDSN, suffix); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to initialize platform database: " + cleanPGError(err.Error()),
		})
		return
	}

	// Generate JWT secret and save config.
	jwtSecret := config.GenerateJWTSecret()

	cfg.Storage.Backend = "postgres"
	cfg.Storage.Postgres.PlatformDSN = platformDSN
	cfg.Storage.Postgres.InstanceSuffix = suffix
	cfg.Storage.Auth.Mode = "builtin"
	cfg.Storage.Auth.JWTSecret = jwtSecret
	cfg.Storage.Auth.DefaultOrgName = req.OrgName
	cfg.Storage.Auth.DefaultOrgSlug = req.OrgSlug

	if err := config.SaveAppConfig(cfg); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Platform DB initialized but failed to save config: " + err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, PlatformInitResponse{
		Success:         true,
		Message:         "Platform initialized successfully. Please restart Astonish to activate platform mode.",
		RestartRequired: true,
	})
}

// cleanPGError removes sensitive connection details from PostgreSQL error messages.
func cleanPGError(msg string) string {
	// Remove anything that looks like a connection string
	if idx := strings.Index(msg, "postgres://"); idx >= 0 {
		end := strings.IndexAny(msg[idx:], " \n\t")
		if end < 0 {
			msg = msg[:idx] + "[redacted]"
		} else {
			msg = msg[:idx] + "[redacted]" + msg[idx+end:]
		}
	}
	return msg
}

// DeploymentModeHandler handles GET /api/platform/mode.
// Returns the current deployment mode so the frontend can adapt.
func DeploymentModeHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"mode": "personal",
		})
		return
	}

	mode := "personal"
	if cfg.Storage.IsPostgres() {
		mode = "platform"
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"mode":       mode,
		"configured": cfg.Storage.Backend != "",
	})
}

// PlatformInitStatusHandler handles GET /api/platform/init/status.
// Returns whether platform mode is already configured and initialized.
func PlatformInitStatusHandler(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"configured":  false,
			"initialized": false,
		})
		return
	}

	configured := cfg.Storage.IsPostgres()
	initialized := false

	if configured {
		// Try connecting to verify the database is actually accessible.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, pgStore, pgErr := pgstore.NewPlatformServices(ctx, cfg.Storage.Postgres)
		if pgErr == nil {
			initialized = true
			pgStore.Close()
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"configured":  configured,
		"initialized": initialized,
	})
}
