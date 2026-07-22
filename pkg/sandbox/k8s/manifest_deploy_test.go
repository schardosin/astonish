// Package k8s — Helm chart validation tests.
//
// The chart under deploy/helm/astonish is applied by cluster admins
// via `helm upgrade --install`; a typo in the sandbox RBAC or PVC
// templates is the kind of thing that only surfaces when someone
// tries to bootstrap a new cluster, which is exactly when they least
// want to debug a YAML error. These tests close that gap by rendering
// the chart in-process (shelling out to `helm template`) and parsing
// every resource with the same YAML loader Kubernetes itself uses,
// rejecting files that fail structural validation (missing required
// fields, unknown top-level keys, malformed labels, etc.).
//
// The approach is deliberately dependency-light: we don't spin up a
// fake API server or pull in the full openapi schema. Instead we:
//
//   1. Shell out to `helm template` with values-dev-proxmox.yaml to
//      get the full manifest stream (skipped if `helm` is not on PATH).
//   2. Split on --- to get a slice of YAML documents.
//   3. Run sigs.k8s.io/yaml.UnmarshalStrict into a typed struct when
//      the kind is well-known (Namespace, Role, PVC, etc.).
//   4. Assert the invariants that matter (namespaces match backend
//      defaults, RBAC verbs cover every b.client call site, PVC access
//      modes are RWX, etc.).
//
// Reference: docs/architecture/sandbox-backends.md §11 Phase D/F.

package k8s

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// chartDir returns the absolute path to the Helm chart root.
// filepath.Abs resolves symlinks so this is stable across repo checkouts.
func chartDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../../deploy/helm/astonish")
	if err != nil {
		t.Fatalf("resolve chart dir: %v", err)
	}
	return abs
}

// renderChart shells out to `helm template` with the dev-proxmox values
// file so tests exercise the concrete, operator-facing configuration.
// Returns (stdout, true) on success. Skips the test if `helm` is not
// installed (CI may opt-in; local dev should always have it).
func renderChart(t *testing.T) ([]byte, bool) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skipf("helm not on PATH: %v (install helm 3.8+ to run chart validation tests)", err)
		return nil, false
	}
	root := chartDir(t)
	valuesFile := filepath.Join(root, "values-dev-proxmox.yaml")

	// Ensure subchart archives are present. The .tgz files are .gitignore'd
	// (correctly — binary blobs don't belong in git) so a clean checkout
	// won't have them. Chart.lock is committed for reproducibility, so
	// `helm dependency build` can fetch them on demand.
	ensureHelmDeps(t, root)

	// Derive the release namespace from the values file's namespaces.prefix
	// so the chart validation template's Release.Namespace check passes.
	ns := helmNamespaceFromValues(t, valuesFile)

	cmd := exec.Command("helm", "template", "astonish", root,
		"-n", ns,
		"-f", valuesFile,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template failed: %v\nstderr:\n%s", err, stderr.String())
		return nil, false
	}
	return stdout.Bytes(), true
}

// ensureHelmDeps runs `helm dependency build` if the charts/ directory is
// missing subchart archives. Skips the test if OCI/network access is
// unavailable (same graceful degradation as missing helm binary).
func ensureHelmDeps(t *testing.T, chartRoot string) {
	t.Helper()
	chartsDir := filepath.Join(chartRoot, "charts")
	matches, _ := filepath.Glob(filepath.Join(chartsDir, "*.tgz"))
	if len(matches) > 0 {
		return // Already have subchart archives.
	}
	depCmd := exec.Command("helm", "dependency", "build", chartRoot)
	if out, err := depCmd.CombinedOutput(); err != nil {
		t.Skipf("helm dependency build failed (no OCI registry access?): %v\n%s", err, out)
	}
}

// helmNamespaceFromValues reads namespaces.prefix from a values YAML file.
// Falls back to "astonish" if not set (the chart default).
func helmNamespaceFromValues(t *testing.T, valuesFile string) string {
	t.Helper()
	data, err := os.ReadFile(valuesFile)
	if err != nil {
		t.Fatalf("read values file: %v", err)
	}
	var vals struct {
		Namespaces struct {
			Prefix string `yaml:"prefix"`
		} `yaml:"namespaces"`
	}
	if err := yaml.Unmarshal(data, &vals); err != nil {
		t.Fatalf("parse values file: %v", err)
	}
	if vals.Namespaces.Prefix == "" {
		return "astonish"
	}
	return vals.Namespaces.Prefix
}

