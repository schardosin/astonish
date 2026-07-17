package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SAP/astonish/pkg/drill"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestTryParseTutorialSceneSlideshowMessage(t *testing.T) {
	payload := map[string]any{
		"title":         "JuicyTrade overview",
		"suite":         "juicytrade-tutorial",
		"drill":         "juicytrade-overview",
		"manifest_path": "/tmp/scene_manifest.json",
		"scenes": []any{
			map[string]any{
				"id": "intro", "voiceover": "Welcome.", "visual_kind": "avatar",
			},
		},
	}
	data, _ := json.Marshal(payload)
	text := tutorialSceneSlideshowPrefix + string(data)

	msg := tryParseTutorialSceneSlideshowMessage(text)
	if msg == nil {
		t.Fatal("expected parsed slideshow message")
	}
	if msg.Type != "tutorial_scene_slideshow" {
		t.Fatalf("type = %q", msg.Type)
	}
	if msg.TutorialTitle != "JuicyTrade overview" || msg.TutorialSuite != "juicytrade-tutorial" {
		t.Fatalf("title/suite = %q / %q", msg.TutorialTitle, msg.TutorialSuite)
	}
	if msg.ManifestPath != "/tmp/scene_manifest.json" {
		t.Fatalf("manifest = %q", msg.ManifestPath)
	}
}

func TestBuildTutorialSceneSlideshowPayload(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "scene_manifest.json")
	manifest := drill.SceneManifest{
		Mode:  "tutorial",
		Suite: "demo",
		Drill: "open_studio",
		Scenes: []drill.SceneClip{
			{ID: "hook", Voiceover: "Hi.", VisualKind: "avatar", VisualDescription: "Presenter"},
			{ID: "click", Voiceover: "Click.", VisualKind: "screen", Path: "/tmp/click.mp4", DurationSeconds: 4.2},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	payload, err := buildTutorialSceneSlideshowPayload(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if payload["suite"] != "demo" || payload["drill"] != "open_studio" {
		t.Fatalf("payload = %#v", payload)
	}
	raw, ok := payload["scenes"].([]map[string]any)
	if !ok {
		t.Fatalf("scenes type = %T", payload["scenes"])
	}
	if len(raw) != 2 {
		t.Fatalf("want 2 scenes, got %d", len(raw))
	}
}

func TestInjectTutorialSceneSlideshows(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "scene_manifest.json")
	manifest := drill.SceneManifest{
		Mode:  "tutorial",
		Suite: "demo",
		Drill: "demo_drill",
		Scenes: []drill.SceneClip{
			{ID: "s1", Voiceover: "One.", VisualKind: "screen", Path: "/tmp/s1.mp4"},
		},
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	messages := []StudioMessage{
		{Type: "tool_result", ToolName: "run_drill", ToolResult: map[string]any{"status": "passed"}},
	}
	events := runDrillEventsWithManifest(manifestPath)

	out := injectTutorialSceneSlideshows(messages, events)
	if len(out) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out))
	}
	if out[1].Type != "tutorial_scene_slideshow" || out[1].ManifestPath != manifestPath {
		t.Fatalf("injected = %#v", out[1])
	}

	messagesWithMarker := []StudioMessage{
		{Type: "tool_result", ToolName: "run_drill"},
		{Type: "tutorial_scene_slideshow", ManifestPath: manifestPath},
	}
	out2 := injectTutorialSceneSlideshows(messagesWithMarker, events)
	if len(out2) != 2 {
		t.Fatalf("expected no duplicate, got %d messages", len(out2))
	}
}

func runDrillEventsWithManifest(manifestPath string) testEvents {
	return testEvents{
		{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Role: "user",
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{
							Name: "run_drill",
							ID:   "rd1",
							Response: map[string]any{
								"status":        "passed",
								"manifest_path": manifestPath,
							},
						}},
					},
				},
			},
		},
	}
}
