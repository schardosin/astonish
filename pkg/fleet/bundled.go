package fleet

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed bundled/*.yaml
var bundledFleets embed.FS

// Cached bundled configs (parsed once, reused forever — they're immutable).
var (
	bundledConfigsOnce sync.Once
	bundledConfigs     map[string]*FleetConfig
	bundledConfigsErr  error
)

// LoadBundledConfigs returns all bundled fleet configs parsed from the
// embedded YAML files. The result is cached after the first call.
// Keys are derived from filenames (e.g., "software-dev" from "software-dev.yaml").
func LoadBundledConfigs() (map[string]*FleetConfig, error) {
	bundledConfigsOnce.Do(func() {
		entries, err := fs.ReadDir(bundledFleets, "bundled")
		if err != nil {
			bundledConfigsErr = fmt.Errorf("reading bundled fleets: %w", err)
			return
		}

		configs := make(map[string]*FleetConfig)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}

			data, err := fs.ReadFile(bundledFleets, filepath.Join("bundled", name))
			if err != nil {
				bundledConfigsErr = fmt.Errorf("reading bundled fleet %s: %w", name, err)
				return
			}

			var cfg FleetConfig
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				bundledConfigsErr = fmt.Errorf("parsing bundled fleet %s: %w", name, err)
				return
			}

			key := strings.TrimSuffix(name, filepath.Ext(name))
			configs[key] = &cfg
		}

		bundledConfigs = configs
	})

	return bundledConfigs, bundledConfigsErr
}

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
