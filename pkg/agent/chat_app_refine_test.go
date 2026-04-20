package agent

import (
	"context"
	"strings"
	"testing"
)

func TestActiveApp_CRUD(t *testing.T) {
	ca := &ChatAgent{
		activeApps: make(map[string]*ActiveApp),
	}
	sessionID := "test-session-1"

	// Initially no active app
	if ca.HasActiveApp(sessionID) {
		t.Fatal("expected no active app initially")
	}
	if ca.GetActiveApp(sessionID) != nil {
		t.Fatal("expected nil for non-existent app")
	}

	// Set an active app
	app := &ActiveApp{
		AppID:   "uuid-1",
		Title:   "Sales Dashboard",
		Code:    "function SalesDashboard() { return <div>hello</div> }",
		Version: 1,
	}
	ca.SetActiveApp(sessionID, app)

	if !ca.HasActiveApp(sessionID) {
		t.Fatal("expected active app after Set")
	}
	got := ca.GetActiveApp(sessionID)
	if got == nil || got.AppID != "uuid-1" {
		t.Fatalf("unexpected app: %+v", got)
	}

	// Record modification
	ca.RecordAppModification(sessionID, "make header blue")
	got = ca.GetActiveApp(sessionID)
	if len(got.Modifications) != 1 || got.Modifications[0] != "make header blue" {
		t.Fatalf("unexpected modifications: %v", got.Modifications)
	}

	// Clear
	ca.ClearActiveApp(sessionID)
	if ca.HasActiveApp(sessionID) {
		t.Fatal("expected no active app after clear")
	}
}

func TestClassifyAppIntent_MagicStrings(t *testing.T) {
	ca := &ChatAgent{
		activeApps: make(map[string]*ActiveApp),
	}

	tests := []struct {
		msg    string
		expect AppRefinementIntent
	}{
		{"__app_done__", AppIntentDone},
		{"done", AppIntentDone},
		{"Done", AppIntentDone},
		{"I'm done", AppIntentDone},
		{"looks good", AppIntentDone},
		{"perfect", AppIntentDone},
		// Without LLM, everything else defaults to refine
		{"make the header blue", AppIntentRefine},
		{"add a search bar", AppIntentRefine},
		{"what's the weather?", AppIntentRefine}, // no LLM = defaults to refine
	}

	for _, tt := range tests {
		got := ca.ClassifyAppIntent(context.Background(), tt.msg, nil)
		if got != tt.expect {
			t.Errorf("ClassifyAppIntent(%q) = %d, want %d", tt.msg, got, tt.expect)
		}
	}
}

func TestClassifyAppIntent_WithLLM(t *testing.T) {
	ca := &ChatAgent{
		activeApps: make(map[string]*ActiveApp),
	}

	tests := []struct {
		msg       string
		llmReply  string
		expect    AppRefinementIntent
	}{
		{"make it blue", "REFINE", AppIntentRefine},
		{"I'm satisfied", "DONE", AppIntentDone},
		{"what's the weather?", "UNRELATED", AppIntentUnrelated},
		{"something", "refine\n", AppIntentRefine}, // lowercase + trailing newline
		{"something", "UNKNOWN_RESPONSE", AppIntentRefine}, // fallback to refine
	}

	for _, tt := range tests {
		llmFunc := func(_ context.Context, _ string) (string, error) {
			return tt.llmReply, nil
		}
		got := ca.ClassifyAppIntent(context.Background(), tt.msg, llmFunc)
		if got != tt.expect {
			t.Errorf("ClassifyAppIntent(%q, llm=%q) = %d, want %d", tt.msg, tt.llmReply, got, tt.expect)
		}
	}
}

func TestBuildAppRefinementContext(t *testing.T) {
	app := &ActiveApp{
		AppID:         "uuid-abc",
		Title:         "Sales Dashboard",
		Code:          "function SalesDashboard() { return <div>v2</div> }",
		Version:       2,
		Modifications: []string{"add a chart", "make header blue"},
	}

	ctx := BuildAppRefinementContext(app)

	// Should contain the current source code
	if !strings.Contains(ctx, "function SalesDashboard()") {
		t.Error("context should contain current source code")
	}

	// Should contain the title
	if !strings.Contains(ctx, "Sales Dashboard") {
		t.Error("context should contain the title")
	}

	// Should contain version number
	if !strings.Contains(ctx, "version 2") {
		t.Error("context should contain version number")
	}

	// Should contain modification history
	if !strings.Contains(ctx, "add a chart") {
		t.Error("context should contain modification history")
	}
	if !strings.Contains(ctx, "make header blue") {
		t.Error("context should contain modification history")
	}

	// Should instruct to output full component in astonish-app fence
	if !strings.Contains(ctx, "```astonish-app") {
		t.Error("context should instruct to use astonish-app fence")
	}
}

func TestBuildAppRefinementContext_NoModifications(t *testing.T) {
	app := &ActiveApp{
		AppID:   "uuid-xyz",
		Title:   "Counter",
		Code:    "function Counter() { return <div>0</div> }",
		Version: 1,
	}

	ctx := BuildAppRefinementContext(app)

	// Should contain code but no modifications section
	if !strings.Contains(ctx, "function Counter()") {
		t.Error("context should contain source code")
	}
	if strings.Contains(ctx, "Previous Modifications") {
		t.Error("context should not contain modifications section when empty")
	}
}
