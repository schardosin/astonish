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

func (m *Migrator) migrateFleets(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	fleetsDir := filepath.Join(m.configDir, "fleets")
	plansDir := filepath.Join(m.configDir, "fleet_plans")

	m.emitProgress(CatFleets, 0, 0, "counting", "")

	// Collect fleet template and plan YAML files
	type fleetFile struct {
		path  string
		key   string
		isPlan bool
	}
	var files []fleetFile

	// Fleet templates
	if entries, err := os.ReadDir(fleetsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
				key := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
				files = append(files, fleetFile{
					path:   filepath.Join(fleetsDir, e.Name()),
					key:    key,
					isPlan: false,
				})
			}
		}
	}

	// Fleet plans
	if entries, err := os.ReadDir(plansDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
				key := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
				files = append(files, fleetFile{
					path:   filepath.Join(plansDir, e.Name()),
					key:    key,
					isPlan: true,
				})
			}
		}
	}

	total := len(files)
	if total == 0 {
		m.emitProgress(CatFleets, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatFleets, 0, total, "migrating", "")

	templateStore := teamDS.FleetTemplates()
	planStore := teamDS.FleetPlans()
	count := 0

	for _, ff := range files {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		data, err := os.ReadFile(ff.path)
		if err != nil {
			continue
		}

		// Parse YAML to generic map (the PG store saves as JSONB)
		var def map[string]interface{}
		if err := yaml.Unmarshal(data, &def); err != nil {
			continue
		}

		if ff.isPlan {
			if err := planStore.Save(def); err != nil {
				return count, fmt.Errorf("failed to save fleet plan %q: %w", ff.key, err)
			}
		} else {
			if err := templateStore.Save(ff.key, def); err != nil {
				return count, fmt.Errorf("failed to save fleet template %q: %w", ff.key, err)
			}
		}

		count++
		m.emitProgress(CatFleets, count, total, "migrating", "")
	}

	m.emitProgress(CatFleets, count, total, "done", "")
	return count, nil
}
