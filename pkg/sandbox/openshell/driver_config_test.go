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
	if len(mounts) != 2 {
		t.Fatalf("volume_mounts len = %d, want 2 (operator + system)", len(mounts))
	}
	m0 := mounts[0].(map[string]any)
	if m0["mount_path"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("mount_path[0] = %v", m0["mount_path"])
	}
	if m0["sub_path"] != "ca-bundle.crt" {
		t.Errorf("sub_path[0] = %v", m0["sub_path"])
	}
	if m0["read_only"] != true {
		t.Errorf("mount[0] read_only = %v", m0["read_only"])
	}
	m1 := mounts[1].(map[string]any)
	if m1["mount_path"] != systemCABundlePath {
		t.Errorf("mount_path[1] = %v, want %s", m1["mount_path"], systemCABundlePath)
	}
	if m1["sub_path"] != "ca-bundle.crt" {
		t.Errorf("sub_path[1] = %v", m1["sub_path"])
	}
	if m1["name"] != "corp-root-ca" {
		t.Errorf("system mount volume name = %v", m1["name"])
	}

	for _, k := range defaultTrustEnvVars {
		if result.TrustEnv[k] != "/etc/astonish-ca/ca-bundle.crt" {
			t.Errorf("TrustEnv[%s] = %q", k, result.TrustEnv[k])
		}
	}
	if len(result.ExtraReadOnly) != 2 {
		t.Fatalf("ExtraReadOnly = %v, want 2 paths", result.ExtraReadOnly)
	}
	if result.ExtraReadOnly[0] != "/etc/astonish-ca/ca-bundle.crt" || result.ExtraReadOnly[1] != systemCABundlePath {
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

func boolPtr(v bool) *bool { return &v }

func TestRenderDriverConfig_InstallSystemTrustFalse(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:               "corp",
		ClaimName:          "pvc-ca",
		MountPath:          "/etc/astonish-ca/ca.pem",
		InstallSystemTrust: boolPtr(false),
	}})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	mounts := result.DriverConfig["kubernetes"].(map[string]any)["containers"].(map[string]any)["agent"].(map[string]any)["volume_mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("volume_mounts len = %d, want 1", len(mounts))
	}
	if mounts[0].(map[string]any)["mount_path"] != "/etc/astonish-ca/ca.pem" {
		t.Errorf("unexpected mounts: %#v", mounts)
	}
}

func TestRenderDriverConfig_RejectTwoSystemTrustInstalls(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{
		{Name: "a", ClaimName: "pvc-a", MountPath: "/etc/astonish-ca/a.crt"},
		{Name: "b", ClaimName: "pvc-b", MountPath: "/etc/astonish-ca/b.crt"},
	})
	if err == nil {
		t.Fatal("expected error for two default install_system_trust bundles")
	}
}

func TestRenderDriverConfig_SecondBundleWithoutSystemTrust(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{
		{Name: "a", ClaimName: "pvc-a", MountPath: "/etc/astonish-ca/a.crt"},
		{Name: "b", ClaimName: "pvc-b", MountPath: "/etc/astonish-ca/b.crt", InstallSystemTrust: boolPtr(false)},
	})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	mounts := result.DriverConfig["kubernetes"].(map[string]any)["containers"].(map[string]any)["agent"].(map[string]any)["volume_mounts"].([]any)
	// a: operator + system; b: operator only
	if len(mounts) != 3 {
		t.Fatalf("volume_mounts len = %d, want 3", len(mounts))
	}
	systemCount := 0
	for _, m := range mounts {
		if m.(map[string]any)["mount_path"] == systemCABundlePath {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("system CA mounts = %d, want 1", systemCount)
	}
}

func TestRenderDriverConfig_MountPathIsSystemCA(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "corp",
		ClaimName: "pvc-ca",
		MountPath: systemCABundlePath,
		SubPath:   "ca-bundle.crt",
	}})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	mounts := result.DriverConfig["kubernetes"].(map[string]any)["containers"].(map[string]any)["agent"].(map[string]any)["volume_mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("volume_mounts len = %d, want 1 (no duplicate)", len(mounts))
	}
	m := mounts[0].(map[string]any)
	if m["mount_path"] != systemCABundlePath {
		t.Errorf("mount_path = %v", m["mount_path"])
	}
	if m["sub_path"] != "ca-bundle.crt" {
		t.Errorf("sub_path = %v", m["sub_path"])
	}
	if result.TrustEnv["SSL_CERT_FILE"] != systemCABundlePath {
		t.Errorf("SSL_CERT_FILE = %q", result.TrustEnv["SSL_CERT_FILE"])
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

func TestRenderDriverConfig_ConfigMapSource(t *testing.T) {
	result, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:          "corp-root-ca",
		Source:        config.CertBundleSourceConfigMap,
		ConfigMapName: "astonish-corp-ca",
		MountPath:     "/etc/astonish-ca/ca-bundle.crt",
		SubPath:       "ca-bundle.crt",
	}})
	if err != nil {
		t.Fatalf("renderDriverConfig: %v", err)
	}
	if result.DriverConfig != nil {
		t.Fatalf("DriverConfig = %#v, want nil (Kyverno injects mounts)", result.DriverConfig)
	}
	if result.TrustEnv["SSL_CERT_FILE"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("SSL_CERT_FILE = %q", result.TrustEnv["SSL_CERT_FILE"])
	}
	if len(result.ExtraReadOnly) != 2 {
		t.Fatalf("ExtraReadOnly = %v, want operator + system paths", result.ExtraReadOnly)
	}
	if result.ExtraReadOnly[0] != "/etc/astonish-ca/ca-bundle.crt" || result.ExtraReadOnly[1] != systemCABundlePath {
		t.Errorf("ExtraReadOnly = %v", result.ExtraReadOnly)
	}
}

func TestRenderDriverConfig_ConfigMapRequiresName(t *testing.T) {
	_, err := renderDriverConfig([]config.CertBundleConfig{{
		Name:      "corp",
		Source:    config.CertBundleSourceConfigMap,
		MountPath: "/etc/astonish-ca/ca.pem",
	}})
	if err == nil {
		t.Fatal("expected error for missing config_map_name")
	}
}

func TestApplyCertBundles_ConfigMapStillSetsTrustEnv(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		CertBundles: []config.CertBundleConfig{{
			Name:          "corp",
			Source:        config.CertBundleSourceConfigMap,
			ConfigMapName: "astonish-corp-ca",
			MountPath:     "/etc/astonish-ca/ca-bundle.crt",
		}},
	}
	env := map[string]string{}
	dc, err := applyCertBundles(cfg, env)
	if err != nil {
		t.Fatalf("applyCertBundles: %v", err)
	}
	if dc != nil {
		t.Fatalf("driver_config = %#v, want nil", dc)
	}
	if env["SSL_CERT_FILE"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("SSL_CERT_FILE = %q", env["SSL_CERT_FILE"])
	}
}

func TestDefaultSandboxPolicy_ConfigMapCertBundleExtraReadOnly(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		CertBundles: []config.CertBundleConfig{{
			Name:          "corp",
			Source:        config.CertBundleSourceConfigMap,
			ConfigMapName: "astonish-corp-ca",
			MountPath:     "/etc/astonish-ca/ca-bundle.crt",
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
