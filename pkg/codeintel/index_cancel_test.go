package codeintel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBuild_CancelledContextDoesNotPoisonCache(t *testing.T) {
	libPath := resolveTestLibraryPath()
	if libPath == "" {
		t.Skip("tree-sitter library not available")
	}
	t.Setenv("ASTONISH_TREESITTER_LIB", libPath)

	root := t.TempDir()
	src := "package sample\n\nfunc Foo() {}\n"
	for i := 0; i < 20; i++ {
		path := filepath.Join(root, fmt.Sprintf("pkg%d.go", i))
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Build(ctx, root)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result on cancel, got %+v", result)
	}

	files, enumErr := enumerateFiles(context.Background(), root)
	if enumErr != nil {
		t.Fatalf("enumerateFiles: %v", enumErr)
	}
	if _, ok := loadDiskCache(root, files); ok {
		t.Fatal("cancelled build poisoned disk cache: loadDiskCache returned a hit")
	}
	if _, err := os.Stat(cachePath(root)); err == nil {
		t.Fatal("cancelled build wrote .codeintel/index.json")
	}
}
