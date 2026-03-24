package testing

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
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
			case "test_suite":
				suites[name] = &LoadedSuite{
					Name:   name,
					File:   path,
					Config: cfg,
				}
			case "test":
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
	// Validate assertions
	for _, node := range test.Config.Nodes {
		if node.Assert != nil {
			if err := validateAssert(node.Name, node.Assert); err != nil {
				return fmt.Errorf("test %q: %w", test.Name, err)
			}
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
		if test.Config.TestConfig != nil {
			for _, tag := range test.Config.TestConfig.Tags {
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

			if cfg.Type == "test" && cfg.Suite == suiteName {
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
