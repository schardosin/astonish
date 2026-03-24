package testing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDiscoverSuites(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
  setup:
    - "npm start &"
  teardown:
    - "pkill npm"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
test_config:
  tags: ["smoke"]
nodes:
  - name: step1
    type: tool
    args:
      tool: shell_command
      command: "echo hello"
`)

	writeYAML(t, tmpDir, "test_api.yaml", `
description: "Test: API"
type: test
suite: myapp
test_config:
  tags: ["api"]
nodes:
  - name: step1
    type: tool
    args:
      tool: shell_command
      command: "curl localhost"
`)

	// Regular flow — should be ignored
	writeYAML(t, tmpDir, "regular_flow.yaml", `
description: "Regular flow"
nodes:
  - name: start
    type: input
`)

	suites, err := DiscoverSuites([]string{tmpDir})
	if err != nil {
		t.Fatalf("DiscoverSuites: %v", err)
	}

	if len(suites) != 1 {
		t.Fatalf("expected 1 suite, got %d", len(suites))
	}

	suite := suites[0]
	if suite.Name != "myapp" {
		t.Errorf("Name = %q, want %q", suite.Name, "myapp")
	}
	if len(suite.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(suite.Tests))
	}
	if suite.Config.SuiteConfig.Template != "@myapp" {
		t.Errorf("Template = %q, want %q", suite.Config.SuiteConfig.Template, "@myapp")
	}
}

func TestDiscoverSuitesNonexistentDir(t *testing.T) {
	suites, err := DiscoverSuites([]string{"/nonexistent/path"})
	if err != nil {
		t.Fatalf("should not error on nonexistent dir: %v", err)
	}
	if len(suites) != 0 {
		t.Errorf("expected 0 suites, got %d", len(suites))
	}
}

func TestDiscoverSuitesOrphanedTest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test without a matching suite
	writeYAML(t, tmpDir, "test_orphan.yaml", `
description: "Orphan test"
type: test
suite: nonexistent
nodes:
  - name: step1
    type: tool
`)

	suites, err := DiscoverSuites([]string{tmpDir})
	if err != nil {
		t.Fatalf("DiscoverSuites: %v", err)
	}
	if len(suites) != 0 {
		t.Errorf("expected 0 suites, got %d", len(suites))
	}
}

func TestFindSuite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	suite, err := FindSuite([]string{tmpDir}, "myapp")
	if err != nil {
		t.Fatalf("FindSuite: %v", err)
	}
	if suite.Name != "myapp" {
		t.Errorf("Name = %q, want %q", suite.Name, "myapp")
	}

	_, err = FindSuite([]string{tmpDir}, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent suite")
	}
}

func TestFindTestAndSuite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	test, suite, err := FindTestAndSuite([]string{tmpDir}, "test_login")
	if err != nil {
		t.Fatalf("FindTestAndSuite: %v", err)
	}
	if test.Name != "test_login" {
		t.Errorf("test Name = %q, want %q", test.Name, "test_login")
	}
	if suite.Name != "myapp" {
		t.Errorf("suite Name = %q, want %q", suite.Name, "myapp")
	}

	_, _, err = FindTestAndSuite([]string{tmpDir}, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent test")
	}
}

func TestValidateSuite(t *testing.T) {
	// Valid suite
	validCfg := configWithSuiteConfig()
	err := ValidateSuite(&LoadedSuite{
		Name:   "myapp",
		Config: &validCfg,
	})
	if err != nil {
		t.Errorf("valid suite should not error: %v", err)
	}

	// Missing suite_config
	badCfg := configNoSuiteConfig()
	err = ValidateSuite(&LoadedSuite{
		Name:   "bad",
		Config: &badCfg,
	})
	if err == nil {
		t.Error("expected error for missing suite_config")
	}
}

func TestValidateTest(t *testing.T) {
	tests := []struct {
		name    string
		test    LoadedTest
		wantErr bool
	}{
		{
			name:    "valid test",
			test:    validLoadedTest(),
			wantErr: false,
		},
		{
			name: "missing suite",
			test: LoadedTest{
				Name:   "bad",
				Config: configTestNoSuite(),
			},
			wantErr: true,
		},
		{
			name: "no nodes",
			test: LoadedTest{
				Name:   "empty",
				Config: configTestNoNodes(),
			},
			wantErr: true,
		},
		{
			name: "invalid assertion type",
			test: LoadedTest{
				Name:   "bad_assert",
				Config: configTestBadAssert(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTest(&tt.test)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestFindTestsForSuite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	writeYAML(t, tmpDir, "test_api.yaml", `
description: "Test: API"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	writeYAML(t, tmpDir, "test_other.yaml", `
description: "Test for other suite"
type: test
suite: other
nodes:
  - name: step1
    type: tool
`)

	tests, err := FindTestsForSuite([]string{tmpDir}, "myapp")
	if err != nil {
		t.Fatalf("FindTestsForSuite: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests for myapp, got %d", len(tests))
	}

	// Verify the right tests were found
	names := map[string]bool{}
	for _, test := range tests {
		names[test.Name] = true
	}
	if !names["test_login"] || !names["test_api"] {
		t.Errorf("expected test_login and test_api, got %v", names)
	}

	// Finding tests for nonexistent suite should return empty
	tests, err = FindTestsForSuite([]string{tmpDir}, "nonexistent")
	if err != nil {
		t.Fatalf("FindTestsForSuite nonexistent: %v", err)
	}
	if len(tests) != 0 {
		t.Errorf("expected 0 tests for nonexistent suite, got %d", len(tests))
	}
}

