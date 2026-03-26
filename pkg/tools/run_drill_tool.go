package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/browser"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/sandbox"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// RunDrillArgs are the arguments for the run_drill tool.
type RunDrillArgs struct {
	SuiteName string `json:"suite_name" jsonschema:"Name of the drill suite to run (without .yaml extension)"`
	Tag       string `json:"tag,omitempty" jsonschema:"Filter drills by tag (comma-separated)"`
	Verbose   bool   `json:"verbose,omitempty" jsonschema:"Show verbose output including setup logs"`
}

// RunDrillResult is the result of the run_drill tool.
type RunDrillResult struct {
	Status  string `json:"status"`  // "passed", "failed", "error"
	Summary string `json:"summary"` // Human-readable summary line
	Report  string `json:"report"`  // Full formatted report text
}

// runDrillDeps holds sandbox dependencies for the run_drill tool.
// For chat sessions, nodePool is set and lazyClient is resolved at runtime.
// For fleet sessions, lazyClient and sessionID are set directly.
type runDrillDeps struct {
	nodePool   *sandbox.NodeClientPool // Chat/Studio sessions (nil when no sandbox)
	lazyClient *sandbox.LazyNodeClient // Fleet sessions (nil for chat sessions)
	sessionID  string                  // Fleet session ID (empty for chat sessions)
}

// NewRunDrillTool creates the run_drill tool for chat/Studio sessions.
// nodePool may be nil when sandbox is not enabled; the tool will use local execution.
func NewRunDrillTool(nodePool *sandbox.NodeClientPool) (tool.Tool, error) {
	deps := &runDrillDeps{
		nodePool: nodePool,
	}
	return newRunDrillToolFromDeps(deps)
}

// NewRunDrillToolWithClient creates the run_drill tool for fleet sessions
// with a dedicated LazyNodeClient that routes into the fleet's container.
func NewRunDrillToolWithClient(lazyClient *sandbox.LazyNodeClient, sessionID string) (tool.Tool, error) {
	deps := &runDrillDeps{
		lazyClient: lazyClient,
		sessionID:  sessionID,
	}
	return newRunDrillToolFromDeps(deps)
}

func newRunDrillToolFromDeps(deps *runDrillDeps) (tool.Tool, error) {
	// Capture deps in the closure
	fn := func(ctx tool.Context, args RunDrillArgs) (RunDrillResult, error) {
		return executeRunDrill(ctx, deps, args)
	}

	return functiontool.New(functiontool.Config{
		Name: "run_drill",
		Description: "Run a deterministic drill suite. Drills execute in the current sandbox container " +
			"(if sandbox is active) or locally on the host. Shell and file tool steps are routed " +
			"into the container; browser tool steps run on the host. " +
			"Returns the full report with pass/fail status for each drill and step.",
	}, fn)
}

