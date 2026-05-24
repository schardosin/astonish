//go:build e2e

// Package chat_core contains E2E tests for the Studio Chat core functionality:
// sending messages, receiving SSE responses, session management, tool calls.
package chat_core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// collectText concatenates the text payload of every "text" SSE event in
// order. Used by tests that need to assert on the agent's full reply.
func collectText(t *testing.T, events []e2eboot.SSEEvent) string {
	t.Helper()
	textEvents := e2eboot.FindAllEvents(events, "text")
	var b strings.Builder
	for i := range textEvents {
		var d struct {
			Text string `json:"text"`
		}
		e2eboot.DecodeEventData(t, &textEvents[i], &d)
		b.WriteString(d.Text)
	}
	return b.String()
}

// TestE2E_Chat_SendAndReceive verifies that a user can send a message via
// POST /api/studio/chat and receive a complete LLM response streamed over SSE.
//
// COVERS: CHAT-001
func TestE2E_Chat_SendAndReceive(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	body := map[string]string{"message": "What is 2 + 2? Reply with only the number."}
	events := h.SSE(t, "/api/studio/chat", body, 60*time.Second)

	// Must have a session event
	sessionEv := e2eboot.FindEvent(events, "session")
	if sessionEv == nil {
		t.Fatal("no 'session' event received")
	}
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("session ID was empty")
	}

	// Must have at least one text event with content
	textEvents := e2eboot.FindAllEvents(events, "text")
	if len(textEvents) == 0 {
		t.Fatal("no 'text' events received — LLM did not respond")
	}

	// Concatenate all text
	var fullText strings.Builder
	for _, ev := range textEvents {
		var d struct {
			Text string `json:"text"`
		}
		e2eboot.DecodeEventData(t, &ev, &d)
		fullText.WriteString(d.Text)
	}
	responseText := fullText.String()
	if !strings.Contains(responseText, "4") {
		t.Errorf("expected LLM response to contain '4', got: %s", responseText)
	}

	// Must have a done event
	doneEv := e2eboot.FindEvent(events, "done")
	if doneEv == nil {
		t.Fatal("no 'done' event received — stream did not complete")
	}

	// Cleanup session (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_MultiTurnContext verifies that the agent can continue a
