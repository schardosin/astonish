package tools

import (
	"path/filepath"
	"testing"

	adrill "github.com/SAP/astonish/pkg/drill"
)

func TestCollectRunDrillArtifactPaths(t *testing.T) {
	report := &adrill.SuiteReport{
		ScenePaths:   []string{"/tmp/clip1.mp4", "recordings/clip2.mp4", ""},
		ManifestPath: "/tmp/reports/scene_manifest.json",
	}
	paths := CollectRunDrillArtifactPaths(report)
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/tmp/clip1.mp4" {
		t.Fatalf("paths[0]=%q", paths[0])
	}
	if !filepath.IsAbs(paths[1]) {
		t.Fatalf("relative clip should be absolutized: %q", paths[1])
	}
	if paths[2] != "/tmp/reports/scene_manifest.json" {
		t.Fatalf("paths[2]=%q", paths[2])
	}

	// Dedupes manifest if also listed in ScenePaths
	report2 := &adrill.SuiteReport{
		ScenePaths:   []string{"/tmp/m.json"},
		ManifestPath: "/tmp/m.json",
	}
	if got := CollectRunDrillArtifactPaths(report2); len(got) != 1 {
		t.Fatalf("expected dedupe, got %v", got)
	}
	if CollectRunDrillArtifactPaths(nil) != nil {
		t.Fatal("nil report should return nil")
	}
}
