//go:build e2e

package chat_auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/SAP/astonish/pkg/store/pgutil"
	"github.com/SAP/astonish/tests/e2eboot"
)

// This is intentionally separate from SQLite's no-infrastructure first-run test.
// Approval is for an entire disposable PG instance: setup changes global roles.
// No cleanup sweep is attempted; the operator must destroy that dedicated instance
// after acceptance (including on failure). No approval is synthesized here.
// COVERS: CHAT-115
func TestE2E_SecurityPostgresFirstRunSetup(t *testing.T) {
	if err := e2eboot.ValidatePostgresFirstRunPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before first-run effects: %v", err)
	}
	if err := e2eboot.ValidateSecurityPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before first-run effects: %v", err)
	}
	root := t.TempDir()
	for _, stage := range []string{firstRunProcessSetup, firstRunProcessRestart} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPostgresFirstRunProcessHelper$", "-test.timeout=170s")
			cmd.Dir = root
			// Same isolated process/config boundary as SQLite continuity. Pass only
			// explicit PG approval and target, never provider keys, kubeconfig,
			// proxies, PG* defaults, operator master keys or bootstrap tokens.
			cmd.Env = []string{"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "TMPDIR=" + root,
				"SUDO_USER=", "ASTONISH_FIRST_RUN_PROCESS=" + stage}
			for _, key := range []string{"ASTONISH_E2E_BACKEND", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED", "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN"} {
				cmd.Env = append(cmd.Env, key+"="+os.Getenv(key))
			}
			// Do not print child logs: a regressed driver may include a DSN.
			if err := cmd.Run(); err != nil {
				t.Fatal("approved PostgreSQL first-run subprocess failed (output withheld)")
			}
		})
		if t.Failed() {
			return
		}
	}
}

// TestPostgresFirstRunGateBeforeEffects exercises the actual acceptance entry
// point with no inherited target or credentials. It never connects to PostgreSQL.
func TestPostgresFirstRunGateBeforeEffects(t *testing.T) {
	for _, target := range []string{"TestE2E_SecurityPostgresFirstRunSetup", "TestPostgresFirstRunProcessHelper"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+target+"$", "-test.timeout=15s")
			cmd.Dir = root
			cmd.Env = []string{"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "TMPDIR=" + root, "SUDO_USER=", "ASTONISH_FIRST_RUN_PROCESS=" + firstRunProcessSetup}
			out, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "BLOCKED before first-run effects") {
				t.Fatal("first-run entry did not fail closed at the approval gate")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected first-run entry created files")
			}
		})
	}
}

// TestPostgresFirstRunIsolationBeforeEffects supplies only synthetic loopback
// parameters; every child must reject its environment before setup or reopening.
// Not an acceptance/manifest test: no real PostgreSQL target is used here.
func TestPostgresFirstRunIsolationBeforeEffects(t *testing.T) {
	cases := []string{"cwd", "home", "tmpdir", "config", "sudo", "stage",
		"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "PGOPTIONS", "PGFUTUREDEFAULT",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"KUBECONFIG", "KUBERNETES_SERVICE_HOST", "APPDATA", "XDG_CONFIG_DIRS", "ASTONISH_TEST_DSN", "ASTONISH_MASTER_KEY"}
	for _, stage := range []string{firstRunProcessSetup, firstRunProcessRestart} {
		for _, name := range cases {
			t.Run(stage+"/"+name, func(t *testing.T) {
				root, other := t.TempDir(), t.TempDir()
				env := map[string]string{
					"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"), "TMPDIR": root, "SUDO_USER": "",
					"ASTONISH_FIRST_RUN_PROCESS": stage, "ASTONISH_E2E_BACKEND": "postgres",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED": "1",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE":  "127.0.0.1:1",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN":       "postgres://synthetic:synthetic@127.0.0.1:1/postgres?sslmode=disable",
				}
				dir := root
				want := "helper requires matching isolated cwd/HOME/TMPDIR/config paths"
				switch name {
				case "cwd":
					dir = other
				case "home":
					env["HOME"] = other
				case "tmpdir":
					env["TMPDIR"] = other
				case "config":
					env["XDG_CONFIG_HOME"] = other
				case "sudo":
					env["SUDO_USER"] = "synthetic"
				case "stage":
					env["ASTONISH_FIRST_RUN_PROCESS"] = "invalid"
					want = "invalid PostgreSQL subprocess stage"
				default:
					// Presence alone is forbidden, even if the inherited default
					// is empty. Never copy the operator's environment or target.
					env[name] = ""
					want = "inherited PostgreSQL helper environment is forbidden"
				}
				if err := e2eboot.ValidatePostgresFirstRunPreflight(func(key string) string { return env[key] }); err != nil {
					t.Fatal("synthetic input failed approval validation before isolation check")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPostgresFirstRunProcessHelper$", "-test.timeout=15s")
				cmd.Dir = dir
				for key, value := range env {
					cmd.Env = append(cmd.Env, key+"="+value)
				}
				out, err := cmd.CombinedOutput()
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "BLOCKED before first-run effects: "+want) {
					t.Fatal("helper did not reject isolation violation before effects (output withheld)")
				}
				for _, path := range []string{root, other} {
					entries, err := os.ReadDir(path)
					if err != nil || len(entries) != 0 {
						t.Fatal("rejected helper modified isolated paths")
					}
				}
			})
		}
	}
}

// Symlink probes retain a forbidden environment variable as a second safety
// barrier: even a regressed path gate cannot reach a driver or setup handler.
func TestPostgresFirstRunSymlinkIsolationBeforeEffects(t *testing.T) {
	for _, stage := range []string{firstRunProcessSetup, firstRunProcessRestart} {
		for _, relative := range []string{"config", "config/astonish", "config/astonish/config.yaml", "process-evidence.json"} {
			t.Run(stage+"/"+relative, func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				link := filepath.Join(root, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPostgresFirstRunProcessHelper$", "-test.timeout=15s")
				cmd.Dir = root
				cmd.Env = []string{
					"HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "TMPDIR=" + root, "SUDO_USER=",
					"ASTONISH_FIRST_RUN_PROCESS=" + stage, "ASTONISH_E2E_BACKEND=postgres",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED=1",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE=127.0.0.1:1",
					"ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN=postgres://synthetic:synthetic@127.0.0.1:1/postgres?sslmode=disable",
					"PGHOST=", // mandatory fallback barrier; never inherit real targets
				}
				out, err := cmd.CombinedOutput()
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "BLOCKED before first-run effects: helper paths must not contain symlinks") {
					t.Fatal("helper did not reject redirected paths before effects (output withheld)")
				}
				if target, err := os.Readlink(link); err != nil || target != outside {
					t.Fatal("rejected helper changed isolation symlink")
				}
				if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
					t.Fatal("rejected helper modified paths outside isolated root")
				}
			})
		}
	}
}