// task across turns within the same session by referencing prior assistant
// output — without the user re-stating it. Concretely: turn 1 produces a
// list of colors; turn 2 says "translate them to Spanish" with no further
// context. The agent must use the prior turn's output as input.
//
// This deliberately does NOT test recalling a user-stated fact ("my name
// is X" → "what is my name?"), which is a tautology — the LLM trivially
// has access to its own context window, so that variant tested nothing.
// Cross-session memory persistence is covered by CHAT-016.
//
// COVERS: CHAT-002
func TestE2E_Chat_MultiTurnContext(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Turn 1: produce three primary colors. Stable, common output that
	// the agent will almost always emit as "red, blue, yellow" (or some
	// permutation containing those words).
	body1 := map[string]any{
		"message": "Name three primary colors. Reply with just the three names separated by commas, nothing else.",
	}
	events1 := h.SSE(t, "/api/studio/chat", body1, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events1)
	if sessionID == "" {
		t.Fatal("no session ID from turn 1")
	}

	// Sanity-check that turn 1 actually produced primary-color words.
	turn1Text := collectText(t, events1)
	turn1Lower := strings.ToLower(turn1Text)
	primary := []string{"red", "blue", "yellow"}
	primaryHits := 0
	for _, c := range primary {
		if strings.Contains(turn1Lower, c) {
			primaryHits++
		}
	}
	if primaryHits < 2 {
		t.Skipf("turn 1 did not produce recognizable primary colors (got %q) — cannot exercise turn-2 continuation", turn1Text)
	}

	// Turn 2: continue the task — translate "them" to Spanish. The user
	// does NOT re-state the colors; the agent must use turn-1 output.
	body2 := map[string]any{
		"message":   "Now translate them to Spanish. Reply with just the three Spanish words separated by commas, nothing else.",
		"sessionId": sessionID,
	}
	events2 := h.SSE(t, "/api/studio/chat", body2, 60*time.Second)

	turn2Text := collectText(t, events2)
	turn2Lower := strings.ToLower(turn2Text)

	// Expected Spanish translations of the primary colors. Accept a
	// loose match: at least 2 of 3 must appear, since the LLM might
	// translate a slightly different subset (e.g. "amarillo" vs
	// "amarillento") or the agent's turn-1 colors might have included
	// a non-primary like "green"/"verde".
	spanish := map[string]string{
		"red":    "rojo",
		"blue":   "azul",
		"yellow": "amarillo",
		"green":  "verde", // accept secondary too
	}
	hits := 0
	for _, es := range spanish {
		if strings.Contains(turn2Lower, es) {
			hits++
		}
	}
	if hits < 2 {
		t.Errorf("turn 2 did not continue task using turn-1 output:\n  turn1=%q\n  turn2=%q\n  expected at least 2 Spanish color words", turn1Text, turn2Text)
	}

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_SessionPersistsAndReloads verifies that a session is persisted
// after chatting and can be retrieved via the sessions list API.
//
// COVERS: CHAT-004
func TestE2E_Chat_SessionPersistsAndReloads(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Create a session by chatting
	body := map[string]string{"message": "Hello, please respond briefly."}
	events := h.SSE(t, "/api/studio/chat", body, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}

	// Wait briefly for persistence
	time.Sleep(1 * time.Second)

	// Fetch session by ID
	resp := h.Get(t, "/api/studio/sessions/"+sessionID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := e2eboot.ReadBody(t, resp)
		t.Fatalf("GET session returned %d: %s", resp.StatusCode, body)
	}

	var detail struct {
		ID       string `json:"id"`
		Messages []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	e2eboot.DecodeJSON(t, resp, &detail)

	if detail.ID != sessionID {
		t.Errorf("session ID mismatch: got %q want %q", detail.ID, sessionID)
	}

	// Should have at least a user message and an agent message
	hasUser := false
	hasAgent := false
	for _, msg := range detail.Messages {
		if msg.Type == "user" {
			hasUser = true
		}
		if msg.Type == "agent" {
			hasAgent = true
		}
	}
	if !hasUser {
		t.Error("session history missing 'user' message")
	}
	if !hasAgent {
		t.Error("session history missing 'agent' message")
	}

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		delResp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}

// TestE2E_Chat_SessionCreateListDelete verifies the full session lifecycle:
// create via chat, verify it appears in the sessions list, delete it.
//
// COVERS: CHAT-028
func TestE2E_Chat_SessionCreateListDelete(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Create a session
	body := map[string]string{"message": "Quick test message."}
	events := h.SSE(t, "/api/studio/chat", body, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}

	// Wait for persistence
	time.Sleep(1 * time.Second)

	// List sessions — ours should be there
	listResp := h.Get(t, "/api/studio/sessions")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list sessions returned %d", listResp.StatusCode)
	}

	listBody := e2eboot.ReadBody(t, listResp)
	if !strings.Contains(listBody, sessionID) {
		t.Errorf("session %s not found in sessions list", sessionID)
	}

	// Delete the session
	delResp := h.Delete(t, "/api/studio/sessions/"+sessionID)
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete session returned %d", delResp.StatusCode)
	}

	// Verify it's gone
	getResp := h.Get(t, "/api/studio/sessions/"+sessionID)
	defer getResp.Body.Close()
	if getResp.StatusCode == http.StatusOK {
		// Check if it returned empty or the session is actually gone
		var detail struct {
			ID string `json:"id"`
		}
		json.NewDecoder(getResp.Body).Decode(&detail)
		if detail.ID == sessionID {
			t.Error("session still exists after deletion")
		}
	}
	// 404 is expected here — session is gone
}

