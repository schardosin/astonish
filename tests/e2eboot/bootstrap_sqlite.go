//go:build e2e

package e2eboot

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/launcher"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
)

// bootstrapSQLite sets up a full platform instance backed by SQLite.
// It uses t.TempDir() for the data directory — perfect per-test isolation
// with automatic cleanup.
//
// Unlike the PG path, this does NOT require ASTONISH_TEST_DSN. It still
// requires a provider API key and K8s access for sandbox-dependent tests.
func bootstrapSQLite(t *testing.T) *Harness {
	t.Helper()

	apiKey := resolveAPIKey()
	if apiKey == "" {
		t.Skip("No provider API key found (BIFROST_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, ANTHROPIC_API_KEY) — skipping E2E test")
	}

	// Shared mode not supported for SQLite — always run isolated.
	if os.Getenv("ASTONISH_E2E_KEEP_ALIVE") == "1" {
		t.Log("[e2eboot] ASTONISH_E2E_KEEP_ALIVE ignored in SQLite mode (always isolated)")
	}

	// Clean slate: remove any pods left behind by a previously crashed run.
	SweepAllPods(t)
	// Post-test: ensure all sandbox pods are cleaned up when the test ends.
	t.Cleanup(func() { SweepAllPods(t) })

	ctx := context.Background()
	dataDir := t.TempDir() // auto-cleaned when test ends

	// SQLite auto-creates platform.db and runs migrations inside New().
	t.Logf("[e2eboot] Bootstrapping platform (SQLite, dataDir=%s)...", dataDir)
	svc, esStore, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:     "file:" + filepath.Join(dataDir, "platform.db"),
		DataDir: dataDir,
	})
	if err != nil {
		t.Fatalf("[e2eboot] NewPlatformServices (SQLite): %v", err)
	}
	t.Cleanup(func() { esStore.Close() })

	// Initialize local embedding model for hybrid vector+keyword memory search.
	// Must be called before XDG_CONFIG_HOME is overridden (resolves models dir
	// from the real user home) and before the server starts serving requests.
	initEmbedFunc(t, esStore)

	// Write config.yaml with backend: sqlite
	configDir := t.TempDir()
	astonishDir := filepath.Join(configDir, "astonish")
	if err := os.MkdirAll(astonishDir, 0755); err != nil {
		t.Fatalf("[e2eboot] mkdir config dir: %v", err)
	}
	writeSQLiteConfig(t, astonishDir, dataDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Master key for encryption path coverage (same as PG mode).
	if os.Getenv("ASTONISH_MASTER_KEY") == "" {
		t.Setenv("ASTONISH_MASTER_KEY", e2eFixedMasterKeyHex)
	}

	// Seed provider in platform settings.
	seedProviderGeneric(t, ctx, esStore.PlatformSettings(), apiKey)

	// Set global platform backend + secrets (used by admin handlers that
	// call getPlatformBackend()/getPlatformSecrets()). In PG mode this is
	// done inside RegisterRoutes via SetPlatformBackend, but when pg==nil
	// we must do it explicitly.
	api.SetPlatformBackend(esStore)
	api.SetPlatformSecrets(esStore.Secrets())

	// Auth (backend-agnostic — esStore implements store.PlatformBackend).
	authCfg := config.PlatformAuthConfig{JWTSecret: defaultJWTSecret}
	storageCfg := config.StorageConfig{
		Backend: "sqlite",
		SQLite:  config.SQLiteConfig{DataDir: dataDir},
		Auth:    authCfg,
	}
	platformAuth := api.NewPlatformAuth(authCfg, esStore, storageCfg)

	// Start server with SQLite options.
	studio, err := launcher.NewStudioServer(0,
		launcher.WithServices(svc),
		launcher.WithPlatformAuth(platformAuth, nil),
		launcher.WithBackend(esStore),
		launcher.WithTenantMiddleware(entstore.TenantMiddleware(esStore)),
	)
	if err != nil {
		t.Fatalf("[e2eboot] NewStudioServer (SQLite): %v", err)
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

	// Give the server a moment to start.
	time.Sleep(500 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", studio.Port())
	t.Logf("[e2eboot] Server running at %s (SQLite backend)", baseURL)

	// Register + login
	registerUser(t, baseURL)
	token := loginUser(t, baseURL)
	t.Logf("[e2eboot] Authenticated (token: %s...)", token[:20])

	return &Harness{
		BaseURL: baseURL,
		Token:   token,
		Store:   esStore,
		DataDir: dataDir,
	}
}

// writeSQLiteConfig writes a config.yaml for SQLite backend with the same
// sandbox/provider config as PG mode.
func writeSQLiteConfig(t *testing.T, dir, dataDir string) {
	t.Helper()

	bifrostURL := os.Getenv("BIFROST_BASE_URL")
	if bifrostURL == "" {
		bifrostURL = "https://bifrost.local.muxpie.com"
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
		kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		if _, err := os.Stat(kubeconfigPath); err != nil {
			kubeconfigPath = "/root/.kube/config"
		}
	}

	configYAML := fmt.Sprintf(`general:
  default_provider: Bifrost
  default_model: sapaicore/anthropic--claude-4.6-opus
providers:
  Bifrost:
    base_url: %s
    type: openai_compat
storage:
  backend: sqlite
  sqlite:
    data_dir: %s
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
`, bifrostURL, dataDir, defaultJWTSecret, kubeconfigPath, sandboxNS, controlPlaneNS)

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatalf("[e2eboot] write config.yaml (SQLite): %v", err)
	}
	t.Logf("[e2eboot] Wrote SQLite test config to %s", path)
}

// seedProviderGeneric seeds the Bifrost provider in platform settings using the
// store.PlatformSettingsStore interface. Works for both PG and SQLite backends.
func seedProviderGeneric(t *testing.T, ctx context.Context, settingsStore store.PlatformSettingsStore, apiKey string) {
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

	if err := settingsStore.Save(ctx, settings); err != nil {
		t.Fatalf("[e2eboot] seed platform provider: %v", err)
	}
	t.Log("[e2eboot] Seeded Bifrost provider in platform settings")
}
