package tools

import (
	"strings"
	"testing"
)

func TestValidateTutorialBlueprint(t *testing.T) {
	bp := &TutorialBlueprint{
		Type:  "tutorial_blueprint",
		Suite: "demo",
		Title: "Open Studio",
		Scenes: []TutorialBlueprintScene{
			{ID: "hook", Title: "Hook", Voiceover: "Welcome.", Visual: TutorialBlueprintVisual{Kind: "avatar", Description: "Presenter"}},
			{ID: "click", Title: "Click", Voiceover: "Click Studio.", Visual: TutorialBlueprintVisual{Kind: "screen", Description: "Click link", DrillNode: "open_studio"}},
			{ID: "why", Title: "Why", Voiceover: "Agents live here.", Visual: TutorialBlueprintVisual{Kind: "broll", Description: "Icons montage"}},
		},
	}
	if errs := ValidateTutorialBlueprint(bp); len(errs) != 0 {
		t.Fatalf("expected valid, got %v", errs)
	}

	bad := &TutorialBlueprint{Suite: "x", Title: "y", Scenes: []TutorialBlueprintScene{
		{ID: "a", Voiceover: "hi", Visual: TutorialBlueprintVisual{Kind: "avatar", Description: "x"}},
	}}
	if errs := ValidateTutorialBlueprint(bad); len(errs) == 0 {
		t.Fatal("expected error for missing screen scene")
	}
}

func TestBlueprintToTutorialDrillYAML(t *testing.T) {
	bp := &TutorialBlueprint{
		Suite: "demo",
		Title: "Open Studio",
		Name:  "open_studio_blueprint",
		Scenes: []TutorialBlueprintScene{
			{ID: "hook", Title: "Hook", Voiceover: "Hi there friend.", Visual: TutorialBlueprintVisual{Kind: "avatar", Description: "A-roll"}},
			{ID: "open", Title: "Open", Voiceover: "Click the Studio link now.", DurationHintS: 4,
				Visual: TutorialBlueprintVisual{Kind: "screen", Description: "Click Studio", DrillNode: "open_studio"}},
			{ID: "broll", Title: "B", Voiceover: "Nice icons.", Visual: TutorialBlueprintVisual{Kind: "broll", Description: "montage"}},
		},
	}
	yamlOut, err := BlueprintToTutorialDrillYAML(bp, "open_studio")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"mode: tutorial",
		"open_studio",
		"Click the Studio link now",
		"blueprint: open_studio_blueprint",
		"visual_kind: avatar",
		"visual_kind: broll",
		"visual_kind: screen",
		"visual_description: A-roll",
		"visual_description: montage",
		"Hi there friend",
	} {
		if !strings.Contains(yamlOut, want) {
			t.Fatalf("missing %q in:\n%s", want, yamlOut)
		}
	}
	// Avatar/broll voiceovers live under drill_config.scenes only — not as executable nodes.
	nodesIdx := strings.Index(yamlOut, "\nnodes:")
	if nodesIdx < 0 {
		t.Fatal("missing nodes section")
	}
	nodesSection := yamlOut[nodesIdx:]
	if strings.Count(nodesSection, "browser_run_code") != 1 {
		t.Fatalf("expected exactly one screen node, got:\n%s", nodesSection)
	}
	for _, want := range []string{"open_app", "enter_fullscreen", "browser_navigate", "browser_fullscreen"} {
		if !strings.Contains(nodesSection, want) {
			t.Fatalf("missing warm-up %q in:\n%s", want, nodesSection)
		}
	}
	if strings.Contains(nodesSection, "visual_kind: avatar") {
		t.Fatal("avatar should not be an executable node")
	}
}

func TestEstimateHoldMsFromVoiceover(t *testing.T) {
	if got := EstimateHoldMsFromVoiceover("one two three four five"); got < 2000 {
		t.Fatalf("got %d", got)
	}
}
