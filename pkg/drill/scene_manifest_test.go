package drill

import (
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestMergeSceneManifest_EmptyCutListUsesRecorded(t *testing.T) {
	recorded := []SceneClip{
		{ID: "a", Path: "/tmp/a.mp4", DurationSeconds: 1.5, VisualKind: "screen"},
	}
	got := MergeSceneManifest(nil, recorded)
	if len(got) != 1 || got[0].Path != "/tmp/a.mp4" {
		t.Fatalf("got %#v", got)
	}
}

func TestMergeSceneManifest_FullCutListOverlay(t *testing.T) {
	cut := []config.TutorialSceneSpec{
		{ID: "hook", Voiceover: "Welcome.", VisualKind: "avatar", VisualDescription: "Presenter", HoldMs: 3000},
		{ID: "open", Voiceover: "Click Studio.", Narration: "Click Studio.", VisualKind: "screen",
			VisualDescription: "Highlight link", HoldMs: 4000, DrillNode: "open_studio"},
		{ID: "outro", Voiceover: "Thanks.", VisualKind: "broll", VisualDescription: "Logo montage", HoldMs: 2000},
	}
	recorded := []SceneClip{
		{ID: "open_studio", Path: "/tmp/open_studio.mp4", DurationSeconds: 4.2, VisualKind: "screen",
			Narration: "Click Studio.", HoldMs: 4000, VisualDescription: "Click Studio."},
		{ID: "orphan", Path: "/tmp/orphan.mp4", DurationSeconds: 1}, // omitted
	}
	got := MergeSceneManifest(cut, recorded)
	if len(got) != 3 {
		t.Fatalf("want 3 scenes, got %d: %#v", len(got), got)
	}
	if got[0].ID != "hook" || got[0].Path != "" || got[0].VisualKind != "avatar" || got[0].VisualDescription != "Presenter" {
		t.Fatalf("avatar row: %#v", got[0])
	}
	if got[1].ID != "open" || got[1].Path != "/tmp/open_studio.mp4" || got[1].DurationSeconds != 4.2 {
		t.Fatalf("screen row: %#v", got[1])
	}
	if got[1].VisualDescription != "Highlight link" {
		t.Fatalf("screen visual_description should come from cut list, got %q", got[1].VisualDescription)
	}
	if got[2].ID != "outro" || got[2].Path != "" || got[2].VisualKind != "broll" {
		t.Fatalf("broll row: %#v", got[2])
	}
}

func TestMergeSceneManifest_MatchBySceneID(t *testing.T) {
	cut := []config.TutorialSceneSpec{
		{ID: "dash", Voiceover: "Dashboard.", VisualKind: "screen", VisualDescription: "Net liq"},
	}
	recorded := []SceneClip{
		{ID: "dash", Path: "/tmp/dash.mp4", DurationSeconds: 9},
	}
	got := MergeSceneManifest(cut, recorded)
	if len(got) != 1 || got[0].Path != "/tmp/dash.mp4" {
		t.Fatalf("got %#v", got)
	}
}
