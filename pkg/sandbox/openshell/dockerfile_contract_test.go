package openshell

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOpenShellDockerfileIncludesRipgrep locks the packaging contract that
// grep_search / find_files require ripgrep inside the OpenShell sandbox image
// rootfs (tools run there without an Astonish overlay chroot).
func TestOpenShellDockerfileIncludesRipgrep(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// pkg/sandbox/openshell → repo root
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../.."))
	dockerfile := filepath.Join(repoRoot, "docker/sandbox-openshell/Dockerfile")
	data, err := os.ReadFile(dockerfile)
	if err != nil {
		t.Fatalf("read %s: %v", dockerfile, err)
	}
	content := string(data)
	if !strings.Contains(content, "ripgrep") {
		t.Fatalf("%s must install ripgrep for grep_search/find_files", dockerfile)
	}
}
