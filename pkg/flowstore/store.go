// Package flowstore manages flow stores (taps) for sharing AI flows via GitHub.
package flowstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// OfficialStoreName is the name of the official Astonish flow store
	OfficialStoreName = "official"
	// OfficialStoreURL is the GitHub repository for the official store
	OfficialStoreURL = "github.com/schardosin/astonish-flows"
)

// Manifest represents the required manifest.yaml in a tap repository
type Manifest struct {
	Name        string                 `yaml:"name" json:"name"`
	Author      string                 `yaml:"author" json:"author"`
	Description string                 `yaml:"description" json:"description"`
	Flows       map[string]FlowMeta    `yaml:"flows" json:"flows"`
}

// FlowMeta contains metadata about a flow from the manifest
type FlowMeta struct {
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags" json:"tags"`
}

// Tap represents a flow store repository
type Tap struct {
	Name     string   `yaml:"name" json:"name"`         // e.g., "myuser/my-flows" or "official"
	URL      string   `yaml:"url" json:"url"`           // Full GitHub URL
	Branch   string   `yaml:"branch" json:"branch"`     // Git branch (defaults to "main")
	Manifest *Manifest `yaml:"-" json:"-"`              // Cached manifest (not persisted)
}

// Flow represents a flow available in a store
type Flow struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	TapName     string   `json:"tap_name"`     // Which tap this flow belongs to
	Installed   bool     `json:"installed"`    // Whether it's installed locally
	LocalPath   string   `json:"local_path"`   // Path if installed
}

// StoreConfig is persisted in config.yaml
type StoreConfig struct {
	Taps []Tap `yaml:"taps" json:"taps"`
}

// Store manages all flow stores
type Store struct {
	mu       sync.RWMutex
	config   *StoreConfig
	official *Tap
	storeDir string // Directory for installed flows and metadata
}

// NewStore creates a new store manager
func NewStore() (*Store, error) {
	storeDir, err := getStoreDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get store directory: %w", err)
	}

	s := &Store{
		config:   &StoreConfig{Taps: []Tap{}},
		storeDir: storeDir,
		official: &Tap{
			Name:   OfficialStoreName,
			URL:    OfficialStoreURL,
			Branch: "main",
		},
	}

	// Load existing config
	if err := s.loadConfig(); err != nil {
		// Config doesn't exist yet, that's fine
	}

	return s, nil
}

// GetOfficialTap returns the official store tap
func (s *Store) GetOfficialTap() *Tap {
	return s.official
}

// GetTaps returns all custom taps
func (s *Store) GetTaps() []Tap {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Taps
}

// GetAllTaps returns official + custom taps
func (s *Store) GetAllTaps() []*Tap {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	taps := []*Tap{s.official}
	for i := range s.config.Taps {
		taps = append(taps, &s.config.Taps[i])
	}
	return taps
}

// AddTap adds a new tap by GitHub URL with smart naming:
// - "owner" → assumes owner/astonish-flows, tap name = "owner"
// - "owner/repo" → if repo != "astonish-flows", tap name = "owner-repo"
// - alias parameter overrides the tap name
func (s *Store) AddTap(urlOrShorthand string, alias string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse the URL with smart naming
	name, url := parseTapURL(urlOrShorthand)
	
	// Override with alias if provided
	if alias != "" {
		name = alias
	}

	// Check if already exists
	for _, t := range s.config.Taps {
		if t.Name == name {
			return "", fmt.Errorf("tap '%s' already exists", name)
		}
	}

	tap := Tap{
		Name:   name,
		URL:    url,
		Branch: "main",
	}

	s.config.Taps = append(s.config.Taps, tap)
	if err := s.saveConfig(); err != nil {
		return "", err
	}
	return name, nil
}

// RemoveTap removes a tap by name
func (s *Store) RemoveTap(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if name == OfficialStoreName {
		return fmt.Errorf("cannot remove the official store")
	}

	for i, t := range s.config.Taps {
		if t.Name == name {
			s.config.Taps = append(s.config.Taps[:i], s.config.Taps[i+1:]...)
			
			// Also remove installed flows for this tap
			tapDir := filepath.Join(s.storeDir, sanitizeName(name))
			os.RemoveAll(tapDir)
			
			return s.saveConfig()
		}
	}

	return fmt.Errorf("tap '%s' not found", name)
}

// ListAllFlows returns all flows from all taps (requires manifests to be loaded)
func (s *Store) ListAllFlows() []Flow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var flows []Flow

	// Add official flows
	if s.official.Manifest != nil {
		for name, meta := range s.official.Manifest.Flows {
			flows = append(flows, Flow{
				Name:        name,
				Description: meta.Description,
				Tags:        meta.Tags,
				TapName:     OfficialStoreName,
				Installed:   s.isFlowInstalled(OfficialStoreName, name),
			})
		}
	}

	// Add custom tap flows
	for _, tap := range s.config.Taps {
		if tap.Manifest != nil {
			for name, meta := range tap.Manifest.Flows {
				flows = append(flows, Flow{
					Name:        name,
					Description: meta.Description,
					Tags:        meta.Tags,
					TapName:     tap.Name,
					Installed:   s.isFlowInstalled(tap.Name, name),
				})
			}
		}
	}

	return flows
}

// GetStoreDir returns the store directory
func (s *Store) GetStoreDir() string {
	return s.storeDir
}

// isFlowInstalled checks if a flow is installed locally
func (s *Store) isFlowInstalled(tapName, flowName string) bool {
	tapDir := filepath.Join(s.storeDir, sanitizeName(tapName))
	flowPath := filepath.Join(tapDir, flowName+".yaml")
	_, err := os.Stat(flowPath)
	return err == nil
}

// loadConfig loads the store config from the main config file
func (s *Store) loadConfig() error {
	path, err := getStoreConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, s.config)
}

// saveConfig saves the store config
func (s *Store) saveConfig() error {
	path, err := getStoreConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Helper functions

func getStoreDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "store"), nil
}

func getStoreConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "store.json"), nil
}

// parseTapURL parses a GitHub URL or shorthand into name and full URL
// Smart naming rules:
// - "owner" → assumes owner/astonish-flows, name = "owner"
// - "owner/astonish-flows" → name = "owner" (default repo)
// - "owner/custom-repo" → name = "owner-custom-repo"
func parseTapURL(input string) (name, url string) {
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimPrefix(input, "github.com/")
	input = strings.TrimSuffix(input, ".git")
	input = strings.TrimSuffix(input, "/")

	const defaultRepo = "astonish-flows"
	
	parts := strings.Split(input, "/")
	
	var owner, repo string
	if len(parts) == 1 {
		// Just owner, assume default repo
		owner = parts[0]
		repo = defaultRepo
	} else {
		owner = parts[0]
		repo = parts[1]
	}
	
	// Determine tap name based on repo
	if repo == defaultRepo {
		// Default repo: just use owner name
		name = owner
	} else {
		// Custom repo: use owner-repo
		name = owner + "-" + repo
	}
	
	url = "github.com/" + owner + "/" + repo
	return name, url
}

// sanitizeName converts a tap name to a safe directory name
func sanitizeName(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}

// GetFlowsDir returns the new flows directory (for user-created flows)
func GetFlowsDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "flows"), nil
}