func executeRunDrill(ctx tool.Context, deps *runDrillDeps, args RunDrillArgs) (RunDrillResult, error) {
	suiteName := strings.TrimSpace(args.SuiteName)
	if suiteName == "" {
		return RunDrillResult{
			Status:  "error",
			Summary: "suite_name is required",
		}, nil
	}

	// Strip extension if provided
	suiteName = strings.TrimSuffix(suiteName, ".yaml")
	suiteName = strings.TrimSuffix(suiteName, ".yml")

	// Discover suites from standard directories
	dirs := adrill.DefaultDrillDirs()

	// Find the suite
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("Suite %q not found: %v", suiteName, err),
		}, nil
	}

	// Validate the suite
	if err := adrill.ValidateSuite(suite); err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("Invalid suite: %v", err),
		}, nil
	}

	// Filter tests by tag if requested
	tests := suite.Tests
	if args.Tag != "" {
		tags := strings.Split(args.Tag, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		tests = adrill.FilterTestsByTag(tests, tags)
		if len(tests) == 0 {
			return RunDrillResult{
				Status:  "passed",
				Summary: fmt.Sprintf("No tests matching tags: %s", args.Tag),
				Report:  fmt.Sprintf("Suite: %s\nNo tests matched tags: %s\n", suiteName, args.Tag),
			}, nil
		}
	}

	if len(tests) == 0 {
		return RunDrillResult{
			Status:  "passed",
			Summary: fmt.Sprintf("Suite %q has no tests", suiteName),
			Report:  fmt.Sprintf("Suite: %s\n(no tests)\n", suiteName),
		}, nil
	}

	// Build the executor based on sandbox availability
	executor := buildTestExecutor(ctx, deps)
	defer executor.Close()

	// Discover the container IP for browser URL substitution.
	// In sandbox mode, browser tools run on the host and need the container's
	// bridge IP to reach services. We discover it by running hostname -I
	// through the sandbox executor (which routes into the container).
	vars := map[string]string{
		"CONTAINER_IP": "localhost", // default for local mode
	}
	if executor.hasSandbox() {
		if ip, err := discoverContainerIP(executor); err == nil && ip != "" {
			vars["CONTAINER_IP"] = ip
		}
	}

	// Create artifact manager
	reportDir := filepath.Join(".astonish", "reports", suiteName)
	am, amErr := adrill.NewArtifactManager(reportDir, suiteName)
	if amErr != nil {
		am = nil // non-fatal
	}

	// Run the suite
	runner := adrill.NewSuiteRunner(executor, am, args.Verbose)
	runner.SetVars(vars)
	report, err := runner.RunSuite(context.Background(), suite, tests)
	if err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("Suite execution failed: %v", err),
		}, nil
	}

	// Format the report
	var buf bytes.Buffer
	adrill.PrintReport(report, &buf)

	// Enrich report with failure details for the conversational AI
	if report.Status != "passed" {
		enrichReportWithFailureContext(&buf, report)
	}

	// Save report to disk
	reportPath, saveErr := adrill.SaveReport(report, reportDir)
	if saveErr == nil && reportPath != "" {
		buf.WriteString(fmt.Sprintf("\nReport saved: %s\n", reportPath))
	}

	return RunDrillResult{
		Status:  report.Status,
		Summary: report.Summary,
		Report:  buf.String(),
	}, nil
}

// ---------------------------------------------------------------------------
// Executor types for the run_drill tool
// ---------------------------------------------------------------------------

// testContainerTools lists tools that should be routed into the sandbox
// container when sandbox is active. Mirrors the relevant subset of
// containerTools in pkg/sandbox/node_tool.go.
var testContainerTools = map[string]bool{
	"shell_command":             true,
	"read_file":                 true,
	"write_file":                true,
	"edit_file":                 true,
	"file_tree":                 true,
	"grep_search":               true,
	"find_files":                true,
	"process_read":              true,
	"process_write":             true,
	"process_list":              true,
	"process_kill":              true,
	"http_request":              true,
	"web_fetch":                 true,
	"read_pdf":                  true,
	"filter_json":               true,
	"git_diff_add_line_numbers": true,
}

// testBrowserToolNames lists all browser tool names for routing.
var testBrowserToolNames = map[string]bool{
	"browser_navigate": true, "browser_navigate_back": true,
	"browser_click": true, "browser_type": true, "browser_hover": true,
	"browser_drag": true, "browser_press_key": true, "browser_select_option": true,
	"browser_fill_form": true, "browser_snapshot": true, "browser_take_screenshot": true,
	"browser_console_messages": true, "browser_network_requests": true,
	"browser_tabs": true, "browser_close": true, "browser_resize": true,
	"browser_wait_for": true, "browser_file_upload": true, "browser_handle_dialog": true,
	"browser_evaluate": true, "browser_run_code": true, "browser_pdf": true,
	"browser_response_body": true, "browser_cookies": true, "browser_storage": true,
	"browser_set_offline": true, "browser_set_headers": true, "browser_set_credentials": true,
	"browser_set_geolocation": true, "browser_set_media": true, "browser_set_timezone": true,
	"browser_set_locale": true, "browser_set_device": true,
	"browser_request_human": true, "browser_handoff_complete": true,
}

