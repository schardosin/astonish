package fleet

import (
	"fmt"
	"sync"
)

// FleetSummary is a lightweight view of a fleet for listing.
type FleetSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
}

// Registry manages a collection of fleet definitions loaded from disk.
type Registry struct {
	dir    string
	fleets map[string]*FleetConfig
	mu     sync.RWMutex
}

// NewRegistry creates a Registry by loading all fleet definitions from dir.
// If dir does not exist, an empty registry is returned (not an error).
func NewRegistry(dir string) (*Registry, error) {
	r := &Registry{
		dir:    dir,
		fleets: make(map[string]*FleetConfig),
	}

	if err := r.Reload(); err != nil {
		return nil, err
	}

	return r, nil
}

// Reload re-reads all fleet files from the directory.
func (r *Registry) Reload() error {
	fleets, err := LoadFleets(r.dir)
	if err != nil {
		return fmt.Errorf("loading fleets from %s: %w", r.dir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if fleets == nil {
		r.fleets = make(map[string]*FleetConfig)
	} else {
		r.fleets = fleets
	}

	return nil
}

// GetFleet returns a fleet by its key (filename stem).
func (r *Registry) GetFleet(key string) (*FleetConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.fleets[key]
	return f, ok
}

// ListFleets returns summaries of all loaded fleets.
func (r *Registry) ListFleets() []FleetSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]FleetSummary, 0, len(r.fleets))
	for key, f := range r.fleets {
		names := make([]string, 0, len(f.Agents))
		for agentKey := range f.Agents {
			names = append(names, agentKey)
		}
		summaries = append(summaries, FleetSummary{
			Key:         key,
			Name:        f.Name,
			Description: f.Description,
			AgentCount:  len(f.Agents),
			AgentNames:  names,
		})
	}

	return summaries
}

// Count returns the number of loaded fleets.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.fleets)
}

// Dir returns the directory this registry reads from.
func (r *Registry) Dir() string {
	return r.dir
}

// Save persists a fleet definition to disk and updates the in-memory registry.
func (r *Registry) Save(key string, fleet *FleetConfig) error {
	if err := SaveFleet(r.dir, key, fleet); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.fleets[key] = fleet

	return nil
}

// Delete removes a fleet from disk and from the in-memory registry.
func (r *Registry) Delete(key string) error {
	if err := DeleteFleet(r.dir, key); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.fleets, key)

	return nil
}

// AllFleets returns a snapshot of all loaded fleet configs, keyed by filename stem.
// The returned map is a shallow copy safe for concurrent read access.
func (r *Registry) AllFleets() map[string]*FleetConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]*FleetConfig, len(r.fleets))
	for k, v := range r.fleets {
		result[k] = v
	}
	return result
}