// TestPostgresFirstRunProcessHelper reuses SQLite's stage and persisted evidence
// types, but PG setup bootstraps users via registration after reopening, not in
// the init handler. This test is inert except when explicitly selected by parent.
func TestPostgresFirstRunProcessHelper(t *testing.T) {
	stage := os.Getenv("ASTONISH_FIRST_RUN_PROCESS")
	if stage == "" {
		t.Skip("subprocess helper")
	}
	if err := e2eboot.ValidatePostgresFirstRunPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before first-run effects: %v", err)
	}
	if stage != firstRunProcessSetup && stage != firstRunProcessRestart {
		t.Fatal("BLOCKED before first-run effects: invalid PostgreSQL subprocess stage")
	}
	root := os.Getenv("HOME")
	cwd, cwdErr := os.Getwd()
	// Resolve symlinks for Darwin's /var -> /private/var temporary roots,
	// but require HOME and TMPDIR to name exactly the same isolated root.
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedCWD, resolveErr := filepath.EvalSymlinks(cwd)
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) ||
		cwdErr != nil || rootErr != nil || resolveErr != nil || resolvedCWD != resolvedRoot ||
		os.Getenv("TMPDIR") != root || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(root, "config") || os.Getenv("SUDO_USER") != "" {
		t.Fatal("BLOCKED before first-run effects: helper requires matching isolated cwd/HOME/TMPDIR/config paths")
	}
	// Checking only XDG_CONFIG_HOME's spelling is insufficient: an existing
	// component can redirect config or evidence reads/writes outside this root.
	// Lstat each component (including dangling links); the private root remains
	// owned by the parent, and Darwin's system /var alias is resolved above.
	for _, relative := range []string{"config", "config/astonish", "config/astonish/config.yaml", "process-evidence.json"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("BLOCKED before first-run effects: helper paths must not contain symlinks")
		}
	}
	// This is a process boundary, not just a DSN check: reject every inherited
	// default, including empty PG*, proxy and service-routing variables. Only
	// the parent's explicit allowlist is accepted; never log names or values.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "HOME", "XDG_CONFIG_HOME", "TMPDIR", "SUDO_USER", "ASTONISH_FIRST_RUN_PROCESS",
			"ASTONISH_E2E_BACKEND", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED",
			"ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN":
		default:
			t.Fatal("BLOCKED before first-run effects: inherited PostgreSQL helper environment is forbidden")
		}
	}
	u, _ := url.Parse(os.Getenv("ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN")) // validated, never logged
	password, _ := u.User.Password()
	port, _ := strconv.Atoi(u.Port())
	payload := api.PlatformInitRequest{Host: u.Hostname(), Port: port, User: u.User.Username(), Password: password, SSLMode: u.Query().Get("sslmode"), OrgName: "Process Restart", OrgSlug: "process-restart"}
	// Synthetic test key only; PG setup does not generate SQLite's key file.
	t.Setenv("ASTONISH_MASTER_KEY", strings.Repeat("cd", 32))
	request := func(server *httptest.Server, method, path, token string, payload any, want int) []byte {
		t.Helper()
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal("encode first-run request")
		}
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal("construct first-run request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Astonish-Bootstrap-Token", token)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := server.Client()
		client.Timeout = 75 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("first-run HTTP transport failed")
		}
		return []byte(urgentStatus(t, resp, want))
	}
	configPath := filepath.Join(root, "config", "astonish", "config.yaml")
	if stage == firstRunProcessSetup {
		if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
			t.Fatal("setup requires absent configuration")
		}
		t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "synthetic-process-bootstrap-token")
		router := mux.NewRouter()
		api.RegisterRoutes(router, nil, nil, nil)
		server := httptest.NewServer(router)
		defer server.Close()
		var result api.PlatformInitResponse
		body := request(server, http.MethodPost, "/api/platform/init", "synthetic-process-bootstrap-token", payload, http.StatusOK)
		if err := json.Unmarshal(body, &result); err != nil || !result.Success || !result.RestartRequired || result.Error != "" {
			t.Fatal("authorized PG setup did not persist restartable configuration")
		}
		server.Close()
	}
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil || !cfg.Storage.IsPostgres() || cfg.Storage.Auth.Mode != config.AuthModeBuiltin || cfg.Storage.Auth.JWTSecret == "" || cfg.Storage.Postgres.InstanceSuffix == "" {
		t.Fatal("invalid persisted PostgreSQL first-run configuration")
	}
	expectedDSN := pgutil.BuildDSN(payload.Host, payload.Port, payload.User, payload.Password, config.PlatformDBName(cfg.Storage.Postgres.InstanceSuffix), payload.SSLMode)
	if cfg.Storage.Postgres.PlatformDSN != expectedDSN {
		t.Fatal("persisted PostgreSQL target differs from approved instance")
	}
	ctx := context.Background()
	svc, es, err := entstore.NewPlatformServices(ctx, entstore.Config{DSN: cfg.Storage.Postgres.PlatformDSN, InstanceSuffix: cfg.Storage.Postgres.InstanceSuffix, MaxOpenConns: 3, MaxIdleConns: 2})
	if err != nil {
		t.Fatal("cannot reopen persisted PostgreSQL first-run services")
	}
	defer es.Close()
	auth := api.NewPlatformAuth(cfg.Storage.Auth, es, cfg.Storage)
	var pendingAudit sync.WaitGroup
	tenantMW := func(next http.Handler) http.Handler {
		return entstore.TenantMiddleware(es)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestSvc := *store.FromRequest(r)
			if requestSvc.Audit != nil && requestSvc.Mode == store.ModePlatform {
				// Add before AuditMiddleware starts asynchronous Log. Closing
				// HTTP before Wait prevents new work from racing the drain.
				pendingAudit.Add(1)
				requestSvc.Audit = localMemoryTrackedAudit{AuditStore: requestSvc.Audit, pending: &pendingAudit}
				r = r.WithContext(store.WithServices(r.Context(), &requestSvc))
			}
			next.ServeHTTP(w, r)
		}))
	}
	router := mux.NewRouter()
	api.RegisterPlatformAuthRoutes(router, auth)
	api.RegisterRoutes(router, svc, es, tenantMW)
	server := httptest.NewServer(api.PlatformAuthMiddleware(auth, router))
	defer func() {
		server.Close()
		pendingAudit.Wait()
	}() // also on Fatal/Goexit, before es.Close and environment cleanup
	credentials := map[string]string{"email": "restart@example.test", "password": "SyntheticPassword123!", "client_type": "cli"}
	if stage == firstRunProcessSetup {
		users, err := es.Users().List(ctx)
		if err != nil || len(users) != 0 {
			t.Fatal("PG init must precede first-user registration")
		}
		request(server, http.MethodPost, "/api/auth/register", "", credentials, http.StatusOK)
	}
	request(server, http.MethodGet, "/api/settings/full", "", nil, http.StatusUnauthorized)
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(request(server, http.MethodPost, "/api/auth/login", "", credentials, http.StatusOK), &login); err != nil || login.AccessToken == "" {
		t.Fatal("persisted first-user password login failed")
	}
	users, err := es.Users().List(ctx)
	if err != nil || len(users) != 1 || users[0].PlatformRole != "superadmin" {
		t.Fatal("first-user identity missing")
	}
	orgs, err := es.Organizations().List(ctx)
	if err != nil || len(orgs) != 1 || orgs[0].Slug != "process-restart" {
		t.Fatal("first organization missing")
	}
	org, err := es.ForOrg("process-restart")
	if err != nil {
		t.Fatal("first organization cannot reopen")
	}
	teams, err := org.Teams().ListTeams(ctx)
	if err != nil || len(teams) != 1 || teams[0].Slug != "general" {
		t.Fatal("first team missing")
	}
	role, err := org.Teams().GetMemberRole(ctx, users[0].ID, teams[0].ID)
	if err != nil || role != "admin" {
		t.Fatal("first-user team membership missing")
	}
	var identity struct {
		User struct {
			ID, Role     string
			PlatformRole string `json:"platform_role"`
		} `json:"user"`
		Org  struct{ ID, Slug string } `json:"org"`
		Team string                    `json:"team"`
	}
	if err := json.Unmarshal(request(server, http.MethodGet, "/api/auth/me", login.AccessToken, nil, http.StatusOK), &identity); err != nil || identity.User.ID != users[0].ID || identity.User.Role != "owner" || identity.User.PlatformRole != "superadmin" || identity.Org.ID != orgs[0].ID || identity.Team != "general" {
		t.Fatal("HTTP identity differs from persisted bootstrap memberships")
	}
	request(server, http.MethodGet, "/api/settings/full", login.AccessToken, nil, http.StatusOK)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("cannot snapshot first-run config")
	}
	beforeState, err := json.Marshal([]any{users, orgs, teams})
	if err != nil {
		t.Fatal("cannot snapshot persisted first-run identities")
	}
	for _, path := range []string{"/api/platform/init", "/api/platform/init/sqlite"} {
		request(server, http.MethodPost, path, login.AccessToken, map[string]string{}, http.StatusConflict)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("configured init changed first-run config/JWT")
	}
	afterUsers, errUsers := es.Users().List(ctx)
	afterOrgs, errOrgs := es.Organizations().List(ctx)
	afterTeams, errTeams := org.Teams().ListTeams(ctx)
	afterState, errState := json.Marshal([]any{afterUsers, afterOrgs, afterTeams})
	if errUsers != nil || errOrgs != nil || errTeams != nil || errState != nil || !bytes.Equal(beforeState, afterState) {
		t.Fatal("configured init changed persisted users, organizations or teams")
	}
	// Reuse SQLite continuity evidence: a genuinely new process must reopen
	// the same config and user/org/team, without seeding replacement identities.
	sum := sha256.Sum256(before)
	evidence := firstRunProcessEvidence{PID: os.Getpid(), UserID: users[0].ID, OrgID: orgs[0].ID, TeamID: teams[0].ID, ConfigHash: hex.EncodeToString(sum[:])}
	path := filepath.Join(root, "process-evidence.json")
	if stage == firstRunProcessSetup {
		data, err := json.Marshal(evidence)
		if err != nil || os.WriteFile(path, data, 0600) != nil {
			t.Fatal("cannot persist first-run evidence")
		}
		return
	}
	data, err := os.ReadFile(path)
	var previous firstRunProcessEvidence
	if err != nil || json.Unmarshal(data, &previous) != nil || previous.PID == 0 || previous.PID == evidence.PID {
		t.Fatal("restart requires a separate completed setup process")
	}
	previous.PID = evidence.PID
	if previous != evidence {
		t.Fatal("restart changed config/JWT or first-user/org/team identity")
	}
}