// TestE2E_Chat_UnauthenticatedRejected verifies that unauthenticated requests
// to the studio chat endpoint are rejected with 401.
//
// COVERS: CHAT-031
func TestE2E_Chat_UnauthenticatedRejected(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Make a request with no auth header
	reqBody, _ := json.Marshal(map[string]string{"message": "hello"})
	req, _ := http.NewRequest("POST", h.BaseURL+"/api/studio/chat", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// TestE2E_Chat_SessionTitleGenerated verifies that after the first chat turn,
// the session receives an auto-generated title (visible in session_title event
// or in the sessions list).
//
// COVERS: CHAT-032
func TestE2E_Chat_SessionTitleGenerated(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	body := map[string]string{"message": "Tell me about the Go programming language in one sentence."}
	events := h.SSE(t, "/api/studio/chat", body, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("no session ID")
	}

	// Check if session_title event was in the stream
	titleEv := e2eboot.FindEvent(events, "session_title")
	if titleEv != nil {
		var d struct {
			Title string `json:"title"`
		}
		e2eboot.DecodeEventData(t, titleEv, &d)
		if d.Title == "" {
			t.Error("session_title event had empty title")
		}
		// Cleanup (skipped under shared-inspect mode)
		if !e2eboot.RetainSessions() {
			resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
			resp.Body.Close()
		}
		return
	}

	// If not in stream (title may be generated async), check the session detail
	time.Sleep(3 * time.Second) // allow async title generation

	resp := h.Get(t, "/api/studio/sessions/"+sessionID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET session returned %d", resp.StatusCode)
	}

	var detail struct {
		Title string `json:"title"`
	}
	e2eboot.DecodeJSON(t, resp, &detail)

	if detail.Title == "" {
		t.Error("session has no auto-generated title after first turn")
	}

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		delResp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		delResp.Body.Close()
	}
}

// TestE2E_Chat_ToolCall verifies that the agent can call a safe tool
// (read-only) and the tool_call + tool_result SSE events are emitted.
//
// COVERS: CHAT-006
func TestE2E_Chat_ToolCall(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Ask something that should trigger a tool call (memory_search or similar)
	// Using autoApprove to not block on unsafe tools.
	body := map[string]any{
		"message":     "Search my memories for the word 'test'. Use the memory_search tool.",
		"autoApprove": true,
	}
	events := h.SSE(t, "/api/studio/chat", body, 90*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)

	// Look for tool_call events
	toolCalls := e2eboot.FindAllEvents(events, "tool_call")
	if len(toolCalls) == 0 {
		// The LLM might not have called a tool — this is acceptable for a real
		// LLM but we should at least verify we got a valid response
		textEvents := e2eboot.FindAllEvents(events, "text")
		if len(textEvents) == 0 {
			t.Fatal("no tool_call and no text events — agent did nothing")
		}
		t.Skip("LLM chose not to use a tool — acceptable with real provider")
	}

	// Verify tool_call has a name
	var tc struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	e2eboot.DecodeEventData(t, &toolCalls[0], &tc)
	if tc.Name == "" {
		t.Error("tool_call event has empty tool name")
	}

	// Should also have a corresponding tool_result
	toolResults := e2eboot.FindAllEvents(events, "tool_result")
	if len(toolResults) == 0 {
		t.Error("tool_call present but no tool_result received")
	}

	// Cleanup (skipped under shared-inspect mode)
	if sessionID != "" && !e2eboot.RetainSessions() {
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_AutoApproveBypassesGate verifies that with autoApprove=true,
// unsafe tools execute without emitting an approval event.
//
// COVERS: CHAT-010
func TestE2E_Chat_AutoApproveBypassesGate(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Ask to write a file (unsafe tool) with autoApprove
	body := map[string]any{
		"message":     "Create a file called /tmp/e2e-test-autoapprove.txt with the text 'hello'. Use write_file.",
		"autoApprove": true,
	}
	events := h.SSE(t, "/api/studio/chat", body, 90*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)

	// Should NOT have an approval event (it was auto-approved)
	approvalEv := e2eboot.FindEvent(events, "approval")
	if approvalEv != nil {
		t.Error("approval event received despite autoApprove=true")
	}

	// Might have auto_approved event
	autoApprovedEvents := e2eboot.FindAllEvents(events, "auto_approved")

	// Should have tool execution (tool_call + tool_result) OR the LLM declined
	toolCalls := e2eboot.FindAllEvents(events, "tool_call")
	if len(toolCalls) == 0 && len(autoApprovedEvents) == 0 {
		// LLM might not have tried — skip
		t.Skip("LLM did not attempt a write tool call")
	}

	// If tool was called, verify no approval gate fired and verify the
	// sandbox actually executed it. Differentiate between "infrastructure
	// broken" (loud fail) and "tool blocked logically" (test failure).
	if len(toolCalls) > 0 {
		switch e2eboot.ClassifyToolOutcome(events, "write_file") {
		case e2eboot.ToolInfraFailure:
			t.Fatal("sandbox infrastructure broken: write_file was called " +
				"but every tool_result reports a sandbox/k8s scheduling " +
				"failure (e.g. 'pod does not have a host assigned'). " +
				"This is not a CHAT-010 regression — fix the cluster " +
				"(check kubectl get pods -n astonishe2e-sandbox).")
		case e2eboot.ToolNotCalled:
			// LLM chose a different tool (e.g. shell_command). That's fine
			// for CHAT-010 as long as a tool_result came back at all.
			toolResults := e2eboot.FindAllEvents(events, "tool_result")
			if len(toolResults) == 0 {
				t.Error("tool_call present but no tool_result — tool may have been blocked")
			}
		case e2eboot.ToolSucceeded:
			// expected path — nothing further to assert here
		}
	}

	// Cleanup (skipped under shared-inspect mode)
	if sessionID != "" && !e2eboot.RetainSessions() {
		resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
		resp.Body.Close()
	}
}

// TestE2E_Chat_CrossSessionMemoryRecall verifies that information saved
// to the long-term memory store in one session is retrievable from a
// brand-new, unrelated session. This is fundamentally different from
// CHAT-002 (same-session transcript replay): the second session starts
// with an empty event history, so the only way to recall the fact is
// via memory injection at session start (or memory_search) against the
// persisted memory store.
//
// The fact under test is durable INFRASTRUCTURE knowledge that fits
// the system's memory rules cleanly: a Proxmox host's hostname and IP.
// Per the reflector prompt (`pkg/agent/platform_reflector.go` and
// `pkg/agent/memory_reflection.go`), connection details / hostnames /
// API base URLs / environment-specific configuration ARE durable team
// knowledge — exactly the bucket cross-session shared memory is for.
//
// This test does NOT skip on missing prerequisites: tests must pass or
// fail. If the system fails to persist a piece of infrastructure
// knowledge stated in session A and recall it in session B, that is a
// real regression and the test fails loudly.
//
// COVERS: CHAT-016
func TestE2E_Chat_CrossSessionMemoryRecall(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// --- Session A: state durable infrastructure facts --------------------
	// We give the system two related, durable, environment-specific facts
	// in one user message. Both fit the reflector's "Connection details /
	// hostnames / environment-specific information" positive criteria:
	//   1. The Proxmox server's hostname is `pve-prod-01`.
	//   2. Its management IP is `192.168.55.42`.
	// We do NOT instruct the agent to use any tool — memory_save is always
	// available and the post-turn reflector also runs, so the system has
	// two chances to persist this knowledge naturally.
	bodyA := map[string]any{
		"message":     "Our Proxmox server is named pve-prod-01 and its management IP is 192.168.55.42. Acknowledge briefly.",
		"autoApprove": true,
	}
	eventsA := h.SSE(t, "/api/studio/chat", bodyA, 180*time.Second)
	sessionA := e2eboot.ExtractSessionIDFromSSE(t, eventsA)
	if sessionA == "" {
		t.Fatal("session A: no session ID extracted from SSE stream")
	}

	// The post-turn memory reflector runs asynchronously after the SSE
	// stream closes (see chat_agent_run.go: `go reflector.Reflect(...)`).
	// Wait long enough for it to complete an LLM call and persist the
	// memory before we ask in session B. The reflector itself has a 45s
	// internal timeout, so 60s is a reasonable upper bound that still
	// keeps the test fast when reflection completes promptly.
	time.Sleep(60 * time.Second)

	// --- Session B: brand-new session, ask for the IP ----------------------
	// No sessionId in the body → backend mints a fresh session with empty
	// transcript. The only way the agent can answer is via persisted
	// memory (injected at session start or fetched via memory_search).
	bodyB := map[string]any{
		"message":     "What is the management IP of our Proxmox server pve-prod-01? Reply with only the IP address, nothing else.",
		"autoApprove": true,
	}
	eventsB := h.SSE(t, "/api/studio/chat", bodyB, 180*time.Second)
	sessionB := e2eboot.ExtractSessionIDFromSSE(t, eventsB)
	if sessionB == "" {
		t.Fatal("session B: no session ID extracted from SSE stream")
	}
	if sessionB == sessionA {
		t.Fatalf("sessions must be distinct: A=%s B=%s", sessionA, sessionB)
	}

	// Collect all text emitted in session B.
	textEvents := e2eboot.FindAllEvents(eventsB, "text")
	var reply strings.Builder
	for i := range textEvents {
		var td struct {
			Text string `json:"text"`
		}
		e2eboot.DecodeEventData(t, &textEvents[i], &td)
		reply.WriteString(td.Text)
	}
	combined := reply.String()
	if !strings.Contains(combined, "192.168.55.42") {
		t.Errorf("session B reply does not contain '192.168.55.42' (the IP stated in session A): %q", combined)
	}

	// Cleanup (skipped under shared-inspect mode)
	if !e2eboot.RetainSessions() {
		if sessionA != "" {
			resp := h.Delete(t, "/api/studio/sessions/"+sessionA)
			resp.Body.Close()
		}
		if sessionB != "" {
			resp := h.Delete(t, "/api/studio/sessions/"+sessionB)
			resp.Body.Close()
		}
	}
}
