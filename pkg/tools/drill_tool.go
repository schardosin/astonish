package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"gopkg.in/yaml.v3"
)

// getEffectiveFlowStore checks the tool context for a platform-mode FlowStore.
// Returns nil in personal mode, meaning the caller should fall back to filesystem.
func getEffectiveFlowStore(ctx context.Context) store.FlowStore {
	if ctx != nil {
		if fs := store.FlowStoreFromContext(ctx); fs != nil {
			return fs
		}
	}
	return nil
}

// getDrillFlowStore returns the team-scoped FlowStore for drill operations.
// Drills are always team-scoped artifacts (platform mode required).
// Falls back to the general FlowStore for backward compatibility with fleet
// sessions where the team store IS the primary flow store.
func getDrillFlowStore(ctx context.Context) store.FlowStore {
	if ctx != nil {
		if fs := store.TeamFlowStoreFromContext(ctx); fs != nil {
			return fs
		}
	}
	// Fallback: fleet sessions inject team store directly as the primary flow store
	return getEffectiveFlowStore(ctx)
}

// getDrillReportStore returns the team-scoped DrillReportStore from context
// (platform mode), or nil if not available (personal mode).
func getDrillReportStore(ctx context.Context) store.DrillReportStore {
	if ctx != nil {
		return store.DrillReportStoreFromContext(ctx)
	}
	return nil
}

// SaveDrillArgs are the arguments for the save_drill tool.
type SaveDrillArgs struct {
	SuiteName string         `json:"suite_name" jsonschema:"Filename for the suite (without .yaml extension, e.g., 'myapp'). This name is what drills reference in their 'suite' field."`
	SuiteYAML string         `json:"suite_yaml" jsonschema:"Full YAML content for the drill suite file. Must have type: drill_suite and a suite_config section."`
	Tests     []DrillFileArg `json:"tests" jsonschema:"Array of drill files to save. Each drill must reference this suite by name."`
	Template  string         `json:"template,omitempty" jsonschema:"Optional sandbox template name (from save_sandbox_template). Stored for future container integration."`
}

// DrillFileArg represents a single drill file to save.
type DrillFileArg struct {
	Name string `json:"name" jsonschema:"Filename for the drill (without .yaml extension, e.g., 'test_api_health')"`
	YAML string `json:"yaml" jsonschema:"Full YAML content for the drill file. Must have type: drill and suite: <suite_name>."`
}

// SaveDrillResult is the result of the save_drill tool.
type SaveDrillResult struct {
	Status    string   `json:"status"`     // "saved" or "error"
	SuitePath string   `json:"suite_path"` // Path where suite was saved
	TestPaths []string `json:"test_paths"` // Paths where tests were saved
	Message   string   `json:"message"`
}

// ValidateDrillArgs are the arguments for the validate_drill tool.
type ValidateDrillArgs struct {
	SuiteYAML string   `json:"suite_yaml" jsonschema:"Full YAML content for the drill suite to validate."`
	TestYAMLs []string `json:"test_yamls" jsonschema:"Array of YAML content strings, one per drill file to validate."`
}

// ValidateDrillResult is the result of the validate_drill tool.
// It reuses ValidationCheck from fleet_plan_validate_tool.go.
type ValidateDrillResult struct {
	Status string            `json:"status"` // "passed" or "failed"
	Checks []ValidationCheck `json:"checks"`
}

