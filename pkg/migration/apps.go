package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

func (m *Migrator) migrateApps(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	appsDir := filepath.Join(m.configDir, "apps")

	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		m.emitProgress(CatApps, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatApps, 0, 0, "counting", "")

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		m.emitProgress(CatApps, 0, 0, "error", "cannot read apps directory")
		return 0, fmt.Errorf("cannot read apps directory: %w", err)
	}

	// Filter for YAML files
	var yamlFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			yamlFiles = append(yamlFiles, e)
		}
	}

	total := len(yamlFiles)
	if total == 0 {
		m.emitProgress(CatApps, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatApps, 0, total, "migrating", "")

	appStore := teamDS.Apps()
	count := 0

	for _, entry := range yamlFiles {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		data, err := os.ReadFile(filepath.Join(appsDir, entry.Name()))
		if err != nil {
			continue
		}

		// Parse the YAML into a generic map so we can pass it as-is to Save()
		var app map[string]interface{}
		if err := yaml.Unmarshal(data, &app); err != nil {
			continue
		}

		// The app store Save() expects the app as-is (it marshals to JSON internally)
		if _, err := appStore.Save(app); err != nil {
			return count, fmt.Errorf("failed to save app %q: %w", entry.Name(), err)
		}

		count++
		m.emitProgress(CatApps, count, total, "migrating", "")
	}

	m.emitProgress(CatApps, count, total, "done", "")
	return count, nil
}
