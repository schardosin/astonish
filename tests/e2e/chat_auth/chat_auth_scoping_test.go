//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers skills, MCP servers, and credentials scoping.
package chat_auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_SecurityUrgentFirstRunSetup exercises the real first-run HTTP routes.
// It deliberately uses only temporary SQLite storage, even in a PG test run:
// PostgreSQL positive setup needs separately approved cluster cleanup scope.
// COVERS: CHAT-109
func TestE2E_SecurityUrgentFirstRunSetup(t *testing.T) {
	for _, mode := range []string{config.AuthModeNone, config.AuthModeBuiltin} {
		t.Run(mode, func(t *testing.T) { urgentFirstRunSetup(t, mode) })
	}
}

func urgentFirstRunSetup(t *testing.T, mode string) {
	h := e2eboot.BootstrapFirstRunSetup(t)
	for _, path := range []string{h.ConfigDir, h.DataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("first-run HTTP acceptance requires absent config/data directory %q: %v", path, err)
		}
	}
	client := &http.Client{Timeout: 90 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	const sqlitePath = "/api/platform/init/sqlite"
	const pgPath = "/api/platform/init"
	payload := api.SQLitePlatformInitRequest{
		DataDir: h.DataDir, OrgName: "First Run Acceptance", OrgSlug: "first-run",
		AuthMode: mode, AdminEmail: "bootstrap@example.test", AdminPassword: "SyntheticPassword123!",
	}
	post := func(t *testing.T, path, token string, body any, want int) api.PlatformInitResponse {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-Astonish-Bootstrap-Token", token)
		}
		// Neither loopback nor proxy-supplied identity is bootstrap authority.
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result api.PlatformInitResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode setup response (HTTP %d): %v", resp.StatusCode, err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: HTTP %d, want %d: %s", path, resp.StatusCode, want, result.Error)
		}
		if want != http.StatusOK && (result.Success || result.RestartRequired || result.Error == "") {
			t.Fatalf("rejection must report an error without success or restart: %+v", result)
		}
		return result
	}
	// Include names, modes, and file contents, so unwanted DBs, keys, config
	// rewrites, or retry-created organizations cannot hide behind a status code.
	snapshot := func(t *testing.T) map[string]string {
		t.Helper()
		root := filepath.Dir(h.ConfigDir)
		files := make(map[string]string)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			value := info.Mode().String()
			if !entry.IsDir() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				value += hex.EncodeToString(sum[:])
			}
			files[path] = value
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	assertStatus := func(t *testing.T, configured, initialized bool) {
		t.Helper()
		resp, err := client.Get(h.BaseURL + "/api/platform/init/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result struct {
			Configured  bool `json:"configured"`
			Initialized bool `json:"initialized"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || result.Configured != configured || result.Initialized != initialized {
			t.Fatalf("unexpected setup status HTTP %d: %+v", resp.StatusCode, result)
		}
	}
	assertStatus(t, false, false)
	for _, tc := range []struct{ name, path, token string }{
		{"sqlite_missing_token_no_effects", sqlitePath, ""},
		{"sqlite_wrong_token_no_effects", sqlitePath, "incorrect-bootstrap-token"},
		{"postgres_missing_token_no_effects", pgPath, ""},
		{"postgres_wrong_token_no_effects", pgPath, "incorrect-bootstrap-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshot(t)
			var body any = payload
			if tc.path == pgPath {
				// Never supply credentials for an operator-owned PG instance.
				body = api.PlatformInitRequest{Host: "127.0.0.1", Port: 1, User: "setup", Password: "test-only", OrgSlug: "first-run"}
			}
			post(t, tc.path, tc.token, body, http.StatusForbidden)
			assertStatus(t, false, false)
			if !reflect.DeepEqual(before, snapshot(t)) {
				t.Fatal("unauthorized setup changed temporary config/data")
			}
		})
	}
	if t.Failed() {
		t.Fatal("authorization/no-effects checks failed before authorized setup")
	}
	if !t.Run("sqlite_authorized_init_persists_local_user_org_team", func(t *testing.T) {
		result := post(t, sqlitePath, h.BootstrapToken, payload, http.StatusOK)
		if !result.Success || !result.RestartRequired || result.Error != "" {
			t.Fatalf("authorized initialization did not succeed: %+v", result)
		}
		assertStatus(t, true, true)
		cfg, err := config.LoadAppConfig()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Storage.IsSQLite() || cfg.Storage.SQLite.DataDir != h.DataDir ||
			cfg.Storage.Auth.Mode != mode ||
			cfg.Storage.Auth.DefaultOrgSlug != payload.OrgSlug {
			t.Fatal("authorized setup did not persist the requested SQLite/auth configuration")
		}
		// Reopen only the test-owned database to prove persisted records, not
		// just an optimistic HTTP success body.
		es, err := entstore.New(context.Background(), entstore.Config{DSN: "file:" + filepath.Join(h.DataDir, "platform.db"), DataDir: h.DataDir})
		if err != nil {
			t.Fatal(err)
		}
		defer es.Close()
		users, err := es.Users().List(context.Background())
		if err != nil || len(users) != 1 {
			t.Fatalf("expected exactly one bootstrap admin, got %d: %v", len(users), err)
		}
		admin := users[0]
		if admin.PlatformRole != "superadmin" || admin.Status != "active" ||
			(mode == config.AuthModeNone && (admin.ID != config.SingleUserID || admin.Email != config.SingleUserEmail)) ||
			(mode == config.AuthModeBuiltin && (admin.Email != payload.AdminEmail || admin.PasswordHash == "" || cfg.Storage.Auth.JWTSecret == "")) {
			t.Fatal("persisted bootstrap administrator does not match authorized request")
		}
		orgs, err := es.Organizations().List(context.Background())
		if err != nil || len(orgs) != 1 || orgs[0].Slug != payload.OrgSlug || orgs[0].Name != payload.OrgName {
			t.Fatalf("expected requested bootstrap organization: %v", err)
		}
		orgRole, err := es.Organizations().GetMemberRole(context.Background(), admin.ID, orgs[0].ID)
		if err != nil || orgRole != "owner" {
			t.Fatalf("expected bootstrap org owner membership, got %q: %v", orgRole, err)
		}
		orgStore, err := es.ForOrg(payload.OrgSlug)
		if err != nil {
			t.Fatal(err)
		}
		teams, err := orgStore.Teams().ListTeams(context.Background())
		if err != nil || len(teams) != 1 || teams[0].Slug != "general" {
			t.Fatalf("expected persisted default team: %v", err)
		}
		teamRole, err := orgStore.Teams().GetMemberRole(context.Background(), admin.ID, teams[0].ID)
		if err != nil || teamRole != "admin" {
			t.Errorf("expected bootstrap General team admin membership, got %q: %v", teamRole, err)
		}
	}) {
		t.Fatal("authorized initialization failed; retry assertions require a configured platform")
	}
	for _, tc := range []struct{ name, path string }{
		{"sqlite_configured_retry_no_effects", sqlitePath},
		{"postgres_configured_retry_no_effects", pgPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshot(t)
			retry := payload
			retry.DataDir = filepath.Join(filepath.Dir(h.DataDir), "retry-data")
			retry.AdminEmail = "replacement@example.test"
			retry.OrgSlug = "replacement"
			post(t, tc.path, h.BootstrapToken, retry, http.StatusConflict)
			assertStatus(t, true, true)
			if !reflect.DeepEqual(before, snapshot(t)) {
				t.Fatal("configured retry modified existing state or created new config/data")
			}
		})
	}
	if mode == config.AuthModeBuiltin {
		t.Run("builtin_restart_login_authorized_settings", func(t *testing.T) {
			if err := h.RestartConfigured(t); err != nil {
				t.Fatal(err)
			}
			// No bootstrap header, token minting, registration, or fixture seeding.
			fresh := &http.Client{Timeout: 30 * time.Second}
			t.Cleanup(fresh.CloseIdleConnections)
			get := func(path, token string, want int) []byte {
				t.Helper()
				req, err := http.NewRequest(http.MethodGet, h.BaseURL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				resp, err := fresh.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil || resp.StatusCode != want {
					t.Fatalf("restarted GET %s: HTTP %d, want %d: %s (%v)", path, resp.StatusCode, want, body, err)
				}
				return body
			}
			get("/api/settings/full", "", http.StatusUnauthorized)
			loginBody, err := json.Marshal(map[string]string{
				"email": payload.AdminEmail, "password": payload.AdminPassword, "client_type": "cli",
			})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := fresh.Post(h.BaseURL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var login struct {
				AccessToken string `json:"access_token"`
				Error       string `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&login); err != nil || resp.StatusCode != http.StatusOK || login.AccessToken == "" || login.Error != "" {
				t.Fatalf("fresh builtin login failed: HTTP %d, error=%q (%v)", resp.StatusCode, login.Error, err)
			}
			var identity struct {
				User struct {
					Email        string `json:"email"`
					Role         string `json:"role"`
					PlatformRole string `json:"platform_role"`
				} `json:"user"`
				Org struct {
					Slug string `json:"slug"`
				} `json:"org"`
				Team string `json:"team"`
			}
			if err := json.Unmarshal(get("/api/auth/me", login.AccessToken, http.StatusOK), &identity); err != nil {
				t.Fatal(err)
			}
			if identity.User.Email != payload.AdminEmail || identity.User.Role != "owner" || identity.User.PlatformRole != "superadmin" || identity.Org.Slug != payload.OrgSlug || identity.Team != "general" {
				t.Fatalf("fresh login did not resolve bootstrap org/team identity: %+v", identity)
			}
			for _, path := range []string{"/api/settings/full", "/api/settings/team/providers"} {
				body := get(path, login.AccessToken, http.StatusOK)
				var settings map[string]any
				if err := json.Unmarshal(body, &settings); err != nil || settings == nil || settings["error"] != nil {
					t.Fatalf("authorized settings did not return a settings object: %s (%v)", body, err)
				}
			}
			get("/api/settings/full", "", http.StatusUnauthorized)
		})
	}
}

