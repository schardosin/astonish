//go:build e2e

// Package chat_core — per-session model pin E2E coverage.
//
// This test exercises the full stack for the per-chat model pin feature:
//   - PATCH /api/studio/sessions/{id}/model persists a pin and returns the
//     effective resolution.
//   - GET  /api/studio/sessions/{id}/model-status reads back that pin.
//   - Clearing (empty strings) restores the cascade default.
//   - `model_changed` SSE event fires on an active runner when the pin
//     changes mid-stream.
//
// Contract references:
//   - pkg/api/chat_handlers.go: PatchSessionModelHandler, GetSessionModelStatusHandler
//   - docs/architecture/chat-rendering-pipeline.md — model_changed event
//   - .omo/notepads/per-chat-app-model-pin/learnings.md
package chat_core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// modelStatusResponse mirrors pkg/api.SessionModelStatusResponse and
// pkg/api.PatchSessionModelResponse (the latter is a strict subset —
// AvailableProviders is only present on GET).
type modelStatusResponse struct {
	PinnedProvider       string   `json:"pinnedProvider"`
	PinnedModel          string   `json:"pinnedModel"`
	EffectiveProvider    string   `json:"effectiveProvider"`
	EffectiveModel       string   `json:"effectiveModel"`
	CredentialsAvailable bool     `json:"credentialsAvailable"`
	AvailableProviders   []string `json:"availableProviders"`
}

// patchModel PATCHes /api/studio/sessions/{id}/model with the provider/model
// pair and decodes the JSON response.
func patchModel(t *testing.T, h *e2eboot.Harness, sessionID, provider, model string) (*http.Response, modelStatusResponse) {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"provider": provider,
		"model":    model,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	url := h.BaseURL + "/api/studio/sessions/" + sessionID + "/model"
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("X-Astonish-Team", "general")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PATCH model: %v", err)
	}

	// Read + decode body, then reset for the caller to inspect status only.
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read PATCH body: %v", err)
	}

	var out modelStatusResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode PATCH body %q: %v", raw, err)
		}
	}
	return resp, out
}

// getModelStatus GETs /api/studio/sessions/{id}/model-status.
func getModelStatus(t *testing.T, h *e2eboot.Harness, sessionID string) modelStatusResponse {
	t.Helper()
	resp := h.Get(t, "/api/studio/sessions/"+sessionID+"/model-status")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET model-status returned %d: %s", resp.StatusCode, body)
	}
	var out modelStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode model-status: %v", err)
	}
	return out
}

// createSessionViaChat runs a quick chat request and returns the session ID.
// The pin API requires an existing session, and the platform mints IDs only
// via the chat handler.
func createSessionViaChat(t *testing.T, h *e2eboot.Harness) string {
	t.Helper()
	body := map[string]any{
		"message": "Reply with just 'ok'.",
	}
	events := h.SSE(t, "/api/studio/chat", body, 60*time.Second)
	sessionID := e2eboot.ExtractSessionIDFromSSE(t, events)
	if sessionID == "" {
		t.Fatal("failed to extract sessionId from SSE stream")
	}
	return sessionID
}