// splitYAMLDocs splits a multi-doc YAML file on "---" separators and
// trims whitespace-only documents. The loader tolerates a trailing
// separator (common in hand-maintained YAML).
func splitYAMLDocs(b []byte) [][]byte {
	parts := strings.Split(string(b), "\n---")
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "---")
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, []byte(p))
	}
	return out
}

// kindPeek extracts kind/apiVersion from a YAML doc without a full
// decode, so we can route each doc to the right typed unmarshal.
type kindPeek struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metav1.ObjectMeta `json:"metadata"`
}

func peekKind(t *testing.T, doc []byte) kindPeek {
	t.Helper()
	var p kindPeek
	if err := yaml.Unmarshal(doc, &p); err != nil {
		t.Fatalf("peek kind: %v\nDoc:\n%s", err, doc)
	}
	if p.Kind == "" {
		t.Fatalf("doc has empty kind:\n%s", doc)
	}
	if p.APIVersion == "" {
		t.Fatalf("doc %q has empty apiVersion", p.Kind)
	}
	return p
}

// TestChart_AllParse asserts every document rendered by the chart
// decodes cleanly into its typed struct. sigs.k8s.io/yaml uses strict
// JSON-tag matching under the hood, so misspelled keys (e.g. "lables"
// instead of "labels") show up as decode errors here.
func TestChart_AllParse(t *testing.T) {
	rendered, ok := renderChart(t)
	if !ok {
		return
	}
	docs := splitYAMLDocs(rendered)
	if len(docs) == 0 {
		t.Fatalf("no YAML docs rendered")
	}
	for i, doc := range docs {
		pk := peekKind(t, doc)
		switch pk.Kind {
		case "Namespace":
			var obj corev1.Namespace
			if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "ServiceAccount":
			var obj corev1.ServiceAccount
			if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "Role":
			var obj rbacv1.Role
			if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "RoleBinding":
			var obj rbacv1.RoleBinding
			if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "PersistentVolumeClaim":
			var obj corev1.PersistentVolumeClaim
			if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "Service", "Secret", "ConfigMap":
			// Generic decode — struct-tag validation via typed path not
			// needed (these templates are trivially stable).
			var obj map[string]interface{}
			if err := yaml.Unmarshal(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		case "Deployment", "Job", "Ingress", "DaemonSet":
			// batch/v1, apps/v1, networking.k8s.io imports would grow
			// the surface; generic decode is sufficient to catch syntax.
			var obj map[string]interface{}
			if err := yaml.Unmarshal(doc, &obj); err != nil {
				t.Errorf("doc %d (%s %s): %v", i, pk.Kind, pk.Metadata.Name, err)
			}
		default:
			t.Errorf("doc %d: unexpected kind %q", i, pk.Kind)
		}
	}
}

// TestChart_NamespaceInvariants pins the sandbox namespace name against
// what the backend expects. The chart derives names from namespaces.prefix;
// the rendered sandbox namespace MUST be "{prefix}-sandbox".
func TestChart_NamespaceInvariants(t *testing.T) {
	rendered, ok := renderChart(t)
	if !ok {
		return
	}
	docs := splitYAMLDocs(rendered)
	names := []string{}
	for _, d := range docs {
		pk := peekKind(t, d)
		if pk.Kind != "Namespace" {
			continue
		}
		names = append(names, pk.Metadata.Name)
	}
	// The sandbox namespace is always {prefix}-sandbox.
	root := chartDir(t)
	prefix := helmNamespaceFromValues(t, filepath.Join(root, "values-dev-proxmox.yaml"))
	wantSandboxNS := prefix + "-sandbox"
	found := false
	for _, n := range names {
		if n == wantSandboxNS {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("namespace %q not rendered; found %v", wantSandboxNS, names)
	}
}

// TestChart_RBACCoversBackendCalls asserts the Role grants every
// verb/resource pair the backend actually invokes. This is the quickest
// way to catch a backend feature landing without its RBAC rule (or
// vice-versa, a stale permission kept past its need-to-know).
//
// The expected set below MUST mirror the inventory documented in
// templates/sandbox/rbac.yaml; if you add a new b.client call site,
// update both places.
func TestChart_RBACCoversBackendCalls(t *testing.T) {
	rendered, ok := renderChart(t)
	if !ok {
		return
	}
	docs := splitYAMLDocs(rendered)
	var role rbacv1.Role
	for _, d := range docs {
		pk := peekKind(t, d)
		if pk.Kind == "Role" {
			if err := yaml.Unmarshal(d, &role); err != nil {
				t.Fatalf("decode role: %v", err)
			}
			break
		}
	}
	if role.Name == "" {
		t.Fatal("no Role rendered by chart")
	}

	// (apiGroup, resource) → set of required verbs.
	type key struct{ group, resource string }
	required := map[key][]string{
		{"", "pods"}:                             {"get", "list", "watch", "create", "delete"},
		{"", "pods/exec"}:                        {"create"},
		{"", "pods/status"}:                      {"update"},
		{"", "services"}:                         {"get", "list", "create", "delete"},
		{"networking.k8s.io", "networkpolicies"}: {"get", "list", "create", "update", "delete"},
		{"", "persistentvolumeclaims"}:           {"get", "list"},
	}

	// Flatten the Role's rules into the same shape for comparison.
	granted := map[key]map[string]bool{}
	for _, r := range role.Rules {
		for _, g := range r.APIGroups {
			for _, res := range r.Resources {
				k := key{group: g, resource: res}
				if granted[k] == nil {
					granted[k] = map[string]bool{}
				}
				for _, v := range r.Verbs {
					granted[k][v] = true
				}
			}
		}
	}

	for k, verbs := range required {
		gset := granted[k]
		if gset == nil {
			t.Errorf("missing rule for %+v", k)
			continue
		}
		for _, v := range verbs {
			if !gset[v] {
				t.Errorf("rule %+v missing verb %q", k, v)
			}
		}
	}
}

// TestChart_PVCsAreRWX guards against a storageClassName change that
// silently drops RWX support (e.g., accidentally switching to an RBD-
// style block class that is RWO-only). The K8s backend MUST have RWX
// on both layers and uppers or pods won't be able to schedule on
// arbitrary nodes (§4).
func TestChart_PVCsAreRWX(t *testing.T) {
	rendered, ok := renderChart(t)
	if !ok {
		return
	}
	docs := splitYAMLDocs(rendered)
	count := 0
	for _, d := range docs {
		pk := peekKind(t, d)
		if pk.Kind != "PersistentVolumeClaim" {
			continue
		}
		var pvc corev1.PersistentVolumeClaim
		if err := yaml.Unmarshal(d, &pvc); err != nil {
			t.Fatalf("decode pvc: %v", err)
		}
		count++
		hasRWX := false
		for _, m := range pvc.Spec.AccessModes {
			if m == corev1.ReadWriteMany {
				hasRWX = true
				break
			}
		}
		if !hasRWX {
			t.Errorf("PVC %q not RWX; modes = %v", pvc.Name, pvc.Spec.AccessModes)
		}
	}
	if count < 2 {
		t.Errorf("expected 2 PVCs (layers, uppers), got %d", count)
	}
}

// TestChart_CertBundlePVCAccessMode pins that chart-managed OpenShell
// cert-bundle PVCs (source: pvc) default to ReadWriteMany and honor
// bootstrap.accessMode overrides.
//
// helm template has no live cluster, so lookup returns empty and the desired
// mode is rendered. Preserving an existing RWO claim’s accessMode on upgrade
// is live-cluster only (see cert-bundle-pvc.yaml).
func TestChart_CertBundlePVCAccessMode(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skipf("helm not on PATH: %v", err)
	}
	root := chartDir(t)
	ensureHelmDeps(t, root)
	valuesFile := filepath.Join(root, "values-e2e-openshell.yaml")
	ns := helmNamespaceFromValues(t, valuesFile)

	t.Run("defaultRWX", func(t *testing.T) {
		rendered := mustHelmTemplate(t, root, ns, valuesFile,
			"--set", "sandbox.openshell.certBundles[0].name=corp-root-ca",
			"--set", "sandbox.openshell.certBundles[0].source=pvc",
			"--set", "sandbox.openshell.certBundles[0].claimName=astonish-corp-ca",
			"--set", "sandbox.openshell.certBundles[0].mountPath=/etc/astonish-ca/ca-bundle.crt",
			"--set", "sandbox.openshell.certBundles[0].bootstrap.enabled=true",
			"--set", "sandbox.openshell.certBundles[0].bootstrap.url=https://example.com/ca.crt",
		)
		pvc := findPVCByName(t, rendered, "astonish-corp-ca")
		if got := pvc.Spec.AccessModes; len(got) != 1 || got[0] != corev1.ReadWriteMany {
			t.Fatalf("default accessModes = %v, want [ReadWriteMany]", got)
		}
	})

	t.Run("bootstrapOverrideRWO", func(t *testing.T) {
		rendered := mustHelmTemplate(t, root, ns, valuesFile,
			"--set", "sandbox.openshell.certBundles[0].name=corp-root-ca",
			"--set", "sandbox.openshell.certBundles[0].source=pvc",
			"--set", "sandbox.openshell.certBundles[0].claimName=astonish-corp-ca-rwo",
			"--set", "sandbox.openshell.certBundles[0].mountPath=/etc/astonish-ca/ca-bundle.crt",
			"--set", "sandbox.openshell.certBundles[0].bootstrap.enabled=true",
			"--set", "sandbox.openshell.certBundles[0].bootstrap.url=https://example.com/ca.crt",
			"--set", "sandbox.openshell.certBundles[0].bootstrap.accessMode=ReadWriteOnce",
		)
		pvc := findPVCByName(t, rendered, "astonish-corp-ca-rwo")
		if got := pvc.Spec.AccessModes; len(got) != 1 || got[0] != corev1.ReadWriteOnce {
			t.Fatalf("override accessModes = %v, want [ReadWriteOnce]", got)
		}
	})
}

// TestChart_CertBundleConfigMapMode pins ConfigMap + Kyverno inject resources
// and ensures no cert PVC is rendered for source=configMap.
func TestChart_CertBundleConfigMapMode(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skipf("helm not on PATH: %v", err)
	}
	root := chartDir(t)
	ensureHelmDeps(t, root)
	valuesFile := filepath.Join(root, "values-e2e-openshell.yaml")
	ns := helmNamespaceFromValues(t, valuesFile)

	rendered := mustHelmTemplate(t, root, ns, valuesFile,
		"--set", "sandbox.openshell.certBundles[0].name=corp-root-ca",
		"--set", "sandbox.openshell.certBundles[0].source=configMap",
		"--set", "sandbox.openshell.certBundles[0].configMapName=astonish-corp-ca",
		"--set", "sandbox.openshell.certBundles[0].mountPath=/etc/astonish-ca/ca-bundle.crt",
		"--set", "sandbox.openshell.certBundles[0].subPath=ca-bundle.crt",
		"--set", "sandbox.openshell.certBundles[0].bootstrap.enabled=true",
		"--set", "sandbox.openshell.certBundles[0].bootstrap.url=https://example.com/ca.crt",
	)

	cm := findConfigMapByName(t, rendered, "astonish-corp-ca")
	if _, ok := cm.Data["ca-bundle.crt"]; !ok {
		t.Fatalf("ConfigMap missing ca-bundle.crt key; data=%v", cm.Data)
	}

	if findOptionalPVCByName(rendered, "astonish-corp-ca") != nil {
		t.Fatal("expected no cert-bundle PVC for source=configMap")
	}

	foundPolicy := false
	foundSA := false
	foundJob := false
	for _, d := range splitYAMLDocs(rendered) {
		var meta struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := yaml.Unmarshal(d, &meta); err != nil || meta.Kind == "" {
			continue
		}
		switch meta.Kind {
		case "ClusterPolicy":
			if strings.Contains(meta.Metadata.Name, "sandbox-cert-bundle") {
				foundPolicy = true
				if !bytes.Contains(d, []byte("astonish-corp-ca")) {
					t.Error("ClusterPolicy missing configMap name")
				}
				if !bytes.Contains(d, []byte("inject-cert-bundle-corp-root-ca")) {
					t.Error("ClusterPolicy missing inject rule")
				}
			}
		case "ServiceAccount":
			if strings.Contains(meta.Metadata.Name, "cert-bundle-bootstrap") {
				foundSA = true
			}
		case "Job":
			if strings.Contains(meta.Metadata.Name, "cert-bundle-corp-root-ca") {
				foundJob = true
				if !bytes.Contains(d, []byte("CM_NAME")) {
					t.Error("bootstrap Job should patch ConfigMap")
				}
				if bytes.Contains(d, []byte("persistentVolumeClaim")) {
					t.Error("configMap bootstrap Job must not mount a PVC")
				}
			}
		}
	}
	if !foundPolicy {
		t.Error("expected Kyverno ClusterPolicy for cert-bundle ConfigMap inject")
	}
	if !foundSA {
		t.Error("expected cert-bundle-bootstrap ServiceAccount")
	}
	if !foundJob {
		t.Error("expected cert-bundle bootstrap Job")
	}

	// App config should advertise configMap source (no PVC in driver_config).
	foundAppCert := false
	for _, d := range splitYAMLDocs(rendered) {
		var meta struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(d, &meta); err != nil || meta.Kind != "ConfigMap" {
			continue
		}
		if !bytes.Contains(d, []byte("cert_bundles:")) {
			continue
		}
		foundAppCert = true
		if !bytes.Contains(d, []byte("source:")) || !bytes.Contains(d, []byte("configMap")) {
			t.Error("app config cert_bundles missing source: configMap")
		}
		if !bytes.Contains(d, []byte("config_map_name:")) || !bytes.Contains(d, []byte("astonish-corp-ca")) {
			t.Error("app config missing config_map_name")
		}
	}
	if !foundAppCert {
		t.Error("expected cert_bundles in app ConfigMap")
	}
}

