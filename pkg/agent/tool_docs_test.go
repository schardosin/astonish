package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncToolsToMemory(t *testing.T) {
	tmpDir := t.TempDir()

	mainTools := mockTools("read_file", "shell_command")

	groups := []*ToolGroup{
		{
			Name:        "fleet",
			Description: "Create and validate fleet plans",
			Tools:       mockTools("save_fleet_plan", "validate_fleet_plan"),
		},
		{
			Name:        "browser",
			Description: "Web automation, screenshots, form filling",
			Tools:       mockTools("browser_navigate", "browser_click"),
		},
	}

	err := SyncToolsToMemory(tmpDir, mainTools, groups)
	if err != nil {
		t.Fatal(err)
	}

	// Check files were created
	toolsDir := filepath.Join(tmpDir, "tools-ref")
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		t.Fatal(err)
	}

	fileNames := make(map[string]bool)
	for _, e := range entries {
		fileNames[e.Name()] = true
	}

	if !fileNames["_main-thread.md"] {
		t.Error("Expected _main-thread.md")
	}
	if !fileNames["fleet.md"] {
		t.Error("Expected fleet.md")
	}
	if !fileNames["browser.md"] {
		t.Error("Expected browser.md")
	}
	if len(entries) != 3 {
		t.Errorf("Expected 3 files, got %d", len(entries))
	}

	// Check fleet.md content
	content, err := os.ReadFile(filepath.Join(toolsDir, "fleet.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, "save_fleet_plan") {
		t.Error("fleet.md should contain save_fleet_plan")
	}
	if !strings.Contains(s, `delegate_tasks`) {
		t.Error("fleet.md should mention delegate_tasks")
	}
	if !strings.Contains(s, `tools: ["fleet"]`) {
		t.Error("fleet.md should show how to access via delegate_tasks")
	}

	// Check idempotent re-sync (no changes)
	err = SyncToolsToMemory(tmpDir, mainTools, groups)
	if err != nil {
		t.Fatal("re-sync failed:", err)
	}

	// Check orphan cleanup: remove browser group and re-sync
	groups = groups[:1] // only fleet
	err = SyncToolsToMemory(tmpDir, mainTools, groups)
	if err != nil {
		t.Fatal(err)
	}
	entries2, _ := os.ReadDir(toolsDir)
	for _, e := range entries2 {
		if e.Name() == "browser.md" {
			t.Error("browser.md should have been cleaned up as orphan")
		}
	}
	if len(entries2) != 2 {
		t.Errorf("After orphan cleanup, expected 2 files, got %d", len(entries2))
	}
}

func TestSyncToolsToMemory_MCPGroupName(t *testing.T) {
	tmpDir := t.TempDir()

	groups := []*ToolGroup{
		{
			Name:        "mcp:github",
			Description: "MCP server: github (5 tools)",
			Tools:       mockTools("github_list_repos"),
		},
	}

	err := SyncToolsToMemory(tmpDir, nil, groups)
	if err != nil {
		t.Fatal(err)
	}

	// MCP group name "mcp:github" should be sanitized to "mcp_github.md"
	toolsDir := filepath.Join(tmpDir, "tools-ref")
	entries, _ := os.ReadDir(toolsDir)
	found := false
	for _, e := range entries {
		if e.Name() == "mcp_github.md" {
			found = true
		}
	}
	if !found {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("Expected mcp_github.md, found: %v", names)
	}
}
