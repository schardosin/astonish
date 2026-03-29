package tools

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	chromem "github.com/philippgille/chromem-go"
	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/tool"
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

// --- search_tools tests ---

// testToolEmbeddingFunc creates a bag-of-words embedding function for tests.
func testToolEmbeddingFunc() chromem.EmbeddingFunc {
	return func(_ context.Context, text string) ([]float32, error) {
		vec := make([]float32, 384)
		words := testToolSplitWords(text)
		for _, word := range words {
			h := sha256.Sum256([]byte(word))
			for i := 0; i < 8; i++ {
				dim := int(binary.LittleEndian.Uint16(h[i*2:])) % 384
				vec[dim] += 1.0
			}
		}
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for i := range vec {
				vec[i] = float32(float64(vec[i]) / norm)
			}
		}
		return vec, nil
	}
}

func testToolSplitWords(s string) []string {
	var words []string
	current := ""
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			current += string(r)
		} else {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

type searchToolsMockTool struct {
	name string
	desc string
}

func (m searchToolsMockTool) Name() string        { return m.name }
func (m searchToolsMockTool) Description() string { return m.desc }
func (m searchToolsMockTool) IsLongRunning() bool { return false }

func testToolIndex(t *testing.T) *agent.ToolIndex {
	t.Helper()
	db := chromem.NewDB()
	idx, err := agent.NewToolIndex(db, testToolEmbeddingFunc())
	if err != nil {
		t.Fatalf("NewToolIndex: %v", err)
	}

	mainTools := []tool.Tool{
		searchToolsMockTool{name: "read_file", desc: "Read contents of a file from disk"},
		searchToolsMockTool{name: "write_file", desc: "Write content to a file on disk"},
	}

	groups := []*agent.ToolGroup{
		{
			Name:        "browser",
			Description: "Web automation, screenshots, form filling",
			Tools: []tool.Tool{
				searchToolsMockTool{name: "browser_navigate", desc: "Navigate the browser to a URL"},
				searchToolsMockTool{name: "browser_take_screenshot", desc: "Capture a screenshot of the current browser page"},
			},
		},
		{
			Name:        "web",
			Description: "HTTP requests and web fetching",
			Tools: []tool.Tool{
				searchToolsMockTool{name: "http_request", desc: "Make an HTTP request to an API endpoint"},
			},
		},
	}

	if err := idx.SyncTools(context.Background(), mainTools, groups); err != nil {
		t.Fatalf("SyncTools: %v", err)
	}
	return idx
}

func TestSearchTools_EmptyQuery(t *testing.T) {
	idx := testToolIndex(t)
	fn := SearchTools(idx, nil)

	_, err := fn(nil, SearchToolsArgs{Query: ""})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestSearchTools_ReturnsResults(t *testing.T) {
	idx := testToolIndex(t)
	fn := SearchTools(idx, nil)

	result, err := fn(nil, SearchToolsArgs{Query: "screenshot browser page", MaxResults: 5})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}

	if result.Count == 0 {
		t.Fatal("expected at least one match")
	}

	for _, m := range result.Matches {
		if m.ToolName == "" {
			t.Error("match has empty tool name")
		}
		if m.GroupName == "" {
			t.Error("match has empty group name")
		}
		if m.Access == "" {
			t.Error("match has empty access instructions")
		}
		if m.Score <= 0 {
			t.Errorf("match %s has non-positive score", m.ToolName)
		}
	}
}

func TestSearchTools_AccessField(t *testing.T) {
	idx := testToolIndex(t)
	fn := SearchTools(idx, nil)

	result, err := fn(nil, SearchToolsArgs{Query: "file read write", MaxResults: 10})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}

	for _, m := range result.Matches {
		if m.IsMainTool {
			if m.Access != "always available (main thread tool)" {
				t.Errorf("main tool %s should have always available access, got: %s", m.ToolName, m.Access)
			}
		} else {
			if m.Access == "" || m.Access == "always available (main thread tool)" {
				t.Errorf("injected tool %s should have 'available (call directly)' access, got: %s", m.ToolName, m.Access)
			}
		}
	}
}

func TestSearchTools_NoResults(t *testing.T) {
	db := chromem.NewDB()
	idx, err := agent.NewToolIndex(db, testToolEmbeddingFunc())
	if err != nil {
		t.Fatalf("NewToolIndex: %v", err)
	}

	fn := SearchTools(idx, nil)
	result, err := fn(nil, SearchToolsArgs{Query: "anything"})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("expected 0 results, got %d", result.Count)
	}
	if result.Message == "" {
		t.Error("expected a message for no results")
	}
}

func TestNewSearchToolsTool(t *testing.T) {
	idx := testToolIndex(t)
	st, err := NewSearchToolsTool(idx, nil)
	if err != nil {
		t.Fatalf("NewSearchToolsTool: %v", err)
	}
	if st.Name() != "search_tools" {
		t.Errorf("expected name 'search_tools', got %q", st.Name())
	}
}

func TestSearchTools_ListAll(t *testing.T) {
	idx := testToolIndex(t)
	fn := SearchTools(idx, nil)

	result, err := fn(nil, SearchToolsArgs{Query: "*"})
	if err != nil {
		t.Fatalf("SearchTools list all: %v", err)
	}

	// testToolIndex has 5 tools: 2 main + 2 browser + 1 web
	if result.Count != 5 {
		t.Errorf("expected 5 tools in full inventory, got %d", result.Count)
	}

	if result.Message == "" {
		t.Error("expected a summary message for list-all")
	}

	// Verify all tools have score 1.0 (inventory mode, not search)
	for _, m := range result.Matches {
		if m.Score != 1.0 {
			t.Errorf("list-all tool %s should have score 1.0, got %f", m.ToolName, m.Score)
		}
	}

	// Verify access instructions are correct
	for _, m := range result.Matches {
		if m.IsMainTool && m.Access != "always available (main thread tool)" {
			t.Errorf("main tool %s should have always available access, got: %s", m.ToolName, m.Access)
		}
		if !m.IsMainTool && m.Access == "always available (main thread tool)" {
			t.Errorf("injected tool %s should not have always available access", m.ToolName)
		}
	}
}

func TestSearchTools_ListAllVariants(t *testing.T) {
	idx := testToolIndex(t)
	fn := SearchTools(idx, nil)

	queries := []string{"*", "list all", "list all tools", "all", "all tools"}
	for _, q := range queries {
		result, err := fn(nil, SearchToolsArgs{Query: q})
		if err != nil {
			t.Fatalf("SearchTools(%q): %v", q, err)
		}
		if result.Count != 5 {
			t.Errorf("SearchTools(%q): expected 5 tools, got %d", q, result.Count)
		}
	}
}
