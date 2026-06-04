// Command e2e-inspector boots a long-lived Astonish StudioServer for the
// "shared instance" mode used by `make test-e2e-inspect`. It is not part of
// the production binary and is only used by E2E test tooling.
//
// Usage:
//
//	./e2e-inspector              Boot the inspector (default).
//	./e2e-inspector --cleanup    Drop all astonish_e2einspect_* databases and
//	                             remove inspector tmp/state files. Refuses
//	                             to run while a live inspector is detected.
//	                             Also deletes any leaked sandbox pods so the
//	                             next run starts on an empty cluster.
//	./e2e-inspector --stop-pods  Delete all sandbox pods in the e2e sandbox
//	                             namespace WITHOUT touching databases or
//	                             tmp files. Safe to run while the inspector
//	                             is alive — useful as a between-suite hygiene
//	                             step that the inspector binary itself
//	                             schedules at shutdown does not provide.
//
// Reads ASTONISH_TEST_DSN, BIFROST_API_KEY (or another provider key), and
// optional BIFROST_BASE_URL, ASTONISH_E2E_SANDBOX_NAMESPACE,
// ASTONISH_E2E_CONTROL_PLANE_NAMESPACE, KUBECONFIG. Writes its state to
// /tmp/astonish-e2e-inspect.json on startup; tests read this file to know
// where to connect.
//
// The process blocks on SIGINT/SIGTERM. On signal, it gracefully shuts down
// the StudioServer and removes its state file. It does NOT drop databases
// at shutdown — that's the job of `--cleanup` (invoked by
// `make test-e2e-inspect-stop`). The decoupling lets a developer browse
// data right up until they explicitly ask for a clean slate.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store/pgutil"
	"github.com/schardosin/astonish/tests/e2eboot"

	// Register sandbox backends so Configure Base, MCP stdio discovery,
	// and other sandbox-dependent E2E features work.
	_ "github.com/schardosin/astonish/pkg/sandbox/k8s"
	_ "github.com/schardosin/astonish/pkg/sandbox/mock"
)

func main() {
	cleanup := flag.Bool("cleanup", false, "Drop astonish_e2einspect_* databases, remove tmp/state files, AND delete leaked sandbox pods, then exit.")
	info := flag.Bool("info", false, "Print the seeded user table for the running inspector and exit.")
	stopPods := flag.Bool("stop-pods", false, "Delete all sandbox pods in the e2e sandbox namespace, then exit. Does not touch databases or tmp files.")
	flag.Parse()

	if *cleanup {
		runCleanup()
		return
	}
	if *info {
		runInfo()
		return
	}
	if *stopPods {
		runStopPods()
		return
	}

	runInspector()
}

func runInspector() {
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		fail("ASTONISH_TEST_DSN is required")
	}

	apiKey := resolveAPIKey()
	if apiKey == "" {
		fail("No provider API key found (BIFROST_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, ANTHROPIC_API_KEY)")
	}

	// Refuse to start if another inspector is already running.
	if state, alive := e2eboot.IsInspectorRunning(); alive {
		fail("Inspector already running (PID %d, port %d). Run `make test-e2e-inspect-stop` first.", state.PID, state.Port)
	}

	// Generate per-run secret (reserved for future admin endpoints).
	secret := mustGenSecret(32)
	if err := os.WriteFile(e2eboot.InspectorSecretFile, []byte(secret), 0600); err != nil {
		fail("write secret file: %v", err)
	}

	// Use a stable config dir under /tmp so XDG_CONFIG_HOME survives across
	// the test runs that share this inspector. Subdir is "astonish".
	parentDir, err := os.MkdirTemp("/tmp", "astonish-e2e-inspect-*")
	if err != nil {
		fail("mkdtemp: %v", err)
	}
	configDir := filepath.Join(parentDir, "astonish")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("[inspector] Starting on port %d (suffix=%s)...\n", e2eboot.InspectorPort, e2eboot.InspectorSuffix)
	core, err := e2eboot.BootstrapPlatformCore(ctx, e2eboot.CoreOptions{
		BaseDSN:      dsn,
		APIKey:       apiKey,
		Suffix:       e2eboot.InspectorSuffix,
		Port:         e2eboot.InspectorPort,
		ConfigDir:    configDir,
		JWTSecret:    e2eboot.InspectorJWTSecret,
		DropExisting: true, // always start fresh
		Log:          stdoutLogger{},
	})
	if err != nil {
		fail("bootstrap: %v", err)
	}

	hostname, _ := os.Hostname()
	state := &e2eboot.InspectorState{
		PID:        os.Getpid(),
		Port:       e2eboot.InspectorPort,
		Hostname:   hostname,
		BaseURL:    core.BaseURL,
		Suffix:     core.Suffix,
		BaseDSN:    core.BaseDSN,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
		UserEmail:  e2eboot.InspectorDefaultEmail,
		UserPasswd: e2eboot.InspectorDefaultPassword,
	}
	if err := e2eboot.WriteInspectorState(state); err != nil {
		core.Shutdown(ctx)
		fail("write state file: %v", err)
	}

	printBanner(state)

	// Block on SIGINT/SIGTERM.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	fmt.Printf("\n[inspector] Received %s, shutting down...\n", sig)

	// Best-effort cleanup of the state file.
	if err := os.Remove(e2eboot.InspectorStateFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("[inspector] WARN: remove state file: %v\n", err)
	}
	if err := os.Remove(e2eboot.InspectorSecretFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("[inspector] WARN: remove secret file: %v\n", err)
	}

	shutdownCtx, scancel := context.WithTimeout(ctx, 10*time.Second)
	defer scancel()
	core.Shutdown(shutdownCtx)
	fmt.Println("[inspector] Stopped.")
}

