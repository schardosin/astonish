package drill

import (
	"context"
	"encoding/base64"
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

// LLMProvider abstracts an LLM for semantic assertion evaluation.
// Any provider that can generate a text completion implements this.
type LLMProvider interface {
	// EvaluateText sends a prompt to the LLM and returns the response text.
	EvaluateText(ctx context.Context, prompt string) (string, error)
}

// SuiteRunner manages the full suite lifecycle: setup → ready check → tests → teardown.
type SuiteRunner struct {
	toolExecutor  ToolExecutor
	artifactMgr   *ArtifactManager
	verbose       bool
	bgSessions    []string          // session IDs from background setup commands
	vars          map[string]string // runtime variables for placeholder substitution (e.g., CONTAINER_IP)
	baseURL       string            // resolved base_url from suite config (after placeholder substitution)
	triageAgent   *TriageAgent      // optional AI triage agent for failure analysis
	triageEnabled bool              // when true, all on_fail defaults become "triage"
	setupLog      string            // captured setup log for triage context
	llmProvider   LLMProvider       // optional LLM for semantic assertions
}

// NewSuiteRunner creates a runner with the given tool executor and artifact manager.
func NewSuiteRunner(executor ToolExecutor, artifactMgr *ArtifactManager, verbose bool) *SuiteRunner {
	return &SuiteRunner{
		toolExecutor: executor,
		artifactMgr:  artifactMgr,
		verbose:      verbose,
	}
}

// SetVars sets runtime variables that will be substituted in tool args.
// The placeholder syntax is {{KEY}} — e.g., {{CONTAINER_IP}} in a browser_navigate
// URL will be replaced with the actual container bridge IP at runtime.
func (sr *SuiteRunner) SetVars(vars map[string]string) {
	sr.vars = vars
}

// SetTriageAgent enables AI triage analysis on test failures.
// When enabled is true, all on_fail defaults are overridden to "triage"
// so that every failure triggers investigation without editing YAML.
func (sr *SuiteRunner) SetTriageAgent(ta *TriageAgent, enableForAll bool) {
	sr.triageAgent = ta
	sr.triageEnabled = enableForAll
}

// SetLLMProvider sets the LLM used for semantic assertion evaluation.
// When set, assert.type: "semantic" will call the LLM to evaluate whether
// actual output satisfies the expected condition.
func (sr *SuiteRunner) SetLLMProvider(provider LLMProvider) {
	sr.llmProvider = provider
}

// RunSuite executes all tests in a suite with shared setup/teardown.
func (sr *SuiteRunner) RunSuite(ctx context.Context, suite *LoadedSuite, tests []LoadedTest) (*SuiteReport, error) {
	report := &SuiteReport{
		Suite:     suite.Name,
		StartedAt: time.Now(),
	}

	sc := suite.Config.SuiteConfig

	// TODO: apply sc.Environment to container in Phase 2

	// Resolve base_url from suite config (apply placeholder substitution)
	if sc != nil && sc.BaseURL != "" {
		sr.baseURL = substituteVarsInString(sc.BaseURL, sr.vars)
	}

	// Dispatch to multi-service or legacy single-service lifecycle
	if sc != nil && len(sc.Services) > 0 {
		return sr.runMultiServiceSuite(ctx, report, sc, suite, tests)
	}

	return sr.runLegacySuite(ctx, report, sc, suite, tests)
}

// runLegacySuite handles the original single-service setup/readycheck/teardown lifecycle.
func (sr *SuiteRunner) runLegacySuite(ctx context.Context, report *SuiteReport, sc *config.DrillSuiteConfig, suite *LoadedSuite, tests []LoadedTest) (*SuiteReport, error) {
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
				sr.killBackgroundSessions(ctx)
				return report, nil
			}
		}
	}
	report.SetupLog = setupLog.String()
	sr.setupLog = setupLog.String()
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
				sr.killBackgroundSessions(ctx)
				return report, nil
			}
		} else {
			if err := sr.runReadyCheckViaExecutor(ctx, sc.ReadyCheck); err != nil {
				report.Status = "error"
				report.Summary = fmt.Sprintf("ready check failed: %v", err)
				report.FinishedAt = time.Now()
				report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
				sr.killBackgroundSessions(ctx)
				return report, nil
			}
		}
	}

	// 4. Execute each test
	sr.executeTests(ctx, report, suite, tests)

	// 5. Run teardown commands (always, even on failure)
	if sc != nil {
		for _, cmd := range sc.Teardown {
			sr.runShellCommand(ctx, cmd, 30) // best-effort
		}
	}

	// 5b. Kill any background sessions started during setup
	sr.killBackgroundSessions(ctx)

	// 6. Compute status and summary
	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.ComputeStatus()
	report.ComputeSummary()

	return report, nil
}

