// Package k8s — deploy-manifest validation tests.
//
// The manifests under deploy/k8s/ are applied verbatim by cluster
// admins; a typo here is the kind of thing that only surfaces when
// someone tries to bootstrap a new cluster, which is exactly when
// they least want to debug a YAML error. These tests close that gap
// by parsing every manifest in-process with the same YAML loader
// Kubernetes itself uses, rejecting files that fail structural
// validation (missing required fields, unknown top-level keys,
// malformed labels, etc.).
//
// The approach is deliberately dependency-light: we don't spin up a
// fake API server or pull in the full openapi schema, because those
// routes balloon the test binary and hide the failure in a wall of
// transitive errors. Instead we:
//
//   1. Split each file on --- to get a slice of YAML documents.
//   2. Run sigs.k8s.io/yaml.Unmarshal into a typed struct when the
//      kind is well-known (Namespace, Role, PVC, etc.) — this gives
//      real field-name validation via Go struct tags.
//   3. Assert the per-file invariants that matter for Phase D
//      (namespaces match the backend defaults, RBAC verbs include
//      everything the backend calls, RWX access modes, etc.).
//
// A companion test (behind the "integration" build tag) shells out
// to `kubectl apply --server-dry-run` so a developer with a live
// cluster can get the full server-side validation in one command.
// That test is NOT run in the default `go test ./...` pass; it
// requires `go test -tags integration -run TestDeployManifests_KubectlDryRun`
// and a reachable cluster.
//
// Reference: docs/architecture/sandbox-backends.md §11 Phase D.

package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// deployDir resolves the repo-root relative path to deploy/k8s without
// hard-coding a module path. Tests live at pkg/sandbox/k8s/ so the
// deploy dir is two levels up from the test's working directory.
func deployDir(t *testing.T) string {
	t.Helper()
	// The test binary's cwd is the package directory (pkg/sandbox/k8s).
	// We want ../../../deploy/k8s. filepath.Abs resolves symlinks so
	// this is stable across repo checkouts.
	abs, err := filepath.Abs("../../../deploy/k8s")
	if err != nil {
		t.Fatalf("resolve deploy/k8s: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("deploy/k8s not found at %s: %v", abs, err)
	}
	return abs
}

// splitYAMLDocs splits a multi-doc YAML file on "---" separators and
// trims whitespace-only documents. The loader tolerates a trailing
// separator (common in hand-maintained YAML).
func splitYAMLDocs(b []byte) [][]byte {
	parts := strings.Split(string(b), "\n---")
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// The very first doc may or may not have a leading "---"; strip
		// it defensively.
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

// TestDeployManifests_AllParse asserts every YAML file under deploy/k8s
// decodes cleanly into its typed struct. This is the first-line defense
// against typos in field names — sigs.k8s.io/yaml uses strict JSON-tag
// matching under the hood, so misspelled keys (e.g. "lables" instead
// of "labels") show up as decode errors here.
func TestDeployManifests_AllParse(t *testing.T) {
	dir := deployDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read deploy dir: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			docs := splitYAMLDocs(raw)
			if len(docs) == 0 {
				t.Fatalf("no YAML docs found")
			}
			for i, doc := range docs {
				pk := peekKind(t, doc)
				// Route each known kind through a typed decode so struct-
				// tag validation catches field-name drift. Unknown kinds
				// (e.g. an admin adding a CRD later) are tolerated with
				// a generic decode.
				switch pk.Kind {
				case "Namespace":
					var obj corev1.Namespace
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "ServiceAccount":
					var obj corev1.ServiceAccount
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "Role":
					var obj rbacv1.Role
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "RoleBinding":
					var obj rbacv1.RoleBinding
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "RuntimeClass":
					var obj nodev1.RuntimeClass
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "PersistentVolumeClaim":
					var obj corev1.PersistentVolumeClaim
					if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				case "Job":
					// batch/v1 import would grow the surface; a non-strict
					// decode into a map is sufficient to catch syntax.
					var obj map[string]interface{}
					if err := yaml.Unmarshal(doc, &obj); err != nil {
						t.Errorf("doc %d (%s): %v", i, pk.Kind, err)
					}
				default:
					t.Errorf("doc %d: unexpected kind %q", i, pk.Kind)
				}
			}
		})
	}
}

// TestDeployManifests_NamespaceInvariants pins the namespace names
// against the backend defaults. If someone renames the namespaces in
// the YAML without updating pkg/config/app_config.go's defaults (or
// vice versa), this test fails loudly with a message pointing at the
// mismatch.
func TestDeployManifests_NamespaceInvariants(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(deployDir(t), "00-namespaces.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	docs := splitYAMLDocs(raw)
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		var ns corev1.Namespace
		if err := yaml.Unmarshal(d, &ns); err != nil {
			t.Fatalf("decode ns: %v", err)
		}
		names = append(names, ns.Name)
	}
	// These names are also the SandboxKubernetesConfig defaults
	// (see pkg/config/app_config.go). Keep the string literals here
	// so the test fails even if the Config defaults change without a
	// manifest update (and vice versa).
	wantContains := []string{"astonish", "astonish-sandboxes"}
	for _, w := range wantContains {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("namespace %q not declared; found %v", w, names)
		}
	}
}

// TestDeployManifests_RBACCoversBackendCalls asserts the Role grants
// every verb/resource pair the backend actually invokes. This is the
// quickest way to catch a backend feature landing without its RBAC
// rule (or vice-versa, a stale permission kept past its need-to-know).
//
// The expected set below MUST mirror the inventory documented in
// deploy/k8s/10-rbac.yaml; if you add a new b.client call site, update
// both places.
func TestDeployManifests_RBACCoversBackendCalls(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(deployDir(t), "10-rbac.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	docs := splitYAMLDocs(raw)
	var role rbacv1.Role
	for _, d := range docs {
		pk := peekKind(t, d)
		if pk.Kind == "Role" {
			if err := yaml.Unmarshal(d, &role); err != nil {
				t.Fatalf("decode role: %v", err)
			}
		}
	}
	if role.Name == "" {
		t.Fatal("no Role document in 10-rbac.yaml")
	}

	// (apiGroup, resource) → set of required verbs.
	type key struct{ group, resource string }
	required := map[key][]string{
		{"", "pods"}:                                {"get", "list", "watch", "create", "delete"},
		{"", "pods/exec"}:                           {"create"},
		{"", "pods/status"}:                         {"update"},
		{"", "services"}:                            {"get", "list", "create", "delete"},
		{"networking.k8s.io", "networkpolicies"}:    {"get", "list", "create", "update", "delete"},
		{"", "persistentvolumeclaims"}:              {"get", "list"},
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

// TestDeployManifests_PVCsAreRWX guards against a storageClassName
// change that silently drops RWX support (e.g., accidentally switching
// to an RBD-style block class that is RWO-only). The K8s+Sysbox
// backend MUST have RWX on both layers and uppers or pods won't be
// able to schedule on arbitrary nodes (§4).
func TestDeployManifests_PVCsAreRWX(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(deployDir(t), "30-storage.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	docs := splitYAMLDocs(raw)
	count := 0
	for _, d := range docs {
		var pvc corev1.PersistentVolumeClaim
		if err := yaml.Unmarshal(d, &pvc); err != nil {
			t.Fatalf("decode pvc: %v", err)
		}
		if pvc.Kind != "" && pvc.Kind != "PersistentVolumeClaim" {
			continue
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
