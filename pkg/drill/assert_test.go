package drill

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func TestEvaluateContains(t *testing.T) {
	tests := []struct {
		name     string
		assert   *config.AssertConfig
		content  string
		wantPass bool
	}{
		{
			name:     "contains match",
			assert:   &config.AssertConfig{Type: "contains", Expected: "hello"},
			content:  "say hello world",
			wantPass: true,
		},
		{
			name:     "contains no match",
			assert:   &config.AssertConfig{Type: "contains", Expected: "goodbye"},
			content:  "say hello world",
			wantPass: false,
		},
		{
			name:     "contains empty expected matches anything",
			assert:   &config.AssertConfig{Type: "contains", Expected: ""},
			content:  "anything",
			wantPass: true,
		},
		{
			name:     "contains empty content no match",
			assert:   &config.AssertConfig{Type: "contains", Expected: "hello"},
			content:  "",
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Evaluate(tt.assert, tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (message: %s)", result.Passed, tt.wantPass, result.Message)
			}
		})
	}
}

func TestEvaluateNotContains(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		content  string
		wantPass bool
	}{
		{
			name:     "not_contains pass",
			expected: "error",
			content:  "all good",
			wantPass: true,
		},
		{
			name:     "not_contains fail",
			expected: "error",
			content:  "an error occurred",
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Evaluate(&config.AssertConfig{Type: "not_contains", Expected: tt.expected}, tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPass)
			}
		})
	}
}

func TestEvaluateRegex(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		content  string
		wantPass bool
		wantMsg  string
	}{
		{
			name:     "regex match",
			pattern:  `\d+ rows? affected`,
			content:  "5 rows affected",
			wantPass: true,
		},
		{
			name:     "regex no match",
			pattern:  `^SUCCESS$`,
			content:  "partial SUCCESS here",
			wantPass: false,
		},
		{
			name:     "regex invalid pattern",
			pattern:  `[invalid`,
			content:  "anything",
			wantPass: false,
			wantMsg:  "invalid regex pattern",
		},
		{
			name:     "regex single row",
			pattern:  `\d+ rows? affected`,
			content:  "1 row affected",
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Evaluate(&config.AssertConfig{Type: "regex", Expected: tt.pattern}, tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (message: %s)", result.Passed, tt.wantPass, result.Message)
			}
			if tt.wantMsg != "" && result.Message == "" {
				t.Errorf("expected message containing %q, got empty", tt.wantMsg)
			}
		})
	}
}

func TestEvaluateExitCode(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		content  string
		wantPass bool
	}{
		{
			name:     "exit code 0 match",
			expected: "0",
			content:  "0",
			wantPass: true,
		},
		{
			name:     "exit code 0 with whitespace",
			expected: "0",
			content:  "  0\n",
			wantPass: true,
		},
		{
			name:     "exit code mismatch",
			expected: "0",
			content:  "1",
			wantPass: false,
		},
		{
			name:     "exit code non-numeric content",
			expected: "0",
			content:  "error",
			wantPass: false,
		},
		{
			name:     "exit code non-numeric expected",
			expected: "abc",
			content:  "0",
			wantPass: false,
		},
		{
			name:     "exit code 127 match",
			expected: "127",
			content:  "127",
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Evaluate(&config.AssertConfig{Type: "exit_code", Expected: tt.expected}, tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (message: %s)", result.Passed, tt.wantPass, result.Message)
			}
		})
	}
}

func TestEvaluateElementExists(t *testing.T) {
	snapshot := `navigation "Main Menu"
  link "Home" ref=ref1
  link "Dashboard" ref=ref2
  button "Login" ref=ref3`

	tests := []struct {
		name     string
		expected string
		wantPass bool
	}{
		{name: "element found", expected: "Dashboard", wantPass: true},
		{name: "element not found", expected: "Settings", wantPass: false},
		{name: "button found", expected: "Login", wantPass: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Evaluate(&config.AssertConfig{Type: "element_exists", Expected: tt.expected}, snapshot)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPass)
			}
		})
	}
}

func TestEvaluateSemantic(t *testing.T) {
	result := Evaluate(&config.AssertConfig{Type: "semantic", Expected: "page looks correct"}, "anything")
	if result.Passed {
		t.Error("semantic assertions should not pass in deterministic mode")
	}
	if result.Message == "" {
		t.Error("semantic assertions should have an explanatory message")
	}
}

func TestEvaluateSemanticWithLLM(t *testing.T) {
	mockLLM := &testLLMProvider{response: "YES\nThe output matches the condition"}
	ctx := context.Background()

	result := EvaluateSemantic(ctx, &config.AssertConfig{
		Type:     "semantic",
		Expected: "response indicates success",
	}, "status: ok", mockLLM)

	if !result.Passed {
		t.Errorf("expected pass, got message: %s", result.Message)
	}
	if result.Type != "semantic" {
		t.Errorf("Type = %q, want %q", result.Type, "semantic")
	}
}

func TestEvaluateSemanticWithLLMNo(t *testing.T) {
	mockLLM := &testLLMProvider{response: "NO\nThe output shows an error"}
	ctx := context.Background()

	result := EvaluateSemantic(ctx, &config.AssertConfig{
		Type:     "semantic",
		Expected: "response indicates success",
	}, "error: failed", mockLLM)

	if result.Passed {
		t.Error("expected fail when LLM says NO")
	}
	if !strings.Contains(result.Message, "mismatch") {
		t.Errorf("message should contain 'mismatch', got: %s", result.Message)
	}
}

func TestEvaluateSemanticLLMError(t *testing.T) {
	mockLLM := &testLLMProvider{err: fmt.Errorf("API error")}
	ctx := context.Background()

	result := EvaluateSemantic(ctx, &config.AssertConfig{
		Type:     "semantic",
		Expected: "anything",
	}, "content", mockLLM)

	if result.Passed {
		t.Error("expected fail on LLM error")
	}
	if !strings.Contains(result.Message, "LLM evaluation failed") {
		t.Errorf("message should contain 'LLM evaluation failed', got: %s", result.Message)
	}
}

type testLLMProvider struct {
	response string
	err      error
}

func (m *testLLMProvider) EvaluateText(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

func TestEvaluateUnknownType(t *testing.T) {
	result := Evaluate(&config.AssertConfig{Type: "nonexistent", Expected: "x"}, "content")
	if result.Passed {
		t.Error("unknown assertion type should not pass")
	}
	if result.Message == "" {
		t.Error("unknown type should have error message")
	}
}

func TestEvaluateNilAssertion(t *testing.T) {
	result := Evaluate(nil, "content")
	if !result.Passed {
		t.Error("nil assertion should pass (no assertion = pass)")
	}
}

func TestTruncateStr(t *testing.T) {
	short := "hello"
	if truncateStr(short, 10) != "hello" {
		t.Error("short string should not be truncated")
	}

	long := "hello world this is a long string"
	truncated := truncateStr(long, 5)
	if truncated != "hello..." {
		t.Errorf("truncated = %q, want %q", truncated, "hello...")
	}
}
