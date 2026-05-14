package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKubeconfig drops a minimal-but-valid kubeconfig into dir/config
// and returns its full path. The embedded CA is a throw-away PEM; it is
// never used for a real connection because these tests only exercise the
// load path, not API calls.
func writeKubeconfig(t *testing.T, dir, contextName string) string {
	t.Helper()
	p := filepath.Join(dir, "config")
	body := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://kube.example:6443
    insecure-skip-tls-verify: true
  name: unit
contexts:
- context:
    cluster: unit
    user: unit
  name: ` + contextName + `
current-context: ` + contextName + `
users:
- name: unit
  user:
    token: deadbeef
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return p
}

// TestLoadRESTConfig_ExplicitPath exercises the KubeconfigPath branch. A
// non-empty path must bypass in-cluster and $KUBECONFIG entirely, and the
// returned rest.Config must reflect the cluster the file names.
func TestLoadRESTConfig_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "local")

	cfg, err := LoadRESTConfig(LoadConfigOptions{KubeconfigPath: path})
	if err != nil {
		t.Fatalf("LoadRESTConfig: %v", err)
	}
	if cfg.Host != "https://kube.example:6443" {
		t.Errorf("Host = %q, want https://kube.example:6443", cfg.Host)
	}
	if cfg.BearerToken != "deadbeef" {
		t.Errorf("BearerToken = %q, want deadbeef", cfg.BearerToken)
	}
	if !cfg.TLSClientConfig.Insecure {
		t.Errorf("TLSClientConfig.Insecure = false, want true (set by kubeconfig)")
	}
}

// TestLoadRESTConfig_DefaultsApplied verifies that QPS/Burst/UserAgent
// are populated when the caller doesn't supply them and the kubeconfig
// leaves them zero.
func TestLoadRESTConfig_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "ctx")

	cfg, err := LoadRESTConfig(LoadConfigOptions{KubeconfigPath: path})
	if err != nil {
		t.Fatalf("LoadRESTConfig: %v", err)
	}
	if cfg.QPS != 50 {
		t.Errorf("QPS = %v, want 50 (default)", cfg.QPS)
	}
	if cfg.Burst != 100 {
		t.Errorf("Burst = %d, want 100 (default)", cfg.Burst)
	}
	if !strings.Contains(cfg.UserAgent, "astonish") {
		t.Errorf("UserAgent = %q, want to contain 'astonish'", cfg.UserAgent)
	}
}

// TestLoadRESTConfig_OverridesHonoured checks that caller-supplied
// QPS/Burst/UserAgent win over both defaults and kubeconfig values.
func TestLoadRESTConfig_OverridesHonoured(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "ctx")

	cfg, err := LoadRESTConfig(LoadConfigOptions{
		KubeconfigPath: path,
		QPS:            12.5,
		Burst:          42,
		UserAgent:      "test/0.1",
	})
	if err != nil {
		t.Fatalf("LoadRESTConfig: %v", err)
	}
	if cfg.QPS != 12.5 {
		t.Errorf("QPS = %v, want 12.5", cfg.QPS)
	}
	if cfg.Burst != 42 {
		t.Errorf("Burst = %d, want 42", cfg.Burst)
	}
	if cfg.UserAgent != "test/0.1" {
		t.Errorf("UserAgent = %q, want test/0.1", cfg.UserAgent)
	}
}

// TestLoadRESTConfig_ContextSelection verifies that opts.Context pins the
// requested context even when the kubeconfig's current-context differs.
func TestLoadRESTConfig_ContextSelection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	body := `apiVersion: v1
kind: Config
clusters:
- cluster: {server: https://a.example:6443, insecure-skip-tls-verify: true}
  name: a
- cluster: {server: https://b.example:6443, insecure-skip-tls-verify: true}
  name: b
contexts:
- context: {cluster: a, user: t}
  name: ctx-a
- context: {cluster: b, user: t}
  name: ctx-b
current-context: ctx-a
users:
- name: t
  user: {token: x}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadRESTConfig(LoadConfigOptions{KubeconfigPath: p, Context: "ctx-b"})
	if err != nil {
		t.Fatalf("LoadRESTConfig: %v", err)
	}
	if cfg.Host != "https://b.example:6443" {
		t.Errorf("context override not honoured: Host = %q, want https://b.example:6443", cfg.Host)
	}
}

// TestLoadRESTConfig_MissingKubeconfig asserts the error path when the
// caller names a kubeconfig that doesn't exist.
func TestLoadRESTConfig_MissingKubeconfig(t *testing.T) {
	_, err := LoadRESTConfig(LoadConfigOptions{KubeconfigPath: "/nonexistent/kubeconfig"})
	if err == nil {
		t.Fatal("expected error for missing kubeconfig, got nil")
	}
	if !strings.Contains(err.Error(), "load kubeconfig") {
		t.Errorf("error message not contextualised: %v", err)
	}
}

// TestLoadRESTConfig_InClusterForcedButUnavailable exercises the branch
// where the caller insists on in-cluster mode but we aren't running
// inside a cluster. The error must be actionable.
func TestLoadRESTConfig_InClusterForcedButUnavailable(t *testing.T) {
	// Run in a clean environment to avoid picking up a genuine SA token
	// if the test happens to execute inside a cluster.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	_, err := LoadRESTConfig(LoadConfigOptions{InCluster: true})
	if err == nil {
		t.Fatal("expected error when forcing in-cluster outside a cluster")
	}
	if !strings.Contains(err.Error(), "in-cluster") {
		t.Errorf("error should mention in-cluster; got %v", err)
	}
}

// TestLoadRESTConfig_FallsBackToKubeconfigEnv asserts that when neither
// InCluster is forced nor KubeconfigPath is set, the loader honours
// $KUBECONFIG.
func TestLoadRESTConfig_FallsBackToKubeconfigEnv(t *testing.T) {
	// Ensure the in-cluster probe can't spuriously succeed on CI.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "ctx")
	t.Setenv("KUBECONFIG", path)

	cfg, err := LoadRESTConfig(LoadConfigOptions{})
	if err != nil {
		t.Fatalf("LoadRESTConfig: %v", err)
	}
	if cfg.Host != "https://kube.example:6443" {
		t.Errorf("Host = %q, want https://kube.example:6443", cfg.Host)
	}
}

// TestNewClientFromOptions_ReturnsClientset is a smoke test for the
// helper that bundles LoadRESTConfig + kubernetes.NewForConfig. It only
// asserts that a non-nil clientset and rest.Config are returned; no API
// calls are made.
func TestNewClientFromOptions_ReturnsClientset(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "ctx")

	cs, rc, err := NewClientFromOptions(LoadConfigOptions{KubeconfigPath: path})
	if err != nil {
		t.Fatalf("NewClientFromOptions: %v", err)
	}
	if cs == nil {
		t.Error("clientset is nil")
	}
	if rc == nil {
		t.Error("rest.Config is nil")
	}
	if rc != nil && rc.Host == "" {
		t.Error("rest.Config.Host is empty")
	}
}
