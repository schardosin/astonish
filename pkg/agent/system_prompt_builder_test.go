package agent

import (
	"strings"
	"testing"
)

func TestAgentIdentity_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		identity *AgentIdentity
		want     bool
	}{
		{"nil", nil, false},
		{"empty", &AgentIdentity{}, false},
		{"name only", &AgentIdentity{Name: "Bot"}, true},
		{"username only", &AgentIdentity{Username: "bot"}, true},
		{"email only", &AgentIdentity{Email: "bot@example.com"}, true},
		{"bio only (not enough)", &AgentIdentity{Bio: "A bot"}, false},
		{"full identity", &AgentIdentity{
			Name:     "Astonish Bot",
			Username: "astonish_ai",
			Email:    "bot@example.com",
			Bio:      "An AI assistant",
			Website:  "https://example.com",
			Locale:   "en-US",
			Timezone: "America/New_York",
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.IsConfigured()
			if got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSystemPromptBuilder_IdentitySection(t *testing.T) {
	builder := &SystemPromptBuilder{
		Identity: &AgentIdentity{
			Name:     "Astonish Bot",
			Username: "astonish_ai",
			Email:    "bot@example.com",
			Bio:      "An AI assistant",
			Website:  "https://example.com",
			Locale:   "en-US",
			Timezone: "America/New_York",
		},
	}

	prompt := builder.Build()

	checks := []string{
		"## Agent Identity",
		"**Name:** Astonish Bot",
		"**Username:** astonish_ai",
		"**Email:** bot@example.com",
		"**Bio:** An AI assistant",
		"**Website:** https://example.com",
		"**Locale:** en-US",
		"**Timezone:** America/New_York",
		"email_wait",
		"browser_request_human",
		"credential store for passwords",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("expected prompt to contain %q", check)
		}
	}
}

func TestSystemPromptBuilder_IdentitySection_Partial(t *testing.T) {
	builder := &SystemPromptBuilder{
		Identity: &AgentIdentity{
			Name:  "Bot",
			Email: "bot@example.com",
		},
	}

	prompt := builder.Build()

	if !strings.Contains(prompt, "## Agent Identity") {
		t.Error("expected identity section")
	}
	if !strings.Contains(prompt, "**Name:** Bot") {
		t.Error("expected name")
	}
	if !strings.Contains(prompt, "**Email:** bot@example.com") {
		t.Error("expected email")
	}
	// Username not set, should not appear
	if strings.Contains(prompt, "**Username:**") {
		t.Error("username should not appear when not set")
	}
	// Bio not set
	if strings.Contains(prompt, "**Bio:**") {
		t.Error("bio should not appear when not set")
	}
}

func TestSystemPromptBuilder_NoIdentitySection(t *testing.T) {
	// Without identity
	builder := &SystemPromptBuilder{}
	prompt := builder.Build()

	if strings.Contains(prompt, "## Agent Identity") {
		t.Error("identity section should not appear when identity is nil")
	}

	// With empty identity
	builder2 := &SystemPromptBuilder{
		Identity: &AgentIdentity{},
	}
	prompt2 := builder2.Build()

	if strings.Contains(prompt2, "## Agent Identity") {
		t.Error("identity section should not appear when identity is unconfigured")
	}
}

func TestSystemPromptBuilder_HandoffGuidance(t *testing.T) {
	builder := &SystemPromptBuilder{
		BrowserAvailable: true,
		Tools:            mockTools("browser_navigate", "browser_request_human"),
	}

	prompt := builder.Build()

	if !strings.Contains(prompt, "Human-in-the-loop") {
		t.Error("expected handoff guidance section in browser automation")
	}
	if !strings.Contains(prompt, "CAPTCHAs") {
		t.Error("expected CAPTCHA mention in handoff guidance")
	}
	if !strings.Contains(prompt, "chrome://inspect") {
		t.Error("expected chrome://inspect mention")
	}
}

func TestSystemPromptBuilder_NoHandoffGuidance(t *testing.T) {
	builder := &SystemPromptBuilder{
		BrowserAvailable: true,
		Tools:            mockTools("browser_navigate"),
	}

	prompt := builder.Build()

	// Browser section should be present
	if !strings.Contains(prompt, "## Browser Automation") {
		t.Error("expected browser automation section")
	}
	// But no handoff guidance since tool is not registered
	if strings.Contains(prompt, "Human-in-the-loop") {
		t.Error("handoff guidance should not appear without browser_request_human tool")
	}
}

func TestSystemPromptBuilder_HasHandoffTool(t *testing.T) {
	builder := &SystemPromptBuilder{
		Tools: mockTools("browser_navigate", "browser_request_human"),
	}
	if !builder.hasHandoffTool() {
		t.Error("expected hasHandoffTool() to return true")
	}

	builder2 := &SystemPromptBuilder{
		Tools: mockTools("browser_navigate"),
	}
	if builder2.hasHandoffTool() {
		t.Error("expected hasHandoffTool() to return false without handoff tool")
	}

	builder3 := &SystemPromptBuilder{}
	if builder3.hasHandoffTool() {
		t.Error("expected hasHandoffTool() to return false with no tools")
	}
}
