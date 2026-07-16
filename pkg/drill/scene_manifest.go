package drill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SAP/astonish/pkg/config"
)

// SceneClip is one beat in a tutorial cut list (screen clip and/or scripted visual).
type SceneClip struct {
	ID                string  `json:"id"`
	Narration         string  `json:"narration,omitempty"`
	Voiceover         string  `json:"voiceover,omitempty"` // alias of narration for blueprint parity
	HoldMs            int     `json:"hold_ms,omitempty"`
	Path              string  `json:"path,omitempty"` // MP4 path for screen scenes; empty for avatar/broll until provider phase
	DurationSeconds   float64 `json:"duration_seconds,omitempty"`
	VisualKind        string  `json:"visual_kind,omitempty"` // avatar | broll | screen
	VisualDescription string  `json:"visual_description,omitempty"`
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

// MergeSceneManifest builds the final cut list from drill_config.scenes, overlaying
// recorded screen clips by drill_node (preferred) or scene id. When cutList is
// empty, returns recorded as-is (hand-authored tutorials without a blueprint).
// Extra recorded IDs not present in cutList are omitted.
func MergeSceneManifest(cutList []config.TutorialSceneSpec, recorded []SceneClip) []SceneClip {
	if len(cutList) == 0 {
		return recorded
	}
	byID := make(map[string]SceneClip, len(recorded))
	for _, clip := range recorded {
		if clip.ID != "" {
			byID[clip.ID] = clip
		}
	}
	out := make([]SceneClip, 0, len(cutList))
	for _, spec := range cutList {
		voice := spec.Voiceover
		if voice == "" {
			voice = spec.Narration
		}
		narr := spec.Narration
		if narr == "" {
			narr = voice
		}
		clip := SceneClip{
			ID:                spec.ID,
			Narration:         narr,
			Voiceover:         voice,
			HoldMs:            spec.HoldMs,
			VisualKind:        spec.VisualKind,
			VisualDescription: spec.VisualDescription,
		}
		rec, ok := byID[spec.DrillNode]
		if !ok {
			rec, ok = byID[spec.ID]
		}
		if ok {
			clip.Path = rec.Path
			clip.DurationSeconds = rec.DurationSeconds
			if clip.HoldMs == 0 {
				clip.HoldMs = rec.HoldMs
			}
			if clip.Narration == "" {
				clip.Narration = rec.Narration
			}
			if clip.Voiceover == "" {
				clip.Voiceover = rec.Voiceover
			}
			if clip.VisualKind == "" {
				clip.VisualKind = rec.VisualKind
			}
			if clip.VisualDescription == "" {
				clip.VisualDescription = rec.VisualDescription
			}
		}
		out = append(out, clip)
	}
	return out
}