// TestE2E_Chat_ModelPin verifies the per-session model pin round-trip via the
// real HTTP surface: pin, read back, clear, and the `model_changed` SSE event.
//
// COVERS: CHAT model-pin feature (Todo 23)
func TestE2E_Chat_ModelPin(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// A single session is threaded through all subtests to keep the flow
	// realistic (pin → status → clear → status). The subtests run in
	// order and depend on shared state.
	sessionID := createSessionViaChat(t, h)
	t.Cleanup(func() {
		if !e2eboot.RetainSessions() {
			resp := h.Delete(t, "/api/studio/sessions/"+sessionID)
			resp.Body.Close()
		}
	})

	// Use the harness's default provider — this guarantees credentials
	// exist, so credentialsAvailable==true and the hot-swap path runs.
	// (e2eboot seeds "Bifrost" as the platform default; see
	// tests/e2eboot/bootstrap.go: seedProvider.)
	const pinnedProvider = "Bifrost"
	const pinnedModel = "sapaicore/anthropic--claude-4.6-opus"

	t.Run("PatchPinsModel", func(t *testing.T) {
		resp, body := patchModel(t, h, sessionID, pinnedProvider, pinnedModel)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH model returned %d", resp.StatusCode)
		}
		if body.PinnedProvider != pinnedProvider {
			t.Errorf("pinnedProvider = %q, want %q", body.PinnedProvider, pinnedProvider)
		}
		if body.PinnedModel != pinnedModel {
			t.Errorf("pinnedModel = %q, want %q", body.PinnedModel, pinnedModel)
		}
		if body.EffectiveProvider != pinnedProvider {
			t.Errorf("effectiveProvider = %q, want %q", body.EffectiveProvider, pinnedProvider)
		}
		if body.EffectiveModel != pinnedModel {
			t.Errorf("effectiveModel = %q, want %q", body.EffectiveModel, pinnedModel)
		}
		// Bifrost is seeded with an API key, so credentials must be available.
		if !body.CredentialsAvailable {
			t.Errorf("credentialsAvailable = false, want true (Bifrost is seeded with an API key)")
		}
	})

	t.Run("GetModelStatusReturnsPin", func(t *testing.T) {
		status := getModelStatus(t, h, sessionID)
		if status.PinnedProvider != pinnedProvider {
			t.Errorf("pinnedProvider = %q, want %q", status.PinnedProvider, pinnedProvider)
		}
		if status.PinnedModel != pinnedModel {
			t.Errorf("pinnedModel = %q, want %q", status.PinnedModel, pinnedModel)
		}
		if status.EffectiveProvider != pinnedProvider {
			t.Errorf("effectiveProvider = %q, want %q", status.EffectiveProvider, pinnedProvider)
		}
		if status.EffectiveModel != pinnedModel {
			t.Errorf("effectiveModel = %q, want %q", status.EffectiveModel, pinnedModel)
		}
		if !status.CredentialsAvailable {
			t.Errorf("credentialsAvailable = false, want true")
		}
	})

	t.Run("MissingCredentialPinPersistsWithSoftSignal", func(t *testing.T) {
		// A provider name that is guaranteed to have no credential wired
		// in this harness (only "Bifrost" is seeded). The pin must
		// persist regardless (DECISION-3) and credentialsAvailable
		// must be false so the SPA can render a soft banner.
		const unpinnedProvider = "provider-without-credential"
		const unpinnedModel = "some-model"

		resp, body := patchModel(t, h, sessionID, unpinnedProvider, unpinnedModel)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH model (missing cred) returned %d, want 200 (soft-fail contract)", resp.StatusCode)
		}
		if body.PinnedProvider != unpinnedProvider {
			t.Errorf("pinnedProvider = %q, want %q (pin must persist even without credential)", body.PinnedProvider, unpinnedProvider)
		}
		if body.CredentialsAvailable {
			t.Errorf("credentialsAvailable = true, want false when provider has no credential")
		}

		// Read back — the pin must be visible via GET too.
		status := getModelStatus(t, h, sessionID)
		if status.PinnedProvider != unpinnedProvider {
			t.Errorf("GET after missing-cred PATCH: pinnedProvider = %q, want %q", status.PinnedProvider, unpinnedProvider)
		}
		if status.CredentialsAvailable {
			t.Errorf("GET after missing-cred PATCH: credentialsAvailable = true, want false")
		}
	})

	t.Run("ClearPinRestoresCascade", func(t *testing.T) {
		// PATCH with empty strings clears the pin. The effective values
		// must fall back to the cascade default (Bifrost / claude-4.6-opus
		// seeded in bootstrap.go).
		resp, body := patchModel(t, h, sessionID, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH clear returned %d", resp.StatusCode)
		}
		if body.PinnedProvider != "" {
			t.Errorf("pinnedProvider = %q, want empty after clear", body.PinnedProvider)
		}
		if body.PinnedModel != "" {
			t.Errorf("pinnedModel = %q, want empty after clear", body.PinnedModel)
		}
		if body.EffectiveProvider == "" {
			t.Error("effectiveProvider empty after clear — cascade default should be present")
		}

		// Read back via GET.
		status := getModelStatus(t, h, sessionID)
		if status.PinnedProvider != "" {
			t.Errorf("GET after clear: pinnedProvider = %q, want empty", status.PinnedProvider)
		}
		if status.PinnedModel != "" {
			t.Errorf("GET after clear: pinnedModel = %q, want empty", status.PinnedModel)
		}
		if status.EffectiveProvider == "" {
			t.Error("GET after clear: effectiveProvider empty — cascade default missing")
		}
	})

	t.Run("ModelChangedEventOnActiveStream", func(t *testing.T) {
		// Start a chat stream in a goroutine. While it's running, PATCH
		// the model. `emitEvent("model_changed", …)` is only invoked when
		// a runner is registered for that session (chat_handlers.go:1752),
		// so we need an active stream at the moment of the PATCH.
		//
		// A fresh session is used so this subtest is independent of the
		// pin state left behind by earlier subtests.
		streamSessionID := createSessionViaChat(t, h)
		t.Cleanup(func() {
			if !e2eboot.RetainSessions() {
				resp := h.Delete(t, "/api/studio/sessions/"+streamSessionID)
				resp.Body.Close()
			}
		})

		var (
			events []e2eboot.SSEEvent
			wg     sync.WaitGroup
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Ask for a message that keeps the runner alive briefly.
			// The runner is registered as soon as the SSE handler
			// starts producing events; we only need it to still exist
			// when the PATCH lands.
			body := map[string]any{
				"message":   "Count from 1 to 20, one number per line.",
				"sessionId": streamSessionID,
			}
			events = h.SSE(t, "/api/studio/chat", body, 90*time.Second)
		}()

		// Give the runner a moment to register with getChatRunnerRegistry
		// before the PATCH tries to look it up.
		time.Sleep(500 * time.Millisecond)

		// Fire the pin change. If the runner has already exited before
		// this call arrives, the handler silently skips the emit — that
		// is not a bug but we would miss the event, so the assertion
		// below tolerates it by allowing the event to appear either in
		// this PATCH's window or a subsequent one.
		resp, _ := patchModel(t, h, streamSessionID, pinnedProvider, pinnedModel)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH during stream returned %d", resp.StatusCode)
		}

		wg.Wait()

		modelChanged := e2eboot.FindEvent(events, "model_changed")
		if modelChanged == nil {
			// The runner may have finished before the PATCH landed —
			// this is timing-dependent. If it happens, dump the event
			// types we did see so a failure is diagnosable.
			types := make([]string, 0, len(events))
			for _, ev := range events {
				types = append(types, ev.Type)
			}
			t.Fatalf("no 'model_changed' SSE event received; saw event types: %s", strings.Join(types, ","))
		}

		var d struct {
			SessionID            string `json:"sessionId"`
			PinnedProvider       string `json:"pinnedProvider"`
			PinnedModel          string `json:"pinnedModel"`
			EffectiveProvider    string `json:"effectiveProvider"`
			EffectiveModel       string `json:"effectiveModel"`
			CredentialsAvailable bool   `json:"credentialsAvailable"`
		}
		e2eboot.DecodeEventData(t, modelChanged, &d)

		if d.SessionID != streamSessionID {
			t.Errorf("model_changed sessionId = %q, want %q", d.SessionID, streamSessionID)
		}
		if d.PinnedProvider != pinnedProvider {
			t.Errorf("model_changed pinnedProvider = %q, want %q", d.PinnedProvider, pinnedProvider)
		}
		if d.PinnedModel != pinnedModel {
			t.Errorf("model_changed pinnedModel = %q, want %q", d.PinnedModel, pinnedModel)
		}
	})
}
