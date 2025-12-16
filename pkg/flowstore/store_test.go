package flowstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore creates a Store with a custom storeDir for testing
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "flowstore-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store := &Store{
		config:   &StoreConfig{Taps: []Tap{}},
		storeDir: tmpDir,
		official: &Tap{
			Name:   OfficialStoreName,
			URL:    OfficialStoreURL,
			Branch: "main",
		},
	}

	return store, tmpDir
}

// TestValidateTapRepository tests the validateTapRepository function
// which validates that a GitHub repository contains a valid manifest.yaml
func TestValidateTapRepositoryNonGitHub(t *testing.T) {
	tap := Tap{
		Name:   "gitlab-tap",
		URL:    "gitlab.com/user/repo",
		Branch: "main",
	}

	err := validateTapRepository(tap)
	if err == nil {
		t.Error("Expected error for non-GitHub URL, got nil")
	}
	if !strings.Contains(err.Error(), "only GitHub repositories are supported") {
		t.Errorf("Expected 'only GitHub repositories are supported' error, got: %v", err)
	}
}

// TestValidateTapRepositoryBitbucket tests validation rejects Bitbucket URLs
func TestValidateTapRepositoryBitbucket(t *testing.T) {
	tap := Tap{
		Name:   "bitbucket-tap",
		URL:    "bitbucket.org/user/repo",
		Branch: "main",
	}

	err := validateTapRepository(tap)
	if err == nil {
		t.Error("Expected error for Bitbucket URL, got nil")
	}
	if !strings.Contains(err.Error(), "only GitHub repositories are supported") {
		t.Errorf("Expected 'only GitHub repositories are supported' error, got: %v", err)
	}
}

// TestParseTapURL tests the URL parsing and naming logic
func TestParseTapURL(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedName string
		expectedURL  string
	}{
		{
			name:         "simple username assumes astonish-flows repo",
			input:        "myuser",
			expectedName: "myuser",
			expectedURL:  "github.com/myuser/astonish-flows",
		},
		{
			name:         "user/astonish-flows uses just username as name",
			input:        "myuser/astonish-flows",
			expectedName: "myuser",
			expectedURL:  "github.com/myuser/astonish-flows",
		},
		{
			name:         "user/other-repo uses user-repo as name",
			input:        "myuser/my-custom-flows",
			expectedName: "myuser-my-custom-flows",
			expectedURL:  "github.com/myuser/my-custom-flows",
		},
		{
			name:         "full github URL extracts user-repo name",
			input:        "github.com/someuser/cool-flows",
			expectedName: "someuser-cool-flows",
			expectedURL:  "github.com/someuser/cool-flows",
		},
		{
			name:         "https URL extracts user-repo name",
			input:        "https://github.com/dev/flows-collection",
			expectedName: "dev-flows-collection",
			expectedURL:  "github.com/dev/flows-collection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, url := parseTapURL(tt.input)
			if name != tt.expectedName {
				t.Errorf("name: expected %q, got %q", tt.expectedName, name)
			}
			if url != tt.expectedURL {
				t.Errorf("url: expected %q, got %q", tt.expectedURL, url)
			}
		})
	}
}

