package persona

import (
	"fmt"
	"sync"
)

// PersonaSummary is a lightweight view of a persona for listing.
type PersonaSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Expertise   []string `json:"expertise,omitempty"`
}

// Registry manages a collection of persona definitions loaded from disk.
// It provides thread-safe lookup and listing.
type Registry struct {
	dir      string
	personas map[string]*PersonaConfig
	mu       sync.RWMutex
}

// NewRegistry creates a Registry by loading all persona definitions from dir.
// If dir does not exist, an empty registry is returned (not an error).
func NewRegistry(dir string) (*Registry, error) {
	r := &Registry{
		dir:      dir,
		personas: make(map[string]*PersonaConfig),
	}

	if err := r.Reload(); err != nil {
		return nil, err
	}

	return r, nil
}

// Reload re-reads all persona files from the directory.
func (r *Registry) Reload() error {
	personas, err := LoadPersonas(r.dir)
	if err != nil {
		return fmt.Errorf("loading personas from %s: %w", r.dir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if personas == nil {
		r.personas = make(map[string]*PersonaConfig)
	} else {
		r.personas = personas
	}

	return nil
}

// GetPersona returns a persona by its key (filename stem).
func (r *Registry) GetPersona(key string) (*PersonaConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.personas[key]
	return p, ok
}

// ListPersonas returns summaries of all loaded personas.
func (r *Registry) ListPersonas() []PersonaSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]PersonaSummary, 0, len(r.personas))
	for key, p := range r.personas {
		summaries = append(summaries, PersonaSummary{
			Key:         key,
			Name:        p.Name,
			Description: p.Description,
			Expertise:   p.Expertise,
		})
	}

	return summaries
}

// Count returns the number of loaded personas.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.personas)
}

// Dir returns the directory this registry reads from.
func (r *Registry) Dir() string {
	return r.dir
}

// Save persists a persona definition to disk and updates the in-memory registry.
func (r *Registry) Save(key string, persona *PersonaConfig) error {
	if err := SavePersona(r.dir, key, persona); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.personas[key] = persona

	return nil
}

// Delete removes a persona from disk and from the in-memory registry.
func (r *Registry) Delete(key string) error {
	if err := DeletePersona(r.dir, key); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.personas, key)

	return nil
}