// closableExecutor extends ToolExecutor with a Close method and sandbox check.
type closableExecutor interface {
	adrill.ToolExecutor
	Close()
	hasSandbox() bool
}

// buildTestExecutor creates the appropriate executor for the run_drill tool.
func buildTestExecutor(ctx tool.Context, deps *runDrillDeps) closableExecutor {
	// Resolve the LazyNodeClient based on context
	var lazyClient *sandbox.LazyNodeClient
	var sessionID string

	if deps.lazyClient != nil {
		// Fleet mode: use the pre-injected client
		lazyClient = deps.lazyClient
		sessionID = deps.sessionID
	} else if deps.nodePool != nil && ctx != nil && ctx.SessionID() != "" {
		// Chat mode: resolve from pool
		lazyClient = deps.nodePool.GetOrCreate(ctx.SessionID())
		sessionID = ctx.SessionID()
	}

	// Browser executor (always host-side)
	hasSandbox := lazyClient != nil
	browserExec := newTestBrowserExecutor(hasSandbox)

	if lazyClient != nil {
		// Sandbox mode: container tools → LazyNodeClient, browser → host, rest → local
		return &testCompositeExecutor{
			sandbox: &testSandboxExecutor{
				lazyClient: lazyClient,
				sessionID:  sessionID,
			},
			browser: browserExec,
			local:   &testLocalExecutor{},
		}
	}

	// No sandbox: everything local or browser
	return &testCompositeExecutor{
		browser: browserExec,
		local:   &testLocalExecutor{},
	}
}

// testCompositeExecutor routes tool calls to the appropriate executor.
type testCompositeExecutor struct {
	sandbox *testSandboxExecutor // nil when sandbox is not active
	browser *testBrowserExecutor
	local   *testLocalExecutor
}

func (c *testCompositeExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	if testBrowserToolNames[name] {
		return c.browser.Execute(ctx, name, args)
	}
	if c.sandbox != nil && testContainerTools[name] {
		return c.sandbox.Execute(ctx, name, args)
	}
	return c.local.Execute(ctx, name, args)
}

func (c *testCompositeExecutor) Close() {
	if c.browser != nil {
		c.browser.Close()
	}
}

func (c *testCompositeExecutor) hasSandbox() bool {
	return c.sandbox != nil
}

// discoverContainerIP runs "hostname -I" through the sandbox executor to get
// the container's bridge IP. Returns the first IPv4 address or empty string.
func discoverContainerIP(executor closableExecutor) (string, error) {
	result, err := executor.Execute(context.Background(), "shell_command", map[string]interface{}{
		"command": "hostname -I | awk '{print $1}'",
		"timeout": 10,
	})
	if err != nil {
		return "", fmt.Errorf("discover container IP: %w", err)
	}

	output := adrill.ExtractOutput(result)
	ip := strings.TrimSpace(output)
	if ip == "" {
		return "", fmt.Errorf("hostname -I returned empty output")
	}
	return ip, nil
}

// testLocalExecutor dispatches to tools.ExecuteTool for local execution.
type testLocalExecutor struct{}

func (e *testLocalExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	return ExecuteTool(ctx, name, args)
}

// testSandboxExecutor proxies container tool calls through a LazyNodeClient.
type testSandboxExecutor struct {
	lazyClient *sandbox.LazyNodeClient
	sessionID  string
}

