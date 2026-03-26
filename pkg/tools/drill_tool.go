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

		// Validate assertions
		for _, node := range testCfg.Nodes {
			if node.Assert != nil {
				validTypes := map[string]bool{
					"contains": true, "not_contains": true, "regex": true,
					"exit_code": true, "element_exists": true, "semantic": true,
				}
				if !validTypes[node.Assert.Type] {
					checks = append(checks, ValidationCheck{
						Name:    label + "_assert_" + node.Name,
						Status:  "failed",
						Message: fmt.Sprintf("Unknown assertion type %q in node %q", node.Assert.Type, node.Name),
					})
					allPassed = false
				}
			}
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

// GetDrillTools returns the save_drill, validate_drill, and delete_drill tools.
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

	return []tool.Tool{saveTool, validateTool, deleteTool}, nil
}
