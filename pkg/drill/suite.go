package drill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/flowstore"
)

// LoadedSuite holds a parsed suite with its associated tests.
type LoadedSuite struct {
	Name   string              // Suite name (derived from filename)
	File   string              // Path to suite YAML
	Config *config.AgentConfig // Parsed suite config
	Tests  []LoadedTest        // Tests belonging to this suite
}

// LoadedTest holds a parsed test with its metadata.
type LoadedTest struct {
	Name   string              // Test name (derived from filename)
	File   string              // Path to test YAML
	Config *config.AgentConfig // Parsed test config
}

// DiscoverSuites scans directories for test suites and their tests.
func DiscoverSuites(dirs []string) ([]LoadedSuite, error) {
	suites := make(map[string]*LoadedSuite)
	var tests []LoadedTest

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !isYAMLFile(entry.Name()) {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			cfg, err := config.LoadAgent(path)
			if err != nil {
				continue // Skip unparseable files
			}

			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

			switch cfg.Type {
			case "drill_suite", "test_suite":
				suites[name] = &LoadedSuite{
					Name:   name,
					File:   path,
					Config: cfg,
				}
			case "drill", "test":
				tests = append(tests, LoadedTest{
					Name:   name,
					File:   path,
					Config: cfg,
				})
			}
		}
	}

	// Associate tests with their suites
	for _, test := range tests {
		suiteName := test.Config.Suite
		if suite, ok := suites[suiteName]; ok {
			suite.Tests = append(suite.Tests, test)
		}
		// Tests with missing suite references are silently skipped during discovery.
		// Validation happens at run time.
	}

	var result []LoadedSuite
	for _, suite := range suites {
		result = append(result, *suite)
	}
	return result, nil
}

// FindSuite finds a specific suite by name across directories.
func FindSuite(dirs []string, suiteName string) (*LoadedSuite, error) {
	suites, err := DiscoverSuites(dirs)
	if err != nil {
		return nil, err
	}

	for _, suite := range suites {
		if suite.Name == suiteName {
			return &suite, nil
		}
	}
	return nil, fmt.Errorf("suite %q not found", suiteName)
}

