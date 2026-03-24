package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TemplateMeta holds metadata about a container template.
type TemplateMeta struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	SnapshotAt  time.Time `json:"snapshot_at,omitempty"`
	FleetPlans  []string  `json:"fleet_plans,omitempty"`
	BasedOn     string    `json:"based_on,omitempty"`
	BinaryHash  string    `json:"binary_hash,omitempty"` // SHA256 of the astonish binary baked into the template
	Nesting     bool      `json:"nesting,omitempty"`     // true if containers need security.nesting=true (e.g., Docker installed)
}

// TemplateRegistry manages template metadata with JSON file persistence.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*TemplateMeta
	filePath  string
}

// NewTemplateRegistry creates a new registry backed by a JSON file.
func NewTemplateRegistry() (*TemplateRegistry, error) {
	dataDir, err := sandboxDataDir()
	if err != nil {
		return nil, err
	}

	r := &TemplateRegistry{
		templates: make(map[string]*TemplateMeta),
		filePath:  filepath.Join(dataDir, "templates.json"),
	}

	if err := r.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load template registry: %w", err)
	}

	return r, nil
}

// Load reads the template registry from disk.
func (r *TemplateRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return err
	}

	var templates []*TemplateMeta
	if err := json.Unmarshal(data, &templates); err != nil {
		return fmt.Errorf("failed to parse template registry: %w", err)
	}

	r.templates = make(map[string]*TemplateMeta)
	for _, t := range templates {
		r.templates[t.Name] = t
	}

	return nil
}

// Save writes the template registry to disk.
func (r *TemplateRegistry) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	templates := make([]*TemplateMeta, 0, len(r.templates))
	for _, t := range r.templates {
		templates = append(templates, t)
	}

	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal template registry: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(r.filePath), 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	return os.WriteFile(r.filePath, data, 0644)
}

// Get returns the metadata for a template, or nil if not found.
func (r *TemplateRegistry) Get(name string) *TemplateMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[name]
}

// Add adds a new template to the registry and saves.
func (r *TemplateRegistry) Add(meta *TemplateMeta) error {
	r.mu.Lock()
	r.templates[meta.Name] = meta
	r.mu.Unlock()
	return r.Save()
}

// Update updates an existing template in the registry and saves.
func (r *TemplateRegistry) Update(meta *TemplateMeta) error {
	return r.Add(meta) // same operation
}

// Remove deletes a template from the registry and saves.
func (r *TemplateRegistry) Remove(name string) error {
	r.mu.Lock()
	delete(r.templates, name)
	r.mu.Unlock()
	return r.Save()
}

// List returns all templates in the registry.
func (r *TemplateRegistry) List() []*TemplateMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*TemplateMeta, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}

	return result
}

// Exists checks if a template exists in the registry.
func (r *TemplateRegistry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.templates[name]
	return ok
}

// AddFleetPlan associates a fleet plan with a template.
func (r *TemplateRegistry) AddFleetPlan(templateName, planKey string) error {
	r.mu.Lock()
	meta, ok := r.templates[templateName]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("template %q not found", templateName)
	}

	// Avoid duplicates
	for _, p := range meta.FleetPlans {
		if p == planKey {
			r.mu.Unlock()
			return nil
		}
	}

	meta.FleetPlans = append(meta.FleetPlans, planKey)
	r.mu.Unlock()
	return r.Save()
}

// sandboxDataDir returns the directory for sandbox data files.
func sandboxDataDir() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine home directory: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	return filepath.Join(dataHome, "astonish", "sandbox"), nil
}