// No real target or credentials: even a regressed authority guard can reach
// only an unused loopback port. Requests must leave temporary paths absent.
func TestPostgresFirstRunRejectionsLocal(t *testing.T) {
	h := e2eboot.BootstrapFirstRunSetup(t)
	for _, tc := range []struct {
		name, token, body string
		status            int
	}{
		{"missing-token", "", `{"host":"127.0.0.1","port":1,"user":"synthetic","password":"synthetic"}`, http.StatusForbidden},
		{"wrong-token", "wrong", `{"host":"127.0.0.1","port":1,"user":"synthetic","password":"synthetic"}`, http.StatusForbidden},
		{"malformed-unauthorized", "", "{", http.StatusForbidden},
		{"malformed-authorized", h.BootstrapToken, "{", http.StatusBadRequest},
		{"missing-user", h.BootstrapToken, `{"host":"127.0.0.1","port":1,"password":"synthetic"}`, http.StatusBadRequest},
		{"missing-password", h.BootstrapToken, `{"host":"127.0.0.1","port":1,"user":"synthetic"}`, http.StatusBadRequest},
		{"invalid-slug", h.BootstrapToken, `{"host":"127.0.0.1","port":1,"user":"synthetic","password":"synthetic","org_slug":"INVALID!"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/platform/init", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal("construct rejection request")
			}
			req.Header.Set("X-Astonish-Bootstrap-Token", tc.token)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("rejection HTTP transport failed")
			}
			var result api.PlatformInitResponse
			if err := json.Unmarshal([]byte(urgentStatus(t, resp, tc.status)), &result); err != nil || result.Success || result.RestartRequired || result.Error == "" {
				t.Fatal("rejected setup returned ambiguous response")
			}
			for _, path := range []string{h.ConfigDir, h.DataDir} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("rejected setup created config, key or data paths")
				}
			}
		})
	}
}

// TestE2E_SecurityUrgentConfigAuthority exercises real auth, tenancy and persistence.
// No mocks; unavailable bootstrap prerequisites are rejected by the strict runner.
func TestE2E_SecurityUrgentConfigAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	// Org owner has legitimate access to org provider management, but not deployment writes.
	resp := alice.Get(t, "/api/settings/org/providers")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner org read: %d", resp.StatusCode)
	}
	before := alice.Get(t, "/api/settings/full")
	if before.StatusCode != http.StatusOK {
		before.Body.Close()
		t.Fatalf("full read: %d", before.StatusCode)
	}
	beforeBody := e2eboot.ReadBody(t, before)
	before.Body.Close()
	for _, email := range []string{e2eboot.UserAliceEmail, e2eboot.UserBobEmail, e2eboot.UserEveEmail} {
		c := seed.Client(email)
		denied := c.Put(t, "/api/settings/full", map[string]any{"agent_identity": map[string]string{"name": "unauthorized-canary"}})
		denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("%s deployment write: %d", email, denied.StatusCode)
		}
		denied = c.Get(t, "/api/settings/platform/providers")
		denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("%s platform read: %d", email, denied.StatusCode)
		}
	}
	after := alice.Get(t, "/api/settings/full")
	defer after.Body.Close()
	if after.StatusCode != http.StatusOK || e2eboot.ReadBody(t, after) != beforeBody {
		t.Fatal("denied writes changed deployment config")
	}
}

// TestE2E_SecurityUrgentIndependentRoleAuthority proves each role independently:
// the org admin is only a team member, and the platform/team admins are only
// org members. Successful nonempty writes are paired with forbidden writes and
// exact persisted snapshots, not merely successful authentication or empty PUTs.
// COVERS: CHAT-111
func TestE2E_SecurityUrgentIndependentRoleAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	platformAdmin, teamAdmin := e2eboot.SeedProviderAuthority(t, h, seed)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	carol := seed.Users[e2eboot.UserCarolEmail]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot encode role authority snapshot")
		}
		return data
	}
	// Exercise the existing role-management and login surfaces rather than
	// minting an owner token or changing the shared seed's role definitions.
	orgRecord, err := h.PlatformBackend().Organizations().GetBySlug(ctx, carol.OrgSlug)
	if err != nil || orgRecord == nil || carol.OrgRole != "member" || carol.TeamRole != "member" {
		t.Fatal("independent org-admin fixture requires a team member")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := h.PlatformBackend().Organizations().SetMemberRole(cleanupCtx, carol.ID, orgRecord.ID, "member"); err != nil {
			t.Error("cannot restore org-admin fixture membership")
		}
	})
	urgentStatus(t, alice.Put(t, "/api/admin/users/"+carol.ID+"/role", map[string]string{"role": "admin"}), http.StatusOK)
	loginBody := encode(map[string]string{"email": carol.Email, "password": e2eboot.SeededUserPassword, "client_type": "cli"})
	httpClient := &http.Client{Timeout: 30 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)
	loginResponse, err := httpClient.Post(h.BaseURL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal("independent org-admin login request failed")
	}
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(urgentStatus(t, loginResponse, http.StatusOK)), &login); err != nil || login.AccessToken == "" {
		t.Fatal("independent org-admin login did not issue an access token")
	}
	request := func(t *testing.T, token, team, method, path string, payload any, status int) string {
		t.Helper()
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(encode(payload))
		}
		req, err := http.NewRequestWithContext(ctx, method, h.BaseURL+path, body)
		if err != nil {
			t.Fatal("cannot construct role authority request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Astonish-Team", team)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatal("role authority HTTP request failed")
		}
		return urgentStatus(t, resp, status)
	}
	var identity struct {
		User struct {
			ID           string `json:"id"`
			Role         string `json:"role"`
			PlatformRole string `json:"platform_role"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(request(t, login.AccessToken, carol.TeamSlug, http.MethodGet, "/api/auth/me", nil, http.StatusOK)), &identity); err != nil || identity.User.ID != carol.ID || identity.User.Role != "admin" || identity.User.PlatformRole != "" {
		t.Fatal("org-admin login is not an independent non-platform principal")
	}
	carolOrg, err := h.PlatformBackend().ForOrg(carol.OrgSlug)
	if err != nil {
		t.Fatal("cannot resolve org-admin team membership")
	}
	carolTeam, err := carolOrg.Teams().GetTeamBySlug(ctx, carol.TeamSlug)
	if err != nil || carolTeam == nil {
		t.Fatal("cannot resolve org-admin team")
	}
	if role, err := carolOrg.Teams().GetMemberRole(ctx, carol.ID, carolTeam.ID); err != nil || role != "member" {
		t.Fatal("org-admin positive would be confounded by team-admin privileges")
	}

	// Seed synthetic secrets in each settings scope and preserve the complete
	// original objects. Never print snapshots, which may include harness secrets.
	type snapshot struct {
		name string
		read func() (any, error)
	}
	var scopes []snapshot
	var secrets []string
	platform := h.PlatformBackend().PlatformSettings()
	platformOriginal, err := platform.Get(ctx)
	if err != nil || platformOriginal == nil {
		t.Fatal("cannot preserve platform settings")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := platform.Save(cleanupCtx, platformOriginal); err != nil {
			t.Error("cannot restore role authority platform settings")
		}
	})
	platformCanary := *platformOriginal
	platformCanary.Providers = map[string]store.ProviderConfig{"role-platform": {"type": "openai", "api_key": "urgent-role-platform-secret"}}
	if err := platform.Save(ctx, &platformCanary); err != nil {
		t.Fatal("cannot seed platform role canary")
	}
	secrets = append(secrets, "urgent-role-platform-secret")
	scopes = append(scopes, snapshot{"platform", func() (any, error) { return platform.Get(ctx) }})
	seenOrgs := make(map[string]bool)
	for _, email := range []string{e2eboot.UserAliceEmail, e2eboot.UserCarolEmail, e2eboot.UserDaveEmail} {
		user := seed.Users[email]
		org, err := h.PlatformBackend().ForOrg(user.OrgSlug)
		if err != nil {
			t.Fatal("cannot resolve role authority org")
		}
		if !seenOrgs[user.OrgSlug] {
			seenOrgs[user.OrgSlug] = true
			settings := h.PlatformBackend().OrgSettings(user.OrgSlug)
			original, err := settings.Get(ctx)
			if err != nil || original == nil {
				t.Fatal("cannot preserve org settings")
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if err := settings.Save(cleanupCtx, original); err != nil {
					t.Error("cannot restore role authority org settings")
				}
			})
			canary := *original
			secret := "urgent-role-org-" + user.OrgSlug + "-secret"
			canary.Providers = map[string]store.ProviderConfig{"role-org": {"type": "openai", "api_key": secret}}
			if err := settings.Save(ctx, &canary); err != nil {
				t.Fatal("cannot seed org role canary")
			}
			secrets = append(secrets, secret)
			scopes = append(scopes, snapshot{user.OrgSlug + "/org", func() (any, error) { return settings.Get(ctx) }})
		}
		settings := org.ForTeam(user.TeamSlug).Settings()
		original, err := settings.Get(ctx)
		if err != nil || original == nil {
			t.Fatal("cannot preserve team settings")
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if err := settings.Save(cleanupCtx, original); err != nil {
				t.Error("cannot restore role authority team settings")
			}
		})
		canary := *original
		secret := "urgent-role-team-" + user.OrgSlug + "-" + user.TeamSlug + "-secret"
		canary.Providers = map[string]store.ProviderConfig{"role-team": {"type": "openai", "api_key": secret}}
		if err := settings.Save(ctx, &canary); err != nil {
			t.Fatal("cannot seed team role canary")
		}
		secrets = append(secrets, secret)
		scopes = append(scopes, snapshot{user.OrgSlug + "/" + user.TeamSlug, func() (any, error) { return settings.Get(ctx) }})
	}
	configPath, err := config.GetConfigPath()
	if err != nil {
		t.Fatal("cannot locate deployment config")
	}
	configOriginal, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("cannot preserve deployment config")
	}
	configInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal("cannot preserve deployment config permissions")
	}
	t.Cleanup(func() {
		if err := os.WriteFile(configPath, configOriginal, configInfo.Mode().Perm()); err != nil {
			t.Error("cannot restore role authority deployment config")
		}
	})
	assertSecretFree := func(t *testing.T, body string) {
		t.Helper()
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Fatal("role authority response exposed a secret canary")
			}
		}
	}
	for _, tc := range []struct {
		name, token, team, endpoint, target string
		orgProviders, fullConfig            bool
	}{
		{"org-admin-settings", login.AccessToken, carol.TeamSlug, "/api/settings/config", carol.OrgSlug + "/" + carol.TeamSlug, false, false},
		{"org-admin-providers", login.AccessToken, carol.TeamSlug, "/api/settings/org/providers", carol.OrgSlug + "/org", true, false},
		{"team-admin-providers", teamAdmin.Token(), teamAdmin.TeamSlug(), "/api/settings/config", carol.OrgSlug + "/" + teamAdmin.TeamSlug(), false, false},
		{"platform-admin-full-config", platformAdmin.Token(), platformAdmin.TeamSlug(), "/api/settings/full", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := make(map[string][]byte)
			for _, scope := range scopes {
				value, err := scope.read()
				if err != nil {
					t.Fatal("cannot snapshot role authority settings")
				}
				before[scope.name] = encode(value)
			}
			fileBefore, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal("cannot snapshot role authority deployment config")
			}
			configBefore, err := config.LoadAppConfig()
			if err != nil {
				t.Fatal("cannot decode deployment config snapshot")
			}
			assertUnchanged := func() {
				t.Helper()
				for _, scope := range scopes {
					value, err := scope.read()
					if err != nil || !bytes.Equal(before[scope.name], encode(value)) {
						t.Fatalf("role authority changed unexpected settings: %s", scope.name)
					}
				}
				fileAfter, err := os.ReadFile(configPath)
				if err != nil || !bytes.Equal(fileBefore, fileAfter) {
					t.Fatal("role authority changed unexpected deployment config")
				}
			}
			secret := "urgent-role-write-" + tc.name + "-secret"
			secrets = append(secrets, secret)
			providers := map[string]store.ProviderConfig{"role-write": {"type": "openai", "api_key": secret, "base_url": "https://example.invalid"}}
			payload := map[string]any{"providers": providers}
			if tc.name == "org-admin-settings" {
				var current store.TeamSettings
				if err := json.Unmarshal(before[tc.target], &current); err != nil {
					t.Fatal("cannot preserve general team settings")
				}
				payload["general"] = api.GeneralSettings{
					DefaultProvider: current.DefaultProvider, DefaultModel: current.DefaultModel,
					WebSearchTool: current.WebSearchTool, WebExtractTool: current.WebExtractTool,
					ContextLength: 12345,
				}
			}
			if tc.orgProviders {
				payload["default_provider"], payload["default_model"] = "role-write", "role-model"
			}
			if tc.fullConfig {
				payload = map[string]any{"agent_identity": map[string]string{"name": "urgent-independent-platform-admin"}}
			}
			// The same well-formed write is forbidden to an ordinary member.
			assertSecretFree(t, urgentStatus(t, bob.Get(t, "/api/settings/config"), http.StatusOK))
			assertSecretFree(t, urgentStatus(t, bob.Put(t, tc.endpoint, payload), http.StatusForbidden))
			assertUnchanged()
			if !tc.fullConfig {
				for _, endpoint := range []string{"/api/settings/platform/providers", "/api/settings/full"} {
					assertSecretFree(t, request(t, tc.token, tc.team, http.MethodPut, endpoint, payload, http.StatusForbidden))
					if endpoint != "/api/settings/full" {
						assertSecretFree(t, request(t, tc.token, tc.team, http.MethodGet, endpoint, nil, http.StatusForbidden))
					}
				}
				if tc.name == "team-admin-providers" {
					assertSecretFree(t, request(t, tc.token, tc.team, http.MethodGet, "/api/settings/org/providers", nil, http.StatusForbidden))
					assertSecretFree(t, request(t, tc.token, tc.team, http.MethodPut, "/api/settings/org/providers", payload, http.StatusForbidden))
					// A valid team administrator cannot select an unrelated sibling
					// team through the tenant header to acquire its write authority.
					assertSecretFree(t, request(t, tc.token, carol.TeamSlug, http.MethodGet, tc.endpoint, nil, http.StatusForbidden))
					assertSecretFree(t, request(t, tc.token, carol.TeamSlug, http.MethodPut, tc.endpoint, payload, http.StatusForbidden))
				}
				assertUnchanged()
			} else {
				for _, denied := range []struct{ token, team string }{{login.AccessToken, carol.TeamSlug}, {teamAdmin.Token(), teamAdmin.TeamSlug()}, {alice.Token(), alice.TeamSlug()}} {
					assertSecretFree(t, request(t, denied.token, denied.team, http.MethodGet, tc.endpoint, nil, http.StatusOK))
					assertSecretFree(t, request(t, denied.token, denied.team, http.MethodPut, tc.endpoint, payload, http.StatusForbidden))
					assertUnchanged()
				}
			}
			assertSecretFree(t, request(t, tc.token, tc.team, http.MethodGet, tc.endpoint, nil, http.StatusOK))
			assertSecretFree(t, request(t, tc.token, tc.team, http.MethodPut, tc.endpoint, payload, http.StatusOK))
			body := request(t, tc.token, tc.team, http.MethodGet, tc.endpoint, nil, http.StatusOK)
			assertSecretFree(t, body)
			if tc.fullConfig {
				var response api.FullConfigResponse
				if err := json.Unmarshal([]byte(body), &response); err != nil || response.AgentIdentity.Name != "urgent-independent-platform-admin" {
					t.Fatal("platform-admin full-config write was not visible over HTTP")
				}
				persisted, err := config.LoadAppConfig()
				configBefore.AgentIdentity = config.AgentIdentityConfig{Name: "urgent-independent-platform-admin"}
				if err != nil || !bytes.Equal(encode(configBefore), encode(persisted)) {
					t.Fatal("platform-admin full-config write changed fields outside the authorized identity update")
				}
				fileBefore, err = os.ReadFile(configPath)
				if err != nil {
					t.Fatal("cannot snapshot authorized deployment write")
				}
			} else if tc.orgProviders {
				want := store.OrgSettings{DefaultProvider: "role-write", DefaultModel: "role-model", Providers: providers}
				before[tc.target] = encode(&want)
				var response struct {
					Providers map[string]store.ProviderConfig `json:"providers"`
				}
				if err := json.Unmarshal([]byte(body), &response); err != nil || response.Providers["role-write"]["api_key"] != "****cret" {
					t.Fatal("org-admin provider write was not visible as a masked HTTP read")
				}
			} else {
				var want store.TeamSettings
				if err := json.Unmarshal(before[tc.target], &want); err != nil {
					t.Fatal("cannot decode expected team settings")
				}
				want.Providers["role-write"] = providers["role-write"]
				if tc.name == "org-admin-settings" {
					want.ContextLength = 12345
				}
				before[tc.target] = encode(&want)
				var response api.AppSettingsResponse
				if err := json.Unmarshal([]byte(body), &response); err != nil {
					t.Fatal("cannot decode authorized settings read")
				}
				if tc.name == "org-admin-settings" && response.General.ContextLength != 12345 {
					t.Fatal("org-admin general settings mutation was not visible over HTTP")
				}
				found := false
				for _, provider := range response.Providers {
					if provider.Name == "role-write" {
						found = provider.Type == "openai" && provider.Fields["api_key"] == "****cret" && provider.Fields["base_url"] == "https://example.invalid"
					}
				}
				if !found {
					t.Fatal("authorized team provider mutation was not visible as a masked HTTP read")
				}
			}
			assertUnchanged()
		})
	}
}

