// Package flowstore manages flow stores (taps) for sharing AI flows via GitHub.
package flowstore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// OfficialStoreName is the name of the official Astonish flow store
	OfficialStoreName = "official"
	// OfficialStoreURL is the GitHub repository for the official store
	OfficialStoreURL = "github.com/schardosin/astonish-flows"
)

// Manifest represents the required manifest.yaml in a tap repository
type Manifest struct {
	Name        string              `yaml:"name" json:"name"`
	Author      string              `yaml:"author" json:"author"`
	Description string              `yaml:"description" json:"description"`
	Flows       map[string]FlowMeta `yaml:"flows" json:"flows"`
	MCPs        map[string]MCPMeta  `yaml:"mcps" json:"mcps"` // MCP servers from this tap
}

// FlowMeta contains metadata about a flow from the manifest
type FlowMeta struct {
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags" json:"tags"`
}

// MCPMeta contains metadata about an MCP server from the manifest
type MCPMeta struct {
	Description string            `yaml:"description" json:"description"`
	Command     string            `yaml:"command" json:"command"`
	Args        []string          `yaml:"args" json:"args"`
	Env         map[string]string `yaml:"env" json:"env"`
	Tags        []string          `yaml:"tags" json:"tags"`
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

// TappedMCP represents an MCP server available from a tapped repository
type TappedMCP struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Tags        []string          `json:"tags"`
	TapName     string            `json:"tap_name"` // Which tap this MCP belongs to
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
		// Only ignore "file not found" errors - other errors should be logged
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: failed to load store config: %v\n", err)
		}
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

// validateTapRepository checks that a tap repository exists and contains a valid manifest.yaml
func validateTapRepository(tap Tap) error {
	// Build the raw GitHub URL for manifest.yaml
	rawURL, token, err := buildRawGitHubURL(tap.URL, tap.Branch, "manifest.yaml")
	if err != nil {
		return err
	}

	// Create HTTP request with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header if token is available
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to repository: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("authentication required (status %d) - set GITHUB_TOKEN or GITHUB_ENTERPRISE_TOKEN environment variable", resp.StatusCode)
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("repository not found or missing manifest.yaml (check that the repo exists and contains a manifest.yaml file)")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected response from GitHub: %s", resp.Status)
	}

	// Read and validate the manifest
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read manifest.yaml: %w", err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("invalid manifest.yaml format: %w", err)
	}

	// Validate manifest has required fields
	if manifest.Name == "" {
		return fmt.Errorf("manifest.yaml is missing required 'name' field")
	}
	if len(manifest.Flows) == 0 {
		return fmt.Errorf("manifest.yaml has no flows defined")
	}

	return nil
}

// buildRawGitHubURL constructs the raw content URL for a GitHub file and returns the auth token if available
// Supports both public GitHub (github.com) and GitHub Enterprise instances
func buildRawGitHubURL(repoURL, branch, filePath string) (rawURL string, token string, err error) {
	// Normalize URL - remove https:// prefix if present
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	
	// Check if this is public GitHub or enterprise
	isPublicGitHub := strings.HasPrefix(repoURL, "github.com/")
	
	if isPublicGitHub {
		// Public GitHub: use raw.githubusercontent.com
		path := strings.TrimPrefix(repoURL, "github.com/")
		rawURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", path, branch, filePath)
		token = os.Getenv("GITHUB_TOKEN")
	} else if strings.Contains(repoURL, "/") {
		// Enterprise GitHub: extract host and path
		// Format: github.enterprise.com/owner/repo
		parts := strings.SplitN(repoURL, "/", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid GitHub URL format: %s", repoURL)
		}
		host := parts[0]
		repoPath := parts[1]
		
		// Enterprise GitHub raw URL format
		// https://github.enterprise.com/raw/owner/repo/branch/file
		rawURL = fmt.Sprintf("https://%s/raw/%s/%s/%s", host, repoPath, branch, filePath)
		token = os.Getenv("GITHUB_ENTERPRISE_TOKEN")
		
		// Fallback to GITHUB_TOKEN if enterprise token not set
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
	} else {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s (expected: github.com/owner/repo or github.enterprise.com/owner/repo)", repoURL)
	}
	
	return rawURL, token, nil
}

