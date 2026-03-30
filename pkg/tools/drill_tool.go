package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/flowstore"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"gopkg.in/yaml.v3"
)

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

func saveDrill(_ tool.Context, args SaveDrillArgs) (SaveDrillResult, error) {
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

	// Determine save directory
	flowsDir, err := flowstore.GetFlowsDir()
	if err != nil {
		return SaveDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to get flows directory: %v", err),
		}, nil
	}

	if err := os.MkdirAll(flowsDir, 0o755); err != nil {
		return SaveDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to create flows directory: %v", err),
		}, nil
	}

	// Save suite file (skip in append mode)
	var suitePath string
	if !appendMode {
		suitePath = filepath.Join(flowsDir, args.SuiteName+".yaml")
		if err := os.WriteFile(suitePath, []byte(args.SuiteYAML), 0o644); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("failed to write suite file: %v", err),
			}, nil
		}
	}

	// Save drill files
	var testPaths []string
	for _, t := range args.Tests {
		testPath := filepath.Join(flowsDir, t.Name+".yaml")
		if err := os.WriteFile(testPath, []byte(t.YAML), 0o644); err != nil {
			return SaveDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("failed to write drill file %q: %v", t.Name, err),
			}, nil
		}
		testPaths = append(testPaths, testPath)
	}

	msg := fmt.Sprintf("Saved %d drill(s) to %s", len(args.Tests), flowsDir)
	if !appendMode {
		msg = fmt.Sprintf("Saved suite %q and %d drill(s) to %s", args.SuiteName, len(args.Tests), flowsDir)
	}

	return SaveDrillResult{
		Status:    "saved",
		SuitePath: suitePath,
		TestPaths: testPaths,
		Message:   msg,
	}, nil
}

func validateDrill(_ tool.Context, args ValidateDrillArgs) (ValidateDrillResult, error) {
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
		nodesWithAssert := 0
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

			// Validate assertion if present
			if node.Assert != nil {
				nodesWithAssert++
				validTypes := map[string]bool{
					"contains": true, "not_contains": true, "regex": true,
					"exit_code": true, "element_exists": true, "semantic": true,
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
		}

		// Warn if no nodes have assertions — drills without assertions always
		// pass regardless of output, which is almost certainly unintended.
		// Common mistake: using "assertions:" (plural) instead of "assert:" (singular),
		// or "value:" instead of "expected:" — these YAML keys are silently ignored.
		if len(testCfg.Nodes) > 0 && nodesWithAssert == 0 {
			checks = append(checks, ValidationCheck{
				Name:    label + "_no_assertions",
				Status:  "failed",
				Message: "No nodes have assertions. Every drill should have at least one node with an 'assert:' block (singular, not 'assertions:'). Use: assert: { type: contains, expected: \"...\" }",
			})
			allPassed = false
		}
	}

	// Check for naming conflicts with existing files
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		if suiteName != "" {
			suitePath := filepath.Join(flowsDir, suiteName+".yaml")
			if _, err := os.Stat(suitePath); err == nil {
				checks = append(checks, ValidationCheck{
					Name:    "name_conflict",
					Status:  "failed",
					Message: fmt.Sprintf("A file named %q already exists in %s — it will be overwritten", suiteName+".yaml", flowsDir),
				})
			}
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

func deleteDrill(_ tool.Context, args DeleteDrillArgs) (DeleteDrillResult, error) {
	// Determine search directories (same as CLI getTestDirs)
	var dirs []string
	if sysDir, err := config.GetAgentsDir(); err == nil {
		dirs = append(dirs, sysDir)
	}
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		dirs = append(dirs, flowsDir)
	}

	// Single test deletion mode
	if args.TestName != "" {
		deletedPath, suiteName, err := adrill.DeleteTest(dirs, args.TestName)
		if err != nil {
			return DeleteDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("Failed to delete test %q: %v", args.TestName, err),
			}, nil
		}
		return DeleteDrillResult{
			Status:  "deleted",
			Deleted: []string{deletedPath},
			Message: fmt.Sprintf("Deleted test %q (was in suite %q)", args.TestName, suiteName),
		}, nil
	}

	// Suite deletion mode
	if args.SuiteName == "" {
		return DeleteDrillResult{
			Status:  "error",
			Message: "Either suite_name or test_name is required",
		}, nil
	}

	// Determine whether to delete associated tests.
	// Go's zero value for bool is false, so we can't distinguish "not passed" from
	// "explicitly false". Since deleting tests is the safer default (avoids orphans),
	// we delete tests unless DeleteTests is explicitly false AND there are no tests,
	// OR the AI explicitly passes delete_tests: false (which we honor).
	deleteTests := args.DeleteTests
	if !args.DeleteTests {
		// Check if there are actually tests — if so, default to deleting them
		// This handles the common case where the AI omits delete_tests entirely
		tests, _ := adrill.FindTestsForSuite(dirs, args.SuiteName)
		if len(tests) > 0 {
			deleteTests = true
		}
	}

	deleted, err := adrill.DeleteSuite(dirs, args.SuiteName, deleteTests)
	if err != nil {
		return DeleteDrillResult{
			Status:  "error",
			Deleted: deleted, // partial deletions may have occurred
			Message: fmt.Sprintf("Failed to delete suite %q: %v", args.SuiteName, err),
		}, nil
	}

	msg := fmt.Sprintf("Deleted suite %q", args.SuiteName)
	testCount := len(deleted) - 1 // subtract the suite file itself
	if testCount > 0 {
		msg += fmt.Sprintf(" and %d test file(s)", testCount)
	}

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

func listDrills(_ tool.Context, args ListDrillsArgs) (ListDrillsResult, error) {
	dirs := adrill.DefaultDrillDirs()

	if args.SuiteName != "" {
		// List drills in a specific suite
		suite, err := adrill.FindSuite(dirs, args.SuiteName)
		if err != nil {
			return ListDrillsResult{
				Status:  "error",
				Message: fmt.Sprintf("Suite %q not found: %v", args.SuiteName, err),
			}, nil
		}
		return ListDrillsResult{
			Status: "ok",
			Suites: []DrillSuiteInfo{buildSuiteInfo(suite)},
		}, nil
	}

	// List all suites
	suites, err := adrill.DiscoverSuites(dirs)
	if err != nil {
		return ListDrillsResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to discover suites: %v", err),
		}, nil
	}

	if len(suites) == 0 {
		return ListDrillsResult{
			Status:  "ok",
			Message: "No drill suites found",
		}, nil
	}

	var infos []DrillSuiteInfo
	for _, s := range suites {
		infos = append(infos, buildSuiteInfo(&s))
	}

	return ListDrillsResult{
		Status: "ok",
		Suites: infos,
	}, nil
}