// TestE2E_SecurityUrgentPlatformProviderMasks uses real JWT middleware and HTTP
// with isolated canaries. Run separately on SQLite and PostgreSQL for acceptance;
// compilation alone does not prove authorization or persistence behavior.
// COVERS: CHAT-108
func TestE2E_SecurityUrgentPlatformProviderMasks(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	admin, teamAdmin := e2eboot.SeedProviderAuthority(t, h, seed)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	a, d := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserDaveEmail]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot snapshot platform provider settings")
		}
		return data
	}
	platform := h.PlatformBackend().PlatformSettings()
	original, err := platform.Get(ctx)
	if err != nil || original == nil {
		t.Fatal("cannot preserve platform settings")
	}
	// Preserve complete settings, including auth/channels/search and defaults.
	// Only fake provider canaries enter HTTP requests; never echo real API keys.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := platform.Save(cleanupCtx, original); err != nil {
			t.Error("cannot restore platform settings")
		}
	})
	type scopeSnapshot struct {
		name   string
		before []byte
		read   func() (any, error)
	}
	var scopes []scopeSnapshot
	for _, user := range []*e2eboot.SeededUser{a, d} {
		orgSettings := h.PlatformBackend().OrgSettings(user.OrgSlug)
		orgOriginal, err := orgSettings.Get(ctx)
		if err != nil || orgOriginal == nil {
			t.Fatal("cannot preserve lower-scope org settings")
		}
		org, err := h.PlatformBackend().ForOrg(user.OrgSlug)
		if err != nil {
			t.Fatal("cannot resolve lower-scope team")
		}
		teamSettings := org.ForTeam(user.TeamSlug).Settings()
		teamOriginal, err := teamSettings.Get(ctx)
		if err != nil || teamOriginal == nil {
			t.Fatal("cannot preserve lower-scope team settings")
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if err := orgSettings.Save(cleanupCtx, orgOriginal); err != nil {
				t.Error("cannot restore lower-scope org settings")
			}
			if err := teamSettings.Save(cleanupCtx, teamOriginal); err != nil {
				t.Error("cannot restore lower-scope team settings")
			}
		})
		orgCanary, teamCanary := *orgOriginal, *teamOriginal
		orgCanary.Providers = map[string]store.ProviderConfig{"org-only": {"type": "openai", "api_key": "urgent-org-only-secret"}}
		teamCanary.Providers = map[string]store.ProviderConfig{"team-only": {"type": "openai", "api_key": "urgent-team-only-secret"}}
		if err := orgSettings.Save(ctx, &orgCanary); err != nil {
			t.Fatal("cannot seed lower-scope org canary")
		}
		if err := teamSettings.Save(ctx, &teamCanary); err != nil {
			t.Fatal("cannot seed lower-scope team canary")
		}
		scopes = append(scopes,
			scopeSnapshot{user.OrgSlug + "/org", encode(&orgCanary), func() (any, error) { return orgSettings.Get(ctx) }},
			scopeSnapshot{user.OrgSlug + "/team", encode(&teamCanary), func() (any, error) { return teamSettings.Get(ctx) }},
		)
	}
	configPath, err := config.GetConfigPath()
	if err != nil {
		t.Fatal("cannot locate deployment config")
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("cannot snapshot deployment config")
	}
	const endpoint = "/api/settings/platform/providers"
	for _, tc := range []struct {
		name, providers string
		status          int
		client          *e2eboot.Client
	}{
		{"redacted-read", "", http.StatusOK, admin},
		{"org-owner-denied", `{"existing":{"api_key":"unauthorized-value"}}`, http.StatusForbidden, alice},
		{"team-admin-denied", `{"existing":{"api_key":"unauthorized-value"}}`, http.StatusForbidden, teamAdmin},
		{"member-denied", `{"existing":{"api_key":"unauthorized-value"}}`, http.StatusForbidden, bob},
		{"exact", `{"existing":{"api_key":"****cret"}}`, http.StatusOK, admin},
		{"stale", `{"existing":{"api_key":"****nope"}}`, http.StatusBadRequest, admin},
		{"unknown-instance", `{"new":{"api_key":"****cret"}}`, http.StatusBadRequest, admin},
		{"foreign-org-instance", `{"org-only":{"api_key":"****cret"}}`, http.StatusBadRequest, admin},
		{"foreign-team-instance", `{"team-only":{"api_key":"****cret"}}`, http.StatusBadRequest, admin},
		{"unknown-field", `{"existing":{"client_secret":"****cret"}}`, http.StatusBadRequest, admin},
		{"empty-stored", `{"empty":{"api_key":"****"}}`, http.StatusBadRequest, admin},
		{"mixed-invalid", `{"existing":{"api_key":"explicit-must-not-persist"},"new":{"api_key":"****cret"}}`, http.StatusBadRequest, admin},
		{"replacement", `{"existing":{"api_key":"urgent-new-secret"}}`, http.StatusOK, admin},
		{"omitted", "", http.StatusOK, admin},
		{"deleted", `{}`, http.StatusOK, admin},
		{"clear", `{"existing":{"api_key":""}}`, http.StatusOK, admin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := *original
			baseline.Providers = map[string]store.ProviderConfig{
				"existing": {"type": "openai", "api_key": "urgent-platform-secret", "base_url": "https://example.invalid"},
				"removed":  {"type": "openai", "api_key": "urgent-other-secret"},
				"empty":    {"type": "openai", "api_key": ""},
			}
			if err := platform.Save(ctx, &baseline); err != nil {
				t.Fatal("cannot seed platform provider canaries")
			}
			want := baseline
			var bodies []string
			if tc.name == "redacted-read" {
				body := urgentStatus(t, admin.Get(t, endpoint), http.StatusOK)
				bodies = append(bodies, body)
				var response struct {
					Providers map[string]store.ProviderConfig `json:"providers"`
				}
				if err := json.Unmarshal([]byte(body), &response); err != nil || response.Providers["existing"]["api_key"] != "****cret" || response.Providers["empty"]["api_key"] != "" || response.Providers["existing"]["base_url"] != "https://example.invalid" {
					t.Fatal("platform read did not mask secrets and preserve nonsecret fields")
				}
			} else {
				request := map[string]any{"default_provider": original.DefaultProvider, "default_model": original.DefaultModel}
				if tc.providers != "" {
					var providers map[string]store.ProviderConfig
					if err := json.Unmarshal([]byte(tc.providers), &providers); err != nil {
						t.Fatal("invalid platform provider test input")
					}
					request["providers"] = providers
				}
				if tc.status == http.StatusForbidden {
					bodies = append(bodies, urgentStatus(t, tc.client.Get(t, endpoint), http.StatusForbidden))
					// Prove an authenticated lower-scope read succeeds, so a blanket
					// auth failure cannot satisfy the platform-denial assertion.
					bodies = append(bodies, urgentStatus(t, tc.client.Get(t, "/api/settings/config"), http.StatusOK))
					if tc.client == teamAdmin {
						bodies = append(bodies, urgentStatus(t, teamAdmin.Put(t, "/api/settings/config", map[string]any{}), http.StatusOK))
						bodies = append(bodies, urgentStatus(t, teamAdmin.Get(t, "/api/settings/org/providers"), http.StatusForbidden))
						bodies = append(bodies, urgentStatus(t, teamAdmin.Put(t, "/api/settings/org/providers", request), http.StatusForbidden))
					}
					bodies = append(bodies, urgentStatus(t, tc.client.Put(t, "/api/settings/full", map[string]any{"agent_identity": map[string]string{"name": "unauthorized-value"}}), http.StatusForbidden))
				}
				bodies = append(bodies, urgentStatus(t, tc.client.Put(t, endpoint, request), tc.status))
				if tc.status == http.StatusOK && tc.name != "omitted" {
					want.Providers = map[string]store.ProviderConfig{}
					switch tc.name {
					case "exact":
						want.Providers["existing"] = store.ProviderConfig{"api_key": "urgent-platform-secret"}
					case "replacement":
						want.Providers["existing"] = store.ProviderConfig{"api_key": "urgent-new-secret"}
					case "clear":
						want.Providers["existing"] = store.ProviderConfig{"api_key": ""}
					}
				}
			}
			for _, body := range bodies {
				for _, secret := range []string{"urgent-platform-secret", "urgent-other-secret", "urgent-org-only-secret", "urgent-team-only-secret", "urgent-new-secret", "unauthorized-value", "explicit-must-not-persist"} {
					if strings.Contains(body, secret) {
						t.Fatal("provider response exposed a secret canary")
					}
				}
			}
			after, err := platform.Get(ctx)
			if err != nil || !bytes.Equal(encode(&want), encode(after)) {
				t.Fatal("platform operation violated exact persistence/rejection semantics")
			}
			for _, scope := range scopes {
				after, err := scope.read()
				if err != nil || !bytes.Equal(scope.before, encode(after)) {
					t.Fatalf("platform operation changed lower-scope settings: %s", scope.name)
				}
			}
			configAfter, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(configBefore, configAfter) {
				t.Fatal("provider operation changed deployment config")
			}
		})
	}
}