// AddTap adds a new tap by GitHub URL with smart naming:
// - "owner" → assumes owner/astonish-flows, tap name = "owner"
// - "owner/repo" → if repo != "astonish-flows", tap name = "owner-repo"
// - alias parameter overrides the tap name
// Validates that the repository exists and contains a manifest.yaml
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

	// Validate the tap by fetching its manifest
	if err := validateTapRepository(tap); err != nil {
		return "", fmt.Errorf("invalid tap repository: %w", err)
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

// ListAllMCPs returns all MCP servers from all taps (requires manifests to be loaded)
func (s *Store) ListAllMCPs() []TappedMCP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var mcps []TappedMCP

	// Add official MCPs
	if s.official.Manifest != nil && s.official.Manifest.MCPs != nil {
		for name, meta := range s.official.Manifest.MCPs {
			mcps = append(mcps, TappedMCP{
				Name:        name,
				Description: meta.Description,
				Command:     meta.Command,
				Args:        meta.Args,
				Env:         meta.Env,
				Tags:        meta.Tags,
				TapName:     OfficialStoreName,
			})
		}
	}

	// Add custom tap MCPs
	for _, tap := range s.config.Taps {
		if tap.Manifest != nil && tap.Manifest.MCPs != nil {
			for name, meta := range tap.Manifest.MCPs {
				mcps = append(mcps, TappedMCP{
					Name:        name,
					Description: meta.Description,
					Command:     meta.Command,
					Args:        meta.Args,
					Env:         meta.Env,
					Tags:        meta.Tags,
					TapName:     tap.Name,
				})
			}
		}
	}

	return mcps
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

	// Safety check: NEVER overwrite existing taps with an empty list
	// This prevents accidental data loss from bugs or failed loads
	if len(s.config.Taps) == 0 {
		if existingData, err := os.ReadFile(path); err == nil {
			var existingConfig StoreConfig
			if json.Unmarshal(existingData, &existingConfig) == nil && len(existingConfig.Taps) > 0 {
				// File has taps but we're about to save empty - refuse and warn
				fmt.Fprintf(os.Stderr, "Warning: refusing to overwrite %d existing taps with empty config\n", len(existingConfig.Taps))
				return nil // Don't overwrite, just return success
			}
		}
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
// - "github.enterprise.com/owner" → assumes owner/astonish-flows on enterprise
// - "github.enterprise.com/owner/repo" → full enterprise URL
func parseTapURL(input string) (name, url string) {
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimSuffix(input, ".git")
	input = strings.TrimSuffix(input, "/")

	const defaultRepo = "astonish-flows"
	
	// Check if this is an enterprise URL (contains a host with dots before the path)
	// e.g., github.enterprise.com/owner or github.mycompany.com/owner/repo
	isEnterprise := strings.Contains(input, ".") && !strings.HasPrefix(input, "github.com/")
	
	var host, owner, repo string
	
	if isEnterprise {
		// Enterprise: host/owner or host/owner/repo
		parts := strings.SplitN(input, "/", 3)
		if len(parts) < 2 {
			// Invalid format, return as-is
			return sanitizeName(input), input
		}
		host = parts[0]
		owner = parts[1]
		if len(parts) == 3 && parts[2] != "" {
			repo = parts[2]
		} else {
			repo = defaultRepo
		}
		
		// Build tap name
		if repo == defaultRepo {
			name = owner
		} else {
			name = owner + "-" + repo
		}
		
		url = host + "/" + owner + "/" + repo
	} else {
		// Public GitHub: strip github.com/ prefix if present
		input = strings.TrimPrefix(input, "github.com/")
		
		parts := strings.Split(input, "/")
		
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
			name = owner
		} else {
			name = owner + "-" + repo
		}
		
		url = "github.com/" + owner + "/" + repo
	}
	
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