func saveDrill(ctx tool.Context, args SaveDrillArgs) (SaveDrillResult, error) {
	// Validate required fields
	if args.SuiteName == "" {
		return SaveDrillResult{Status: "error", Message: "suite_name is required"}, nil
	}
	if len(args.Tests) == 0 {
		return SaveDrillResult{Status: "error", Message: "at least one drill is required"}, nil
	}

	// Append mode: when suite_yaml is empty, skip suite validation/writing
	// and only save the new drill files (used by /drill-add).
	appendMode := args.SuiteYAML == ""

	if !appendMode {
		// Validate suite YAML parses correctly
		var suiteCfg config.AgentConfig
		if err := yaml.Unmarshal([]byte(args.SuiteYAML), &suiteCfg); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("invalid suite YAML: %v", err),
			}, nil
		}

		if suiteCfg.Type != "drill_suite" {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("suite must have type: drill_suite (or type: test_suite for backward compat), got %q", suiteCfg.Type),
			}, nil
		}
		if suiteCfg.SuiteConfig == nil {
			return SaveDrillResult{
				Status:  "error",
				Message: "suite must have a suite_config section",
			}, nil
		}
	}

	// Validate each test YAML
	for i, t := range args.Tests {
		if t.Name == "" {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test[%d]: name is required", i),
			}, nil
		}
		if t.YAML == "" {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: yaml is required", t.Name),
			}, nil
		}

		var testCfg config.AgentConfig
		if err := yaml.Unmarshal([]byte(t.YAML), &testCfg); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: invalid YAML: %v", t.Name, err),
			}, nil
		}

		if testCfg.Type != "drill" {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: must have type: drill (or type: test for backward compat), got %q", t.Name, testCfg.Type),
			}, nil
		}
		if testCfg.Suite != args.SuiteName {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: suite field must be %q, got %q", t.Name, args.SuiteName, testCfg.Suite),
			}, nil
		}
		if len(testCfg.Nodes) == 0 {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: must have at least one node", t.Name),
			}, nil
		}
	}

	// Save to team-scoped flow store (drills are team-only artifacts).
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return SaveDrillResult{
			Status:  "error",
			Message: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	var suitePath string
	if !appendMode {
		if err := fs.SaveFlow(ctx, args.SuiteName, args.SuiteYAML); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("failed to save suite to store: %v", err),
			}, nil
		}
		suitePath = "store://" + args.SuiteName
	}

	var testPaths []string
	for _, t := range args.Tests {
		if err := fs.SaveFlow(ctx, t.Name, t.YAML); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("failed to save drill %q to store: %v", t.Name, err),
			}, nil
		}
		testPaths = append(testPaths, "store://"+t.Name)
	}

	msg := fmt.Sprintf("Saved %d drill(s) to team store", len(args.Tests))
	if !appendMode {
		msg = fmt.Sprintf("Saved suite %q and %d drill(s) to team store", args.SuiteName, len(args.Tests))
	}

	return SaveDrillResult{
		Status:    "saved",
		SuitePath: suitePath,
		TestPaths: testPaths,
		Message:   msg,
	}, nil
}

