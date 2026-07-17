package agent

import "testing"

func TestTutorialBlueprintApprovedLifecycle(t *testing.T) {
	c := &ChatAgent{}
	const sid = "sess-approve"

	if c.HasTutorialBlueprintApproved(sid) {
		t.Fatal("expected no approval initially")
	}

	c.MarkTutorialBlueprintApproved(sid)
	if !c.HasTutorialBlueprintApproved(sid) {
		t.Fatal("expected approved after Mark")
	}

	// Pending clear must not clear approved.
	c.SetPendingTutorialBlueprint(sid, &TutorialBlueprintPending{YAML: "x", Title: "t", Suite: "s"})
	c.CancelPendingTutorialBlueprint(sid)
	if !c.HasTutorialBlueprintApproved(sid) {
		t.Fatal("CancelPending must not clear approved flag")
	}

	c.ClearTutorialBlueprintApproved(sid)
	if c.HasTutorialBlueprintApproved(sid) {
		t.Fatal("expected cleared after Clear")
	}
}

func TestTutorialDrillGateUsesSessionApproval(t *testing.T) {
	c := &ChatAgent{}
	const sid = "sess-gate"
	args := map[string]any{
		"tests": []any{
			map[string]any{"name": "open", "yaml": tutorialModeYAML},
		},
	}

	blocked, result := CheckTutorialDrillToolGate("save_drill", args, c.HasTutorialBlueprintApproved(sid))
	if !blocked {
		t.Fatal("expected gate to block without approval")
	}
	if result["status"] != "error" {
		t.Fatalf("status = %v", result["status"])
	}

	c.MarkTutorialBlueprintApproved(sid)
	blocked, _ = CheckTutorialDrillToolGate("save_drill", args, c.HasTutorialBlueprintApproved(sid))
	if blocked {
		t.Fatal("expected gate to allow after MarkTutorialBlueprintApproved")
	}
}