// TestE2E_FirstRunSetupProcessContinuity is deliberately independent of the
// infrastructure security suite. Each stage runs this test binary, not a daemon,
// with an allowlisted environment and only temporary SQLite/loopback resources.
func TestE2E_FirstRunSetupProcessContinuity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{config.AuthModeBuiltin, config.AuthModeNone} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for _, stage := range []string{firstRunProcessSetup, firstRunProcessRestart} {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				cmd := exec.CommandContext(ctx, executable, "-test.run=^TestFirstRunSetupProcessHelper$", "-test.v", "-test.timeout=80s")
				cmd.Dir = root
				// Do not inherit operator keys, database DSNs, kubeconfig, proxies,
				// bootstrap capabilities, or dedicated-test approval variables.
				cmd.Env = []string{
					"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
					"TMPDIR=" + root, "SUDO_USER=", "ASTONISH_FIRST_RUN_PROCESS=" + stage,
					"ASTONISH_FIRST_RUN_MODE=" + mode,
				}
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("%s subprocess: %v\n%s", stage, err, output)
				}
				t.Logf("%s subprocess passed:\n%s", stage, output)
			}
		})
	}
}

const (
	firstRunProcessSetup   = "setup"
	firstRunProcessRestart = "restart"
	firstRunProcessSecret  = "synthetic-first-run-continuity-secret"
	firstRunProcessKeyName = "first-run-process-continuity"

	// Non-secret, non-default canaries are written only by the setup process.
	firstRunProcessPrompt   = "First-run persisted settings continuity canary."
	firstRunProcessMaxTools = 17
	firstRunProcessProvider = "first-run-provider-canary"
	firstRunProcessModel    = "first-run-model-canary"
	firstRunProcessBaseURL  = "https://first-run-provider.example.test/v1"
)

