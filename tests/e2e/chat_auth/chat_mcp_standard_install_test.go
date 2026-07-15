//go:build e2e

// Standard MCP server install in platform mode — encryption envelope contract
// AND platform-tier cascade contract.
//
// Two related scenarios live in this file:
//
//   - CHAT-069 pins the JSONB UTF-8 regression: when ASTONISH_MASTER_KEY is set,
//     pgMCPServerStore.Save must wrap AES-GCM ciphertext in a base64 envelope
//     ({"_encrypted":"..."}) before writing to the JSONB env column. Without
//     the envelope, raw ciphertext bytes such as 0x8e violate JSONB's UTF-8
//     contract and Postgres rejects the INSERT with SQLSTATE 22021.
//
//   - CHAT-070 pins the platform-tier cascade contract: a server installed at
//     scope=platform must be visible to chat in EVERY org/team without a
//     per-org/team install. Older code only looked at org+team MCP stores at
//     chat-build time, silently hiding platform-tier servers from the agent's
//     tool list and system prompt. Pins the fix that adds Platform to
//     store.MCPServerStores and makes loadMCPConfig walk all three tiers.
//
// The master key is injected by e2eboot.BootstrapPlatformCore() so that both
// isolated and shared-inspector modes exercise the same encryption path.

package chat_auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_StandardMCPInstall_PlatformEncryptionEnvelope installs a standard
// MCP server with a non-empty env in platform scope and asserts the env
// round-trips through the JSONB-encrypted-at-rest envelope.
//
// COVERS: CHAT-069
func TestE2E_StandardMCPInstall_PlatformEncryptionEnvelope(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Use a fake but realistic API key value containing a byte that would
	// trigger SQLSTATE 22021 in the buggy path. AES-GCM ciphertext for any
	// payload will produce non-UTF-8 bytes; the value itself only needs to be
	// non-trivial enough that envJSON > 2 bytes (the gate in Save()).
	const apiKey = "tvly-test-CHAT069-do-not-use-in-prod-AAAAAAAAA"

	installBody := map[string]any{
		"env": map[string]string{
			"TAVILY_API_KEY": apiKey,
		},
	}

	// Install with platform scope: this is the only path where pgMCPServerStore
	// is constructed with a non-nil secrets store and so encrypts env at rest.
	resp := h.Post(t, "/api/standard-servers/tavily/install?scope=platform", installBody)
	body := e2eboot.ReadBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		// Surface the exact error so a regression to the JSONB-rejects-ciphertext
		// path is diagnosable from the failure log alone.
		if strings.Contains(body, "invalid byte sequence for encoding") || strings.Contains(body, "22021") {
			t.Fatalf("CHAT-069 regression: encryption envelope missing — JSONB rejected ciphertext: %s", body)
		}
		t.Fatalf("install returned %d: %s", resp.StatusCode, body)
	}

	// Round-trip read: the server must come back with the env decrypted to its
	// original plaintext value. This validates both Save's envelope-write and
	// decryptEnv's envelope-read.
	listResp := h.Get(t, "/api/mcp-platform/servers?scope=platform")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list servers returned %d: %s", listResp.StatusCode, e2eboot.ReadBody(t, listResp))
	}

	// Response shape varies; decode loosely and look for the server by name.
	var raw any
	if err := json.NewDecoder(listResp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode servers list: %v", err)
	}

	got := findEnvForServer(raw, "tavily", "TAVILY_API_KEY")
	if got == "" {
		// Provide the raw response for diagnosability — schema may have shifted.
		dump, _ := json.Marshal(raw)
		t.Fatalf("could not locate tavily TAVILY_API_KEY in platform servers list: %s", string(dump))
	}
	if got != apiKey {
		t.Fatalf("env round-trip mismatch: want %q, got %q", apiKey, got)
	}
}