func resolveAPIKey() string {
	for _, env := range []string{
		"BIFROST_API_KEY",
		"OPENAI_API_KEY",
		"GOOGLE_API_KEY",
		"ANTHROPIC_API_KEY",
	} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}

func mustGenSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		fail("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func printBanner(s *e2eboot.InspectorState) {
	bar := "────────────────────────────────────────────────────────────────"
	fmt.Println()
	fmt.Println(bar)
	fmt.Println("E2E inspector is running.")
	fmt.Println()
	fmt.Println("Reach it from this host:")
	fmt.Printf("  http://localhost:%d\n", s.Port)
	if s.Hostname != "" && s.Hostname != "localhost" {
		fmt.Printf("  http://%s:%d\n", s.Hostname, s.Port)
	}
	fmt.Println()
	fmt.Println("Reach it from your laptop (run on your laptop):")
	fmt.Printf("  ssh -L %d:localhost:%d %s\n", s.Port, s.Port, sshTarget(s.Hostname))
	fmt.Printf("  Then open http://localhost:%d\n", s.Port)
	fmt.Println()
	fmt.Println("Login:")
	fmt.Printf("  Email:    %s\n", s.UserEmail)
	fmt.Printf("  Password: %s\n", s.UserPasswd)
	fmt.Println()
	fmt.Println("Tests provision additional users (Alice/Bob/Carol/Dave/Eve) per package.")
	fmt.Println("After the suite runs, list them with:")
	fmt.Println("  bin/e2e-inspector --info")
	fmt.Println()
	fmt.Println("Tests will run against this instance until you stop it with:")
	fmt.Println("  make test-e2e-inspect-stop")
	fmt.Println(bar)
	fmt.Println()
}

func sshTarget(hostname string) string {
	if hostname == "" || hostname == "localhost" {
		return "<dev-host>"
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "<user>"
	}
	return fmt.Sprintf("%s@%s", user, hostname)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[inspector] ERROR: "+format+"\n", args...)
	os.Exit(1)
}

type stdoutLogger struct{}

