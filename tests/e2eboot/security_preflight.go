//go:build e2e

package e2eboot

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
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

// ValidatePostgresFirstRunPreflight is a pure, additional gate for setup, which
// changes cluster-wide roles. A disposable database on a shared instance is NOT
// sufficient. The operator must approve and later destroy the entire dedicated
// instance; this fixture deliberately does not drop databases or restore roles.
// Bind approval to an explicit host:port and use a separate DSN, never silently
// reuse ASTONISH_TEST_DSN. This acknowledgement cannot prove physical isolation.
func ValidatePostgresFirstRunPreflight(getenv func(string) string) error {
	if getenv("ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED") != "1" {
		return fmt.Errorf("ASTONISH_E2E_FIRST_RUN_POSTGRES_DEDICATED=1 required: approve disposable entire PostgreSQL instance and cluster-role changes")
	}
	if getenv("ASTONISH_E2E_BACKEND") != "postgres" {
		return fmt.Errorf("first-run PostgreSQL requires explicit postgres backend")
	}
	u, err := url.Parse(getenv("ASTONISH_E2E_FIRST_RUN_POSTGRES_DSN"))
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || u.Port() == "" || u.Path != "/postgres" || u.Fragment != "" {
		return fmt.Errorf("first-run DSN must explicitly specify PostgreSQL username/password, host, port and /postgres")
	}
	password, ok := u.User.Password()
	port, err := strconv.Atoi(u.Port())
	if !ok || password == "" || err != nil || port < 1 || port > 65535 || strings.ContainsAny(u.Hostname(), ",/\\") {
		return fmt.Errorf("invalid explicit first-run PostgreSQL connection parameters")
	}
	if getenv("ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE") != net.JoinHostPort(u.Hostname(), u.Port()) {
		return fmt.Errorf("ASTONISH_E2E_FIRST_RUN_POSTGRES_INSTANCE must match the approved DSN host:port")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 || len(query["sslmode"]) != 1 {
		return fmt.Errorf("first-run DSN permits only one explicit sslmode parameter; routing overrides are forbidden")
	}
	switch query.Get("sslmode") {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("first-run DSN requires explicit non-fallback sslmode")
	}
	if getenv("ASTONISH_E2E_KEEP_ALIVE") != "" {
		return fmt.Errorf("first-run PostgreSQL cannot attach to an inspector")
	}
	return nil
}

func requireSecurityPreflight(t *testing.T) {
	t.Helper()
	if os.Getenv("ASTONISH_SECURITY_ACCEPTANCE") != "1" && !strings.HasPrefix(t.Name(), "TestE2E_Security") {
		return
	}
	if err := ValidateSecurityPreflight(os.Getenv); err != nil {
		t.Fatalf("BLOCKED before cleanup: %v", err)
	}
}