// TestE2E_StandardMCPInstall_PlatformCascadeVisibleToOrgChat installs Tavily
// at scope=platform, then verifies the platform-tier cascade contract: a
// server with cached_tools at the platform tier MUST be visible to org/team-
// scoped GET /api/tools without any per-org/team install.
//
// This test isolates the cascade logic from sandbox-based discovery. After
// install, it writes synthetic cached_tools directly to the platform MCP store
// (simulating what asyncDiscoverAndCacheTools would produce), then asserts
// visibility through the same HTTP path the chat agent uses.
//
// Real end-to-end discovery (sandbox container creation → MCP server startup →
// tool listing → DB caching) is covered by TestE2E_Apps_MCPDataSource which
// exercises the full path with a real Tavily API key and ensureBaseConfigured.
//
// COVERS: CHAT-070
func TestE2E_StandardMCPInstall_PlatformCascadeVisibleToOrgChat(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const apiKey = "tvly-test-CHAT070-do-not-use-in-prod-BBBBBBBBB"

	installBody := map[string]any{
		"env": map[string]string{
			"TAVILY_API_KEY": apiKey,
		},
	}

	resp := h.Post(t, "/api/standard-servers/tavily/install?scope=platform", installBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install returned %d: %s", resp.StatusCode, body)
	}

	// Simulate what async discovery would produce: write cached_tools directly
	// to the platform MCP store. This isolates the cascade contract test from
	// sandbox availability, Node.js presence, and network access to npm.
	syntheticTools := json.RawMessage(`[{"name":"tavily_search","description":"Search the web using Tavily"}]`)
	platformMCP := h.PlatformBackend().PlatformMCPServers()
	if err := platformMCP.UpdateCachedTools(context.Background(), "tavily", syntheticTools); err != nil {
		t.Fatalf("failed to write synthetic cached_tools: %v", err)
	}

	// The cascade contract: platform-tier cached_tools MUST be visible to an
	// org/team-scoped /api/tools request (the same path chat uses).
	if !hasToolInList(t, h, "tavily_search") {
		dump := dumpToolList(t, h)
		t.Fatalf("CHAT-070 regression: platform-tier tavily_search not visible to org-scoped /api/tools call. "+
			"This means platform MCP servers are not cascading into the chat tool list. Tools seen: %s", dump)
	}
}

func hasToolInList(t *testing.T, h *e2eboot.Harness, toolName string) bool {
	t.Helper()
	resp := h.Get(t, "/api/tools")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	for _, tl := range payload.Tools {
		if tl.Name == toolName {
			return true
		}
	}
	return false
}

func dumpToolList(t *testing.T, h *e2eboot.Harness) string {
	t.Helper()
	resp := h.Get(t, "/api/tools")
	defer resp.Body.Close()
	b, _ := json.Marshal(struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body,omitempty"`
	}{
		Status: resp.StatusCode,
	})
	return string(b)
}

// findEnvForServer walks the loosely-typed servers-list payload and returns
// the value of envKey for the server whose name matches serverName. Returns
// "" if not found. Tolerates both array-of-server and {servers:[...]} shapes
// so the test does not break on minor API envelope changes.
func findEnvForServer(payload any, serverName, envKey string) string {
	visit := func(servers []any) string {
		for _, s := range servers {
			m, ok := s.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name != serverName {
				continue
			}
			env, ok := m["env"].(map[string]any)
			if !ok {
				continue
			}
			v, _ := env[envKey].(string)
			return v
		}
		return ""
	}

	switch v := payload.(type) {
	case []any:
		return visit(v)
	case map[string]any:
		if arr, ok := v["servers"].([]any); ok {
			if found := visit(arr); found != "" {
				return found
			}
		}
		// Some endpoints return a map keyed by server name.
		if m, ok := v[serverName].(map[string]any); ok {
			if env, ok := m["env"].(map[string]any); ok {
				if val, ok := env[envKey].(string); ok {
					return val
				}
			}
		}
	}
	return ""
}
