package fleet

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed bundled/*.yaml
var bundledFleets embed.FS

// EnsureBundled copies bundled fleet YAML files to the target directory.
// Bundled files are always overwritten to ensure users get the latest
// defaults. Users who want custom fleet configs should create files with
// different names (e.g., my-software-dev.yaml).
// Returns the number of fleets written.
func EnsureBundled(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("creating fleets directory: %w", err)
	}

	entries, err := fs.ReadDir(bundledFleets, "bundled")
	if err != nil {
		return 0, fmt.Errorf("reading bundled fleets: %w", err)
	}

	written := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		dest := filepath.Join(dir, entry.Name())

		data, err := fs.ReadFile(bundledFleets, filepath.Join("bundled", entry.Name()))
		if err != nil {
			return written, fmt.Errorf("reading bundled fleet %s: %w", entry.Name(), err)
		}

		if err := os.WriteFile(dest, data, 0644); err != nil {
			return written, fmt.Errorf("writing fleet %s: %w", dest, err)
		}
		written++
	}

	return written, nil
}
