package astonish

import (
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
