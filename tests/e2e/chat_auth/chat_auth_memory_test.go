//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers memory authorization across users, teams, and orgs.
package chat_auth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_Memory_PersonalIsolation verifies that personal memories are
// visible only to their owner and invisible to other users, even teammates.
//
// COVERS: CHAT-017
func TestE2E_Memory_PersonalIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice can see her own personal memory
	t.Run("alice_sees_own_personal_memory", func(t *testing.T) {
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

		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				found = true
				break
			}
		}
		if !found {
			t.Error("alice cannot see her own personal memory")
		}
	})

	// Bob searches for Alice's personal memory — should get nothing
	t.Run("bob_cannot_search_alice_personal_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAlicePersonal,
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
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				t.Error("bob (same team) should NOT see alice's personal memory via search")
			}
		}
	})

	// Eve (different org) searches for Alice's personal memory — should get nothing
	t.Run("eve_cannot_search_alice_personal_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAlicePersonal,
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
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				t.Error("eve (different org) should NOT see alice's personal memory")
			}
		}
	})
}

// TestE2E_Memory_TeamVisibility verifies that team memories are searchable
// by all members of that team.
//
// COVERS: CHAT-018
func TestE2E_Memory_TeamVisibility(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team admin) sees red team memory
	t.Run("alice_sees_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
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
			t.Error("alice (red team) cannot see red team memory")
		}
	})

	// Bob (red team member) also sees red team memory
	t.Run("bob_sees_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
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
			t.Error("bob (red team member) cannot see red team memory")
		}
	})
}

// TestE2E_Memory_TeamIsolation verifies that team memories are invisible
// to members of other teams in the same org.
//
// COVERS: CHAT-019
func TestE2E_Memory_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) cannot see blue team memory
	t.Run("alice_cannot_see_blue_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeBlueTeam) {
				t.Error("alice (red team) should NOT see blue team memory")
			}
		}
	})

	// Carol (blue team) cannot see red team memory
	t.Run("carol_cannot_see_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeRedTeam) {
				t.Error("carol (blue team) should NOT see red team memory")
			}
		}
	})
}

// TestE2E_Memory_OrgIsolation verifies that org-level memories are visible
// across teams within the org but invisible to other orgs.
//
// COVERS: CHAT-020
func TestE2E_Memory_OrgIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (acme/red) can search and find acme org memory
	t.Run("alice_can_find_acme_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
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

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("alice (acme) cannot find acme org memory via search")
		}
	})

	// Carol (acme/blue) can also find acme org memory (cross-team within org)
	t.Run("carol_can_find_acme_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
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

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("carol (acme/blue) cannot find acme org memory — cross-team org visibility broken")
		}
	})

	// Eve (globex) cannot find acme org memory
	t.Run("eve_cannot_find_acme_org_memory", func(t *testing.T) {
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
				t.Error("eve (globex) should NOT see acme org memory — cross-org isolation broken")
			}
		}
	})

	// Eve (globex) CAN find globex org memory
	t.Run("eve_can_find_globex_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemGlobexOrg,
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

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemGlobexOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("eve (globex) cannot find her own org memory")
		}
	})
}