func (stdoutLogger) Logf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// runCleanup drops every astonish_e2einspect_* database and removes the
// inspector's tmp/state files. Refuses to run while a live inspector PID
// is detected — caller must stop the process first. Always idempotent:
// finding nothing to do is a successful no-op.
func runCleanup() {
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		fail("ASTONISH_TEST_DSN is required for --cleanup (it's the admin connection used to DROP DATABASE)")
	}

	if state, alive := e2eboot.IsInspectorRunning(); alive {
		fail("Inspector is still alive (PID %d, port %d). Stop it first (kill that PID), then re-run --cleanup.", state.PID, state.Port)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("[inspector] Cleanup: dropping astonish_%s_* databases...\n", e2eboot.InspectorSuffix)
	e2eboot.DropAllDBsWithSuffix(ctx, dsn, e2eboot.InspectorSuffix, stdoutLogger{})

	// Remove inspector state files (idempotent).
	for _, path := range []string{
		e2eboot.InspectorStateFile,
		e2eboot.InspectorSecretFile,
		"/tmp/astonish-e2e-inspect.pid",
		"/tmp/astonish-e2e-inspect.log",
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("[inspector] WARN: remove %s: %v\n", path, err)
		}
	}

	// Remove inspector tmp config dirs (e.g. /tmp/astonish-e2e-inspect-1234567890).
	entries, err := os.ReadDir("/tmp")
	if err != nil {
		fmt.Printf("[inspector] WARN: read /tmp: %v\n", err)
	} else {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "astonish-e2e-inspect-") {
				continue
			}
			full := filepath.Join("/tmp", name)
			if err := os.RemoveAll(full); err != nil {
				fmt.Printf("[inspector] WARN: remove %s: %v\n", full, err)
			} else {
				fmt.Printf("[inspector] Removed tmp dir %s\n", full)
			}
		}
	}

	// Drain any sandbox pods left behind by retained sessions. This is the
	// counterpart to ASTONISH_E2E_KEEP_ALIVE=1 keeping pods alive during
	// the suite — at full cleanup time we want a clean cluster too.
	ns := os.Getenv("ASTONISH_E2E_SANDBOX_NAMESPACE")
	if ns == "" {
		ns = "astonishe2e-sandbox"
	}
	if _, err := exec.LookPath("kubectl"); err == nil {
		deleteSandboxPods(ns)
	}

	fmt.Println("[inspector] Cleanup complete.")
}

// runStopPods deletes all sandbox pods in the e2e sandbox namespace using
// kubectl. This is a between-suite hygiene step — it does NOT touch the
// inspector's databases, tmp files, or state. Safe to run while the
// inspector is alive (the inspector itself doesn't pin any pods).
//
// Reads the sandbox namespace from $ASTONISH_E2E_SANDBOX_NAMESPACE,
// defaulting to "astonishe2e-sandbox" (the value used by the e2e test
// helm install — see Makefile E2E_K8S_SANDBOX_NS).
//
// Idempotent: an empty namespace is a successful no-op. Missing kubectl
// or unreachable cluster is reported but not fatal — sandbox infra may
// legitimately be down between sessions.
func runStopPods() {
	ns := os.Getenv("ASTONISH_E2E_SANDBOX_NAMESPACE")
	if ns == "" {
		ns = "astonishe2e-sandbox"
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		fmt.Println("[inspector] --stop-pods: kubectl not in PATH, skipping (cluster not accessible from this host)")
		return
	}

	deleteSandboxPods(ns)
}

// deleteSandboxPods runs `kubectl delete pods -n <ns> --all` with a short
// grace period so the call returns promptly even if some pods are
// unresponsive. Failures are logged at WARN level but do not abort —
// callers (cleanup or stop-pods) treat this as best-effort hygiene.
func deleteSandboxPods(ns string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Quick existence check — `kubectl get ns` returns non-zero if absent.
	checkCmd := exec.CommandContext(ctx, "kubectl", "get", "ns", ns, "--no-headers")
	if err := checkCmd.Run(); err != nil {
		fmt.Printf("[inspector] --stop-pods: namespace %q not present, nothing to clean.\n", ns)
		return
	}

	fmt.Printf("[inspector] --stop-pods: deleting all pods in namespace %q...\n", ns)
	delCmd := exec.CommandContext(ctx, "kubectl", "delete", "pods", "-n", ns,
		"--all", "--grace-period=5", "--ignore-not-found")
	delCmd.Stdout = os.Stdout
	delCmd.Stderr = os.Stderr
	if err := delCmd.Run(); err != nil {
		fmt.Printf("[inspector] --stop-pods: WARN: kubectl delete returned: %v\n", err)
		return
	}
	fmt.Printf("[inspector] --stop-pods: namespace %q drained.\n", ns)
}

// seededUserRow describes one seeded user as it should appear in the info
// table. The layout is fixed by tests/e2eboot/layout.go and re-stated here
// so this binary doesn't need the e2e build tag.
type seededUserRow struct {
	LocalPart string // "alice" / "bob" / ...
	Domain    string // "@acme.test" / "@globex.test"
	OrgBase   string // "acme" / "globex"
	TeamBase  string // "red" / "blue" / "engineering"
	OrgRole   string
	TeamRole  string
}

