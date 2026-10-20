//go:build e2e

package e2eboot

import (
	"os"
	"os/exec"
	"strings"
	"testing"
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

// Re-enter the real bootstrap with an unsafe namespace and a PATH trap. No DB,
// runtime, provider, or cleanup command may be reached, on either backend.
func TestSecurityPreflightBeforeBootstrap(t *testing.T) {
	if os.Getenv("ASTONISH_PREFLIGHT_CHILD") == "1" {
		Bootstrap(t)
		t.Fatal("unsafe bootstrap returned")
	}
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSecurityPreflightBeforeBootstrap$")
			env := securityPreflightEnv()
			env["ASTONISH_PREFLIGHT_CHILD"] = "1"
			env["ASTONISH_SECURITY_ACCEPTANCE"] = "1"
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

// Used by make test-security-preflight; never calls Bootstrap or any cleanup.
func TestSecurityPreflightEnvironment(t *testing.T) {
	if os.Getenv("ASTONISH_SECURITY_PREFLIGHT") != "1" {
		t.Skip("explicit environment preflight only")
	}
	if err := ValidateSecurityPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before cleanup: %v", err)
	}
}
