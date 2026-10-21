package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/SAP/astonish/pkg/store/pgutil"
)

// platformInitMu serializes both bootstrap entry points through config persistence.
var platformInitMu sync.Mutex

// minBootstrapTokenLen is the minimum acceptable length for the operator
// bootstrap token. A short token is brute-forceable; refusing it fails closed
// rather than letting a weak value authorize first-run setup.
const minBootstrapTokenLen = 16

// requireBootstrapAuthority checks an operator-provisioned capability. Setup is
// auth-exempt before users exist; neither loopback nor forwarded headers prove
// authority. The token is deliberately never generated or returned over HTTP.
func requireBootstrapAuthority(w http.ResponseWriter, r *http.Request) bool {
	expected := os.Getenv("ASTONISH_BOOTSTRAP_TOKEN")
	provided := r.Header.Get("X-Astonish-Bootstrap-Token")
	// A missing or too-short expected token can never authorize: constant-time
	// compare against a weak secret still accepts that weak secret.
	if len(expected) < minBootstrapTokenLen || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
		respondJSON(w, http.StatusForbidden, PlatformInitResponse{
			Error: "Explicit bootstrap authorization required",
		})
		return false
	}
	return true
}

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
	platformInitMu.Lock()
	defer platformInitMu.Unlock()

	// Guard: refuse if already in platform mode.
	cfg, err := config.LoadAppConfig()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to load config: " + err.Error(),
		})
		return
	}
	if cfg.Storage.IsPostgres() || cfg.Storage.IsSQLite() {
		respondJSON(w, http.StatusConflict, PlatformInitResponse{
			Error: "Platform mode is already configured. To reconfigure, edit config.yaml directly.",
		})
		return
	}

	if !requireBootstrapAuthority(w, r) {
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

	// Build a temporary DSN and open a single *sql.DB connection for the entire
	// admin phase. Using *sql.DB keeps the TCP socket pooled, which is critical
	// for kubectl port-forward tunnels.
	tempDSN := pgutil.BuildDSN(req.Host, req.Port, req.User, req.Password, "postgres", req.SSLMode)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	adminDB, connErr := sql.Open("pgx", tempDSN)
	if connErr != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to connect to PostgreSQL: " + cleanPGError(connErr.Error()),
		})
		return
	}
	adminDB.SetMaxOpenConns(1)
	adminDB.SetMaxIdleConns(1)
	if connErr = adminDB.PingContext(ctx); connErr != nil {
		adminDB.Close()
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to connect to PostgreSQL: " + cleanPGError(connErr.Error()),
		})
		return
	}

	for attempts := 0; attempts < 5; attempts++ {
		exists, checkErr := pgutil.PlatformDBExistsDB(ctx, adminDB, suffix)
		if checkErr != nil {
			adminDB.Close()
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
	platformDSN := pgutil.BuildDSN(req.Host, req.Port, req.User, req.Password, platformDBName, req.SSLMode)

	// Bootstrap: create DB and run migrations via entstore.
	// Pass adminDB so bootstrap reuses our existing connection.
	entCfg := entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: suffix,
	}
	if err := entstore.BootstrapPlatform(ctx, entCfg, adminDB); err != nil {
		adminDB.Close()
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to initialize platform database: " + cleanPGError(err.Error()),
		})
		return
	}
	adminDB.Close()

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

// SQLitePlatformInitRequest is the request body for POST /api/platform/init/sqlite.
type SQLitePlatformInitRequest struct {
	DataDir       string `json:"data_dir"`
	OrgName       string `json:"org_name"`
	OrgSlug       string `json:"org_slug"`
	AuthMode      string `json:"auth_mode,omitempty"`
	AdminEmail    string `json:"admin_email"`
	AdminName     string `json:"admin_name"`
	AdminPassword string `json:"admin_password"`
}

// SQLitePlatformInitHandler handles POST /api/platform/init/sqlite.
//
// It creates the SQLite platform database, bootstraps the first user/org/team,
// generates a JWT secret and master encryption key, and saves the config.
// This endpoint only works when the system is NOT already running in platform mode.
func SQLitePlatformInitHandler(w http.ResponseWriter, r *http.Request) {
	sqlitePlatformInitHandler(w, r, prepareSQLiteSetupMasterKey)
}

