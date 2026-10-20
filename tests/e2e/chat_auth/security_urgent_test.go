//go:build e2e

package chat_auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_SecurityUrgentConfigAuthority exercises real auth, tenancy and persistence.
// No mocks; unavailable bootstrap prerequisites are rejected by the strict runner.
func TestE2E_SecurityUrgentConfigAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	// Org owner has legitimate access to org provider management, but not deployment writes.
	resp := alice.Get(t, "/api/settings/org/providers")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner org read: %d", resp.StatusCode)
	}
	before := alice.Get(t, "/api/settings/full")
	if before.StatusCode != http.StatusOK {
		before.Body.Close()
		t.Fatalf("full read: %d", before.StatusCode)
	}
	beforeBody := e2eboot.ReadBody(t, before)
	before.Body.Close()
	for _, email := range []string{e2eboot.UserAliceEmail, e2eboot.UserBobEmail, e2eboot.UserEveEmail} {
		c := seed.Client(email)
		denied := c.Put(t, "/api/settings/full", map[string]any{"agent_identity": map[string]string{"name": "unauthorized-canary"}})
		denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("%s deployment write: %d", email, denied.StatusCode)
		}
		denied = c.Get(t, "/api/settings/platform/providers")
		denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("%s platform read: %d", email, denied.StatusCode)
		}
	}
	after := alice.Get(t, "/api/settings/full")
	defer after.Body.Close()
	if after.StatusCode != http.StatusOK || e2eboot.ReadBody(t, after) != beforeBody {
		t.Fatal("denied writes changed deployment config")
	}
}

// urgentStatus deliberately rejects infrastructure errors as authorization proof.
func urgentStatus(t *testing.T, resp *http.Response, allowed ...int) string {
	t.Helper()
	defer resp.Body.Close()
	body := e2eboot.ReadBody(t, resp)
	for _, status := range allowed {
		if resp.StatusCode == status {
			return body
		}
	}
	// Avoid printing potentially sensitive response bodies on a regression.
	t.Fatalf("%s %s: status=%d, want=%v", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, allowed)
	return ""
}

func TestE2E_SecurityUrgentSessionOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	const marker = "urgent-session-owner-canary"
	events := alice.SSE(t, "/api/studio/chat", map[string]any{"message": "Reply with exactly " + marker}, 120*time.Second)
	id := e2eboot.ExtractSessionIDFromSSE(t, events)
	if id == "" {
		t.Fatal("owner session missing")
	}
	path := "/api/studio/sessions/" + id
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) {
		t.Fatal("owner session content missing")
	}
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserCarolEmail, e2eboot.UserEveEmail} {
		t.Run(email, func(t *testing.T) {
			other := seed.Client(email)
			urgentStatus(t, other.Get(t, path), http.StatusForbidden, http.StatusNotFound)
			list := urgentStatus(t, other.Get(t, "/api/studio/sessions"), http.StatusOK)
			if strings.Contains(list, id) {
				t.Fatal("foreign session leaked in list")
			}
			// Deletion is idempotent and deliberately hides inaccessible sessions.
			urgentStatus(t, other.Delete(t, path), http.StatusForbidden, http.StatusNotFound, http.StatusNoContent)
			urgentStatus(t, other.Post(t, "/api/studio/chat", map[string]any{"sessionId": id, "message": "unauthorized-injection"}), http.StatusForbidden, http.StatusNotFound)
			after := urgentStatus(t, alice.Get(t, path), http.StatusOK)
			if after != before {
				t.Fatal("denied request mutated owner's session")
			}
		})
	}
	urgentStatus(t, alice.Delete(t, path), http.StatusOK, http.StatusNoContent)
	urgentStatus(t, alice.Get(t, path), http.StatusNotFound)
}

func TestE2E_SecurityUrgentArtifactOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice := seed.Client(e2eboot.UserAliceEmail)
	const marker = "urgent-artifact-owner-canary"
	events := alice.SSE(t, "/api/studio/chat", map[string]any{
		"message": `Use write_file to write exactly "` + marker + `" to /tmp/urgent-artifact.md. Do not do anything else.`, "autoApprove": true,
	}, 180*time.Second)
	id := e2eboot.ExtractSessionIDFromSSE(t, events)
	artifacts := e2eboot.FindAllEvents(events, "artifact")
	if id == "" || len(artifacts) == 0 {
		t.Fatal("required real sandbox artifact was not produced; acceptance cannot pass")
	}
	var artifact struct {
		Path string `json:"path"`
	}
	e2eboot.DecodeEventData(t, &artifacts[0], &artifact)
	if artifact.Path == "" {
		t.Fatal("empty artifact path")
	}
	path := "/api/studio/artifacts/content?" + url.Values{"path": {artifact.Path}, "session": {id}}.Encode()
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) {
		t.Fatal("owner artifact download did not contain fixture")
	}
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserCarolEmail, e2eboot.UserEveEmail} {
		urgentStatus(t, seed.Client(email).Get(t, path), http.StatusForbidden, http.StatusNotFound)
	}
	urgentStatus(t, alice.Get(t, "/api/studio/artifacts/content?"+url.Values{"path": {artifact.Path}}.Encode()), http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound)
	if urgentStatus(t, alice.Get(t, path), http.StatusOK) != before {
		t.Fatal("artifact changed after denials")
	}
	urgentStatus(t, alice.Delete(t, "/api/studio/sessions/"+id), http.StatusOK, http.StatusNoContent)
}