// runMultiServiceSuite handles multi-service lifecycle:
// start services in declaration order → per-service ready checks → run tests → teardown in reverse order.
func (sr *SuiteRunner) runMultiServiceSuite(ctx context.Context, report *SuiteReport, sc *config.DrillSuiteConfig, suite *LoadedSuite, tests []LoadedTest) (*SuiteReport, error) {
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
			sr.killBackgroundSessions(ctx)

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
					sr.killBackgroundSessions(ctx)
					report.Status = "error"
					report.SetupLog = setupLog.String()
					report.Summary = fmt.Sprintf("service %q ready check failed: output does not contain %q", svc.Name, svc.ReadyCheck.Pattern)
					report.FinishedAt = time.Now()
					report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
					return report, nil
				}
			} else {
				if err := sr.runReadyCheckViaExecutor(ctx, svc.ReadyCheck); err != nil {
					sr.teardownServices(ctx, startedServices)
					sr.killBackgroundSessions(ctx)
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
	sr.setupLog = setupLog.String()
	if sr.artifactMgr != nil && setupLog.Len() > 0 {
		sr.artifactMgr.SaveSetupLog(setupLog.String())
	}

	// Execute tests
	sr.executeTests(ctx, report, suite, tests)

	// Teardown all services in reverse order (always, even on failure)
	sr.teardownServices(ctx, startedServices)

	// Kill any background sessions started during setup
	sr.killBackgroundSessions(ctx)

	// Compute status and summary
	report.FinishedAt = time.Now()
	report.Duration = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.ComputeStatus()
	report.ComputeSummary()

	return report, nil
}

// executeTests runs all tests and appends results to the report.
func (sr *SuiteRunner) executeTests(ctx context.Context, report *SuiteReport, suite *LoadedSuite, tests []LoadedTest) {
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

		// Parameterized tests: run once per parameter set
		if len(test.Config.Parameters) > 0 {
			for i, paramSet := range test.Config.Parameters {
				testReport := sr.runParameterizedTest(ctx, &test, suite, paramSet, i)
				report.Tests = append(report.Tests, testReport)
			}
		} else {
			testReport := sr.runTest(ctx, &test, suite)
			report.Tests = append(report.Tests, testReport)
		}
	}

	// Build overall analysis summary from individual triage verdicts
	if sr.triageAgent != nil {
		sr.buildAnalysisSummary(report)
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
// When a triage agent is configured and a step fails with on_fail: "triage",
// the agent investigates the failure and may trigger an automatic retry.
func (sr *SuiteRunner) runTest(ctx context.Context, test *LoadedTest, suite *LoadedSuite) TestReport {
	tc := test.Config.DrillConfig
	timeout := 120
	stepTimeout := 30
	onFail := "stop"
	maxRetries := 0
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
		if tc.MaxRetries > 0 {
			maxRetries = tc.MaxRetries
		}
		tags = tc.Tags
	}

	// When --analyze is active, override defaults
	if sr.triageEnabled {
		if onFail == "stop" {
			onFail = "triage"
		}
		if maxRetries == 0 {
			maxRetries = 1
		}
	}

	report := sr.runTestAttempt(ctx, test, suite, timeout, stepTimeout, onFail, tags)
	report.Tags = tags

	// Handle retries: if any step got a triage verdict recommending retry,
	// re-run the entire test from scratch (deterministic re-run, no AI).
	retriesLeft := maxRetries
	for retriesLeft > 0 && report.Status == "failed" && sr.shouldRetry(report) {
		retriesLeft--
		report.Retries++
		if sr.verbose {
			fmt.Printf("  Retrying test %q (attempt %d)...\n", test.Config.Description, report.Retries+1)
		}
		report = sr.runTestAttempt(ctx, test, suite, timeout, stepTimeout, onFail, tags)
		report.Retries = maxRetries - retriesLeft
		report.Tags = tags
	}

	return report
}

// runParameterizedTest runs a single test with a specific parameter set.
// The parameter values are merged into the runner's vars for placeholder
// substitution, then restored after the run completes.
func (sr *SuiteRunner) runParameterizedTest(ctx context.Context, test *LoadedTest, suite *LoadedSuite, paramSet map[string]string, paramIdx int) TestReport {
	// Save current vars and merge parameter values
	savedVars := sr.vars
	mergedVars := make(map[string]string, len(sr.vars)+len(paramSet))
	for k, v := range sr.vars {
		mergedVars[k] = v
	}
	for k, v := range paramSet {
		mergedVars[k] = v
	}
	sr.vars = mergedVars

	report := sr.runTest(ctx, test, suite)

	// Tag the report with the parameter set
	report.ParameterSet = paramSet
	// Append parameter index to the name for clarity
	report.Name = fmt.Sprintf("%s [param %d]", report.Name, paramIdx+1)

	// Restore original vars
	sr.vars = savedVars

	return report
}

// runTestAttempt executes a single attempt of a test.
func (sr *SuiteRunner) runTestAttempt(ctx context.Context, test *LoadedTest, suite *LoadedSuite, timeout, stepTimeout int, onFail string, _ []string) TestReport {
	testCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	report := TestReport{
		Name:      test.Config.Description,
		File:      test.File,
		StartedAt: time.Now(),
	}

	// Resolve auto-wait settings from drill config
	autoWait := false
	autoWaitTimeout := 5000 // default 5s in milliseconds
	if tc := test.Config.DrillConfig; tc != nil {
		autoWait = tc.AutoWait
		if tc.AutoWaitTimeout > 0 {
			autoWaitTimeout = tc.AutoWaitTimeout
		}
	}

	nodes := resolveExecutionOrder(test.Config)
	allPassed := true

	for _, node := range nodes {
		stepResult := sr.executeStep(testCtx, node, stepTimeout, autoWait, autoWaitTimeout)
		report.Steps = append(report.Steps, stepResult)

		if stepResult.Status == "failed" || stepResult.Status == "error" {
			allPassed = false
			// Determine on_fail behavior
			stepOnFail := onFail
			if node.Assert != nil && node.Assert.OnFail != "" {
				stepOnFail = node.Assert.OnFail
			}
			// Override to triage when --analyze is active
			if sr.triageEnabled && stepOnFail == "stop" {
				stepOnFail = "triage"
			}

			if stepOnFail == "triage" && sr.triageAgent != nil && suite != nil {
				// Run AI triage investigation
				verdict, err := sr.triageAgent.Investigate(testCtx, stepResult, *test, suite, sr.setupLog)
				if err != nil {
					if sr.verbose {
						fmt.Printf("  Triage error: %v\n", err)
					}
				} else {
					// Attach verdict to the step
					report.Steps[len(report.Steps)-1].Triage = verdict
				}
				// Triage mode implies stop-after-triage (we've diagnosed the problem)
				break
			}
			if stepOnFail == "stop" || stepOnFail == "triage" {
				break
			}
			// "continue" — keep going
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

// shouldRetry checks if any step in the report has a triage verdict recommending retry.
func (sr *SuiteRunner) shouldRetry(report TestReport) bool {
	for _, step := range report.Steps {
		if step.Triage != nil && step.Triage.Retry {
			return true
		}
	}
	return false
}

// buildAnalysisSummary aggregates triage verdicts into a suite-level analysis string.
func (sr *SuiteRunner) buildAnalysisSummary(report *SuiteReport) {
	var summaries []string
	for _, test := range report.Tests {
		for _, step := range test.Steps {
			if step.Triage != nil {
				summary := fmt.Sprintf("[%s] %s > %s: %s (confidence: %.0f%%) — %s",
					step.Triage.Classification,
					test.Name, step.Name,
					step.Triage.RootCause,
					step.Triage.Confidence*100,
					step.Triage.Recommendation)
				summaries = append(summaries, summary)
			}
		}
	}
	if len(summaries) > 0 {
		report.Analysis = strings.Join(summaries, "\n")
	}
}

// executeStep runs a single tool node and evaluates its assertion.
// When autoWait is true and the tool is an interactive browser tool,
// a browser_wait_for call is injected before the actual tool execution
// to wait for the target element to appear.
func (sr *SuiteRunner) executeStep(ctx context.Context, node config.Node, stepTimeout int, autoWait bool, autoWaitTimeout int) StepResult {
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

	// Apply runtime variable substitution (e.g., {{CONTAINER_IP}})
	if len(sr.vars) > 0 {
		toolArgs = substituteVarsInArgs(toolArgs, sr.vars)
	}

	// Apply base_url resolution for browser_navigate with relative URLs
	if sr.baseURL != "" && toolName == "browser_navigate" {
		if url, ok := toolArgs["url"].(string); ok && strings.HasPrefix(url, "/") {
			toolArgs["url"] = strings.TrimRight(sr.baseURL, "/") + url
		}
	}

	// Auto-wait: inject a browser_wait_for call before interactive browser tools
	if autoWait && isInteractiveBrowserTool(toolName) {
		if waitTarget := extractWaitTarget(toolName, toolArgs); waitTarget != "" {
			waitArgs := map[string]interface{}{
				"selector": waitTarget,
				"timeout":  autoWaitTimeout,
			}
			sr.toolExecutor.Execute(stepCtx, "browser_wait_for", waitArgs) // best-effort
		}
	}

	start := time.Now()

	// Execute the tool
	toolResult, err := sr.toolExecutor.Execute(stepCtx, toolName, toolArgs)
	result.Duration = time.Since(start).Milliseconds()

	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		// Preserve raw error context for triage (cap at 10KB)
		errOutput := err.Error()
		if len(errOutput) > 10240 {
			errOutput = errOutput[:10240] + "\n... (truncated)"
		}
		result.Output = errOutput
		return result
	}

	// Extract output for assertions and artifacts
	output := ExtractOutput(toolResult)

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

		// Semantic assertions: use LLM if available
		if node.Assert.Type == "semantic" && sr.llmProvider != nil {
			assertResult := EvaluateSemantic(stepCtx, node.Assert, content, sr.llmProvider)
			result.Assertion = assertResult
		} else if node.Assert.Type == "visual_match" {
			// Visual regression: compare screenshot against baseline
			assertResult := sr.evaluateVisual(node, toolResult)
			result.Assertion = assertResult
		} else {
			assertResult := Evaluate(node.Assert, content)
			result.Assertion = assertResult
		}

		if result.Assertion.Passed {
			result.Status = "passed"
		} else {
			result.Status = "failed"
			// Preserve raw output for triage (cap at 10KB)
			if len(output) > 10240 {
				result.Output = output[:10240] + "\n... (truncated)"
			} else {
				result.Output = output
			}
		}
	} else {
		result.Status = "passed"
	}

	return result
}

// runShellCommand executes a shell command via the tool executor.
// Commands ending with & are automatically run in background mode to prevent
// the PTY from closing and killing the process. The session ID is tracked
// so background processes can be cleaned up after teardown.
func (sr *SuiteRunner) runShellCommand(ctx context.Context, command string, timeout int) (string, error) {
	bg := isBackgroundCommand(command)

	args := map[string]interface{}{
		"command": command,
		"timeout": timeout,
	}

	if bg {
		// Strip trailing & and use the background flag instead.
		// When background=true, the process manager keeps the PTY session
		// alive so the child process survives. With "cmd &" in non-background
		// mode, sh exits immediately and waitLoop closes the PTY, killing
		// the backgrounded child via SIGHUP.
		args["command"] = stripBackgroundSuffix(command)
		args["background"] = true
	}

	result, err := sr.toolExecutor.Execute(ctx, "shell_command", args)
	if err != nil {
		return "", err
	}

	output := ExtractOutput(result)

	// Track background session IDs for cleanup
	if bg {
		if sid := extractSessionID(result); sid != "" {
			sr.bgSessions = append(sr.bgSessions, sid)
		}
	}

	return output, nil
}

// isBackgroundCommand returns true if the command ends with & (after trimming).
func isBackgroundCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	return strings.HasSuffix(trimmed, "&") && !strings.HasSuffix(trimmed, "&&")
}

// stripBackgroundSuffix removes the trailing & from a command.
func stripBackgroundSuffix(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	trimmed = strings.TrimSuffix(trimmed, "&")
	return strings.TrimSpace(trimmed)
}

// extractSessionID extracts the session_id from a shell command result.
func extractSessionID(result any) string {
	if result == nil {
		return ""
	}
	data, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	if sid, ok := m["session_id"]; ok {
		return fmt.Sprintf("%v", sid)
	}
	return ""
}

// killBackgroundSessions kills all tracked background sessions via process_kill.
// This is called after teardown commands have run, as a safety net to ensure
// PTY resources are released even if teardown pkill commands miss a process.
func (sr *SuiteRunner) killBackgroundSessions(ctx context.Context) {
	for _, sid := range sr.bgSessions {
		sr.toolExecutor.Execute(ctx, "process_kill", map[string]interface{}{
			"session_id": sid,
		})
	}
	sr.bgSessions = nil
}

// runReadyCheckViaExecutor polls for service readiness by running shell commands
// (curl for HTTP, nc for port) through the tool executor. This ensures the check
// runs in the same environment as the service (inside the sandbox container when
// sandbox is active, or on the host in local mode). This avoids the problem where
// RunReadyCheck() makes HTTP/TCP calls directly from the host process, which
// cannot reach services listening on localhost inside a container.
func (sr *SuiteRunner) runReadyCheckViaExecutor(ctx context.Context, rc *config.ReadyCheck) error {
	if rc == nil {
		return nil
	}

	timeout := rc.Timeout
	if timeout <= 0 {
		timeout = DefaultReadyCheckTimeout
	}
	interval := rc.Interval
	if interval <= 0 {
		interval = DefaultReadyCheckInterval
	}

	var checkCmd string
	switch rc.Type {
	case "http":
		if rc.URL == "" {
			return fmt.Errorf("ready check http: url is required")
		}
		// Use curl to get the HTTP status code. If curl is not installed,
		// fall back to wget (common in Alpine/minimal containers).
		checkCmd = fmt.Sprintf(
			"if command -v curl >/dev/null 2>&1; then curl -s -o /dev/null -w '%%{http_code}' --max-time 5 %q; "+
				"elif command -v wget >/dev/null 2>&1; then wget -q -O /dev/null --server-response --timeout=5 %q 2>&1 | awk '/HTTP/{print $2}' | tail -1; "+
				"else echo 'ERR_NO_HTTP_CLIENT'; fi",
			rc.URL, rc.URL)
	case "port":
		host := rc.Host
		if host == "" {
			host = "localhost"
		}
		if rc.Port <= 0 {
			return fmt.Errorf("ready check port: port is required")
		}
		// Try nc first; fall back to bash /dev/tcp if nc is not installed.
		// The shell_command tool uses "sh -c", so we explicitly invoke bash
		// for the /dev/tcp fallback which is a bash-specific feature.
		checkCmd = fmt.Sprintf("(command -v nc >/dev/null 2>&1 && nc -z %s %d) || bash -c 'echo >/dev/tcp/%s/%d' 2>/dev/null", host, rc.Port, host, rc.Port)
	case "output_contains":
		// output_contains is handled inline by the runner (checks setup output)
		return fmt.Errorf("output_contains ready check must be handled by the runner")
	default:
		return fmt.Errorf("unknown ready check type: %q", rc.Type)
	}

	deadline := time.After(time.Duration(timeout) * time.Second)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Track last error for diagnostic reporting on timeout
	var lastErr error

	// Try immediately before first tick
	if lastErr = sr.checkReadyOnce(ctx, rc.Type, checkCmd); lastErr == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ready check cancelled: %w", ctx.Err())
		case <-deadline:
			if lastErr != nil {
				return fmt.Errorf("ready check timed out after %ds (type: %s): last error: %v", timeout, rc.Type, lastErr)
			}
			return fmt.Errorf("ready check timed out after %ds (type: %s)", timeout, rc.Type)
		case <-ticker.C:
			if lastErr = sr.checkReadyOnce(ctx, rc.Type, checkCmd); lastErr == nil {
				return nil
			}
		}
	}
}

