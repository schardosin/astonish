package flowstore

import (
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

	branch := tap.Branch
	if branch == "" {
		branch = "main"
	}

	// Use raw file URL (works for both public and enterprise GitHub)
	rawURL, token, err := buildRawGitHubURL(tap.URL, branch, "manifest.yaml")
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	manifest, err := s.fetchAndParseManifestRaw(rawURL, token)
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

// FetchManifestForceRefresh fetches the manifest from GitHub, bypassing CDN cache
// Uses the commit SHA instead of branch name to guarantee no CDN caching
func (s *Store) FetchManifestForceRefresh(tap *Tap) (*Manifest, error) {
	branch := tap.Branch
	if branch == "" {
		branch = "main"
	}

	// Get the latest commit SHA for the branch
	sha, token, err := s.getLatestCommitSHA(tap.URL, branch)
	if err != nil {
		// Fall back to refs/heads format if SHA lookup fails
		fmt.Fprintf(os.Stderr, "Warning: Could not get latest SHA, falling back to refs/heads: %v\n", err)
		rawURL, token, err := buildRawGitHubURLWithRefs(tap.URL, branch, "manifest.yaml")
		if err != nil {
			return nil, fmt.Errorf("invalid repository URL: %w", err)
		}
		manifest, err := s.fetchAndParseManifestRawFresh(rawURL, token)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch manifest from %s: %w", tap.Name, err)
		}
		if err := s.cacheManifest(tap, manifest); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to cache manifest: %v\n", err)
		}
		tap.Manifest = manifest
		return manifest, nil
	}

	// Build URL using SHA (bypasses all CDN caching)
	repoURL := strings.TrimPrefix(tap.URL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	
	var rawURL string
	if strings.HasPrefix(repoURL, "github.com/") {
		path := strings.TrimPrefix(repoURL, "github.com/")
		rawURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/manifest.yaml", path, sha)
	} else {
		// Enterprise: use refs/heads as fallback
		rawURL, token, _ = buildRawGitHubURLWithRefs(tap.URL, branch, "manifest.yaml")
	}

	manifest, err := s.fetchAndParseManifestRawFresh(rawURL, token)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest from %s: %w", tap.Name, err)
	}

	// Cache the manifest
	if err := s.cacheManifest(tap, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cache manifest: %v\n", err)
	}

	tap.Manifest = manifest
	return manifest, nil
}

// getLatestCommitSHA gets the latest commit SHA for a branch using GitHub API
func (s *Store) getLatestCommitSHA(repoURL, branch string) (sha string, token string, err error) {
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	
	var apiURL string
	
	if strings.HasPrefix(repoURL, "github.com/") {
		path := strings.TrimPrefix(repoURL, "github.com/")
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", path, branch)
		token = os.Getenv("GITHUB_TOKEN")
	} else {
		// Enterprise GitHub
		parts := strings.SplitN(repoURL, "/", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid GitHub URL format")
		}
		host := parts[0]
		repoPath := parts[1]
		apiURL = fmt.Sprintf("https://%s/api/v3/repos/%s/commits/%s", host, repoPath, branch)
		token = os.Getenv("GITHUB_ENTERPRISE_TOKEN")
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", err
	}
	
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github.sha")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("failed to get commit SHA: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	return strings.TrimSpace(string(body)), token, nil
}

// FetchFlow downloads a specific flow YAML file from a tap
func (s *Store) FetchFlow(tap *Tap, flowName string) ([]byte, error) {
	branch := tap.Branch
	if branch == "" {
		branch = "main"
	}

	// Use raw file URL (works for both public and enterprise GitHub)
	filePath := fmt.Sprintf("flows/%s.yaml", flowName)
	rawURL, token, err := buildRawGitHubURL(tap.URL, branch, filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	content, err := s.fetchRawFileContent(rawURL, token)
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

// ForceRefreshAllManifests fetches manifests from remote, ignoring all caches
// including GitHub CDN cache by fetching via commit SHA
func (s *Store) ForceRefreshAllManifests() error {
	var errors []string

	// Force refresh official
	s.clearCachedManifest(s.official)
	if _, err := s.FetchManifestForceRefresh(s.official); err != nil {
		errors = append(errors, fmt.Sprintf("official: %v", err))
	}

	// Force refresh custom taps
	for i := range s.config.Taps {
		tap := &s.config.Taps[i]
		s.clearCachedManifest(tap)
		if _, err := s.FetchManifestForceRefresh(tap); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", tap.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors refreshing manifests:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}

// clearCachedManifest removes the cached manifest for a tap
func (s *Store) clearCachedManifest(tap *Tap) {
	tapCacheDir := filepath.Join(s.storeDir, sanitizeName(tap.Name))
	manifestPath := filepath.Join(tapCacheDir, "manifest.yaml")
	os.Remove(manifestPath)
}

// fetchRawFileContent fetches raw file content from a URL with optional auth
func (s *Store) fetchRawFileContent(url string, token string) ([]byte, error) {
	return s.fetchRawFileContentWithOptions(url, token, false)
}

// fetchRawFileContentFresh fetches raw file content, bypassing all caches
func (s *Store) fetchRawFileContentFresh(url string, token string) ([]byte, error) {
	return s.fetchRawFileContentWithOptions(url, token, true)
}

// fetchRawFileContentWithOptions fetches raw file content with cache control
func (s *Store) fetchRawFileContentWithOptions(url string, token string, noCache bool) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)

	// Add authorization header if token is available
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	// Add cache-busting headers to bypass any HTTP caching
	if noCache {
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Pragma", "no-cache")
		req.Header.Set("Expires", "0")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("authentication required (status %d) - set GITHUB_TOKEN or GITHUB_ENTERPRISE_TOKEN environment variable", resp.StatusCode)
	}

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("file not found (404)")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// fetchAndParseManifestRaw fetches a manifest from a raw URL and parses it
func (s *Store) fetchAndParseManifestRaw(url string, token string) (*Manifest, error) {
	content, err := s.fetchRawFileContent(url, token)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// fetchAndParseManifestRawFresh fetches and parses manifest, bypassing all caches
func (s *Store) fetchAndParseManifestRawFresh(url string, token string) (*Manifest, error) {
	content, err := s.fetchRawFileContentFresh(url, token)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
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
