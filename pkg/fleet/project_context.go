package fleet

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// GenerateProjectContext dispatches to the configured generator strategy,
// produces or updates the project context file in the workspace, and returns
// its content (capped to the configured max size). Returns empty string on
// any failure (non-fatal: fleet sessions can proceed without project context).
func GenerateProjectContext(ctx context.Context, workspaceDir string, cfg *ProjectContextConfig) string {
	if cfg == nil || cfg.Generator == "" || workspaceDir == "" {
		return ""
	}

	switch cfg.Generator {
	case "load_file":
		return LoadProjectContextFile(workspaceDir, cfg)
	default:
		slog.Warn("unknown project context generator", "component", "fleet-context", "generator", cfg.Generator)
		return ""
	}
}

// LoadProjectContextFile reads an existing project context file from the
// workspace without generating or updating it. Used for session recovery
// (the file should already exist from the original session) and for the
// "load_file" generator strategy.
func LoadProjectContextFile(workspaceDir string, cfg *ProjectContextConfig) string {
	if cfg == nil || cfg.OutputFile == "" || workspaceDir == "" {
		return ""
	}

	path := filepath.Join(workspaceDir, cfg.OutputFile)
	absWorkspace, err1 := filepath.Abs(workspaceDir)
	absPath, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil || !strings.HasPrefix(absPath, absWorkspace+string(filepath.Separator)) {
		slog.Error("project context output file escapes workspace", "component", "fleet-context", "path", path)
		return ""
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("failed to read project context file", "component", "fleet-context", "path", path, "error", err)
		}
		return ""
	}

	content := string(data)
	if content == "" {
		return ""
	}

	return capProjectContext(content, cfg.GetMaxSizeBytes())
}

// capProjectContext truncates content to the given max bytes, appending a
// truncation notice if needed.
func capProjectContext(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}
	return content[:maxBytes] + "\n\n[... truncated to fit context budget ...]\n"
}
