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

// ImportFlowsResult holds the result of a flow import operation.
type ImportFlowsResult struct {
	Total    int `json:"total"`    // Total YAML files found
	Imported int `json:"imported"` // Successfully imported/updated
}

// ImportFlows scans the personal-mode config directories for flow YAML files
// and imports (upserts) them into the given team's PostgreSQL flow store.
// This is idempotent — existing flows are updated, new ones are inserted.
//
// configDir is the personal-mode config directory (e.g. ~/.config/astonish).
// teamDS is the target team's data store.
// progressFn is an optional callback for progress updates (may be nil).
func ImportFlows(ctx context.Context, configDir string, teamDS store.TeamDataStore, progressFn func(imported, total int)) (*ImportFlowsResult, error) {
	// Scan all directories where flows/drills may live
	storeDir := filepath.Join(configDir, "store")
	walkPaths := []string{
		storeDir,
		filepath.Join(configDir, "flows"),
		filepath.Join(configDir, "agents"),
	}

	var flowFiles []string
	for _, base := range walkPaths {
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && (strings.HasSuffix(info.Name(), ".yaml") || strings.HasSuffix(info.Name(), ".yml")) {
				// Skip manifest.yaml files (they're tap metadata, not flows)
				if info.Name() != "manifest.yaml" {
					flowFiles = append(flowFiles, path)
				}
			}
			return nil
		})
	}

	total := len(flowFiles)
	if total == 0 {
		return &ImportFlowsResult{Total: 0, Imported: 0}, nil
	}

	// Get the pgFlowStore to use its SaveFlowDefinition method
	flowStore := teamDS.Flows()
	pgFS, hasSave := flowStore.(interface {
		SaveFlowDefinition(ctx context.Context, name string, definition any, yamlContent string) error
	})
	if !hasSave {
		return nil, fmt.Errorf("flow store does not support SaveFlowDefinition; expected pgstore implementation")
	}

	count := 0
	for _, path := range flowFiles {
		if ctx.Err() != nil {
			return &ImportFlowsResult{Total: total, Imported: count}, ctx.Err()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Parse YAML to get the flow definition
		var flowDef map[string]interface{}
		if err := yaml.Unmarshal(data, &flowDef); err != nil {
			continue
		}

		// Derive flow name from filename
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

		// Upsert into PG (stores type column from YAML + full YAML content)
		if err := pgFS.SaveFlowDefinition(ctx, name, flowDef, string(data)); err != nil {
			return &ImportFlowsResult{Total: total, Imported: count}, fmt.Errorf("failed to save flow %q: %w", name, err)
		}

		count++
		if progressFn != nil {
			progressFn(count, total)
		}
	}

	return &ImportFlowsResult{Total: total, Imported: count}, nil
}

// migrateFlows is the migration-time wrapper around ImportFlows.
// It emits progress events via the Migrator's progress system.
func (m *Migrator) migrateFlows(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	m.emitProgress(CatFlows, 0, 0, "counting", "")

	progressFn := func(imported, total int) {
		m.emitProgress(CatFlows, imported, total, "migrating", "")
	}

	result, err := ImportFlows(ctx, m.configDir, teamDS, progressFn)
	if err != nil {
		if result != nil {
			m.emitProgress(CatFlows, result.Imported, result.Total, "error", err.Error())
		}
		return 0, err
	}

	if result.Total == 0 {
		m.emitProgress(CatFlows, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatFlows, result.Imported, result.Total, "done", "")
	return result.Imported, nil
}
