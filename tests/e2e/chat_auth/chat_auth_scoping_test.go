//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers skills, MCP servers, and credentials scoping.
package chat_auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// TestE2E_Skill_TeamIsolation verifies that team skills are visible to team
// members but not to members of other teams.
//
// COVERS: CHAT-022
func TestE2E_Skill_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) sees red team skill
	t.Run("alice_sees_red_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.SkillAcmeRedTeam) {
			t.Error("alice (red team) cannot see red team skill")
		}
	})

	// Carol (blue team) does NOT see red team skill
	t.Run("carol_does_not_see_red_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.SkillAcmeRedTeam) {
			t.Error("carol (blue team) should NOT see red team skill")
		}
	})

	// Carol (blue team) sees blue team skill
	t.Run("carol_sees_blue_team_skill", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.SkillAcmeBlueTeam) {
			t.Error("carol (blue team) cannot see blue team skill")
		}
	})

	// Eve (globex) does NOT see acme red team skill
	t.Run("eve_does_not_see_acme_skills", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.SkillAcmeRedTeam) || strings.Contains(body, e2eboot.SkillAcmeBlueTeam) {
			t.Error("eve (globex) should NOT see any acme team skills")
		}
	})
}

// TestE2E_MCP_TeamIsolation verifies that team MCP servers are visible to
// team members but not to members of other teams or other orgs.
//
// COVERS: CHAT-023
func TestE2E_MCP_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) sees red team MCP server
	t.Run("alice_sees_red_team_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.MCPAcmeRedTeam) {
			t.Error("alice (red team) cannot see red team MCP server")
		}
	})

	// Eve (globex) does NOT see acme MCP servers
	t.Run("eve_does_not_see_acme_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.MCPAcmeRedTeam) || strings.Contains(body, e2eboot.MCPAcmeBlueTeam) || strings.Contains(body, e2eboot.MCPAcmeOrg) {
			t.Error("eve (globex) should NOT see any acme MCP servers")
		}
	})

	// Eve sees globex MCP server
	t.Run("eve_sees_globex_mcp", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.MCPGlobexTeam) {
			t.Error("eve (globex) cannot see her own team MCP server")
		}
	})
}

// TestE2E_Credential_PersonalIsolation verifies that personal credentials
// are visible only to their owner.
//
// COVERS: CHAT-024
func TestE2E_Credential_PersonalIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice sees her personal credential
	t.Run("alice_sees_own_credential", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.CredAlicePersonal) {
			t.Error("alice cannot see her own personal credential")
		}
	})

	// Alice does NOT see bob's credential
	t.Run("alice_does_not_see_bob_credential", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("alice should NOT see bob's personal credential")
		}
	})

	// Bob sees his own but not alice's
	t.Run("bob_sees_own_not_alice", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("bob cannot see his own personal credential")
		}
		if strings.Contains(body, e2eboot.CredAlicePersonal) {
			t.Error("bob should NOT see alice's personal credential")
		}
	})

	// Eve (different org) does NOT see acme credentials
	t.Run("eve_does_not_see_acme_credentials", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.CredAlicePersonal) || strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Error("eve (globex) should NOT see any acme personal credentials")
		}
	})
}

// TestE2E_Session_CrossOrgDenied verifies that a user in one org cannot
// access another org's session by ID.
//
// COVERS: CHAT-026
func TestE2E_Session_CrossOrgDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice creates a session
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": "Hello from Alice for session isolation test.",
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	// Self-clean Alice's session so this test is safe under shared-seed
	// (Plan D): tests in chat_auth share Alice's session list across runs.
	// Skipped under shared-inspect mode (ASTONISH_E2E_KEEP_ALIVE=1) so the
	// session remains browsable in the inspector UI.
	t.Cleanup(func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		if resp != nil {
			resp.Body.Close()
		}
	})

	// Eve (different org) tries to fetch Alice's session
	eveClient := seed.Client(e2eboot.UserEveEmail)
	resp := eveClient.Get(t, "/api/studio/sessions/"+sessionID)
	defer resp.Body.Close()

	// Should be 404 or empty (not 200 with Alice's data)
	if resp.StatusCode == http.StatusOK {
		var detail struct {
			ID       string `json:"id"`
			Messages []any  `json:"messages"`
		}
		e2eboot.DecodeJSON(t, resp, &detail)
		if detail.ID == sessionID && len(detail.Messages) > 0 {
			t.Error("eve (globex) was able to read alice's (acme) session — cross-org isolation broken")
		}
	}
	// 404, 403, or empty 200 are all acceptable
}
