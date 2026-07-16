package browser

import (
	"testing"
)

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
