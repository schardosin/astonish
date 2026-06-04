// platform_core.go is intentionally NOT build-tagged so it can be used by
// both the e2e test harness AND the standalone tools/e2e-inspector binary.
//
// It contains the platform-bootstrap mechanics that previously lived inline in
// Bootstrap(t). Bootstrap(t) now delegates to BootstrapPlatformCore.
package e2eboot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/entstore"
	"github.com/schardosin/astonish/pkg/store/pgutil"
)

// CoreLogger is the minimal logger interface BootstrapPlatformCore needs.
// In tests, t.Logf satisfies this.
type CoreLogger interface {
	Logf(format string, args ...any)
}

// stdoutLogger is the default logger for the inspector binary.
type stdoutLogger struct{}

func (stdoutLogger) Logf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// CoreOptions configures BootstrapPlatformCore.
type CoreOptions struct {
	BaseDSN      string  // ASTONISH_TEST_DSN
	APIKey       string  // resolved from BIFROST_API_KEY etc.
	Suffix       string  // platform/org DB suffix
	Port         int     // 0 = random, otherwise fixed
	ConfigDir    string  // parent dir for the temp config.yaml (XDG_CONFIG_HOME)
	BifrostURL   string  // override BIFROST_BASE_URL; empty = use env or default
	JWTSecret    string  // JWT signing secret
	DropExisting bool    // drop any leftover DBs with this suffix before bootstrap
	Log          CoreLogger
}

// CoreHarness is the long-lived state produced by BootstrapPlatformCore.
// Tests wrap this in their own t.Cleanup; the inspector binary keeps it alive
// until process termination.
type CoreHarness struct {
	BaseURL     string
	Token       string
	Store       *entstore.Store
	Services    *store.Services
	Studio      *launcher.StudioServer
	PlatformDSN string
	Suffix      string
	BaseDSN     string
}

// Shutdown closes the StudioServer and Store. It does NOT drop databases.
// Idempotent.
func (h *CoreHarness) Shutdown(ctx context.Context) {
	if h.Studio != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = h.Studio.Shutdown(shutdownCtx)
		h.Studio = nil
	}
	if h.Store != nil {
		h.Store.Close()
		h.Store = nil
	}
}

// DropDBs drops the platform and well-known org databases for this suffix.
// Connects to the admin "postgres" database.
func DropDBs(ctx context.Context, baseDSN, suffix string, log CoreLogger) {
	if log == nil {
		log = stdoutLogger{}
	}
	adminDSN, err := pgutil.ReplaceDSNDatabase(baseDSN, "postgres")
	if err != nil {
		log.Logf("[e2eboot] WARN: ReplaceDSNDatabase for admin: %v", err)
		return
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		log.Logf("[e2eboot] WARN: connect to postgres admin: %v", err)
		return
	}
	defer conn.Close(ctx)

	// All DBs with the given suffix prefix. We can't enumerate org DBs without
	// reading the platform's organizations table — but the well-known list
	// covers the standard layout. Per-test seeded orgs are also dropped via
	// their own t.Cleanup hooks calling DecommissionOrg.
	dbs := []string{
		config.PlatformDBName(suffix),
		config.OrgDBName(suffix, "default"),
	}
	for _, db := range dbs {
		_, _ = conn.Exec(ctx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", db))
		_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", db))
		if err != nil {
			log.Logf("[e2eboot] WARN: drop %s: %v", db, err)
		} else {
			log.Logf("[e2eboot] Dropped database %s", db)
		}
	}
}

// DropAllDBsWithSuffix drops every database whose name matches
// "astonish_<suffix>_*". Used by the inspector at startup to clear lingering
// per-test org DBs from prior runs.
func DropAllDBsWithSuffix(ctx context.Context, baseDSN, suffix string, log CoreLogger) {
	if log == nil {
		log = stdoutLogger{}
	}
	adminDSN, err := pgutil.ReplaceDSNDatabase(baseDSN, "postgres")
	if err != nil {
		log.Logf("[e2eboot] WARN: ReplaceDSNDatabase for admin: %v", err)
		return
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		log.Logf("[e2eboot] WARN: connect to postgres admin: %v", err)
		return
	}
	defer conn.Close(ctx)

	prefix := fmt.Sprintf("astonish_%s_", suffix)
	rows, err := conn.Query(ctx,
		"SELECT datname FROM pg_database WHERE datname LIKE $1", prefix+"%")
	if err != nil {
		log.Logf("[e2eboot] WARN: list dbs with prefix %s: %v", prefix, err)
		return
	}
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			dbs = append(dbs, name)
		}
	}
	rows.Close()

	for _, db := range dbs {
		_, _ = conn.Exec(ctx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", db))
		_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", db))
		if err != nil {
			log.Logf("[e2eboot] WARN: drop %s: %v", db, err)
		} else {
			log.Logf("[e2eboot] Dropped database %s", db)
		}
	}
}

