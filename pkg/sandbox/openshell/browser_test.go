package openshell

import (
	"strings"
	"testing"
)

func TestKasmvncConfigYAML_LocksViewportResolution(t *testing.T) {
	yaml := kasmvncConfigYAML(1920, 1080)
	for _, want := range []string{
		"allow_resize: false",
		"width: 1920",
		"height: 1080",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("kasmvncConfigYAML missing %q", want)
		}
	}
	def := kasmvncConfigYAML(0, 0)
	if strings.Contains(def, "width: 1024") || strings.Contains(def, "height: 768") {
		t.Fatal("must not use KasmVNC package default 1024x768")
	}
}

func TestBuildBrowserLaunchScript_WritesViewportYAMLOverlay(t *testing.T) {
	script := buildBrowserLaunchScript(BrowserLaunchConfig{}, 1920, 1080, defaultKasmVNCPort)
	for _, want := range []string{
		"/tmp/astonish-kasmvnc/.vnc/kasmvnc.yaml",
		"HOME=/tmp/astonish-kasmvnc",
		"allow_resize: false",
		"width: 1920",
		"height: 1080",
		"-geometry 1920x1080",
		"-AcceptSetDesktopSize 0",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("launch script missing %q", want)
		}
	}
}