// sqlitePlatformInitHandler keeps key preparation injectable per request without
// changing the public handler or introducing a process-wide mutable hook.
func sqlitePlatformInitHandler(w http.ResponseWriter, r *http.Request, prepareMasterKey func() error) {
	platformInitMu.Lock()
	defer platformInitMu.Unlock()

	// Guard: refuse if already in platform mode.
	cfg, err := config.LoadAppConfig()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to load config",
		})
		return
	}
	if cfg.Storage.IsPostgres() || cfg.Storage.IsSQLite() {
		respondJSON(w, http.StatusConflict, PlatformInitResponse{
			Error: "Platform mode is already configured. To reconfigure, edit config.yaml directly.",
		})
		return
	}

	if !requireBootstrapAuthority(w, r) {
		return
	}
	var req SQLitePlatformInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.AuthMode == "" {
		req.AuthMode = config.AuthModeNone
	}
	if err := config.ValidateAuthMode("sqlite", req.AuthMode); err != nil {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{Error: "Unsupported auth mode"})
		return
	}
	if req.AuthMode == config.AuthModeBuiltin && req.AdminEmail == "" {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "Admin email is required",
		})
		return
	}
	if req.AuthMode == config.AuthModeBuiltin && (req.AdminPassword == "" || len(req.AdminPassword) < 8) {
		respondJSON(w, http.StatusBadRequest, PlatformInitResponse{
			Error: "Admin password must be at least 8 characters",
		})
		return
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

	// Determine data directory.
	dataDir := req.DataDir
	if dataDir == "" {
		dataDir = cfg.Storage.SQLite.GetDataDir()
	}

	// Prepare filesystem state only after authorization and request validation.
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{Error: "Failed to create SQLite data directory"})
		return
	}
	if req.AuthMode == config.AuthModeBuiltin {
		if err := prepareMasterKey(); err != nil {
			respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{Error: "Failed to prepare master encryption key"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Bootstrap: create SQLite platform database and run schema migrations.
	entCfg := entstore.Config{
		DSN:     "file:" + filepath.Join(dataDir, "platform.db"),
		DataDir: dataDir,
	}
	if err := entstore.BootstrapPlatform(ctx, entCfg, nil); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to initialize SQLite database",
		})
		return
	}

	// Open the store to seed admin user, org, and team.
	entStore, err := entstore.New(ctx, entCfg)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to open SQLite database after bootstrap",
		})
		return
	}
	defer entStore.Close()

	if req.AuthMode == config.AuthModeNone {
		if err := store.ProvisionSingleUser(ctx, entStore, req.OrgName, req.OrgSlug); err != nil {
			respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{Error: "Failed to create local workspace"})
			return
		}
		cfg.Storage.Backend = "sqlite"
		cfg.Storage.SQLite.DataDir = dataDir
		cfg.Storage.Auth.Mode = config.AuthModeNone
		cfg.Storage.Auth.DefaultOrgName = req.OrgName
		cfg.Storage.Auth.DefaultOrgSlug = req.OrgSlug
		if err := config.SaveAppConfig(cfg); err != nil {
			respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{Error: "Platform initialized but failed to save config"})
			return
		}
		respondJSON(w, http.StatusOK, PlatformInitResponse{
			Success:         true,
			Message:         "SQLite single-user workspace initialized successfully. Please restart Astonish to activate it.",
			RestartRequired: true,
		})
		return
	}

	// Seed admin user.
	now := time.Now()
	adminDisplayName := req.AdminName
	if adminDisplayName == "" {
		adminDisplayName = req.AdminEmail
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to hash admin password",
		})
		return
	}
	userID := uuid.New().String()
	adminUser := &store.User{
		ID:           userID,
		Email:        req.AdminEmail,
		DisplayName:  adminDisplayName,
		PasswordHash: string(hash),
		PlatformRole: "superadmin",
		Status:       "active",
		CreatedAt:    now,
	}
	if err := entStore.Users().Create(ctx, adminUser); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to create admin user",
		})
		return
	}

	// Seed organization.
	orgID := uuid.New().String()
	org := &store.Organization{
		ID:        orgID,
		Name:      req.OrgName,
		Slug:      req.OrgSlug,
		Status:    "active",
		CreatedAt: now,
	}
	if err := entStore.Organizations().Create(ctx, org); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to create organization",
		})
		return
	}

	// Add admin as org owner.
	if err := entStore.Organizations().AddMember(ctx, userID, orgID, "owner"); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to add org membership",
		})
		return
	}

	// Provision org.
	if err := entStore.ProvisionOrg(ctx, orgID, req.OrgSlug); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to provision org",
		})
		return
	}

	// Create default team.
	orgStore, err := entStore.ForOrg(req.OrgSlug)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to open org store",
		})
		return
	}
	team := &store.Team{
		ID:         uuid.New().String(),
		Name:       "General",
		Slug:       "general",
		SchemaName: entstore.TeamSchemaName("general"),
		CreatedAt:  now,
	}
	if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to create team",
		})
		return
	}

	if err := orgStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID: userID, TeamID: team.ID, Role: "admin", JoinedAt: now,
	}); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to add team membership",
		})
		return
	}

	// Provision team and personal schema.
	if err := orgStore.ProvisionTeam(ctx, "general"); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to provision team",
		})
		return
	}
	if err := orgStore.ProvisionPersonalSchema(ctx, userID); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Failed to provision personal schema",
		})
		return
	}

	// Generate JWT secret and save config.
	jwtSecret := config.GenerateJWTSecret()

	cfg.Storage.Backend = "sqlite"
	cfg.Storage.SQLite.DataDir = dataDir
	cfg.Storage.Auth.Mode = "builtin"
	cfg.Storage.Auth.JWTSecret = jwtSecret
	cfg.Storage.Auth.DefaultOrgName = req.OrgName
	cfg.Storage.Auth.DefaultOrgSlug = req.OrgSlug

	if err := config.SaveAppConfig(cfg); err != nil {
		respondJSON(w, http.StatusInternalServerError, PlatformInitResponse{
			Error: "Platform initialized but failed to save config",
		})
		return
	}

	respondJSON(w, http.StatusOK, PlatformInitResponse{
		Success:         true,
		Message:         "SQLite platform initialized successfully. Please restart Astonish to activate platform mode.",
		RestartRequired: true,
	})
}