var seededUserLayout = []seededUserRow{
	{"alice", "@acme.test", "acme", "red", "owner", "admin"},
	{"bob", "@acme.test", "acme", "red", "member", "member"},
	{"carol", "@acme.test", "acme", "blue", "member", "member"},
	{"dave", "@globex.test", "globex", "engineering", "owner", "admin"},
	{"eve", "@globex.test", "globex", "engineering", "member", "member"},
}

// runInfo connects to the inspector's platform DB, finds every plus-tagged
// seeded user (e.g. alice+chatauth@acme.test), and prints a per-package
// login table. Refuses to run if no inspector state file is found.
func runInfo() {
	state, err := e2eboot.ReadInspectorState()
	if err != nil {
		if os.IsNotExist(err) {
			fail("No inspector state file at %s — is the inspector running? Start it with `make test-e2e-inspect`.", e2eboot.InspectorStateFile)
		}
		fail("read inspector state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	platformDSN, err := pgutil.ReplaceDSNDatabase(state.BaseDSN, config.PlatformDBName(state.Suffix))
	if err != nil {
		fail("derive platform DSN: %v", err)
	}
	conn, err := pgx.Connect(ctx, platformDSN)
	if err != nil {
		fail("connect to platform DB: %v", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, "SELECT email FROM users WHERE email LIKE '%+%@%' ORDER BY email")
	if err != nil {
		fail("query users: %v", err)
	}
	defer rows.Close()

	// Collect packages → list of plus-tagged emails actually present.
	pkgEmails := map[string]map[string]bool{} // package → set of "<local>+<pkg>@<domain>"
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			continue
		}
		pkg := extractPackageFromEmail(email)
		if pkg == "" {
			continue
		}
		if pkgEmails[pkg] == nil {
			pkgEmails[pkg] = map[string]bool{}
		}
		pkgEmails[pkg][email] = true
	}

	if len(pkgEmails) == 0 {
		fmt.Println("No seeded users found yet. Run the test suite (`make test-e2e-inspect`) to provision them.")
		fmt.Println()
		fmt.Println("Bootstrap user (always available):")
		fmt.Printf("  Email:    %s\n", state.UserEmail)
		fmt.Printf("  Password: %s\n", state.UserPasswd)
		return
	}

	printUserTable(state, pkgEmails)
}

func extractPackageFromEmail(email string) string {
	// Format: "<local>+<pkg>@<domain>"
	plus := strings.Index(email, "+")
	at := strings.LastIndex(email, "@")
	if plus < 0 || at <= plus+1 {
		return ""
	}
	return email[plus+1 : at]
}

func printUserTable(state *e2eboot.InspectorState, pkgEmails map[string]map[string]bool) {
	bar := "────────────────────────────────────────────────────────────────"
	fmt.Println(bar)
	fmt.Println("Inspector login reference")
	fmt.Println(bar)
	fmt.Println()
	fmt.Println("URL:")
	fmt.Printf("  http://localhost:%d\n", state.Port)
	if state.Hostname != "" && state.Hostname != "localhost" {
		fmt.Printf("  http://%s:%d\n", state.Hostname, state.Port)
	}
	fmt.Println()
	fmt.Println("Bootstrap user (sees only the \"default\" org — useful for platform-admin pages):")
	fmt.Printf("  Email:    %s\n", state.UserEmail)
	fmt.Printf("  Password: %s\n", state.UserPasswd)
	fmt.Println()
	fmt.Println("Seeded test actors (each sees their own org and data — including chat sessions):")
	fmt.Printf("  Password: %s   (same for all seeded users)\n", e2eboot.SeededUserPassword)
	fmt.Println()

	// Sort packages for stable output.
	pkgs := make([]string, 0, len(pkgEmails))
	for p := range pkgEmails {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, pkg := range pkgs {
		fmt.Printf("  Package %q:\n", pkg)
		for _, row := range seededUserLayout {
			email := fmt.Sprintf("%s+%s%s", row.LocalPart, pkg, row.Domain)
			if !pkgEmails[pkg][email] {
				continue // user wasn't actually provisioned (partial seed)
			}
			fmt.Printf("    %-40s  org=%-22s team=%-12s role=%s/%s\n",
				email,
				fmt.Sprintf("%s-%s", row.OrgBase, pkg),
				row.TeamBase,
				row.OrgRole, row.TeamRole)
		}
		fmt.Println()
	}

	fmt.Println("Note: `flow_assistant` tests use only the bootstrap user — no seeded actors.")
	fmt.Println(bar)
}
