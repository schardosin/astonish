package agent

import (
	"strings"
	"testing"
)

const tutorialModeYAML = `type: drill
suite: demo-tutorial
description: Open Studio
drill_config:
  mode: tutorial
  tags: [tutorial]
nodes:
  - name: open
    type: tool
    args:
      tool: browser_navigate
      url: https://example.com
`

const testModeYAML = `type: drill
suite: demo
description: Health
drill_config:
  tags: [smoke]
nodes:
  - name: s
    type: tool
    args:
      tool: shell_command
      command: echo hi
    assert:
      type: contains
      expected: hi
`

func TestDrillYAMLsContainTutorialMode(t *testing.T) {
	if !DrillYAMLsContainTutorialMode([]string{tutorialModeYAML}) {
		t.Fatal("expected true for mode: tutorial")
	}
	if DrillYAMLsContainTutorialMode([]string{testModeYAML}) {
		t.Fatal("expected false for non-tutorial drill")
	}
	if !DrillYAMLsContainTutorialMode([]string{testModeYAML, tutorialModeYAML}) {
		t.Fatal("expected true when any YAML is tutorial")
	}
	if DrillYAMLsContainTutorialMode([]string{"not: yaml: [[[", ""}) {
		t.Fatal("malformed YAML should not count as tutorial")
	}
	if DrillYAMLsContainTutorialMode(nil) {
		t.Fatal("nil should be false")
	}
}

func TestCheckTutorialDrillToolGate_SaveValidate(t *testing.T) {
	saveArgs := map[string]any{
		"tests": []any{
			map[string]any{"name": "open", "yaml": tutorialModeYAML},
		},
	}
	blocked, result := CheckTutorialDrillToolGate("save_drill", saveArgs, false)
	if !blocked {
		t.Fatal("save_drill with tutorial mode should block without approval")
	}
	if result["status"] != "error" {
		t.Fatalf("status = %v, want error", result["status"])
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "present_tutorial_blueprint") {
		t.Fatalf("message should mention present_tutorial_blueprint: %q", msg)
	}

	blocked, _ = CheckTutorialDrillToolGate("save_drill", saveArgs, true)
	if blocked {
		t.Fatal("save_drill should allow after approval")
	}

	validateArgs := map[string]any{
		"test_yamls": []any{tutorialModeYAML},
	}
	blocked, _ = CheckTutorialDrillToolGate("validate_drill", validateArgs, false)
	if !blocked {
		t.Fatal("validate_drill with tutorial mode should block without approval")
	}
	blocked, _ = CheckTutorialDrillToolGate("validate_drill", validateArgs, true)
	if blocked {
		t.Fatal("validate_drill should allow after approval")
	}

	nonTutorialArgs := map[string]any{
		"tests": []any{
			map[string]any{"name": "health", "yaml": testModeYAML},
		},
	}
	blocked, _ = CheckTutorialDrillToolGate("save_drill", nonTutorialArgs, false)
	if blocked {
		t.Fatal("non-tutorial save_drill should not require approval")
	}
}

func TestCheckTutorialDrillToolGate_BlueprintToTutorial(t *testing.T) {
	blocked, _ := CheckTutorialDrillToolGate("blueprint_to_tutorial_drill", map[string]any{}, false)
	if !blocked {
		t.Fatal("blueprint_to_tutorial_drill should block without approval")
	}
	blocked, _ = CheckTutorialDrillToolGate("blueprint_to_tutorial_drill", map[string]any{}, true)
	if blocked {
		t.Fatal("blueprint_to_tutorial_drill should allow after approval")
	}
	blocked, _ = CheckTutorialDrillToolGate("shell_command", map[string]any{}, false)
	if blocked {
		t.Fatal("unrelated tools must not be gated")
	}
}
