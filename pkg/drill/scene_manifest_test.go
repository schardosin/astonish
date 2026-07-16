package drill

import "testing"

func TestSanitizeSceneFilename(t *testing.T) {
	got := SanitizeSceneFilename("Open Studio!")
	if got != "Open_Studio.mp4" {
		t.Fatalf("got %q", got)
	}
	if SanitizeSceneFilename("") != "scene.mp4" {
		t.Fatal("empty should become scene.mp4")
	}
}

func TestIsTutorialMode(t *testing.T) {
	if IsTutorialMode(nil) {
		t.Fatal("nil should be false")
	}
}
