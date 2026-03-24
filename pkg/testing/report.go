package testing

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SuiteReport holds results of running an entire test suite.
type SuiteReport struct {
	Suite      string       `json:"suite"`
	Status     string       `json:"status"` // "passed", "failed", "error"
	Duration   int64        `json:"duration_ms"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	SetupLog   string       `json:"setup_log,omitempty"`
	Tests      []TestReport `json:"tests"`
	Summary    string       `json:"summary"`
}

// TestReport holds results of a single test.
type TestReport struct {
	Name       string       `json:"name"`
	File       string       `json:"file,omitempty"`
	Status     string       `json:"status"` // "passed", "failed", "error", "skipped"
	Duration   int64        `json:"duration_ms"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Steps      []StepResult `json:"steps"`
	Tags       []string     `json:"tags,omitempty"`
}

// StepResult holds the result of a single test step.
type StepResult struct {
	Name      string           `json:"name"`
	Tool      string           `json:"tool"`
	Status    string           `json:"status"` // "passed", "failed", "error", "skipped"
	Duration  int64            `json:"duration_ms"`
	Assertion *AssertionResult `json:"assertion,omitempty"`
	Artifacts []string         `json:"artifacts,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// ComputeSummary sets the Summary field based on test results.
func (sr *SuiteReport) ComputeSummary() {
	total := len(sr.Tests)
	passed := 0
	for _, t := range sr.Tests {
		if t.Status == "passed" {
			passed++
		}
	}
	sr.Summary = fmt.Sprintf("%d/%d tests passed", passed, total)
}

// ComputeStatus sets the Status field based on test results.
func (sr *SuiteReport) ComputeStatus() {
	for _, t := range sr.Tests {
		if t.Status == "error" {
			sr.Status = "error"
			return
		}
		if t.Status == "failed" {
			sr.Status = "failed"
		}
	}
	if sr.Status == "" {
		sr.Status = "passed"
	}
}

// SaveReport writes a suite report as JSON to the given directory.
func SaveReport(report *SuiteReport, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(dir, "suite_report.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	return path, nil
}

// LoadReport reads a suite report from a JSON file.
func LoadReport(path string) (*SuiteReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}

	var report SuiteReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal report: %w", err)
	}

	return &report, nil
}

// PrintReport writes a human-readable summary of a suite report.
func PrintReport(report *SuiteReport, w io.Writer) {
	fmt.Fprintf(w, "Suite: %s\n", report.Suite)
	fmt.Fprintf(w, "Status: %s\n", statusIcon(report.Status))
	fmt.Fprintf(w, "Duration: %dms\n", report.Duration)
	fmt.Fprintf(w, "Summary: %s\n", report.Summary)
	fmt.Fprintln(w)

	for _, test := range report.Tests {
		tags := ""
		if len(test.Tags) > 0 {
			tags = " [" + strings.Join(test.Tags, ", ") + "]"
		}
		fmt.Fprintf(w, "  %s %s (%dms)%s\n", statusIcon(test.Status), test.Name, test.Duration, tags)

		for _, step := range test.Steps {
			icon := statusIcon(step.Status)
			if step.Assertion != nil && !step.Assertion.Passed {
				fmt.Fprintf(w, "    %s %s: %s\n", icon, step.Name, step.Assertion.Message)
			} else if step.Error != "" {
				fmt.Fprintf(w, "    %s %s: %s\n", icon, step.Name, step.Error)
			} else {
				fmt.Fprintf(w, "    %s %s (%dms)\n", icon, step.Name, step.Duration)
			}
		}
	}
}

func statusIcon(status string) string {
	switch status {
	case "passed":
		return "PASS"
	case "failed":
		return "FAIL"
	case "error":
		return "ERR "
	case "skipped":
		return "SKIP"
	default:
		return "??? "
	}
}
