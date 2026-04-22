package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfMDPath(t *testing.T) {
	path := SelfMDPath("/some/dir")
	expected := filepath.Join("/some/dir", "SELF.md")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestLoadSelfMD_NonExistent(t *testing.T) {
	dir := t.TempDir()
	content, err := LoadSelfMD(dir)
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty string, got: %q", content)
	}
}

func TestWriteAndLoadSelfMD(t *testing.T) {
	dir := t.TempDir()
	content := "# Test SELF.md\n\nSome content.\n"

	if err := WriteSelfMD(dir, content); err != nil {
		t.Fatalf("WriteSelfMD failed: %v", err)
	}

	loaded, err := LoadSelfMD(dir)
	if err != nil {
		t.Fatalf("LoadSelfMD failed: %v", err)
	}
	if loaded != content {
		t.Errorf("expected %q, got %q", content, loaded)
	}
}

func TestWriteSelfMD_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "memory")

	if err := WriteSelfMD(dir, "test content"); err != nil {
		t.Fatalf("WriteSelfMD failed: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestGenerateSelfMD_MinimalConfig(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName: "openai",
		ModelName:    "gpt-4",
	}

	content := GenerateSelfMD(cfg)

	checks := []string{
		"# Astonish Self-Configuration",
		"Provider: openai",
		"Model: gpt-4",
		"## Tools",
		"## Memory System",
		"## Self-Management",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected content to contain %q", check)
		}
	}
}

func TestGenerateSelfMD_WithProviders(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName: "openai",
		ModelName:    "gpt-4",
		Providers: map[string]string{
			"openai":    "openai: gpt-4",
			"anthropic": "anthropic: claude-3-opus",
		},
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "## All Configured Providers") {
		t.Error("expected providers section")
	}
	if !strings.Contains(content, "openai") && !strings.Contains(content, "**(active)**") {
		t.Error("expected active provider marker")
	}
	if !strings.Contains(content, "anthropic") {
		t.Error("expected anthropic provider listed")
	}
}

func TestGenerateSelfMD_WithMCPServers(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName: "test",
		ModelName:    "test-model",
		MCPServers: []MCPServerInfo{
			{Name: "tavily", Category: "web", Keyless: false, Active: true},
			{Name: "my-custom-server", Category: "", Keyless: false, Active: true},
		},
		MCPConfigPath: "/path/to/mcp_config.json",
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "## MCP Servers") {
		t.Error("expected MCP servers section")
	}
	if !strings.Contains(content, "tavily") {
		t.Error("expected tavily listed")
	}
	if !strings.Contains(content, "my-custom-server") {
		t.Error("expected custom server listed")
	}
}

func TestGenerateSelfMD_FlowsSectionRemoved(t *testing.T) {
	// FlowEntries still exist in the config struct for backward compatibility
	// but they are no longer rendered in SELF.md.
	cfg := &SelfMDConfig{
		ProviderName: "test",
		ModelName:    "test-model",
		FlowEntries: []FlowInfo{
			{Name: "check_server", Description: "Check server status via SSH"},
		},
	}

	content := GenerateSelfMD(cfg)

	if strings.Contains(content, "## Saved Flows") {
		t.Error("flows section should not be in SELF.md — flow discovery is via search_flows tool")
	}
	if strings.Contains(content, "check_server") {
		t.Error("individual flows should not be listed in SELF.md")
	}
}

func TestGenerateSelfMD_MemoryEnabled(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:  "test",
		ModelName:     "test-model",
		MemoryEnabled: true,
		EmbeddingInfo: "openai (text-embedding-3-small)",
		ChunkCount:    42,
		MemoryDir:     "/path/to/memory",
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "Status: enabled") {
		t.Error("expected memory enabled status")
	}
	if !strings.Contains(content, "openai (text-embedding-3-small)") {
		t.Error("expected embedding info")
	}
	if !strings.Contains(content, "42") {
		t.Error("expected chunk count")
	}
}

func TestGenerateSelfMD_MemoryDisabled(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:  "test",
		ModelName:     "test-model",
		MemoryEnabled: false,
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "Status: disabled") {
		t.Error("expected memory disabled status")
	}
}

