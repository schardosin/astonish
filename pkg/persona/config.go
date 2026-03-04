package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PersonaConfig represents a single persona definition loaded from YAML.
// Personas define *who* an agent is: identity, expertise, and system prompt.
// They are immutable and reusable across multiple fleets.
type PersonaConfig struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Prompt      string   `yaml:"prompt" json:"prompt"`
	Expertise   []string `yaml:"expertise,omitempty" json:"expertise,omitempty"`
}

// Validate checks that required fields are present and non-empty.
func (p *PersonaConfig) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("persona name is required")
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return fmt.Errorf("persona %q: prompt is required", p.Name)
	}
	return nil
}

// LoadPersona reads and parses a single persona YAML file.
func LoadPersona(path string) (*PersonaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading persona file %s: %w", path, err)
	}

	var persona PersonaConfig
	if err := yaml.Unmarshal(data, &persona); err != nil {
		return nil, fmt.Errorf("parsing persona file %s: %w", path, err)
	}

	if err := persona.Validate(); err != nil {
		return nil, fmt.Errorf("validating persona file %s: %w", path, err)
	}

	return &persona, nil
}

// LoadPersonas reads all .yaml/.yml files from the given directory
// and returns a map keyed by filename stem (e.g. "developer" for developer.yaml).
func LoadPersonas(dir string) (map[string]*PersonaConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading personas directory %s: %w", dir, err)
	}

	personas := make(map[string]*PersonaConfig)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		persona, err := LoadPersona(path)
		if err != nil {
			return nil, err
		}

		// Key by filename without extension
		key := strings.TrimSuffix(name, filepath.Ext(name))
		personas[key] = persona
	}

	return personas, nil
}

// SavePersona writes a persona config to a YAML file.
func SavePersona(dir string, key string, persona *PersonaConfig) error {
	if err := persona.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating personas directory: %w", err)
	}

	data, err := yaml.Marshal(persona)
	if err != nil {
		return fmt.Errorf("marshalling persona %q: %w", persona.Name, err)
	}

	path := filepath.Join(dir, key+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing persona file %s: %w", path, err)
	}

	return nil
}

// DeletePersona removes a persona YAML file from the directory.
func DeletePersona(dir string, key string) error {
	path := filepath.Join(dir, key+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("persona %q not found", key)
		}
		return fmt.Errorf("deleting persona %q: %w", key, err)
	}
	return nil
}
