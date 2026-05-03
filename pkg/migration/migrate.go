// Package migration handles one-time data migration from file-based personal
// mode to PostgreSQL-backed platform mode. It reads all data from the local
// filesystem (~/.config/astonish/) and writes it into the appropriate PG
// schemas (team, personal, org).
package migration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// Category identifies a data category for migration.
type Category string

const (
	CatCredentials Category = "credentials"
	CatSessions    Category = "sessions"
	CatApps        Category = "apps"
	CatFlows       Category = "flows"
	CatScheduler   Category = "scheduler"
	CatFleets      Category = "fleets"
	CatSkills      Category = "skills"
	CatMemory      Category = "memory"
)

// AllCategories defines the migration order: credentials first (may be needed
// by other stores), memory last (slowest due to re-embedding).
var AllCategories = []Category{
	CatCredentials,
	CatSessions,
	CatApps,
	CatFlows,
	CatScheduler,
	CatFleets,
	CatSkills,
	CatMemory,
}

// Progress represents the progress of migrating a single category.
type Progress struct {
	Category Category `json:"category"`
	Current  int      `json:"current"`
	Total    int      `json:"total"`
	Status   string   `json:"status"` // "pending", "counting", "migrating", "done", "error", "skipped"
	Error    string   `json:"error,omitempty"`
}

// Summary holds the final migration results.
type Summary struct {
	Success    bool            `json:"success"`
	Categories map[Category]int `json:"categories"` // category -> count migrated
	Duration   time.Duration   `json:"duration"`
	Errors     []string        `json:"errors,omitempty"`
}

// ProgressFunc is called whenever migration progress updates.
type ProgressFunc func(Progress)

// Migrator orchestrates the file→database migration.
type Migrator struct {
	// Source paths (file-based personal mode)
	configDir string

	// Target (PG platform mode)
	pgStore  *pgstore.PGStore
	orgSlug  string
	teamSlug string
	userID   string

	// App config for memory embedding settings
	appCfg *config.AppConfig

	// Progress callback
	onProgress ProgressFunc

	mu     sync.Mutex
	status map[Category]*Progress
}

// Config holds the parameters for creating a Migrator.
type Config struct {
	ConfigDir string // e.g. ~/.config/astonish
	PGStore   *pgstore.PGStore
	OrgSlug   string
	TeamSlug  string
	UserID    string
	AppCfg    *config.AppConfig
}

// New creates a new Migrator with the given configuration.
func New(cfg Config) *Migrator {
	status := make(map[Category]*Progress, len(AllCategories))
	for _, cat := range AllCategories {
		status[cat] = &Progress{Category: cat, Status: "pending"}
	}
	return &Migrator{
		configDir: cfg.ConfigDir,
		pgStore:   cfg.PGStore,
		orgSlug:   cfg.OrgSlug,
		teamSlug:  cfg.TeamSlug,
		userID:    cfg.UserID,
		appCfg:    cfg.AppCfg,
		status:    status,
	}
}

// SetProgressFunc sets the progress callback.
func (m *Migrator) SetProgressFunc(fn ProgressFunc) {
	m.onProgress = fn
}

// Run executes the full migration in order.
func (m *Migrator) Run(ctx context.Context) (*Summary, error) {
	start := time.Now()
	summary := &Summary{
		Success:    true,
		Categories: make(map[Category]int),
	}

	// Get the org data store for the target org
	orgDS, err := m.pgStore.ForOrg(m.orgSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to get org data store for %q: %w", m.orgSlug, err)
	}
	defer orgDS.Close()

	teamDS := orgDS.ForTeam(m.teamSlug)

	for _, cat := range AllCategories {
		if ctx.Err() != nil {
			summary.Success = false
			summary.Errors = append(summary.Errors, "migration cancelled")
			break
		}

		count, err := m.migrateCategory(ctx, cat, teamDS, orgDS)
		summary.Categories[cat] = count
		if err != nil {
			summary.Success = false
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", cat, err))
			slog.Error("migration category failed", "category", cat, "error", err)
			// Continue with other categories — don't stop on individual failures
		}
	}

	summary.Duration = time.Since(start)

	// Mark migration as complete by renaming data dirs
	if summary.Success {
		m.markComplete()
	}

	return summary, nil
}

// migrateCategory runs migration for a single category.
func (m *Migrator) migrateCategory(ctx context.Context, cat Category, teamDS store.TeamDataStore, orgDS store.OrgDataStore) (int, error) {
	switch cat {
	case CatCredentials:
		return m.migrateCredentials(ctx, teamDS)
	case CatSessions:
		return m.migrateSessions(ctx, teamDS)
	case CatApps:
		return m.migrateApps(ctx, teamDS)
	case CatFlows:
		return m.migrateFlows(ctx, teamDS)
	case CatScheduler:
		return m.migrateScheduler(ctx, teamDS)
	case CatFleets:
		return m.migrateFleets(ctx, teamDS)
	case CatSkills:
		return m.migrateSkills(ctx, orgDS)
	case CatMemory:
		return m.migrateMemory(ctx, teamDS)
	default:
		return 0, fmt.Errorf("unknown category: %s", cat)
	}
}

// emitProgress updates the status and notifies the listener.
func (m *Migrator) emitProgress(cat Category, current, total int, status string, errMsg string) {
	m.mu.Lock()
	p := m.status[cat]
	p.Current = current
	p.Total = total
	p.Status = status
	p.Error = errMsg
	m.mu.Unlock()

	if m.onProgress != nil {
		m.onProgress(Progress{
			Category: cat,
			Current:  current,
			Total:    total,
			Status:   status,
			Error:    errMsg,
		})
	}
}

// GetStatus returns the current status of all categories.
func (m *Migrator) GetStatus() []Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]Progress, 0, len(AllCategories))
	for _, cat := range AllCategories {
		result = append(result, *m.status[cat])
	}
	return result
}

// markComplete renames migrated data directories so the migration check
// doesn't trigger again on next startup.
func (m *Migrator) markComplete() {
	timestamp := time.Now().Format("20060102-150405")
	suffix := ".pre-migration-" + timestamp

	dirsToRename := []string{
		"sessions",
		"credentials.enc",
		"apps",
		"store.json", // flow taps
		"scheduler",
		"fleets",
		"fleet_plans",
	}

	for _, name := range dirsToRename {
		src := filepath.Join(m.configDir, name)
		if _, err := os.Stat(src); err == nil {
			dst := src + suffix
			if err := os.Rename(src, dst); err != nil {
				slog.Warn("failed to rename migrated data", "src", src, "dst", dst, "error", err)
			}
		}
	}

	// Write a marker file
	marker := filepath.Join(m.configDir, ".migration-complete")
	_ = os.WriteFile(marker, []byte(fmt.Sprintf("migrated at %s\n", timestamp)), 0644)
}

// HasFileData checks if file-based data exists that could be migrated.
func HasFileData(configDir string) bool {
	checks := []string{
		filepath.Join(configDir, "sessions", "index.json"),
		filepath.Join(configDir, "credentials.enc"),
		filepath.Join(configDir, "apps"),
		filepath.Join(configDir, "memory", "MEMORY.md"),
	}
	for _, path := range checks {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// IsMigrationComplete checks if a migration has already been performed.
func IsMigrationComplete(configDir string) bool {
	marker := filepath.Join(configDir, ".migration-complete")
	_, err := os.Stat(marker)
	return err == nil
}
