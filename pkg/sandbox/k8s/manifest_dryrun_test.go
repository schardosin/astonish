//go:build integration

// Package k8s — integration test that runs `kubectl apply --dry-run=server`
// against the real cluster pointed at by the ambient KUBECONFIG.
//
// This file is opt-in via `go test -tags integration`. The default
// `go test ./...` pass skips it entirely, so CI without cluster access
// (or dev laptops) aren't penalised.
//
// To run:
//
//	KUBECONFIG=~/.kube/config \
//	    go test -tags integration -run TestDeployManifests_KubectlServerDryRun \
//	    ./pkg/sandbox/k8s
//
// Failure surfaces kubectl's full stderr so typos like an unknown
// storageClassName or a RuntimeClass referencing a non-installed
// handler are immediately obvious.

package k8s

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeployManifests_KubectlServerDryRun shells out to kubectl and
// submits each deploy manifest for server-side validation. The cluster
// evaluates admission controllers, defaulting, and schema — catching
// every class of error the in-process TestDeployManifests_AllParse
// cannot (e.g., a forbidden fieldPath under pod-security: restricted,
// or a missing CRD definition on the target cluster).
//
// A missing kubectl or KUBECONFIG is surfaced as a skipped-with-reason
// rather than a failure, since developers may forget to set it.
func TestDeployManifests_KubectlServerDryRun(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not in PATH; install kubectl or unset -tags=integration")
	}
	if os.Getenv("KUBECONFIG") == "" {
		// Still allow if the default ~/.kube/config is present.
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, ".kube/config")); err != nil {
			t.Skip("no KUBECONFIG and no ~/.kube/config; cannot reach cluster")
		}
	}

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
			path := filepath.Join(dir, name)
			// --dry-run=server forces the apiserver to run admission
			// and defaulting without persisting. --validate=true is the
			// default on modern kubectl, stated explicitly for clarity.
			cmd := exec.Command(
				"kubectl", "apply",
				"--dry-run=server",
				"--validate=true",
				"-f", path,
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf(
					"kubectl apply failed for %s:\nstdout:\n%s\nstderr:\n%s\nerr: %v",
					name, stdout.String(), stderr.String(), err,
				)
			}
		})
	}
}
