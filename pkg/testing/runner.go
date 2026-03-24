package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

// ToolExecutor abstracts deterministic tool dispatch for the test runner.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (any, error)
}

// SuiteRunner manages the full suite lifecycle: setup → ready check → tests → teardown.
type SuiteRunner struct {
	toolExecutor ToolExecutor
	artifactMgr  *ArtifactManager
	verbose      bool
}

// NewSuiteRunner creates a runner with the given tool executor and artifact manager.
func NewSuiteRunner(executor ToolExecutor, artifactMgr *ArtifactManager, verbose bool) *SuiteRunner {
	return &SuiteRunner{
		toolExecutor: executor,
		artifactMgr:  artifactMgr,
		verbose:      verbose,
	}
}

// RunSuite executes all tests in a suite with shared setup/teardown.
func (sr *SuiteRunner) RunSuite(ctx context.Context, suite *LoadedSuite, tests []LoadedTest) (*SuiteReport, error) {
	report := &SuiteReport{
		Suite:     suite.Name,
		StartedAt: time.Now(),
	}

	sc := suite.Config.SuiteConfig

	// 1. Set environment variables
	// (In Phase 2, these are set inside the container. For now, they're noted in the report.)
	if sc != nil {
		_ = sc.Environment
	}

	// Dispatch to multi-service or legacy single-service lifecycle
	if sc != nil && len(sc.Services) > 0 {
		return sr.runMultiServiceSuite(ctx, report, sc, tests)
	}

	return sr.runLegacySuite(ctx, report, sc, tests)
}

// runLegacySuite handles the original single-service setup/readycheck/teardown lifecycle.
func (sr *SuiteRunner) runLegacySuite(ctx context.Context, report *SuiteReport, sc *config.TestSuiteConfig, tests []LoadedTest) (*SuiteReport, error) {
	// 2. Run setup commands
	var setupLog strings.Builder
	if sc != nil {
		for _, cmd := range sc.Setup {
			result, err := sr.runShellCommand(ctx, cmd, 120)
			if result != "" {
				setupLog.WriteString(result)
				setupLog.WriteString("\n")
			}
			if err != nil {
				report.Status = "error"
				report.SetupLog = setupLog.String()
				report.Summary = fmt.Sprintf("setup failed: %v", err)
				report.FinishedAt = time.Now()
				report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
				if sr.artifactMgr != nil {
					sr.artifactMgr.SaveSetupLog(setupLog.String())
				}
				return report, nil
			}
		}
	}
	report.SetupLog = setupLog.String()
	if sr.artifactMgr != nil && setupLog.Len() > 0 {
		sr.artifactMgr.SaveSetupLog(setupLog.String())
	}

	// 3. Run ready check (skip if not configured or empty)
	if sc != nil && sc.ReadyCheck != nil && isReadyCheckConfigured(sc.ReadyCheck) {
		if sc.ReadyCheck.Type == "output_contains" {
			// Check the setup output for the pattern
			if !CheckOutputContains(setupLog.String(), sc.ReadyCheck.Pattern) {
				report.Status = "error"
				report.Summary = fmt.Sprintf("ready check failed: setup output does not contain %q", sc.ReadyCheck.Pattern)
				report.FinishedAt = time.Now()
				report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
				return report, nil
			}
		} else {
			if err := RunReadyCheck(ctx, sc.ReadyCheck); err != nil {
				report.Status = "error"
				report.Summary = fmt.Sprintf("ready check failed: %v", err)
				report.FinishedAt = time.Now()
				report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
				return report, nil
			}
		}
	}

	// 4. Execute each test
	sr.executeTests(ctx, report, tests)

	// 5. Run teardown commands (always, even on failure)
	if sc != nil {
		for _, cmd := range sc.Teardown {
			sr.runShellCommand(ctx, cmd, 30) // best-effort
		}
	}

	// 6. Compute status and summary
	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.ComputeStatus()
	report.ComputeSummary()

	return report, nil
}

