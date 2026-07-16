package tools

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"gopkg.in/yaml.v3"
)

// --- draft_tutorial_blueprint ---

type DraftTutorialBlueprintArgs struct {
	Suite         string `json:"suite" jsonschema:"required,Suite name for the tutorial."`
	Title         string `json:"title" jsonschema:"required,Video title."`
	Audience      string `json:"audience,omitempty" jsonschema:"Target audience."`
	Tone          string `json:"tone,omitempty" jsonschema:"Narration tone."`
	BlueprintYAML string `json:"blueprint_yaml,omitempty" jsonschema:"Optional full blueprint YAML to normalize. If empty, scenes_yaml is required."`
	ScenesYAML    string `json:"scenes_yaml,omitempty" jsonschema:"YAML list of scenes when not passing full blueprint_yaml."`
	Name          string `json:"name,omitempty" jsonschema:"Optional blueprint identity name."`
}

type DraftTutorialBlueprintResult struct {
	Status        string `json:"status"`
	BlueprintYAML string `json:"blueprint_yaml"`
	Message       string `json:"message"`
	SceneCount    int    `json:"scene_count"`
	ScreenCount   int    `json:"screen_count"`
}

func draftTutorialBlueprint(_ tool.Context, args DraftTutorialBlueprintArgs) (DraftTutorialBlueprintResult, error) {
	var bp *TutorialBlueprint
	var err error
	if strings.TrimSpace(args.BlueprintYAML) != "" {
		bp, err = ParseTutorialBlueprintYAML(args.BlueprintYAML)
		if err != nil {
			return DraftTutorialBlueprintResult{}, fmt.Errorf("parse blueprint_yaml: %w", err)
		}
	} else if strings.TrimSpace(args.ScenesYAML) != "" {
		var scenes []TutorialBlueprintScene
		if err := yaml.Unmarshal([]byte(args.ScenesYAML), &scenes); err != nil {
			return DraftTutorialBlueprintResult{}, fmt.Errorf("parse scenes_yaml: %w", err)
		}
		bp = &TutorialBlueprint{Scenes: scenes}
	} else {
		return DraftTutorialBlueprintResult{}, fmt.Errorf("blueprint_yaml or scenes_yaml is required")
	}
	bp.Type = "tutorial_blueprint"
	bp.Suite = args.Suite
	bp.Title = args.Title
	if args.Audience != "" {
		bp.Audience = args.Audience
	}
	if args.Tone != "" {
		bp.Tone = args.Tone
	}
	if args.Name != "" {
		bp.Name = args.Name
	}
	for i := range bp.Scenes {
		if bp.Scenes[i].ID == "" {
			bp.Scenes[i].ID = sanitizeBlueprintName(bp.Scenes[i].Title)
			if bp.Scenes[i].ID == "scene" {
				bp.Scenes[i].ID = fmt.Sprintf("scene_%d", i+1)
			}
		}
		if bp.Scenes[i].Visual.Kind == "screen" && bp.Scenes[i].Visual.DrillNode == "" {
			bp.Scenes[i].Visual.DrillNode = bp.Scenes[i].ID
		}
		if bp.Scenes[i].DurationHintS <= 0 && bp.Scenes[i].Voiceover != "" {
			bp.Scenes[i].DurationHintS = EstimateHoldMsFromVoiceover(bp.Scenes[i].Voiceover) / 1000
			if bp.Scenes[i].DurationHintS < 2 {
				bp.Scenes[i].DurationHintS = 2
			}
		}
	}
	out, err := MarshalTutorialBlueprintYAML(bp)
	if err != nil {
		return DraftTutorialBlueprintResult{}, err
	}
	screen := 0
	for _, sc := range bp.Scenes {
		if sc.Visual.Kind == "screen" {
			screen++
		}
	}
	return DraftTutorialBlueprintResult{
		Status:        "ok",
		BlueprintYAML: out,
		Message:       "Draft blueprint ready. Call validate_tutorial_blueprint then present_tutorial_blueprint.",
		SceneCount:    len(bp.Scenes),
		ScreenCount:   screen,
	}, nil
}