// checkReadyOnce runs a single ready check poll via the tool executor.
func (sr *SuiteRunner) checkReadyOnce(ctx context.Context, checkType, cmd string) error {
	result, err := sr.toolExecutor.Execute(ctx, "shell_command", map[string]interface{}{
		"command": cmd,
		"timeout": 10,
	})
	if err != nil {
		return err
	}

	// For HTTP checks, verify the status code is 2xx
	if checkType == "http" {
		output := ExtractOutput(result)
		output = strings.TrimSpace(output)
		if len(output) >= 3 {
			// Extract the last 3 chars (the HTTP status code from curl -w)
			code := output[len(output)-3:]
			if code[0] == '2' {
				return nil
			}
			return fmt.Errorf("http ready check: status %s", code)
		}
		return fmt.Errorf("http ready check: unexpected output %q", output)
	}

	// For port checks, nc -z exits 0 on success; check exit_code
	if checkType == "port" {
		exitCode := extractExitCode(result)
		if exitCode == "0" {
			return nil
		}
		return fmt.Errorf("port ready check: connection failed")
	}

	return nil
}
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

// ExtractOutput converts a tool result to a string representation.
func ExtractOutput(result any) string {
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

// isInteractiveBrowserTool returns true if the tool is a browser interaction
// that targets a specific element and benefits from auto-wait.
func isInteractiveBrowserTool(name string) bool {
	switch name {
	case "browser_click", "browser_type", "browser_hover",
		"browser_select_option", "browser_fill_form", "browser_drag":
		return true
	}
	return false
}

// extractWaitTarget derives a CSS selector or ref to wait for from the tool args.
// Returns empty string if no meaningful wait target can be determined.
func extractWaitTarget(toolName string, args map[string]interface{}) string {
	// Check for explicit "selector" argument
	if sel, ok := args["selector"].(string); ok && sel != "" {
		return sel
	}
	// Check for "ref" argument (snapshot-based ref like "ref5")
	if ref, ok := args["ref"].(string); ok && ref != "" {
		// Refs are positional identifiers from browser_snapshot; we can't
		// translate them to CSS selectors for wait_for. Skip auto-wait.
		return ""
	}
	// For browser_fill_form, check for fields with selectors
	if toolName == "browser_fill_form" {
		if fields, ok := args["fields"].([]interface{}); ok && len(fields) > 0 {
			if field, ok := fields[0].(map[string]interface{}); ok {
				if sel, ok := field["selector"].(string); ok && sel != "" {
					return sel
				}
			}
		}
	}
	return ""
}

// evaluateVisual compares a screenshot from the tool result against a stored baseline.
// On first run (no baseline exists), the screenshot is saved as the baseline and passes.
func (sr *SuiteRunner) evaluateVisual(node config.Node, toolResult any) *AssertionResult {
	result := &AssertionResult{
		Type:     "visual_match",
		Expected: node.Assert.Expected, // baseline name
	}

	// Extract screenshot data from tool result
	screenshotData := extractScreenshotData(toolResult)
	if screenshotData == nil {
		result.Passed = false
		result.Message = "no screenshot data in tool result (use browser_take_screenshot)"
		return result
	}

	threshold := node.Assert.Threshold
	if threshold <= 0 {
		threshold = 0.01 // default 1%
	}

	baselineName := node.Assert.Expected
	if baselineName == "" {
		baselineName = node.Name
	}

	// Load or create baseline
	baselineDir := BaselineDir(sr.artifactMgr)
	baseline, err := LoadBaseline(baselineDir, baselineName)
	if err != nil {
		// No baseline exists — save current as baseline and pass
		if saveErr := SaveBaseline(baselineDir, baselineName, screenshotData); saveErr != nil {
			result.Passed = false
			result.Message = fmt.Sprintf("failed to save baseline: %v", saveErr)
			return result
		}
		result.Passed = true
		result.Message = "baseline created (first run)"
		return result
	}

	// Compare images
	diffPct, diffImg, err := CompareImages(baseline, screenshotData, threshold)
	if err != nil {
		result.Passed = false
		result.Message = fmt.Sprintf("image comparison failed: %v", err)
		return result
	}

	result.Actual = fmt.Sprintf("%.2f%% different", diffPct*100)

	if diffPct <= threshold {
		result.Passed = true
		result.Message = fmt.Sprintf("visual match within threshold (%.2f%% diff, %.2f%% allowed)", diffPct*100, threshold*100)
	} else {
		result.Passed = false
		result.Message = fmt.Sprintf("visual regression: %.2f%% pixels differ (threshold: %.2f%%)", diffPct*100, threshold*100)

		// Save diff image as artifact
		if sr.artifactMgr != nil && diffImg != nil {
			path, _ := sr.artifactMgr.SaveDiffImage(node.Name, diffImg)
			if path != "" {
				result.Message += fmt.Sprintf(" (diff saved: %s)", path)
			}
		}
	}

	return result
}

// extractScreenshotData extracts raw PNG bytes from a browser_take_screenshot result.
func extractScreenshotData(result any) []byte {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	// browser_take_screenshot returns base64-encoded image data
	if b64, ok := m["image"].(string); ok && b64 != "" {
		decoded, err := base64Decode(b64)
		if err != nil {
			return nil
		}
		return decoded
	}
	if b64, ok := m["screenshot"].(string); ok && b64 != "" {
		decoded, err := base64Decode(b64)
		if err != nil {
			return nil
		}
		return decoded
	}
	return nil
}

// base64Decode decodes a base64 string, trying standard then URL encoding.
func base64Decode(s string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(s)
	}
	return data, err
}