// runMultiServiceSuite handles multi-service lifecycle:
// start services in declaration order → per-service ready checks → run tests → teardown in reverse order.
func (sr *SuiteRunner) runMultiServiceSuite(ctx context.Context, report *SuiteReport, sc *config.TestSuiteConfig, tests []LoadedTest) (*SuiteReport, error) {
	var setupLog strings.Builder
	var startedServices []config.ServiceConfig // track which services started (for reverse teardown)

	// Start each service in declaration order
	for _, svc := range sc.Services {
		if sr.verbose {
			setupLog.WriteString(fmt.Sprintf("--- Starting service: %s ---\n", svc.Name))
		}

		// Run the service setup command
		result, err := sr.runShellCommand(ctx, svc.Setup, 120)
		if result != "" {
			setupLog.WriteString(result)
			setupLog.WriteString("\n")
		}
		if err != nil {
			// Teardown already-started services in reverse order
			sr.teardownServices(ctx, startedServices)

			report.Status = "error"
			report.SetupLog = setupLog.String()
			report.Summary = fmt.Sprintf("service %q setup failed: %v", svc.Name, err)
			report.FinishedAt = time.Now()
			report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
			if sr.artifactMgr != nil {
				sr.artifactMgr.SaveSetupLog(setupLog.String())
			}
			return report, nil
		}

		startedServices = append(startedServices, svc)

		// Run per-service ready check
		if svc.ReadyCheck != nil && isReadyCheckConfigured(svc.ReadyCheck) {
			if svc.ReadyCheck.Type == "output_contains" {
				if !CheckOutputContains(setupLog.String(), svc.ReadyCheck.Pattern) {
					sr.teardownServices(ctx, startedServices)
					report.Status = "error"
					report.SetupLog = setupLog.String()
					report.Summary = fmt.Sprintf("service %q ready check failed: output does not contain %q", svc.Name, svc.ReadyCheck.Pattern)
					report.FinishedAt = time.Now()
					report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
					return report, nil
				}
			} else {
				if err := RunReadyCheck(ctx, svc.ReadyCheck); err != nil {
					sr.teardownServices(ctx, startedServices)
					report.Status = "error"
					report.SetupLog = setupLog.String()
					report.Summary = fmt.Sprintf("service %q ready check failed: %v", svc.Name, err)
					report.FinishedAt = time.Now()
					report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
					return report, nil
				}
			}
		}

		if sr.verbose {
			setupLog.WriteString(fmt.Sprintf("--- Service %s: ready ---\n", svc.Name))
		}
	}

	report.SetupLog = setupLog.String()
	if sr.artifactMgr != nil && setupLog.Len() > 0 {
		sr.artifactMgr.SaveSetupLog(setupLog.String())
	}

	// Execute tests
	sr.executeTests(ctx, report, tests)

	// Teardown all services in reverse order (always, even on failure)
	sr.teardownServices(ctx, startedServices)

	// Compute status and summary
	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.ComputeStatus()
	report.ComputeSummary()

	return report, nil
}

// executeTests runs all tests and appends results to the report.
func (sr *SuiteRunner) executeTests(ctx context.Context, report *SuiteReport, tests []LoadedTest) {
	for _, test := range tests {
		if err := ValidateTest(&test); err != nil {
			report.Tests = append(report.Tests, TestReport{
				Name:       test.Config.Description,
				File:       test.File,
				Status:     "error",
				StartedAt:  time.Now(),
				FinishedAt: time.Now(),
				Steps:      []StepResult{{Name: "validation", Status: "error", Error: err.Error()}},
			})
			continue
		}

		testReport := sr.runTest(ctx, &test)
		report.Tests = append(report.Tests, testReport)
	}
}

// teardownServices tears down services in reverse declaration order (best-effort).
func (sr *SuiteRunner) teardownServices(ctx context.Context, services []config.ServiceConfig) {
	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		if svc.Teardown != "" {
			sr.runShellCommand(ctx, svc.Teardown, 30) // best-effort
		}
	}
}