func validateDrill(ctx tool.Context, args ValidateDrillArgs) (ValidateDrillResult, error) {
	var checks []ValidationCheck
	allPassed := true

	// Validate suite YAML
	var suiteCfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(args.SuiteYAML), &suiteCfg); err != nil {
		checks = append(checks, ValidationCheck{
			Name:    "suite_parse",
			Status:  "failed",
			Message: fmt.Sprintf("Suite YAML parse error: %v", err),
		})
		allPassed = false
	} else {
		checks = append(checks, ValidationCheck{
			Name:    "suite_parse",
			Status:  "passed",
			Message: "Suite YAML parses correctly",
		})

		if suiteCfg.Type != "drill_suite" {
			checks = append(checks, ValidationCheck{
				Name:    "suite_type",
				Status:  "failed",
				Message: fmt.Sprintf("Expected type: drill_suite (or test_suite), got %q", suiteCfg.Type),
			})
			allPassed = false
		} else {
			checks = append(checks, ValidationCheck{
				Name:    "suite_type",
				Status:  "passed",
				Message: "Suite type is drill_suite",
			})
		}

		if suiteCfg.SuiteConfig == nil {
			checks = append(checks, ValidationCheck{
				Name:    "suite_config",
				Status:  "failed",
				Message: "Missing suite_config section",
			})
			allPassed = false
		} else {
			checks = append(checks, ValidationCheck{
				Name:    "suite_config",
				Status:  "passed",
				Message: "Suite config present",
			})

			// Validate services if present
			if len(suiteCfg.SuiteConfig.Services) > 0 {
				namesSeen := make(map[string]bool)
				for i, svc := range suiteCfg.SuiteConfig.Services {
					svcLabel := fmt.Sprintf("service[%d]", i)
					if svc.Name == "" {
						checks = append(checks, ValidationCheck{
							Name:    svcLabel + "_name",
							Status:  "failed",
							Message: fmt.Sprintf("Service at index %d is missing a 'name' field", i),
						})
						allPassed = false
					} else if namesSeen[svc.Name] {
						checks = append(checks, ValidationCheck{
							Name:    svcLabel + "_name",
							Status:  "failed",
							Message: fmt.Sprintf("Duplicate service name %q", svc.Name),
						})
						allPassed = false
					} else {
						namesSeen[svc.Name] = true
					}

					if svc.Setup == "" {
						checks = append(checks, ValidationCheck{
							Name:    svcLabel + "_setup",
							Status:  "failed",
							Message: fmt.Sprintf("Service %q is missing a 'setup' command", svc.Name),
						})
						allPassed = false
					}

					// Validate per-service ready check
					if svc.ReadyCheck != nil {
						svcRC := svc.ReadyCheck
						switch svcRC.Type {
						case "http":
							if svcRC.URL == "" {
								checks = append(checks, ValidationCheck{
									Name:    svcLabel + "_ready_check",
									Status:  "failed",
									Message: fmt.Sprintf("Service %q: ready check type 'http' requires a non-empty 'url' field", svc.Name),
								})
								allPassed = false
							}
						case "port":
							if svcRC.Port <= 0 {
								checks = append(checks, ValidationCheck{
									Name:    svcLabel + "_ready_check",
									Status:  "failed",
									Message: fmt.Sprintf("Service %q: ready check type 'port' requires a positive 'port' value", svc.Name),
								})
								allPassed = false
							}
						case "output_contains":
							if svcRC.Pattern == "" {
								checks = append(checks, ValidationCheck{
									Name:    svcLabel + "_ready_check",
									Status:  "failed",
									Message: fmt.Sprintf("Service %q: ready check type 'output_contains' requires a non-empty 'pattern' field", svc.Name),
								})
								allPassed = false
							}
						case "":
							checks = append(checks, ValidationCheck{
								Name:    svcLabel + "_ready_check",
								Status:  "failed",
								Message: fmt.Sprintf("Service %q: ready check is present but missing 'type' field", svc.Name),
							})
							allPassed = false
						}
					}
				}

				if allPassed {
					checks = append(checks, ValidationCheck{
						Name:    "services",
						Status:  "passed",
						Message: fmt.Sprintf("%d service(s) configured", len(suiteCfg.SuiteConfig.Services)),
					})
				}
			}

			// Validate ready_check if present (legacy single-service)
			rc := suiteCfg.SuiteConfig.ReadyCheck
			if rc != nil {
				switch rc.Type {
				case "http":
					if rc.URL == "" {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "failed",
							Message: "Ready check type 'http' requires a non-empty 'url' field",
						})
						allPassed = false
					} else {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "passed",
							Message: fmt.Sprintf("Ready check: http %s", rc.URL),
						})
					}
				case "port":
					if rc.Port <= 0 {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "failed",
							Message: "Ready check type 'port' requires a positive 'port' value. If the suite does not need a ready check (e.g., CLI tools, build checks), omit the ready_check section entirely.",
						})
						allPassed = false
					} else {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "passed",
							Message: fmt.Sprintf("Ready check: port %d", rc.Port),
						})
					}
				case "output_contains":
					if rc.Pattern == "" {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "failed",
							Message: "Ready check type 'output_contains' requires a non-empty 'pattern' field",
						})
						allPassed = false
					} else {
						checks = append(checks, ValidationCheck{
							Name:    "ready_check",
							Status:  "passed",
							Message: fmt.Sprintf("Ready check: output_contains %q", rc.Pattern),
						})
					}
				case "":
					checks = append(checks, ValidationCheck{
						Name:    "ready_check",
						Status:  "failed",
						Message: "Ready check is present but missing 'type' field. Either set a valid type (http, port, output_contains) or remove the ready_check section entirely.",
					})
					allPassed = false
				default:
					checks = append(checks, ValidationCheck{
						Name:    "ready_check",
						Status:  "failed",
						Message: fmt.Sprintf("Unknown ready check type %q. Valid types: http, port, output_contains", rc.Type),
					})
					allPassed = false
				}
			}
		}
	}

	// Derive suite name from description or use placeholder for cross-ref checks
	suiteName := strings.TrimSuffix(filepath.Base(suiteCfg.Description), ".yaml")

	// Validate each test YAML
	for i, testYAML := range args.TestYAMLs {
		label := fmt.Sprintf("test[%d]", i)

		var testCfg config.AgentConfig
		if err := yaml.Unmarshal([]byte(testYAML), &testCfg); err != nil {
			checks = append(checks, ValidationCheck{
				Name:    label + "_parse",
				Status:  "failed",
				Message: fmt.Sprintf("Test YAML parse error: %v", err),
			})
			allPassed = false
			continue
		}

		checks = append(checks, ValidationCheck{
			Name:    label + "_parse",
			Status:  "passed",
			Message: "Test YAML parses correctly",
		})

		if testCfg.Type != "drill" {
			checks = append(checks, ValidationCheck{
				Name:    label + "_type",
				Status:  "failed",
				Message: fmt.Sprintf("Expected type: drill (or test), got %q", testCfg.Type),
			})
			allPassed = false
		}

		if testCfg.Suite == "" {
			checks = append(checks, ValidationCheck{
				Name:    label + "_suite_ref",
				Status:  "failed",
				Message: "Missing suite field",
			})
			allPassed = false
		}

		if len(testCfg.Nodes) == 0 {
			checks = append(checks, ValidationCheck{
				Name:    label + "_nodes",
				Status:  "failed",
				Message: "No nodes defined",
			})
			allPassed = false
		} else {
			checks = append(checks, ValidationCheck{
				Name:    label + "_nodes",
				Status:  "passed",
				Message: fmt.Sprintf("%d node(s) defined", len(testCfg.Nodes)),
			})
		}

		// Validate node schema and assertions
		tutorial := testCfg.DrillConfig != nil && testCfg.DrillConfig.Mode == "tutorial"
		if testCfg.DrillConfig != nil {
			switch testCfg.DrillConfig.Mode {
			case "", "test", "tutorial":
				checks = append(checks, ValidationCheck{
					Name:    label + "_mode",
					Status:  "passed",
					Message: fmt.Sprintf("drill_config.mode %q ok", testCfg.DrillConfig.Mode),
				})
			default:
				checks = append(checks, ValidationCheck{
					Name:    label + "_mode",
					Status:  "failed",
					Message: fmt.Sprintf("Unknown drill_config.mode %q (want \"\", \"test\", or \"tutorial\")", testCfg.DrillConfig.Mode),
				})
				allPassed = false
			}
		}

		nodesWithAssert := 0
		nodesWithNarration := 0
		for j, node := range testCfg.Nodes {
			nodeLabel := fmt.Sprintf("%s_node[%d]", label, j)
			if node.Name != "" {
				nodeLabel = fmt.Sprintf("%s_node_%s", label, node.Name)
			}

			// Check node type is "tool" (the only valid type for drill nodes)
			if node.Type != "tool" {
				checks = append(checks, ValidationCheck{
					Name:    nodeLabel + "_type",
					Status:  "failed",
					Message: fmt.Sprintf("Node %q has type %q but drill nodes must have type: tool. Use 'type: tool' with 'args.tool: <tool_name>'.", node.Name, node.Type),
				})
				allPassed = false
			}

			// Check args.tool is present and non-empty
			if node.Type == "tool" {
				toolName, _ := node.Args["tool"].(string)
				if toolName == "" {
					checks = append(checks, ValidationCheck{
						Name:    nodeLabel + "_args_tool",
						Status:  "failed",
						Message: fmt.Sprintf("Node %q is missing 'args.tool'. Each drill node must specify the tool to run, e.g.: args: { tool: shell_command, command: \"...\" }", node.Name),
					})
					allPassed = false
				}
			}

			switch node.Record {
			case "", "start", "stop", "segment":
			default:
				checks = append(checks, ValidationCheck{
					Name:    nodeLabel + "_record",
					Status:  "failed",
					Message: fmt.Sprintf("Node %q has unknown record %q (want \"\", \"start\", \"stop\", or \"segment\")", node.Name, node.Record),
				})
				allPassed = false
			}
			if node.HoldMs < 0 {
				checks = append(checks, ValidationCheck{
					Name:    nodeLabel + "_hold_ms",
					Status:  "failed",
					Message: fmt.Sprintf("Node %q has negative hold_ms", node.Name),
				})
				allPassed = false
			}
			if node.Narration != "" {
				nodesWithNarration++
				if tutorial && node.HoldMs == 0 {
					checks = append(checks, ValidationCheck{
						Name:    nodeLabel + "_narration_hold",
						Status:  "failed",
						Message: fmt.Sprintf("Tutorial node %q has narration but hold_ms is 0 — set hold_ms (~150 wpm) so the scene can be voiced", node.Name),
					})
					allPassed = false
				}
			}

			// Validate assertion if present
			if node.Assert != nil {
				nodesWithAssert++
				validTypes := map[string]bool{
					"contains": true, "not_contains": true, "regex": true,
					"exit_code": true, "element_exists": true, "semantic": true,
					"visual_match": true,
				}
				if !validTypes[node.Assert.Type] {
					checks = append(checks, ValidationCheck{
						Name:    nodeLabel + "_assert_type",
						Status:  "failed",
						Message: fmt.Sprintf("Unknown assertion type %q in node %q", node.Assert.Type, node.Name),
					})
					allPassed = false
				}
			}

			if tutorial {
				toolName, _ := node.Args["tool"].(string)
				if tutorialNodeIsStubTODO(node) {
					checks = append(checks, ValidationCheck{
						Name:    nodeLabel + "_todo_stub",
						Status:  "failed",
						Message: fmt.Sprintf("Tutorial node %q still has a browser_run_code TODO stub — replace with real UI actions before save_drill", node.Name),
					})
					allPassed = false
				}
				recorded := node.Record != "" || node.Narration != ""
				if recorded && toolName == "browser_navigate" && !tutorialWarmupNode(node.Name) {
					checks = append(checks, ValidationCheck{
						Name:    nodeLabel + "_navigate_only",
						Status:  "failed",
						Message: fmt.Sprintf("Tutorial recorded node %q uses browser_navigate — prefer sidebar/nav clicks (animate_cursor). Only warm-up open_app may navigate", node.Name),
					})
					allPassed = false
				}
				if recorded && !tutorialWarmupNode(node.Name) {
					if node.Assert == nil {
						checks = append(checks, ValidationCheck{
							Name:    nodeLabel + "_content_assert",
							Status:  "failed",
							Message: fmt.Sprintf("Tutorial recorded node %q needs a content assert (e.g. assert: { type: contains, source: snapshot, expected: \"...\" }) so broken/empty pages fail", node.Name),
						})
						allPassed = false
					} else if !tutorialContentAssert(node.Assert) {
						checks = append(checks, ValidationCheck{
							Name:    nodeLabel + "_content_assert",
							Status:  "failed",
							Message: fmt.Sprintf("Tutorial recorded node %q assert must prove UI content (type contains/element_exists/not_contains/regex, preferably source: snapshot)", node.Name),
						})
						allPassed = false
					}
				}
			}
		}

		// Drills without assertions always pass on tool success alone — unintended
		// for both test and tutorial (tutorials must prove page content).
		if len(testCfg.Nodes) > 0 && nodesWithAssert == 0 {
			checks = append(checks, ValidationCheck{
				Name:    label + "_no_assertions",
				Status:  "failed",
				Message: "No nodes have assertions. Every drill should have at least one node with an 'assert:' block (singular, not 'assertions:'). Use: assert: { type: contains, expected: \"...\" }",
			})
			allPassed = false
		}
		if tutorial && nodesWithNarration == 0 && len(testCfg.Nodes) > 0 {
			checks = append(checks, ValidationCheck{
				Name:    label + "_no_narration",
				Status:  "failed",
				Message: "Tutorial drill has no narration fields — add narration (+ hold_ms, record: segment) on each scene beat",
			})
			allPassed = false
		}
	}

	// Check for naming conflicts with existing flows in team store
	if fs := getDrillFlowStore(ctx); fs != nil && suiteName != "" {
		if _, err := fs.GetFlow(ctx, suiteName); err == nil {
			checks = append(checks, ValidationCheck{
				Name:    "name_conflict",
				Status:  "warning",
				Message: fmt.Sprintf("A suite named %q already exists in team store — it will be overwritten", suiteName),
			})
		}
	}

	status := "passed"
	if !allPassed {
		status = "failed"
	}

	return ValidateDrillResult{
		Status: status,
		Checks: checks,
	}, nil
}