type firstRunProcessEvidence struct {
	PID                             int
	UserID, OrgID, TeamID           string
	KeyHash, CipherHash, ConfigHash string
}

// TestFirstRunSetupProcessHelper is inert unless invoked by the parent above.
// Local router wiring avoids Studio's wildcard listener in builtin mode; the
// production route registration, auth, tenant middleware and stores are used.
func TestFirstRunSetupProcessHelper(t *testing.T) {
	stage := os.Getenv("ASTONISH_FIRST_RUN_PROCESS")
	if stage == "" {
		t.Skip("subprocess helper")
	}
	if stage != firstRunProcessSetup && stage != firstRunProcessRestart {
		t.Fatal("invalid subprocess stage")
	}
	mode := os.Getenv("ASTONISH_FIRST_RUN_MODE")
	if mode != config.AuthModeBuiltin && mode != config.AuthModeNone {
		t.Fatal("invalid subprocess auth mode")
	}
	root := os.Getenv("HOME")
	if !filepath.IsAbs(root) || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(root, "config") {
		t.Fatal("helper requires isolated absolute HOME/config paths")
	}
	configDir := filepath.Join(root, "config", "astonish")
	dataDir := filepath.Join(root, "data")
	if mode == config.AuthModeNone {
		// No-auth setup intentionally does not generate .store_key. Exercise
		// encryption with an explicit synthetic environment key in this mode.
		t.Setenv("ASTONISH_MASTER_KEY", strings.Repeat("cd", 32))
	}
	request := func(server *httptest.Server, method, path, token string, body any, want int) []byte {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if stage == firstRunProcessSetup {
			req.Header.Set("X-Astonish-Bootstrap-Token", token)
		} else if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := server.Client()
		client.Timeout = 30 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: HTTP %d, want %d (%v)", method, path, resp.StatusCode, want, err)
		}
		return result
	}
	if stage == firstRunProcessSetup {
		for _, path := range []string{configDir, dataDir} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("setup requires absent config/data paths: %v", err)
			}
		}
		t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "synthetic-process-bootstrap-token")
		router := mux.NewRouter()
		api.RegisterRoutes(router, nil, nil, nil)
		server := httptest.NewServer(router)
		t.Cleanup(server.Close)
		payload := api.SQLitePlatformInitRequest{
			DataDir: dataDir, AuthMode: mode, OrgName: "Process Restart", OrgSlug: "process-restart",
			AdminEmail: "restart@example.test", AdminPassword: "SyntheticPassword123!",
		}
		var result api.PlatformInitResponse
		body := request(server, http.MethodPost, "/api/platform/init/sqlite", "synthetic-process-bootstrap-token", payload, http.StatusOK)
		server.Close()
		if err := json.Unmarshal(body, &result); err != nil || !result.Success || !result.RestartRequired || result.Error != "" {
			t.Fatalf("setup did not persist a restartable workspace: %v", err)
		}
	}
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil || !cfg.Storage.IsSQLite() || cfg.Storage.SQLite.DataDir != dataDir || cfg.Storage.Auth.Mode != mode {
		t.Fatalf("helper must reopen only persisted temporary SQLite configuration: %v", err)
	}
	ctx := context.Background()
	svc, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: "file:" + filepath.Join(dataDir, "platform.db"), DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	if stage == firstRunProcessSetup {
		if err := es.Secrets().SetSecret(firstRunProcessKeyName, firstRunProcessSecret); err != nil {
			t.Fatal(err)
		}
	}
	keyText := []byte(os.Getenv("ASTONISH_MASTER_KEY"))
	if mode == config.AuthModeBuiltin {
		keyText, err = os.ReadFile(filepath.Join(configDir, ".store_key"))
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(configDir, ".store_key"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("persisted key must remain owner-only: %v", err)
		}
	} else if _, err := os.Stat(filepath.Join(configDir, ".store_key")); !os.IsNotExist(err) {
		t.Fatalf("no-auth environment-key fixture must not create .store_key: %v", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(keyText)))
	if err != nil || len(key) != 32 {
		t.Fatal("expected a valid synthetic AES-256 key")
	}
	row, err := es.PlatformClient().PlatformSecret.Get(ctx, firstRunProcessKeyName)
	if err != nil {
		t.Fatal(err)
	}
	// Direct authenticated decryption rules out plaintext compatibility fallback.
	plain, err := credentials.Decrypt(row.Value, key)
	if err != nil || string(plain) != firstRunProcessSecret || bytes.Contains(row.Value, []byte(firstRunProcessSecret)) {
		t.Fatalf("persisted secret is not encrypted with the retained key: %v", err)
	}
	if _, err := credentials.Decrypt(row.Value, bytes.Repeat([]byte{0x42}, 32)); err == nil {
		t.Fatal("wrong synthetic key must not decrypt the persisted ciphertext")
	}
	if es.Secrets().GetSecret(firstRunProcessKeyName) != firstRunProcessSecret {
		t.Fatal("reopened production secret store cannot decrypt the synthetic secret")
	}
	users, err := es.Users().List(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("expected only the setup-created user: %v", err)
	}
	orgs, err := es.Organizations().List(ctx)
	if err != nil || len(orgs) != 1 || orgs[0].Slug != "process-restart" {
		t.Fatalf("expected only the setup-created organization: %v", err)
	}
	org, err := es.ForOrg("process-restart")
	if err != nil {
		t.Fatal(err)
	}
	teams, err := org.Teams().ListTeams(ctx)
	if err != nil || len(teams) != 1 || teams[0].Slug != "general" {
		t.Fatalf("expected only the setup-created team: %v", err)
	}
	if stage == firstRunProcessSetup {
		// Persist through the production stores before the setup process exits.
		// Restart must only read these values; no provider is contacted.
		cfg.Chat.SystemPrompt = firstRunProcessPrompt
		cfg.Chat.MaxTools = firstRunProcessMaxTools
		if err := config.SaveAppConfig(cfg); err != nil {
			t.Fatal(err)
		}
		settingsStore := org.ForTeam(teams[0].Slug).Settings()
		settings, err := settingsStore.Get(ctx)
		if err != nil || settings == nil {
			t.Fatalf("load setup team settings: %v", err)
		}
		settings.DefaultProvider = firstRunProcessProvider
		settings.DefaultModel = firstRunProcessModel
		settings.Providers = map[string]store.ProviderConfig{
			firstRunProcessProvider: {"type": "openai_compat", "base_url": firstRunProcessBaseURL},
		}
		if err := settingsStore.Save(ctx, settings); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	configBytes, err := os.ReadFile(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	evidence := firstRunProcessEvidence{PID: os.Getpid(), UserID: users[0].ID, OrgID: orgs[0].ID, TeamID: teams[0].ID,
		KeyHash: hash(keyText), CipherHash: hash(row.Value), ConfigHash: hash(configBytes)}
	evidencePath := filepath.Join(root, "process-evidence.json")
	if stage == firstRunProcessSetup {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(evidencePath, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var previous firstRunProcessEvidence
	if err := json.Unmarshal(data, &previous); err != nil {
		t.Fatal(err)
	}
	if previous.PID == evidence.PID || previous.PID == 0 {
		t.Fatal("restart must run in a different process after setup exited")
	}
	previous.PID = evidence.PID
	if previous != evidence {
		t.Fatal("restart changed persisted user/org/team identity, key, ciphertext, or configuration")
	}
	auth := api.NewPlatformAuth(cfg.Storage.Auth, es, cfg.Storage)
	router := mux.NewRouter()
	api.RegisterPlatformAuthRoutes(router, auth)
	api.RegisterRoutes(router, svc, es, entstore.TenantMiddleware(es))
	server := httptest.NewServer(api.PlatformAuthMiddleware(auth, router))
	defer server.Close()
	token := ""
	if mode == config.AuthModeBuiltin {
		request(server, http.MethodGet, "/api/settings/full", "", nil, http.StatusUnauthorized)
		var login struct {
			AccessToken string `json:"access_token"`
		}
		body := request(server, http.MethodPost, "/api/auth/login", "", map[string]string{
			"email": "restart@example.test", "password": "SyntheticPassword123!", "client_type": "cli",
		}, http.StatusOK)
		if err := json.Unmarshal(body, &login); err != nil || login.AccessToken == "" {
			t.Fatalf("new-process password login failed: %v", err)
		}
		token = login.AccessToken
	}
	var identity struct {
		User struct {
			ID, Email, Role string
			PlatformRole    string `json:"platform_role"`
		} `json:"user"`
		Org  struct{ ID, Slug string } `json:"org"`
		Team string                    `json:"team"`
	}
	if err := json.Unmarshal(request(server, http.MethodGet, "/api/auth/me", token, nil, http.StatusOK), &identity); err != nil {
		t.Fatal(err)
	}
	if identity.User.ID != evidence.UserID || identity.User.Email != users[0].Email || identity.User.Role != "owner" || identity.User.PlatformRole != "superadmin" || identity.Org.ID != evidence.OrgID || identity.Org.Slug != "process-restart" || identity.Team != "general" {
		t.Fatalf("restarted HTTP identity does not match setup: %+v", identity)
	}
	var fullSettings api.FullConfigResponse
	if err := json.Unmarshal(request(server, http.MethodGet, "/api/settings/full", token, nil, http.StatusOK), &fullSettings); err != nil {
		t.Fatalf("decode restarted full settings: %v", err)
	}
	if fullSettings.Chat.SystemPrompt != firstRunProcessPrompt || fullSettings.Chat.MaxTools != firstRunProcessMaxTools {
		t.Fatal("restarted full settings did not retain the setup chat canaries")
	}
	var teamProviders struct {
		Providers       map[string]map[string]string `json:"providers"`
		DefaultProvider string                       `json:"default_provider"`
		DefaultModel    string                       `json:"default_model"`
	}
	if err := json.Unmarshal(request(server, http.MethodGet, "/api/settings/team/providers", token, nil, http.StatusOK), &teamProviders); err != nil {
		t.Fatalf("decode restarted team provider settings: %v", err)
	}
	if teamProviders.DefaultProvider != firstRunProcessProvider || teamProviders.DefaultModel != firstRunProcessModel ||
		!reflect.DeepEqual(teamProviders.Providers, map[string]map[string]string{
			firstRunProcessProvider: {"type": "openai_compat", "base_url": firstRunProcessBaseURL},
		}) {
		t.Fatal("restarted team provider settings did not retain the setup provider canaries")
	}
	if mode == config.AuthModeBuiltin {
		request(server, http.MethodGet, "/api/settings/full", "", nil, http.StatusUnauthorized)
	}
	t.Log("separate-process SQLite reopen, encrypted secret continuity, identity and HTTP settings verified")
}

// TestE2E_SecurityUrgentFullConfigMasksLocal exercises only temporary SQLite and
// loopback HTTP, with production route registration, JWT and tenant middleware.
// Keep this and its subtests sequential: configuration paths are process-global.
// COVERS: CHAT-114
func TestE2E_SecurityUrgentFullConfigMasksLocal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		t.Setenv(name, root)
	}
	t.Setenv("SUDO_USER", "")
	t.Setenv("ASTONISH_MASTER_KEY", strings.Repeat("ab", 32))
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	svc, backend, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN: "file:" + filepath.Join(dataDir, "platform.db"), DataDir: dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	authCfg := config.PlatformAuthConfig{Mode: config.AuthModeBuiltin, JWTSecret: "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"}
	storage := config.StorageConfig{Backend: "sqlite", Auth: authCfg}
	auth := api.NewPlatformAuth(authCfg, backend, storage)
	var pendingAudit sync.WaitGroup
	tenantMW := func(next http.Handler) http.Handler {
		return entstore.TenantMiddleware(backend)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestSvc := *store.FromRequest(r)
			if requestSvc.Audit != nil && requestSvc.Mode == store.ModePlatform {
				// Register before the handler returns, so Close then Wait cannot
				// race an Add in the asynchronous audit goroutine.
				pendingAudit.Add(1)
				requestSvc.Audit = localMemoryTrackedAudit{AuditStore: requestSvc.Audit, pending: &pendingAudit}
				r = r.WithContext(store.WithServices(r.Context(), &requestSvc))
			}
			next.ServeHTTP(w, r)
		}))
	}
	router := mux.NewRouter()
	api.RegisterPlatformAuthRoutes(router, auth)
	api.RegisterRoutes(router, svc, backend, tenantMW)
	server := httptest.NewServer(api.PlatformAuthMiddleware(auth, router))
	// Register last, including on Fatal/Goexit: close HTTP and join audits
	// before seed cleanup decommissions tenant DBs or restores the environment.
	defer func() {
		t.Cleanup(func() {
			server.Close()
			pendingAudit.Wait()
		})
	}()
	h := &e2eboot.Harness{BaseURL: server.URL, Store: backend, DataDir: dataDir}
	seed := e2eboot.Seed(t, h)
	platformAdmin, teamAdmin := e2eboot.SeedProviderAuthority(t, h, seed)
	// Give a separate org member only org-admin authority; do not conflate it
	// with either platform or team administration in the denied-role matrix.
	orgAdminUser := seed.Users[e2eboot.UserCarolEmail]
	org, err := backend.Organizations().GetBySlug(context.Background(), orgAdminUser.OrgSlug)
	if err != nil || org == nil {
		t.Fatal("resolve isolated org-admin organization")
	}
	if err := backend.Organizations().SetMemberRole(context.Background(), orgAdminUser.ID, org.ID, "admin"); err != nil {
		t.Fatal("persist isolated org-admin role")
	}
	issuer := api.NewJWTIssuer(authCfg.JWTSecret, 15*time.Minute, 90*24*time.Hour)
	orgAdminUser.Token, err = issuer.IssueAccessToken(orgAdminUser.ID, orgAdminUser.Email, orgAdminUser.Name, orgAdminUser.OrgSlug, orgAdminUser.TeamSlug, "admin", "")
	if err != nil {
		t.Fatal("issue isolated org-admin JWT")
	}
	client := server.Client()
	client.Timeout = 30 * time.Second
	t.Cleanup(client.CloseIdleConnections)

	const (
		path       = "/api/settings/full"
		proxy      = "https://proxy-user:proxy-p%40ss@proxy.example.test:8080/tunnel?token=proxy-first%2fvalue&token=proxy-second+value&vendor=proxy-vendor&empty=#proxy-fragment"
		cdp        = "wss://cdp-user:cdp-p%40ss@browser.example.test:9222/devtools?token=cdp-first%2fvalue&token=cdp-second+value&vendor=cdp-vendor&empty=#cdp-fragment"
		key        = "synthetic-embedding-original-key"
		baseURL    = "https://embedding.example.test/v1"
		newProxy   = "https://new-proxy-user:new-proxy-password@new-proxy.example.test/tunnel?token=new-proxy-token"
		newCDP     = "wss://new-cdp-user:new-cdp-password@new-browser.example.test/devtools?token=new-cdp-token"
		newKey     = "synthetic-embedding-replacement-key"
		newBaseURL = "https://new-embedding.example.test/v1"
	)
	secrets := []string{
		"proxy-user", "proxy-p%40ss", "proxy-p@ss", "proxy-first", "proxy-second", "proxy-vendor", "proxy-fragment",
		"cdp-user", "cdp-p%40ss", "cdp-p@ss", "cdp-first", "cdp-second", "cdp-vendor", "cdp-fragment",
		"synthetic-vnc-password", key, newKey, "new-proxy-user", "new-proxy-password", "new-proxy-token",
		"new-cdp-user", "new-cdp-password", "new-cdp-token",
	}
	request := func(t *testing.T, actor *e2eboot.Client, method, endpoint string, payload any, want int) []byte {
		t.Helper()
		var body []byte
		if raw, ok := payload.(string); ok {
			body = []byte(raw) // Deliberately malformed JSON in authorization cases.
		} else if payload != nil {
			var err error
			body, err = json.Marshal(payload)
			if err != nil {
				t.Fatal("encode full-config request")
			}
		}
		req, err := http.NewRequest(method, server.URL+endpoint, bytes.NewReader(body))
		if err != nil {
			t.Fatal("construct full-config request")
		}
		req.Header.Set("Content-Type", "application/json")
		if actor != nil {
			req.Header.Set("Authorization", "Bearer "+actor.Token())
			req.Header.Set("X-Astonish-Team", actor.TeamSlug())
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("full-config HTTP request failed")
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal("read full-config HTTP response")
		}
		for _, secret := range secrets {
			if bytes.Contains(result, []byte(secret)) {
				t.Fatal("HTTP response disclosed a synthetic credential")
			}
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: HTTP %d, want %d", method, endpoint, resp.StatusCode, want)
		}
		if method == http.MethodPut && want == http.StatusOK {
			var status struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(result, &status) != nil || status.Status != "ok" {
				t.Fatal("successful PUT must acknowledge persisted settings")
			}
		}
		return result
	}
	reset := func(t *testing.T, storedProxy, storedCDP, storedKey string) {
		t.Helper()
		cfg := &config.AppConfig{Storage: storage}
		cfg.Browser = config.BrowserAppConfig{Proxy: storedProxy, RemoteCDPURL: storedCDP,
			KasmVNCPort: 6902, KasmVNCPassword: "synthetic-vnc-password", ViewportWidth: 1024}
		cfg.Memory.Embedding = config.EmbeddingConfig{Provider: "openai", BaseURL: baseURL, APIKey: storedKey}
		if err := config.SaveAppConfig(cfg); err != nil {
			t.Fatal("save temporary full-config fixture")
		}
	}
	snapshot := func(t *testing.T) []byte {
		t.Helper()
		filename, err := config.GetConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	readback := func(t *testing.T) api.FullConfigResponse {
		t.Helper()
		before := snapshot(t)
		var result api.FullConfigResponse
		if json.Unmarshal(request(t, platformAdmin, http.MethodGet, path, nil, http.StatusOK), &result) != nil {
			t.Fatal("decode full-config GET")
		}
		if !bytes.Equal(before, snapshot(t)) {
			t.Fatal("GET mutated the configuration file")
		}
		return result
	}
	assertStored := func(t *testing.T, wantProxy, wantCDP, wantKey, wantProvider, wantBase string) {
		t.Helper()
		cfg, err := config.LoadAppConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Browser.Proxy != wantProxy || cfg.Browser.RemoteCDPURL != wantCDP ||
			cfg.Memory.Embedding.APIKey != wantKey || cfg.Memory.Embedding.Provider != wantProvider || cfg.Memory.Embedding.BaseURL != wantBase {
			t.Fatal("persisted credentials/destination differ from the requested contract")
		}
		if cfg.Browser.KasmVNCPort != 6902 || cfg.Browser.KasmVNCPassword != "synthetic-vnc-password" {
			t.Fatal("browser update lost trusted handoff configuration")
		}
	}
	maskedPayload := func(mask api.FullConfigResponse) map[string]any {
		return map[string]any{
			"browser": map[string]any{"proxy": mask.Browser.Proxy, "remote_cdp_url": mask.Browser.RemoteCDPURL, "viewport_width": 1440},
			"memory":  map[string]any{"embedding": map[string]any{"provider": mask.Memory.Embedding.Provider, "base_url": mask.Memory.Embedding.BaseURL, "api_key": mask.Memory.Embedding.APIKey}},
		}
	}

	t.Run("platform_admin_masked_roundtrip", func(t *testing.T) {
		reset(t, proxy, cdp, key)
		var identity struct {
			User struct {
				Role         string `json:"role"`
				PlatformRole string `json:"platform_role"`
			} `json:"user"`
		}
		if json.Unmarshal(request(t, platformAdmin, http.MethodGet, "/api/auth/me", nil, http.StatusOK), &identity) != nil ||
			identity.User.Role != "member" || identity.User.PlatformRole != "superadmin" {
			t.Fatal("positive principal must be an independent platform admin, not an org admin/owner")
		}
		before := readback(t)
		for _, pair := range []struct{ masked, destination string }{
			{before.Browser.Proxy, "proxy.example.test:8080/tunnel"},
			{before.Browser.RemoteCDPURL, "browser.example.test:9222/devtools"},
		} {
			if !strings.Contains(pair.masked, pair.destination) || !strings.Contains(pair.masked, "****") || strings.Count(pair.masked, "token=") != 2 {
				t.Fatal("browser mask must retain destination and repeated query names")
			}
		}
		if before.Memory.Embedding.APIKey != "****-key" || before.Browser.KasmVNCPassword != "****word" {
			t.Fatal("GET must mask embedding and handoff credentials")
		}
		request(t, platformAdmin, http.MethodPut, path, maskedPayload(before), http.StatusOK)
		assertStored(t, proxy, cdp, key, "openai", baseURL)
		after := readback(t)
		if after.Browser.Proxy != before.Browser.Proxy || after.Browser.RemoteCDPURL != before.Browser.RemoteCDPURL ||
			after.Memory.Embedding.APIKey != before.Memory.Embedding.APIKey || after.Browser.ViewportWidth != 1440 {
			t.Fatal("masked roundtrip did not retain masks and persist the accompanying nonsecret edit")
		}
	})

	t.Run("rejected_masks", func(t *testing.T) {
		for _, field := range []string{"proxy", "remote_cdp_url", "embedding"} {
			t.Run(field, func(t *testing.T) {
				cases := []string{"invalid", "nonexistent", "changed_host", "changed_path", "changed_scheme", "changed_port", "changed_query", "encoded_nonexistent"}
				if field == "embedding" {
					cases = []string{"invalid", "nonexistent", "changed_base_url", "changed_provider", "changed_base_url_omitted_key", "changed_provider_omitted_key", "changed_base_url_null_key"}
				}
				for _, name := range cases {
					t.Run(name, func(t *testing.T) {
						reset(t, proxy, cdp, key)
						mask := readback(t)
						if name == "nonexistent" || name == "encoded_nonexistent" {
							p, c, k := proxy, cdp, key
							switch field {
							case "proxy":
								p = ""
							case "remote_cdp_url":
								c = ""
							case "embedding":
								k = ""
							}
							reset(t, p, c, k)
						}
						payload := maskedPayload(mask)
						// An earlier section would change in memory before validation fails;
						// rejection must still leave the entire file byte-for-byte intact.
						payload["chat"] = map[string]any{"system_prompt": "rejected-mask-must-not-persist"}
						if field == "embedding" {
							emb := payload["memory"].(map[string]any)["embedding"].(map[string]any)
							switch name {
							case "invalid":
								emb["api_key"] = "****stale"
							case "changed_base_url", "changed_base_url_omitted_key", "changed_base_url_null_key":
								emb["base_url"] = newBaseURL
							case "changed_provider", "changed_provider_omitted_key":
								emb["provider"] = "openai-compat"
							}
							if strings.HasSuffix(name, "_omitted_key") {
								delete(emb, "api_key")
							}
							if strings.HasSuffix(name, "_null_key") {
								emb["api_key"] = nil
							}
						} else {
							browser := payload["browser"].(map[string]any)
							value := browser[field].(string)
							host, port, urlPath, scheme := "proxy.example.test", ":8080", "/tunnel", "https:"
							if field == "remote_cdp_url" {
								host, port, urlPath, scheme = "browser.example.test", ":9222", "/devtools", "wss:"
							}
							switch name {
							case "invalid":
								value = "****stale"
							case "changed_host":
								value = strings.Replace(value, host, "other.example.test", 1)
							case "changed_path":
								value = strings.Replace(value, urlPath, "/other", 1)
							case "changed_scheme":
								value = strings.Replace(value, scheme, "http:", 1)
							case "changed_port":
								value = strings.Replace(value, port, ":9999", 1)
							case "changed_query":
								value = strings.Replace(value, "token=", "other=", 1)
							case "encoded_nonexistent":
								value = "https://example.test?token=%2A%2A%2A%2A"
							}
							browser[field] = value
						}
						before := snapshot(t)
						request(t, platformAdmin, http.MethodPut, path, payload, http.StatusBadRequest)
						if !bytes.Equal(before, snapshot(t)) {
							t.Fatal("rejected mask changed configuration bytes")
						}
						readback(t)
					})
				}
			})
		}
	})

	t.Run("denied_roles", func(t *testing.T) {
		reset(t, proxy, cdp, key)
		mask := readback(t)
		for _, actor := range []struct {
			name   string
			client *e2eboot.Client
			status int
		}{
			{"anonymous", nil, http.StatusUnauthorized},
			{"org_owner", seed.Client(e2eboot.UserAliceEmail), http.StatusForbidden},
			{"org_admin", seed.Client(e2eboot.UserCarolEmail), http.StatusForbidden},
			{"team_admin", teamAdmin, http.StatusForbidden},
			{"member", seed.Client(e2eboot.UserBobEmail), http.StatusForbidden},
		} {
			t.Run(actor.name, func(t *testing.T) {
				for _, name := range []string{"masked", "malformed"} {
					t.Run(name, func(t *testing.T) {
						var payload any = maskedPayload(mask)
						if name == "malformed" {
							payload = "not-json"
						}
						before := snapshot(t)
						request(t, actor.client, http.MethodPut, path, payload, actor.status)
						if !bytes.Equal(before, snapshot(t)) {
							t.Fatal("denied principal changed configuration bytes")
						}
						assertStored(t, proxy, cdp, key, "openai", baseURL)
					})
				}
			})
		}
	})

	t.Run("legitimate_updates", func(t *testing.T) {
		for _, name := range []string{"omitted", "null", "clear", "replacement", "changed_destination_clear", "changed_destination_replacement"} {
			t.Run(name, func(t *testing.T) {
				reset(t, proxy, cdp, key)
				browser := map[string]any{"viewport_width": 1600}
				emb := map[string]any{"model": "embedding-model-canary"}
				p, c, k, provider, destination := proxy, cdp, key, "openai", baseURL
				switch name {
				case "null":
					browser["proxy"], browser["remote_cdp_url"] = nil, nil
					emb["api_key"], emb["provider"], emb["base_url"] = nil, nil, nil
				case "clear", "changed_destination_clear":
					p, c, k = "", "", ""
					browser["proxy"], browser["remote_cdp_url"], emb["api_key"] = p, c, k
				case "replacement", "changed_destination_replacement":
					p, c, k = newProxy, newCDP, newKey
					browser["proxy"], browser["remote_cdp_url"], emb["api_key"] = p, c, k
				}
				if strings.HasPrefix(name, "changed_destination_") {
					provider, destination = "openai-compat", newBaseURL
					emb["provider"], emb["base_url"] = provider, destination
				}
				before := readback(t)
				request(t, platformAdmin, http.MethodPut, path, map[string]any{
					"browser": browser, "memory": map[string]any{"embedding": emb},
				}, http.StatusOK)
				assertStored(t, p, c, k, provider, destination)
				after := readback(t)
				if after.Browser.ViewportWidth != 1600 || after.Memory.Embedding.Model != "embedding-model-canary" ||
					after.Memory.Embedding.Provider != provider || after.Memory.Embedding.BaseURL != destination {
					t.Fatal("legitimate nonsecret update did not persist through HTTP")
				}
				if name == "omitted" || name == "null" {
					if after.Browser.Proxy != before.Browser.Proxy || after.Browser.RemoteCDPURL != before.Browser.RemoteCDPURL || after.Memory.Embedding.APIKey != before.Memory.Embedding.APIKey {
						t.Fatal("omitted/null credentials did not retain their masks")
					}
				} else if k == "" {
					if after.Browser.Proxy != "" || after.Browser.RemoteCDPURL != "" || after.Memory.Embedding.APIKey != "" {
						t.Fatal("explicit clear did not clear HTTP readback")
					}
				} else if !strings.Contains(after.Browser.Proxy, "new-proxy.example.test") || !strings.Contains(after.Browser.RemoteCDPURL, "new-browser.example.test") || after.Memory.Embedding.APIKey != "****-key" {
					t.Fatal("replacement credentials did not produce masked readback")
				}
			})
		}
	})
}

