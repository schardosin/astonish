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

// IsBundledKey reports whether key is an Astonish-shipped (embedded) fleet template.
// Bundled templates are immutable: they always win over any same-key DB row.
func IsBundledKey(key string) bool {
	bundled, err := LoadBundledConfigs()
	if err != nil || bundled == nil {
		return false
	}
	_, ok := bundled[key]
	return ok
}

// BundledKeys returns the set of embedded fleet template keys.
func BundledKeys() map[string]struct{} {
	bundled, err := LoadBundledConfigs()
	if err != nil || bundled == nil {
		return map[string]struct{}{}
	}
	keys := make(map[string]struct{}, len(bundled))
	for k := range bundled {
		keys[k] = struct{}{}
	}
	return keys
}

// EnsureBundled copies bundled fleet YAML files to the target directory.
//
// Legacy: used only by personal-mode / file-based fleet bootstrapping.
// Platform mode (Postgres or SQLite team DB) serves bundled templates from the
// embedded FS and must never overwrite user data via this helper.
// Bundled files are always overwritten when this is called.
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
