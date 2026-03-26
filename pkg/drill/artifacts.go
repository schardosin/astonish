package drill

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ArtifactManager handles saving proof artifacts (logs, screenshots) for test runs.
type ArtifactManager struct {
	baseDir string // Base artifact directory for this suite run
}

// NewArtifactManager creates an artifact manager with a timestamped subdirectory.
func NewArtifactManager(baseDir, suiteName string) (*ArtifactManager, error) {
	ts := time.Now().Format("2006-01-02T15-04-05")
	dir := filepath.Join(baseDir, fmt.Sprintf("%s_%s", suiteName, ts))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	return &ArtifactManager{baseDir: dir}, nil
}

// Dir returns the artifact directory path.
func (am *ArtifactManager) Dir() string {
	return am.baseDir
}

// SaveLog writes text content (typically PTY output) as a log file.
func (am *ArtifactManager) SaveLog(stepName, content string) (string, error) {
	filename := stepName + "_output.log"
	path := filepath.Join(am.baseDir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write log artifact: %w", err)
	}
	return path, nil
}

// SaveScreenshot decodes base64 image data and writes it as a file.
func (am *ArtifactManager) SaveScreenshot(stepName, base64Data, format string) (string, error) {
	if format == "" {
		format = "png"
	}
	filename := fmt.Sprintf("%s_post.%s", stepName, format)
	path := filepath.Join(am.baseDir, filename)

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("decode screenshot: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot artifact: %w", err)
	}
	return path, nil
}

// SaveSetupLog writes the combined setup command output.
func (am *ArtifactManager) SaveSetupLog(content string) (string, error) {
	path := filepath.Join(am.baseDir, "setup_output.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write setup log: %w", err)
	}
	return path, nil
}
