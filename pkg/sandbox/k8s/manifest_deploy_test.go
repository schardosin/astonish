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