func tutorialWarmupNode(name string) bool {
	switch name {
	case "open_app", "enter_fullscreen":
		return true
	default:
		return false
	}
}

func tutorialNodeIsStubTODO(node config.Node) bool {
	toolName, _ := node.Args["tool"].(string)
	if toolName != "browser_run_code" {
		return false
	}
	code, _ := node.Args["code"].(string)
	if code == "" {
		return false
	}
	lower := strings.ToLower(code)
	return strings.Contains(code, "TODO:") ||
		strings.Contains(lower, "return 'todo'") ||
		strings.Contains(lower, `return "todo"`)
}

func tutorialContentAssert(a *config.AssertConfig) bool {
	if a == nil {
		return false
	}
	switch a.Type {
	case "contains", "not_contains", "regex", "element_exists":
		return true
	default:
		// semantic/visual_match also prove content, but prefer snapshot string checks
		return a.Source == "snapshot" || a.Type == "semantic" || a.Type == "visual_match"
	}
}

// DeleteDrillArgs are the arguments for the delete_drill tool.
type DeleteDrillArgs struct {
	SuiteName   string `json:"suite_name" jsonschema:"Name of the suite to delete (without .yaml extension)"`
	DeleteTests bool   `json:"delete_tests,omitempty" jsonschema:"Also delete all drill files belonging to this suite (default: true)"`
	TestName    string `json:"test_name,omitempty" jsonschema:"Delete a single drill file by name instead of a whole suite. When set, suite_name is ignored."`
}

