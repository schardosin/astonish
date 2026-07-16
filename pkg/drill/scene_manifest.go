package drill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
)

// SceneClip is one recorded beat in a tutorial drill.
type SceneClip struct {
	ID              string  `json:"id"`
	Narration       string  `json:"narration,omitempty"`
	HoldMs          int     `json:"hold_ms,omitempty"`
	Path            string  `json:"path,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// SceneManifest is written to the artifact directory after a tutorial drill run.
type SceneManifest struct {
	Mode   string      `json:"mode"`
	Suite  string      `json:"suite"`
	Drill  string      `json:"drill"`
	Scenes []SceneClip `json:"scenes"`
}

var safeSceneName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// IsTutorialMode reports whether drill_config.mode is tutorial.
func IsTutorialMode(dc *config.DrillConfig) bool {
	return dc != nil && dc.Mode == "tutorial"
}

// SanitizeSceneFilename turns a node name into a safe MP4 basename.
func SanitizeSceneFilename(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		s = "scene"
	}
	s = safeSceneName.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		s = "scene"
	}
	if !strings.HasSuffix(strings.ToLower(s), ".mp4") {
		s += ".mp4"
	}
	return s
}

// WriteSceneManifest writes scene_manifest.json under dir and returns its path.
func WriteSceneManifest(dir string, m SceneManifest) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("artifact dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "scene_manifest.json")
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
