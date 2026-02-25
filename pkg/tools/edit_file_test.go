package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	return path
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}
	return string(data)
}

func TestEditFile_ExactSingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "Hello World\nGoodbye World\n")

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "Hello",
		NewString: "Hi",
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.Replacements != 1 {
		t.Errorf("Replacements = %d, want 1", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "Hi World\nGoodbye World\n" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_ExactMultipleMatchError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "foo bar foo baz")

	_, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "foo",
		NewString: "qux",
	})
	if err == nil {
		t.Fatal("expected error for multiple matches without replace_all, got nil")
	}
}

func TestEditFile_ExactReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "foo bar foo baz foo")

	result, err := EditFile(nil, EditFileArgs{
		Path:       path,
		OldString:  "foo",
		NewString:  "qux",
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 3 {
		t.Errorf("Replacements = %d, want 3", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "qux bar qux baz qux" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_ExactNotFound(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "Hello World")

	_, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "nonexistent",
		NewString: "replacement",
	})
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
}

func TestEditFile_ExactDeletion(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "keep this remove this keep this too")

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: " remove this",
		NewString: "",
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("Replacements = %d, want 1", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "keep this keep this too" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_RegexSingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.go", "func oldName() {\n}\n")

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: `func (\w+)\(\)`,
		NewString: "func newName()",
		Regex:     true,
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("Replacements = %d, want 1", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "func newName() {\n}\n" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_RegexCaptureGroups(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "version=1.2.3")

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: `version=(\d+)\.(\d+)\.(\d+)`,
		NewString: "version=$1.$2.99",
		Regex:     true,
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("Replacements = %d, want 1", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "version=1.2.99" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_RegexReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "  foo  \n  bar  \n  baz  \n")

	result, err := EditFile(nil, EditFileArgs{
		Path:       path,
		OldString:  `(?m)^\s+`,
		NewString:  "",
		Regex:      true,
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 3 {
		t.Errorf("Replacements = %d, want 3", result.Replacements)
	}

	got := readTestFile(t, path)
	if got != "foo  \nbar  \nbaz  \n" {
		t.Errorf("file content = %q", got)
	}
}

func TestEditFile_RegexMultipleMatchError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "abc 123 def 456")

	_, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: `\d+`,
		NewString: "NUM",
		Regex:     true,
	})
	if err == nil {
		t.Fatal("expected error for multiple regex matches without replace_all, got nil")
	}
}

func TestEditFile_RegexNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello world")

	_, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: `\d+`,
		NewString: "NUM",
		Regex:     true,
	})
	if err == nil {
		t.Fatal("expected error for no regex match, got nil")
	}
}

func TestEditFile_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello world")

	_, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: `[invalid`,
		NewString: "x",
		Regex:     true,
	})
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestEditFile_EmptyPathError(t *testing.T) {
	_, err := EditFile(nil, EditFileArgs{
		Path:      "",
		OldString: "x",
		NewString: "y",
	})
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestEditFile_EmptyOldStringError(t *testing.T) {
	_, err := EditFile(nil, EditFileArgs{
		Path:      "/tmp/doesnt_matter",
		OldString: "",
		NewString: "y",
	})
	if err == nil {
		t.Fatal("expected error for empty old_string, got nil")
	}
}

func TestEditFile_NonexistentFile(t *testing.T) {
	_, err := EditFile(nil, EditFileArgs{
		Path:      filepath.Join(t.TempDir(), "nope.txt"),
		OldString: "x",
		NewString: "y",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestEditFile_MultilineExact(t *testing.T) {
	dir := t.TempDir()
	content := "func old() {\n\treturn 1\n}\n\nfunc keep() {\n\treturn 2\n}\n"
	path := writeTestFile(t, dir, "test.go", content)

	result, err := EditFile(nil, EditFileArgs{
		Path:      path,
		OldString: "func old() {\n\treturn 1\n}",
		NewString: "func new() {\n\treturn 42\n}",
	})
	if err != nil {
		t.Fatalf("EditFile() error = %v", err)
	}
	if result.Replacements != 1 {
		t.Errorf("Replacements = %d, want 1", result.Replacements)
	}

	got := readTestFile(t, path)
	expected := "func new() {\n\treturn 42\n}\n\nfunc keep() {\n\treturn 2\n}\n"
	if got != expected {
		t.Errorf("file content = %q, want %q", got, expected)
	}
}
