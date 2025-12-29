package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileTree(t *testing.T) {
	// Create a temp directory structure for testing
	tmpDir, err := os.MkdirTemp("", "filetree_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test structure
	os.MkdirAll(filepath.Join(tmpDir, "src", "pkg"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "node_modules", "dep"), 0755) // Should be excluded
	os.MkdirAll(filepath.Join(tmpDir, ".git", "objects"), 0755)     // Should be excluded
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "app.go"), []byte("package src"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "pkg", "util.go"), []byte("package pkg"), 0644)

	t.Run("BasicFileTree", func(t *testing.T) {
		result, err := FileTree(nil, FileTreeArgs{
			Path:     tmpDir,
			MaxDepth: 3,
		})
		if err != nil {
			t.Fatalf("FileTree failed: %v", err)
		}

		if result.Root != tmpDir {
			t.Errorf("Expected root %s, got %s", tmpDir, result.Root)
		}

		// Check that node_modules is excluded
		for _, entry := range result.Entries {
			if entry.Name == "node_modules" {
				t.Error("node_modules should be excluded from tree")
			}
		}

		if result.Total == 0 {
			t.Error("Expected some files in tree")
		}
	})

	t.Run("DepthLimiting", func(t *testing.T) {
		result, err := FileTree(nil, FileTreeArgs{
			Path:     tmpDir,
			MaxDepth: 1,
		})
		if err != nil {
			t.Fatalf("FileTree failed: %v", err)
		}

		// With max depth 1, all entries should have depth 0
		for _, entry := range result.Entries {
			if entry.Depth >= 1 {
				t.Errorf("Depth limiting not working - found entry at depth %d", entry.Depth)
			}
		}
	})
}


func TestGrepSearch(t *testing.T) {
	// Create temp directory with test files
	tmpDir, err := os.MkdirTemp("", "grep_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package main\n\nfunc helper() {\n\tfmt.Println(\"helper function\")\n}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello world\nthis is a test\nhello again"), 0644)

	t.Run("BasicSearch", func(t *testing.T) {
		result, err := GrepSearch(nil, GrepSearchArgs{
			Pattern:    "hello",
			SearchPath: tmpDir,
		})
		if err != nil {
			t.Fatalf("GrepSearch failed: %v", err)
		}

		if result.Total == 0 {
			t.Error("Expected to find matches for 'hello'")
		}
	})

	t.Run("CaseSensitiveSearch", func(t *testing.T) {
		resultInsensitive, _ := GrepSearch(nil, GrepSearchArgs{
			Pattern:       "HELLO",
			SearchPath:    tmpDir,
			CaseSensitive: false,
		})

		resultSensitive, _ := GrepSearch(nil, GrepSearchArgs{
			Pattern:       "HELLO",
			SearchPath:    tmpDir,
			CaseSensitive: true,
		})

		if resultSensitive.Total >= resultInsensitive.Total && resultInsensitive.Total > 0 {
			t.Error("Case sensitive search should return fewer results than insensitive")
		}
	})

	t.Run("IncludeGlobs", func(t *testing.T) {
		result, err := GrepSearch(nil, GrepSearchArgs{
			Pattern:      "hello",
			SearchPath:   tmpDir,
			IncludeGlobs: []string{"*.go"},
		})
		if err != nil {
			t.Fatalf("GrepSearch failed: %v", err)
		}

		// Should only find matches in .go files
		for _, match := range result.Matches {
			if filepath.Ext(match.File) != ".go" {
				t.Errorf("Found match in non-.go file: %s", match.File)
			}
		}
	})

	t.Run("MaxResults", func(t *testing.T) {
		result, err := GrepSearch(nil, GrepSearchArgs{
			Pattern:    "hello",
			SearchPath: tmpDir,
			MaxResults: 1,
		})
		if err != nil {
			t.Fatalf("GrepSearch failed: %v", err)
		}

		if result.Total > 1 {
			t.Errorf("MaxResults not respected: got %d results", result.Total)
		}
	})
}

func TestFindFiles(t *testing.T) {
	// Create temp directory with test files
	tmpDir, err := os.MkdirTemp("", "findfiles_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test structure
	os.MkdirAll(filepath.Join(tmpDir, "src", "pkg"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "app.go"), []byte("package src"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "app_test.go"), []byte("package src"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "node_modules", "index.js"), []byte("module.exports = {}"), 0644)

	t.Run("GlobPattern", func(t *testing.T) {
		result, err := FindFiles(nil, FindFilesArgs{
			Pattern:    "*.go",
			SearchPath: tmpDir,
		})
		if err != nil {
			t.Fatalf("FindFiles failed: %v", err)
		}

		if result.Total == 0 {
			t.Error("Expected to find .go files")
		}

		for _, file := range result.Files {
			if filepath.Ext(file.Path) != ".go" {
				t.Errorf("Found non-.go file: %s", file.Path)
			}
		}
	})

	t.Run("TestFilePattern", func(t *testing.T) {
		result, err := FindFiles(nil, FindFilesArgs{
			Pattern:    "*_test.go",
			SearchPath: tmpDir,
		})
		if err != nil {
			t.Fatalf("FindFiles failed: %v", err)
		}

		if result.Total != 2 {
			t.Errorf("Expected 2 test files, got %d", result.Total)
		}
	})

	t.Run("ExcludesNodeModules", func(t *testing.T) {
		result, err := FindFiles(nil, FindFilesArgs{
			Pattern:    "*.js",
			SearchPath: tmpDir,
		})
		if err != nil {
			t.Fatalf("FindFiles failed: %v", err)
		}

		// node_modules should be excluded, so no .js files should be found
		if result.Total > 0 {
			t.Error("Should not find files in node_modules")
		}
	})

	t.Run("MaxResults", func(t *testing.T) {
		result, err := FindFiles(nil, FindFilesArgs{
			Pattern:    "*.go",
			SearchPath: tmpDir,
			MaxResults: 2,
		})
		if err != nil {
			t.Fatalf("FindFiles failed: %v", err)
		}

		if result.Total > 2 {
			t.Errorf("MaxResults not respected: got %d", result.Total)
		}
	})
}
