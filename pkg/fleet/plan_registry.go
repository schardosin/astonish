package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// PlanSummary is a lightweight view of a fleet plan for listing.
type PlanSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedFrom string   `json:"created_from,omitempty"`
	ChannelType string   `json:"channel_type"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
}

// PlanRegistry manages fleet plans stored as YAML files on disk.
type PlanRegistry struct {
	dir   string
	plans map[string]*FleetPlan
	mu    sync.RWMutex
}

// NewPlanRegistry creates a PlanRegistry by loading all plans from dir.
// If dir does not exist, an empty registry is returned.
func NewPlanRegistry(dir string) (*PlanRegistry, error) {
	r := &PlanRegistry{
		dir:   dir,
		plans: make(map[string]*FleetPlan),
	}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads all fleet plan files from the directory.
func (r *PlanRegistry) Reload() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			r.mu.Lock()
			r.plans = make(map[string]*FleetPlan)
			r.mu.Unlock()
			return nil
		}
		return fmt.Errorf("reading fleet plans directory %s: %w", r.dir, err)
	}

	plans := make(map[string]*FleetPlan)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(r.dir, name)
		plan, loadErr := LoadFleetPlan(path)
		if loadErr != nil {
			return loadErr
		}

		key := strings.TrimSuffix(name, filepath.Ext(name))
		plan.Key = key
		plans[key] = plan
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans = plans
	return nil
}

// GetPlan returns a fleet plan by key.
func (r *PlanRegistry) GetPlan(key string) (*FleetPlan, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plans[key]
	return p, ok
}

// ListPlans returns summaries of all loaded fleet plans.
func (r *PlanRegistry) ListPlans() []PlanSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]PlanSummary, 0, len(r.plans))
	for key, p := range r.plans {
		names := make([]string, 0, len(p.Agents))
		for agentKey := range p.Agents {
			names = append(names, agentKey)
		}
		summaries = append(summaries, PlanSummary{
			Key:         key,
			Name:        p.Name,
			Description: p.Description,
			CreatedFrom: p.CreatedFrom,
			ChannelType: p.Channel.Type,
			AgentCount:  len(p.Agents),
			AgentNames:  names,
		})
	}
	return summaries
}

// Save persists a fleet plan to disk and updates the in-memory registry.
func (r *PlanRegistry) Save(plan *FleetPlan) error {
	if strings.TrimSpace(plan.Key) == "" {
		return fmt.Errorf("fleet plan key is required")
	}
	if strings.TrimSpace(plan.Name) == "" {
		return fmt.Errorf("fleet plan name is required")
	}

	now := time.Now()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now

	if err := os.MkdirAll(r.dir, 0755); err != nil {
		return fmt.Errorf("creating fleet plans directory: %w", err)
	}

	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshalling fleet plan %q: %w", plan.Name, err)
	}

	path := filepath.Join(r.dir, plan.Key+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing fleet plan file %s: %w", path, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans[plan.Key] = plan
	return nil
}

// Delete removes a fleet plan from disk and the in-memory registry.
func (r *PlanRegistry) Delete(key string) error {
	path := filepath.Join(r.dir, key+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("fleet plan %q not found", key)
		}
		return fmt.Errorf("deleting fleet plan %q: %w", key, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plans, key)
	return nil
}

// Count returns the number of loaded fleet plans.
func (r *PlanRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plans)
}

// Dir returns the directory this registry reads from.
func (r *PlanRegistry) Dir() string {
	return r.dir
}

// LoadFleetPlan reads and parses a single fleet plan YAML file.
func LoadFleetPlan(path string) (*FleetPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading fleet plan file %s: %w", path, err)
	}

	var plan FleetPlan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing fleet plan file %s: %w", path, err)
	}

	// Default channel type to "chat"
	if plan.Channel.Type == "" {
		plan.Channel.Type = "chat"
	}

	return &plan, nil
}
