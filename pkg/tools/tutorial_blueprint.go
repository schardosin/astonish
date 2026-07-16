package tools

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// TutorialBlueprint is a HeyGen-style Scene|Voiceover|Visual cut list.
type TutorialBlueprint struct {
	Type     string                   `json:"type" yaml:"type"`
	Suite    string                   `json:"suite" yaml:"suite"`
	Name     string                   `json:"name,omitempty" yaml:"name,omitempty"` // flow/file identity
	Title    string                   `json:"title" yaml:"title"`
	Audience string                   `json:"audience,omitempty" yaml:"audience,omitempty"`
	Tone     string                   `json:"tone,omitempty" yaml:"tone,omitempty"`
	Scenes   []TutorialBlueprintScene `json:"scenes" yaml:"scenes"`
}

// TutorialBlueprintScene is one row in the blueprint table.
type TutorialBlueprintScene struct {
	ID            string                  `json:"id" yaml:"id"`
	Title         string                  `json:"title" yaml:"title"`
	Voiceover     string                  `json:"voiceover" yaml:"voiceover"`
	Visual        TutorialBlueprintVisual `json:"visual" yaml:"visual"`
	DurationHintS int                     `json:"duration_hint_s,omitempty" yaml:"duration_hint_s,omitempty"`
}

// TutorialBlueprintVisual describes how the scene should look on screen.
type TutorialBlueprintVisual struct {
	Kind        string `json:"kind" yaml:"kind"` // avatar | broll | screen
	Description string `json:"description" yaml:"description"`
	Look        string `json:"look,omitempty" yaml:"look,omitempty"`
	DrillNode   string `json:"drill_node,omitempty" yaml:"drill_node,omitempty"`
}

var blueprintIDSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// ValidateTutorialBlueprint checks required fields and visual kinds.
func ValidateTutorialBlueprint(bp *TutorialBlueprint) []string {
	var errs []string
	if bp == nil {
		return []string{"blueprint is nil"}
	}
	if bp.Type != "" && bp.Type != "tutorial_blueprint" {
		errs = append(errs, fmt.Sprintf("type must be tutorial_blueprint, got %q", bp.Type))
	}
	if strings.TrimSpace(bp.Suite) == "" {
		errs = append(errs, "suite is required")
	}
	if strings.TrimSpace(bp.Title) == "" {
		errs = append(errs, "title is required")
	}
	if len(bp.Scenes) == 0 {
		errs = append(errs, "at least one scene is required")
	}
	seen := map[string]bool{}
	screenCount := 0
	for i, sc := range bp.Scenes {
		label := fmt.Sprintf("scenes[%d]", i)
		if sc.ID == "" {
			errs = append(errs, label+": id is required")
		} else if seen[sc.ID] {
			errs = append(errs, label+": duplicate id "+sc.ID)
		} else {
			seen[sc.ID] = true
		}
		if strings.TrimSpace(sc.Voiceover) == "" {
			errs = append(errs, label+": voiceover is required")
		}
		switch sc.Visual.Kind {
		case "avatar", "broll", "screen":
		default:
			errs = append(errs, label+`: visual.kind must be "avatar", "broll", or "screen"`)
		}
		if strings.TrimSpace(sc.Visual.Description) == "" {
			errs = append(errs, label+": visual.description is required")
		}
		if sc.Visual.Kind == "screen" {
			screenCount++
			if strings.TrimSpace(sc.Visual.DrillNode) == "" && strings.TrimSpace(sc.ID) == "" {
				errs = append(errs, label+": screen scenes need drill_node or id")
			}
		}
	}
	if screenCount == 0 && len(bp.Scenes) > 0 {
		errs = append(errs, "at least one scene with visual.kind=screen is required to produce a tutorial drill")
	}
	return errs
}

// EstimateHoldMsFromVoiceover estimates hold_ms from ~150 wpm narration.
func EstimateHoldMsFromVoiceover(voiceover string) int {
	words := 0
	inWord := false
	for _, r := range voiceover {
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			words++
			inWord = true
		}
	}
	ms := words * 400
	if ms < 2000 {
		ms = 2000
	}
	// Round to nearest 100ms
	return ((ms + 50) / 100) * 100
}