// COVERS: CHAT-105
func TestE2E_SecurityUrgentOrgProviderMasks(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	aliceUser, bobUser := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserBobEmail]
	daveUser := seed.Users[e2eboot.UserDaveEmail]
	if aliceUser.OrgSlug != bobUser.OrgSlug || aliceUser.OrgSlug == daveUser.OrgSlug || aliceUser.OrgRole != "owner" || bobUser.OrgRole != "member" {
		t.Fatal("provider authority fixtures are invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	orgSettings := h.PlatformBackend().OrgSettings(aliceUser.OrgSlug)
	foreignSettings := h.PlatformBackend().OrgSettings(daveUser.OrgSlug)
	original, err := orgSettings.Get(ctx)
	if err != nil || original == nil {
		t.Fatal("cannot preserve org settings")
	}
	foreignOriginal, err := foreignSettings.Get(ctx)
	if err != nil || foreignOriginal == nil {
		t.Fatal("cannot preserve foreign settings")
	}
	// Restore before harness teardown, even if a subcase fails. Never use operator
	// credentials for canaries or change the harness's working provider defaults.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := orgSettings.Save(cleanupCtx, original); err != nil {
			t.Error("cannot restore org settings")
		}
		if err := foreignSettings.Save(cleanupCtx, foreignOriginal); err != nil {
			t.Error("cannot restore foreign settings")
		}
	})
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot snapshot provider settings")
		}
		return data
	}
	platformBefore, err := h.PlatformBackend().PlatformSettings().Get(ctx)
	if err != nil || platformBefore == nil {
		t.Fatal("cannot snapshot platform settings")
	}
	platformJSON := encode(platformBefore)
	foreign := *foreignOriginal
	foreign.Providers = map[string]store.ProviderConfig{"foreign": {"type": "openai", "api_key": "urgent-foreign-secret"}}
	if err := foreignSettings.Save(ctx, &foreign); err != nil {
		t.Fatal("cannot seed foreign provider canary")
	}
	foreignJSON := encode(&foreign)
	const endpoint = "/api/settings/org/providers"
	for _, tc := range []struct {
		name, providers string
		status          int
	}{
		{"redacted-read", "", http.StatusOK},
		{"member-denied", `{"existing":{"api_key":"unauthorized-value"}}`, http.StatusForbidden},
		{"exact", `{"existing":{"api_key":"****cret"}}`, http.StatusOK},
		{"stale", `{"existing":{"api_key":"****nope"}}`, http.StatusBadRequest},
		{"unknown-instance", `{"new":{"api_key":"****cret"}}`, http.StatusBadRequest},
		{"foreign-instance", `{"foreign":{"api_key":"****cret"}}`, http.StatusBadRequest},
		{"unknown-field", `{"existing":{"client_secret":"****cret"}}`, http.StatusBadRequest},
		{"empty-stored", `{"empty":{"api_key":"****"}}`, http.StatusBadRequest},
		{"mixed-invalid", `{"existing":{"api_key":"explicit-must-not-persist"},"new":{"api_key":"****cret"}}`, http.StatusBadRequest},
		{"replacement", `{"existing":{"api_key":"urgent-new-secret"}}`, http.StatusOK},
		{"omitted", "", http.StatusOK},
		{"deleted", `{}`, http.StatusOK},
		{"clear", `{"existing":{"api_key":""}}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := *original
			baseline.Providers = map[string]store.ProviderConfig{
				"existing": {"type": "openai", "api_key": "urgent-original-secret", "base_url": "https://example.invalid"},
				"removed":  {"type": "openai", "api_key": "urgent-other-secret"},
				"empty":    {"type": "openai", "api_key": ""},
			}
			if err := orgSettings.Save(ctx, &baseline); err != nil {
				t.Fatal("cannot seed provider mask fixture")
			}
			before := encode(&baseline)
			var body string
			if tc.name == "redacted-read" {
				body = urgentStatus(t, alice.Get(t, endpoint), http.StatusOK)
				var response struct {
					Providers map[string]store.ProviderConfig `json:"providers"`
				}
				if err := json.Unmarshal([]byte(body), &response); err != nil || response.Providers["existing"]["api_key"] != "****cret" || response.Providers["empty"]["api_key"] != "" || response.Providers["existing"]["base_url"] != "https://example.invalid" {
					t.Fatal("org read did not return masked secrets and intact nonsecret fields")
				}
				urgentStatus(t, bob.Get(t, endpoint), http.StatusForbidden)
			} else {
				request := map[string]any{"default_provider": original.DefaultProvider, "default_model": original.DefaultModel}
				if tc.providers != "" {
					var providers map[string]store.ProviderConfig
					if err := json.Unmarshal([]byte(tc.providers), &providers); err != nil {
						t.Fatal("invalid provider test input")
					}
					request["providers"] = providers
				}
				client := alice
				if tc.name == "member-denied" {
					client = bob
				}
				body = urgentStatus(t, client.Put(t, endpoint, request), tc.status)
			}
			for _, secret := range []string{"urgent-original-secret", "urgent-other-secret", "urgent-foreign-secret", "urgent-new-secret", "explicit-must-not-persist", "unauthorized-value"} {
				if strings.Contains(body, secret) {
					t.Fatal("provider response exposed a secret canary")
				}
			}
			after, err := orgSettings.Get(ctx)
			if err != nil || after == nil {
				t.Fatal("cannot inspect persisted org settings")
			}
			if tc.status != http.StatusOK || tc.name == "redacted-read" || tc.name == "omitted" {
				if !bytes.Equal(before, encode(after)) {
					t.Fatal("rejected request, read or omitted providers mutated settings")
				}
			} else {
				want := map[string]store.ProviderConfig{}
				switch tc.name {
				case "exact":
					want["existing"] = store.ProviderConfig{"api_key": "urgent-original-secret"}
				case "replacement":
					want["existing"] = store.ProviderConfig{"api_key": "urgent-new-secret"}
				case "clear":
					want["existing"] = store.ProviderConfig{"api_key": ""}
				}
				if len(after.Providers) != len(want) || (len(want) > 0 && !bytes.Equal(encode(after.Providers), encode(want))) || after.DefaultProvider != original.DefaultProvider || after.DefaultModel != original.DefaultModel {
					t.Fatal("authorized replacement did not persist exact provider/default semantics")
				}
			}
			foreignAfter, err := foreignSettings.Get(ctx)
			if err != nil || !bytes.Equal(foreignJSON, encode(foreignAfter)) {
				t.Fatal("org operation changed another organization's settings")
			}
			platformAfter, err := h.PlatformBackend().PlatformSettings().Get(ctx)
			if err != nil || !bytes.Equal(platformJSON, encode(platformAfter)) {
				t.Fatal("org operation changed platform settings")
			}
		})
	}
}

// COVERS: CHAT-107
func TestE2E_SecurityUrgentTeamProviderMasks(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	a, b, d := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserBobEmail], seed.Users[e2eboot.UserDaveEmail]
	if a.OrgSlug != b.OrgSlug || a.TeamSlug != b.TeamSlug || a.TeamRole != "admin" || b.TeamRole != "member" || a.OrgSlug == d.OrgSlug {
		t.Fatal("team provider authority fixtures are invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	org, err := h.Store.ForOrg(a.OrgSlug)
	if err != nil {
		t.Fatal("cannot resolve team settings organization")
	}
	foreignOrg, err := h.Store.ForOrg(d.OrgSlug)
	if err != nil {
		t.Fatal("cannot resolve foreign organization")
	}
	team := org.ForTeam(a.TeamSlug).Settings()
	foreignTeam := foreignOrg.ForTeam(d.TeamSlug).Settings()
	orgSettings := h.PlatformBackend().OrgSettings(a.OrgSlug)
	original, err := team.Get(ctx)
	if err != nil || original == nil {
		t.Fatal("cannot preserve team settings")
	}
	orgOriginal, err := orgSettings.Get(ctx)
	if err != nil || orgOriginal == nil {
		t.Fatal("cannot preserve org settings")
	}
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot snapshot settings")
		}
		return data
	}
	clone := func(value *store.TeamSettings) *store.TeamSettings {
		t.Helper()
		var result store.TeamSettings
		if err := json.Unmarshal(encode(value), &result); err != nil {
			t.Fatal("cannot copy team settings")
		}
		return &result
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := team.Save(cleanupCtx, original); err != nil {
			t.Error("cannot restore team settings")
		}
		if err := orgSettings.Save(cleanupCtx, orgOriginal); err != nil {
			t.Error("cannot restore org settings")
		}
	})
	inherited := *orgOriginal
	inherited.Providers = map[string]store.ProviderConfig{"inherited": {"type": "openai", "api_key": "urgent-inherited-secret"}}
	if err := orgSettings.Save(ctx, &inherited); err != nil {
		t.Fatal("cannot seed inherited canary")
	}
	foreignBefore, err := foreignTeam.Get(ctx)
	if err != nil {
		t.Fatal("cannot snapshot foreign team")
	}
	platformBefore, err := h.PlatformBackend().PlatformSettings().Get(ctx)
	if err != nil || platformBefore == nil {
		t.Fatal("cannot snapshot platform")
	}
	foreignJSON, platformJSON, orgJSON := encode(foreignBefore), encode(platformBefore), encode(&inherited)
	const endpoint = "/api/settings/config"
	for _, mode := range []string{"patch", "replace"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name, providers string
				status          int
			}{
				{"redacted-read", "", http.StatusOK},
				{"member-denied", `{"existing":{"api_key":"unauthorized-value"}}`, http.StatusForbidden},
				{"exact", `{"existing":{"api_key":"****cret"}}`, http.StatusOK},
				{"stale", `{"existing":{"api_key":"****nope"}}`, http.StatusBadRequest},
				{"unknown-instance", `{"new":{"api_key":"****cret"}}`, http.StatusBadRequest},
				{"inherited-instance", `{"inherited":{"api_key":"****cret"}}`, http.StatusBadRequest},
				{"unknown-field", `{"existing":{"client_secret":"****cret"}}`, http.StatusBadRequest},
				{"empty-stored", `{"empty":{"api_key":"****"}}`, http.StatusBadRequest},
				{"mixed-invalid", `{"existing":{"api_key":"explicit-must-not-persist"},"new":{"api_key":"****cret"}}`, http.StatusBadRequest},
				{"replacement", `{"existing":{"api_key":"urgent-new-secret"}}`, http.StatusOK},
				{"omitted", "", http.StatusOK},
				{"empty", `{}`, http.StatusOK},
				{"clear", `{"existing":{"api_key":""}}`, http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					baseline := clone(original)
					baseline.Providers = map[string]store.ProviderConfig{
						"existing": {"type": "openai", "api_key": "urgent-team-secret", "base_url": "https://example.invalid"},
						"removed":  {"type": "openai", "api_key": "urgent-other-secret"},
						"empty":    {"type": "openai", "api_key": ""},
					}
					if err := team.Save(ctx, baseline); err != nil {
						t.Fatal("cannot seed team provider fixture")
					}
					want := clone(baseline)
					var body string
					if tc.name == "redacted-read" {
						body = urgentStatus(t, alice.Get(t, endpoint), http.StatusOK)
						if !strings.Contains(body, "****cret") || !strings.Contains(body, "https://example.invalid") {
							t.Fatal("team read lost masked or nonsecret fields")
						}
					} else {
						request := map[string]any{}
						if tc.providers != "" {
							var providers map[string]map[string]string
							if err := json.Unmarshal([]byte(tc.providers), &providers); err != nil {
								t.Fatal("invalid team provider input")
							}
							request["providers"] = providers
							if mode == "replace" {
								entries := make([]map[string]string, 0, len(providers))
								for name, fields := range providers {
									fields["name"] = name
									entries = append(entries, fields)
								}
								request["providers"] = map[string]map[string]string{"__replace_all__": {"__array__": string(encode(entries))}}
							}
						}
						client := alice
						if tc.name == "member-denied" {
							client = bob
						}
						body = urgentStatus(t, client.Put(t, endpoint, request), tc.status)
						if tc.status == http.StatusOK && tc.providers != "" {
							if mode == "replace" {
								want.Providers = map[string]store.ProviderConfig{}
							}
							if tc.name != "empty" {
								value := ""
								if tc.name == "exact" {
									value = "urgent-team-secret"
								} else if tc.name == "replacement" {
									value = "urgent-new-secret"
								}
								if mode == "replace" {
									want.Providers["existing"] = store.ProviderConfig{}
								}
								want.Providers["existing"]["api_key"] = value
							}
						}
					}
					for _, secret := range []string{"urgent-team-secret", "urgent-other-secret", "urgent-inherited-secret", "urgent-new-secret", "unauthorized-value", "explicit-must-not-persist"} {
						if strings.Contains(body, secret) {
							t.Fatal("team response exposed a secret canary")
						}
					}
					after, err := team.Get(ctx)
					if err != nil || !bytes.Equal(encode(want), encode(after)) {
						t.Fatal("team update did not preserve exact patch/replacement or rejection semantics")
					}
					orgAfter, err := orgSettings.Get(ctx)
					if err != nil || !bytes.Equal(orgJSON, encode(orgAfter)) {
						t.Fatal("team update changed inherited org settings")
					}
					foreignAfter, err := foreignTeam.Get(ctx)
					if err != nil || !bytes.Equal(foreignJSON, encode(foreignAfter)) {
						t.Fatal("team update changed foreign team settings")
					}
					platformAfter, err := h.PlatformBackend().PlatformSettings().Get(ctx)
					if err != nil || !bytes.Equal(platformJSON, encode(platformAfter)) {
						t.Fatal("team update changed platform settings")
					}
				})
			}
		})
	}
}

// TestE2E_SecurityUrgentConfiguredInit runs against the backend selected by the
// existing harness. Acceptance requires separate SQLite and PostgreSQL runs;
// neither a skipped bootstrap nor a 403/5xx response proves the configured guard.
// COVERS: CHAT-101
func TestE2E_SecurityUrgentConfiguredInit(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	// Never inherit an operator bootstrap capability for a rejection test.
	t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", "")
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatal("cannot read configured platform")
	}
	if h.IsSQLite() != cfg.Storage.IsSQLite() || (!cfg.Storage.IsSQLite() && !cfg.Storage.IsPostgres()) {
		t.Fatal("configured backend does not match the live harness")
	}
	if cfg.Storage.Auth.JWTSecret == "" {
		t.Fatal("configured platform has no JWT secret")
	}
	configPath, err := config.GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("cannot snapshot configured platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	settingsSnapshot := func(t *testing.T) []byte {
		t.Helper()
		settings, err := h.PlatformBackend().PlatformSettings().Get(ctx)
		if err != nil || settings == nil {
			t.Fatal("cannot read persisted platform settings")
		}
		data, err := json.Marshal(settings)
		if err != nil {
			t.Fatal("cannot snapshot persisted platform settings")
		}
		return data
	}
	beforeSettings := settingsSnapshot(t)
	beforeOrgs, err := h.PlatformBackend().Organizations().Count(ctx)
	if err != nil || beforeOrgs == 0 {
		t.Fatal("configured organization fixture missing")
	}
	rejectedDataDir := filepath.Join(t.TempDir(), "rejected-init")
	for _, endpoint := range []struct {
		name, path string
		body       any
	}{
		{"postgres", "/api/platform/init", api.PlatformInitRequest{
			// Deliberately no usable DB credentials or remote destination.
			Host: "127.0.0.1", Port: 1, User: "urgent-rejected", Password: "not-a-real-password",
			OrgName: "Rejected Init", OrgSlug: "urgent-rejected-init",
		}},
		{"sqlite", "/api/platform/init/sqlite", api.SQLitePlatformInitRequest{
			DataDir: rejectedDataDir, OrgName: "Rejected Init", OrgSlug: "urgent-rejected-init",
			AuthMode: config.AuthModeBuiltin, AdminEmail: "urgent-rejected-init@example.invalid",
			AdminPassword: "NotARealPassword123!",
		}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, email := range []string{e2eboot.UserAliceEmail, e2eboot.UserBobEmail} {
				t.Run(email, func(t *testing.T) {
					client := seed.Client(email)
					beforeRead := urgentStatus(t, client.Get(t, "/api/settings/full"), http.StatusOK)
					body := urgentStatus(t, client.Post(t, endpoint.path, endpoint.body), http.StatusConflict)
					var result api.PlatformInitResponse
					if err := json.Unmarshal([]byte(body), &result); err != nil || result.Success || result.RestartRequired || result.Error == "" {
						t.Fatal("configured init did not return an explicit rejection")
					}
					afterConfig, err := os.ReadFile(configPath)
					if err != nil || !bytes.Equal(beforeConfig, afterConfig) {
						t.Fatal("rejected init changed config (including backend/JWT)")
					}
					if !bytes.Equal(beforeSettings, settingsSnapshot(t)) {
						t.Fatal("rejected init changed persisted platform settings")
					}
					afterOrgs, err := h.PlatformBackend().Organizations().Count(ctx)
					if err != nil || afterOrgs != beforeOrgs {
						t.Fatal("rejected init changed organization count")
					}
					if _, err := os.Stat(rejectedDataDir); !os.IsNotExist(err) {
						t.Fatal("rejected init created a SQLite destination or its absence could not be verified")
					}
					if urgentStatus(t, client.Get(t, "/api/settings/full"), http.StatusOK) != beforeRead {
						t.Fatal("authenticated config read changed after rejected init")
					}
				})
			}
		})
	}
}

// TestE2E_SecurityUrgentSameTeamOwnerDenied covers the role direction absent
// from SessionOwnership: Alice's org-owner/team-admin role must not grant access
// to Bob's personal session. Bob must still be able to resume and delete it.
// COVERS: CHAT-102
func TestE2E_SecurityUrgentSameTeamOwnerDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	aliceUser, bobUser := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserBobEmail]
	if aliceUser.ID == bobUser.ID || aliceUser.OrgSlug != bobUser.OrgSlug || aliceUser.TeamSlug != bobUser.TeamSlug || aliceUser.OrgRole != "owner" || aliceUser.TeamRole != "admin" {
		t.Fatal("same-team org-owner fixture is invalid")
	}
	const marker = "urgent-member-owned-session-canary"
	events := bob.SSE(t, "/api/studio/chat", map[string]any{"message": "Reply with exactly " + marker}, 120*time.Second)
	urgentCompletedChat(t, events)
	id := e2eboot.ExtractSessionIDFromSSE(t, events)
	if id == "" {
		t.Fatal("member's session missing")
	}
	path := "/api/studio/sessions/" + id
	before := urgentStatus(t, bob.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) || !strings.Contains(urgentStatus(t, bob.Get(t, "/api/studio/sessions"), http.StatusOK), id) {
		t.Fatal("session owner cannot read/list the fixture")
	}
	urgentStatus(t, alice.Get(t, path), http.StatusNotFound, http.StatusForbidden)
	if strings.Contains(urgentStatus(t, alice.Get(t, "/api/studio/sessions"), http.StatusOK), id) {
		t.Fatal("org owner can list another same-team user's personal session")
	}
	urgentStatus(t, alice.Delete(t, path), http.StatusNotFound, http.StatusForbidden, http.StatusNoContent)
	if urgentStatus(t, bob.Get(t, path), http.StatusOK) != before {
		t.Fatal("org-owner delete mutated member's session")
	}
	urgentStatus(t, alice.Post(t, "/api/studio/chat", map[string]any{
		"sessionId": id, "message": "urgent-owner-unauthorized-injection",
	}), http.StatusNotFound, http.StatusForbidden)
	if urgentStatus(t, bob.Get(t, path), http.StatusOK) != before {
		t.Fatal("org-owner injection mutated member's session")
	}
	const resumedMarker = "urgent-authorized-owner-resume-canary"
	events = bob.SSE(t, "/api/studio/chat", map[string]any{
		"sessionId": id, "message": "Reply with exactly " + resumedMarker,
	}, 120*time.Second)
	urgentCompletedChat(t, events)
	if e2eboot.ExtractSessionIDFromSSE(t, events) != id {
		t.Fatal("authorized resume did not retain the session ID")
	}
	after := urgentStatus(t, bob.Get(t, path), http.StatusOK)
	if !strings.Contains(after, marker) || !strings.Contains(after, resumedMarker) || strings.Contains(after, "urgent-owner-unauthorized-injection") {
		t.Fatal("owner resume lost history or retained unauthorized content")
	}
	urgentStatus(t, bob.Delete(t, path), http.StatusOK, http.StatusNoContent)
	urgentStatus(t, bob.Get(t, path), http.StatusNotFound)
	if strings.Contains(urgentStatus(t, bob.Get(t, "/api/studio/sessions"), http.StatusOK), id) {
		t.Fatal("deleted session remains in owner's list")
	}
}

// COVERS: CHAT-106
func TestE2E_SecurityUrgentSameSlugCrossOrgSessions(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	clients := e2eboot.SeedSameSlugTeams(t, h, seed)
	emails := []string{e2eboot.UserAliceEmail, e2eboot.UserDaveEmail}
	ids, snapshots, markers := make(map[string]string), make(map[string]string), make(map[string]string)
	if clients[emails[0]].TeamSlug() != clients[emails[1]].TeamSlug() {
		t.Fatal("collision clients must send the same team slug")
	}
	for _, email := range emails {
		client := clients[email]
		marker := "urgent-private-" + uuid.NewString()
		events := client.SSE(t, "/api/studio/chat", map[string]any{"message": "Reply with exactly " + marker}, 120*time.Second)
		urgentCompletedChat(t, events)
		id := e2eboot.ExtractSessionIDFromSSE(t, events)
		if id == "" || id == ids[emails[0]] {
			t.Fatal("same-slug teams did not create distinct sessions")
		}
		ids[email], markers[email] = id, marker
		snapshots[email] = urgentStatus(t, client.Get(t, "/api/studio/sessions/"+id), http.StatusOK)
		if !strings.Contains(snapshots[email], marker) {
			t.Fatal("owner's persisted session lacks its private marker")
		}
	}
	for index, email := range emails {
		t.Run(email, func(t *testing.T) {
			owner, otherEmail := clients[email], emails[1-index]
			other, id := clients[otherEmail], ids[email]
			path := "/api/studio/sessions/" + id
			assertUnchanged := func(t *testing.T) {
				t.Helper()
				for _, actor := range emails {
					if urgentStatus(t, clients[actor].Get(t, "/api/studio/sessions/"+ids[actor]), http.StatusOK) != snapshots[actor] {
						t.Fatal("cross-org request changed persisted session content")
					}
				}
			}
			t.Run("read-list-denied", func(t *testing.T) {
				body := urgentStatus(t, other.Get(t, path), http.StatusNotFound, http.StatusForbidden)
				list := urgentStatus(t, other.Get(t, "/api/studio/sessions"), http.StatusOK)
				if strings.Contains(body+list, markers[email]) || strings.Contains(list, id) || !strings.Contains(list, ids[otherEmail]) {
					t.Fatal("same-slug session list leaked another org or omitted the owner's session")
				}
				assertUnchanged(t)
			})
			t.Run("delete-denied", func(t *testing.T) {
				urgentStatus(t, other.Delete(t, path), http.StatusNotFound, http.StatusForbidden, http.StatusNoContent)
				assertUnchanged(t)
			})
			t.Run("injection-denied", func(t *testing.T) {
				urgentStatus(t, other.Post(t, "/api/studio/chat", map[string]any{
					"sessionId": id, "message": "urgent-cross-org-injection",
				}), http.StatusNotFound, http.StatusForbidden)
				assertUnchanged(t)
			})
			t.Run("owner-resume", func(t *testing.T) {
				marker := "urgent-resumed-" + uuid.NewString()
				events := owner.SSE(t, "/api/studio/chat", map[string]any{
					"sessionId": id, "message": "Reply with exactly " + marker,
				}, 120*time.Second)
				urgentCompletedChat(t, events)
				if e2eboot.ExtractSessionIDFromSSE(t, events) != id {
					t.Fatal("owner resume changed session identity")
				}
				after := urgentStatus(t, owner.Get(t, path), http.StatusOK)
				if !strings.Contains(after, marker) || !strings.Contains(after, markers[email]) || strings.Contains(after, markers[otherEmail]) || strings.Contains(after, "urgent-cross-org-injection") {
					t.Fatal("owner resume lost private history or acquired foreign content")
				}
				snapshots[email] = after
				assertUnchanged(t)
			})
		})
	}
	for _, email := range emails {
		t.Run("owner-delete/"+email, func(t *testing.T) {
			urgentStatus(t, clients[email].Delete(t, "/api/studio/sessions/"+ids[email]), http.StatusOK, http.StatusNoContent)
			urgentStatus(t, clients[email].Get(t, "/api/studio/sessions/"+ids[email]), http.StatusNotFound)
			if strings.Contains(urgentStatus(t, clients[email].Get(t, "/api/studio/sessions"), http.StatusOK), ids[email]) {
				t.Fatal("deleted session remains in owner's list")
			}
			for _, remaining := range emails {
				if remaining != email && snapshots[remaining] != "" && urgentStatus(t, clients[remaining].Get(t, "/api/studio/sessions/"+ids[remaining]), http.StatusOK) != snapshots[remaining] {
					t.Fatal("owner delete changed another organization's session")
				}
			}
			snapshots[email] = ""
		})
	}
}

// A session event alone is not positive authorization proof: provider or
// infrastructure failures may emit it before terminating the chat.
func urgentCompletedChat(t *testing.T, events []e2eboot.SSEEvent) {
	t.Helper()
	if e2eboot.FindEvent(events, "error") != nil || e2eboot.FindEvent(events, "done") == nil || len(e2eboot.FindAllEvents(events, "text")) == 0 {
		t.Fatal("authorized chat did not complete with a provider response")
	}
}

// urgentStatus deliberately rejects infrastructure errors as authorization proof.
func urgentStatus(t *testing.T, resp *http.Response, allowed ...int) string {
	t.Helper()
	defer resp.Body.Close()
	body := e2eboot.ReadBody(t, resp)
	for _, status := range allowed {
		if resp.StatusCode == status {
			return body
		}
	}
	// Avoid printing potentially sensitive response bodies on a regression.
	t.Fatalf("%s %s: status=%d, want=%v", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, allowed)
	return ""
}

func TestE2E_SecurityUrgentSessionOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	const marker = "urgent-session-owner-canary"
	events := alice.SSE(t, "/api/studio/chat", map[string]any{"message": "Reply with exactly " + marker}, 120*time.Second)
	id := e2eboot.ExtractSessionIDFromSSE(t, events)
	if id == "" {
		t.Fatal("owner session missing")
	}
	path := "/api/studio/sessions/" + id
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) {
		t.Fatal("owner session content missing")
	}
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserCarolEmail, e2eboot.UserEveEmail} {
		t.Run(email, func(t *testing.T) {
			other := seed.Client(email)
			urgentStatus(t, other.Get(t, path), http.StatusForbidden, http.StatusNotFound)
			list := urgentStatus(t, other.Get(t, "/api/studio/sessions"), http.StatusOK)
			if strings.Contains(list, id) {
				t.Fatal("foreign session leaked in list")
			}
			// Deletion is idempotent and deliberately hides inaccessible sessions.
			urgentStatus(t, other.Delete(t, path), http.StatusForbidden, http.StatusNotFound, http.StatusNoContent)
			urgentStatus(t, other.Post(t, "/api/studio/chat", map[string]any{"sessionId": id, "message": "unauthorized-injection"}), http.StatusForbidden, http.StatusNotFound)
			after := urgentStatus(t, alice.Get(t, path), http.StatusOK)
			if after != before {
				t.Fatal("denied request mutated owner's session")
			}
		})
	}
	urgentStatus(t, alice.Delete(t, path), http.StatusOK, http.StatusNoContent)
	urgentStatus(t, alice.Get(t, path), http.StatusNotFound)
}

func TestE2E_SecurityUrgentArtifactOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	const marker = "urgent-artifact-owner-canary"
	events := alice.SSE(t, "/api/studio/chat", map[string]any{
		"message": `Use write_file to write exactly "` + marker + `" to /tmp/urgent-artifact.md. Do not do anything else.`, "autoApprove": true,
	}, 180*time.Second)
	id := e2eboot.ExtractSessionIDFromSSE(t, events)
	artifacts := e2eboot.FindAllEvents(events, "artifact")
	if id == "" || len(artifacts) == 0 {
		t.Fatal("required real sandbox artifact was not produced; acceptance cannot pass")
	}
	var artifact struct {
		Path string `json:"path"`
	}
	e2eboot.DecodeEventData(t, &artifacts[0], &artifact)
	if artifact.Path == "" {
		t.Fatal("empty artifact path")
	}
	path := "/api/studio/artifacts/content?" + url.Values{"path": {artifact.Path}, "session": {id}}.Encode()
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) {
		t.Fatal("owner artifact download did not contain fixture")
	}
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserCarolEmail, e2eboot.UserEveEmail} {
		urgentStatus(t, seed.Client(email).Get(t, path), http.StatusForbidden, http.StatusNotFound)
	}
	urgentStatus(t, alice.Get(t, "/api/studio/artifacts/content?"+url.Values{"path": {artifact.Path}}.Encode()), http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound)
	if urgentStatus(t, alice.Get(t, path), http.StatusOK) != before {
		t.Fatal("artifact changed after denials")
	}
	urgentStatus(t, alice.Delete(t, "/api/studio/sessions/"+id), http.StatusOK, http.StatusNoContent)
}

func TestE2E_SecurityUrgentMCPAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	path := "/api/mcp-platform/servers/" + e2eboot.MCPAcmeRedTeam + "?scope=team"
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	urgentStatus(t, bob.Delete(t, path), http.StatusForbidden)
	if urgentStatus(t, alice.Get(t, path), http.StatusOK) != before {
		t.Fatal("member delete mutated team MCP server")
	}
	// Org owner must not mutate deployment-wide MCP through modern or legacy routes.
	for _, client := range []*e2eboot.Client{alice, bob} {
		urgentStatus(t, client.Post(t, "/api/mcp-platform/servers?scope=platform", map[string]any{}), http.StatusForbidden)
		urgentStatus(t, client.Put(t, "/api/settings/mcp?scope=platform", map[string]any{}), http.StatusForbidden)
		urgentStatus(t, client.Post(t, "/api/mcp/install-inline?scope=platform", map[string]any{}), http.StatusForbidden)
	}
	urgentStatus(t, alice.Delete(t, "/api/mcp-platform/servers/"+e2eboot.MCPAcmeRedTeam+"?scope=effective"), http.StatusBadRequest)
	urgentStatus(t, alice.Delete(t, path), http.StatusOK)
	urgentStatus(t, alice.Get(t, path), http.StatusNotFound)
	// The same operation is legal for the owner in org scope, and leaves another org untouched.
	urgentStatus(t, alice.Delete(t, "/api/mcp-platform/servers/"+e2eboot.MCPAcmeOrg+"?scope=org"), http.StatusOK)
	urgentStatus(t, seed.Client(e2eboot.UserDaveEmail).Get(t, "/api/mcp-platform/servers/"+e2eboot.MCPGlobexOrg+"?scope=org"), http.StatusOK)
}

func TestE2E_SecurityUrgentMemoryAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	before := urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK)
	var listed struct {
		Results []struct {
			ID      string `json:"id"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(before), &listed); err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, entry := range listed.Results {
		if strings.Contains(entry.Snippet, e2eboot.MemAcmeRedTeam) {
			id = entry.ID
		}
	}
	if id == "" {
		t.Fatal("owner's seeded team memory missing")
	}
	request := api.MemoryConsolidationApplyRequest{
		Card: memory.ScenarioCard{CanonicalKey: "urgent-console-access", Title: "Urgent Console Access", Scope: "team", SourceMemoryIDs: []string{id}, RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}, TargetScope: "team",
	}
	urgentStatus(t, bob.Post(t, "/api/memories/consolidate/apply", request), http.StatusForbidden)
	if urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK) != before {
		t.Fatal("denied consolidation changed source or destination")
	}
	urgentStatus(t, alice.Post(t, "/api/memories/consolidate/apply", request), http.StatusOK)
	after := urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK)
	if strings.Contains(after, e2eboot.MemAcmeRedTeam) || !strings.Contains(after, "Urgent Console Access") {
		t.Fatal("authorized consolidation failed to replace source with card")
	}
	// Positive personal reads and same-team/cross-org search denials use existing fixtures.
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserEveEmail} {
		body := urgentStatus(t, seed.Client(email).Post(t, "/api/memories/search", map[string]string{"query": e2eboot.MemAlicePersonal}), http.StatusOK)
		if strings.Contains(body, e2eboot.MemAlicePersonal) {
			t.Fatal("foreign personal memory leaked")
		}
	}
	if !strings.Contains(urgentStatus(t, alice.Get(t, "/api/memories/personal"), http.StatusOK), e2eboot.MemAlicePersonal) {
		t.Fatal("owner personal read failed")
	}
}

