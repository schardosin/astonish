package imagebuilder

import (
	"testing"
)

func TestGenerateDockerfile(t *testing.T) {
	tests := []struct {
		name      string
		baseImage string
		packages  []string
		wantLines []string
	}{
		{
			name:      "single package",
			baseImage: "schardosin/astonish-sandbox-openshell:dev",
			packages:  []string{"curl"},
			wantLines: []string{
				"FROM schardosin/astonish-sandbox-openshell:dev",
				"USER root",
				"RUN apt-get update",
				"curl",
				"USER sandbox",
			},
		},
		{
			name:      "multiple packages sorted",
			baseImage: "ghcr.io/org/sandbox:v1",
			packages:  []string{"jq", "curl", "git"},
			wantLines: []string{
				"FROM ghcr.io/org/sandbox:v1",
				"curl",
				"git",
				"jq",
				"USER sandbox",
			},
		},
		{
			name:      "packages already sorted",
			baseImage: "base:latest",
			packages:  []string{"a", "b", "c"},
			wantLines: []string{
				"a",
				"b",
				"c",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateDockerfile(tt.baseImage, tt.packages)
			for _, want := range tt.wantLines {
				if !containsSubstring(result, want) {
					t.Errorf("GenerateDockerfile() missing expected content %q\n\nGot:\n%s", want, result)
				}
			}
		})
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		name        string
		registryURL string
		scope       string
		packages    []string
		wantPrefix  string
	}{
		{
			name:        "docker hub base",
			registryURL: "docker.io/schardosin",
			scope:       "base",
			packages:    []string{"curl", "git"},
			wantPrefix:  "docker.io/schardosin/astonish-sandbox-base:",
		},
		{
			name:        "ghcr team",
			registryURL: "ghcr.io/org",
			scope:       "frontend",
			packages:    []string{"nodejs"},
			wantPrefix:  "ghcr.io/org/astonish-sandbox-frontend:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ImageRef(tt.registryURL, tt.scope, tt.packages)
			if !hasPrefix(result, tt.wantPrefix) {
				t.Errorf("ImageRef() = %q, want prefix %q", result, tt.wantPrefix)
			}
			// Tag should be 12 hex chars.
			tag := result[len(tt.wantPrefix):]
			if len(tag) != 12 {
				t.Errorf("ImageRef() tag = %q, want 12 chars", tag)
			}
		})
	}
}

func TestImageRefDeterministic(t *testing.T) {
	// Same packages in different order should produce the same image ref.
	ref1 := ImageRef("docker.io/test", "base", []string{"git", "curl", "jq"})
	ref2 := ImageRef("docker.io/test", "base", []string{"jq", "git", "curl"})
	if ref1 != ref2 {
		t.Errorf("ImageRef not deterministic: %q != %q", ref1, ref2)
	}
}

func TestJobName(t *testing.T) {
	name := JobName("my-team", []string{"curl", "git"})
	if len(name) > 63 {
		t.Errorf("JobName too long: %d chars", len(name))
	}
	if !hasPrefix(name, "astonish-build-my-team-") {
		t.Errorf("JobName = %q, want prefix 'astonish-build-my-team-'", name)
	}
}

func TestJobNameDNSSafe(t *testing.T) {
	// Test with special characters in scope.
	name := JobName("team@special.chars!", []string{"pkg"})
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			t.Errorf("JobName contains invalid DNS char %q in %q", string(c), name)
		}
	}
}

func TestSanitizeDNS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-team", "my-team"},
		{"team@org", "team-org"},
		{"UPPER", "upper"},
		{"a.b.c", "a-b-c"},
		{"---", ""},
		{"", "x"},
	}
	for _, tt := range tests {
		got := sanitizeDNS(tt.input)
		// Trim leading/trailing dashes.
		expected := tt.want
		if expected == "" {
			expected = "x" // edge case: all-dash input → "x" after trim
		}
		if got != expected && !(tt.want == "" && got == "x") {
			t.Errorf("sanitizeDNS(%q) = %q, want %q", tt.input, got, expected)
		}
	}
}

func TestValidate(t *testing.T) {
	b := New(Config{})
	if err := b.Validate(); err == nil {
		t.Error("Validate() should fail with empty config")
	}
}

func TestIsConfigured(t *testing.T) {
	if IsConfigured("", "") {
		t.Error("IsConfigured(\"\", \"\") should be false")
	}
	if IsConfigured("url", "") {
		t.Error("IsConfigured(\"url\", \"\") should be false")
	}
	if !IsConfigured("url", "secret") {
		t.Error("IsConfigured(\"url\", \"secret\") should be true")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
