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

func (m *Migrator) migrateFlows(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	storeDir := filepath.Join(m.configDir, "store")
	storeJSON := filepath.Join(m.configDir, "store.json")

	// Check if flow store exists
	if _, err := os.Stat(storeJSON); os.IsNotExist(err) {
		if _, err := os.Stat(storeDir); os.IsNotExist(err) {
			m.emitProgress(CatFlows, 0, 0, "skipped", "")
			return 0, nil
		}
	}

	m.emitProgress(CatFlows, 0, 0, "counting", "")

	// Find all installed flow YAML files
	var flowFiles []string
	walkPaths := []string{storeDir, filepath.Join(m.configDir, "flows")}

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
		m.emitProgress(CatFlows, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatFlows, 0, total, "migrating", "")

	// Get the pgFlowStore to use its SaveFlow method
	flowStore := teamDS.Flows()
	pgFS, hasSave := flowStore.(interface {
		SaveFlow(ctx context.Context, name string, definition any) error
	})
	if !hasSave {
		// If the flow store doesn't support SaveFlow, we need to use the pgstore directly
		// This happens when the team data store wraps a non-PG flow store
		m.emitProgress(CatFlows, 0, total, "error", "flow store does not support SaveFlow")
		return 0, fmt.Errorf("flow store does not support SaveFlow; expected pgstore implementation")
	}
	count := 0

	for _, path := range flowFiles {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Parse YAML to get the flow name and definition
		var flowDef map[string]interface{}
		if err := yaml.Unmarshal(data, &flowDef); err != nil {
			continue
		}

		// Derive flow name from filename
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

		// Store the full YAML content as the definition
		if err := pgFS.SaveFlow(ctx, name, flowDef); err != nil {
			return count, fmt.Errorf("failed to save flow %q: %w", name, err)
		}

		count++
		m.emitProgress(CatFlows, count, total, "migrating", "")
	}

	m.emitProgress(CatFlows, count, total, "done", "")
	return count, nil
}
