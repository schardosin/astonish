package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"gopkg.in/yaml.v3"
)

// FlowRegistry indexes saved flows for lookup by natural language description.
// The registry is stored as a JSON file on disk and loaded into memory.
type FlowRegistry struct {
	path    string
	entries []FlowRegistryEntry
	mu      sync.RWMutex
}

// FlowRegistryEntry describes a single saved flow.
type FlowRegistryEntry struct {
	FlowFile    string     `json:"flowFile"`
	Description string     `json:"description"`
	Type        string     `json:"type,omitempty"` // "drill", "drill_suite", or empty for regular flows
	Tags        []string   `json:"tags"`
	CreatedAt   time.Time  `json:"createdAt"`
	UsedCount   int        `json:"usedCount"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// registryData is the on-disk format.
type registryData struct {
	Version int                 `json:"version"`
	Entries []FlowRegistryEntry `json:"entries"`
}

// NewFlowRegistry loads or creates a flow registry at the given path.
func NewFlowRegistry(path string) (*FlowRegistry, error) {
	r := &FlowRegistry{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No registry yet -- that's fine
			return r, nil
		}
		return nil, fmt.Errorf("failed to read flow registry: %w", err)
	}

	var rd registryData
	if err := json.Unmarshal(data, &rd); err != nil {
		return nil, fmt.Errorf("failed to parse flow registry: %w", err)
	}
	r.entries = rd.Entries
	return r, nil
}

// DefaultRegistryPath returns the default path for the flow registry file.
func DefaultRegistryPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "flow_registry.json"), nil
}

// Entries returns a copy of all registry entries.
func (r *FlowRegistry) Entries() []FlowRegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]FlowRegistryEntry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// Register adds a new flow to the registry and persists to disk.
func (r *FlowRegistry) Register(entry FlowRegistryEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return r.save()
}

// Remove deletes an entry by flow file name.
func (r *FlowRegistry) Remove(flowFile string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.FlowFile == flowFile {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			return r.save()
		}
	}
	return fmt.Errorf("flow not found in registry: %s", flowFile)
}

// HasFlow checks whether a flow file is already registered.
func (r *FlowRegistry) HasFlow(flowFile string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.FlowFile == flowFile {
			return true
		}
	}
	return false
}

// SyncFromDirectory scans a directory for .yaml flow files and registers any
// that aren't already in the registry. Also prunes entries whose YAML files
// no longer exist on disk. Returns the number of newly registered flows.
func (r *FlowRegistry) SyncFromDirectory(flowsDir string) (int, error) {
	dirEntries, err := os.ReadDir(flowsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist — prune all entries since no YAML files can exist
			r.mu.Lock()
			if len(r.entries) > 0 {
				r.entries = nil
				if err := r.save(); err != nil {
					slog.Warn("failed to save flow registry", "error", err)
				}
			}
			r.mu.Unlock()
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read flows directory: %w", err)
	}

	// Build set of existing YAML files on disk
	onDisk := make(map[string]bool)
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		onDisk[de.Name()] = true
	}

	// Prune entries whose YAML no longer exists, and backfill Type for
	// entries that were created before the Type field was added.
	r.mu.Lock()
	pruned := false
	backfilled := false
	var kept []FlowRegistryEntry
	for _, e := range r.entries {
		if onDisk[e.FlowFile] {
			// Backfill Type if missing (pre-existing registry entries)
			if e.Type == "" {
				parsed := parseFlowYAMLForRegistry(filepath.Join(flowsDir, e.FlowFile), e.FlowFile)
				if parsed.Type != "" {
					e.Type = parsed.Type
					backfilled = true
				}
			}
			kept = append(kept, e)
		} else {
			pruned = true
		}
	}
	if pruned || backfilled {
		r.entries = kept
		if err := r.save(); err != nil {
			slog.Warn("failed to save flow registry", "error", err)
		}
	}
	r.mu.Unlock()

	// Register new YAML files not yet in the registry
	added := 0
	for filename := range onDisk {
		if r.HasFlow(filename) {
			continue
		}

		entry := parseFlowYAMLForRegistry(filepath.Join(flowsDir, filename), filename)
		if regErr := r.Register(entry); regErr != nil {
			continue
		}
		added++
	}

	return added, nil
}

// parseFlowYAMLForRegistry reads a flow YAML file and extracts metadata
// for a registry entry. Falls back to filename-based defaults on parse errors.
func parseFlowYAMLForRegistry(path, filename string) FlowRegistryEntry {
	entry := FlowRegistryEntry{
		FlowFile:  filename,
		CreatedAt: time.Now(),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		entry.Description = strings.TrimSuffix(filename, ".yaml")
		return entry
	}

	var agentCfg config.AgentConfig
	if err := yaml.Unmarshal(data, &agentCfg); err != nil {
		entry.Description = strings.TrimSuffix(filename, ".yaml")
		return entry
	}

	entry.Type = agentCfg.Type

	if agentCfg.Description != "" {
		entry.Description = agentCfg.Description
	} else {
		entry.Description = strings.TrimSuffix(filename, ".yaml")
	}

	// Extract tool names as tags (gives some signal for flow matching)
	toolSet := make(map[string]bool)
	for _, node := range agentCfg.Nodes {
		for _, t := range node.ToolsSelection {
			toolSet[t] = true
		}
	}
	for t := range toolSet {
		entry.Tags = append(entry.Tags, t)
	}

	return entry
}

// save writes the registry to disk atomically.
func (r *FlowRegistry) save() error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	rd := registryData{
		Version: 1,
		Entries: r.entries,
	}

	data, err := json.MarshalIndent(rd, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Atomic write: temp file + rename
	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry temp file: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename registry file: %w", err)
	}

	return nil
}
