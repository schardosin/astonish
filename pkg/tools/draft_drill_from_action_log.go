package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
	"gopkg.in/yaml.v3"
)

// DraftDrillFromActionLogArgs converts a captured action log into draft tutorial YAML.
type DraftDrillFromActionLogArgs struct {
	Suite       string `json:"suite" jsonschema:"required,Suite name for the draft drill."`
	Name        string `json:"name,omitempty" jsonschema:"Drill file/base name. Default: tutorial_from_capture."`
	Description string `json:"description,omitempty" jsonschema:"Human-readable description."`
	ActionsJSON string `json:"actions_json" jsonschema:"required,JSON array from browser_get_action_log (events or .json field)."`
}

// DraftDrillFromActionLogResult is the drafted tutorial drill YAML.
type DraftDrillFromActionLogResult struct {
	Status      string `json:"status"`
	YAML        string `json:"yaml"`
	StepCount   int    `json:"step_count"`
	Message     string `json:"message"`
	SkippedHint string `json:"skipped_hint,omitempty"`
}

func draftDrillFromActionLog(_ tool.Context, args DraftDrillFromActionLogArgs) (DraftDrillFromActionLogResult, error) {
	if args.Suite == "" {
		return DraftDrillFromActionLogResult{}, fmt.Errorf("suite is required")
	}
	if strings.TrimSpace(args.ActionsJSON) == "" {
		return DraftDrillFromActionLogResult{}, fmt.Errorf("actions_json is required")
	}
	events, err := browser.ParseActionLogJSON(args.ActionsJSON)
	if err != nil {
		// Allow wrapping {events:[...]} from tool result
		var wrap struct {
			Events []browser.ActionEvent `json:"events"`
		}
		if err2 := json.Unmarshal([]byte(args.ActionsJSON), &wrap); err2 != nil || len(wrap.Events) == 0 {
			return DraftDrillFromActionLogResult{}, fmt.Errorf("parse actions_json: %w", err)
		}
		events = wrap.Events
	}
	name := args.Name
	if name == "" {
		name = "tutorial_from_capture"
	}
	desc := args.Description
	if desc == "" {
		desc = "Tutorial draft from browser action capture (fill narration/hold_ms)"
	}

	yamlOut, steps, skipped := DraftTutorialYAMLFromActions(args.Suite, name, desc, events)
	msg := fmt.Sprintf("Drafted %d tutorial steps. Fill narration and hold_ms, then validate_drill / save_drill.", steps)
	return DraftDrillFromActionLogResult{
		Status:      "ok",
		YAML:        yamlOut,
		StepCount:   steps,
		Message:     msg,
		SkippedHint: skipped,
	}, nil
}

