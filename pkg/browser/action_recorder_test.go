package browser

import (
	"strings"
	"testing"
)

func TestActionCaptureEvalJSIsFunction(t *testing.T) {
	// go-rod Page.Eval wraps as `(<js>).apply(...)` — Eval payloads must be functions.
	for name, js := range map[string]string{
		"actionRecorderEvalJS":   actionRecorderEvalJS,
		"actionCaptureDisableJS": actionCaptureDisableJS,
		"actionCaptureClearJS":   actionCaptureClearJS,
		"actionCaptureGetLogJS":  actionCaptureGetLogJS,
	} {
		trimmed := strings.TrimSpace(js)
		if !strings.HasPrefix(trimmed, "() =>") && !strings.HasPrefix(trimmed, "function") {
			t.Errorf("%s must be a function expression for Page.Eval, got prefix %q", name, trimmed[:min(40, len(trimmed))])
		}
		if strings.HasSuffix(trimmed, ")();") {
			t.Errorf("%s must not be a completed IIFE (Page.Eval would call undefined.apply)", name)
		}
	}
	onNew := strings.TrimSpace(actionRecorderOnNewDocJS)
	if !strings.HasPrefix(onNew, "(function()") || !strings.HasSuffix(onNew, ")();") {
		t.Errorf("actionRecorderOnNewDocJS must be an IIFE for EvalOnNewDocument, got %q…", onNew[:min(60, len(onNew))])
	}
}

func TestPreferStableSelector(t *testing.T) {
	got := PreferStableSelector("div > span:nth-of-type(2)", "#main", `[data-testid="save"]`, `button[aria-label="Save"]`)
	if got != `[data-testid="save"]` {
		t.Fatalf("expected data-testid winner, got %q", got)
	}
	got = PreferStableSelector("div > a", `button[aria-label="Go"]`)
	if got != `button[aria-label="Go"]` {
		t.Fatalf("expected aria-label, got %q", got)
	}
	got = PreferStableSelector("", "#x")
	if got != "#x" {
		t.Fatalf("expected #x, got %q", got)
	}
}

func TestParseActionLogJSON(t *testing.T) {
	raw := `[{"t":10,"type":"click","selector":"[data-testid=\"x\"]","label":"X","url":"http://localhost/"},{"t":20,"type":"change","selector":"input[name=\"q\"]","value":"hi"}]`
	events, err := ParseActionLogJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("len=%d", len(events))
	}
	if events[0].Type != "click" || events[0].Selector != `[data-testid="x"]` {
		t.Fatalf("event0=%+v", events[0])
	}
	if events[1].Type != "change" || events[1].Value != "hi" {
		t.Fatalf("event1=%+v", events[1])
	}
	empty, err := ParseActionLogJSON("[]")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
}