// --- validate_tutorial_blueprint ---

type ValidateTutorialBlueprintArgs struct {
	BlueprintYAML string `json:"blueprint_yaml" jsonschema:"required,Blueprint YAML to validate."`
}

type ValidateTutorialBlueprintResult struct {
	Status  string   `json:"status"`
	Valid   bool     `json:"valid"`
	Errors  []string `json:"errors,omitempty"`
	Message string   `json:"message"`
}

func validateTutorialBlueprint(_ tool.Context, args ValidateTutorialBlueprintArgs) (ValidateTutorialBlueprintResult, error) {
	bp, err := ParseTutorialBlueprintYAML(args.BlueprintYAML)
	if err != nil {
		return ValidateTutorialBlueprintResult{
			Status:  "error",
			Valid:   false,
			Errors:  []string{err.Error()},
			Message: "YAML parse failed",
		}, nil
	}
	errs := ValidateTutorialBlueprint(bp)
	if len(errs) > 0 {
		return ValidateTutorialBlueprintResult{
			Status:  "ok",
			Valid:   false,
			Errors:  errs,
			Message: fmt.Sprintf("%d validation error(s)", len(errs)),
		}, nil
	}
	return ValidateTutorialBlueprintResult{
		Status:  "ok",
		Valid:   true,
		Message: fmt.Sprintf("Blueprint valid (%d scenes)", len(bp.Scenes)),
	}, nil
}

// --- present_tutorial_blueprint ---

type PresentTutorialBlueprintArgs struct {
	BlueprintYAML string `json:"blueprint_yaml" jsonschema:"required,Validated blueprint YAML to show in the chat approval card."`
}

type PresentTutorialBlueprintResult struct {
	Status        string                   `json:"status"`
	Message       string                   `json:"message"`
	Title         string                   `json:"title"`
	Suite         string                   `json:"suite"`
	BlueprintYAML string                   `json:"blueprint_yaml"`
	Scenes        []TutorialBlueprintScene `json:"scenes"`
	Present       bool                     `json:"present_tutorial_blueprint"` // signal for chat_runner SSE
}

func presentTutorialBlueprint(_ tool.Context, args PresentTutorialBlueprintArgs) (PresentTutorialBlueprintResult, error) {
	bp, err := ParseTutorialBlueprintYAML(args.BlueprintYAML)
	if err != nil {
		return PresentTutorialBlueprintResult{}, fmt.Errorf("parse blueprint: %w", err)
	}
	if errs := ValidateTutorialBlueprint(bp); len(errs) > 0 {
		return PresentTutorialBlueprintResult{
			Status:  "error",
			Message: "Blueprint invalid: " + strings.Join(errs, "; "),
		}, nil
	}
	out, err := MarshalTutorialBlueprintYAML(bp)
	if err != nil {
		return PresentTutorialBlueprintResult{}, err
	}
	return PresentTutorialBlueprintResult{
		Status:        "awaiting_approval",
		Message:       "Blueprint presented for creator approval. The turn ends here — wait for Approve & generate, Request changes, or Cancel. Do not call blueprint_to_tutorial_drill or run_drill until the creator approves.",
		Title:         bp.Title,
		Suite:         bp.Suite,
		BlueprintYAML: out,
		Scenes:        bp.Scenes,
		Present:       true,
	}, nil
}

// --- blueprint_to_tutorial_drill ---

type BlueprintToTutorialDrillArgs struct {
	BlueprintYAML string `json:"blueprint_yaml" jsonschema:"required,Approved blueprint YAML."`
	DrillName     string `json:"drill_name,omitempty" jsonschema:"Drill file name (without .yaml)."`
}