// DeleteDrillResult is the result of the delete_drill tool.
type DeleteDrillResult struct {
	Status  string   `json:"status"`  // "deleted" or "error"
	Deleted []string `json:"deleted"` // Paths of deleted files
	Message string   `json:"message"`
}

func deleteDrill(ctx tool.Context, args DeleteDrillArgs) (DeleteDrillResult, error) {
	// Delete from the team-scoped flow store (drills are team-only artifacts).
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return DeleteDrillResult{
			Status:  "error",
			Message: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	// Single drill deletion mode
	if args.TestName != "" {
		if err := fs.DeleteFlow(ctx, args.TestName); err != nil {
			return DeleteDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("Failed to delete drill %q: %v", args.TestName, err),
			}, nil
		}
		return DeleteDrillResult{
			Status:  "deleted",
			Deleted: []string{args.TestName},
			Message: fmt.Sprintf("Deleted drill %q from team store", args.TestName),
		}, nil
	}

	// Suite deletion mode
	if args.SuiteName == "" {
		return DeleteDrillResult{
			Status:  "error",
			Message: "Either suite_name or test_name is required",
		}, nil
	}

	var deleted []string

	// Delete child drills first
	drills := fs.ListFlowsByType(ctx, []string{"drill", "test"})
	for _, d := range drills {
		if d.Suite == args.SuiteName {
			if err := fs.DeleteFlow(ctx, d.Name); err == nil {
				deleted = append(deleted, d.Name)
			}
		}
	}

	// Delete the suite itself
	if err := fs.DeleteFlow(ctx, args.SuiteName); err != nil {
		return DeleteDrillResult{
			Status:  "error",
			Deleted: deleted,
			Message: fmt.Sprintf("Failed to delete suite %q: %v", args.SuiteName, err),
		}, nil
	}
	deleted = append(deleted, args.SuiteName)

	msg := fmt.Sprintf("Deleted suite %q", args.SuiteName)
	testCount := len(deleted) - 1
	if testCount > 0 {
		msg += fmt.Sprintf(" and %d drill(s)", testCount)
	}
	msg += " from team store"

	return DeleteDrillResult{
		Status:  "deleted",
		Deleted: deleted,
		Message: msg,
	}, nil
}

