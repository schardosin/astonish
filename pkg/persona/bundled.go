package persona

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed bundled/*.yaml
var bundledPersonas embed.FS

// EnsureBundled copies bundled persona YAML files to the target directory.
// Bundled files are always overwritten to ensure users get the latest
// defaults. Users who want custom personas should create files with
// different names.
// Returns the number of personas written.
func EnsureBundled(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("creating personas directory: %w", err)
	}

	entries, err := fs.ReadDir(bundledPersonas, "bundled")
	if err != nil {
		return 0, fmt.Errorf("reading bundled personas: %w", err)
	}

	written := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		dest := filepath.Join(dir, entry.Name())

		data, err := fs.ReadFile(bundledPersonas, filepath.Join("bundled", entry.Name()))
		if err != nil {
			return written, fmt.Errorf("reading bundled persona %s: %w", entry.Name(), err)
		}

		if err := os.WriteFile(dest, data, 0644); err != nil {
			return written, fmt.Errorf("writing persona %s: %w", dest, err)
		}
		written++
	}

	return written, nil
}