// DraftTutorialYAMLFromActions maps capture events to a tutorial drill YAML document.
func DraftTutorialYAMLFromActions(suite, name, description string, events []browser.ActionEvent) (yamlOut string, stepCount int, skippedHint string) {
	type nodeDraft struct {
		Name      string
		Narration string
		HoldMs    int
		Record    string
		Tool      string
		Args      map[string]any
	}
	var nodes []nodeDraft
	var skipped []string
	lastURL := ""

	for i, ev := range events {
		switch ev.Type {
		case "navigate":
			url := ev.URL
			if url == "" || url == lastURL {
				continue
			}
			lastURL = url
			nodes = append(nodes, nodeDraft{
				Name:      fmt.Sprintf("navigate_%d", len(nodes)+1),
				Narration: "",
				HoldMs:    2000,
				Record:    "segment",
				Tool:      "browser_navigate",
				Args:      map[string]any{"tool": "browser_navigate", "url": url},
			})
		case "click":
			sel := browser.PreferStableSelector(ev.Selector)
			if sel == "" {
				skipped = append(skipped, fmt.Sprintf("click[%d]: no selector", i))
				continue
			}
			js := fmt.Sprintf(`(() => { const el = document.querySelector(%q); if (!el) return 'missing'; el.click(); return 'clicked'; })()`, sel)
			nodes = append(nodes, nodeDraft{
				Name:      sanitizeNodeName(ev.Label, fmt.Sprintf("click_%d", len(nodes)+1)),
				Narration: "",
				HoldMs:    3000,
				Record:    "segment",
				Tool:      "browser_run_code",
				Args:      map[string]any{"tool": "browser_run_code", "code": js},
			})
		case "change":
			sel := browser.PreferStableSelector(ev.Selector)
			if sel == "" {
				skipped = append(skipped, fmt.Sprintf("change[%d]: no selector", i))
				continue
			}
			val := ev.Value
			if val == "***" {
				skipped = append(skipped, fmt.Sprintf("change[%d]: password field skipped (fill manually)", i))
				continue
			}
			js := fmt.Sprintf(
				`(() => { const el = document.querySelector(%q); if (!el) return 'missing'; el.focus(); el.value = %q; el.dispatchEvent(new Event('input', {bubbles:true})); el.dispatchEvent(new Event('change', {bubbles:true})); return 'typed'; })()`,
				sel, val,
			)
			nodes = append(nodes, nodeDraft{
				Name:      sanitizeNodeName(ev.Label, fmt.Sprintf("type_%d", len(nodes)+1)),
				Narration: "",
				HoldMs:    2500,
				Record:    "segment",
				Tool:      "browser_run_code",
				Args:      map[string]any{"tool": "browser_run_code", "code": js},
			})
		case "keydown":
			if ev.Key != "Enter" {
				continue
			}
			nodes = append(nodes, nodeDraft{
				Name:      fmt.Sprintf("press_enter_%d", len(nodes)+1),
				Narration: "",
				HoldMs:    2000,
				Record:    "segment",
				Tool:      "browser_press_key",
				Args:      map[string]any{"tool": "browser_press_key", "key": "Enter"},
			})
		default:
			skipped = append(skipped, fmt.Sprintf("%s[%d]: unsupported", ev.Type, i))
		}
	}

	// Build YAML manually for stable ordering / comments via description.
	doc := map[string]any{
		"description": description,
		"type":        "drill",
		"suite":       suite,
		"drill_config": map[string]any{
			"mode":         "tutorial",
			"tags":         []string{"tutorial"},
			"timeout":      300,
			"step_timeout": 60,
		},
	}
	nodeMaps := make([]map[string]any, 0, len(nodes))
	flow := make([]map[string]string, 0)
	for i, n := range nodes {
		nm := map[string]any{
			"name":      n.Name,
			"narration": n.Narration,
			"hold_ms":   n.HoldMs,
			"record":    n.Record,
			"type":      "tool",
			"args":      n.Args,
		}
		nodeMaps = append(nodeMaps, nm)
		if i+1 < len(nodes) {
			flow = append(flow, map[string]string{"from": n.Name, "to": nodes[i+1].Name})
		}
	}
	doc["nodes"] = nodeMaps
	if len(flow) > 0 {
		doc["flow"] = flow
	}
	_ = name // name is the drill identity when saving; YAML description carries intent

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Sprintf("# marshal error: %v\n", err), 0, ""
	}
	hint := ""
	if len(skipped) > 0 {
		hint = strings.Join(skipped, "; ")
	}
	header := "# Draft from action capture — fill narration text before save.\n"
	header += fmt.Sprintf("# Suggested drill name when saving: %s\n", name)
	return header + string(out), len(nodes), hint
}

var nonNodeName = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeNodeName(label, fallback string) string {
	s := strings.TrimSpace(strings.ToLower(label))
	if s == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteByte('_')
		}
	}
	out := nonNodeName.ReplaceAllString(b.String(), "_")
	out = strings.Trim(out, "_")
	if out == "" || len(out) > 40 {
		return fallback
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "step_" + out
	}
	return out
}