func TestDeleteSuiteWithTests(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	writeYAML(t, tmpDir, "test_api.yaml", `
description: "Test: API"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	// Also have a test for another suite — it should NOT be deleted
	writeYAML(t, tmpDir, "test_other.yaml", `
description: "Test for other suite"
type: test
suite: other
nodes:
  - name: step1
    type: tool
`)

	dirs := []string{tmpDir}

	deleted, err := DeleteSuite(dirs, "myapp", true)
	if err != nil {
		t.Fatalf("DeleteSuite: %v", err)
	}

	// Should have deleted 3 files (2 tests + 1 suite)
	if len(deleted) != 3 {
		t.Errorf("expected 3 deleted files, got %d: %v", len(deleted), deleted)
	}

	// Verify files are actually gone
	for _, path := range deleted {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("file should be deleted: %s", path)
		}
	}

	// Verify the other suite's test was NOT deleted
	otherPath := filepath.Join(tmpDir, "test_other.yaml")
	if _, err := os.Stat(otherPath); os.IsNotExist(err) {
		t.Error("test_other.yaml should NOT have been deleted")
	}
}

func TestDeleteSuiteKeepTests(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	dirs := []string{tmpDir}

	deleted, err := DeleteSuite(dirs, "myapp", false)
	if err != nil {
		t.Fatalf("DeleteSuite: %v", err)
	}

	// Should have deleted only the suite file
	if len(deleted) != 1 {
		t.Errorf("expected 1 deleted file, got %d: %v", len(deleted), deleted)
	}

	// Test file should still exist
	testPath := filepath.Join(tmpDir, "test_login.yaml")
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("test_login.yaml should still exist when keep-tests is true")
	}
}

func TestDeleteTest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writeYAML(t, tmpDir, "myapp.yaml", `
description: "MyApp Tests"
type: test_suite
suite_config:
  template: "@myapp"
`)

	writeYAML(t, tmpDir, "test_login.yaml", `
description: "Test: Login"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	writeYAML(t, tmpDir, "test_api.yaml", `
description: "Test: API"
type: test
suite: myapp
nodes:
  - name: step1
    type: tool
`)

	dirs := []string{tmpDir}

	deletedPath, suiteName, err := DeleteTest(dirs, "test_login")
	if err != nil {
		t.Fatalf("DeleteTest: %v", err)
	}

	if suiteName != "myapp" {
		t.Errorf("suiteName = %q, want %q", suiteName, "myapp")
	}

	// Verify the file is gone
	if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
		t.Errorf("test_login.yaml should be deleted")
	}

	// Suite and other test should still exist
	suitePath := filepath.Join(tmpDir, "myapp.yaml")
	if _, err := os.Stat(suitePath); os.IsNotExist(err) {
		t.Error("suite file should still exist")
	}

	apiPath := filepath.Join(tmpDir, "test_api.yaml")
	if _, err := os.Stat(apiPath); os.IsNotExist(err) {
		t.Error("test_api.yaml should still exist")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "suite-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dirs := []string{tmpDir}

	// Delete nonexistent suite
	_, err = DeleteSuite(dirs, "nonexistent", true)
	if err == nil {
		t.Error("expected error when deleting nonexistent suite")
	}

	// Delete nonexistent test
	_, _, err = DeleteTest(dirs, "nonexistent")
	if err == nil {
		t.Error("expected error when deleting nonexistent test")
	}
}

func TestFilterTestsByTag(t *testing.T) {
	tests := []LoadedTest{
		{Name: "t1", Config: configWithTags("smoke", "auth")},
		{Name: "t2", Config: configWithTags("api")},
		{Name: "t3", Config: configWithTags("smoke", "api")},
		{Name: "t4", Config: configWithTags("integration")},
	}

	filtered := FilterTestsByTag(tests, []string{"smoke"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 smoke tests, got %d", len(filtered))
	}

	filtered = FilterTestsByTag(tests, []string{"api"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 api tests, got %d", len(filtered))
	}

	filtered = FilterTestsByTag(tests, []string{"integration"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 integration test, got %d", len(filtered))
	}

	// No filter = all tests
	filtered = FilterTestsByTag(tests, nil)
	if len(filtered) != 4 {
		t.Fatalf("expected 4 tests with no filter, got %d", len(filtered))
	}
}

// --- helpers ---

func configWithSuiteConfig() config.AgentConfig {
	return config.AgentConfig{
		Type:        "test_suite",
		SuiteConfig: &config.TestSuiteConfig{Template: "@test"},
	}
}

func configNoSuiteConfig() config.AgentConfig {
	return config.AgentConfig{
		Type: "test_suite",
	}
}

func validLoadedTest() LoadedTest {
	return LoadedTest{
		Name: "test_ok",
		Config: &config.AgentConfig{
			Type:  "test",
			Suite: "myapp",
			Nodes: []config.Node{{Name: "step1", Type: "tool"}},
		},
	}
}

func configTestNoSuite() *config.AgentConfig {
	return &config.AgentConfig{
		Type:  "test",
		Nodes: []config.Node{{Name: "step1", Type: "tool"}},
	}
}

func configTestNoNodes() *config.AgentConfig {
	return &config.AgentConfig{
		Type:  "test",
		Suite: "myapp",
	}
}

func configTestBadAssert() *config.AgentConfig {
	return &config.AgentConfig{
		Type:  "test",
		Suite: "myapp",
		Nodes: []config.Node{
			{
				Name:   "step1",
				Type:   "tool",
				Assert: &config.AssertConfig{Type: "invalid_type", Expected: "x"},
			},
		},
	}
}

func configWithTags(tags ...string) *config.AgentConfig {
	return &config.AgentConfig{
		Type:       "test",
		Suite:      "myapp",
		TestConfig: &config.TestConfig{Tags: tags},
		Nodes:      []config.Node{{Name: "s", Type: "tool"}},
	}
}
