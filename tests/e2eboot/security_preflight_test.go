//go:build e2e

package e2eboot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func securityPreflightEnv() map[string]string {
	return map[string]string{
		"ASTONISH_E2E_DEDICATED":               "1",
		"ASTONISH_E2E_SANDBOX_NAMESPACE":       "astonishe2e-security-sandbox",
		"ASTONISH_E2E_CONTROL_PLANE_NAMESPACE": "astonishe2e-security-control",
		"KUBECONFIG":                           "/dedicated/kubeconfig",
		"ASTONISH_TEST_DSN":                    "dedicated-test-placeholder",
		"BIFROST_API_KEY":                      "test-placeholder",
	}
}

func TestPostgresFirstRunPreflightValidation(t *testing.T) {
	valid := func() map[string]string {
		return map[string]string{
			"ASTONISH_E2E_BACKEND":                      "postgres",
			"ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED": "1",
			"ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE":  "dedicated.invalid:5432",
			"ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN":       "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable",
		}
	}
	t.Run("approved-instance", func(t *testing.T) {
		env := valid()
		if err := ValidatePostgresFirstRunPreflight(func(k string) string { return env[k] }); err != nil {
			t.Fatal("explicit dedicated instance rejected")
		}
	})
	for _, tc := range []struct{ name, key, value string }{
		{"missing-approval", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED", ""},
		{"ambiguous-approval", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED", "true"},
		{"wrong-backend", "ASTONISH_E2E_BACKEND", "sqlite"},
		{"missing-instance", "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE", ""},
		{"different-instance", "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE", "shared.invalid:5432"},
		{"missing-dsn", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", ""},
		{"invalid-dsn", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic-password%zz"},
		{"random-database-not-isolation", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic:synthetic-password@dedicated.invalid:5432/random?sslmode=disable"},
		{"implicit-port", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic:synthetic-password@dedicated.invalid/postgres?sslmode=disable"},
		{"missing-password", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic@dedicated.invalid:5432/postgres?sslmode=disable"},
		{"routing-override", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable&host=shared.invalid"},
		{"ssl-fallback", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=prefer"},
		{"duplicate-sslmode", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN", "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable&sslmode=require"},
		{"inspector", "ASTONISH_E2E_KEEP_ALIVE", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := valid()
			env[tc.key] = tc.value
			err := ValidatePostgresFirstRunPreflight(func(k string) string { return env[k] })
			if err == nil {
				t.Fatal("unsafe first-run environment accepted")
			}
			if strings.Contains(err.Error(), "synthetic-password") {
				t.Fatal("gate error disclosed DSN credentials")
			}
		})
	}
}

func TestSecurityPreflightValidation(t *testing.T) {
	valid := securityPreflightEnv()
	if err := ValidateSecurityPreflight(func(k string) string { return valid[k] }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ASTONISH_E2E_DEDICATED", ""},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", ""},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", "astonish-sandbox"},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", "production"},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", "astonishe2e-"},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", "astonishe2e-test;unsafe"},
		{"ASTONISH_E2E_SANDBOX_NAMESPACE", "astonishe2e-" + strings.Repeat("a", 64)},
		{"ASTONISH_E2E_CONTROL_PLANE_NAMESPACE", "astonish"},
		{"ASTONISH_E2E_CONTROL_PLANE_NAMESPACE", valid["ASTONISH_E2E_SANDBOX_NAMESPACE"]},
		{"KUBECONFIG", ""}, {"ASTONISH_TEST_DSN", ""}, {"BIFROST_API_KEY", ""},
		{"ASTONISH_E2E_KEEP_ALIVE", "1"}, {"ASTONISH_E2E_SANDBOX_BACKEND", "openshell"},
		{"ASTONISH_E2E_BACKEND", "unknown"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			env := securityPreflightEnv()
			env[tc.key] = tc.value
			if err := ValidateSecurityPreflight(func(k string) string { return env[k] }); err == nil {
				t.Fatal("unsafe environment accepted")
			}
		})
	}
	valid["ASTONISH_E2E_CONTROL_PLANE_NAMESPACE"] = "astonishe2e"
	valid["ASTONISH_E2E_SANDBOX_NAMESPACE"] = "astonishe2e-sandbox"
	if err := ValidateSecurityPreflight(func(k string) string { return valid[k] }); err != nil {
		t.Fatalf("approved plan namespace pair rejected: %v", err)
	}
	valid["ASTONISH_E2E_BACKEND"] = "sqlite"
	delete(valid, "ASTONISH_TEST_DSN")
	if err := ValidateSecurityPreflight(func(k string) string { return valid[k] }); err != nil {
		t.Fatal(err)
	}
}

// This probe runs only in the subprocess below; it must be protected by its
// TestE2E_Security prefix even without ASTONISH_SECURITY_ACCEPTANCE.
func TestE2E_SecurityPreflightProbe(t *testing.T) {
	if os.Getenv("ASTONISH_PREFLIGHT_CHILD") != "1" {
		t.Skip("subprocess-only safety probe")
	}
	Bootstrap(t)
	t.Fatal("unsafe bootstrap returned")
}

// Re-enter the real bootstrap with an unsafe namespace and a PATH trap. No DB,
// runtime, provider, or cleanup command may be reached, on either backend.
func TestSecurityPreflightBeforeBootstrap(t *testing.T) {
	if os.Getenv("ASTONISH_PREFLIGHT_CHILD") == "1" {
		Bootstrap(t)
		t.Fatal("unsafe bootstrap returned")
	}
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, byPrefix := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prefix=%t", backend, byPrefix), func(t *testing.T) {
				pattern := "^TestSecurityPreflightBeforeBootstrap$"
				if byPrefix {
					pattern = "^TestE2E_SecurityPreflightProbe$"
				}
				cmd := exec.Command(os.Args[0], "-test.run="+pattern)
				// Even a regressed gate must keep fallback SQLite files out of the checkout.
				cmd.Dir = t.TempDir()
				env := securityPreflightEnv()
				env["ASTONISH_PREFLIGHT_CHILD"] = "1"
				if !byPrefix {
					env["ASTONISH_SECURITY_ACCEPTANCE"] = "1"
				}
				env["ASTONISH_E2E_BACKEND"] = backend
				env["ASTONISH_E2E_SANDBOX_NAMESPACE"] = "astonish-sandbox"
				// Deliberately do not inherit current runtime/DB credentials.
				env["PATH"] = t.TempDir()
				env["HOME"] = t.TempDir()
				for k, v := range env {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
				out, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "BLOCKED before cleanup") {
					t.Fatalf("bootstrap did not fail at the safety gate: %v\n%s", err, out)
				}
			})
		}
	}
}

// Exercise the actual Make preflight entrypoint, not just its pure validators.
// Removing its first-run callback must turn the rejected PostgreSQL cases into
// failures here. This entrypoint never bootstraps or contacts external services.
func TestSecurityPreflightEnvironmentFirstRunAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		backend     string
		approval    string
		instance    string
		dsn         string
		wantBlocked string
	}{
		{
			name: "postgres-missing-all-first-run-authority", backend: "postgres",
			wantBlocked: "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED=1 required",
		},
		{
			name: "postgres-missing-approval", backend: "postgres",
			instance:    "dedicated.invalid:5432",
			dsn:         "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable",
			wantBlocked: "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED=1 required",
		},
		{
			name: "postgres-missing-instance", backend: "postgres", approval: "1",
			dsn:         "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable",
			wantBlocked: "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE must match",
		},
		{
			name: "postgres-mismatched-instance", backend: "postgres", approval: "1",
			instance:    "shared.invalid:5432",
			dsn:         "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable",
			wantBlocked: "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE must match",
		},
		{
			name: "postgres-approved-instance", backend: "postgres", approval: "1",
			instance: "dedicated.invalid:5432",
			dsn:      "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable",
		},
		{name: "sqlite-without-postgres-authority-or-dsn", backend: "sqlite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := securityPreflightEnv()
			env["ASTONISH_SECURITY_PREFLIGHT"] = "1"
			env["ASTONISH_E2E_BACKEND"] = tc.backend
			for key, value := range map[string]string{
				"ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED": tc.approval,
				"ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE":  tc.instance,
				"ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN":       tc.dsn,
			} {
				if value != "" {
					env[key] = value
				}
			}
			if tc.backend == "sqlite" {
				delete(env, "ASTONISH_TEST_DSN")
			}
			if err := ValidateSecurityPreflight(func(key string) string { return env[key] }); err != nil {
				t.Fatalf("ordinary preflight fixture must be valid: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSecurityPreflightEnvironment$", "-test.v")
			cmd.Dir = t.TempDir()
			// Build a fresh environment: never inherit provider/DB credentials,
			// kube configuration, inspector state, or executable lookup paths.
			env["HOME"] = t.TempDir()
			env["PATH"] = t.TempDir()
			env["TMPDIR"] = t.TempDir()
			cmd.Env = make([]string, 0, len(env))
			for key, value := range env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("side-effect-free preflight timed out: %v\n%s", ctx.Err(), out)
			}
			output := string(out)
			if strings.Contains(output, "synthetic-password") {
				t.Fatal("preflight disclosed DSN credentials")
			}
			if strings.Contains(output, "--- SKIP:") || strings.Contains(output, "BLOCKED before cleanup") {
				t.Fatalf("did not reach first-run authority validation:\n%s", out)
			}
			if tc.wantBlocked != "" {
				if err == nil || !strings.Contains(output, "BLOCKED before first-run effects: "+tc.wantBlocked) {
					t.Fatalf("preflight did not reject first-run authority: %v\n%s", err, out)
				}
			} else if err != nil || !strings.Contains(output, "--- PASS: TestSecurityPreflightEnvironment ") {
				t.Fatalf("valid preflight did not pass: %v\n%s", err, out)
			}
		})
	}
}

