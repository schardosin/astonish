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
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
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
// at scope=platform, then issues a tools-listing request through the same
// HTTP path the chat agent uses to assemble its tool set. The platform-tier
// install MUST be visible without any per-org/team install: that is the
// documented inheritance contract for Services.PlatformMCPServers and is
// what users expect when they install a standard server "for everyone."
//
// We intentionally test through GET /api/tools rather than fabricating a chat
// turn so the assertion is deterministic and provider-key-free: GET /api/tools
// shares the same DB-walking logic (tools_cache.go GetCachedToolsForRequest)
// with the chat path's tool list assembly (chat_factory.go loadMCPConfig +
// getPlatformCachedTools), and both were patched together. If /api/tools
// can see tavily_search after a platform install, the chat path will too.
//
// To make this test independent of any pre-existing tools cache, we wait for
// the discovery goroutine spawned by installStandardServerPlatform to finish
// (it writes cached_tools back to the platform store). The wait is bounded.
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

	// installStandardServerPlatform discovers tools in a background goroutine
	// (pkg/api/standard_servers_handler.go ~287). Poll briefly for tavily_search
	// to appear in /api/tools. This polls the SAME endpoint the UI uses, which
	// shares its cascade logic with the chat agent.
	if !waitForToolInList(t, h, "tavily_search", 10) {
		// Diagnostic: dump current tool list so a cascade regression is visible.
		dump := dumpToolList(t, h)
		t.Fatalf("CHAT-070 regression: platform-tier tavily_search not visible to org-scoped /api/tools call within timeout. "+
			"This means platform MCP servers are not cascading into the chat tool list. Tools seen: %s", dump)
	}
}

// waitForToolInList polls GET /api/tools up to maxAttempts times (200ms apart)
// looking for a tool with the given name. Returns true on first sighting.
func waitForToolInList(t *testing.T, h *e2eboot.Harness, toolName string, maxAttempts int) bool {
	t.Helper()
	for i := 0; i < maxAttempts; i++ {
		if hasToolInList(t, h, toolName) {
			return true
		}
		// Short sleep between polls; intentionally short so total budget is small.
		sleepMillis(200)
	}
	return false
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

func sleepMillis(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
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