type BlueprintToTutorialDrillResult struct {
	Status        string `json:"status"`
	DrillYAML     string `json:"drill_yaml"`
	BlueprintYAML string `json:"blueprint_yaml"`
	DrillName     string `json:"drill_name"`
	BlueprintName string `json:"blueprint_name"`
	ScreenCount   int    `json:"screen_count"`
	Message       string `json:"message"`
}

func blueprintToTutorialDrill(_ tool.Context, args BlueprintToTutorialDrillArgs) (BlueprintToTutorialDrillResult, error) {
	return BlueprintToTutorialDrillFromYAML(args.BlueprintYAML, args.DrillName)
}

// BlueprintToTutorialDrillFromYAML converts blueprint YAML to drill YAML (shared with API approve path).
func BlueprintToTutorialDrillFromYAML(blueprintYAML, drillName string) (BlueprintToTutorialDrillResult, error) {
	bp, err := ParseTutorialBlueprintYAML(blueprintYAML)
	if err != nil {
		return BlueprintToTutorialDrillResult{}, fmt.Errorf("parse blueprint: %w", err)
	}
	drillYAML, err := BlueprintToTutorialDrillYAML(bp, drillName)
	if err != nil {
		return BlueprintToTutorialDrillResult{}, err
	}
	bpYAML, err := MarshalTutorialBlueprintYAML(bp)
	if err != nil {
		return BlueprintToTutorialDrillResult{}, err
	}
	if drillName == "" {
		drillName = sanitizeBlueprintName(bp.Title)
	}
	screen := 0
	for _, sc := range bp.Scenes {
		if sc.Visual.Kind == "screen" {
			screen++
		}
	}
	return BlueprintToTutorialDrillResult{
		Status:        "ok",
		DrillYAML:     drillYAML,
		BlueprintYAML: bpYAML,
		DrillName:     drillName,
		BlueprintName: blueprintRefName(bp, drillName),
		ScreenCount:   screen,
		Message: "Screen scenes converted to executable nodes; full cut list (avatar/broll/screen) " +
			"is under drill_config.scenes for scene_manifest.json. Replace TODO browser_run_code " +
			"with real UI actions, then validate_drill and save_drill.",
	}, nil
}

// GetTutorialBlueprintTools returns blueprint authoring tools.
func GetTutorialBlueprintTools() ([]tool.Tool, error) {
	draftTool, err := functiontool.New(functiontool.Config{
		Name: "draft_tutorial_blueprint",
		Description: "Normalize a HeyGen-style tutorial video blueprint (Scene|Voiceover|Visual) from " +
			"interview notes / scenes YAML. Call validate_tutorial_blueprint next, then present_tutorial_blueprint.",
	}, draftTutorialBlueprint)
	if err != nil {
		return nil, err
	}
	validateTool, err := functiontool.New(functiontool.Config{
		Name:        "validate_tutorial_blueprint",
		Description: "Validate a tutorial_blueprint YAML (voiceover, visual.kind avatar|broll|screen, at least one screen scene).",
	}, validateTutorialBlueprint)
	if err != nil {
		return nil, err
	}
	presentTool, err := functiontool.New(functiontool.Config{
		Name: "present_tutorial_blueprint",
		Description: "Show the blueprint in an in-chat Scene|Voiceover|Visual approval card. " +
			"Call only after validate_tutorial_blueprint passes. The agent turn ends after this tool; " +
			"do not call further tools until the creator Approves, Requests changes, or Cancels.",
	}, presentTutorialBlueprint)
	if err != nil {
		return nil, err
	}
	convertTool, err := functiontool.New(functiontool.Config{
		Name: "blueprint_to_tutorial_drill",
		Description: "After blueprint approval, convert screen scenes into mode:tutorial drill YAML. " +
			"Avatar/broll scenes are kept in the blueprint only. Then refine selectors, validate_drill, save_drill.",
	}, blueprintToTutorialDrill)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{draftTool, validateTool, presentTool, convertTool}, nil
}