// FindTestAndSuite finds a test by name and resolves its suite.
func FindTestAndSuite(dirs []string, testName string) (*LoadedTest, *LoadedSuite, error) {
	suites, err := DiscoverSuites(dirs)
	if err != nil {
		return nil, nil, err
	}

	for _, suite := range suites {
		for _, test := range suite.Tests {
			if test.Name == testName {
				return &test, &suite, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("test %q not found", testName)
}

// ValidateSuite checks that a suite has valid configuration.
func ValidateSuite(suite *LoadedSuite) error {
	if suite.Config.SuiteConfig == nil {
		return fmt.Errorf("suite %q: missing suite_config", suite.Name)
	}
	return nil
}

// ValidateTest checks that a test has valid configuration.
func ValidateTest(test *LoadedTest) error {
	if test.Config.Suite == "" {
		return fmt.Errorf("test %q: missing required 'suite' field", test.Name)
	}
	if len(test.Config.Nodes) == 0 {
		return fmt.Errorf("test %q: no nodes defined", test.Name)
	}
	if dc := test.Config.DrillConfig; dc != nil {
		switch dc.Mode {
		case "", "test", "tutorial":
		default:
			return fmt.Errorf("test %q: unknown drill_config.mode %q (want \"\", \"test\", or \"tutorial\")", test.Name, dc.Mode)
		}
	}
	for _, node := range test.Config.Nodes {
		if node.Assert != nil {
			if err := validateAssert(node.Name, node.Assert); err != nil {
				return fmt.Errorf("test %q: %w", test.Name, err)
			}
		}
		switch node.Record {
		case "", "start", "stop", "segment":
		default:
			return fmt.Errorf("test %q: node %q: unknown record %q (want \"\", \"start\", \"stop\", or \"segment\")", test.Name, node.Name, node.Record)
		}
		if node.HoldMs < 0 {
			return fmt.Errorf("test %q: node %q: hold_ms must be >= 0", test.Name, node.Name)
		}
	}
	return nil
}

func validateAssert(nodeName string, assert *config.AssertConfig) error {
	validTypes := map[string]bool{
		"contains":       true,
		"not_contains":   true,
		"regex":          true,
		"exit_code":      true,
		"element_exists": true,
		"semantic":       true,
		"visual_match":   true,
	}
	if !validTypes[assert.Type] {
		return fmt.Errorf("node %q: unknown assertion type %q", nodeName, assert.Type)
	}

	validSources := map[string]bool{
		"":           true, // default = output
		"output":     true,
		"exit_code":  true,
		"snapshot":   true,
		"screenshot": true,
		"pty_buffer": true,
	}
	if !validSources[assert.Source] {
		return fmt.Errorf("node %q: unknown assertion source %q", nodeName, assert.Source)
	}

	validOnFail := map[string]bool{
		"":         true, // default = from test config
		"stop":     true,
		"continue": true,
		"triage":   true,
	}
	if !validOnFail[assert.OnFail] {
		return fmt.Errorf("node %q: unknown on_fail value %q", nodeName, assert.OnFail)
	}

	return nil
}

// FilterTestByName returns the test matching the given name, or nil if not found.
func FilterTestByName(tests []LoadedTest, name string) *LoadedTest {
	for _, test := range tests {
		if test.Name == name {
			return &test
		}
	}
	return nil
}

// FilterTestsByTag returns tests that have at least one of the specified tags.
func FilterTestsByTag(tests []LoadedTest, tags []string) []LoadedTest {
	if len(tags) == 0 {
		return tests
	}

	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}

	var filtered []LoadedTest
	for _, test := range tests {
		if test.Config.DrillConfig != nil {
			for _, tag := range test.Config.DrillConfig.Tags {
				if tagSet[tag] {
					filtered = append(filtered, test)
					break
				}
			}
		}
	}
	return filtered
}

// FindTestsForSuite finds all test files that reference the given suite name.
// Unlike FindSuite (which only returns tests already associated during discovery),
// this scans all directories for test files that have suite: <suiteName>.
func FindTestsForSuite(dirs []string, suiteName string) ([]LoadedTest, error) {
	var tests []LoadedTest

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !isYAMLFile(entry.Name()) {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			cfg, err := config.LoadAgent(path)
			if err != nil {
				continue
			}

			if (cfg.Type == "drill" || cfg.Type == "test") && cfg.Suite == suiteName {
				name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				tests = append(tests, LoadedTest{
					Name:   name,
					File:   path,
					Config: cfg,
				})
			}
		}
	}

	return tests, nil
}

// DeleteSuite removes a suite file and optionally all its associated test files.
// Returns the list of deleted file paths. If deleteTests is true, associated
// test files are deleted first, then the suite file.
func DeleteSuite(dirs []string, suiteName string, deleteTests bool) ([]string, error) {
	suite, err := FindSuite(dirs, suiteName)
	if err != nil {
		return nil, err
	}

	var deleted []string

	// Delete associated test files first
	if deleteTests {
		tests, err := FindTestsForSuite(dirs, suiteName)
		if err != nil {
			return deleted, fmt.Errorf("finding tests for suite %q: %w", suiteName, err)
		}
		for _, test := range tests {
			if err := os.Remove(test.File); err != nil {
				return deleted, fmt.Errorf("removing test %q: %w", test.Name, err)
			}
			deleted = append(deleted, test.File)
		}
	}

	// Delete the suite file
	if err := os.Remove(suite.File); err != nil {
		return deleted, fmt.Errorf("removing suite %q: %w", suiteName, err)
	}
	deleted = append(deleted, suite.File)

	return deleted, nil
}

// DeleteTest removes a single test file by name.
// Returns the deleted file path and the suite name it belonged to.
func DeleteTest(dirs []string, testName string) (string, string, error) {
	test, suite, err := FindTestAndSuite(dirs, testName)
	if err != nil {
		return "", "", err
	}

	if err := os.Remove(test.File); err != nil {
		return "", "", fmt.Errorf("removing test %q: %w", testName, err)
	}

	return test.File, suite.Name, nil
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// DefaultDrillDirs returns the standard directories to scan for drill suites
// and drills: the system agents dir, a local ./agents/ dir, and the user
// flows dir. This is the canonical set used by both the CLI and the
// run_drill tool.
func DefaultDrillDirs() []string {
	var dirs []string

	// System agents directory
	if sysDir, err := config.GetAgentsDir(); err == nil {
		dirs = append(dirs, sysDir)
	}

	// Local agents directory
	if info, err := os.Stat("agents"); err == nil && info.IsDir() {
		dirs = append(dirs, "agents")
	}

	// User flows directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		dirs = append(dirs, flowsDir)
	}

	return dirs
}

// IsTutorialSuite reports whether the suite already contains tutorial drills.
// A suite is a tutorial suite if any drill has drill_config.mode == "tutorial"
// or a "tutorial" tag. Regular smoke/CI suites must not receive tutorial drills.
func IsTutorialSuite(suite *LoadedSuite) bool {
	if suite == nil {
		return false
	}
	for _, test := range suite.Tests {
		if test.Config == nil || test.Config.DrillConfig == nil {
			continue
		}
		dc := test.Config.DrillConfig
		if dc.Mode == "tutorial" {
			return true
		}
		for _, tag := range dc.Tags {
			if strings.EqualFold(tag, "tutorial") {
				return true
			}
		}
	}
	return false
}

// BuildSuiteContext returns a formatted string describing the suite and its
// existing drills. This is used by the /drill-add and /tutorial-drill-add prompts
// to give the LLM context about what already exists.
func BuildSuiteContext(suite *LoadedSuite) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Suite: %s\n", suite.Name))
	b.WriteString(fmt.Sprintf("Description: %s\n", suite.Config.Description))
	b.WriteString(fmt.Sprintf("File: %s\n", suite.File))
	if IsTutorialSuite(suite) {
		b.WriteString("TutorialSuite: yes\n")
	} else {
		b.WriteString("TutorialSuite: no\n")
	}

	if suite.Config.SuiteConfig != nil {
		sc := suite.Config.SuiteConfig
		if sc.Template != "" {
			b.WriteString(fmt.Sprintf("Template: %s\n", sc.Template))
		}
		if sc.Workspace != "" {
			b.WriteString(fmt.Sprintf("Workspace: %s\n", sc.Workspace))
		}
		if sc.Branch != "" {
			b.WriteString(fmt.Sprintf("Branch: %s\n", sc.Branch))
		}
		if sc.BaseURL != "" {
			b.WriteString(fmt.Sprintf("Base URL: %s\n", sc.BaseURL))
		}
		if len(sc.Setup) > 0 {
			b.WriteString("Setup:\n")
			for _, cmd := range sc.Setup {
				b.WriteString(fmt.Sprintf("  - %s\n", cmd))
			}
		}
		if len(sc.Configure) > 0 {
			b.WriteString("Configure:\n")
			for _, cmd := range sc.Configure {
				b.WriteString(fmt.Sprintf("  - %s\n", cmd))
			}
		}
		if len(sc.Services) > 0 {
			b.WriteString("Services:\n")
			for _, svc := range sc.Services {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", svc.Name, svc.Setup))
			}
		}
		if sc.ReadyCheck != nil {
			b.WriteString(fmt.Sprintf("ReadyCheck: %s\n", formatReadyCheck(sc.ReadyCheck)))
		}
		if len(sc.Credentials) > 0 {
			b.WriteString("Credentials (logical → store entry):\n")
			for logical, entry := range sc.Credentials {
				b.WriteString(fmt.Sprintf("  - %s → %s\n", logical, entry))
			}
		}
		if sc.CredentialInjection != nil {
			if len(sc.CredentialInjection.Env) > 0 {
				b.WriteString("CredentialInjection.env:\n")
				for _, e := range sc.CredentialInjection.Env {
					b.WriteString(fmt.Sprintf("  - %s → $%s (field %s)\n", e.Credential, e.Var, e.Field))
				}
			}
			if len(sc.CredentialInjection.Files) > 0 {
				b.WriteString("CredentialInjection.files:\n")
				for _, f := range sc.CredentialInjection.Files {
					b.WriteString(fmt.Sprintf("  - %s → %s (field %s)\n", f.Credential, f.Path, f.Field))
				}
			}
		}
	}

	b.WriteString(fmt.Sprintf("\nExisting drills (%d):\n", len(suite.Tests)))
	for _, test := range suite.Tests {
		b.WriteString(fmt.Sprintf("  - %s: %s\n", test.Name, test.Config.Description))
		if test.Config.DrillConfig != nil {
			if test.Config.DrillConfig.Mode != "" {
				b.WriteString(fmt.Sprintf("    Mode: %s\n", test.Config.DrillConfig.Mode))
			}
			if len(test.Config.DrillConfig.Tags) > 0 {
				b.WriteString(fmt.Sprintf("    Tags: %s\n", strings.Join(test.Config.DrillConfig.Tags, ", ")))
			}
		}
		if len(test.Config.Nodes) > 0 {
			b.WriteString("    Steps:\n")
			for _, node := range test.Config.Nodes {
				assertInfo := ""
				if node.Assert != nil {
					assertInfo = fmt.Sprintf(" [assert: %s %q]", node.Assert.Type, node.Assert.Expected)
				}
				tool := ""
				if args, ok := node.Args["tool"]; ok {
					tool = fmt.Sprintf(" (tool: %v)", args)
				}
				b.WriteString(fmt.Sprintf("      %s%s%s\n", node.Name, tool, assertInfo))
			}
		}
	}

	if IsTutorialSuite(suite) {
		b.WriteString("\nREUSE: Do not change template/setup/credentials unless the creator opts out.\n")
	}

	return b.String()
}

func formatReadyCheck(rc *config.ReadyCheck) string {
	if rc == nil {
		return ""
	}
	switch rc.Type {
	case "http":
		return fmt.Sprintf("http %s", rc.URL)
	case "port":
		host := rc.Host
		if host == "" {
			host = "localhost"
		}
		return fmt.Sprintf("port %s:%d", host, rc.Port)
	case "output_contains":
		return fmt.Sprintf("output_contains %q", rc.Pattern)
	default:
		if rc.Type != "" {
			return rc.Type
		}
		return "configured"
	}
}