func TestE2E_SecurityUrgentMCPAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	path := "/api/mcp-platform/servers/" + e2eboot.MCPAcmeRedTeam + "?scope=team"
	before := urgentStatus(t, alice.Get(t, path), http.StatusOK)
	urgentStatus(t, bob.Delete(t, path), http.StatusForbidden)
	if urgentStatus(t, alice.Get(t, path), http.StatusOK) != before {
		t.Fatal("member delete mutated team MCP server")
	}
	// Org owner must not mutate deployment-wide MCP through modern or legacy routes.
	for _, client := range []*e2eboot.Client{alice, bob} {
		urgentStatus(t, client.Post(t, "/api/mcp-platform/servers?scope=platform", map[string]any{}), http.StatusForbidden)
		urgentStatus(t, client.Put(t, "/api/settings/mcp?scope=platform", map[string]any{}), http.StatusForbidden)
		urgentStatus(t, client.Post(t, "/api/mcp/install-inline?scope=platform", map[string]any{}), http.StatusForbidden)
	}
	urgentStatus(t, alice.Delete(t, "/api/mcp-platform/servers/"+e2eboot.MCPAcmeRedTeam+"?scope=effective"), http.StatusBadRequest)
	urgentStatus(t, alice.Delete(t, path), http.StatusOK)
	urgentStatus(t, alice.Get(t, path), http.StatusNotFound)
	// The same operation is legal for the owner in org scope, and leaves another org untouched.
	urgentStatus(t, alice.Delete(t, "/api/mcp-platform/servers/"+e2eboot.MCPAcmeOrg+"?scope=org"), http.StatusOK)
	urgentStatus(t, seed.Client(e2eboot.UserDaveEmail).Get(t, "/api/mcp-platform/servers/"+e2eboot.MCPGlobexOrg+"?scope=org"), http.StatusOK)
}

func TestE2E_SecurityUrgentMemoryAuthority(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	before := urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK)
	var listed struct {
		Results []struct {
			ID      string `json:"id"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(before), &listed); err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, entry := range listed.Results {
		if strings.Contains(entry.Snippet, e2eboot.MemAcmeRedTeam) {
			id = entry.ID
		}
	}
	if id == "" {
		t.Fatal("owner's seeded team memory missing")
	}
	request := api.MemoryConsolidationApplyRequest{
		Card: memory.ScenarioCard{CanonicalKey: "urgent-console-access", Title: "Urgent Console Access", Scope: "team", SourceMemoryIDs: []string{id}, RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}, TargetScope: "team",
	}
	urgentStatus(t, bob.Post(t, "/api/memories/consolidate/apply", request), http.StatusForbidden)
	if urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK) != before {
		t.Fatal("denied consolidation changed source or destination")
	}
	urgentStatus(t, alice.Post(t, "/api/memories/consolidate/apply", request), http.StatusOK)
	after := urgentStatus(t, alice.Get(t, "/api/memories/team"), http.StatusOK)
	if strings.Contains(after, e2eboot.MemAcmeRedTeam) || !strings.Contains(after, "Urgent Console Access") {
		t.Fatal("authorized consolidation failed to replace source with card")
	}
	// Positive personal reads and same-team/cross-org search denials use existing fixtures.
	for _, email := range []string{e2eboot.UserBobEmail, e2eboot.UserEveEmail} {
		body := urgentStatus(t, seed.Client(email).Post(t, "/api/memories/search", map[string]string{"query": e2eboot.MemAlicePersonal}), http.StatusOK)
		if strings.Contains(body, e2eboot.MemAlicePersonal) {
			t.Fatal("foreign personal memory leaked")
		}
	}
	if !strings.Contains(urgentStatus(t, alice.Get(t, "/api/memories/personal"), http.StatusOK), e2eboot.MemAlicePersonal) {
		t.Fatal("owner personal read failed")
	}
}

func TestE2E_SecurityUrgentChannelOwnership(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	alice, bob := seed.Client(e2eboot.UserAliceEmail), seed.Client(e2eboot.UserBobEmail)
	ctx := context.Background()
	// Seed a pending email delivery in the real platform store: SMTP delivery is
	// outside this test; verification and persistence use the real HTTP server.
	const code = "UrgentEmailTokenCaseSensitive123"
	const email = "urgent-verified@acme.test"
	if err := h.Store.LinkCodes().Generate(ctx, code, seed.Users[e2eboot.UserAliceEmail].ID, email, "email:"+email); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/user/channels/verify-email-code"
	urgentStatus(t, bob.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": strings.ToLower(code)}), http.StatusBadRequest)
	pending, err := h.Store.LinkCodes().Peek(ctx, code)
	if err != nil || pending == nil {
		t.Fatal("rejected verification consumed owner's token")
	}
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusOK)
	linked, err := h.PlatformBackend().UserChannels().GetByExternalID(ctx, "email", email)
	if err != nil || linked == nil || !linked.Verified || linked.UserID != seed.Users[e2eboot.UserAliceEmail].ID {
		t.Fatal("valid owner verification was not persisted")
	}
	urgentStatus(t, alice.Post(t, endpoint, map[string]string{"code": code}), http.StatusBadRequest)
}