func (e *testSandboxExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	raw, err := e.lazyClient.Call(e.sessionID, name, args)
	if err != nil {
		return nil, fmt.Errorf("sandbox call %s: %w", name, err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal sandbox result for %s: %w", name, err)
	}
	return result, nil
}

// testBrowserExecutor lazily initializes a browser.Manager and dispatches
// browser tool calls using the closure-based factory pattern from pkg/tools.
type testBrowserExecutor struct {
	mu             sync.Mutex
	mgr            *browser.Manager
	guard          *browser.NavigationGuard
	refs           *browser.RefMap
	blockPrivateIP bool // true = block private IPs (non-sandbox), false = allow (sandbox)
}

func newTestBrowserExecutor(sandbox bool) *testBrowserExecutor {
	return &testBrowserExecutor{
		blockPrivateIP: !sandbox,
	}
}

func (b *testBrowserExecutor) ensureInit() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mgr != nil {
		return
	}
	cfg := browser.DefaultConfig()
	cfg.Headless = true  // test runner always headless
	cfg.UserDataDir = "" // temp dir avoids SingletonLock conflict with Studio browser
	b.mgr = browser.NewManager(cfg)
	b.guard = browser.DefaultNavigationGuard()
	if !b.blockPrivateIP {
		b.guard.BlockPrivateNetworks = false
	}
	b.refs = browser.NewRefMap()
}

func (b *testBrowserExecutor) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mgr != nil {
		b.mgr.Cleanup()
	}
}

func (b *testBrowserExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	b.ensureInit()

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal browser tool args: %w", err)
	}

	switch name {
	case "browser_navigate":
		var a BrowserNavigateArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserNavigate(b.mgr, b.guard)(nil, a)

	case "browser_navigate_back":
		var a BrowserNavigateBackArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserNavigateBack(b.mgr)(nil, a)

	case "browser_click":
		var a BrowserClickArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserClick(b.mgr, b.refs)(nil, a)

	case "browser_type":
		var a BrowserTypeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserType(b.mgr, b.refs)(nil, a)

	case "browser_hover":
		var a BrowserHoverArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserHover(b.mgr, b.refs)(nil, a)

	case "browser_drag":
		var a BrowserDragArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserDrag(b.mgr, b.refs)(nil, a)

	case "browser_press_key":
		var a BrowserPressKeyArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserPressKey(b.mgr)(nil, a)

	case "browser_select_option":
		var a BrowserSelectOptionArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSelectOption(b.mgr, b.refs)(nil, a)

	case "browser_fill_form":
		var a BrowserFillFormArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserFillForm(b.mgr, b.refs)(nil, a)

	case "browser_snapshot":
		var a BrowserSnapshotArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSnapshot(b.mgr, b.refs)(nil, a)

	case "browser_take_screenshot":
		var a BrowserScreenshotArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserTakeScreenshot(b.mgr, b.refs)(nil, a)

	case "browser_console_messages":
		var a BrowserConsoleMessagesArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserConsoleMessages(b.mgr)(nil, a)

	case "browser_network_requests":
		var a BrowserNetworkRequestsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserNetworkRequests(b.mgr)(nil, a)

	case "browser_tabs":
		var a BrowserTabsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserTabs(b.mgr, b.guard)(nil, a)

	case "browser_close":
		var a BrowserCloseArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserClose(b.mgr)(nil, a)

	case "browser_resize":
		var a BrowserResizeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserResize(b.mgr)(nil, a)

	case "browser_wait_for":
		var a BrowserWaitForArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserWaitFor(b.mgr)(nil, a)

	case "browser_file_upload":
		var a BrowserFileUploadArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserFileUpload(b.mgr, b.refs)(nil, a)

	case "browser_handle_dialog":
		var a BrowserHandleDialogArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserHandleDialog(b.mgr)(nil, a)

	case "browser_evaluate":
		var a BrowserEvaluateArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserEvaluate(b.mgr, b.refs)(nil, a)

	case "browser_run_code":
		var a BrowserRunCodeArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserRunCode(b.mgr)(nil, a)

	case "browser_pdf":
		var a BrowserPDFArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserPDF(b.mgr)(nil, a)

	case "browser_response_body":
		var a BrowserResponseBodyArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserResponseBody(b.mgr)(nil, a)

	case "browser_cookies":
		var a BrowserCookiesArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserCookies(b.mgr)(nil, a)

	case "browser_storage":
		var a BrowserStorageArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserStorage(b.mgr)(nil, a)

	case "browser_set_offline":
		var a BrowserSetOfflineArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetOffline(b.mgr)(nil, a)

	case "browser_set_headers":
		var a BrowserSetHeadersArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetHeaders(b.mgr)(nil, a)

	case "browser_set_credentials":
		var a BrowserSetCredentialsArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetCredentials(b.mgr)(nil, a)

	case "browser_set_geolocation":
		var a BrowserSetGeolocationArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetGeolocation(b.mgr)(nil, a)

	case "browser_set_media":
		var a BrowserSetMediaArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetMedia(b.mgr)(nil, a)

	case "browser_set_timezone":
		var a BrowserSetTimezoneArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetTimezone(b.mgr)(nil, a)

	case "browser_set_locale":
		var a BrowserSetLocaleArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetLocale(b.mgr)(nil, a)

	case "browser_set_device":
		var a BrowserSetDeviceArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserSetDevice(b.mgr)(nil, a)

	case "browser_request_human":
		var a BrowserRequestHumanArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserRequestHuman(b.mgr)(nil, a)

	case "browser_handoff_complete":
		var a BrowserHandoffCompleteArgs
		if err := json.Unmarshal(argsJSON, &a); err != nil {
			return nil, fmt.Errorf("invalid args for %s: %w", name, err)
		}
		return BrowserHandoffComplete(b.mgr)(nil, a)

	default:
		return nil, fmt.Errorf("unknown browser tool: %s", name)
	}
}

