package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// IncrementUsage updates usage stats for a flow.
func (r *FlowRegistry) IncrementUsage(flowFile string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for i := range r.entries {
		if r.entries[i].FlowFile == flowFile {
			r.entries[i].UsedCount++
			r.entries[i].LastUsedAt = &now
			break
		}
	}
	_ = r.save() // best-effort persist
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

// BuildMatchPrompt returns a prompt that lists all registry entries for LLM matching.
// Returns empty string if the registry is empty.
func (r *FlowRegistry) BuildMatchPrompt(userRequest string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.entries) == 0 {
		return ""
	}

	prompt := "# Saved Flows\n\n"
	prompt += "Below is a list of saved reusable flows. If the user's request matches one of these flows, "
	prompt += "reply with ONLY the exact flow filename. If none match, reply with ONLY the word NONE.\n\n"

	for _, e := range r.entries {
		prompt += fmt.Sprintf("- **%s**: %s", e.FlowFile, e.Description)
		if len(e.Tags) > 0 {
			prompt += fmt.Sprintf(" (tags: %s)", joinTags(e.Tags))
		}
		prompt += "\n"
	}

	prompt += fmt.Sprintf("\n# User Request\n\n%s\n", userRequest)
	prompt += "\n# Your Response\n\nReply with the exact flow filename or NONE.\n"

	return prompt
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

func joinTags(tags []string) string {
	result := ""
	for i, t := range tags {
		if i > 0 {
			result += ", "
		}
		result += t
	}
	return result
}