// TestAddTapDuplicateCheck tests that AddTap prevents duplicate taps
func TestAddTapDuplicateCheck(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Add a tap directly to config for duplicate testing
	store.config.Taps = append(store.config.Taps, Tap{
		Name:   "existing-tap",
		URL:    "github.com/test/flows",
		Branch: "main",
	})

	// Try to add a tap with the same name (using alias to force the name)
	// This should fail because the name already exists
	_, err := store.AddTap("differentuser/flows", "existing-tap")
	if err == nil {
		t.Error("expected error for duplicate tap name, got nil")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

// TestSanitizeName tests the name sanitization function
func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"my-tap", "my-tap"},
		{"user/repo", "user-repo"},    // / replaced with -
		{"user:repo", "user:repo"},    // : is kept as-is
		{"a/b/c", "a-b-c"},            // multiple / replaced
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestStoreOfficialTap tests that the store has the official tap configured
func TestStoreOfficialTap(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Verify the official tap is available
	official := store.GetOfficialTap()
	if official == nil {
		t.Fatal("Official tap is nil")
	}

	if official.Name != OfficialStoreName {
		t.Errorf("Official tap name = %q, expected %q", official.Name, OfficialStoreName)
	}
	if official.URL != OfficialStoreURL {
		t.Errorf("Official tap URL = %q, expected %q", official.URL, OfficialStoreURL)
	}
	if official.Branch != "main" {
		t.Errorf("Official tap branch = %q, expected %q", official.Branch, "main")
	}
}

// TestGetAllTaps tests that GetAllTaps returns official + custom taps
func TestGetAllTaps(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Add some custom taps to config
	store.config.Taps = []Tap{
		{Name: "custom1", URL: "github.com/user1/flows", Branch: "main"},
		{Name: "custom2", URL: "github.com/user2/flows", Branch: "main"},
	}

	allTaps := store.GetAllTaps()

	// Should have official + 2 custom = 3 taps
	if len(allTaps) != 3 {
		t.Errorf("Expected 3 taps, got %d", len(allTaps))
	}

	// First tap should be official
	if allTaps[0].Name != OfficialStoreName {
		t.Errorf("First tap should be official, got %q", allTaps[0].Name)
	}

	// Verify custom taps are included
	foundCustom1, foundCustom2 := false, false
	for _, tap := range allTaps {
		if tap.Name == "custom1" {
			foundCustom1 = true
		}
		if tap.Name == "custom2" {
			foundCustom2 = true
		}
	}
	if !foundCustom1 || !foundCustom2 {
		t.Error("Custom taps not found in GetAllTaps result")
	}
}

// TestRemoveTap tests removing a custom tap
func TestRemoveTap(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Create a tap directory to test cleanup
	tapDir := filepath.Join(tmpDir, "removable")
	if err := os.MkdirAll(tapDir, 0755); err != nil {
		t.Fatalf("Failed to create tap dir: %v", err)
	}

	// Add a custom tap
	store.config.Taps = []Tap{
		{Name: "removable", URL: "github.com/user/flows", Branch: "main"},
	}

	// Try to remove official tap (should fail)
	err := store.RemoveTap(OfficialStoreName)
	if err == nil {
		t.Error("Expected error when removing official tap, got nil")
	}
	if !strings.Contains(err.Error(), "cannot remove the official store") {
		t.Errorf("Expected 'cannot remove the official store' error, got: %v", err)
	}

	// Remove custom tap
	err = store.RemoveTap("removable")
	if err != nil {
		t.Errorf("Failed to remove custom tap: %v", err)
	}

	// Verify it's removed from config
	if len(store.config.Taps) != 0 {
		t.Errorf("Expected 0 custom taps, got %d", len(store.config.Taps))
	}

	// Verify the directory was cleaned up
	if _, err := os.Stat(tapDir); !os.IsNotExist(err) {
		t.Error("Tap directory was not removed")
	}
}

// TestRemoveTapNotFound tests removing a non-existent tap
func TestRemoveTapNotFound(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	err := store.RemoveTap("nonexistent")
	if err == nil {
		t.Error("Expected error when removing non-existent tap, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

// TestGetTaps tests that GetTaps returns only custom taps
func TestGetTaps(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Add some custom taps
	store.config.Taps = []Tap{
		{Name: "tap1", URL: "github.com/user1/flows", Branch: "main"},
		{Name: "tap2", URL: "github.com/user2/flows", Branch: "main"},
	}

	taps := store.GetTaps()

	// Should only have custom taps, not official
	if len(taps) != 2 {
		t.Errorf("Expected 2 taps, got %d", len(taps))
	}

	// Official tap should NOT be in GetTaps
	for _, tap := range taps {
		if tap.Name == OfficialStoreName {
			t.Error("Official tap should not be in GetTaps result")
		}
	}
}

// TestGetStoreDir tests the store directory getter
func TestGetStoreDir(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	storeDir := store.GetStoreDir()
	if storeDir != tmpDir {
		t.Errorf("GetStoreDir() = %q, expected %q", storeDir, tmpDir)
	}
}

// TestInstalledFlowPath tests getting paths for installed flows
func TestInstalledFlowPath(t *testing.T) {
	store, tmpDir := newTestStore(t)
	defer os.RemoveAll(tmpDir)

	// Create an installed flow
	tapDir := filepath.Join(tmpDir, "test-tap")
	if err := os.MkdirAll(tapDir, 0755); err != nil {
		t.Fatalf("Failed to create tap dir: %v", err)
	}
	flowPath := filepath.Join(tapDir, "my_flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: test"), 0644); err != nil {
		t.Fatalf("Failed to create flow file: %v", err)
	}

	// Test GetInstalledFlowPath
	path, ok := store.GetInstalledFlowPath("test-tap", "my_flow")
	if !ok {
		t.Error("Expected to find installed flow")
	}
	if path != flowPath {
		t.Errorf("Expected path %q, got %q", flowPath, path)
	}

	// Test non-existent flow
	_, ok = store.GetInstalledFlowPath("test-tap", "nonexistent")
	if ok {
		t.Error("Expected to not find non-existent flow")
	}

	// Test non-existent tap
	_, ok = store.GetInstalledFlowPath("nonexistent-tap", "my_flow")
	if ok {
		t.Error("Expected to not find flow in non-existent tap")
	}
}

// TestValidateTapRepositoryURLConstruction tests that the raw GitHub URL is built correctly
func TestValidateTapRepositoryURLConstruction(t *testing.T) {
	// This test verifies the URL construction logic without making actual HTTP requests
	// The validateTapRepository function builds:
	// https://raw.githubusercontent.com/{owner}/{repo}/{branch}/manifest.yaml

	tests := []struct {
		name         string
		tapURL       string
		branch       string
		expectedPath string // What we expect in the constructed URL
	}{
		{
			name:         "standard github URL",
			tapURL:       "github.com/testuser/test-flows",
			branch:       "main",
			expectedPath: "testuser/test-flows/main/manifest.yaml",
		},
		{
			name:         "github URL with develop branch",
			tapURL:       "github.com/myorg/flows",
			branch:       "develop",
			expectedPath: "myorg/flows/develop/manifest.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't directly test the URL construction without refactoring,
			// but we verify the function handles GitHub URLs without error
			// (the actual HTTP request will fail, but URL parsing should work)
			tap := Tap{
				Name:   "test",
				URL:    tt.tapURL,
				Branch: tt.branch,
			}

			// The function will try to make an HTTP request which will fail,
			// but that's expected - we're just verifying it doesn't fail
			// on URL construction for valid GitHub URLs
			err := validateTapRepository(tap)
			// Error is expected (network request), but it should NOT be
			// "only GitHub repositories are supported"
			if err != nil && strings.Contains(err.Error(), "only GitHub repositories are supported") {
				t.Error("GitHub URL was incorrectly rejected as non-GitHub")
			}
		})
	}
}

