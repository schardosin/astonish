package drill

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
	Analysis   string       `json:"analysis,omitempty"` // AI triage summary (from --analyze or on_fail: triage)
}

// TestReport holds results of a single test.
type TestReport struct {
	Name         string            `json:"name"`
	File         string            `json:"file,omitempty"`
	Status       string            `json:"status"` // "passed", "failed", "error", "skipped"
	Duration     int64             `json:"duration_ms"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	Steps        []StepResult      `json:"steps"`
	Tags         []string          `json:"tags,omitempty"`
	Retries      int               `json:"retries,omitempty"`       // number of retries attempted
	ParameterSet map[string]string `json:"parameter_set,omitempty"` // which parameter set this run used (for parameterized tests)
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
	Output    string           `json:"output,omitempty"` // raw tool output for failed/errored steps (capped at 10KB)
	Triage    *TriageVerdict   `json:"triage,omitempty"` // AI diagnosis (when on_fail: triage or --analyze)
}

// TriageVerdict is the structured output from the L1 triage agent.
type TriageVerdict struct {
	Classification string   `json:"classification"` // "transient", "bug", "environment", "test_issue"
	Confidence     float64  `json:"confidence"`     // 0.0 - 1.0
	RootCause      string   `json:"root_cause"`
	Evidence       []string `json:"evidence"`
	Location       string   `json:"location,omitempty"` // file:line if applicable
	Recommendation string   `json:"recommendation"`
	Retry          bool     `json:"retry"`                   // whether the runner should retry
	FullAnalysis   string   `json:"full_analysis,omitempty"` // raw LLM analysis text (for verbose output)
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
		retryNote := ""
		if test.Retries > 0 {
			retryNote = fmt.Sprintf(" (retried %dx)", test.Retries)
		}
		paramNote := ""
		if len(test.ParameterSet) > 0 {
			var params []string
			for k, v := range test.ParameterSet {
				params = append(params, fmt.Sprintf("%s=%q", k, v))
			}
			paramNote = " {" + strings.Join(params, ", ") + "}"
		}
		fmt.Fprintf(w, "  %s %s (%dms)%s%s%s\n", statusIcon(test.Status), test.Name, test.Duration, tags, paramNote, retryNote)

		for _, step := range test.Steps {
			icon := statusIcon(step.Status)
			if step.Assertion != nil && !step.Assertion.Passed {
				fmt.Fprintf(w, "    %s %s: %s\n", icon, step.Name, step.Assertion.Message)
			} else if step.Error != "" {
				fmt.Fprintf(w, "    %s %s: %s\n", icon, step.Name, step.Error)
			} else {
				fmt.Fprintf(w, "    %s %s (%dms)\n", icon, step.Name, step.Duration)
			}

			// Print triage verdict inline with the failed step
			if step.Triage != nil {
				printTriageVerdict(w, step.Triage)
			}
		}
	}

	// Print overall analysis at the bottom
	if report.Analysis != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "--- AI Analysis ---\n")
		fmt.Fprintf(w, "%s\n", report.Analysis)
	}
}

// printTriageVerdict renders a triage verdict below a failed step.
func printTriageVerdict(w io.Writer, v *TriageVerdict) {
	fmt.Fprintln(w)
	classIcon := "BUG"
	switch v.Classification {
	case "transient":
		classIcon = "TRANSIENT"
	case "environment":
		classIcon = "ENV"
	case "test_issue":
		classIcon = "TEST"
	}
	fmt.Fprintf(w, "         Verdict: %s (confidence: %.0f%%)\n", classIcon, v.Confidence*100)
	fmt.Fprintf(w, "         Root cause: %s\n", v.RootCause)
	if len(v.Evidence) > 0 {
		fmt.Fprintf(w, "         Evidence:\n")
		for _, e := range v.Evidence {
			fmt.Fprintf(w, "           - %s\n", e)
		}
	}
	if v.Location != "" {
		fmt.Fprintf(w, "         Location: %s\n", v.Location)
	}
	fmt.Fprintf(w, "         Recommendation: %s\n", v.Recommendation)
	if v.Retry {
		fmt.Fprintf(w, "         Action: Retrying (classified as transient)\n")
	}
	fmt.Fprintln(w)
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