// substituteVarsInString replaces {{KEY}} and {KEY} placeholders in a string
// with values from the vars map. Double braces are replaced first to avoid
// partial matches. Single braces are replaced as a fallback because LLMs
// sometimes emit {CONTAINER_IP} instead of {{CONTAINER_IP}}.
// Unknown placeholders are left as-is.
func substituteVarsInString(s string, vars map[string]string) string {
	if len(vars) == 0 {
		return s
	}
	for key, val := range vars {
		// Double braces first (canonical form)
		s = strings.ReplaceAll(s, "{{"+key+"}}", val)
		// Single braces fallback (LLM sometimes drops one layer)
		s = strings.ReplaceAll(s, "{"+key+"}", val)
	}
	return s
}

// substituteVarsInArgs recursively walks a tool args map and replaces
// {{KEY}} placeholders in all string values. Returns a new map (does not
// modify the input).
func substituteVarsInArgs(args map[string]interface{}, vars map[string]string) map[string]interface{} {
	if len(vars) == 0 || len(args) == 0 {
		return args
	}
	result := make(map[string]interface{}, len(args))
	for k, v := range args {
		result[k] = substituteVarsInValue(v, vars)
	}
	return result
}

// substituteVarsInValue recursively substitutes placeholders in a single value.
func substituteVarsInValue(v interface{}, vars map[string]string) interface{} {
	switch val := v.(type) {
	case string:
		return substituteVarsInString(val, vars)
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			result[k] = substituteVarsInValue(v2, vars)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v2 := range val {
			result[i] = substituteVarsInValue(v2, vars)
		}
		return result
	default:
		return v
	}
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
