package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/migration"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// MigrationManager handles the file→database migration lifecycle.
type MigrationManager struct {
	pgStore   *pgstore.PGStore
	authCfg   config.PlatformAuthConfig
	storeCfg  config.StorageConfig
	configDir string
	appCfg    *config.AppConfig

	mu       sync.Mutex
	migrator *migration.Migrator
	running  bool
	summary  *migration.Summary

	// SSE subscribers for progress updates
	subMu       sync.Mutex
	subscribers map[chan migration.Progress]struct{}
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(pgStore *pgstore.PGStore, authCfg config.PlatformAuthConfig, storeCfg config.StorageConfig, configDir string, appCfg *config.AppConfig) *MigrationManager {
	return &MigrationManager{
		pgStore:     pgStore,
		authCfg:     authCfg,
		storeCfg:    storeCfg,
		configDir:   configDir,
		appCfg:      appCfg,
		subscribers: make(map[chan migration.Progress]struct{}),
	}
}

// RegisterMigrationRoutes registers the migration API endpoints.
// These routes bypass auth middleware (they're part of the initial setup flow).
func RegisterMigrationRoutes(router *mux.Router, mm *MigrationManager) {
	router.HandleFunc("/api/migration/status", mm.handleStatus).Methods("GET")
	router.HandleFunc("/api/migration/start", mm.handleStart).Methods("POST")
	router.HandleFunc("/api/migration/progress", mm.handleProgress).Methods("GET")
}

// RegisterReimportRoutes registers the admin re-import endpoint.
// This is separate from migration routes because it must always be available
// in platform mode, even after migration is complete. Requires admin auth.
func RegisterReimportRoutes(router *mux.Router, pgStore *pgstore.PGStore, configDir string) {
	router.HandleFunc("/api/admin/reimport-flows", handleReimportFlows(pgStore, configDir)).Methods("POST")
}

// IsMigrationAvailable checks if file data exists and migration hasn't been done yet.
func (mm *MigrationManager) IsMigrationAvailable() bool {
	if migration.IsMigrationComplete(mm.configDir) {
		return false
	}
	return migration.HasFileData(mm.configDir)
}

// --- Handler: GET /api/migration/status ---

func (mm *MigrationManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	mm.mu.Lock()
	running := mm.running
	summary := mm.summary
	mm.mu.Unlock()

	resp := map[string]any{
		"migration_available": mm.IsMigrationAvailable(),
		"running":             running,
	}

	if summary != nil {
		resp["summary"] = summary
	}

	respondJSON(w, http.StatusOK, resp)
}

// --- Handler: POST /api/migration/start ---

type migrationStartRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (mm *MigrationManager) handleStart(w http.ResponseWriter, r *http.Request) {
	mm.mu.Lock()
	if mm.running {
		mm.mu.Unlock()
		respondError(w, http.StatusConflict, "migration already in progress")
		return
	}
	mm.mu.Unlock()

	var req migrationStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate input
	if err := validateEmail(req.Email); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = strings.Split(req.Email, "@")[0]
	}

	ctx := r.Context()

	// Step 1: Create the platform user
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to process password")
		return
	}

	user := &store.User{
		ID:           uuid.New().String(),
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		PasswordHash: string(hash),
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	if err := mm.pgStore.Users().Create(ctx, user); err != nil {
		slog.Error("migration: failed to create user", "email", user.Email, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Step 2: Provision org + team (reuse the same logic as first-registration)
	orgSlug := mm.authCfg.GetDefaultOrgSlug()
	orgName := mm.authCfg.GetDefaultOrgName()
	dbName := pgstore.OrgDBName(mm.pgStore.InstanceSuffix(), orgSlug)
	teamSlug := "general"

	org := &store.Organization{
		ID:        uuid.New().String(),
		Name:      orgName,
		Slug:      orgSlug,
		DBName:    dbName,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	if err := mm.pgStore.Organizations().Create(ctx, org); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	if err := mm.pgStore.ProvisionOrg(ctx, org.ID, orgSlug); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to provision organization database")
		return
	}

	// Make user the org owner
	if err := mm.pgStore.Organizations().AddMember(ctx, user.ID, org.ID, "owner"); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add user to organization")
		return
	}

	// Create default team
	orgDataStore, err := mm.pgStore.ForOrg(orgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to connect to organization database")
		return
	}

	defaultTeam := &store.Team{
		ID:         uuid.New().String(),
		Name:       "General",
		Slug:       teamSlug,
		SchemaName: pgstore.TeamSchemaName(teamSlug),
		CreatedAt:  time.Now(),
	}
	if err := orgDataStore.Teams().CreateTeam(ctx, defaultTeam); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create default team")
		return
	}

	if err := orgDataStore.ProvisionTeam(ctx, teamSlug); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to provision team schema")
		return
	}

	if err := orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   user.ID,
		TeamID:   defaultTeam.ID,
		Role:     "admin",
		JoinedAt: time.Now(),
	}); err != nil {
		slog.Warn("migration: failed to add user to team", "error", err)
	}

	if err := orgDataStore.ProvisionPersonalSchema(ctx, user.ID); err != nil {
		slog.Warn("migration: failed to provision personal schema", "error", err)
	}

	// Step 3: Start migration in background
	migrator := migration.New(migration.Config{
		ConfigDir: mm.configDir,
		PGStore:   mm.pgStore,
		OrgSlug:   orgSlug,
		TeamSlug:  teamSlug,
		UserID:    user.ID,
		AppCfg:    mm.appCfg,
	})

	migrator.SetProgressFunc(func(p migration.Progress) {
		mm.broadcastProgress(p)
	})

	mm.mu.Lock()
	mm.migrator = migrator
	mm.running = true
	mm.summary = nil
	mm.mu.Unlock()

	go func() {
		migCtx := context.Background()
		summary, err := migrator.Run(migCtx)
		if err != nil {
			slog.Error("migration failed", "error", err)
		}

		mm.mu.Lock()
		mm.running = false
		mm.summary = summary
		mm.mu.Unlock()

		// Send final complete event
		mm.broadcastProgress(migration.Progress{
			Category: "complete",
			Status:   "done",
		})
	}()

	respondJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"user_id": user.ID,
		"org":     orgSlug,
		"team":    teamSlug,
	})
}

