//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers same-team, same-org isolation: even when two users share
// an org and a team, their personal sessions and artifacts must remain
// invisible to each other. Personal scopes are user-scoped, not team-scoped.
package chat_auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_Session_SameTeamUserDenied verifies that a user in the same team
// and same org as the session owner cannot:
//   - read the session via GET /api/studio/sessions/{id}
//   - see the session ID in their own /api/studio/sessions list
//   - delete the session via DELETE /api/studio/sessions/{id}
//   - inject content into the session via POST /api/studio/chat with the
//     victim's sessionId
//
// Both Alice and Bob are seeded into acme/red — the most permissive org/team
// scope a non-owner could possibly have. If isolation holds here, it holds
// for less-privileged peers too.
//
// COVERS: CHAT-064
func TestE2E_Session_SameTeamUserDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (acme/red) creates a session
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": "Hello from Alice for same-team isolation test.",
	}, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}

	bobClient := seed.Client(e2eboot.UserBobEmail)

	t.Run("bob_cannot_read_alice_session", func(t *testing.T) {
		resp := bobClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var detail struct {
				ID       string `json:"id"`
				Messages []any  `json:"messages"`
			}
			e2eboot.DecodeJSON(t, resp, &detail)
			if detail.ID == sessionID && len(detail.Messages) > 0 {
				t.Error("bob (same team) read alice's session content — same-team personal session isolation broken")
			}
		}
		// 404, 403, or empty = pass
	})

	t.Run("bob_sessions_list_excludes_alice", func(t *testing.T) {
		resp := bobClient.Get(t, "/api/studio/sessions")
		defer resp.Body.Close()

		body := e2eboot.ReadBody(t, resp)
		if strings.Contains(body, sessionID) {
			t.Error("bob's session list contains alice's session ID — same-team list isolation broken")
		}
	})

	t.Run("bob_cannot_delete_alice_session", func(t *testing.T) {
		resp := bobClient.Delete(t, "/api/studio/sessions/"+sessionID)
		defer resp.Body.Close()

		// If DELETE returns OK/NoContent, verify alice's session still exists.
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
			defer aliceResp.Body.Close()
			if aliceResp.StatusCode != http.StatusOK {
				t.Error("bob's DELETE appears to have actually removed alice's session — same-team delete isolation broken")
			}
		}
	})

	// Bob tries to inject content into Alice's session by POSTing with her
	// sessionId. The chat handler resolves session storage per-user, so even
	// if it cosmetically echoes the sessionId back, Bob's writes must NOT
	// land in Alice's personal session content.
	t.Run("bob_cannot_inject_into_alice_session_content", func(t *testing.T) {
		const bobInjection = "Bob trying to inject into Alice's same-team session"
		resp := bobClient.PostWithTimeout(t, "/api/studio/chat", map[string]any{
			"message":   bobInjection,
			"sessionId": sessionID,
		}, 30*time.Second)
		// Drain Bob's stream so the request completes.
		if resp.StatusCode == http.StatusOK {
			_ = e2eboot.ParseSSEStream(t, resp.Body)
		}
		resp.Body.Close()

		// Brief wait for any persistence on Bob's side.
		time.Sleep(1 * time.Second)

		// Fetch Alice's session as Alice — Bob's text must NOT be present.
		aliceResp := aliceClient.Get(t, "/api/studio/sessions/"+sessionID)
		defer aliceResp.Body.Close()
		if aliceResp.StatusCode != http.StatusOK {
			t.Fatalf("alice cannot read her own session: %d", aliceResp.StatusCode)
		}
		body := e2eboot.ReadBody(t, aliceResp)
		if strings.Contains(body, bobInjection) {
			t.Error("bob's message was injected into alice's session content — same-team content isolation broken")
		}
	})

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		delResp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}

// TestE2E_Artifact_SameTeamUserDenied verifies that a user in the same team
// and same org as the artifact owner cannot download the artifact via the
// content endpoint. Artifact authorization must be session-owner-bound, not
// just team-bound.
//
// COVERS: CHAT-065
func TestE2E_Artifact_SameTeamUserDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (acme/red) creates a session with an artifact
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	body := map[string]any{
		"message":     `Write exactly "secret data from alice for same-team test" to /tmp/e2e-artifact-sameteam.md using write_file. Nothing else.`,
		"autoApprove": true,
	}
	events := aliceClient.SSE(t, "/api/studio/chat", body, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	defer func() {
		if e2eboot.RetainSessions() {
			return
		}
		resp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}()

	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		// Distinguish "LLM didn't try" (skip) from "sandbox broken" (fatal).
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: alice's write_file was " +
				"called but every tool_result reports a sandbox/k8s " +
				"scheduling failure. CHAT-065 cannot be exercised — fix " +
				"the cluster (kubectl get pods -n astonishe2e-sandbox).")
		case e2eboot.ToolNotCalled, e2eboot.ToolSucceeded:
			t.Skip("LLM did not produce an artifact — cannot test same-team artifact denial")
		}
	}

	var artifact struct {
		Path string `json:"path"`
	}
	e2eboot.DecodeEventData(t, &artifactEvents[0], &artifact)
	if artifact.Path == "" {
		t.Fatal("artifact path is empty")
	}

	// Bob (same team, same org) tries to download Alice's artifact
	bobClient := seed.Client(e2eboot.UserBobEmail)
	bobResp := bobClient.Get(t, "/api/studio/artifacts/content?path="+artifact.Path+"&session="+sessionID)
	defer bobResp.Body.Close()

	if bobResp.StatusCode == http.StatusOK {
		bobBody := e2eboot.ReadBody(t, bobResp)
		if strings.Contains(bobBody, "secret data from alice for same-team test") {
			t.Error("bob (same team) was able to read alice's artifact content — same-team artifact isolation broken")
		}
	}
	// 404 or 403 are expected — pass
}