// ListDrillsArgs are the arguments for the list_drills tool.
type ListDrillsArgs struct {
	SuiteName string `json:"suite_name,omitempty" jsonschema:"Optional: list drills in a specific suite. If omitted, lists all suites and their drills."`
}

// ListDrillsResult is the result of the list_drills tool.
type ListDrillsResult struct {
	Status  string           `json:"status"` // "ok" or "error"
	Suites  []DrillSuiteInfo `json:"suites,omitempty"`
	Message string           `json:"message,omitempty"`
}

// DrillSuiteInfo describes a suite and its drills for listing.
type DrillSuiteInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Template    string      `json:"template,omitempty"`
	DrillCount  int         `json:"drill_count"`
	Drills      []DrillInfo `json:"drills,omitempty"`
}

// DrillInfo describes a single drill for listing.
type DrillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	StepCount   int      `json:"step_count"`
}

func listDrills(ctx tool.Context, args ListDrillsArgs) (ListDrillsResult, error) {
	// List from the team-scoped flow store (drills are team-only artifacts).
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return ListDrillsResult{
			Status:  "error",
			Message: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	suiteFlows := fs.ListFlowsByType(ctx, []string{"drill_suite", "test_suite"})
	drillFlows := fs.ListFlowsByType(ctx, []string{"drill", "test"})

	// Build lookup of drills grouped by suite
	drillsBySuite := make(map[string][]store.FlowSummary)
	for _, d := range drillFlows {
		drillsBySuite[d.Suite] = append(drillsBySuite[d.Suite], d)
	}

	// If specific suite requested, filter
	if args.SuiteName != "" {
		found := false
		for _, sf := range suiteFlows {
			if sf.Name == args.SuiteName {
				info := buildSuiteInfoFromStore(ctx, fs, sf, drillsBySuite[sf.Name])
				return ListDrillsResult{
					Status: "ok",
					Suites: []DrillSuiteInfo{info},
				}, nil
			}
		}
		if !found {
			return ListDrillsResult{
				Status:  "error",
				Message: fmt.Sprintf("Suite %q not found", args.SuiteName),
			}, nil
		}
	}

	if len(suiteFlows) == 0 {
		return ListDrillsResult{
			Status:  "ok",
			Message: "No drill suites found",
		}, nil
	}

	var infos []DrillSuiteInfo
	for _, sf := range suiteFlows {
		infos = append(infos, buildSuiteInfoFromStore(ctx, fs, sf, drillsBySuite[sf.Name]))
	}

	return ListDrillsResult{
		Status: "ok",
		Suites: infos,
	}, nil
}

// buildSuiteInfoFromStore constructs a DrillSuiteInfo from flow store data.
// It parses suite YAML for description/template and each drill's YAML for
// step counts and tags.
func buildSuiteInfoFromStore(ctx context.Context, fs store.FlowStore, suite store.FlowSummary, drills []store.FlowSummary) DrillSuiteInfo {
	info := DrillSuiteInfo{
		Name:        suite.Name,
		Description: suite.Description,
		DrillCount:  len(drills),
	}

	// Parse suite YAML for template
	if yamlContent, err := fs.GetFlow(ctx, suite.Name); err == nil {
		var cfg config.AgentConfig
		if yaml.Unmarshal([]byte(yamlContent), &cfg) == nil && cfg.SuiteConfig != nil {
			info.Template = cfg.SuiteConfig.Template
		}
	}

	// Parse each drill's YAML for details
	for _, d := range drills {
		di := DrillInfo{
			Name:        d.Name,
			Description: d.Description,
			Tags:        d.Tags,
		}
		if yamlContent, err := fs.GetFlow(ctx, d.Name); err == nil {
			var cfg config.AgentConfig
			if yaml.Unmarshal([]byte(yamlContent), &cfg) == nil {
				di.StepCount = len(cfg.Nodes)
				if cfg.DrillConfig != nil {
					di.Tags = cfg.DrillConfig.Tags
				}
			}
		}
		info.Drills = append(info.Drills, di)
	}

	return info
}

// ReadDrillArgs are the arguments for the read_drill tool.
type ReadDrillArgs struct {
	Name string `json:"name" jsonschema:"Name of the drill or suite to read (without .yaml extension). For a drill, returns the raw YAML content. For a suite, returns a formatted overview of the suite and all its drills."`
}

// ReadDrillResult is the result of the read_drill tool.
type ReadDrillResult struct {
	Status  string `json:"status"`  // "ok" or "error"
	Type    string `json:"type"`    // "drill_yaml", "suite_overview"
	Content string `json:"content"` // The YAML content or formatted overview
	Message string `json:"message,omitempty"`
}

func readDrill(ctx tool.Context, args ReadDrillArgs) (ReadDrillResult, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return ReadDrillResult{
			Status:  "error",
			Message: "name is required",
		}, nil
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	// Read from the team-scoped flow store (drills are team-only artifacts).
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return ReadDrillResult{
			Status:  "error",
			Message: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	yamlContent, err := fs.GetFlow(ctx, name)
	if err != nil {
		return ReadDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("No drill or suite named %q found in team store", name),
		}, nil
	}

	// Parse to determine if it's a suite or drill
	var cfg config.AgentConfig
	if parseErr := yaml.Unmarshal([]byte(yamlContent), &cfg); parseErr == nil && cfg.Type == "drill_suite" {
		// Build a suite overview from store data
		drillFlows := fs.ListFlowsByType(ctx, []string{"drill", "test"})
		var overview strings.Builder
		overview.WriteString(fmt.Sprintf("# Suite: %s\n", name))
		if cfg.Description != "" {
			overview.WriteString(fmt.Sprintf("Description: %s\n", cfg.Description))
		}
		overview.WriteString(fmt.Sprintf("\n## Drills (%d):\n", 0))
		count := 0
		for _, d := range drillFlows {
			if d.Suite == name {
				count++
				overview.WriteString(fmt.Sprintf("- %s", d.Name))
				if d.Description != "" {
					overview.WriteString(fmt.Sprintf(": %s", d.Description))
				}
				overview.WriteString("\n")
			}
		}
		// Fix the count in the header
		result := strings.Replace(overview.String(),
			fmt.Sprintf("## Drills (%d):", 0),
			fmt.Sprintf("## Drills (%d):", count), 1)
		overview.Reset()
		overview.WriteString(result)
		overview.WriteString(fmt.Sprintf("\n## Suite YAML:\n```yaml\n%s\n```\n", yamlContent))

		return ReadDrillResult{
			Status:  "ok",
			Type:    "suite_overview",
			Content: overview.String(),
		}, nil
	}

	// It's a drill — return raw YAML
	return ReadDrillResult{
		Status:  "ok",
		Type:    "drill_yaml",
		Content: yamlContent,
	}, nil
}

// EditDrillArgs are the arguments for the edit_drill tool.
type EditDrillArgs struct {
	Name string `json:"name" jsonschema:"Name of the existing drill to edit (without .yaml extension). The drill must already exist — use save_drill to create new drills."`
	YAML string `json:"yaml" jsonschema:"Complete updated YAML content for the drill file. Must be a valid drill with type: drill, a suite reference, and at least one node."`
}

// EditDrillResult is the result of the edit_drill tool.
type EditDrillResult struct {
	Status  string `json:"status"`            // "ok" or "error"
	Path    string `json:"path,omitempty"`    // Path where the drill was saved
	Message string `json:"message,omitempty"` // Human-readable status
}

func editDrill(ctx tool.Context, args EditDrillArgs) (EditDrillResult, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return EditDrillResult{
			Status:  "error",
			Message: "name is required",
		}, nil
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	if strings.TrimSpace(args.YAML) == "" {
		return EditDrillResult{
			Status:  "error",
			Message: "yaml content is required",
		}, nil
	}

	// Validate the new YAML
	var cfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(args.YAML), &cfg); err != nil {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("invalid YAML: %v", err),
		}, nil
	}

	if cfg.Type != "drill" && cfg.Type != "test" {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("type must be 'drill', got %q", cfg.Type),
		}, nil
	}

	if len(cfg.Nodes) == 0 {
		return EditDrillResult{
			Status:  "error",
			Message: "drill must have at least one node",
		}, nil
	}

	// Save to team-scoped flow store (drills are team-only artifacts).
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return EditDrillResult{
			Status:  "error",
			Message: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	// Verify drill exists in the store
	if _, err := fs.GetFlow(ctx, name); err != nil {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("drill %q not found in team store — use save_drill to create new drills", name),
		}, nil
	}
	if err := fs.SaveFlow(ctx, name, args.YAML); err != nil {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to save drill to store: %v", err),
		}, nil
	}
	return EditDrillResult{
		Status:  "ok",
		Path:    "store://" + name,
		Message: fmt.Sprintf("Drill %q updated in team store", name),
	}, nil
}

