package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSmallGoRepo(t *testing.T) {
	_ = requireTestLibrary(t)

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

func TestBuildSmallPythonRepo(t *testing.T) {
	_ = requireTestLibrary(t)
	root := t.TempDir()
	source := `def add(a, b):
    return a + b

def use():
    return add(1, 2)
`
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Index.Definitions("add", "", "")) == 0 {
		t.Fatal("expected python function definition for add")
	}
	if len(result.Index.References("add", "", "")) == 0 {
		t.Fatal("expected python reference to add")
	}
}

func TestBuildSmallJavaScriptRepo(t *testing.T) {
	_ = requireTestLibrary(t)
	root := t.TempDir()
	source := `function add(a, b) {
  return a + b;
}

function use() {
  return add(1, 2);
}
`
	if err := os.WriteFile(filepath.Join(root, "main.js"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Index.Definitions("add", "", "")) == 0 {
		t.Fatal("expected javascript function definition for add")
	}
	if len(result.Index.References("add", "", "")) == 0 {
		t.Fatal("expected javascript reference to add")
	}
}

func TestBuildSmallTypeScriptRepo(t *testing.T) {
	_ = requireTestLibrary(t)
	root := t.TempDir()
	source := `function add(a: number, b: number): number {
  return a + b;
}

function use(): number {
  return add(1, 2);
}
`
	if err := os.WriteFile(filepath.Join(root, "main.ts"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Index.Definitions("add", "", "")) == 0 {
		t.Fatal("expected typescript function definition for add")
	}
	if len(result.Index.References("add", "", "")) == 0 {
		t.Fatal("expected typescript reference to add")
	}
}

func requireTestLibrary(t *testing.T) string {
	t.Helper()
	libPath := resolveTestLibraryPath()
	if libPath == "" {
		t.Skip("tree-sitter library not available")
	}
	t.Setenv("ASTONISH_TREESITTER_LIB", libPath)
	// Ensure we load the resolved path even if an earlier test cached a miss.
	treesitterResetForSmoke()
	return libPath
}
