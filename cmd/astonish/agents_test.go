package astonish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseTapAddArgs tests the argument parsing for tap add command
func TestParseTapAddArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		expectedURL   string
		expectedAlias string
	}{
		{
			name:          "simple owner/repo",
			args:          []string{"company/flows"},
			expectedURL:   "company/flows",
			expectedAlias: "",
		},
		{
			name:          "simple owner",
			args:          []string{"company"},
			expectedURL:   "company",
			expectedAlias: "",
		},
		{
			name:          "url with --as alias",
			args:          []string{"company/flows", "--as", "c"},
			expectedURL:   "company/flows",
			expectedAlias: "c",
		},
		{
			name:          "enterprise: alias url format",
			args:          []string{"cronus", "github.enterprise.com/cronus/flows"},
			expectedURL:   "github.enterprise.com/cronus/flows",
			expectedAlias: "cronus",
		},
		{
			name:          "enterprise: full url without alias",
			args:          []string{"github.enterprise.com/team/flows"},
			expectedURL:   "github.enterprise.com/team/flows",
			expectedAlias: "",
		},
		{
			name:          "enterprise: full url with --as alias",
			args:          []string{"github.enterprise.com/team/flows", "--as", "team"},
			expectedURL:   "github.enterprise.com/team/flows",
			expectedAlias: "team",
		},
		{
			name:          "https enterprise url with alias",
			args:          []string{"myalias", "https://github.mycompany.com/org/repo"},
			expectedURL:   "https://github.mycompany.com/org/repo",
			expectedAlias: "myalias",
		},
		{
			name:          "empty args",
			args:          []string{},
			expectedURL:   "",
			expectedAlias: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, alias := parseTapAddArgs(tt.args)
			if url != tt.expectedURL {
				t.Errorf("url: expected %q, got %q", tt.expectedURL, url)
			}
			if alias != tt.expectedAlias {
				t.Errorf("alias: expected %q, got %q", tt.expectedAlias, alias)
			}
		})
	}
}

// TestParseFlowRef tests the flow reference parsing
func TestParseFlowRef(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		expectedTap  string
		expectedFlow string
	}{
		{
			name:         "simple flow name",
			ref:          "my_flow",
			expectedTap:  "official",
			expectedFlow: "my_flow",
		},
		{
			name:         "tap/flow format",
			ref:          "mytap/my_flow",
			expectedTap:  "mytap",
			expectedFlow: "my_flow",
		},
		{
			name:         "tap with multiple slashes",
			ref:          "mytap/sub/flow",
			expectedTap:  "mytap",
			expectedFlow: "sub/flow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tap, flow := parseFlowRef(tt.ref)
			if tap != tt.expectedTap {
				t.Errorf("tap: expected %q, got %q", tt.expectedTap, tap)
			}
			if flow != tt.expectedFlow {
				t.Errorf("flow: expected %q, got %q", tt.expectedFlow, flow)
			}
		})
	}
}

// TestHandleImportCommand tests the flow import command
func TestHandleImportCommand(t *testing.T) {
	// Create a temporary directory for test flows
	tempDir := t.TempDir()
	
	// Create a valid test YAML file
	validYAML := `name: test_flow
description: A test flow
nodes:
  - id: start
    type: start
`
	validFilePath := tempDir + "/valid_flow.yaml"
	if err := os.WriteFile(validFilePath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create an invalid YAML file (not a valid agent config)
	invalidYAML := `not: valid
agent: config
`
	invalidFilePath := tempDir + "/invalid_flow.yaml"
	if err := os.WriteFile(invalidFilePath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a non-YAML file
	nonYAMLPath := tempDir + "/not_yaml.txt"
	if err := os.WriteFile(nonYAMLPath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no args",
			args:        []string{},
			expectError: true,
			errorMsg:    "no file specified",
		},
		{
			name:        "file not found",
			args:        []string{"/nonexistent/path/flow.yaml"},
			expectError: true,
			errorMsg:    "file not found",
		},
		{
			name:        "non-yaml file",
			args:        []string{nonYAMLPath},
			expectError: true,
			errorMsg:    "must be a YAML file",
		},
		// Note: config.LoadAgent accepts any valid YAML, so we can't test for invalid flow structure
		// Note: We can't easily test successful import without mocking flowstore.GetFlowsDir()
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleImportCommand(tt.args)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestHandleRemoveCommand tests the flow remove command
func TestHandleRemoveCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no args",
			args:        []string{},
			expectError: true,
			errorMsg:    "no flow name specified",
		},
		{
			name:        "nonexistent flow",
			args:        []string{"nonexistent_flow_name_12345"},
			expectError: true,
			errorMsg:    "flow not found",
		},
		{
			name:        "flow with .yaml extension",
			args:        []string{"nonexistent_flow.yaml"},
			expectError: true,
			errorMsg:    "flow not found",
		},
		{
			name:        "flow with .yml extension",
			args:        []string{"nonexistent_flow.yml"},
			expectError: true,
			errorMsg:    "flow not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleRemoveCommand(tt.args)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestImportCommandArgsParser tests parsing of --as flag
func TestImportCommandArgsParser(t *testing.T) {
	// Test the logic for extracting --as flag value
	tests := []struct {
		name         string
		args         []string
		expectedName string
	}{
		{
			name:         "no --as flag",
			args:         []string{"myflow.yaml"},
			expectedName: "myflow.yaml",
		},
		{
			name:         "with --as flag",
			args:         []string{"myflow.yaml", "--as", "newname"},
			expectedName: "newname.yaml",
		},
		{
			name:         "with --as flag including extension",
			args:         []string{"myflow.yaml", "--as", "newname.yaml"},
			expectedName: "newname.yaml",
		},
		{
			name:         "with --as flag and .yml extension",
			args:         []string{"myflow.yaml", "--as", "newname.yml"},
			expectedName: "newname.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the --as parsing logic from handleImportCommand
			destName := filepath.Base(tt.args[0])
			for i := 1; i < len(tt.args); i++ {
				if tt.args[i] == "--as" && i+1 < len(tt.args) {
					destName = tt.args[i+1]
					if !strings.HasSuffix(destName, ".yaml") && !strings.HasSuffix(destName, ".yml") {
						destName += ".yaml"
					}
					break
				}
			}
			if destName != tt.expectedName {
				t.Errorf("expected %q, got %q", tt.expectedName, destName)
			}
		})
	}
}