func TestGenerateSelfMD_CredentialCLISection(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName: "test",
		ModelName:    "test-model",
	}

	content := GenerateSelfMD(cfg)

	checks := []string{
		"### Credential CLI Commands",
		"astonish credential add <name>",
		"astonish credential list",
		"astonish credential remove <name>",
		"astonish credential test <name>",
		"Interactive TUI form",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected content to contain %q", check)
		}
	}
}

func TestGenerateSelfMD_BrowserHandoff(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:     "test",
		ModelName:        "test-model",
		HandoffAvailable: true,
	}

	content := GenerateSelfMD(cfg)

	checks := []string{
		"## Browser Handoff",
		"browser_request_human",
		"browser_snapshot",
		"CAPTCHA",
		"non-blocking",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected content to contain %q", check)
		}
	}
}

func TestGenerateSelfMD_NoBrowserHandoff(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:     "test",
		ModelName:        "test-model",
		HandoffAvailable: false,
	}

	content := GenerateSelfMD(cfg)

	if strings.Contains(content, "## Browser Handoff") {
		t.Error("browser handoff section should not appear when not available")
	}
}

func TestGenerateSelfMD_AgentIdentity(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:       "test",
		ModelName:          "test-model",
		IdentityConfigured: true,
		IdentityName:       "Astonish Bot",
		IdentityUsername:   "astonish_ai",
		IdentityEmail:      "bot@example.com",
	}

	content := GenerateSelfMD(cfg)

	checks := []string{
		"## Agent Identity",
		"Name: Astonish Bot",
		"Username: astonish_ai",
		"Email: bot@example.com",
		"email tools",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected content to contain %q", check)
		}
	}
}

func TestGenerateSelfMD_AgentIdentityPartial(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:       "test",
		ModelName:          "test-model",
		IdentityConfigured: true,
		IdentityName:       "Bot",
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "## Agent Identity") {
		t.Error("expected identity section")
	}
	if !strings.Contains(content, "Name: Bot") {
		t.Error("expected name")
	}
	// Username and Email not set, should not appear
	if strings.Contains(content, "Username:") {
		t.Error("username should not appear when not set")
	}
	if strings.Contains(content, "Email:") {
		t.Error("email should not appear when not set")
	}
}

func TestGenerateSelfMD_NoAgentIdentity(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:       "test",
		ModelName:          "test-model",
		IdentityConfigured: false,
	}

	content := GenerateSelfMD(cfg)

	if strings.Contains(content, "## Agent Identity") {
		t.Error("identity section should not appear when not configured")
	}
}

func TestGenerateSelfMD_AppsEnabled(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:   "test",
		ModelName:      "test-model",
		AppsEnabled:    true,
		SavedAppsCount: 5,
	}

	content := GenerateSelfMD(cfg)

	checks := []string{
		"## Apps (Generative UI)",
		"astonish-app",
		"useAppData",
		"useAppAction",
		"useAppAI",
		"useAppState",
		"**Saved apps**: 5",
		"sandboxed iframe",
		"Recharts",
		"Lucide React",
		"generative-ui",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected content to contain %q", check)
		}
	}
}

func TestGenerateSelfMD_AppsEnabledNoSaved(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName:   "test",
		ModelName:      "test-model",
		AppsEnabled:    true,
		SavedAppsCount: 0,
	}

	content := GenerateSelfMD(cfg)

	if !strings.Contains(content, "## Apps (Generative UI)") {
		t.Error("expected apps section when enabled")
	}
	if strings.Contains(content, "Saved apps:") {
		t.Error("saved apps count should not appear when zero")
	}
}

func TestGenerateSelfMD_AppsDisabled(t *testing.T) {
	cfg := &SelfMDConfig{
		ProviderName: "test",
		ModelName:    "test-model",
		AppsEnabled:  false,
	}

	content := GenerateSelfMD(cfg)

	if strings.Contains(content, "## Apps (Generative UI)") {
		t.Error("apps section should not appear when not enabled")
	}
}