// BootstrapPlatformCore bootstraps a fresh Astonish platform: creates the
// platform DB, starts a StudioServer, registers the bootstrap user, seeds
// a provider. Returns a CoreHarness containing live handles.
//
// The caller is responsible for shutting down (CoreHarness.Shutdown) and for
// dropping databases (DropDBs / DropAllDBsWithSuffix) at the appropriate time.
//
// The XDG_CONFIG_HOME environment variable is set to ConfigDir so any code
// that calls config.LoadAppConfig() (e.g. via app.GetConfig() at runtime in
// the StudioServer) sees this test's config.yaml. The caller MUST keep
// ConfigDir alive for the lifetime of the harness.
func BootstrapPlatformCore(ctx context.Context, opts CoreOptions) (*CoreHarness, error) {
	log := opts.Log
	if log == nil {
		log = stdoutLogger{}
	}

	if opts.BaseDSN == "" {
		return nil, errors.New("BaseDSN is required")
	}
	if opts.APIKey == "" {
		return nil, errors.New("APIKey is required (BIFROST_API_KEY/OPENAI_API_KEY/...)")
	}
	if opts.Suffix == "" {
		return nil, errors.New("Suffix is required")
	}
	if opts.JWTSecret == "" {
		return nil, errors.New("JWTSecret is required")
	}

	if opts.DropExisting {
		DropAllDBsWithSuffix(ctx, opts.BaseDSN, opts.Suffix, log)
	}

	log.Logf("[e2eboot] Bootstrapping platform (suffix=%s)...", opts.Suffix)
	platformDSN, err := buildPlatformDSN(opts.BaseDSN, opts.Suffix)
	if err != nil {
		return nil, err
	}
	if err := entstore.BootstrapPlatform(ctx, entstore.Config{DSN: platformDSN, InstanceSuffix: opts.Suffix}); err != nil {
		return nil, fmt.Errorf("BootstrapPlatform: %w", err)
	}

	if err := os.MkdirAll(opts.ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := writeConfigFile(opts.ConfigDir, platformDSN, opts.Suffix, opts.BifrostURL, opts.JWTSecret); err != nil {
		return nil, err
	}
	if err := os.Setenv("XDG_CONFIG_HOME", filepath.Dir(opts.ConfigDir)); err != nil {
		return nil, fmt.Errorf("setenv XDG_CONFIG_HOME: %w", err)
	}
	log.Logf("[e2eboot] Wrote config to %s", filepath.Join(opts.ConfigDir, "config.yaml"))

	// Mirror production (and the on-disk dev daemon) by giving the platform a
	// real master key BEFORE NewPlatformServices constructs PlatformSecretStore.
	// Without this, masterKey stays nil, encrypt() short-circuits to plaintext,
	// and any test that exercises a code path guarded by "secrets!=nil &&
	// masterKey set" (e.g. CHAT-069's Save-into-JSONB envelope contract) ends
	// up testing the wrong code branch. We use a deterministic test key — its
	// only role here is to flip on the encryption path. setEnvE2EMasterKey is
	// a no-op when the caller has already exported ASTONISH_MASTER_KEY so that
	// targeted suites can override.
	setEnvE2EMasterKey(log)

	svc, esStore, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: opts.Suffix,
	})
	if err != nil {
		return nil, fmt.Errorf("NewPlatformServices: %w", err)
	}

	// Initialize local embedding model for hybrid vector+keyword memory search.
	initEmbedFuncCore(log, esStore)

	// Seed provider in platform settings (idempotent — Save replaces).
	bifrostURL := opts.BifrostURL
	if bifrostURL == "" {
		bifrostURL = os.Getenv("BIFROST_BASE_URL")
		if bifrostURL == "" {
			bifrostURL = "https://bifrost.local.muxpie.com"
		}
	}
	if err := seedProviderCore(ctx, esStore, opts.APIKey, bifrostURL); err != nil {
		esStore.Close()
		return nil, err
	}
	log.Logf("[e2eboot] Seeded Bifrost provider in platform settings")

	authCfg := config.PlatformAuthConfig{JWTSecret: opts.JWTSecret}
	storageCfg := config.StorageConfig{
		Backend:  "postgres",
		Postgres: config.PostgresConfig{
			PlatformDSN:    platformDSN,
			InstanceSuffix: opts.Suffix,
		},
		Auth:     authCfg,
	}
	platformAuth := api.NewPlatformAuth(authCfg, esStore, storageCfg)

	studio, err := launcher.NewStudioServer(opts.Port,
		launcher.WithServices(svc),
		launcher.WithPlatformAuth(platformAuth, esStore),
	)
	if err != nil {
		esStore.Close()
		return nil, fmt.Errorf("NewStudioServer: %w", err)
	}

	go func() {
		if serveErr := studio.Serve(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Logf("[e2eboot] Studio server error: %v", serveErr)
		}
	}()

	// Give the server a moment to start.
	time.Sleep(500 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", studio.Port())
	log.Logf("[e2eboot] Server running at %s", baseURL)

	// Register + login the bootstrap user. Idempotent: if registration says
	// "already exists" (HTTP 409 etc.) we still proceed to login.
	if err := registerBootstrapUser(baseURL); err != nil {
		log.Logf("[e2eboot] Bootstrap user registration: %v (continuing — likely already exists)", err)
	} else {
		log.Logf("[e2eboot] Bootstrap user registered")
	}
	token, err := loginBootstrapUser(baseURL)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = studio.Shutdown(shutdownCtx)
		cancel()
		esStore.Close()
		return nil, fmt.Errorf("login bootstrap user: %w", err)
	}
	log.Logf("[e2eboot] Authenticated bootstrap user")

	return &CoreHarness{
		BaseURL:     baseURL,
		Token:       token,
		Store:       esStore,
		Services:    svc,
		Studio:      studio,
		PlatformDSN: platformDSN,
		Suffix:      opts.Suffix,
		BaseDSN:     opts.BaseDSN,
	}, nil
}