// TestE2E_Skill_TeamIsolation verifies that team skills are visible to team
// members but not to members of other teams.
//
// COVERS: CHAT-022
func TestE2E_Skill_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) sees red team skill
	t.Run("alice_sees_red_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.SkillAcmeRedTeam) {
			t.Error("alice (red team) cannot see red team skill")
		}
	})

	// Carol (blue team) does NOT see red team skill
	t.Run("carol_does_not_see_red_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.SkillAcmeRedTeam) {
			t.Error("carol (blue team) should NOT see red team skill")
		}
	})

	// Carol (blue team) sees blue team skill
	t.Run("carol_sees_blue_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.SkillAcmeBlueTeam) {
			t.Error("carol (blue team) cannot see blue team skill")
		}
	})

	// Eve (globex) does NOT see acme red team skill
	t.Run("eve_does_not_see_acme_skills", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.SkillAcmeRedTeam) || strings.Contains(body, e2eboot.SkillAcmeBlueTeam) {
			t.Error("eve (globex) should NOT see any acme team skills")
		}
	})
}

// TestE2E_MCP_TeamIsolation verifies that team MCP servers are visible to
// team members but not to members of other teams or other orgs.
//
// COVERS: CHAT-023
func TestE2E_MCP_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) sees red team MCP server
	t.Run("alice_sees_red_team_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.MCPAcmeRedTeam) {
			t.Error("alice (red team) cannot see red team MCP server")
		}
	})

	// Eve (globex) does NOT see acme MCP servers
	t.Run("eve_does_not_see_acme_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.MCPAcmeRedTeam) || strings.Contains(body, e2eboot.MCPAcmeBlueTeam) || strings.Contains(body, e2eboot.MCPAcmeOrg) {
			t.Error("eve (globex) should NOT see any acme MCP servers")
		}
	})

	// Eve sees globex MCP server
	t.Run("eve_sees_globex_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.MCPGlobexTeam) {
			t.Error("eve (globex) cannot see her own team MCP server")
		}
	})
}

