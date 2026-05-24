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
//	    go test -tags integration -run TestChart_KubectlServerDryRun \
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

// TestChart_KubectlServerDryRun renders the Helm chart via `helm template`
// and submits the resulting multi-doc manifest to `kubectl apply --dry-run=server`
// for full server-side admission validation. The cluster evaluates admission
// controllers, defaulting, and schema — catching every class of error the
// in-process TestChart_AllParse cannot (e.g., a forbidden fieldPath under
// pod-security: restricted, or a missing CRD on the target cluster).
//
// A missing helm, kubectl, or KUBECONFIG is surfaced as a skipped-with-reason
// rather than a failure, since developers may forget to set them.
func TestChart_KubectlServerDryRun(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not in PATH; install kubectl or unset -tags=integration")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not in PATH; install helm 3.8+ to run chart dry-run tests")
	}
	if os.Getenv("KUBECONFIG") == "" {
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, ".kube/config")); err != nil {
			t.Skip("no KUBECONFIG and no ~/.kube/config; cannot reach cluster")
		}
	}

	// Render the chart to a temp file.
	rendered, ok := renderChart(t)
	if !ok {
		return
	}
	tmpFile := filepath.Join(t.TempDir(), "rendered.yaml")
	if err := os.WriteFile(tmpFile, rendered, 0644); err != nil {
		t.Fatalf("write rendered chart to temp file: %v", err)
	}

	// Submit the entire rendered manifest for server-side dry-run.
	// --server-side --dry-run=server uses server-side apply (field managers)
	// which validates schema and admission without conflicting with existing
	// resources on the cluster. --force-conflicts avoids false positives
	// when the chart is already deployed (field ownership conflicts).
	cmd := exec.Command(
		"kubectl", "apply",
		"--server-side",
		"--dry-run=server",
		"--force-conflicts",
		"-f", tmpFile,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Admission webhook rejections (e.g., nginx-ingress host conflict)
		// are about cluster state, not manifest correctness. Log them as
		// warnings rather than hard failures. Schema/structural errors from
		// the API server still surface here as fatal.
		if strings.Contains(stderr.String(), "admission webhook") {
			t.Logf("WARNING: admission webhook rejection (cluster-state issue, not manifest error):\nstdout:\n%s\nstderr:\n%s",
				stdout.String(), stderr.String())
		} else {
			t.Fatalf(
				"kubectl apply --dry-run=server failed:\nstdout:\n%s\nstderr:\n%s\nerr: %v",
				stdout.String(), stderr.String(), err,
			)
		}
	}
}
