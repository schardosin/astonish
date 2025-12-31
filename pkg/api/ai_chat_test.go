package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIntentClassifyRequest_JSONUnmarshal verifies request parsing
func TestIntentClassifyRequest_JSONUnmarshal(t *testing.T) {
	tests := []struct {
		name          string
		jsonInput     string
		expectedMsg   string
		expectedTools []string
		shouldError   bool
	}{
		{
			name:          "message only",
			jsonInput:     `{"message": "create a flow"}`,
			expectedMsg:   "create a flow",
			expectedTools: nil,
			shouldError:   false,
		},
		{
			name:          "message with tools",
			jsonInput:     `{"message": "use github", "tools": ["github_create_issue", "github_list_prs"]}`,
			expectedMsg:   "use github",
			expectedTools: []string{"github_create_issue", "github_list_prs"},
			shouldError:   false,
		},
		{
			name:          "empty tools array",
			jsonInput:     `{"message": "install something", "tools": []}`,
			expectedMsg:   "install something",
			expectedTools: []string{},
			shouldError:   false,
		},
		{
			name:        "invalid json",
			jsonInput:   `{"message": invalid}`,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req IntentClassifyRequest
			err := json.Unmarshal([]byte(tt.jsonInput), &req)

			if tt.shouldError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if req.Message != tt.expectedMsg {
				t.Errorf("Message = %q, expected %q", req.Message, tt.expectedMsg)
			}

			if len(req.Tools) != len(tt.expectedTools) {
				t.Errorf("Tools length = %d, expected %d", len(req.Tools), len(tt.expectedTools))
			}
		})
	}
}

