package api

import (
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestCollectArtifacts_RunDrillArtifactPaths(t *testing.T) {
	events := testEvents{
		{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "run_drill", ID: "rd1", Args: map[string]any{"suite_name": "demo"}}},
					},
				},
			},
		},
		{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Role: "user",
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{
							Name: "run_drill",
							ID:   "rd1",
							Response: map[string]any{
								"status":  "passed",
								"summary": "1/1 tests passed",
								"artifact_paths": []any{
									"/tmp/astonish-recordings/open_studio.mp4",
									"/home/user/.config/astonish/reports/demo/scene_manifest.json",
								},
								"manifest_path": "/home/user/.config/astonish/reports/demo/scene_manifest.json",
							},
						}},
					},
				},
			},
		},
	}

	arts := collectArtifacts(events)
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(arts), arts)
	}
	if arts[0].FileType != "Video" || arts[0].ToolName != "run_drill" {
		t.Fatalf("first artifact: %+v", arts[0])
	}
	if arts[1].FileName != "scene_manifest.json" || arts[1].ToolName != "run_drill" {
		t.Fatalf("second artifact: %+v", arts[1])
	}
}

func TestCollectArtifacts_RunDrillSkipsOnError(t *testing.T) {
	events := testEvents{
		{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "run_drill", ID: "rd2"}},
					},
				},
			},
		},
		{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{
							ID: "rd2",
							Response: map[string]any{
								"error":          "suite failed",
								"artifact_paths": []any{"/tmp/x.mp4"},
							},
						}},
					},
				},
			},
		},
	}
	if arts := collectArtifacts(events); len(arts) != 0 {
		t.Fatalf("expected no artifacts on error, got %+v", arts)
	}
}

func TestExtractArtifactPathsFromRunDrillResponse(t *testing.T) {
	paths := extractArtifactPathsFromRunDrillResponse(map[string]any{
		"artifact_paths": []string{"/a.mp4"},
		"manifest_path":  "/m.json",
	})
	if len(paths) != 2 {
		t.Fatalf("got %v", paths)
	}
}
