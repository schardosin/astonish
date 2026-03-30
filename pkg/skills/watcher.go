package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherConfig holds the configuration for the skills directory watcher.
type WatcherConfig struct {
	UserDir      string
	ExtraDirs    []string
	WorkspaceDir string
	Allowlist    []string
	MemoryDir    string
	DebounceMs   int
	DebugMode    bool
}

// WatchSkillDirs watches skill source directories for changes and re-syncs
// eligible skills to the memory directory. The memory indexer's own file watcher
// then picks up the changes in memory/skills/ and reindexes them.
//
// This handles all skill installation methods: CLI install, manual file copy,
// editing existing skills, etc.
func WatchSkillDirs(ctx context.Context, cfg WatcherConfig) error {
	debounceMs := cfg.DebounceMs
	if debounceMs <= 0 {
		debounceMs = 2000
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create skills watcher: %w", err)
	}
	defer watcher.Close()

	// Collect all skill source directories to watch
	dirs := collectSkillDirs(cfg)
	if len(dirs) == 0 {
		slog.Debug("no skill directories to watch", "component", "skills-watcher")
		// Block until cancelled — nothing to watch
		<-ctx.Done()
		return nil
	}

	// Watch parent directories too, so we detect if a skills dir itself is
	// deleted and recreated (e.g. rm -rf ~/.config/astonish/skills && mkdir ...).
	// Without this, deleting the watched dir removes the inotify watch and
	// recreating it goes unnoticed.
	parentDirs := make(map[string]bool)
	for _, dir := range dirs {
		parent := filepath.Dir(dir)
		if parent != "" && parent != dir && !parentDirs[parent] {
			parentDirs[parent] = true
			_ = watcher.Add(parent)
		}
	}

	// Track which directories are our skill roots so we can re-watch on recreate
	skillRoots := make(map[string]bool, len(dirs))

	// Add watches for each directory and its immediate subdirectories
	for _, dir := range dirs {
		skillRoots[dir] = true
		watchDirRecursive(watcher, dir, cfg.DebugMode)
	}

	if cfg.DebugMode {
		slog.Debug("watching skill directories", "component", "skills-watcher", "count", len(dirs))
	}

	// Debounce: wait for changes to settle before re-syncing
	var mu sync.Mutex
	var timer *time.Timer
	dirty := false

	doSync := func() {
		mu.Lock()
		if !dirty {
			mu.Unlock()
			return
		}
		dirty = false
		mu.Unlock()

		if cfg.DebugMode {
			slog.Debug("skill change detected, re-syncing to memory", "component", "skills-watcher")
		}

		allSkills, err := LoadSkills(cfg.UserDir, cfg.ExtraDirs, cfg.WorkspaceDir, cfg.Allowlist)
		if err != nil {
			if cfg.DebugMode {
				slog.Debug("error loading skills", "component", "skills-watcher", "error", err)
			}
			return
		}

		if err := SyncSkillsToMemory(allSkills, cfg.MemoryDir); err != nil {
			if cfg.DebugMode {
				slog.Debug("error syncing skills to memory", "component", "skills-watcher", "error", err)
			}
		} else if cfg.DebugMode {
			eligible := FilterEligible(allSkills)
			slog.Debug("synced skills to memory", "component", "skills-watcher", "eligible", len(eligible))
		}
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Watch for new subdirectories (new skill folders or recreated skill roots)
			if event.Has(fsnotify.Create) {
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() {
					// If a skill root was recreated, re-watch it and its children
					if skillRoots[event.Name] {
						watchDirRecursive(watcher, event.Name, cfg.DebugMode)
						slog.Debug("re-watching recreated skill root", "component", "skills-watcher", "dir", event.Name)
					} else {
						_ = watcher.Add(event.Name)
						slog.Debug("watching new directory", "component", "skills-watcher", "dir", event.Name)
					}
					// New directory likely already contains files (e.g. skills install
					// creates dir + writes SKILL.md atomically before the watch is set up).
					// Mark dirty so we re-sync.
					mu.Lock()
					dirty = true
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, doSync)
					mu.Unlock()
					continue
				}
			}

			// Directory removal (skill folder deleted)
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// If a watched directory was removed, trigger re-sync.
				// We can't stat the path (it's gone), so check if it doesn't
				// look like a file with an extension.
				baseName := filepath.Base(event.Name)
				if !strings.Contains(baseName, ".") {
					mu.Lock()
					dirty = true
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, doSync)
					mu.Unlock()
					continue
				}
			}

			// Only trigger re-sync for .md or _meta.json file changes
			baseName := filepath.Base(event.Name)
			if !strings.HasSuffix(strings.ToLower(baseName), ".md") {
				if baseName != "_meta.json" {
					continue
				}
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				mu.Lock()
				dirty = true
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, doSync)
				mu.Unlock()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Debug("skills watcher error", "component", "skills-watcher", "error", err)
		}
	}
}

// collectSkillDirs returns skill source directories that exist on disk.
func collectSkillDirs(cfg WatcherConfig) []string {
	var dirs []string
	seen := make(map[string]bool)

	add := func(dir string) {
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		// Create the directory if it doesn't exist, so we can watch it
		if err := os.MkdirAll(abs, 0755); err != nil {
			return
		}
		seen[abs] = true
		dirs = append(dirs, abs)
	}

	add(cfg.UserDir)
	for _, d := range cfg.ExtraDirs {
		add(d)
	}

	return dirs
}

// watchDirRecursive adds a watch on the directory and its immediate subdirectories.
func watchDirRecursive(watcher *fsnotify.Watcher, dir string, debugMode bool) {
	if err := watcher.Add(dir); err != nil {
		slog.Debug("cannot watch directory", "component", "skills-watcher", "dir", dir, "error", err)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			subDir := filepath.Join(dir, e.Name())
			if err := watcher.Add(subDir); err != nil {
				slog.Debug("cannot watch subdirectory", "component", "skills-watcher", "dir", subDir, "error", err)
			}
		}
	}
}