// runTest executes a single test and returns its report.
func (sr *SuiteRunner) runTest(ctx context.Context, test *LoadedTest) TestReport {
	tc := test.Config.TestConfig
	timeout := 120
	stepTimeout := 30
	onFail := "stop"
	var tags []string

	if tc != nil {
		if tc.Timeout > 0 {
			timeout = tc.Timeout
		}
		if tc.StepTimeout > 0 {
			stepTimeout = tc.StepTimeout
		}
		if tc.OnFail != "" {
			onFail = tc.OnFail
		}
		tags = tc.Tags
	}

	testCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	report := TestReport{
		Name:      test.Config.Description,
		File:      test.File,
		StartedAt: time.Now(),
		Tags:      tags,
	}

	nodes := resolveExecutionOrder(test.Config)
	allPassed := true

	for _, node := range nodes {
		stepResult := sr.executeStep(testCtx, node, stepTimeout)
		report.Steps = append(report.Steps, stepResult)

		if stepResult.Status == "failed" || stepResult.Status == "error" {
			allPassed = false
			// Determine on_fail behavior
			stepOnFail := onFail
			if node.Assert != nil && node.Assert.OnFail != "" {
				stepOnFail = node.Assert.OnFail
			}
			if stepOnFail == "stop" {
				break
			}
		}
	}

	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()

	if allPassed {
		report.Status = "passed"
	} else {
		report.Status = "failed"
	}

	return report
}

// executeStep runs a single tool node and evaluates its assertion.
func (sr *SuiteRunner) executeStep(ctx context.Context, node config.Node, stepTimeout int) StepResult {
	stepCtx, cancel := context.WithTimeout(ctx, time.Duration(stepTimeout)*time.Second)
	defer cancel()

	toolName := extractToolName(node)

	result := StepResult{
		Name: node.Name,
		Tool: toolName,
	}

	if toolName == "" {
		result.Status = "error"
		result.Error = "no tool specified in node args"
		return result
	}

	// Build tool args (excluding the "tool" key itself)
	toolArgs := make(map[string]interface{})
	for k, v := range node.Args {
		if k != "tool" {
			toolArgs[k] = v
		}
	}

	start := time.Now()

	// Execute the tool
	toolResult, err := sr.toolExecutor.Execute(stepCtx, toolName, toolArgs)
	result.Duration = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	// Extract output for assertions and artifacts
	output := extractOutput(toolResult)

	// Capture proof artifacts
	if sr.artifactMgr != nil && output != "" {
		if isShellTool(toolName) {
			path, _ := sr.artifactMgr.SaveLog(node.Name, output)
			if path != "" {
				result.Artifacts = append(result.Artifacts, path)
			}
		}
	}

	// Evaluate assertion
	if node.Assert != nil {
		content := getAssertionContent(node.Assert, toolResult, output)
		assertResult := Evaluate(node.Assert, content)
		result.Assertion = assertResult

		if assertResult.Passed {
			result.Status = "passed"
		} else {
			result.Status = "failed"
		}
	} else {
		result.Status = "passed"
	}

	return result
}

// runShellCommand executes a shell command via the tool executor.
func (sr *SuiteRunner) runShellCommand(ctx context.Context, command string, timeout int) (string, error) {
	args := map[string]interface{}{
		"command": command,
		"timeout": timeout,
	}

	result, err := sr.toolExecutor.Execute(ctx, "shell_command", args)
	if err != nil {
		return "", err
	}

	output := extractOutput(result)
	return output, nil
}

// extractToolName gets the tool name from node args.
func extractToolName(node config.Node) string {
	if tool, ok := node.Args["tool"]; ok {
		if s, ok := tool.(string); ok {
			return s
		}
	}
	// Fallback: check tools_selection
	if len(node.ToolsSelection) > 0 {
		return node.ToolsSelection[0]
	}
	return ""
}