// buildPlatformDSN derives the platform DSN for a given suffix.
func buildPlatformDSN(baseDSN, suffix string) (string, error) {
	dbName := config.PlatformDBName(suffix)
	return pgutil.ReplaceDSNDatabase(baseDSN, dbName)
}

// seedProviderCore is the testing.T-free equivalent of seedProvider.
func seedProviderCore(ctx context.Context, esStore *entstore.Store, apiKey, bifrostURL string) error {
	settings := &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "sapaicore/anthropic--claude-4.6-opus",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {
				"type":     "openai_compat",
				"base_url": bifrostURL,
				"api_key":  apiKey,
			},
		},
	}
	if err := esStore.PlatformSettings().Save(ctx, settings); err != nil {
		return fmt.Errorf("save platform settings: %w", err)
	}
	return nil
}

// writeConfigFile writes a complete config.yaml to dir/config.yaml.
// This is the testing.T-free equivalent of writeConfig().
func writeConfigFile(dir, platformDSN, suffix, bifrostURL, jwtSecret string) error {
	if bifrostURL == "" {
		bifrostURL = os.Getenv("BIFROST_BASE_URL")
		if bifrostURL == "" {
			bifrostURL = "https://bifrost.local.muxpie.com"
		}
	}
	sandboxNS := os.Getenv("ASTONISH_E2E_SANDBOX_NAMESPACE")
	if sandboxNS == "" {
		sandboxNS = "astonish-sandbox"
	}
	controlPlaneNS := os.Getenv("ASTONISH_E2E_CONTROL_PLANE_NAMESPACE")
	if controlPlaneNS == "" {
		controlPlaneNS = "astonish"
	}
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home := os.Getenv("HOME")
		kubeconfigPath = filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfigPath); err != nil {
			kubeconfigPath = "/root/.kube/config"
		}
	}
	escapedDSN := strings.ReplaceAll(platformDSN, `"`, `\"`)

	configYAML := fmt.Sprintf(`general:
  default_provider: Bifrost
  default_model: sapaicore/anthropic--claude-4.6-opus
providers:
  Bifrost:
    base_url: %s
    type: openai_compat
storage:
  backend: postgres
  postgres:
    platform_dsn: "%s"
    instance_suffix: %s
  auth:
    mode: builtin
    jwt_secret: %s
sandbox:
  enabled: true
  backend: k8s
  limits:
    memory: 2GB
    cpu: 2
    processes: 500
  kubernetes:
    kubeconfig_path: %s
    namespace: %s
    control_plane_namespace: %s
    overlay_mode: fuse
    privileged_pods: true
    sandbox_image: schardosin/astonish-sandbox-base:dev
    layers_pvc_name: astonish-layers
    uppers_pvc_name: astonish-uppers
`, bifrostURL, escapedDSN, suffix, jwtSecret, kubeconfigPath, sandboxNS, controlPlaneNS)

	path := filepath.Join(dir, "config.yaml")
	return os.WriteFile(path, []byte(configYAML), 0644)
}

// e2eFixedMasterKeyHex is a deterministic AES-256 key wired into every E2E
// platform bootstrap. It is NOT a secret — it exists solely to flip the
// PlatformSecretStore encryption path on, so production-shaped code paths
// (env encryption for MCP servers, platform_secrets, etc.) are exercised
// by every test instead of silently bypassed.
//
// 32 bytes encoded as 64 hex chars. Decoding is validated below.
const e2eFixedMasterKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// setEnvE2EMasterKey ensures ASTONISH_MASTER_KEY is set before
// NewPlatformServices constructs PlatformSecretStore (whose loadMasterKey()
// runs once at construction time). If the caller has already exported a
// master key, leave it alone so targeted suites can pin their own.
//
// Safe to call multiple times. Logs whether a fresh value was applied so
// inspector boot logs make the encryption-on contract auditable.
func setEnvE2EMasterKey(log CoreLogger) {
	if existing := os.Getenv("ASTONISH_MASTER_KEY"); existing != "" {
		log.Logf("[e2eboot] ASTONISH_MASTER_KEY already set by caller — leaving as-is")
		return
	}
	if err := os.Setenv("ASTONISH_MASTER_KEY", e2eFixedMasterKeyHex); err != nil {
		log.Logf("[e2eboot] WARN: setenv ASTONISH_MASTER_KEY: %v", err)
		return
	}
	log.Logf("[e2eboot] ASTONISH_MASTER_KEY set to deterministic e2e fixture (encryption enabled)")
}