func buildSuiteInfo(suite *adrill.LoadedSuite) DrillSuiteInfo {
	info := DrillSuiteInfo{
		Name:        suite.Name,
		Description: suite.Config.Description,
		DrillCount:  len(suite.Tests),
	}
	if suite.Config.SuiteConfig != nil {
		info.Template = suite.Config.SuiteConfig.Template
	}
	for _, test := range suite.Tests {
		di := DrillInfo{
			Name:        test.Name,
			Description: test.Config.Description,
			StepCount:   len(test.Config.Nodes),
		}
		if test.Config.DrillConfig != nil {
			di.Tags = test.Config.DrillConfig.Tags
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

func readDrill(_ tool.Context, args ReadDrillArgs) (ReadDrillResult, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return ReadDrillResult{
			Status:  "error",
			Message: "name is required",
		}, nil
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	dirs := adrill.DefaultDrillDirs()

	// First try to find as a drill (test)
	test, _, err := adrill.FindTestAndSuite(dirs, name)
	if err == nil {
		content, readErr := os.ReadFile(test.File)
		if readErr != nil {
			return ReadDrillResult{
				Status:  "error",
				Message: fmt.Sprintf("Found drill %q but failed to read file: %v", name, readErr),
			}, nil
		}
		return ReadDrillResult{
			Status:  "ok",
			Type:    "drill_yaml",
			Content: string(content),
		}, nil
	}

	// Try as a suite
	suite, err := adrill.FindSuite(dirs, name)
	if err == nil {
		overview := adrill.BuildSuiteContext(suite)
		return ReadDrillResult{
			Status:  "ok",
			Type:    "suite_overview",
			Content: overview,
		}, nil
	}

	return ReadDrillResult{
		Status:  "error",
		Message: fmt.Sprintf("No drill or suite named %q found", name),
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

func editDrill(_ tool.Context, args EditDrillArgs) (EditDrillResult, error) {
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

	// Find the existing drill file
	dirs := adrill.DefaultDrillDirs()
	test, _, err := adrill.FindTestAndSuite(dirs, name)
	if err != nil {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("drill %q not found — use save_drill to create new drills", name),
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

	// Write to the existing file path
	yamlBytes := []byte(args.YAML)
	if !strings.HasSuffix(args.YAML, "\n") {
		yamlBytes = append(yamlBytes, '\n')
	}

	if err := os.WriteFile(test.File, yamlBytes, 0644); err != nil {
		return EditDrillResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to write file: %v", err),
		}, nil
	}

	return EditDrillResult{
		Status:  "ok",
		Path:    test.File,
		Message: fmt.Sprintf("Drill %q updated at %s", name, filepath.Base(test.File)),
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

	return []tool.Tool{saveTool, validateTool, deleteTool, listTool, readTool, editTool}, nil
}
