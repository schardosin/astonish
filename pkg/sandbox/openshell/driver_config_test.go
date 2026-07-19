package openshell

import (
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestRenderDriverConfig_Empty(t *testing.T) {
	result, err := renderDriverConfig(nil)
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	if result.DriverConfig != nil {
		t.Errorf("DriverConfig = %v, want nil", result.DriverConfig)
	}
	if len(result.TrustEnv) != 0 {
		t.Errorf("TrustEnv = %v, want empty", result.TrustEnv)
	}
}

func TestRenderDriverConfig_PVCShape(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "corp-root-ca",
		ClaimName: "astonish-corp-ca",
		MountPath: "/etc/astonish-ca/ca-bundle.crt",
		SubPath:   "ca-bundle.crt",
	}})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	if result.DriverConfig == nil {
		t.Fatal("expected DriverConfig")
	}
	k8s, ok := result.DriverConfig["kubernetes"].(map[string]any)
	if !ok {
		t.Fatalf("kubernetes block missing: %#v", result.DriverConfig)
	}
	vols, ok := k8s["volumes"].([]any)
	if !ok || len(vols) != 1 {
		t.Fatalf("volumes = %#v", k8s["volumes"])
	}
	vol := vols[0].(map[string]any)
	if vol["name"] != "corp-root-ca" {
		t.Errorf("volume name = %v", vol["name"])
	}
	pvc := vol["persistent_volume_claim"].(map[string]any)
	if pvc["claim_name"] != "astonish-corp-ca" {
		t.Errorf("claim_name = %v", pvc["claim_name"])
	}
	if pvc["read_only"] != true {
		t.Errorf("read_only = %v, want true", pvc["read_only"])
	}

	containers := k8s["containers"].(map[string]any)
	agent := containers["agent"].(map[string]any)
	mounts := agent["volume_mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("volume_mounts len = %d", len(mounts))
	}
	m := mounts[0].(map[string]any)
	if m["mount_path"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("mount_path = %v", m["mount_path"])
	}
	if m["sub_path"] != "ca-bundle.crt" {
		t.Errorf("sub_path = %v", m["sub_path"])
	}
	if m["read_only"] != true {
		t.Errorf("mount read_only = %v", m["read_only"])
	}

	for _, k := range defaultTrustEnvVars {
		if result.TrustEnv[k] != "/etc/astonish-ca/ca-bundle.crt" {
			t.Errorf("TrustEnv[%s] = %q", k, result.TrustEnv[k])
		}
	}
	if len(result.ExtraReadOnly) != 1 || result.ExtraReadOnly[0] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("ExtraReadOnly = %v", result.ExtraReadOnly)
	}
}

func TestRenderDriverConfig_CustomTrustEnv(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "corp",
		ClaimName: "pvc-ca",
		MountPath: "/etc/astonish-ca/ca.pem",
		TrustEnv:  []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"},
	}})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	if len(result.TrustEnv) != 2 {
		t.Fatalf("TrustEnv = %v, want 2 keys", result.TrustEnv)
	}
	if result.TrustEnv["SSL_CERT_FILE"] != "/etc/astonish-ca/ca.pem" {
		t.Errorf("SSL_CERT_FILE = %q", result.TrustEnv["SSL_CERT_FILE"])
	}
}

func TestRenderDriverConfig_RejectSandboxMount(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "bad",
		ClaimName: "pvc",
		MountPath: "/sandbox/certs/ca.pem",
	}})
	if err == nil {
		t.Fatal("expected error for /sandbox mount")
	}
}

func TestRenderDriverConfig_RejectOpenshellMount(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "bad",
		ClaimName: "pvc",
		MountPath: "/etc/openshell-tls/ca.pem",
	}})
	if err == nil {
		t.Fatal("expected error for /etc/openshell-tls mount")
	}
}

func TestRenderDriverConfig_RejectRelativeMount(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "bad",
		ClaimName: "pvc",
		MountPath: "etc/astonish-ca/ca.pem",
	}})
	if err == nil {
		t.Fatal("expected error for relative mount_path")
	}
}

func TestRenderDriverConfig_RejectDuplicateName(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{
		{Name: "corp", ClaimName: "pvc-a", MountPath: "/etc/astonish-ca/a.crt"},
		{Name: "corp", ClaimName: "pvc-b", MountPath: "/etc/astonish-ca/b.crt"},
	})
	if err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestApplyCertBundles_MergesTrustEnv(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		CertBundles: []config.CertBundleConfig{{
			Name:      "corp",
			ClaimName: "astonish-corp-ca",
			MountPath: "/etc/astonish-ca/ca-bundle.crt",
		}},
	}
	env := map[string]string{"ASTONISH_SESSION_ID": "s1"}
	dc, err := applyCertBundles(cfg, env)
	if err != nil {
		t.Fatalf("applyCertBundles: %v", err)
	}
	if dc == nil {
		t.Fatal("expected driver_config")
	}
	if env["SSL_CERT_FILE"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("SSL_CERT_FILE = %q", env["SSL_CERT_FILE"])
	}
	if env["ASTONISH_SESSION_ID"] != "s1" {
		t.Error("session id env overwritten")
	}
}

func TestDefaultSandboxPolicy_CertBundleExtraReadOnly(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		CertBundles: []config.CertBundleConfig{{
			Name:      "corp",
			ClaimName: "pvc",
			MountPath: "/etc/astonish-ca/ca-bundle.crt",
		}},
	}
	policy := defaultSandboxPolicy(cfg)
	found := false
	for _, p := range policy.Filesystem.ReadOnly {
		if p == "/etc/astonish-ca/ca-bundle.crt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cert mount path in Landlock read-only set")
	}
}