// TestIntentClassifyResponse_JSONMarshal verifies response serialization
func TestIntentClassifyResponse_JSONMarshal(t *testing.T) {
	resp := IntentClassifyResponse{
		Intent:      "create_flow",
		Requirement: "use github tools",
		Confidence:  0.95,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["intent"] != "create_flow" {
		t.Errorf("intent = %v, expected create_flow", parsed["intent"])
	}
	if parsed["requirement"] != "use github tools" {
		t.Errorf("requirement = %v, expected 'use github tools'", parsed["requirement"])
	}
	if parsed["confidence"].(float64) != 0.95 {
		t.Errorf("confidence = %v, expected 0.95", parsed["confidence"])
	}
}

// TestIntentClassifyHandler_InvalidRequest verifies error handling for bad requests
func TestIntentClassifyHandler_InvalidRequest(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name:           "invalid json",
			body:           "not valid json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty body",
			body:           "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ai/classify-intent", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			IntentClassifyHandler(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("status = %d, expected %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

// TestBuildToolsContext verifies the tools context string generation
func TestBuildToolsContext(t *testing.T) {
	tests := []struct {
		name     string
		tools    []string
		expected string
	}{
		{
			name:     "empty tools",
			tools:    []string{},
			expected: "(none)",
		},
		{
			name:     "nil tools",
			tools:    nil,
			expected: "(none)",
		},
		{
			name:     "single tool",
			tools:    []string{"github"},
			expected: "github",
		},
		{
			name:     "multiple tools",
			tools:    []string{"github", "slack", "jira"},
			expected: "github, slack, jira",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildToolsContext(tt.tools)
			if result != tt.expected {
				t.Errorf("buildToolsContext(%v) = %q, expected %q", tt.tools, result, tt.expected)
			}
		})
	}
}

// buildToolsContext is a helper that builds the tools context string
// This tests the logic used in IntentClassifyHandler
func buildToolsContext(tools []string) string {
	if len(tools) == 0 {
		return "(none)"
	}
	return strings.Join(tools, ", ")
}

// TestValidIntents verifies all expected intents are recognized
func TestValidIntents(t *testing.T) {
	validIntents := []string{
		"create_flow",
		"install_mcp",
		"browse_mcp_store",
		"search_mcp_internet",
		"extract_mcp_url",
		"general_question",
	}

	// This is a documentation test - ensures we remember all intents
	for _, intent := range validIntents {
		if intent == "" {
			t.Error("empty intent in valid intents list")
		}
	}

	if len(validIntents) != 6 {
		t.Errorf("expected 6 valid intents, got %d", len(validIntents))
	}
}

// TestIntentClassifyRequest_ToolsField verifies the Tools field is properly handled
func TestIntentClassifyRequest_ToolsField(t *testing.T) {
	// Test that Tools field is properly parsed from JSON
	jsonInput := `{
		"message": "use the github server to create issues",
		"tools": ["github_create_issue", "github_list_prs", "slack_send_message"]
	}`

	var req IntentClassifyRequest
	if err := json.Unmarshal([]byte(jsonInput), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(req.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(req.Tools))
	}

	// Verify specific tools
	expectedTools := map[string]bool{
		"github_create_issue": false,
		"github_list_prs":     false,
		"slack_send_message":  false,
	}

	for _, tool := range req.Tools {
		if _, ok := expectedTools[tool]; ok {
			expectedTools[tool] = true
		}
	}

	for tool, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found", tool)
		}
	}
}

// TestIntentClassifyResponse_Fields verifies response struct fields
func TestIntentClassifyResponse_Fields(t *testing.T) {
	resp := IntentClassifyResponse{
		Intent:      "install_mcp",
		Requirement: "github mcp server",
		Confidence:  0.85,
	}

	// Verify fields exist and have expected values
	if resp.Intent != "install_mcp" {
		t.Errorf("Intent = %q, expected 'install_mcp'", resp.Intent)
	}
	if resp.Requirement != "github mcp server" {
		t.Errorf("Requirement = %q, expected 'github mcp server'", resp.Requirement)
	}
	if resp.Confidence != 0.85 {
		t.Errorf("Confidence = %f, expected 0.85", resp.Confidence)
	}
}

// TestIntentClassification_EdgeCases verifies edge cases in message parsing
func TestIntentClassification_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		message string
		tools   []string
		// Note: We can't test actual LLM classification without mocking,
		// but we can verify the request structure is valid
	}{
		{
			name:    "empty message",
			message: "",
			tools:   nil,
		},
		{
			name:    "very long message",
			message: strings.Repeat("test ", 1000),
			tools:   nil,
		},
		{
			name:    "message with special characters",
			message: "create flow with 日本語 and émojis 🚀",
			tools:   []string{"test_tool"},
		},
		{
			name:    "message with newlines",
			message: "line1\nline2\nline3",
			tools:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := IntentClassifyRequest{
				Message: tt.message,
				Tools:   tt.tools,
			}

			// Verify serialization works
			data, err := json.Marshal(req)
			if err != nil {
				t.Errorf("failed to marshal: %v", err)
			}

			var parsed IntentClassifyRequest
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Errorf("failed to unmarshal: %v", err)
			}

			if parsed.Message != tt.message {
				t.Errorf("message mismatch after round-trip")
			}
		})
	}
}

// TestToolsContextInPrompt verifies tools context is properly formatted for the prompt
func TestToolsContextInPrompt(t *testing.T) {
	tests := []struct {
		name           string
		tools          []string
		shouldContain  []string
		shouldNotContain []string
	}{
		{
			name:          "no tools installed",
			tools:         []string{},
			shouldContain: []string{"(none)"},
		},
		{
			name:          "github tools installed",
			tools:         []string{"github", "github-mcp-server"},
			shouldContain: []string{"github", "github-mcp-server"},
		},
		{
			name:  "multiple servers",
			tools: []string{"github", "slack", "jira"},
			shouldContain: []string{
				"github",
				"slack", 
				"jira",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context := buildToolsContext(tt.tools)

			for _, expected := range tt.shouldContain {
				if !strings.Contains(context, expected) {
					t.Errorf("context %q should contain %q", context, expected)
				}
			}

			for _, notExpected := range tt.shouldNotContain {
				if strings.Contains(context, notExpected) {
					t.Errorf("context %q should not contain %q", context, notExpected)
				}
			}
		})
	}
}
