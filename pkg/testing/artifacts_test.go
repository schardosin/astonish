package testing

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewArtifactManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, err := NewArtifactManager(tmpDir, "myapp")
	if err != nil {
		t.Fatalf("NewArtifactManager: %v", err)
	}

	if !strings.HasPrefix(am.Dir(), tmpDir) {
		t.Errorf("Dir = %q, should start with %q", am.Dir(), tmpDir)
	}

	// Verify directory was created
	info, err := os.Stat(am.Dir())
	if err != nil {
		t.Fatalf("stat artifact dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("artifact dir should be a directory")
	}
}

func TestSaveLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")
	content := "command output line 1\ncommand output line 2\n"

	path, err := am.SaveLog("check_status", content)
	if err != nil {
		t.Fatalf("SaveLog: %v", err)
	}

	if !strings.HasSuffix(path, "check_status_output.log") {
		t.Errorf("path = %q, should end with check_status_output.log", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", string(data), content)
	}
}

func TestSaveScreenshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")

	// Create fake PNG data
	original := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	b64 := base64.StdEncoding.EncodeToString(original)

	path, err := am.SaveScreenshot("login_page", b64, "png")
	if err != nil {
		t.Fatalf("SaveScreenshot: %v", err)
	}

	if !strings.HasSuffix(path, "login_page_post.png") {
		t.Errorf("path = %q, should end with login_page_post.png", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	if len(data) != len(original) {
		t.Errorf("data length = %d, want %d", len(data), len(original))
	}
}

func TestSaveScreenshotDefaultFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")
	b64 := base64.StdEncoding.EncodeToString([]byte("fake"))

	path, err := am.SaveScreenshot("step1", b64, "")
	if err != nil {
		t.Fatalf("SaveScreenshot: %v", err)
	}
	if !strings.HasSuffix(path, "step1_post.png") {
		t.Errorf("default format should be png, got path %q", path)
	}
}

func TestSaveScreenshotInvalidBase64(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")
	_, err = am.SaveScreenshot("step1", "not-valid-base64!!!", "png")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestSaveSetupLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "artifact-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	am, _ := NewArtifactManager(tmpDir, "test")
	path, err := am.SaveSetupLog("starting app...\nlistening on :3000\n")
	if err != nil {
		t.Fatalf("SaveSetupLog: %v", err)
	}

	if filepath.Base(path) != "setup_output.log" {
		t.Errorf("filename = %q, want setup_output.log", filepath.Base(path))
	}
}
