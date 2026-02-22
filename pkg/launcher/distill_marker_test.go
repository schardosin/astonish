package launcher

import (
	"testing"
)

func TestExtractDistillMarker(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic marker",
			input:    "Some response text.\n\n[DISTILL: check server status via SSH]",
			expected: "check server status via SSH",
		},
		{
			name:     "marker in middle of text",
			input:    "Before text [DISTILL: deploy Docker container] after text",
			expected: "deploy Docker container",
		},
		{
			name:     "no marker",
			input:    "Just a regular response without any marker.",
			expected: "",
		},
		{
			name:     "empty description",
			input:    "[DISTILL: ]",
			expected: "",
		},
		{
			name:     "marker with extra spaces",
			input:    "[DISTILL:   create new Go microservice   ]",
			expected: "create new Go microservice",
		},
		{
			name:     "marker in code block",
			input:    "```\n[DISTILL: should still match]\n```",
			expected: "should still match",
		},
		{
			name:     "multiple markers returns first",
			input:    "[DISTILL: first] and [DISTILL: second]",
			expected: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDistillMarker(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestStripDistillMarker(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip marker from end",
			input:    "Response text.\n\n[DISTILL: check server status]",
			expected: "Response text.\n\n",
		},
		{
			name:     "no marker to strip",
			input:    "Just regular text.",
			expected: "Just regular text.",
		},
		{
			name:     "strip marker from middle",
			input:    "Before [DISTILL: something] after",
			expected: "Before  after",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only marker",
			input:    "[DISTILL: entire chunk is marker]",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripDistillMarker(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
