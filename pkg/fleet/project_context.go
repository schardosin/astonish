package fleet

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/schardosin/astonish/pkg/safepath"
)

// projectContextTimeout is the maximum time allowed for project context
// generation. OpenCode /init needs to scan the codebase, which can take
// a while for large repositories.
const projectContextTimeout = 15 * time.Minute

// OpenCodeBinaryFinder is a function that locates the OpenCode binary.
// Must be set by the caller (typically daemon/run.go) before fleet sessions
// start. When nil, the opencode_init generator is unavailable.
var OpenCodeBinaryFinder func() (string, error)

// OpenCodeConfigPath is the path to the Astonish-managed opencode.json.
// Set by the daemon after generating the config. When set, all OpenCode
// invocations from the fleet context generator use this config.
var OpenCodeConfigPath string

// OpenCodeExtraEnv holds extra environment variables to set for OpenCode
// invocations (e.g., ASTONISH_OC_API_KEY, AICORE_SERVICE_KEY).
var OpenCodeExtraEnv map[string]string

// OpenCodeModelFlag is the full provider/model string to pass as --model.
var OpenCodeModelFlag string

// GenerateProjectContext dispatches to the configured generator strategy,
// produces or updates the project context file in the workspace, and returns
// its content (capped to the configured max size). Returns empty string on
// any failure (non-fatal: fleet sessions can proceed without project context).
func GenerateProjectContext(ctx context.Context, workspaceDir string, cfg *ProjectContextConfig) string {
	if cfg == nil || cfg.Generator == "" || workspaceDir == "" {
		return ""
	}

	switch cfg.Generator {
	case "opencode_init":
		return generateViaOpenCodeInit(ctx, workspaceDir, cfg)
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
	if err := safepath.ContainedWithin(path, workspaceDir); err != nil {
		slog.Error("project context output file escapes workspace", "component", "fleet-context", "path", path, "error", err)
		return ""
	}
	data, err := os.ReadFile(path)
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

// generateViaOpenCodeInit runs OpenCode with a task that triggers /init to
// analyze the codebase and generate or update the project context file.
func generateViaOpenCodeInit(ctx context.Context, workspaceDir string, cfg *ProjectContextConfig) string {
	if OpenCodeBinaryFinder == nil {
		slog.Warn("opencode binary finder not configured, skipping project context generation", "component", "fleet-context")
		return ""
	}

	binary, err := OpenCodeBinaryFinder()
	if err != nil {
		slog.Warn("opencode binary not found, skipping project context generation", "component", "fleet-context", "error", err)
		return ""
	}

	outputFile := cfg.OutputFile
	if outputFile == "" {
		outputFile = "AGENTS.md"
	}

	task := fmt.Sprintf(
		"Analyze this codebase and generate or update the %s file with project structure, "+
			"build/test/lint commands, code conventions, and key architectural patterns. "+
			"If %s already exists, update it with any new information while preserving "+
			"manually added sections. Keep it concise and focused on what an AI coding "+
			"agent needs to know to work effectively in this codebase.",
		outputFile, outputFile,
	)

	genCtx, cancel := context.WithTimeout(ctx, projectContextTimeout)
	defer cancel()

	// Run OpenCode in JSON format so output is structured, but we only care
	// about whether it succeeds. The actual output is the file on disk.
	cmdArgs := []string{"run", "--format", "json", "--dir", workspaceDir}
	if OpenCodeModelFlag != "" {
		cmdArgs = append(cmdArgs, "--model", OpenCodeModelFlag)
	}
	cmdArgs = append(cmdArgs, task)

	slog.Info("generating project context via opencode /init", "component", "fleet-context", "workspace", workspaceDir, "timeout", projectContextTimeout)
	start := time.Now()

	cmd := exec.CommandContext(genCtx, binary, cmdArgs...)
	cmd.Dir = workspaceDir

	// Build environment with managed config and extra env vars
	env := os.Environ()
	if OpenCodeConfigPath != "" {
		env = setEnv(env, "OPENCODE_CONFIG", OpenCodeConfigPath)
	}
	for k, v := range OpenCodeExtraEnv {
		env = setEnv(env, k, v)
	}
	cmd.Env = env

	// Capture stderr for error reporting
	stderrBuf := &limitedBuffer{max: 4096}
	cmd.Stderr = stderrBuf

	// Drain stdout (NDJSON events) to prevent blocking
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to create stdout pipe", "component", "fleet-context", "error", err)
		return ""
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start opencode", "component", "fleet-context", "error", err)
		return ""
	}

	// Drain stdout in background to prevent the process from blocking
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			// discard output
		}
	}()

	err = cmd.Wait()
	elapsed := time.Since(start)

	if genCtx.Err() == context.DeadlineExceeded {
		slog.Warn("opencode /init timed out", "component", "fleet-context", "elapsed", elapsed.Round(time.Second))
		// Still try to read the file; partial generation may have produced something useful
	} else if err != nil {
		slog.Error("opencode /init failed", "component", "fleet-context", "elapsed", elapsed.Round(time.Second), "error", err, "stderr", stderrBuf.String())
		// Still try to read the file
	} else {
		slog.Info("opencode /init completed", "component", "fleet-context", "elapsed", elapsed.Round(time.Second))
	}

	// Read the generated file
	return LoadProjectContextFile(workspaceDir, cfg)
}

// capProjectContext truncates content to the given max bytes, appending a
// truncation notice if needed.
func capProjectContext(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}
	return content[:maxBytes] + "\n\n[... truncated to fit context budget ...]\n"
}

// limitedBuffer is a bytes buffer that stops accepting writes after reaching max.
type limitedBuffer struct {
	data []byte
	max  int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.max - len(b.data)
	if remaining <= 0 {
		return len(p), nil // discard but report success
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.data)
}

// setEnv appends or replaces an environment variable in a slice.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
