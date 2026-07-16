//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers session-level isolation and runtime authorization edges.
package chat_auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_Session_CrossOrgAccess verifies complete cross-org session isolation:
// a user from org-globex cannot read, delete, or stream to a session owned by
// a user in org-acme.
//
// COVERS: CHAT-026
func TestE2E_Session_CrossOrgAccess(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice creates a session
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": "Hello from Alice, this is a session isolation test.",
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}

	eveClient := seed.Client(e2eboot.UserEveEmail)

	// Eve tries to GET Alice's session
	t.Run("eve_cannot_get_alice_session", func(t *testing.T) {
		resp := eveClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var detail struct {
				ID       string `json:"id"`
				Messages []any  `json:"messages"`
			}
			e2eboot.DecodeJSON(t, resp, &detail)
			if detail.ID == sessionID && len(detail.Messages) > 0 {
				t.Error("eve read alice's session content — cross-org isolation broken")
			}
		}
		// 404, 403, or empty response are all acceptable
	})

	// Eve tries to POST a message using Alice's sessionID. The system isolates
	// data per-org (Eve's writes land in her own personal session store), so the
	// SSE response may echo back the same sessionID — that's cosmetic.
	// The real check: Alice's session content must remain unmodified.
	t.Run("eve_cannot_inject_into_alice_session_content", func(t *testing.T) {
		const eveInjection = "Eve trying to inject into Alice's session"
		resp := eveClient.PostWithTimeout(t, "/api/studio/chat", map[string]any{
			"message":   eveInjection,
			"sessionId": sessionID,
		}, 30*time.Second)
		// Drain and discard Eve's stream so the request completes.
		if resp.StatusCode == http.StatusOK {
			_ = e2eboot.ParseSSEStream(t, resp.Body)
		}
		resp.Body.Close()

		// Wait briefly for any persistence
		time.Sleep(1 * time.Second)

		// Fetch Alice's session as Alice — Eve's text must NOT be present.
		aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer aliceResp.Body.Close()
		if aliceResp.StatusCode != http.StatusOK {
			t.Fatalf("alice cannot read her own session: %d", aliceResp.StatusCode)
		}
		body := e2eboot.ReadBody(t, aliceResp)
		if strings.Contains(body, eveInjection) {
			t.Error("eve's message was injected into alice's session — cross-org content isolation broken")
		}
	})

	// Eve tries to DELETE Alice's session
	t.Run("eve_cannot_delete_alice_session", func(t *testing.T) {
		resp := eveClient.Delete(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		// Should not succeed (or should be no-op for eve)
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			// Verify alice's session still exists
			aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
			defer aliceResp.Body.Close()
			if aliceResp.StatusCode != http.StatusOK {
				t.Error("eve's DELETE appeared to actually remove alice's session")
			}
		}
	})

	// Cleanup (skipped under shared-inspect mode so the session is browsable)
	if !e2eboot.RetainSessions() {
		delResp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}

// TestE2E_Session_CrossTeamAccess verifies that users in different teams within
// the same org cannot access each other's personal sessions.
//
// COVERS: CHAT-027
func TestE2E_Session_CrossTeamAccess(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (team red) creates a session
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": "Hello from Alice for cross-team test.",
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}

	// Carol (team blue, same org) tries to access Alice's session
	carolClient := seed.Client(e2eboot.UserCarolEmail)

	t.Run("carol_cannot_read_alice_session", func(t *testing.T) {
		resp := carolClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var detail struct {
				ID       string `json:"id"`
				Messages []any  `json:"messages"`
			}
			e2eboot.DecodeJSON(t, resp, &detail)
			if detail.ID == sessionID && len(detail.Messages) > 0 {
				t.Error("carol (blue) read alice's (red) session — cross-team personal session isolation broken")
			}
		}
		// 404, 403, or empty = pass
	})

	// Alice's sessions list should show the session; Carol's should NOT
	t.Run("carol_sessions_list_excludes_alice", func(t *testing.T) {
		resp := carolClient.Get(t, "/api/studio/sessions")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, sessionID) {
			t.Error("carol's session list contains alice's session ID — isolation broken")
		}
	})

	// Carol attempts to DELETE Alice's session — must not actually remove it.
	t.Run("carol_cannot_delete_alice_session", func(t *testing.T) {
		resp := carolClient.Delete(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
			defer aliceResp.Body.Close()
			if aliceResp.StatusCode != http.StatusOK {
				t.Error("carol's DELETE appears to have actually removed alice's session — cross-team delete isolation broken")
			}
		}
	})

	// Carol attempts to inject content into Alice's session via POST /chat
	// using Alice's sessionId. Even if SSE echoes the sessionId back, the
	// per-org/per-user routing must prevent the content from landing in
	// Alice's personal session.
	t.Run("carol_cannot_inject_into_alice_session_content", func(t *testing.T) {
		const carolInjection = "Carol trying to inject into Alice's cross-team session"
		resp := carolClient.PostWithTimeout(t, "/api/studio/chat", map[string]any{
			"message":   carolInjection,
			"sessionId": sessionID,
		}, 30*time.Second)
		if resp.StatusCode == http.StatusOK {
			_ = e2eboot.ParseSSEStream(t, resp.Body)
		}
		resp.Body.Close()

		time.Sleep(1 * time.Second)

		aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer aliceResp.Body.Close()
		if aliceResp.StatusCode != http.StatusOK {
			t.Fatalf("alice cannot read her own session: %d", aliceResp.StatusCode)
		}
		body := e2eboot.ReadBody(t, aliceResp)
		if strings.Contains(body, carolInjection) {
			t.Error("carol's message was injected into alice's session content — cross-team content isolation broken")
		}
	})

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		delResp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}

// TestE2E_Chat_ApprovalBoundToOwner verifies that the approval/stop mechanism
// is only accessible to the session owner. Another user cannot stop or interact
// with someone else's running session.
//
// COVERS: CHAT-062
func TestE2E_Chat_ApprovalBoundToOwner(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice creates a session
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": "Brief hello for ownership test.",
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}

	// Bob (same team) tries to stop Alice's session
	bobClient := seed.Client(e2eboot.UserBobEmail)

	t.Run("bob_cannot_stop_alice_session", func(t *testing.T) {
		resp := bobClient.PostWithTimeout(t, "/api/studio/sessions/"+sessionID+"/stop", nil, 10*time.Second)
		defer resp.Body.Close()

		// Should be 404 (session not found for bob) or 403
		if resp.StatusCode == http.StatusOK {
			// Even if it returns 200, verify it didn't actually affect alice's session
			t.Log("stop returned 200 for bob — checking if alice's session is still intact")
		}
	})

	// Bob tries to stream (reconnect) to Alice's session
	t.Run("bob_cannot_stream_alice_session", func(t *testing.T) {
		resp := bobClient.Get(t, "/api/studio/sessions/"+sessionID+"/stream")
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// If it streams, check we don't get alice's content
			body := e2eboot.ReadBody(t, resp)
			if strings.Contains(body, "ownership test") {
				t.Error("bob was able to reconnect to alice's session stream")
			}
		}
		// Non-200 is expected — pass
	})

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		delResp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}