// enrichReportWithFailureContext appends detailed failure information to the
// report text so the conversational AI can diagnose issues without needing
// to invoke the triage agent. Includes raw output, assertion details, and
// setup log excerpts for each failed step.
func enrichReportWithFailureContext(buf *bytes.Buffer, report *adrill.SuiteReport) {
	buf.WriteString("\n--- Failure Details ---\n")

	for _, test := range report.Tests {
		if test.Status == "passed" {
			continue
		}
		for _, step := range test.Steps {
			if step.Status != "failed" && step.Status != "error" {
				continue
			}

			buf.WriteString(fmt.Sprintf("\nStep: %s (tool: %s)\n", step.Name, step.Tool))

			if step.Error != "" {
				buf.WriteString(fmt.Sprintf("  Error: %s\n", step.Error))
			}

			if step.Assertion != nil {
				buf.WriteString(fmt.Sprintf("  Assertion: %s\n", step.Assertion.Type))
				buf.WriteString(fmt.Sprintf("  Expected: %s\n", step.Assertion.Expected))
				if step.Assertion.Actual != "" {
					actual := step.Assertion.Actual
					if len(actual) > 2048 {
						actual = actual[:2048] + "... (truncated)"
					}
					buf.WriteString(fmt.Sprintf("  Actual: %s\n", actual))
				}
				if step.Assertion.Message != "" {
					buf.WriteString(fmt.Sprintf("  Message: %s\n", step.Assertion.Message))
				}
			}

			if step.Output != "" {
				output := step.Output
				if len(output) > 5120 {
					output = output[:5120] + "\n... (truncated)"
				}
				buf.WriteString(fmt.Sprintf("  Raw output:\n%s\n", output))
			}
		}
	}

	// Include setup log excerpt if available and tests failed
	if report.SetupLog != "" {
		logSnippet := report.SetupLog
		if len(logSnippet) > 2048 {
			logSnippet = "... (truncated)\n" + logSnippet[len(logSnippet)-2048:]
		}
		buf.WriteString(fmt.Sprintf("\nSetup log:\n%s\n", logSnippet))
	}
}
