//go:build e2e

package e2eboot

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// ValidateSecurityPreflight is intentionally side-effect free: it must run before
// Bootstrap can sweep a namespace, drop a database, or attach to an inspector.
// The acknowledgement cannot establish that a DB is disposable; the operator
// must independently provision a dedicated PostgreSQL instance and PVCs.
func ValidateSecurityPreflight(getenv func(string) string) error {
	if getenv("ASTONISH_E2E_DEDICATED") != "1" {
		return fmt.Errorf("ASTONISH_E2E_DEDICATED=1 required: acknowledge dedicated disposable DB, namespaces, and PVCs")
	}
	for _, key := range []string{"ASTONISH_E2E_SANDBOX_NAMESPACE", "ASTONISH_E2E_CONTROL_PLANE_NAMESPACE"} {
		ns := getenv(key)
		approvedControl := key == "ASTONISH_E2E_CONTROL_PLANE_NAMESPACE" && ns == "astonishe2e"
		if (!approvedControl && !regexp.MustCompile(`^(astonishe2e|astonish-e2e)-[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(ns)) || len(ns) > 63 {
			return fmt.Errorf("%s must explicitly name a dedicated astonishe2e-* or astonish-e2e-* namespace", key)
		}
	}
	if getenv("ASTONISH_E2E_SANDBOX_NAMESPACE") == getenv("ASTONISH_E2E_CONTROL_PLANE_NAMESPACE") {
		return fmt.Errorf("sandbox and control-plane namespaces must be distinct")
	}
	if getenv("KUBECONFIG") == "" {
		return fmt.Errorf("explicit KUBECONFIG for the dedicated acceptance cluster required")
	}
	if getenv("ASTONISH_E2E_KEEP_ALIVE") == "1" {
		return fmt.Errorf("security acceptance must not attach to an existing inspector")
	}
	if backend := getenv("ASTONISH_E2E_SANDBOX_BACKEND"); backend != "" && backend != "k8s" {
		return fmt.Errorf("security acceptance supports only the isolated k8s sandbox backend")
	}
	if backend := getenv("ASTONISH_E2E_BACKEND"); backend != "" && backend != "postgres" && backend != "sqlite" {
		return fmt.Errorf("unsupported acceptance storage backend")
	}
	if getenv("ASTONISH_E2E_BACKEND") != "sqlite" && getenv("ASTONISH_TEST_DSN") == "" {
		return fmt.Errorf("dedicated admin-capable ASTONISH_TEST_DSN required")
	}
	if getenv("BIFROST_API_KEY") == "" && getenv("OPENAI_API_KEY") == "" && getenv("GOOGLE_API_KEY") == "" && getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("a supported provider API key is required")
	}
	return nil
}

func requireSecurityPreflight(t *testing.T) {
	t.Helper()
	if os.Getenv("ASTONISH_SECURITY_ACCEPTANCE") != "1" && !strings.HasPrefix(t.Name(), "TestE2E_SecurityUrgent") {
		return
	}
	if err := ValidateSecurityPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before cleanup: %v", err)
	}
}
