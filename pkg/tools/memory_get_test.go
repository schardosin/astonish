package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryGet_BasicRead(t *testing.T) {
	tmpDir := t.TempDir()
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	handler := MemoryGet(tmpDir)
	result, err := handler(nil, MemoryGetArgs{Path: "test.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalLines != 6 { // 5 lines + trailing newline = 6
		t.Errorf("expected 6 total lines, got %d", result.TotalLines)
	}
	if result.From != 1 {
		t.Errorf("expected from=1, got %d", result.From)
	}
}

func TestMemoryGet_LineRange(t *testing.T) {
	tmpDir := t.TempDir()
	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	if err := os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	handler := MemoryGet(tmpDir)
	result, err := handler(nil, MemoryGetArgs{Path: "test.md", From: 2, Lines: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "line 2\nline 3" {
		t.Errorf("expected 'line 2\\nline 3', got %q", result.Content)
	}
	if result.From != 2 {
		t.Errorf("expected from=2, got %d", result.From)
	}
	if result.To != 3 { // lines 2 and 3 → last line is 3
		t.Errorf("expected to=3, got %d", result.To)
	}
}

func TestMemoryGet_SubdirectoryFile(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "projects")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "test.md"), []byte("project content"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := MemoryGet(tmpDir)
	result, err := handler(nil, MemoryGetArgs{Path: "projects/test.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "project content" {
		t.Errorf("expected 'project content', got %q", result.Content)
	}
}

func TestMemoryGet_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	handler := MemoryGet(tmpDir)
	_, err := handler(nil, MemoryGetArgs{Path: "nonexistent.md"})
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestMemoryGet_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	handler := MemoryGet(tmpDir)
	_, err := handler(nil, MemoryGetArgs{Path: "../../etc/passwd"})
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
}

func TestMemoryGet_EmptyPath(t *testing.T) {
	tmpDir := t.TempDir()

	handler := MemoryGet(tmpDir)
	_, err := handler(nil, MemoryGetArgs{Path: ""})
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestMemoryGet_BeyondEOF(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "short.md"), []byte("one\ntwo"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := MemoryGet(tmpDir)
	result, err := handler(nil, MemoryGetArgs{Path: "short.md", From: 100, Lines: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "" {
		t.Errorf("expected empty content when starting beyond EOF, got %q", result.Content)
	}
}