// COVERS: CHAT-104
func TestE2E_SecurityUrgentMemoryParticipants(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	bob := seed.Client(e2eboot.UserBobEmail)
	aliceUser, bobUser := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserBobEmail]
	if aliceUser.OrgSlug != bobUser.OrgSlug || aliceUser.TeamSlug != bobUser.TeamSlug || bobUser.TeamRole != "member" {
		t.Fatal("same-team non-admin fixture missing")
	}
	org, err := h.Store.ForOrg(bobUser.OrgSlug)
	if err != nil {
		t.Fatal("cannot resolve fixture organization")
	}
	mem := org.ForTeam(bobUser.TeamSlug).Memories()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Index snapshots by ID so database ordering cannot conceal or invent a change.
	snapshot := func(t *testing.T) map[string]store.MemorySearchResult {
		t.Helper()
		rows, err := mem.List(ctx, "", 1000, 0)
		if err != nil || len(rows) >= 1000 {
			t.Fatal("cannot obtain complete persisted memory snapshot")
		}
		result := make(map[string]store.MemorySearchResult, len(rows))
		for _, row := range rows {
			result[row.ID] = row
		}
		return result
	}
	add := func(t *testing.T, content, category, creator string) string {
		t.Helper()
		if err := mem.Add(ctx, store.MemoryEntry{Content: content, Category: category, CreatedBy: creator}); err != nil {
			t.Fatal("cannot seed consolidation participant")
		}
		for id, row := range snapshot(t) {
			if row.Snippet == content && row.CreatedBy == creator {
				return id
			}
		}
		t.Fatal("seeded participant not persisted with expected content and owner")
		return ""
	}
	for _, participant := range []string{"source", "duplicate", "discard", "destination", "explicit-destination"} {
		t.Run(participant, func(t *testing.T) {
			for _, authorized := range []bool{false, true} {
				name, creator := "denied", aliceUser.ID
				if authorized {
					name, creator = "allowed", bobUser.ID
				}
				t.Run(name, func(t *testing.T) {
					key := "urgent-" + uuid.NewString()
					card := memory.ScenarioCard{
						CanonicalKey: key, Title: key, Scope: "team",
						RecommendedRecipe: []string{"Use the noVNC ticket endpoint for " + key + "."},
					}
					// Always include an authorized source before the potentially forbidden
					// participant: rejection must not partially delete this source either.
					ownSource := add(t, "authorized raw source "+key, "urgent_raw", bobUser.ID)
					card.SourceMemoryIDs = []string{ownSource}
					content, category := "participant raw source "+key, "urgent_raw"
					if participant != "source" && participant != "discard" {
						storedCard := card
						storedCard.SourceMemoryIDs = nil
						if participant == "duplicate" {
							storedCard.CanonicalKey = "duplicate-" + uuid.NewString()
							storedCard.Title = storedCard.CanonicalKey
							storedCard.RecommendedRecipe = []string{"Run pg_dump for " + storedCard.CanonicalKey + "."}
						}
						content, category = memory.RenderScenarioCard(storedCard), memory.ScenarioCardCategory
					}
					id := add(t, content, category, creator)
					req := api.MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"}
					switch participant {
					case "source", "discard":
						req.Card.SourceMemoryIDs = append(req.Card.SourceMemoryIDs, id)
					case "duplicate", "explicit-destination":
						req.DuplicateCardIDs = []string{id}
					}
					if participant == "discard" {
						req.Card.RecommendedRecipe = []string{memory.ScenarioCardPlaceholderRecipe}
					}
					before := snapshot(t)
					status := http.StatusForbidden
					if authorized {
						status = http.StatusOK
					}
					body := urgentStatus(t, bob.Post(t, "/api/memories/consolidate/apply", req), status)
					after := snapshot(t)
					if !authorized {
						beforeJSON, err1 := json.Marshal(before)
						afterJSON, err2 := json.Marshal(after)
						if err1 != nil || err2 != nil || !bytes.Equal(beforeJSON, afterJSON) {
							t.Fatal("denied consolidation changed persisted sources, duplicates or destination")
						}
						return
					}
					var result api.MemoryConsolidationApplyResponse
					if err := json.Unmarshal([]byte(body), &result); err != nil || !result.Applied {
						t.Fatal("authorized consolidation did not report applied")
					}
					if _, exists := after[ownSource]; exists {
						t.Fatal("authorized source was not deleted")
					}
					if participant == "destination" || participant == "explicit-destination" {
						row, exists := after[id]
						if !exists || row.Snippet == before[id].Snippet || !strings.Contains(row.Snippet, ownSource) || result.Result.ExistingID != id || len(after) != len(before)-1 {
							t.Fatal("authorized destination was not updated in place")
						}
					} else {
						if _, exists := after[id]; exists {
							t.Fatal("authorized participant was not deleted")
						}
						if participant == "discard" {
							if result.Action != "discarded" || len(after) != len(before)-2 {
								t.Fatal("discard created a destination or failed to remove both sources")
							}
						} else {
							found := false
							for newID, row := range after {
								if _, existed := before[newID]; !existed && memory.IsScenarioCard(row) && strings.Contains(row.Snippet, key) {
									found = true
								}
							}
							if !found || len(after) != len(before)-1 {
								t.Fatal("authorized consolidation did not replace participants with one durable card")
							}
						}
					}
					for existingID, row := range before {
						if existingID == id || existingID == ownSource {
							continue
						}
						if after[existingID] != row {
							t.Fatal("authorized consolidation changed unrelated memory")
						}
					}
				})
			}
		})
	}
}