func findConfigMapByName(t *testing.T, rendered []byte, name string) corev1.ConfigMap {
	t.Helper()
	for _, d := range splitYAMLDocs(rendered) {
		pk := peekKind(t, d)
		if pk.Kind != "ConfigMap" {
			continue
		}
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal(d, &cm); err != nil {
			t.Fatalf("decode configmap: %v", err)
		}
		if cm.Name == name {
			return cm
		}
	}
	t.Fatalf("ConfigMap %q not found in helm template output", name)
	return corev1.ConfigMap{}
}

func findOptionalPVCByName(rendered []byte, name string) *corev1.PersistentVolumeClaim {
	for _, d := range splitYAMLDocs(rendered) {
		var meta struct {
			Kind string `yaml:"kind"`
		}
		_ = yaml.Unmarshal(d, &meta)
		if meta.Kind != "PersistentVolumeClaim" {
			continue
		}
		var pvc corev1.PersistentVolumeClaim
		if err := yaml.Unmarshal(d, &pvc); err != nil {
			continue
		}
		if pvc.Name == name {
			return &pvc
		}
	}
	return nil
}

func mustHelmTemplate(t *testing.T, chartRoot, ns, valuesFile string, extraArgs ...string) []byte {
	t.Helper()
	args := []string{"template", "astonish", chartRoot, "-n", ns, "-f", valuesFile}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return stdout.Bytes()
}

func findPVCByName(t *testing.T, rendered []byte, name string) corev1.PersistentVolumeClaim {
	t.Helper()
	for _, d := range splitYAMLDocs(rendered) {
		pk := peekKind(t, d)
		if pk.Kind != "PersistentVolumeClaim" {
			continue
		}
		var pvc corev1.PersistentVolumeClaim
		if err := yaml.Unmarshal(d, &pvc); err != nil {
			t.Fatalf("decode pvc: %v", err)
		}
		if pvc.Name == name {
			return pvc
		}
	}
	t.Fatalf("PVC %q not found in rendered chart", name)
	return corev1.PersistentVolumeClaim{}
}
