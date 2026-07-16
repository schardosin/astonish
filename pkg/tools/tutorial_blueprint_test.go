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
	for _, want := range []string{"mode: tutorial", "open_studio", "Click the Studio link now", "blueprint: open_studio_blueprint"} {
		if !strings.Contains(yamlOut, want) {
			t.Fatalf("missing %q in:\n%s", want, yamlOut)
		}
	}
	if strings.Contains(yamlOut, "Hi there friend") {
		t.Fatal("avatar voiceover should not become a drill node")
	}
}

func TestEstimateHoldMsFromVoiceover(t *testing.T) {
	if got := EstimateHoldMsFromVoiceover("one two three four five"); got < 2000 {
		t.Fatalf("got %d", got)
	}
}
