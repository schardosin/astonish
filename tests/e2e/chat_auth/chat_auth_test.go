//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries:
// memory isolation, skill scoping, MCP server access control, and credential
// privacy across users, teams, and organizations.
package chat_auth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// TestE2E_Seed_LayoutIsCorrect verifies that the multi-tenant seed
// materializes all expected data and that basic scoping works.
// This is the foundational test — if it fails, all other auth boundary
// tests should be considered untrustworthy.
//
// COVERS: INFRA-001
func TestE2E_Seed_LayoutIsCorrect(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	t.Run("all_users_created", func(t *testing.T) {
		emails := []string{
			e2eboot.UserAliceEmail,
			e2eboot.UserBobEmail,
			e2eboot.UserCarolEmail,
			e2eboot.UserDaveEmail,
			e2eboot.UserEveEmail,
		}
		for _, email := range emails {
			if _, ok := seed.Users[email]; !ok {
				t.Errorf("expected user %s in seed result", email)
			}
		}
	})

	t.Run("alice_can_list_personal_memories", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/personal")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
			Count   int              `json:"count"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		if response.Count == 0 {
			t.Fatal("alice should have at least 1 personal memory")
		}

		// Verify it's alice's memory by checking snippet
		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("alice's personal memory not found in response")
		}
	})

	t.Run("alice_can_see_red_team_memories", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
			Count   int              `json:"count"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeRedTeam) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("red team memory not found for alice")
		}
	})

	t.Run("alice_cannot_see_blue_team_memories", func(t *testing.T) {
		// Alice is in team red, not blue — when querying as team red
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
			Count   int              `json:"count"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeBlueTeam) {
				t.Errorf("alice (team red) should NOT see blue team memory")
			}
		}
	})

	t.Run("eve_cannot_see_acme_org_memories", func(t *testing.T) {
		// Eve is in globex — cross-org isolation
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAcmeOrg,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				t.Errorf("eve (globex) should NOT see acme org memory via search")
			}
		}
	})

	t.Run("alice_can_see_red_team_skills", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.SkillAcmeRedTeam) {
			t.Errorf("red team skill not found for alice")
		}
	})

	t.Run("alice_cannot_see_blue_team_skills", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/skills?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.SkillAcmeBlueTeam) {
			t.Errorf("alice (team red) should NOT see blue team skill")
		}
	})

	t.Run("alice_can_see_red_team_mcp_servers", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.MCPAcmeRedTeam) {
			t.Errorf("red team MCP server not found for alice")
		}
	})

	t.Run("eve_cannot_see_acme_mcp_servers", func(t *testing.T) {
		// Eve is in globex — cross-org
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Get(t, "/api/mcp-platform/servers?scope=team")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, e2eboot.MCPAcmeRedTeam) || strings.Contains(body, e2eboot.MCPAcmeOrg) {
			t.Errorf("eve (globex) should NOT see acme MCP servers")
		}
	})

	t.Run("alice_can_list_personal_credentials", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/credentials?scope=personal")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body := e2eboot.ReadBody(t, resp)
		if !strings.Contains(body, e2eboot.CredAlicePersonal) {
			t.Errorf("alice personal credential not found")
		}
		// Should NOT contain bob's credential
		if strings.Contains(body, e2eboot.CredBobPersonal) {
			t.Errorf("alice should NOT see bob's personal credential")
		}
	})

	t.Run("unauthenticated_request_rejected", func(t *testing.T) {
		// Make a request with no auth header
		req, _ := http.NewRequest("GET", h.BaseURL+"/api/memories/personal", nil)
		req.Header.Set("Content-Type", "application/json")
		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}
