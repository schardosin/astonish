package tools

import (
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/browser"
)

func TestDraftDrillYAMLFromActions(t *testing.T) {
	events := []browser.ActionEvent{
		{Type: "navigate", URL: "http://localhost:3000/"},
		{Type: "click", Selector: `[data-testid="studio-link"]`, Label: "Studio"},
		{Type: "change", Selector: `input[name="q"]`, Value: "hello"},
		{Type: "keydown", Key: "Enter"},
		{Type: "change", Selector: `input[type="password"]`, Value: "***"},
	}
	yamlOut, steps, skipped := DraftDrillYAMLFromActions("demo-suite", "my_flow", "Demo capture", events)
	if steps < 3 {
		t.Fatalf("expected >=3 steps, got %d\n%s", steps, yamlOut)
	}
	for _, banned := range []string{"mode: tutorial", "record:", "narration:", "hold_ms:"} {
		if strings.Contains(yamlOut, banned) {
			t.Fatalf("skeleton must not contain %q\n%s", banned, yamlOut)
		}
	}
	if !strings.Contains(yamlOut, "browser_navigate") {
		t.Fatalf("missing navigate\n%s", yamlOut)
	}
	if !strings.Contains(yamlOut, "browser_run_code") {
		t.Fatalf("missing run_code\n%s", yamlOut)
	}
	if !strings.Contains(skipped, "password") {
		t.Fatalf("expected password skip hint, got %q", skipped)
	}
}
