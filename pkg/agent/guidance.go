package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GuidanceDocs maps guidance document names to their markdown content.
// Each entry is written to memory/guidance/{name}.md at startup and indexed
// into the vector store by the existing memory file watcher.
var GuidanceDocs = map[string]string{
	"browser-automation":    guidanceBrowserAutomation,
	"credential-management": guidanceCredentialManagement,
	"job-scheduling":        guidanceJobScheduling,
	"task-delegation":       guidanceTaskDelegation,
	"process-management":    guidanceProcessManagement,
	"web-access":            guidanceWebAccess,
	"memory-usage":          guidanceMemoryUsage,
	"sandbox-templates":     guidanceSandboxTemplates,
}

// SyncGuidanceToMemory writes guidance docs to memory/guidance/.
// Files are only written when content changes (SHA-256 comparison).
// Orphaned guidance files (from removed docs) are cleaned up.
// This mirrors the pattern from skills.SyncSkillsToMemory.
func SyncGuidanceToMemory(memoryDir string) error {
	guidanceDir := filepath.Join(memoryDir, "guidance")
	if err := os.MkdirAll(guidanceDir, 0755); err != nil {
		return fmt.Errorf("create guidance dir: %w", err)
	}

	written := make(map[string]bool)
	for name, content := range GuidanceDocs {
		filename := name + ".md"
		written[filename] = true
		absPath := filepath.Join(guidanceDir, filename)

		// Skip if content is unchanged (compare SHA-256)
		existing, err := os.ReadFile(absPath)
		if err == nil {
			existingHash := fmt.Sprintf("%x", sha256.Sum256(existing))
			newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			if existingHash == newHash {
				continue
			}
		}

		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write guidance %s: %w", filename, err)
		}
	}

	// Remove orphaned guidance files
	entries, err := os.ReadDir(guidanceDir)
	if err != nil {
		return nil // Directory might not exist yet on first run
	}
	for _, e := range entries {
		if !written[e.Name()] && strings.HasSuffix(e.Name(), ".md") {
			os.Remove(filepath.Join(guidanceDir, e.Name()))
		}
	}

	return nil
}
