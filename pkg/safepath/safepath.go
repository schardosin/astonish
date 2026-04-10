package safepath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateName checks that a user-supplied name is safe to use as a single
// path component (e.g. agent name, flow name, plan key, session ID).
// It rejects empty names, names containing path separators, traversal
// sequences, and null bytes.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("name must not contain null bytes")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name must not be a relative path component")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name must not contain path traversal sequences")
	}
	return nil
}

// ContainedWithin verifies that the resolved path is contained within baseDir.
// Both paths are cleaned before comparison. Returns an error if the path
// escapes the base directory.
func ContainedWithin(path, baseDir string) error {
	cleanPath := filepath.Clean(path)
	cleanBase := filepath.Clean(baseDir) + string(filepath.Separator)

	if cleanPath == filepath.Clean(baseDir) {
		return nil
	}
	if !strings.HasPrefix(cleanPath, cleanBase) {
		return fmt.Errorf("path %q is not contained within %q", path, baseDir)
	}
	return nil
}
