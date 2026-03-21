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

func TestSystemPromptBuilder_SlimPrompt(t *testing.T) {
	// A fully-loaded builder should produce a compact prompt
	builder := &SystemPromptBuilder{
		WorkspaceDir:          "/root",
		InstructionsContent:   "Be helpful.",
		BrowserAvailable:      true,
		MemorySearchAvailable: true,
		WebSearchAvailable:    true,
		Timezone:              "America/New_York",
		Tools: mockTools(
			"read_file", "shell_command", "save_credential",
			"schedule_job", "process_read", "http_request",
			"delegate_tasks", "email_list",
		),
	}

	prompt := builder.Build()

	// Should have compact sections
	if !strings.Contains(prompt, "## Tool Use") {
		t.Error("expected Tool Use section")
	}
	if !strings.Contains(prompt, "## Environment") {
		t.Error("expected Environment section")
	}
	if !strings.Contains(prompt, "## Capabilities") {
		t.Error("expected Capabilities section")
	}
	if !strings.Contains(prompt, "Timezone: America/New_York") {
		t.Error("expected timezone in environment")
	}

	// Capabilities line should list all detected capabilities
	for _, cap := range []string{
		"browser automation", "credential management", "job scheduling",
		"process management", "task delegation", "persistent memory",
		"web search", "email",
	} {
		if !strings.Contains(prompt, cap) {
			t.Errorf("expected capabilities to include %q", cap)
		}
	}

	// Should NOT have old verbose sections (these are now in guidance docs)
	for _, removed := range []string{
		"## Available Tools",
		"## Browser Automation",
		"## Job Scheduling",
		"## Credential Management",
		"## Interactive Commands",
		"## Commands That Open Text Editors",
		"## HTTP Requests",
		"## Task Delegation",
		"## Persistent Memory",
		"## Knowledge Recall",
		"## Self-Configuration",
	} {
		if strings.Contains(prompt, removed) {
			t.Errorf("prompt should NOT contain removed section %q", removed)
		}
	}

	// Should reference memory_search for guidance
	if !strings.Contains(prompt, "memory_search") {
		t.Error("expected guidance hint referencing memory_search")
	}

	// Verify prompt is reasonably compact (under 4000 chars ~ 1000 tokens)
	if len(prompt) > 4000 {
		t.Errorf("prompt too large for slim design: %d chars (target < 4000)", len(prompt))
	}
}

func TestSystemPromptBuilder_DynamicSections(t *testing.T) {
	builder := &SystemPromptBuilder{
		ChannelHints:   "Format as plain text.",
		SchedulerHints: "This is a scheduled run.",
		SessionContext: "You are in fleet wizard mode.",
	}

	prompt := builder.Build()

	if !strings.Contains(prompt, "## Output Constraints") {
		t.Error("expected Output Constraints section")
	}
	if !strings.Contains(prompt, "## Execution Context") {
		t.Error("expected Execution Context section")
	}
	if !strings.Contains(prompt, "## Session Task") {
		t.Error("expected Session Task section")
	}
}

func TestSystemPromptBuilder_KnowledgeInBuild(t *testing.T) {
	// When knowledge/plan fields are set, Build() includes them at the end.
	builder := &SystemPromptBuilder{
		RelevantKnowledge: "**infra/portainer.md** (53%)\nPortainer at 192.168.1.223",
	}
	prompt := builder.Build()

	if !strings.Contains(prompt, "## Knowledge For This Task") {
		t.Error("expected Knowledge For This Task section when RelevantKnowledge is set")
	}
	if !strings.Contains(prompt, "Portainer at 192.168.1.223") {
		t.Error("expected knowledge content in prompt")
	}
}

func TestSystemPromptBuilder_KnowledgeNotInBuildWhenEmpty(t *testing.T) {
	// When no knowledge/plan is set, Build() should not include those sections.
	builder := &SystemPromptBuilder{}
	prompt := builder.Build()

	for _, section := range []string{
		"## Execution Plan",
		"## Knowledge For This Task",
		"### Knowledge From Previous Experience",
	} {
		if strings.Contains(prompt, section) {
			t.Errorf("Build() should NOT contain %q when fields are empty", section)
		}
	}
}

func TestSystemPromptBuilder_ExecutionPlanWithKnowledge(t *testing.T) {
	builder := &SystemPromptBuilder{
		ExecutionPlan:     "Step 1: SSH into server\nStep 2: Run pct list",
		RelevantKnowledge: "Use --verbose flag",
	}
	prompt := builder.Build()

	if !strings.Contains(prompt, "## Execution Plan") {
		t.Error("expected Execution Plan section")
	}
	if !strings.Contains(prompt, "### Knowledge From Previous Experience") {
		t.Error("expected Knowledge From Previous Experience sub-section")
	}
	if !strings.Contains(prompt, "Use --verbose flag") {
		t.Error("expected knowledge content")
	}
	if !strings.Contains(prompt, "Step 1: SSH into server") {
		t.Error("expected plan steps")
	}
}