// prepareSQLiteSetupMasterKey follows the platform store's key precedence and
// hex format. Existing keys are never rotated here: an unreadable or invalid key
// requires operator repair, not replacement (it may protect existing secrets).
func prepareSQLiteSetupMasterKey() error {
	return prepareSQLiteSetupMasterKeyWithFileOps(func(f *os.File) sqliteSetupKeyFile { return f })
}

type sqliteSetupKeyFile interface {
	WriteString(string) (int, error)
	Sync() error
	Close() error
}

// wrapFile only adapts operations on a private, owner-only temporary file.
// Publication remains a real atomic no-replace filesystem operation.
func prepareSQLiteSetupMasterKeyWithFileOps(wrapFile func(*os.File) sqliteSetupKeyFile) error {
	validKey := func(value string) bool {
		key, err := hex.DecodeString(value)
		return err == nil && len(key) == 32
	}
	if value := os.Getenv("ASTONISH_MASTER_KEY"); value != "" {
		if !validKey(value) {
			return fmt.Errorf("invalid environment master key")
		}
		return nil
	}
	configDir, err := config.GetConfigDir()
	if err != nil {
		return err
	}
	keyPath := filepath.Join(configDir, ".store_key")
	readKey := func() error {
		value, err := os.ReadFile(keyPath)
		if err != nil {
			return err
		}
		if !validKey(strings.TrimSpace(string(value))) {
			return fmt.Errorf("invalid stored master key")
		}
		return nil
	}
	if err := readKey(); !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	masterKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, masterKey); err != nil {
		return err
	}
	// Keep provisional key material private until write, sync and close succeed.
	// CreateTemp uses exclusive creation and mode 0600 in the destination directory.
	f, err := os.CreateTemp(configDir, ".store_key-*")
	if err != nil {
		return err
	}
	// Best-effort cleanup owns only this temporary name, never the final key.
	defer os.Remove(f.Name())
	fileOps := wrapFile(f)
	encoded := hex.EncodeToString(masterKey)
	n, writeErr := fileOps.WriteString(encoded)
	if writeErr == nil && n != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = fileOps.Sync()
	}
	closeErr := fileOps.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Link publishes the complete inode without replacing any competing key or
	// dangling symlink. Never fall back to rename, which can overwrite a winner.
	if err := os.Link(f.Name(), keyPath); err != nil {
		if os.IsExist(err) {
			return readKey()
		}
		return err
	}
	// The parent directory is not synced: this is atomic publication, not a
	// guarantee that the directory entry survives a machine/power failure.
	return nil
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
	} else if cfg.Storage.IsSQLite() {
		mode = "sqlite"
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

	configured := cfg.Storage.IsPostgres() || cfg.Storage.IsSQLite()
	initialized := false

	if cfg.Storage.IsPostgres() {
		// Try connecting to verify the database is actually accessible.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		entCfg := entstore.Config{
			DSN:            cfg.Storage.Postgres.GetPlatformDSN(),
			InstanceSuffix: cfg.Storage.Postgres.InstanceSuffix,
		}
		_, entS, pgErr := entstore.NewPlatformServices(ctx, entCfg)
		if pgErr == nil {
			initialized = true
			entS.Close()
		}
	} else if cfg.Storage.IsSQLite() {
		// Check that the platform.db exists and is accessible.
		dataDir := cfg.Storage.SQLite.GetDataDir()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		entCfg := entstore.Config{
			DSN:     "file:" + filepath.Join(dataDir, "platform.db"),
			DataDir: dataDir,
		}
		s, sqlErr := entstore.New(ctx, entCfg)
		if sqlErr == nil {
			initialized = true
			s.Close()
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"configured":  configured,
		"initialized": initialized,
	})
}