// Run the unchanged Make prerequisites with the real selector and preflight,
// while trapping every downstream effect. In particular, a missing dependency
// or a reordered kubectl call must fail this test without reaching live systems.
func TestSecurityPreflightMakeFirstRunAuthority(t *testing.T) {
	absoluteTool := func(name string) string {
		t.Helper()
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("actual Make preflight test requires %s: %v", name, err)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	makePath, nodePath := absoluteTool("make"), absoluteTool("node")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Go tests start in the package directory, not the repository root.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"test-security-preflight", "test-security"} {
		for _, tc := range []struct {
			name, approval, instance, wantBlocked string
		}{
			{"missing-approval", "", "dedicated.invalid:5432", "ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED=1 required"},
			{"mismatched-instance", "1", "shared.invalid:5432", "ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE must match"},
		} {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				write := func(relative string, data []byte, mode os.FileMode) {
					t.Helper()
					path := filepath.Join(root, relative)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, data, mode); err != nil {
						t.Fatal(err)
					}
				}
				// Deliberately copy only these files, never .env or .env.local.
				for _, relative := range []string{"Makefile", "tests/scenarios/parse-run.mjs"} {
					data, err := os.ReadFile(filepath.Join(repoRoot, relative))
					if err != nil {
						t.Fatal(err)
					}
					write(relative, data, 0600)
				}
				write("trace", nil, 0600)
				write("effects", nil, 0600)
				write("bin/node", []byte(`#!/bin/bash
if [[ "$#" != 2 || "$1" != tests/scenarios/parse-run.mjs || "$2" != --security-selection ]]; then
  printf 'unexpected node\n' >> "$PREFLIGHT_EFFECTS"
  exit 91
fi
"$PREFLIGHT_NODE" "$@" || exit $?
printf 'selection\n' >> "$PREFLIGHT_TRACE"
`), 0700)
				write("bin/go", []byte(`#!/bin/bash
if [[ "$#" != 6 || "$*" != 'test -tags=e2e -count=1 -run ^TestSecurityPreflightEnvironment$ ./tests/e2eboot' || "$ASTONISH_SECURITY_PREFLIGHT" != 1 || -n "$ASTONISH_SECURITY_ACCEPTANCE" ]]; then
  printf 'unexpected go/acceptance\n' >> "$PREFLIGHT_EFFECTS"
  exit 92
fi
printf 'preflight:%s\n' "$ASTONISH_E2E_BACKEND" >> "$PREFLIGHT_TRACE"
exec "$PREFLIGHT_TEST_BINARY" '-test.run=^TestSecurityPreflightEnvironment$' -test.v
`), 0700)
				for _, tool := range []string{"kubectl", "git", "mktemp"} {
					write("bin/"+tool, []byte("#!/bin/bash\nprintf 'unexpected "+tool+"\\n' >> \"$PREFLIGHT_EFFECTS\"\nexit 93\n"), 0700)
				}
				env := securityPreflightEnv()
				env["ASTONISH_E2E_BACKEND"] = "postgres"
				env["ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED"] = tc.approval
				env["ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE"] = tc.instance
				env["ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN"] = "postgres://synthetic:synthetic-password@dedicated.invalid:5432/postgres?sslmode=disable"
				if err := ValidateSecurityPreflight(func(key string) string { return env[key] }); err != nil {
					t.Fatalf("ordinary preflight fixture must be valid: %v", err)
				}
				// No inherited credentials, config, shell startup hooks, or Make flags.
				env["HOME"], env["TMPDIR"] = t.TempDir(), t.TempDir()
				env["KUBECONFIG"] = filepath.Join(root, "nonexistent-kubeconfig")
				env["PATH"] = filepath.Join(root, "bin")
				env["DEV_ARCH"] = "synthetic" // Avoid unrelated uname/sed Make expansion.
				env["PREFLIGHT_NODE"] = nodePath
				env["PREFLIGHT_TEST_BINARY"] = testBinary
				env["PREFLIGHT_TRACE"] = filepath.Join(root, "trace")
				env["PREFLIGHT_EFFECTS"] = filepath.Join(root, "effects")
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, makePath, "--no-print-directory", target,
					"SECURITY_TRACK=urgent", "SECURITY_STORAGE=both", "SECURITY_RUNTIME=k8s")
				cmd.Dir = root
				cmd.Env = make([]string, 0, len(env))
				for key, value := range env {
					cmd.Env = append(cmd.Env, key+"="+value)
				}
				out, runErr := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("isolated Make preflight timed out: %v", ctx.Err())
				}
				output := string(out)
				if strings.Contains(output, "synthetic-password") {
					t.Fatal("Make preflight disclosed DSN credentials")
				}
				for relative, want := range map[string]string{"trace": "selection\npreflight:postgres\n", "effects": ""} {
					got, err := os.ReadFile(filepath.Join(root, relative))
					if err != nil {
						t.Fatal(err)
					}
					if string(got) != want {
						t.Errorf("unexpected %s: got %q, want %q", relative, got, want)
					}
				}
				if runErr == nil || !strings.Contains(output, "BLOCKED before first-run effects: "+tc.wantBlocked) {
					t.Fatalf("Make did not fail at the first-run authority gate: %v\n%s", runErr, out)
				}
				if strings.Contains(output, "--- SKIP:") || strings.Contains(output, "BLOCKED before cleanup") {
					t.Fatalf("Make did not reach first-run authority validation:\n%s", out)
				}
			})
		}
	}
}

// Used by make test-security-preflight; never calls Bootstrap or any cleanup.
func TestSecurityPreflightEnvironment(t *testing.T) {
	if os.Getenv("ASTONISH_SECURITY_PREFLIGHT") != "1" {
		t.Skip("explicit environment preflight only")
	}
	if err := ValidateSecurityPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before cleanup: %v", err)
	}
	if os.Getenv("ASTONISH_E2E_BACKEND") == "postgres" {
		if err := ValidatePostgresFirstRunPreflight(os.Getenv); err != nil {
			t.Fatalf("BLOCKED before first-run effects: %v", err)
		}
	}
}
