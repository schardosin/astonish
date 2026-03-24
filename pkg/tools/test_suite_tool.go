package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"gopkg.in/yaml.v3"
)

// SaveTestSuiteArgs are the arguments for the save_test_suite tool.
type SaveTestSuiteArgs struct {
	SuiteName string        `json:"suite_name" jsonschema:"Filename for the suite (without .yaml extension, e.g., 'myapp'). This name is what tests reference in their 'suite' field."`
	SuiteYAML string        `json:"suite_yaml" jsonschema:"Full YAML content for the test suite file. Must have type: test_suite and a suite_config section."`
	Tests     []TestFileArg `json:"tests" jsonschema:"Array of test files to save. Each test must reference this suite by name."`
	Template  string        `json:"template,omitempty" jsonschema:"Optional sandbox template name (from save_sandbox_template). Stored for future container integration."`
}

// TestFileArg represents a single test file to save.
type TestFileArg struct {
	Name string `json:"name" jsonschema:"Filename for the test (without .yaml extension, e.g., 'test_api_health')"`
	YAML string `json:"yaml" jsonschema:"Full YAML content for the test file. Must have type: test and suite: <suite_name>."`
}

// SaveTestSuiteResult is the result of the save_test_suite tool.
type SaveTestSuiteResult struct {
	Status    string   `json:"status"`     // "saved" or "error"
	SuitePath string   `json:"suite_path"` // Path where suite was saved
	TestPaths []string `json:"test_paths"` // Paths where tests were saved
	Message   string   `json:"message"`
}

// ValidateTestSuiteArgs are the arguments for the validate_test_suite tool.
type ValidateTestSuiteArgs struct {
	SuiteYAML string   `json:"suite_yaml" jsonschema:"Full YAML content for the test suite to validate."`
	TestYAMLs []string `json:"test_yamls" jsonschema:"Array of YAML content strings, one per test file to validate."`
}

// ValidateTestSuiteResult is the result of the validate_test_suite tool.
// It reuses ValidationCheck from fleet_plan_validate_tool.go.
type ValidateTestSuiteResult struct {
	Status string            `json:"status"` // "passed" or "failed"
	Checks []ValidationCheck `json:"checks"`
}

func saveTestSuite(_ tool.Context, args SaveTestSuiteArgs) (SaveTestSuiteResult, error) {
	// Validate required fields
	if args.SuiteName == "" {
		return SaveTestSuiteResult{Status: "error", Message: "suite_name is required"}, nil
	}
	if args.SuiteYAML == "" {
		return SaveTestSuiteResult{Status: "error", Message: "suite_yaml is required"}, nil
	}
	if len(args.Tests) == 0 {
		return SaveTestSuiteResult{Status: "error", Message: "at least one test is required"}, nil
	}

	// Validate suite YAML parses correctly
	var suiteCfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(args.SuiteYAML), &suiteCfg); err != nil {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: fmt.Sprintf("invalid suite YAML: %v", err),
		}, nil
	}

	if suiteCfg.Type != "test_suite" {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: fmt.Sprintf("suite must have type: test_suite, got %q", suiteCfg.Type),
		}, nil
	}
	if suiteCfg.SuiteConfig == nil {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: "suite must have a suite_config section",
		}, nil
	}

	// Validate each test YAML
	for i, t := range args.Tests {
		if t.Name == "" {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test[%d]: name is required", i),
			}, nil
		}
		if t.YAML == "" {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: yaml is required", t.Name),
			}, nil
		}

		var testCfg config.AgentConfig
		if err := yaml.Unmarshal([]byte(t.YAML), &testCfg); err != nil {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: invalid YAML: %v", t.Name, err),
			}, nil
		}

		if testCfg.Type != "test" {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: must have type: test, got %q", t.Name, testCfg.Type),
			}, nil
		}
		if testCfg.Suite != args.SuiteName {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: suite field must be %q, got %q", t.Name, args.SuiteName, testCfg.Suite),
			}, nil
		}
		if len(testCfg.Nodes) == 0 {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("test %q: must have at least one node", t.Name),
			}, nil
		}
	}

	// Determine save directory
	flowsDir, err := flowstore.GetFlowsDir()
	if err != nil {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to get flows directory: %v", err),
		}, nil
	}

	if err := os.MkdirAll(flowsDir, 0o755); err != nil {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to create flows directory: %v", err),
		}, nil
	}

	// Save suite file
	suitePath := filepath.Join(flowsDir, args.SuiteName+".yaml")
	if err := os.WriteFile(suitePath, []byte(args.SuiteYAML), 0o644); err != nil {
		return SaveTestSuiteResult{
			Status:  "error",
			Message: fmt.Sprintf("failed to write suite file: %v", err),
		}, nil
	}

	// Save test files
	var testPaths []string
	for _, t := range args.Tests {
		testPath := filepath.Join(flowsDir, t.Name+".yaml")
		if err := os.WriteFile(testPath, []byte(t.YAML), 0o644); err != nil {
			return SaveTestSuiteResult{
				Status:  "error",
				Message: fmt.Sprintf("failed to write test file %q: %v", t.Name, err),
			}, nil
		}
		testPaths = append(testPaths, testPath)
	}

	return SaveTestSuiteResult{
		Status:    "saved",
		SuitePath: suitePath,
		TestPaths: testPaths,
		Message:   fmt.Sprintf("Saved suite %q and %d test(s) to %s", args.SuiteName, len(args.Tests), flowsDir),
	}, nil
}

func validateTestSuite(_ tool.Context, args ValidateTestSuiteArgs) (ValidateTestSuiteResult, error) {
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

		if suiteCfg.Type != "test_suite" {
			checks = append(checks, ValidationCheck{
				Name:    "suite_type",
				Status:  "failed",
				Message: fmt.Sprintf("Expected type: test_suite, got %q", suiteCfg.Type),
			})
			allPassed = false
		} else {
			checks = append(checks, ValidationCheck{
				Name:    "suite_type",
				Status:  "passed",
				Message: "Suite type is test_suite",
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

		if testCfg.Type != "test" {
			checks = append(checks, ValidationCheck{
				Name:    label + "_type",
				Status:  "failed",
				Message: fmt.Sprintf("Expected type: test, got %q", testCfg.Type),
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

	return ValidateTestSuiteResult{
		Status: status,
		Checks: checks,
	}, nil
}

// GetTestSuiteTools returns the save_test_suite and validate_test_suite tools.
func GetTestSuiteTools() ([]tool.Tool, error) {
	saveTool, err := functiontool.New(functiontool.Config{
		Name: "save_test_suite",
		Description: "Save a test suite and its test files. Creates the suite YAML (type: test_suite) " +
			"and all associated test YAML files (type: test) to the user's flows directory. " +
			"Validates all YAML before saving. Call validate_test_suite first to check for issues.",
	}, saveTestSuite)
	if err != nil {
		return nil, fmt.Errorf("create save_test_suite tool: %w", err)
	}

	validateTool, err := functiontool.New(functiontool.Config{
		Name: "validate_test_suite",
		Description: "Validate a test suite and its tests without saving. Checks that YAML parses correctly, " +
			"types are correct, assertions are valid, and cross-references match. " +
			"Call this before save_test_suite to catch issues early.",
	}, validateTestSuite)
	if err != nil {
		return nil, fmt.Errorf("create validate_test_suite tool: %w", err)
	}

	return []tool.Tool{saveTool, validateTool}, nil
}