// extractOutput converts a tool result to a string representation.
func extractOutput(result any) string {
	if result == nil {
		return ""
	}

	// Try common result struct fields via JSON round-trip
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return string(data)
	}

	// Prefer stdout field (from ShellCommandResult)
	if stdout, ok := m["stdout"]; ok {
		return fmt.Sprintf("%v", stdout)
	}
	// Prefer content field (from ReadFileResult)
	if content, ok := m["content"]; ok {
		return fmt.Sprintf("%v", content)
	}
	// Prefer body field (from HttpRequestResult)
	if body, ok := m["body"]; ok {
		return fmt.Sprintf("%v", body)
	}

	return string(data)
}

// getAssertionContent extracts the content to assert against based on the assertion source.
func getAssertionContent(assert *config.AssertConfig, result any, defaultOutput string) string {
	source := assert.Source
	if source == "" {
		source = "output"
	}

	switch source {
	case "output":
		return defaultOutput
	case "exit_code":
		return extractExitCode(result)
	default:
		// For snapshot, screenshot, pty_buffer — fall back to output in Phase 1
		return defaultOutput
	}
}

// extractExitCode extracts exit code from a shell command result.
func extractExitCode(result any) string {
	data, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	if code, ok := m["exit_code"]; ok {
		if code == nil {
			return ""
		}
		return fmt.Sprintf("%v", code)
	}
	return ""
}

// isReadyCheckConfigured returns true if the ready check has enough configuration
// to actually run. A ReadyCheck with port: 0, no URL, and no pattern is treated
// as "not configured" — the AI likely generated a placeholder.
func isReadyCheckConfigured(rc *config.ReadyCheck) bool {
	if rc == nil {
		return false
	}
	switch rc.Type {
	case "http":
		return rc.URL != ""
	case "port":
		return rc.Port > 0
	case "output_contains":
		return rc.Pattern != ""
	default:
		return false
	}
}

// isShellTool returns true if the tool name is a shell/process tool.
func isShellTool(name string) bool {
	return name == "shell_command" || strings.HasPrefix(name, "process_")
}

// resolveExecutionOrder returns nodes in flow order.
// For now, this does a simple topological sort based on the flow edges.
// If no flow is defined, nodes are returned in declaration order.
func resolveExecutionOrder(cfg *config.AgentConfig) []config.Node {
	if len(cfg.Flow) == 0 {
		return cfg.Nodes
	}

	// Build adjacency: from → to
	nodeMap := make(map[string]config.Node)
	for _, n := range cfg.Nodes {
		nodeMap[n.Name] = n
	}

	// Build edge list and find roots (nodes not targeted by any edge)
	targets := make(map[string]bool)
	edges := make(map[string][]string)
	for _, f := range cfg.Flow {
		if f.To != "" {
			edges[f.From] = append(edges[f.From], f.To)
			targets[f.To] = true
		}
		for _, e := range f.Edges {
			edges[f.From] = append(edges[f.From], e.To)
			targets[e.To] = true
		}
	}

	// Find root nodes (appear in flow but not as targets)
	var roots []string
	seen := make(map[string]bool)
	for _, f := range cfg.Flow {
		if !targets[f.From] && !seen[f.From] {
			roots = append(roots, f.From)
			seen[f.From] = true
		}
	}

	if len(roots) == 0 {
		return cfg.Nodes
	}

	// BFS from roots
	var ordered []config.Node
	visited := make(map[string]bool)
	queue := roots

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		if node, ok := nodeMap[name]; ok {
			ordered = append(ordered, node)
		}
		for _, next := range edges[name] {
			if !visited[next] {
				queue = append(queue, next)
			}
		}
	}

	// Add any nodes not in the flow (shouldn't happen in well-formed tests, but be safe)
	for _, n := range cfg.Nodes {
		if !visited[n.Name] {
			ordered = append(ordered, n)
		}
	}

	return ordered
}
