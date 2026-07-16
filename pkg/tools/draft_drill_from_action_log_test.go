package tools

import (
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/browser"
)

func TestDraftTutorialYAMLFromActions(t *testing.T) {
	events := []browser.ActionEvent{
		{Type: "navigate", URL: "http://localhost:3000/"},
		{Type: "click", Selector: `[data-testid="studio-link"]`, Label: "Studio"},
		{Type: "change", Selector: `input[name="q"]`, Value: "hello"},
		{Type: "keydown", Key: "Enter"},
		{Type: "change", Selector: `input[type="password"]`, Value: "***"},
	}
	yamlOut, steps, skipped := DraftTutorialYAMLFromActions("demo-suite", "my_tutorial", "Demo tutorial", events)
	if steps < 3 {
		t.Fatalf("expected >=3 steps, got %d\n%s", steps, yamlOut)
	}
	if !strings.Contains(yamlOut, "mode: tutorial") {
		t.Fatalf("missing mode: tutorial\n%s", yamlOut)
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
