package openshell

import (
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestResolvePresets_DefaultAll(t *testing.T) {
	// Empty presets = all presets enabled.
	cfg := config.NetworkPolicyConfig{}
	endpoints := ResolvePresets(cfg)

	if len(endpoints) == 0 {
		t.Fatal("expected non-empty endpoints for default (empty) presets")
	}

	// Verify we get endpoints from all presets.
	hostSet := make(map[string]bool)
	for _, ep := range endpoints {
		hostSet[ep.Host] = true
	}

	// Spot-check a host from each preset.
	want := []string{
		"github.com",            // code_hosting
		"registry.npmjs.org",    // package_registries
		"api.openai.com",        // llm_apis
		"api.tavily.com",        // tools
		"ghcr.io",              // system
		"duckduckgo.com",        // search
		"*.cloudflare.com",      // cdn
	}
	for _, h := range want {
		if !hostSet[h] {
			t.Errorf("expected host %q in resolved endpoints", h)
		}
	}
}

func TestResolvePresets_ExplicitDefault(t *testing.T) {
	// "default" preset = all presets.
	cfg := config.NetworkPolicyConfig{
		Presets: []string{"default"},
	}
	endpoints := ResolvePresets(cfg)

	if len(endpoints) == 0 {
		t.Fatal("expected non-empty endpoints for 'default' preset")
	}

	// Should be same count as empty presets.
	cfgEmpty := config.NetworkPolicyConfig{}
	endpointsEmpty := ResolvePresets(cfgEmpty)
	if len(endpoints) != len(endpointsEmpty) {
		t.Errorf("'default' preset produced %d endpoints, empty presets produced %d — should be equal",
			len(endpoints), len(endpointsEmpty))
	}
}

func TestResolvePresets_SelectivePresets(t *testing.T) {
	// Only code_hosting and tools.
	cfg := config.NetworkPolicyConfig{
		Presets: []string{"code_hosting", "tools"},
	}
	endpoints := ResolvePresets(cfg)

	hostSet := make(map[string]bool)
	for _, ep := range endpoints {
		hostSet[ep.Host] = true
	}

	// Should have code_hosting hosts.
	if !hostSet["github.com"] {
		t.Error("expected github.com from code_hosting preset")
	}
	// Should have tools hosts.
	if !hostSet["api.tavily.com"] {
		t.Error("expected api.tavily.com from tools preset")
	}
	// Should NOT have llm_apis hosts.
	if hostSet["api.openai.com"] {
		t.Error("did not expect api.openai.com — llm_apis not selected")
	}
}

func TestResolvePresets_ExtraEndpoints(t *testing.T) {
	cfg := config.NetworkPolicyConfig{
		Presets: []string{"tools"},
		ExtraEndpoints: []config.NetworkEndpointConfig{
			{Host: "internal.mycompany.com", Port: 443},
			{Host: "*.mycompany.com", Port: 8443},
		},
	}
	endpoints := ResolvePresets(cfg)

	// Should have tools hosts + 2 extra.
	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Host] = true
		if ep.Host == "*.mycompany.com" && ep.Port != 8443 {
			t.Errorf("expected port 8443 for *.mycompany.com, got %d", ep.Port)
		}
	}
	if !found["api.tavily.com"] {
		t.Error("expected api.tavily.com from tools preset")
	}
	if !found["internal.mycompany.com"] {
		t.Error("expected internal.mycompany.com from extra endpoints")
	}
	if !found["*.mycompany.com"] {
		t.Error("expected *.mycompany.com from extra endpoints")
	}
}

func TestResolvePresets_UnknownPresetIgnored(t *testing.T) {
	cfg := config.NetworkPolicyConfig{
		Presets: []string{"code_hosting", "nonexistent_preset"},
	}
	endpoints := ResolvePresets(cfg)

	// Should only have code_hosting hosts.
	hostSet := make(map[string]bool)
	for _, ep := range endpoints {
		hostSet[ep.Host] = true
	}
	if !hostSet["github.com"] {
		t.Error("expected github.com from code_hosting")
	}
	// Ensure no panic and results are still valid.
	if len(endpoints) == 0 {
		t.Error("expected non-empty endpoints")
	}
}

func TestResolvePresets_PortDefaults(t *testing.T) {
	cfg := config.NetworkPolicyConfig{
		Presets: []string{"code_hosting"},
	}
	endpoints := ResolvePresets(cfg)

	// SSH endpoints should have port 22.
	for _, ep := range endpoints {
		if ep.Host == "ssh.github.com" && ep.Port != 22 {
			t.Errorf("ssh.github.com port = %d, want 22", ep.Port)
		}
		if ep.Host == "github.com" && ep.Port != 443 {
			t.Errorf("github.com port = %d, want 443", ep.Port)
		}
	}
}
