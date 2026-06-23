//go:build e2e

// Package e2eboot provides a shared test harness for all E2E tests.
// It bootstraps a full Astonish platform (fresh DB, real StudioServer,
// real auth, real provider) and returns a Harness that tests use to
// interact with the platform via HTTP.
//
// Every E2E test uses the same bootstrap shape. There is no "lite" mode.
package e2eboot

import (
	"context"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/entstore"
	"github.com/schardosin/astonish/pkg/store/pgutil"
)

const (
	defaultEmail     = "e2e@test.local"
	defaultPassword  = "E2ETest2024!"
	defaultJWTSecret = "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"
)

// Harness holds a fully-bootstrapped E2E platform and provides
// helpers for making authenticated HTTP requests against it.
type Harness struct {
	BaseURL     string
	Token       string
	Store       *entstore.Store          // unified store (PG or SQLite)
	DataDir     string                   // SQLite data directory (empty in PG mode)
	PlatformDSN string                   // PG platform DSN (empty in SQLite mode)
	Suffix      string
	BaseDSN     string

	// PerTestSuffix is non-empty when running in shared-instance mode
	// (ASTONISH_E2E_KEEP_ALIVE=1). Tests that seed orgs/teams/users in
	// shared mode MUST append this to slugs and email local-parts so
	// 26 tests can coexist in a single platform DB.
	//
	// In isolated mode (default) this is empty and existing tests work
	// unchanged.
	PerTestSuffix string

	// SharedMode indicates this Harness is attached to a long-lived
	// inspector StudioServer rather than owning its own server.
	SharedMode bool
}

// PlatformBackend returns the store.PlatformBackend regardless of whether
// the harness is backed by PG or SQLite. Use this in code that needs to
// work with both backends (e.g., seed.go).
func (h *Harness) PlatformBackend() store.PlatformBackend {
	return h.Store
}

// IsSQLite returns true if this harness is using the SQLite backend.
func (h *Harness) IsSQLite() bool {
	return h.Store != nil && h.Store.Dialect() == entstore.DialectSQLite
}