// TestE2E_Credential_PersonalIsolation verifies that personal credentials
// are visible only to their owner.
//
// COVERS: CHAT-024
func TestE2E_Credential_PersonalIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice sees her personal credential
	t.Run("alice_sees_own_credential", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.CredAlicePersonal) {
			t.Error("alice cannot see her own personal credential")
		}
	})

	// Alice does NOT see bob's credential
	t.Run("alice_does_not_see_bob_credential", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("alice should NOT see bob's personal credential")
		}
	})

	// Bob sees his own but not alice's
	t.Run("bob_sees_own_not_alice", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("bob cannot see his own personal credential")
		}
		if strings.Contains(body, e2eboot.CredAlicePersonal) {
			t.Error("bob should NOT see alice's personal credential")
		}
	})

	// Eve (different org) does NOT see acme credentials
	t.Run("eve_does_not_see_acme_credentials", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.CredAlicePersonal) || strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("eve (globex) should NOT see any acme personal credentials")
		}
	})
}

// TestE2E_Session_CrossOrgDenied verifies that a user in one org cannot
// access another org's session by ID.
//
// COVERS: CHAT-026
func TestE2E_Session_CrossOrgDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice creates a session; an emitted ID alone is not a completed chat.
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	const aliceMessage = "Hello from Alice for session isolation test."
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": aliceMessage,
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	// Self-clean Alice's session so this test is safe under shared-seed
	// (Plan D): tests in chat_auth share Alice's session list across runs.
	// Skipped under shared-inspect mode (ASTONISH_E2E_KEEP_ALIVE=1) so the
	// session remains browsable in the inspector UI.
	t.Cleanup(func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		if resp != nil {
			resp.Body.Close()
		}
	})

	urgentCompletedChat(t, events)
	path := "/api/studio/sessions/" + sessionID
	// Prove the owner's HTTP view contains the actual persisted session, not
	// an empty success response, before attempting the foreign read.
	before := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK)
	var detail api.StudioSessionDetailResponse
	if err := json.Unmarshal([]byte(before), &detail); err != nil {
		t.Fatalf("decode owner's session: %v", err)
	}
	hasOwnerMessage := false
	for _, message := range detail.Messages {
		if message.Content == aliceMessage {
			hasOwnerMessage = true
		}
	}
	if detail.ID != sessionID || !hasOwnerMessage {
		t.Fatal("owner's persisted session lacks its identity or original content")
	}

	// Eve (different org) must receive an explicit authorization denial;
	// empty 200, rate limiting and server errors cannot prove isolation.
	body := urgentStatus(t, seed.Client(e2eboot.UserEveEmail).Get(t, path), http.StatusForbidden, http.StatusNotFound)
	for _, private := range []string{sessionID, aliceMessage, seed.Users[e2eboot.UserAliceEmail].ID, e2eboot.UserAliceEmail} {
		if strings.Contains(body, private) {
			t.Error("denial response leaked the owner's identity or session content")
		}
	}
	for _, message := range detail.Messages {
		if message.Content != "" && strings.Contains(body, message.Content) {
			t.Error("denial response leaked persisted session content")
		}
	}
	if after := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK); after != before {
		t.Error("rejected cross-org read changed the owner's persisted session")
	}
}