// BlueprintToTutorialDrillYAML builds mode:tutorial drill YAML: executable nodes for
// screen scenes only, plus drill_config.scenes with the full ordered cut list
// (avatar / broll / screen) for scene_manifest.json after run_drill.
func BlueprintToTutorialDrillYAML(bp *TutorialBlueprint, drillName string) (string, error) {
	if errs := ValidateTutorialBlueprint(bp); len(errs) > 0 {
		return "", fmt.Errorf("invalid blueprint: %s", strings.Join(errs, "; "))
	}
	if drillName == "" {
		drillName = bp.Name
	}
	if drillName == "" {
		drillName = sanitizeBlueprintName(bp.Title)
	}

	type sceneOut struct {
		ID                string `yaml:"id"`
		Narration         string `yaml:"narration,omitempty"`
		Voiceover         string `yaml:"voiceover,omitempty"`
		HoldMs            int    `yaml:"hold_ms,omitempty"`
		VisualKind        string `yaml:"visual_kind,omitempty"`
		VisualDescription string `yaml:"visual_description,omitempty"`
		DrillNode         string `yaml:"drill_node,omitempty"`
	}
	type nodeOut struct {
		Name      string         `yaml:"name"`
		Narration string         `yaml:"narration,omitempty"`
		HoldMs    int            `yaml:"hold_ms,omitempty"`
		Record    string         `yaml:"record,omitempty"`
		Type      string         `yaml:"type"`
		Args      map[string]any `yaml:"args"`
	}
	var scenes []sceneOut
	var nodes []nodeOut
	var flow []map[string]string
	var prev string

	// Unrecorded warm-up: open the app then fullscreen before any segment.
	// Recording starts before steps with record/narration — keep these first.
	nodes = append(nodes,
		nodeOut{
			Name: "open_app",
			Type: "tool",
			Args: map[string]any{
				"tool": "browser_navigate",
				"url":  "{{SUITE_BASE_URL}}", // agent: replace with suite_config.base_url / home
			},
		},
		nodeOut{
			Name: "enter_fullscreen",
			Type: "tool",
			Args: map[string]any{
				"tool":    "browser_fullscreen",
				"enabled": true,
			},
		},
	)
	flow = append(flow, map[string]string{"from": "open_app", "to": "enter_fullscreen"})
	prev = "enter_fullscreen"

	for _, sc := range bp.Scenes {
		hold := sc.DurationHintS * 1000
		if hold <= 0 {
			hold = EstimateHoldMsFromVoiceover(sc.Voiceover)
		}
		sceneID := sc.ID
		if sceneID == "" {
			sceneID = sanitizeBlueprintName(sc.Title)
		}
		entry := sceneOut{
			ID:                sceneID,
			Narration:         sc.Voiceover,
			Voiceover:         sc.Voiceover,
			HoldMs:            hold,
			VisualKind:        sc.Visual.Kind,
			VisualDescription: sc.Visual.Description,
		}
		if sc.Visual.Kind == "screen" {
			nodeName := sc.Visual.DrillNode
			if nodeName == "" {
				nodeName = sc.ID
			}
			nodeName = sanitizeBlueprintName(nodeName)
			entry.DrillNode = nodeName
			// Placeholder — agent should replace with highlight → animated click path.
			code := fmt.Sprintf(
				`(() => { /* TODO: %q — %s; use browser_highlight + browser_click(animate_cursor:true); prefer nav clicks over navigate */ return 'todo'; })()`,
				nodeName, sc.Visual.Description,
			)
			nodes = append(nodes, nodeOut{
				Name:      nodeName,
				Narration: sc.Voiceover,
				HoldMs:    hold,
				Record:    "segment",
				Type:      "tool",
				Args: map[string]any{
					"tool": "browser_run_code",
					"code": code,
				},
			})
			if prev != "" {
				flow = append(flow, map[string]string{"from": prev, "to": nodeName})
			}
			prev = nodeName
		}
		scenes = append(scenes, entry)
	}
	if len(nodes) <= 2 {
		return "", fmt.Errorf("no screen scenes to convert")
	}

	doc := map[string]any{
		"description": bp.Title,
		"type":        "drill",
		"suite":       bp.Suite,
		"drill_config": map[string]any{
			"mode":         "tutorial",
			"tags":         []string{"tutorial"},
			"timeout":      300,
			"step_timeout": 60,
			"blueprint":    blueprintRefName(bp, drillName),
			"scenes":       scenes,
		},
		"nodes": nodes,
	}
	if len(flow) > 0 {
		doc["flow"] = flow
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# Generated from tutorial blueprint %q — warm-up (open_app, enter_fullscreen) is unrecorded; replace screen TODOs with highlight + animated clicks.\n", bp.Title)
	return header + string(out), nil
}

func blueprintRefName(bp *TutorialBlueprint, drillName string) string {
	if bp.Name != "" {
		return bp.Name
	}
	return drillName + "_blueprint"
}

func sanitizeBlueprintName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = blueprintIDSanitizer.ReplaceAllString(strings.ReplaceAll(s, " ", "_"), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "scene"
	}
	if s[0] >= '0' && s[0] <= '9' {
		return "s_" + s
	}
	return s
}

// MarshalTutorialBlueprintYAML returns the blueprint as YAML with type set.
func MarshalTutorialBlueprintYAML(bp *TutorialBlueprint) (string, error) {
	cp := *bp
	cp.Type = "tutorial_blueprint"
	if cp.Name == "" {
		cp.Name = sanitizeBlueprintName(cp.Title) + "_blueprint"
	}
	data, err := yaml.Marshal(&cp)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseTutorialBlueprintYAML parses a blueprint from YAML or JSON-ish YAML.
func ParseTutorialBlueprintYAML(raw string) (*TutorialBlueprint, error) {
	var bp TutorialBlueprint
	if err := yaml.Unmarshal([]byte(raw), &bp); err != nil {
		return nil, err
	}
	if bp.Type == "" {
		bp.Type = "tutorial_blueprint"
	}
	return &bp, nil
}
