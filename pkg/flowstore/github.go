package flowstore

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	githubAPIBase = "https://api.github.com"
	userAgent     = "astonish-flowstore/1.0"
)

// GitHubFile represents a file from the GitHub API
type GitHubFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
}

// FetchManifest fetches the manifest.yaml from a tap's GitHub repository
func (s *Store) FetchManifest(tap *Tap) (*Manifest, error) {
	// Try to get manifest from cache first
	cached, err := s.loadCachedManifest(tap)
	if err == nil && cached != nil {
		tap.Manifest = cached
		return cached, nil
	}

	// Fetch from GitHub
	owner, repo := parseGitHubURL(tap.URL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid GitHub URL: %s", tap.URL)
	}

	branch := tap.Branch
	if branch == "" {
		branch = "main"
	}

	// Fetch manifest.yaml
	url := fmt.Sprintf("%s/repos/%s/%s/contents/manifest.yaml?ref=%s", githubAPIBase, owner, repo, branch)
	
	manifest, err := s.fetchAndParseManifest(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest from %s: %w", tap.Name, err)
	}

	// Cache the manifest
	if err := s.cacheManifest(tap, manifest); err != nil {
		// Log but don't fail
		fmt.Fprintf(os.Stderr, "Warning: failed to cache manifest: %v\n", err)
	}

	tap.Manifest = manifest
	return manifest, nil
}

// FetchFlow downloads a specific flow YAML file from a tap
func (s *Store) FetchFlow(tap *Tap, flowName string) ([]byte, error) {
	owner, repo := parseGitHubURL(tap.URL)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid GitHub URL: %s", tap.URL)
	}

	branch := tap.Branch
	if branch == "" {
		branch = "main"
	}

	// Fetch flows/{flowName}.yaml
	url := fmt.Sprintf("%s/repos/%s/%s/contents/flows/%s.yaml?ref=%s", 
		githubAPIBase, owner, repo, flowName, branch)
	
	content, err := s.fetchFileContent(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch flow '%s' from %s: %w", flowName, tap.Name, err)
	}

	return content, nil
}

// InstallFlow downloads and caches a flow locally
func (s *Store) InstallFlow(tapName, flowName string) error {
	// Find the tap
	var tap *Tap
	if tapName == OfficialStoreName {
		tap = s.official
	} else {
		for i := range s.config.Taps {
			if s.config.Taps[i].Name == tapName {
				tap = &s.config.Taps[i]
				break
			}
		}
	}

	if tap == nil {
		return fmt.Errorf("tap '%s' not found", tapName)
	}

	// Fetch the flow
	content, err := s.FetchFlow(tap, flowName)
	if err != nil {
		return err
	}

	// Save to cache
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tapName))
	if err := os.MkdirAll(tapCacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	flowPath := filepath.Join(tapCacheDir, flowName+".yaml")
	if err := os.WriteFile(flowPath, content, 0644); err != nil {
		return fmt.Errorf("failed to save flow: %w", err)
	}

	return nil
}

// UninstallFlow removes a cached flow
func (s *Store) UninstallFlow(tapName, flowName string) error {
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tapName))
	flowPath := filepath.Join(tapCacheDir, flowName+".yaml")

	if _, err := os.Stat(flowPath); os.IsNotExist(err) {
		return fmt.Errorf("flow '%s' is not installed", flowName)
	}

	return os.Remove(flowPath)
}

// GetInstalledFlowPath returns the path to an installed flow, if it exists
func (s *Store) GetInstalledFlowPath(tapName, flowName string) (string, bool) {
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tapName))
	flowPath := filepath.Join(tapCacheDir, flowName+".yaml")

	if _, err := os.Stat(flowPath); err == nil {
		return flowPath, true
	}
	return "", false
}

// UpdateAllManifests fetches fresh manifests for all taps
func (s *Store) UpdateAllManifests() error {
	var errors []string

	// Update official
	if _, err := s.FetchManifest(s.official); err != nil {
		errors = append(errors, fmt.Sprintf("official: %v", err))
	}

	// Update custom taps
	for i := range s.config.Taps {
		if _, err := s.FetchManifest(&s.config.Taps[i]); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", s.config.Taps[i].Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors updating manifests:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}

// fetchAndParseManifest fetches a manifest from GitHub API and parses it
func (s *Store) fetchAndParseManifest(url string) (*Manifest, error) {
	content, err := s.fetchFileContent(url)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// fetchFileContent fetches file content from GitHub API
func (s *Store) fetchFileContent(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Use GITHUB_TOKEN if available (for higher rate limits)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("file not found (404)")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var file GitHubFile
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// GitHub returns base64-encoded content
	if file.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 content: %w", err)
		}
		return decoded, nil
	}

	return []byte(file.Content), nil
}

// loadCachedManifest loads a cached manifest from disk
func (s *Store) loadCachedManifest(tap *Tap) (*Manifest, error) {
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tap.Name))
	manifestPath := filepath.Join(tapCacheDir, "manifest.yaml")

	// Check if cache exists and is recent (less than 1 hour old)
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, err
	}

	if time.Since(info.ModTime()) > time.Hour {
		return nil, fmt.Errorf("cache expired")
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// cacheManifest saves a manifest to disk
func (s *Store) cacheManifest(tap *Tap, manifest *Manifest) error {
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tap.Name))
	if err := os.MkdirAll(tapCacheDir, 0755); err != nil {
		return err
	}

	manifestPath := filepath.Join(tapCacheDir, "manifest.yaml")
	
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// parseGitHubURL extracts owner and repo from a GitHub URL
func parseGitHubURL(url string) (owner, repo string) {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "github.com/")
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", ""
}
