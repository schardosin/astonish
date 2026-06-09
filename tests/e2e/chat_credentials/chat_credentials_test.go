//go:build e2e

// Package chat_credentials contains E2E tests for credential redaction in
// platform mode. These tests verify that secrets stored in the credential
// store are never leaked to the frontend — neither in the live SSE stream
// during a chat session, nor when the session history is loaded afterwards.
//
// This is a security-critical test. Failures indicate credential leakage.
package chat_credentials

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// TestE2E_Chat_CredentialRedactionLiveStream verifies that when the LLM
// resolves a credential during a chat session, the raw secret value is
// NEVER present in the SSE event stream sent to the browser.
//
// Strategy:
//   - Use the seeded credential "alice-personal-cred" with known token
//     "fake-token-for-alice-personal-cred"
//   - Ask the agent to list credentials and use the credential (triggering
//     resolve_credential internally)
//   - Scan the ENTIRE SSE stream for the raw token value
//
// COVERS: CHAT-076
func TestE2E_Chat_CredentialRedactionLiveStream(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	client := seed.Client(e2eboot.UserAliceEmail)

	// The secret value that MUST be redacted (from seed.go: setCred)
	rawSecret := fmt.Sprintf("fake-token-for-%s", e2eboot.CredAlicePersonal)

	// Send a chat message that will cause the agent to resolve the credential.
	// The prompt is designed to trigger resolve_credential tool usage.
	body := map[string]any{
		"message": fmt.Sprintf(
			"I have a credential named %q. Please resolve it using the resolve_credential tool and show me the result. Also run a shell command: echo 'resolved'.",
			e2eboot.CredAlicePersonal,
		),
	}

	events := client.SSE(t, "/api/studio/chat", body, 90*time.Second)

	// Extract session ID for history check
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID in SSE stream")
	}

	// CRITICAL ASSERTION: The raw secret must NEVER appear in any SSE event
	for i, ev := range events {
		if strings.Contains(ev.Data, rawSecret) {
			t.Fatalf("SECURITY VIOLATION: SSE event[%d] type=%q contains raw credential secret %q\nData: %s",
				i, ev.Type, rawSecret, ev.Data)
		}
	}

	t.Logf("PASS: Raw secret %q not found in %d SSE events", rawSecret, len(events))

	// Verify we got a meaningful response (not just an error)
	doneEv := e2eboot.FindEvent(events, "done")
	if doneEv == nil {
		t.Fatal("no 'done' event — chat did not complete")
	}

	// Cleanup
	if !e2eboot.RetainSessions() {
		resp := client.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_CredentialRedactionSessionHistory verifies that when a session
// is loaded from the database (GET /api/studio/sessions/{id}), the response
// body does NOT contain raw credential secrets — even if they were present
// in the session events before redaction was retroactively applied.
//
// Strategy:
//   - Chat with the agent, triggering credential resolution
//   - Load the session detail via the REST API
//   - Scan the response body for the raw secret
//
// COVERS: CHAT-077
func TestE2E_Chat_CredentialRedactionSessionHistory(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	client := seed.Client(e2eboot.UserAliceEmail)

	// The secret value that MUST be redacted
	rawSecret := fmt.Sprintf("fake-token-for-%s", e2eboot.CredAlicePersonal)

	// Send a chat message that triggers credential usage
	body := map[string]any{
		"message": fmt.Sprintf(
			"Resolve the credential named %q using resolve_credential and tell me what type it is.",
			e2eboot.CredAlicePersonal,
		),
	}

	events := client.SSE(t, "/api/studio/chat", body, 90*time.Second)

	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID in SSE stream")
	}

	// Wait briefly for session persistence to complete
	time.Sleep(1 * time.Second)

	// Load session history via REST API
	resp := client.Get(t, "/api/studio/sessions/"+sessionID)
	respBody := e2eboot.ReadBody(t, resp)

	// CRITICAL ASSERTION: The raw secret must NOT appear in the session history
	if strings.Contains(respBody, rawSecret) {
		t.Fatalf("SECURITY VIOLATION: Session history response contains raw credential secret %q\nBody length: %d bytes",
			rawSecret, len(respBody))
	}

	t.Logf("PASS: Raw secret %q not found in session history response (%d bytes)", rawSecret, len(respBody))

	// Sanity check: verify the response is valid JSON with messages
	var historyResp struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(respBody), &historyResp); err != nil {
		t.Fatalf("session history response is not valid JSON: %v", err)
	}
	if len(historyResp.Messages) == 0 {
		t.Log("WARN: no messages in session history — test may not have triggered credential resolution")
	}

	// Cleanup
	if !e2eboot.RetainSessions() {
		resp := client.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_CredentialRedactionTeamCredential verifies that team-level
// credentials are also properly redacted. Team credentials are resolved
// through the merged credential store (personal-first, team-fallback),
// and their values must be equally protected.
//
// COVERS: CHAT-078
func TestE2E_Chat_CredentialRedactionTeamCredential(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	client := seed.Client(e2eboot.UserAliceEmail)

	// Team credential secret value
	rawSecret := fmt.Sprintf("fake-token-for-%s", e2eboot.CredAcmeRedTeam)

	// Ask the agent to resolve the team credential
	body := map[string]any{
		"message": fmt.Sprintf(
			"Use the resolve_credential tool to resolve the credential named %q and tell me the Authorization header value.",
			e2eboot.CredAcmeRedTeam,
		),
	}

	events := client.SSE(t, "/api/studio/chat", body, 90*time.Second)

	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID in SSE stream")
	}

	// CRITICAL: Check live stream
	for i, ev := range events {
		if strings.Contains(ev.Data, rawSecret) {
			t.Fatalf("SECURITY VIOLATION: SSE event[%d] type=%q leaks team credential %q\nData: %s",
				i, ev.Type, rawSecret, ev.Data)
		}
	}

	// Also check session history
	time.Sleep(1 * time.Second)
	resp := client.Get(t, "/api/studio/sessions/"+sessionID)
	respBody := e2eboot.ReadBody(t, resp)

	if strings.Contains(respBody, rawSecret) {
		t.Fatalf("SECURITY VIOLATION: Session history leaks team credential %q", rawSecret)
	}

	t.Logf("PASS: Team credential %q properly redacted in both live stream and history", e2eboot.CredAcmeRedTeam)

	// Cleanup
	if !e2eboot.RetainSessions() {
		resp := client.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}
