package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
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
//
// IMPORTANT: Every mutation (Add, Remove, Update, AddFleetPlan) reloads the
// registry from disk before modifying and saving. This prevents a long-lived
// in-memory instance (e.g., in the daemon) from overwriting changes made by
// another process (e.g., CLI `sandbox template delete`). Without this, the
// daemon's stale in-memory map would resurrect deleted templates on the next
// Save() call.
type TemplateRegistry struct {
	mu        sync.Mutex
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

	if err := r.loadLocked(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load template registry: %w", err)
	}

	return r, nil
}

// loadLocked reads the template registry from disk. Caller must hold mu.
func (r *TemplateRegistry) loadLocked() error {
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

// saveLocked writes the template registry to disk. Caller must hold mu.
func (r *TemplateRegistry) saveLocked() error {
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

// Load reads the template registry from disk, replacing in-memory state.
func (r *TemplateRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked()
}

// Save writes the template registry to disk.
func (r *TemplateRegistry) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

// Get returns the metadata for a template, or nil if not found.
func (r *TemplateRegistry) Get(name string) *TemplateMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.templates[name]
}

// Add adds or replaces a template in the registry and saves.
// It reloads from disk first to avoid overwriting changes from other processes.
func (r *TemplateRegistry) Add(meta *TemplateMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reload from disk so we don't clobber changes made by other processes
	// (e.g., CLI deleting a template while the daemon is running).
	if err := r.loadLocked(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to reload registry before add: %w", err)
	}

	r.templates[meta.Name] = meta
	return r.saveLocked()
}

// Update updates an existing template in the registry and saves.
func (r *TemplateRegistry) Update(meta *TemplateMeta) error {
	return r.Add(meta) // same operation
}

// Remove deletes a template from the registry and saves.
// It reloads from disk first to avoid overwriting changes from other processes.
func (r *TemplateRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.loadLocked(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to reload registry before remove: %w", err)
	}

	delete(r.templates, name)
	return r.saveLocked()
}

// List returns all templates in the registry.
func (r *TemplateRegistry) List() []*TemplateMeta {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]*TemplateMeta, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}

	return result
}

// Exists checks if a template exists in the registry.
func (r *TemplateRegistry) Exists(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.templates[name]
	return ok
}

// AddFleetPlan associates a fleet plan with a template.
// It reloads from disk first to avoid overwriting changes from other processes.
func (r *TemplateRegistry) AddFleetPlan(templateName, planKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.loadLocked(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to reload registry before AddFleetPlan: %w", err)
	}

	meta, ok := r.templates[templateName]
	if !ok {
		return fmt.Errorf("template %q not found", templateName)
	}

	// Avoid duplicates
	for _, p := range meta.FleetPlans {
		if p == planKey {
			return nil
		}
	}

	meta.FleetPlans = append(meta.FleetPlans, planKey)
	return r.saveLocked()
}

// sandboxDataDir returns the directory for sandbox data files.
// When running under sudo, resolves the real user's home via SUDO_USER
// so that data files are consistent regardless of whether sudo is used.
func sandboxDataDir() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := realUserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine home directory: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	return filepath.Join(dataHome, "astonish", "sandbox"), nil
}

// realUserHomeDir returns the home directory of the real (non-root) user.
// When running under sudo, SUDO_USER identifies the original user and we
// resolve their home directory. This ensures sandbox data files (sessions.json,
// templates.json) are stored in the same location whether the command is run
// with or without sudo.
func realUserHomeDir() (string, error) {
	if os.Getuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil {
				return u.HomeDir, nil
			}
		}
	}
	return os.UserHomeDir()
}
