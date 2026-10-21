//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// Personal scopes are user-scoped, not team-scoped.
package chat_auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/tests/e2eboot"
)

// TestE2E_Session_SameTeamUserDenied verifies that sharing acme/red does not
// allow Bob to read, list, delete, or inject into Alice's personal session.
// COVERS: CHAT-064
func TestE2E_Session_SameTeamUserDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	const aliceMessage = "Hello from Alice for same-team isolation test."
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]string{
		"message": aliceMessage,
	}, 60*time.Second)
	urgentCompletedChat(t, events)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	path := "/api/studio/sessions/" + sessionID
	t.Cleanup(func() {
		if !e2eboot.RetainSessions() {
			resp := aliceClient.Delete(t, path)
			resp.Body.Close()
		}
	})

	// A denial is meaningful only if the owner has a real persisted session.
	before := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK)
	var detail struct {
		ID       string `json:"id"`
		Messages []any  `json:"messages"`
	}
	if err := json.Unmarshal([]byte(before), &detail); err != nil {
		t.Fatalf("decode owner's session: %v", err)
	}
	if detail.ID != sessionID || len(detail.Messages) == 0 || !strings.Contains(before, aliceMessage) {
		t.Fatal("owner's persisted session lacks its identity or original content")
	}
	assertOwnerUnchanged := func(t *testing.T) {
		t.Helper()
		if after := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK); after != before {
			t.Fatal("rejected same-team request changed the owner's persisted session")
		}
	}
	bobClient := seed.Client(e2eboot.UserBobEmail)

	t.Run("bob_cannot_read_alice_session", func(t *testing.T) {
		body := urgentStatus(t, bobClient.Get(t, path), http.StatusForbidden, http.StatusNotFound)
		if strings.Contains(body, aliceMessage) || strings.Contains(body, sessionID) {
			t.Error("denial response leaked the owner's session identity or content")
		}
		assertOwnerUnchanged(t)
	})

	t.Run("bob_sessions_list_excludes_alice", func(t *testing.T) {
		body := urgentStatus(t, bobClient.Get(t, "/api/studio/sessions"), http.StatusOK)
		var sessions []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(body), &sessions); err != nil || sessions == nil {
			t.Fatal("bob's session list is not a valid JSON array")
		}
		if strings.Contains(body, sessionID) || strings.Contains(body, aliceMessage) {
			t.Error("bob's session list contains alice's session identity or content")
		}
		assertOwnerUnchanged(t)
	})

	t.Run("bob_cannot_delete_alice_session", func(t *testing.T) {
		// Idempotent DELETE may hide an inaccessible session with 204, but
		// only unchanged owner persistence proves that it did not delete it.
		urgentStatus(t, bobClient.Delete(t, path), http.StatusForbidden, http.StatusNotFound, http.StatusNoContent)
		assertOwnerUnchanged(t)
	})

	t.Run("bob_cannot_inject_into_alice_session_content", func(t *testing.T) {
		urgentStatus(t, bobClient.PostWithTimeout(t, "/api/studio/chat", map[string]any{
			"message":   "Bob trying to inject into Alice's same-team session",
			"sessionId": sessionID,
		}, 30*time.Second), http.StatusForbidden, http.StatusNotFound)
		assertOwnerUnchanged(t)
	})
}

// TestE2E_Artifact_SameTeamUserDenied requires positive owner access as well as
// denial for a same-team peer. Global artifact unavailability is not isolation.
// COVERS: CHAT-065
func TestE2E_Artifact_SameTeamUserDenied(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)
	aliceClient := seed.Client(e2eboot.UserAliceEmail)
	const marker = "secret data from alice for same-team test"
	events := aliceClient.SSE(t, "/api/studio/chat", map[string]any{
		"message":     `Write exactly "` + marker + `" to /tmp/e2e-artifact-sameteam.md using write_file. Nothing else.`,
		"autoApprove": true,
	}, 120*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("alice did not get a session ID")
	}
	t.Cleanup(func() {
		if !e2eboot.RetainSessions() {
			resp := aliceClient.Delete(t, "/api/studio/sessions/"+sessionID)
			resp.Body.Close()
		}
	})
	urgentCompletedChat(t, events)
	artifactEvents := e2eboot.FindAllEvents(events, "artifact")
	if len(artifactEvents) == 0 {
		t.Fatal("required sandbox artifact was not produced; same-team denial cannot be established")
	}
	var artifact struct {
		Path string `json:"path"`
	}
	e2eboot.DecodeEventData(t, &artifactEvents[0], &artifact)
	if artifact.Path == "" {
		t.Fatal("artifact path is empty")
	}
	path := "/api/studio/artifacts/content?" + url.Values{"path": {artifact.Path}, "session": {sessionID}}.Encode()
	// Do not relax ART containment or accept an owner's 404 as a positive
	// control: live-artifact unavailability must leave this acceptance failing.
	before := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK)
	if !strings.Contains(before, marker) {
		t.Fatal("owner artifact response lacks the fixture content")
	}
	sessionPath := "/api/studio/sessions/" + sessionID
	sessionBefore := urgentStatus(t, aliceClient.Get(t, sessionPath), http.StatusOK)
	if !strings.Contains(sessionBefore, marker) {
		t.Fatal("owner's persisted session lacks the artifact fixture")
	}
	bobBody := urgentStatus(t, seed.Client(e2eboot.UserBobEmail).Get(t, path), http.StatusForbidden, http.StatusNotFound)
	if strings.Contains(bobBody, marker) {
		t.Error("artifact denial response leaked owner content")
	}
	if after := urgentStatus(t, aliceClient.Get(t, path), http.StatusOK); after != before {
		t.Error("rejected artifact read changed owner artifact content")
	}
	if after := urgentStatus(t, aliceClient.Get(t, sessionPath), http.StatusOK); after != sessionBefore {
		t.Error("rejected artifact read changed the owner's persisted session")
	}
}