// GetDrillTools returns the drill management tools: save, validate, delete, list, read, and edit.
func GetDrillTools() ([]tool.Tool, error) {
	saveTool, err := functiontool.New(functiontool.Config{
		Name: "save_drill",
		Description: "Save a drill suite and its drill files. Creates the suite YAML (type: drill_suite) " +
			"and all associated drill YAML files (type: drill) to the user's flows directory. " +
			"Validates all YAML before saving. Call validate_drill first to check for issues.",
	}, saveDrill)
	if err != nil {
		return nil, fmt.Errorf("create save_drill tool: %w", err)
	}

	validateTool, err := functiontool.New(functiontool.Config{
		Name: "validate_drill",
		Description: "Validate a drill suite and its drills without saving. Checks that YAML parses correctly, " +
			"types are correct, assertions are valid, and cross-references match. " +
			"Call this before save_drill to catch issues early.",
	}, validateDrill)
	if err != nil {
		return nil, fmt.Errorf("create validate_drill tool: %w", err)
	}

	deleteTool, err := functiontool.New(functiontool.Config{
		Name: "delete_drill",
		Description: "Delete a drill suite and its associated drill files, or delete a single drill file. " +
			"When deleting a suite, all drill files that reference it are also removed by default. " +
			"To delete a single drill without touching the suite, pass test_name instead of suite_name.",
	}, deleteDrill)
	if err != nil {
		return nil, fmt.Errorf("create delete_drill tool: %w", err)
	}

	listTool, err := functiontool.New(functiontool.Config{
		Name: "list_drills",
		Description: "List drill suites and their drills. Shows names, descriptions, tags, and step counts. " +
			"Use without arguments to list all suites, or pass suite_name to list drills in a specific suite. " +
			"Drill files are managed by Astonish on the host — they are NOT accessible via read_file or shell_command in sandboxes.",
	}, listDrills)
	if err != nil {
		return nil, fmt.Errorf("create list_drills tool: %w", err)
	}

	readTool, err := functiontool.New(functiontool.Config{
		Name: "read_drill",
		Description: "Read a drill's YAML content or a suite's overview. Pass a drill name to get its raw YAML " +
			"(useful for understanding existing patterns or preparing edits). Pass a suite name to get a formatted " +
			"overview of the suite config, services, and all drill summaries. " +
			"Drill files are managed by Astonish on the host — use this tool instead of read_file to access drill content.",
	}, readDrill)
	if err != nil {
		return nil, fmt.Errorf("create read_drill tool: %w", err)
	}

	editTool, err := functiontool.New(functiontool.Config{
		Name: "edit_drill",
		Description: "Edit an existing drill file. Replaces the drill's YAML content with the provided YAML. " +
			"The drill must already exist (use save_drill to create new drills). " +
			"Typical workflow: read_drill to get current YAML → modify assertions/steps → edit_drill to save changes. " +
			"Drill files are managed by Astonish on the host — use this tool instead of write_file to edit drill content.",
	}, editDrill)
	if err != nil {
		return nil, fmt.Errorf("create edit_drill tool: %w", err)
	}

	draftFromLogTool, err := functiontool.New(functiontool.Config{
		Name: "draft_drill_from_action_log",
		Description: "Convert a browser DOM action capture log (from browser_get_action_log) into a mode-neutral " +
			"drill YAML skeleton (steps only). Chat should add assertions for tests or mode/narration/record for " +
			"tutorials before validate_drill and save_drill.",
	}, draftDrillFromActionLog)
	if err != nil {
		return nil, fmt.Errorf("create draft_drill_from_action_log tool: %w", err)
	}

	blueprintTools, err := GetTutorialBlueprintTools()
	if err != nil {
		return nil, err
	}

	out := []tool.Tool{saveTool, validateTool, deleteTool, listTool, readTool, editTool, draftFromLogTool}
	out = append(out, blueprintTools...)
	return out, nil
}
