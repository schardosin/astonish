package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/adk/tool"
)

// SyncToolsToMemory writes tool reference docs to memory/tools-ref/ for
// vector indexing. Each tool group gets a markdown file listing its tools
// with names, descriptions, and how to access them via delegate_tasks.
// Main-thread tools get a separate file. MCP toolsets are included.
//
// This enables semantic search to find the right tool when the user asks
// "save a fleet plan" or "take a screenshot" — the search returns the
// tool description and its group name, so the LLM knows to delegate.
//
// Files are only written when content changes (SHA-256 comparison).
// Orphaned files from removed groups are cleaned up.
func SyncToolsToMemory(memoryDir string, mainTools []tool.Tool, groups []*ToolGroup) error {
	toolsDir := filepath.Join(memoryDir, "tools-ref")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return fmt.Errorf("create tools-ref dir: %w", err)
	}

	written := make(map[string]bool)

	// Generate doc for main-thread tools
	if len(mainTools) > 0 {
		var sb strings.Builder
		sb.WriteString("# Main Thread Tools\n\n")
		sb.WriteString("These tools are directly available — no delegation needed.\n")
		sb.WriteString("Call them directly from the main agent.\n\n")
		for _, t := range mainTools {
			sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", t.Name(), t.Description()))
		}
		filename := "_main-thread.md"
		written[filename] = true
		writeIfChanged(filepath.Join(toolsDir, filename), sb.String())
	}

	// Generate doc for each tool group
	// Use a minimal read-only context for resolving MCP toolset tools
	ctx := &minimalReadonlyContext{Context: context.Background()}

	for _, g := range groups {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Tool Group: %s\n\n", g.Name))
		sb.WriteString(fmt.Sprintf("%s\n\n", g.Description))
		sb.WriteString(fmt.Sprintf("Access via: `delegate_tasks` with `tools: [\"%s\"]`\n\n", g.Name))

		// Regular tools
		for _, t := range g.Tools {
			sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", t.Name(), t.Description()))
		}

		// MCP toolset tools
		for _, ts := range g.Toolsets {
			mcpTools, err := ts.Tools(ctx)
			if err != nil {
				continue
			}
			for _, t := range mcpTools {
				sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", t.Name(), t.Description()))
			}
		}

		filename := g.Name + ".md"
		// Sanitize filename for MCP groups like "mcp:github"
		filename = strings.ReplaceAll(filename, ":", "_")
		written[filename] = true
		writeIfChanged(filepath.Join(toolsDir, filename), sb.String())
	}

	// Remove orphaned files
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !written[e.Name()] && strings.HasSuffix(e.Name(), ".md") {
			os.Remove(filepath.Join(toolsDir, e.Name()))
		}
	}

	return nil
}

// writeIfChanged writes content to path only if it differs from the existing
// file (SHA-256 comparison). Returns true if the file was written.
func writeIfChanged(path, content string) bool {
	existing, err := os.ReadFile(path)
	if err == nil {
		existingHash := fmt.Sprintf("%x", sha256.Sum256(existing))
		newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		if existingHash == newHash {
			return false
		}
	}
	_ = os.WriteFile(path, []byte(content), 0644)
	return true
}

// SortedGroups returns tool groups sorted by name for deterministic output.
func SortedGroups(groups map[string]*ToolGroup) []*ToolGroup {
	sorted := make([]*ToolGroup, 0, len(groups))
	for _, g := range groups {
		sorted = append(sorted, g)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}