// COVERS: CHAT-103
func TestE2E_SecurityUrgentChannelProofLifecycle(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	channels := h.PlatformBackend().UserChannels()
	const email = "urgent-proof-lifecycle@acme.test"
	pending := &store.UserChannel{
		ID:     uuid.NewString(),
		UserID: seed.Users[e2eboot.UserAliceEmail].ID, ChannelType: "email",
		ExternalID: email, DisplayName: email, CreatedAt: time.Now(),
	}
	if err := channels.Link(ctx, pending); err != nil {
		t.Fatal("cannot seed pending channel")
	}
	snapshot := func(t *testing.T) []byte {
		t.Helper()
		ch, err := channels.GetByExternalID(ctx, "email", email)
		if err != nil || ch == nil {
			t.Fatal("persisted channel missing")
		}
		data, err := json.Marshal(ch)
		if err != nil {
			t.Fatal("cannot snapshot channel")
		}
		return data
	}
	before := snapshot(t)
	var initial store.UserChannel
	if err := json.Unmarshal(before, &initial); err != nil || initial.Verified || initial.Enabled || initial.VerifiedAt != nil {
		t.Fatal("fixture must start unverified and disabled")
	}
	const endpoint = "/api/user/channels/verify-email-code"
	t.Run("direct-verification", func(t *testing.T) {
		for _, client := range []*e2eboot.Client{alice, bob} {
			urgentStatus(t, client.Post(t, "/api/user/channels/"+pending.ID+"/verify", map[string]any{}), http.StatusGone)
			if !bytes.Equal(snapshot(t), before) {
				t.Fatal("direct verification changed the pending channel")
			}
		}
	})
	t.Run("expired-proof", func(t *testing.T) {
		const code = "UrgentExpiredProofCaseSensitive123"
		if err := h.Store.LinkCodes().GenerateWithTTL(ctx, code, pending.UserID, email, "email:"+email, -time.Minute); err != nil {
			t.Fatal("cannot seed expired proof")
		}
		urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
		if !bytes.Equal(snapshot(t), before) {
			t.Fatal("expired proof changed channel state")
		}
	})
	t.Run("concurrent-proof-and-replay", func(t *testing.T) {
		const code = "UrgentConcurrentProofCaseSensitive123"
		if err := h.Store.LinkCodes().Generate(ctx, code, pending.UserID, email, "email:"+email); err != nil {
			t.Fatal("cannot seed concurrent proof")
		}
		// Resolve the test server through the authenticated client; do not copy
		// bootstrap/auth setup or call testing.Fatal from request goroutines.
		probe := alice.Get(t, "/api/user/channels")
		target := probe.Request.URL.ResolveReference(&url.URL{Path: endpoint}).String()
		urgentStatus(t, probe, http.StatusOK)
		const workers = 8
		requests := make([]*http.Request, workers)
		for i := range requests {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(`{"code":"`+code+`"}`))
			if err != nil {
				t.Fatal("cannot construct proof request")
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+alice.Token())
			req.Header.Set("X-Astonish-Team", alice.TeamSlug())
			requests[i] = req
		}
		type result struct {
			status int
			err    error
		}
		results := make(chan result, workers)
		start := make(chan struct{})
		client := &http.Client{Timeout: 30 * time.Second}
		for _, req := range requests {
			go func() {
				<-start
				resp, err := client.Do(req)
				if err != nil {
					results <- result{err: err}
					return
				}
				_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
				closeErr := resp.Body.Close()
				if readErr == nil {
					readErr = closeErr
				}
				results <- result{status: resp.StatusCode, err: readErr}
			}()
		}
		close(start)
		successes := 0
		for range workers {
			r := <-results
			if r.err != nil {
				t.Error("concurrent proof request failed at transport level")
				continue
			}
			switch r.status {
			case http.StatusOK:
				successes++
			case http.StatusBadRequest:
			default:
				t.Errorf("concurrent proof status=%d; infrastructure/rate errors are not replay proof", r.status)
			}
		}
		if successes != 1 {
			t.Fatalf("successful proof consumers=%d, want exactly one", successes)
		}
		linked, err := channels.GetByExternalID(ctx, "email", email)
		if err != nil || linked == nil || linked.ID != pending.ID || linked.UserID != pending.UserID || !linked.Verified || !linked.Enabled || linked.VerifiedAt == nil {
			t.Fatal("winning proof did not verify and enable the owner's existing channel")
		}
		rows, err := channels.ListByChannelType(ctx, "email")
		if err != nil {
			t.Fatal("cannot inspect persisted links")
		}
		matches := 0
		for _, row := range rows {
			if row.ExternalID == email {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal("concurrent proof created duplicate links")
		}
		remaining, err := h.Store.LinkCodes().Peek(ctx, code)
		if err != nil || remaining != nil {
			t.Fatal("winning proof was not consumed")
		}
		verified := snapshot(t)
		urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
		if !bytes.Equal(snapshot(t), verified) {
			t.Fatal("replay changed verified channel")
		}
	})
	t.Run("linked-to-other-user", func(t *testing.T) {
		const code = "UrgentConflictingProofCaseSensitive123"
		if err := h.Store.LinkCodes().Generate(ctx, code, seed.Users[e2eboot.UserBobEmail].ID, email, "email:"+email); err != nil {
			t.Fatal("cannot seed conflicting proof")
		}
		beforeConflict := snapshot(t)
		urgentStatus(t, bob.Post(t, endpoint, map[string]string{"code": code}), http.StatusConflict)
		urgentStatus(t, bob.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
		if !bytes.Equal(snapshot(t), beforeConflict) {
			t.Fatal("conflicting user's proof changed channel ownership or state")
		}
	})
}

func TestE2E_SecurityUrgentChannelOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	ctx := context.Background()
	// Seed a pending email delivery in the real platform store: SMTP delivery is
	// outside this test; verification and persistence use the real HTTP server.
	const code = "UrgentEmailTokenCaseSensitive123"
	const email = "urgent-verified@acme.test"
	if err := h.Store.LinkCodes().Generate(ctx, code, seed.Users[e2eboot.UserAliceEmail].ID, email, "email:"+email); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/user/channels/verify-email-code"
	urgentStatus(t, bob.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": strings.ToLower(code)}), http.StatusBadRequest)
	pending, err := h.Store.LinkCodes().Peek(ctx, code)
	if err != nil || pending == nil {
		t.Fatal("rejected verification consumed owner's token")
	}
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusOK)
	linked, err := h.PlatformBackend().UserChannels().GetByExternalID(ctx, "email", email)
	if err != nil || linked == nil || !linked.Verified || linked.UserID != seed.Users[e2eboot.UserAliceEmail].ID {
		t.Fatal("valid owner verification was not persisted")
	}
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
}
