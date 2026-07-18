package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSmallGoRepo(t *testing.T) {
	libPath := resolveTestLibraryPath()
	if libPath == "" {
		t.Skip("tree-sitter library not available")
	}
	t.Setenv("ASTONISH_TREESITTER_LIB", libPath)

	root := t.TempDir()
	source := `package sample

func Add(a int, b int) int {
	return a + b
}

func Use() int {
	return Add(1, 2)
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defs := result.Index.Definitions("Add", "", "")
	if len(defs) != 1 {
		t.Fatalf("expected one Add definition, got %d", len(defs))
	}
	refs := result.Index.References("Add", "", "")
	if len(refs) == 0 {
		t.Fatalf("expected Add reference")
	}
}