// Bootstrap sets up a full platform instance for a single E2E test.
// It creates a fresh Postgres database (suffix derived from test name),
// starts a real StudioServer, seeds the provider, registers a user, and
// logs in. All resources are cleaned up via t.Cleanup.
//
// Prerequisites (env vars):
//   - ASTONISH_TEST_DSN: admin Postgres connection string
//   - BIFROST_API_KEY (or OPENAI_API_KEY, GOOGLE_API_KEY, ANTHROPIC_API_KEY)
//
// If ASTONISH_TEST_DSN is unset, the test is skipped.
func Bootstrap(t *testing.T) *Harness {
	t.Helper()

	// SQLite backend: entirely different bootstrap path — no PG required.
	if os.Getenv("ASTONISH_E2E_BACKEND") == "sqlite" {
		return bootstrapSQLite(t)
	}

	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set — skipping E2E test")
	}

	apiKey := resolveAPIKey()
	if apiKey == "" {
		t.Skip("No provider API key found (BIFROST_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, ANTHROPIC_API_KEY) — skipping E2E test")
	}

	// Shared-instance mode: attach to a long-lived inspector instance. This
	// is the path taken by `make test-e2e-inspect`. All 26 tests share one
	// StudioServer + one platform DB; per-test isolation is achieved via
	// PerTestSuffix on slugs/emails.
	if os.Getenv("ASTONISH_E2E_KEEP_ALIVE") == "1" {
		return bootstrapShared(t, dsn)
	}

	// Clean slate: remove any sandboxes left behind by a previously crashed run.
	GetSandboxHelper().SweepAll(t)
	// Post-test: ensure all sandboxes are cleaned up when the test ends.
	t.Cleanup(func() { GetSandboxHelper().SweepAll(t) })

	ctx := context.Background()
	suffix := deriveSuffix(t.Name())

	// Drop any leftover databases from a previous failed run (ALL dbs with this suffix,
	// including leaked org dbs like _acme/_globex from prior crashes).
	DropAllDBsWithSuffix(ctx, dsn, suffix, t)

	// Bootstrap fresh platform
	t.Logf("[e2eboot] Bootstrapping platform (suffix=%s)...", suffix)
	platformDSN := buildDSN(t, dsn, suffix)
	if err := entstore.BootstrapPlatform(ctx, entstore.Config{DSN: platformDSN, InstanceSuffix: suffix}, nil); err != nil {
		t.Fatalf("[e2eboot] BootstrapPlatform: %v", err)
	}
	t.Cleanup(func() { DropAllDBsWithSuffix(ctx, dsn, suffix, t) })

	// Write temp config.yaml
	configDir := t.TempDir()
	astonishDir := filepath.Join(configDir, "astonish")
	if err := os.MkdirAll(astonishDir, 0755); err != nil {
		t.Fatalf("[e2eboot] mkdir config dir: %v", err)
	}

	writeConfig(t, astonishDir, platformDSN, suffix)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Mirror production by giving the platform a real master key BEFORE
	// NewPlatformServices constructs PlatformSecretStore. Without this,
	// masterKey stays nil, encrypt() short-circuits to plaintext, and any
	// test exercising a code path guarded by "secrets!=nil && masterKey set"
	// (e.g. CHAT-069's Save-into-JSONB envelope contract) tests the wrong
	// branch. We use t.Setenv so the value is auto-restored at test end —
	// keeps test isolation clean across the suite.
	if os.Getenv("ASTONISH_MASTER_KEY") == "" {
		t.Setenv("ASTONISH_MASTER_KEY", e2eFixedMasterKeyHex)
	}

	// Connect entstore
	svc, esStore, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: suffix,
		MaxOpenConns:   10,
		MaxIdleConns:   5,
	})
	if err != nil {
		t.Fatalf("[e2eboot] NewPlatformServices: %v", err)
	}
	t.Cleanup(func() { esStore.Close() })

	// Initialize local embedding model for hybrid vector+keyword memory search.
	initEmbedFunc(t, esStore)

	// Seed provider in platform settings
	seedProvider(t, ctx, esStore, apiKey)

	// Create PlatformAuth
	authCfg := config.PlatformAuthConfig{
		JWTSecret: defaultJWTSecret,
	}
	storageCfg := config.StorageConfig{
		Backend:  "postgres",
		Postgres: config.PostgresConfig{
			PlatformDSN:    platformDSN,
			InstanceSuffix: suffix,
		},
		Auth:     authCfg,
	}
	platformAuth := api.NewPlatformAuth(authCfg, esStore, storageCfg)

	// Start server
	studio, err := launcher.NewStudioServer(0,
		launcher.WithServices(svc),
		launcher.WithPlatformAuth(platformAuth, esStore),
		launcher.WithTenantMiddleware(entstore.TenantMiddleware(esStore)),
	)
	if err != nil {
		t.Fatalf("[e2eboot] NewStudioServer: %v", err)
	}

	go func() {
		if serveErr := studio.Serve(); serveErr != nil && serveErr != http.ErrServerClosed {
			t.Logf("[e2eboot] Studio server error: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = studio.Shutdown(shutdownCtx)
	})

	// Give the server a moment to start
	time.Sleep(500 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", studio.Port())
	t.Logf("[e2eboot] Server running at %s", baseURL)

	// Register + login
	registerUser(t, baseURL)
	token := loginUser(t, baseURL)
	t.Logf("[e2eboot] Authenticated (token: %s...)", token[:20])

	return &Harness{
		BaseURL:     baseURL,
		Token:       token,
		Store:       esStore,
		PlatformDSN: platformDSN,
		Suffix:      suffix,
		BaseDSN:     dsn,
	}
}

// deriveSuffix produces a short, deterministic DB suffix from a test name.
// Format: "e2e" + crc32 hex (8 chars) → total ~11 chars.
func deriveSuffix(testName string) string {
	h := crc32.ChecksumIEEE([]byte(testName))
	return fmt.Sprintf("e2e%08x", h)
}

func resolveAPIKey() string {
	for _, env := range []string{
		"BIFROST_API_KEY",
		"OPENAI_API_KEY",
		"GOOGLE_API_KEY",
		"ANTHROPIC_API_KEY",
	} {
		if key := os.Getenv(env); key != "" {
			return key
		}
	}
	return ""
}

func buildDSN(t *testing.T, baseDSN, suffix string) string {
	t.Helper()
	dbName := config.PlatformDBName(suffix)
	result, err := pgutil.ReplaceDSNDatabase(baseDSN, dbName)
	if err != nil {
		t.Fatalf("[e2eboot] ReplaceDSNDatabase: %v", err)
	}
	return result
}

func dropDBs(t *testing.T, ctx context.Context, baseDSN, suffix string) {
	t.Helper()
	adminDSN, err := pgutil.ReplaceDSNDatabase(baseDSN, "postgres")
	if err != nil {
		t.Logf("[e2eboot] WARN: ReplaceDSNDatabase for admin: %v", err)
		return
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Logf("[e2eboot] WARN: connect to postgres admin: %v", err)
		return
	}
	defer conn.Close(ctx)

	dbs := []string{
		config.PlatformDBName(suffix),
		config.OrgDBName(suffix, "default"),
	}
	for _, db := range dbs {
		_, _ = conn.Exec(ctx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", db))
		_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", db))
		if err != nil {
			t.Logf("[e2eboot] WARN: drop %s: %v", db, err)
		} else {
			t.Logf("[e2eboot] Dropped database %s", db)
		}
	}
}

func seedProvider(t *testing.T, ctx context.Context, esStore *entstore.Store, apiKey string) {
	t.Helper()

	bifrostURL := os.Getenv("BIFROST_BASE_URL")
	if bifrostURL == "" {
		bifrostURL = "https://bifrost.local.muxpie.com"
	}

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
		t.Fatalf("[e2eboot] seed platform provider: %v", err)
	}
	t.Log("[e2eboot] Seeded Bifrost provider in platform settings")
}

func writeConfig(t *testing.T, dir, platformDSN, suffix string) {
	t.Helper()

	bifrostURL := os.Getenv("BIFROST_BASE_URL")
	if bifrostURL == "" {
		bifrostURL = "https://bifrost.local.muxpie.com"
	}

	// Escape any special chars in DSN for YAML
	escapedDSN := strings.ReplaceAll(platformDSN, `"`, `\"`)

	var sandboxBlock string
	switch SandboxBackendName() {
	case "openshell":
		gatewayAddr := os.Getenv("ASTONISH_E2E_OPENSHELL_GATEWAY")
		if gatewayAddr == "" {
			gatewayAddr = "localhost:18080"
		}
		sandboxBlock = fmt.Sprintf(`sandbox:
  enabled: true
  backend: openshell
  limits:
    memory: 2GB
    cpu: 2
    processes: 500
  openshell:
    gateway_addr: %s
    gateway_tls: false
    sandbox_image: schardosin/astonish-sandbox-openshell:dev
    network_policy:
      presets:
        - default
`, gatewayAddr)
	default:
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
			kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
			if _, err := os.Stat(kubeconfigPath); err != nil {
				kubeconfigPath = "/root/.kube/config"
			}
		}
		sandboxBlock = fmt.Sprintf(`sandbox:
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
`, kubeconfigPath, sandboxNS, controlPlaneNS)
	}

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
%s`, bifrostURL, escapedDSN, suffix, defaultJWTSecret, sandboxBlock)

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatalf("[e2eboot] write config.yaml: %v", err)
	}
	t.Logf("[e2eboot] Wrote test config to %s", path)
}
