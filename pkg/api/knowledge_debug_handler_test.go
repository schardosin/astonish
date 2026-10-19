package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestResolveInvocationKnowledgeDebug(t *testing.T) {
	tracking := func(id string, data any) *session.Event {
		e := &session.Event{InvocationID: id, Author: "system"}
		e.Actions.StateDelta = map[string]any{"_knowledge_injection": data}
		return e
	}
	response := func(id string) *session.Event { return &session.Event{InvocationID: id, Author: "assistant"} }
	user := &session.Event{Author: "user"}
	user.Content = genai.NewContentFromText("next", "user")
	legacy := map[string]any{"type": "knowledge", "query": "legacy"}
	captured := func(id string, dispatched bool) map[string]any {
		return map[string]any{"version": 1, "invocation_id": id, "capture_status": "captured", "dispatched": dispatched, "type": "knowledge"}
	}
	for _, tc := range []struct {
		name       string
		events     []*session.Event
		id         string
		want       bool
		dispatched bool
	}{
		{"exact", []*session.Event{tracking("A", captured("A", true)), response("A")}, "A", true, true},
		{"after response", []*session.Event{response("A"), tracking("A", captured("A", true))}, "A", true, true},
		{"missing must not borrow B", []*session.Event{response("A"), tracking("B", captured("B", true)), response("B")}, "A", false, false},
		{"missing must not borrow A", []*session.Event{tracking("A", captured("A", true)), response("A"), response("B")}, "B", false, false},
		{"mismatch", []*session.Event{tracking("A", captured("B", true)), response("A")}, "A", false, false},
		{"missing versioned event id", []*session.Event{tracking("", captured("A", true)), response("A")}, "A", false, false},
		{"malformed", []*session.Event{tracking("A", "bad"), response("A")}, "A", false, false},
		{"unknown", []*session.Event{tracking("A", captured("A", true)), response("A")}, "unknown", false, false},
		{"prepared then dispatched", []*session.Event{tracking("A", captured("A", false)), tracking("A", captured("A", true))}, "A", true, true},
		{"bounded legacy", []*session.Event{user, tracking("", legacy), response("A")}, "A", true, false},
		{"legacy user boundary", []*session.Event{tracking("", legacy), user, response("A")}, "A", false, false},
		{"legacy invocation boundary", []*session.Event{tracking("", legacy), response("B"), response("A")}, "A", false, false},
		{"legacy exact", []*session.Event{tracking("A", legacy)}, "A", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			knowledge, _ := resolveInvocationKnowledgeDebug(tc.events, tc.id)
			if (knowledge != nil) != tc.want {
				t.Fatalf("knowledge=%#v, want record=%v", knowledge, tc.want)
			}
			if knowledge != nil && knowledge["version"] != nil && knowledge["dispatched"] != tc.dispatched {
				t.Fatalf("dispatch=%v", knowledge)
			}
			if knowledge != nil && knowledge["version"] == nil && knowledge["capture_status"] != "unavailable" {
				t.Fatal("legacy snapshot not marked unavailable")
			}
		})
	}
	if legacy["capture_status"] != nil {
		t.Fatal("resolver mutated persisted legacy payload")
	}
	toolEvent := &session.Event{InvocationID: "B", Author: "system"}
	toolEvent.Actions.StateDelta = map[string]any{"_tool_injection": map[string]any{"tools": []string{"echo"}}}
	_, toolsA := resolveInvocationKnowledgeDebug([]*session.Event{response("A"), toolEvent}, "A")
	_, toolsB := resolveInvocationKnowledgeDebug([]*session.Event{response("A"), toolEvent}, "B")
	if toolsA != nil || toolsB == nil {
		t.Fatal("tool attribution crossed invocation")
	}
}

func TestStudioKnowledgeDebugRequiresSuperadmin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/studio/sessions/session/knowledge-debug?invocationId=inv", nil)
	w := httptest.NewRecorder()
	StudioKnowledgeDebugHandler(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestStudioKnowledgeDebugRequiresInvocationId(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/studio/sessions/session/knowledge-debug", nil)
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{ID: "admin", PlatformRole: "superadmin"}))
	w := httptest.NewRecorder()
	StudioKnowledgeDebugHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPlatformAdminKnowledgeDebugRequiresSuperadmin(t *testing.T) {
	tests := []struct {
		name string
		user *PlatformUser
		want int
	}{
		{name: "unauthenticated", want: http.StatusUnauthorized},
		{name: "regular user", user: &PlatformUser{ID: "user", PlatformRole: ""}, want: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/platform/admin/sessions/session/knowledge-debug?org=acme&scope=personal&user=user&invocationId=inv", nil)
			if tt.user != nil {
				req = req.WithContext(WithPlatformUser(req.Context(), tt.user))
			}
			w := httptest.NewRecorder()
			PlatformAdminKnowledgeDebugHandler(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