// --- Handler: GET /api/migration/progress (SSE) ---

func (mm *MigrationManager) handleProgress(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Create a channel for this subscriber
	ch := make(chan migration.Progress, 64)
	mm.subMu.Lock()
	mm.subscribers[ch] = struct{}{}
	mm.subMu.Unlock()

	defer func() {
		mm.subMu.Lock()
		delete(mm.subscribers, ch)
		mm.subMu.Unlock()
		close(ch)
	}()

	// Send current status first
	mm.mu.Lock()
	if mm.migrator != nil {
		for _, p := range mm.migrator.GetStatus() {
			data, _ := json.Marshal(p)
			fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
		}
	}
	if mm.summary != nil {
		data, _ := json.Marshal(mm.summary)
		fmt.Fprintf(w, "event: complete\ndata: %s\n\n", data)
	}
	mm.mu.Unlock()
	flusher.Flush()

	// Stream updates
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			if p.Category == "complete" {
				mm.mu.Lock()
				summary := mm.summary
				mm.mu.Unlock()
				if summary != nil {
					data, _ := json.Marshal(summary)
					fmt.Fprintf(w, "event: complete\ndata: %s\n\n", data)
				}
			} else {
				data, _ := json.Marshal(p)
				fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
			}
			flusher.Flush()
		}
	}
}

// broadcastProgress sends a progress update to all SSE subscribers.
func (mm *MigrationManager) broadcastProgress(p migration.Progress) {
	mm.subMu.Lock()
	defer mm.subMu.Unlock()

	for ch := range mm.subscribers {
		select {
		case ch <- p:
		default:
			// Drop if subscriber is slow
		}
	}
}

// --- Handler: POST /api/admin/reimport-flows ---
// Re-imports flows from the personal-mode filesystem into a team's database.
// Requires admin/owner authentication. Idempotent — safe to run multiple times.
//
// Request body:
//
//	{ "org": "optional-org-slug", "team": "general" }
//
// If org is omitted, uses the caller's org. If team is omitted, defaults to "general".
func handleReimportFlows(pgStore *pgstore.PGStore, configDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAdmin(w, r)
		if user == nil {
			return
		}

		var req struct {
			Org  string `json:"org"`
			Team string `json:"team"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Accept empty body — use defaults
			req.Org = ""
			req.Team = ""
		}

		orgSlug := req.Org
		if orgSlug == "" {
			orgSlug = user.OrgSlug
		}
		teamSlug := req.Team
		if teamSlug == "" {
			teamSlug = "general"
		}

		// Get the team data store
		orgDS, err := pgStore.ForOrg(orgSlug)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to connect to organization database: "+err.Error())
			return
		}

		teamDS := orgDS.ForTeam(teamSlug)

		// Run the import
		result, err := migration.ImportFlows(r.Context(), configDir, teamDS, nil)
		if err != nil {
			slog.Error("reimport-flows failed", "org", orgSlug, "team", teamSlug, "error", err)
			resp := map[string]any{
				"status":   "error",
				"error":    err.Error(),
				"total":    0,
				"imported": 0,
			}
			if result != nil {
				resp["total"] = result.Total
				resp["imported"] = result.Imported
			}
			respondJSON(w, http.StatusInternalServerError, resp)
			return
		}

		slog.Info("reimport-flows completed", "org", orgSlug, "team", teamSlug,
			"total", result.Total, "imported", result.Imported)

		respondJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"total":    result.Total,
			"imported": result.Imported,
			"org":      orgSlug,
			"team":     teamSlug,
		})
	}
}
