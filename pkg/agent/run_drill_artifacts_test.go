package agent

import (
	"testing"
)

func TestExtractRunDrillArtifactPaths(t *testing.T) {
	paths := extractRunDrillArtifactPaths(map[string]any{
		"artifact_paths": []any{"/tmp/a.mp4", "/tmp/b.mp4"},
		"manifest_path":  "/tmp/scene_manifest.json",
	})
	if len(paths) != 3 {
		t.Fatalf("got %v", paths)
	}

	// Dedupes when manifest also in artifact_paths
	paths = extractRunDrillArtifactPaths(map[string]any{
		"artifact_paths": []string{"/tmp/m.json"},
		"manifest_path":  "/tmp/m.json",
	})
	if len(paths) != 1 || paths[0] != "/tmp/m.json" {
		t.Fatalf("got %v", paths)
	}

	var captured []string
	captureRunDrillArtifacts(func(path, toolName string) {
		if toolName != "run_drill" {
			t.Fatalf("toolName=%q", toolName)
		}
		captured = append(captured, path)
	}, map[string]any{"artifact_paths": []string{"/abs/clip.mp4"}})
	if len(captured) != 1 || captured[0] != "/abs/clip.mp4" {
		t.Fatalf("captured=%v", captured)
	}
}
