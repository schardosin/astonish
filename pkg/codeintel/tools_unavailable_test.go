package codeintel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/codeintel/internal/treesitter"
)

func TestRepoMap_LibraryMissingReturnsUnavailable(t *testing.T) {
	treesitter.ResetDefaultLibraryForTest()
	t.Cleanup(treesitter.ResetDefaultLibraryForTest)

	missing := filepath.Join(t.TempDir(), "does-not-exist.so")
	t.Setenv("ASTONISH_TREESITTER_LIB", missing)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := RepoMap(nil, RepoMapArgs{Path: root})
	if err == nil {
		t.Fatal("expected error when tree-sitter library is missing")
	}
	if !errors.Is(err, treesitter.ErrLibraryUnavailable) {
		t.Fatalf("expected ErrLibraryUnavailable, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "grep_search") || !strings.Contains(msg, "find_files") {
		t.Fatalf("error should steer agent to text tools, got %q", msg)
	}

	_, buildErr := Build(context.Background(), root)
	if buildErr == nil {
		t.Fatal("expected Build error when library is missing")
	}
	if !errors.Is(buildErr, treesitter.ErrLibraryUnavailable) {
		t.Fatalf("Build: expected ErrLibraryUnavailable, got %v", buildErr)
	}
}
