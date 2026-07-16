package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/agent"
)

const testBlueprintYAML = `type: tutorial_blueprint
suite: demo-tutorial
title: Open Studio
scenes:
  - id: hook
    title: Hook
    voiceover: Welcome to Studio.
    visual:
      kind: avatar
      description: Presenter
  - id: open
    title: Open
    voiceover: Click the Studio link.
    duration_hint_s: 4
    visual:
      kind: screen
      description: Click Studio
      drill_node: open_studio
`

func TestHandleTutorialBlueprintIntent_ApproveContinues(t *testing.T) {
	chatAgent := &agent.ChatAgent{}
	chatAgent.SetPendingTutorialBlueprint("sess-1", &agent.TutorialBlueprintPending{
		YAML:  testBlueprintYAML,
		Title: "Open Studio",
		Suite: "demo-tutorial",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/studio/chat", nil)
	var flusher http.Flusher // nil is fine for SendSSE

	handled, rewrite := handleTutorialBlueprintIntent(
		req, rec, flusher, chatAgent, nil, "user1", "sess-1", "__tutorial_blueprint_approve__",
	)
	if handled {
		t.Fatal("approve should fall through to ChatRunner (handled=false)")
	}
	if rewrite == "" {
		t.Fatal("expected rewriteMsg with drill YAML")
	}
	for _, want := range []string{
		"Approve & generate",
		"open_studio",
		"demo-tutorial",
		"validate_drill",
		"save_drill",
		"mode: tutorial",
		"browser_highlight",
		"animate_cursor",
		"REFINE CHECKLIST",
		"Dry-run",
		"source: snapshot",
	} {
		if !strings.Contains(rewrite, want) {
			t.Fatalf("rewriteMsg missing %q:\n%s", want, rewrite)
		}
	}
	if chatAgent.HasPendingTutorialBlueprint("sess-1") {
		t.Fatal("pending blueprint should be cleared after approve")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tutorial_blueprint_approved") {
		t.Fatalf("expected tutorial_blueprint_approved SSE, got:\n%s", body)
	}
	if strings.Contains(body, "event: done") {
		t.Fatal("approve must not emit done before ChatRunner")
	}
	if strings.Contains(body, "event: text") {
		t.Fatal("approve must not dump drill YAML as model text")
	}
}

func TestHandleTutorialBlueprintIntent_ApproveNoPending(t *testing.T) {
	chatAgent := &agent.ChatAgent{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/studio/chat", nil)

	handled, rewrite := handleTutorialBlueprintIntent(
		req, rec, nil, chatAgent, nil, "user1", "sess-2", "__tutorial_blueprint_approve__",
	)
	if !handled {
		t.Fatal("expected handled=true when no pending blueprint")
	}
	if rewrite != "" {
		t.Fatalf("expected empty rewrite, got %q", rewrite)
	}
	if !strings.Contains(rec.Body.String(), "No pending tutorial blueprint") {
		t.Fatalf("expected error SSE, got:\n%s", rec.Body.String())
	}
}

func TestHandleTutorialBlueprintIntent_Cancel(t *testing.T) {
	chatAgent := &agent.ChatAgent{}
	chatAgent.SetPendingTutorialBlueprint("sess-3", &agent.TutorialBlueprintPending{
		YAML:  testBlueprintYAML,
		Title: "Open Studio",
		Suite: "demo-tutorial",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/studio/chat", nil)

	handled, rewrite := handleTutorialBlueprintIntent(
		req, rec, nil, chatAgent, nil, "user1", "sess-3", "__tutorial_blueprint_cancel__",
	)
	if !handled {
		t.Fatal("cancel should fully handle the request")
	}
	if rewrite != "" {
		t.Fatalf("expected empty rewrite, got %q", rewrite)
	}
	if chatAgent.HasPendingTutorialBlueprint("sess-3") {
		t.Fatal("pending should be cleared on cancel")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cancelled") {
		t.Fatalf("expected cancel text, got:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatal("cancel should emit done")
	}
}

func TestHandleTutorialBlueprintIntent_ReviseFallsThrough(t *testing.T) {
	chatAgent := &agent.ChatAgent{}
	chatAgent.SetPendingTutorialBlueprint("sess-4", &agent.TutorialBlueprintPending{
		YAML: testBlueprintYAML, Title: "T", Suite: "s",
	})
	handled, rewrite := handleTutorialBlueprintIntent(
		httptest.NewRequest(http.MethodPost, "/", nil),
		httptest.NewRecorder(), nil, chatAgent, nil, "u", "sess-4", "__tutorial_blueprint_revise__",
	)
	if handled || rewrite != "" {
		t.Fatalf("revise should fall through unchanged, got handled=%v rewrite=%q", handled, rewrite)
	}
	if !chatAgent.HasPendingTutorialBlueprint("sess-4") {
		t.Fatal("revise must keep pending blueprint")
	}
}
