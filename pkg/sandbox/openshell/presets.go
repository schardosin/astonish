package openshell

import "github.com/SAP/astonish/pkg/config"

// presetEndpoint defines a single allowed host:port pair within a preset.
type presetEndpoint struct {
	Host string
	Port uint32 // 0 = defaults to 443 at proto mapping time
}

// networkPresets maps preset names to their endpoint lists.
// The "default" meta-preset enables all individual presets.
var networkPresets = map[string][]presetEndpoint{
	"code_hosting": {
		{Host: "github.com", Port: 443},
		{Host: "*.github.com", Port: 443},
		{Host: "*.githubusercontent.com", Port: 443},
		{Host: "gitlab.com", Port: 443},
		{Host: "*.gitlab.com", Port: 443},
		{Host: "bitbucket.org", Port: 443},
		{Host: "*.bitbucket.org", Port: 443},
		{Host: "ssh.github.com", Port: 22},
		{Host: "ssh.gitlab.com", Port: 22},
	},
	"package_registries": {
		{Host: "registry.npmjs.org", Port: 443},
		{Host: "registry.yarnpkg.com", Port: 443},
		{Host: "pypi.org", Port: 443},
		{Host: "files.pythonhosted.org", Port: 443},
		{Host: "crates.io", Port: 443},
		{Host: "*.crates.io", Port: 443},
		{Host: "pkg.go.dev", Port: 443},
		{Host: "proxy.golang.org", Port: 443},
		{Host: "sum.golang.org", Port: 443},
		{Host: "dl.google.com", Port: 443},
		{Host: "rubygems.org", Port: 443},
		{Host: "*.rubygems.org", Port: 443},
		{Host: "maven.org", Port: 443},
		{Host: "*.maven.org", Port: 443},
	},
	"llm_apis": {
		{Host: "api.openai.com", Port: 443},
		{Host: "api.anthropic.com", Port: 443},
		{Host: "*.anthropic.com", Port: 443},
		{Host: "**.googleapis.com", Port: 443},
		{Host: "**.openai.azure.com", Port: 443},
		{Host: "generativelanguage.googleapis.com", Port: 443},
		{Host: "api.groq.com", Port: 443},
		{Host: "api.together.xyz", Port: 443},
		{Host: "api.fireworks.ai", Port: 443},
		{Host: "api.mistral.ai", Port: 443},
		{Host: "api.deepseek.com", Port: 443},
		{Host: "api.cohere.ai", Port: 443},
	},
	"tools": {
		{Host: "opencode.ai", Port: 443},
		{Host: "*.opencode.ai", Port: 443},
		{Host: "api.brave.com", Port: 443},
		{Host: "google.serper.dev", Port: 443},
		{Host: "api.tavily.com", Port: 443},
	},
	"system": {
		{Host: "**.archive.ubuntu.com", Port: 443},
		{Host: "**.archive.ubuntu.com", Port: 80},
		{Host: "security.ubuntu.com", Port: 443},
		{Host: "security.ubuntu.com", Port: 80},
		{Host: "*.docker.io", Port: 443},
		{Host: "*.docker.com", Port: 443},
		{Host: "ghcr.io", Port: 443},
		{Host: "*.ghcr.io", Port: 443},
		{Host: "production.cloudflare.docker.com", Port: 443},
	},
	"search": {
		{Host: "www.google.com", Port: 443},
		{Host: "*.google.com", Port: 443},
		{Host: "duckduckgo.com", Port: 443},
		{Host: "*.wikipedia.org", Port: 443},
		{Host: "*.stackoverflow.com", Port: 443},
		{Host: "stackoverflow.com", Port: 443},
		{Host: "*.sap.com", Port: 443},
		{Host: "sap.com", Port: 443},
	},
	"cdn": {
		{Host: "*.cloudflare.com", Port: 443},
		{Host: "*.cloudfront.net", Port: 443},
		{Host: "*.akamaized.net", Port: 443},
		{Host: "*.fastly.net", Port: 443},
	},
}

// allPresetNames lists all individual preset names (excludes "default").
var allPresetNames = []string{
	"code_hosting",
	"package_registries",
	"llm_apis",
	"tools",
	"system",
	"search",
	"cdn",
}

// ResolvePresets expands a NetworkPolicyConfig into a flat list of EndpointSpec
// entries ready for proto mapping. When Presets is empty or contains "default",
// all presets are enabled.
func ResolvePresets(cfg config.NetworkPolicyConfig) []EndpointSpec {
	activePresets := resolveActivePresets(cfg.Presets)

	// Estimate capacity.
	n := 0
	for _, name := range activePresets {
		n += len(networkPresets[name])
	}
	n += len(cfg.ExtraEndpoints)

	endpoints := make([]EndpointSpec, 0, n)

	// Collect from active presets.
	for _, name := range activePresets {
		for _, ep := range networkPresets[name] {
			endpoints = append(endpoints, EndpointSpec{
				Host: ep.Host,
				Port: ep.Port,
			})
		}
	}

	// Append extra endpoints.
	for _, ep := range cfg.ExtraEndpoints {
		endpoints = append(endpoints, EndpointSpec{
			Host: ep.Host,
			Port: ep.Port,
		})
	}

	return endpoints
}

// resolveActivePresets returns the list of preset names to enable.
// "default" or empty means all presets.
func resolveActivePresets(presets []string) []string {
	if len(presets) == 0 {
		return allPresetNames
	}
	for _, p := range presets {
		if p == "default" {
			return allPresetNames
		}
	}
	// Return only presets that exist in the registry.
	active := make([]string, 0, len(presets))
	for _, p := range presets {
		if _, ok := networkPresets[p]; ok {
			active = append(active, p)
		}
	}
	return active
}
