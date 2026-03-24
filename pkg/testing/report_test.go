package testing

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSuiteReportComputeSummary(t *testing.T) {
	report := &SuiteReport{
		Tests: []TestReport{
			{Name: "test1", Status: "passed"},
			{Name: "test2", Status: "failed"},
			{Name: "test3", Status: "passed"},
		},
	}
	report.ComputeSummary()
	if report.Summary != "2/3 tests passed" {
		t.Errorf("Summary = %q, want %q", report.Summary, "2/3 tests passed")
	}
}

func TestSuiteReportComputeStatus(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []string
		wantStatus string
	}{
		{
			name:       "all passed",
			statuses:   []string{"passed", "passed"},
			wantStatus: "passed",
		},
		{
			name:       "one failed",
			statuses:   []string{"passed", "failed"},
			wantStatus: "failed",
		},
		{
			name:       "error takes precedence",
			statuses:   []string{"passed", "failed", "error"},
			wantStatus: "error",
		},
		{
			name:       "no tests",
			statuses:   []string{},
			wantStatus: "passed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &SuiteReport{}
			for _, s := range tt.statuses {
				report.Tests = append(report.Tests, TestReport{Status: s})
			}
			report.ComputeStatus()
			if report.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", report.Status, tt.wantStatus)
			}
		})
	}
}

func TestSaveAndLoadReport(t *testing.T) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "report-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	now := time.Now().Truncate(time.Second)
	original := &SuiteReport{
		Suite:      "myapp",
		Status:     "passed",
		Duration:   5000,
		StartedAt:  now,
		FinishedAt: now.Add(5 * time.Second),
		Summary:    "2/2 tests passed",
		Tests: []TestReport{
			{
				Name:       "test_login",
				Status:     "passed",
				Duration:   2000,
				StartedAt:  now,
				FinishedAt: now.Add(2 * time.Second),
				Tags:       []string{"smoke"},
				Steps: []StepResult{
					{
						Name:     "step1",
						Tool:     "shell_command",
						Status:   "passed",
						Duration: 500,
					},
				},
			},
		},
	}

	path, err := SaveReport(original, tmpDir)
	if err != nil {
		t.Fatalf("SaveReport: %v", err)
	}
	if !strings.HasSuffix(path, "suite_report.json") {
		t.Errorf("path = %q, want suffix suite_report.json", path)
	}

	loaded, err := LoadReport(path)
	if err != nil {
		t.Fatalf("LoadReport: %v", err)
	}

	if loaded.Suite != original.Suite {
		t.Errorf("Suite = %q, want %q", loaded.Suite, original.Suite)
	}
	if loaded.Status != original.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, original.Status)
	}
	if loaded.Duration != original.Duration {
		t.Errorf("Duration = %d, want %d", loaded.Duration, original.Duration)
	}
	if len(loaded.Tests) != 1 {
		t.Fatalf("Tests length = %d, want 1", len(loaded.Tests))
	}
	if loaded.Tests[0].Tags[0] != "smoke" {
		t.Errorf("Tags[0] = %q, want %q", loaded.Tests[0].Tags[0], "smoke")
	}
}

func TestLoadReportNotFound(t *testing.T) {
	_, err := LoadReport("/nonexistent/path/report.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestSaveReportCreatesDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "report-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	nestedDir := filepath.Join(tmpDir, "nested", "deep")
	report := &SuiteReport{Suite: "test", Status: "passed"}
	_, err = SaveReport(report, nestedDir)
	if err != nil {
		t.Fatalf("SaveReport to nested dir: %v", err)
	}
}

func TestPrintReport(t *testing.T) {
	report := &SuiteReport{
		Suite:    "myapp",
		Status:   "failed",
		Duration: 5000,
		Summary:  "1/2 tests passed",
		Tests: []TestReport{
			{
				Name:     "test_login",
				Status:   "passed",
				Duration: 2000,
				Tags:     []string{"smoke"},
				Steps: []StepResult{
					{Name: "step1", Tool: "shell_command", Status: "passed", Duration: 500},
				},
			},
			{
				Name:     "test_api",
				Status:   "failed",
				Duration: 3000,
				Steps: []StepResult{
					{
						Name:   "check_status",
						Tool:   "shell_command",
						Status: "failed",
						Assertion: &AssertionResult{
							Passed:  false,
							Message: "expected output to contain \"ok\"",
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	PrintReport(report, &buf)
	output := buf.String()

	if !strings.Contains(output, "myapp") {
		t.Error("output should contain suite name")
	}
	if !strings.Contains(output, "FAIL") {
		t.Error("output should contain FAIL status")
	}
	if !strings.Contains(output, "1/2 tests passed") {
		t.Error("output should contain summary")
	}
	if !strings.Contains(output, "smoke") {
		t.Error("output should contain tags")
	}
	if !strings.Contains(output, "expected output to contain") {
		t.Error("output should contain assertion failure message")
	}
}
