package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/config"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// RunDrillArgs are the arguments for the run_drill tool.
type RunDrillArgs struct {
	SuiteName string `json:"suite_name" jsonschema:"Name of the drill suite to run (without .yaml extension)"`
	TestName  string `json:"test_name,omitempty" jsonschema:"Run a single drill by name instead of the full suite. The drill must belong to the specified suite."`
	Tag       string `json:"tag,omitempty" jsonschema:"Filter drills by tag (comma-separated)"`
	Verbose   bool   `json:"verbose,omitempty" jsonschema:"Show verbose output including setup logs"`
	Force     bool   `json:"force,omitempty" jsonschema:"Skip auto-switching to the suite's sandbox template and run on the current container instead. Only use when the user explicitly wants to stay on the current sandbox."`
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
	nodePool         *sandbox.NodeClientPool   // Chat/Studio sessions (nil when no sandbox)
	templateRegistry *sandbox.TemplateRegistry // Optional; used to inject bootstrap_files after auto-switch
	browserMgr       *browser.Manager          // Shared in-container browser (chat/fleet); never host Chrome
	lazyClient       *sandbox.LazyNodeClient   // Fleet Incus sessions
	toolClient       sandbox.ToolNodeClient    // Fleet backend-agnostic sessions
	sessionID        string                    // Fleet session ID (empty for chat sessions)
	llmProvider      adrill.LLMProvider        // Optional LLM for semantic assertions
}

// NewRunDrillTool creates the run_drill tool for chat/Studio sessions.
// nodePool may be nil when sandbox is not enabled; the tool will use local execution.
// tplRegistry is optional — when set, auto-switching to the suite template also
// injects that template's bootstrap_files.
// browserMgr should be the chat-wired Manager (SandboxEnabled + container callbacks).
// Browser steps refuse to launch host Chromium when browserMgr is nil or unwired.
// llmProvider is optional — when set, semantic assertions (assert.type: "semantic")
// will use it to evaluate whether tool output satisfies the expected condition.
func NewRunDrillTool(nodePool *sandbox.NodeClientPool, tplRegistry *sandbox.TemplateRegistry, browserMgr *browser.Manager, llmProvider adrill.LLMProvider) (tool.Tool, error) {
	deps := &runDrillDeps{
		nodePool:         nodePool,
		templateRegistry: tplRegistry,
		browserMgr:       browserMgr,
		llmProvider:      llmProvider,
	}
	return newRunDrillToolFromDeps(deps)
}

// NewRunDrillToolWithClient creates the run_drill tool for fleet sessions
// with a dedicated LazyNodeClient that routes into the fleet's container.
// browserMgr must be wired for in-container Chromium (same session as lazyClient).
func NewRunDrillToolWithClient(lazyClient *sandbox.LazyNodeClient, sessionID string, browserMgr *browser.Manager, llmProvider adrill.LLMProvider) (tool.Tool, error) {
	deps := &runDrillDeps{
		lazyClient:  lazyClient,
		sessionID:   sessionID,
		browserMgr:  browserMgr,
		llmProvider: llmProvider,
	}
	return newRunDrillToolFromDeps(deps)
}

// NewRunDrillToolWithToolClient creates run_drill for fleet sessions using a
// backend-agnostic ToolNodeClient (OpenShell/K8s fleet path).
// browserMgr must be wired for in-container Chromium when browser steps are used.
func NewRunDrillToolWithToolClient(client sandbox.ToolNodeClient, sessionID string, browserMgr *browser.Manager, llmProvider adrill.LLMProvider) (tool.Tool, error) {
	deps := &runDrillDeps{
		toolClient:  client,
		sessionID:   sessionID,
		browserMgr:  browserMgr,
		llmProvider: llmProvider,
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
		Description: "Run a deterministic drill suite (or a single drill with test_name). " +
			"Automatically handles setup, ready_check, and teardown from the suite config — " +
			"do NOT manually start services before calling this tool. " +
			"When the suite declares a sandbox template and the current session is on a different " +
			"template (typically @base), run_drill switches to the suite template first, then runs. " +
			"Pass force=true only if the user explicitly wants to stay on the current container. " +
			"Shell, file, and browser tool steps all run inside the sandbox container " +
			"(browser via Chromium+KasmVNC in the session — same as chat). Use localhost in URLs. " +
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

	// Discover the suite from the team-scoped flow store (drills are team-only).
	var suite *adrill.LoadedSuite
	fs := getDrillFlowStore(ctx)
	if fs == nil {
		return RunDrillResult{
			Status:  "error",
			Summary: "Drill management requires platform mode (team-scoped store not available)",
		}, nil
	}

	var loadErr error
	suite, loadErr = adrill.LoadSuiteFromStore(fs, ctx, suiteName)
	if loadErr != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("Suite %q not found: %v", suiteName, loadErr),
		}, nil
	}

	// Validate the suite
	if err := adrill.ValidateSuite(suite); err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("Invalid suite: %v", err),
		}, nil
	}

	// Template auto-switch (chat mode with sandbox only).
	// Fleet sessions skip this — the container is already provisioned for the fleet.
	// No-sandbox sessions skip this — template is irrelevant for local execution.
	// By default, switch to the suite's template before running so drills never
	// start against @base when the suite requires a project template.
	suiteTemplate := ""
	if suite.Config != nil && suite.Config.SuiteConfig != nil {
		suiteTemplate = suite.Config.SuiteConfig.Template
	}
	if suiteTemplate == "" && suite.Config != nil {
		suiteTemplate = suite.Config.Template
	}
	if err := ensureDrillSandboxTemplate(ctx, deps, suiteName, suiteTemplate, args.Force); err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: err.Error(),
		}, nil
	}

	// Inject suite (or fleet-plan fallback) credentials before configure/setup.
	if err := injectDrillCredentials(ctx, deps, suiteName, suite); err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: fmt.Sprintf("credential injection failed: %v", err),
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

	// Filter to a single test by name if requested
	if args.TestName != "" {
		testName := strings.TrimSuffix(strings.TrimSuffix(args.TestName, ".yaml"), ".yml")
		match := adrill.FilterTestByName(tests, testName)
		if match == nil {
			return RunDrillResult{
				Status:  "error",
				Summary: fmt.Sprintf("Test %q not found in suite %q", testName, suiteName),
			}, nil
		}
		tests = []adrill.LoadedTest{*match}
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

	// Fail closed before suite setup if sandbox is active but the shared
	// browser Manager was never wired for in-container Chromium.
	if executor.hasSandbox() && (deps.browserMgr == nil || !deps.browserMgr.SandboxEnabled) {
		return RunDrillResult{
			Status: "error",
			Summary: "Sandbox is active but the in-container browser is not wired " +
				"(SandboxEnabled=false). Restart Studio after upgrading; do not rewrite " +
				"drill URLs to the container bridge IP — browser steps use Chromium inside the session.",
		}, nil
	}

	// CONTAINER_IP is for optional {{CONTAINER_IP}} placeholders in older drills.
	// Prefer localhost in YAML: shell and in-container browser share the sandbox network.
	vars := map[string]string{
		"CONTAINER_IP": "localhost", // default for local mode
	}
	if executor.hasSandbox() {
		if ip, err := executor.containerIP(); err == nil && ip != "" {
			vars["CONTAINER_IP"] = ip
		}
	}

	// Create artifact manager
	reportsDir, err := config.GetReportsDir()
	if err != nil {
		slog.Warn("failed to get reports directory", "error", err)
	}
	reportDir := filepath.Join(reportsDir, suiteName)
	am, amErr := adrill.NewArtifactManager(reportDir, suiteName)
	if amErr != nil {
		am = nil // non-fatal
	}

	// Run the suite
	runner := adrill.NewSuiteRunner(executor, am, args.Verbose)
	runner.SetVars(vars)
	if deps.llmProvider != nil {
		runner.SetLLMProvider(deps.llmProvider)
	}
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

	// Platform mode: persist to the team-scoped drill report store (PostgreSQL)
	if rptStore := getDrillReportStore(ctx); rptStore != nil {
		reportJSON, jsonErr := json.Marshal(report)
		if jsonErr == nil {
			if storeErr := rptStore.SaveReport(ctx, &store.DrillReport{
				Suite:      suiteName,
				Status:     report.Status,
				Summary:    report.Summary,
				DurationMs: report.Duration,
				ReportData: reportJSON,
				StartedAt:  report.StartedAt,
				FinishedAt: report.FinishedAt,
			}); storeErr != nil {
				slog.Warn("failed to save drill report to store", "suite", suiteName, "error", storeErr)
			}
		}
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
	"browser_request_human": true,
}

// closableExecutor extends ToolExecutor with a Close method and sandbox check.
type closableExecutor interface {
	adrill.ToolExecutor
	Close()
	hasSandbox() bool
	// containerIP returns the container's bridge IPv4 address via the Incus API.
	// Returns empty string and error when sandbox is not active.
	containerIP() (string, error)
}

// buildTestExecutor creates the appropriate executor for the run_drill tool.
func buildTestExecutor(ctx tool.Context, deps *runDrillDeps) closableExecutor {
	var toolClient sandbox.ToolNodeClient
	var sessionID string
	var ipClient *sandbox.LazyNodeClient

	if deps.toolClient != nil {
		toolClient = deps.toolClient
		sessionID = deps.sessionID
	} else if deps.lazyClient != nil {
		toolClient = deps.lazyClient
		sessionID = deps.sessionID
		ipClient = deps.lazyClient
	} else if deps.nodePool != nil && ctx != nil && ctx.SessionID() != "" {
		toolClient = deps.nodePool.GetOrCreate(ctx.SessionID())
		sessionID = ctx.SessionID()
	}

	hasSandbox := toolClient != nil
	browserExec := newTestBrowserExecutor(deps.browserMgr, sessionID, hasSandbox)

	if toolClient != nil {
		return &testCompositeExecutor{
			sandbox: &testSandboxExecutor{
				client:    toolClient,
				sessionID: sessionID,
				ipClient:  ipClient,
			},
			browser: browserExec,
			local:   &testLocalExecutor{},
		}
	}

	// No sandbox: shell/file run locally; browser still requires in-container mgr.
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

func (c *testCompositeExecutor) containerIP() (string, error) {
	if c.sandbox == nil {
		return "", fmt.Errorf("no sandbox active")
	}
	if c.sandbox.ipClient != nil {
		return c.sandbox.ipClient.GetContainerIP(c.sandbox.sessionID)
	}
	return "", fmt.Errorf("container IP not available for this sandbox backend")
}

// testSandboxExecutor proxies container tool calls through a ToolNodeClient.
type testSandboxExecutor struct {
	client    sandbox.ToolNodeClient
	sessionID string
	ipClient  *sandbox.LazyNodeClient // optional, Incus only
}

func (e *testSandboxExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	raw, err := e.client.Call(e.sessionID, name, args)
	if err != nil {
		return nil, fmt.Errorf("sandbox call %s: %w", name, err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal sandbox result for %s: %w", name, err)
	}
	return result, nil
}

// testLocalExecutor dispatches to tools.ExecuteTool for local execution.
type testLocalExecutor struct{}

func (e *testLocalExecutor) Execute(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	return ExecuteTool(ctx, name, args)
}

// testBrowserExecutor dispatches browser tool calls through a Manager that is
// already wired for in-container Chromium (same path as Studio chat). It never
// launches host Chrome.
type testBrowserExecutor struct {
	mu             sync.Mutex
	mgr            *browser.Manager
	guard          *browser.NavigationGuard
	refs           *browser.RefMap
	sessionID      string
	initialized    bool
}

func newTestBrowserExecutor(mgr *browser.Manager, sessionID string, _ bool) *testBrowserExecutor {
	return &testBrowserExecutor{
		mgr:       mgr,
		sessionID: sessionID,
	}
}

func (b *testBrowserExecutor) ensureInit() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initialized {
		if b.mgr != nil && b.sessionID != "" {
			b.mgr.EnsureSessionID(b.sessionID)
		}
		return nil
	}
	if b.mgr == nil || !b.mgr.SandboxEnabled {
		return fmt.Errorf("browser drills require an in-container browser (sandbox Chromium+KasmVNC); host Chromium is disabled — restart Studio if you recently upgraded")
	}
	if b.mgr.ContainerResolveFunc == nil || b.mgr.ContainerStartBrowserFunc == nil {
		return fmt.Errorf("browser Manager has SandboxEnabled but missing container callbacks; in-container Chromium is not wired")
	}
	b.guard = &browser.NavigationGuard{BlockPrivateNetworks: false}
	b.refs = browser.NewRefMap()
	if b.sessionID != "" {
		b.mgr.EnsureSessionID(b.sessionID)
	}
	b.initialized = true
	return nil
}

func (b *testBrowserExecutor) Close() {
	// Manager is owned by chat/fleet/CLI wiring — do not Cleanup here.
}

func (b *testBrowserExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	if err := b.ensureInit(); err != nil {
		return nil, err
	}

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
		buf.WriteString(fmt.Sprintf("\nTest: %s (%s)\n", test.Name, test.Status))
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

	// Add actionable hint for fixing failures
	hasTestFailures := false
	for _, test := range report.Tests {
		if test.Status == "failed" {
			hasTestFailures = true
			break
		}
	}
	if hasTestFailures {
		buf.WriteString("\n--- How to Fix ---\n")
		buf.WriteString("To fix failing drills: use read_drill to inspect the drill YAML, investigate the app code\n")
		buf.WriteString("to understand actual behavior, then use edit_drill to update the assertions or test logic.\n")
		buf.WriteString("Re-run with run_drill to verify the fix.\n")
	}
}

// templateDisplay returns a user-friendly display name for a sandbox template.
// Empty string (the default base container) is rendered as "@base".
func templateDisplay(t string) string {
	n := normalizeSandboxTemplateName(t)
	if n == "" {
		return "@base"
	}
	return n
}

// normalizeSandboxTemplateName canonicalizes template names for comparison.
// Strips a leading "@" and treats "" / "base" as the default @base sandbox.
func normalizeSandboxTemplateName(t string) string {
	t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "@"))
	if t == "" || t == "base" {
		return ""
	}
	return t
}

// injectDrillCredentials materializes suite credential_injection (or a fleet-plan
// fallback) into the active sandbox before configure/setup runs.
func injectDrillCredentials(ctx tool.Context, deps *runDrillDeps, suiteName string, suite *adrill.LoadedSuite) error {
	if suite == nil || suite.Config == nil {
		return nil
	}
	sc := suite.Config.SuiteConfig
	goCtx := context.Background()
	if ctx != nil {
		goCtx = ctx
	}

	planStore := store.FleetPlanStoreFromContext(goCtx)
	spec, err := adrill.ResolveInjectionSpec(goCtx, suiteName, sc, planStore)
	if err != nil {
		return err
	}
	if spec == nil || !spec.HasWork() {
		return nil
	}

	cs := getEffectiveCredStore(goCtx)
	fileStore := GetCredentialStore()

	target, err := buildDrillInjectionTarget(ctx, deps)
	if err != nil {
		return err
	}
	if target.SessionID == "" && target.LazyClient == nil && target.Backend == nil && target.ExecIncus == nil {
		slog.Warn("run_drill skipping credential injection: no sandbox target",
			"component", "run-drill", "suite", suiteName)
		return nil
	}

	_, err = adrill.ApplyCredentialInjection(goCtx, spec, cs, fileStore, target)
	if err != nil {
		return err
	}
	slog.Info("run_drill applied credential injection",
		"component", "run-drill",
		"suite", suiteName,
		"owner", spec.OwnerKey,
		"env_count", len(spec.Injection.Env),
		"file_count", len(spec.Injection.Files),
	)
	return nil
}

func buildDrillInjectionTarget(ctx tool.Context, deps *runDrillDeps) (adrill.InjectionTarget, error) {
	target := adrill.InjectionTarget{}
	if deps == nil {
		return target, nil
	}

	if deps.lazyClient != nil {
		target.LazyClient = deps.lazyClient
		target.SessionID = deps.sessionID
		if target.SessionID == "" && ctx != nil {
			target.SessionID = ctx.SessionID()
		}
		if _, err := deps.lazyClient.EnsureContainerReady(target.SessionID); err != nil {
			return target, fmt.Errorf("sandbox not ready for credential injection: %w", err)
		}
		client := deps.lazyClient.GetIncusClient()
		containerName := deps.lazyClient.GetContainerName()
		if client != nil && containerName != "" {
			target.ExecIncus = func(command []string, env map[string]string) ([]byte, []byte, int, error) {
				out, err := sandbox.ExecSimpleWithEnv(client, containerName, command, env)
				if err != nil {
					return nil, nil, -1, err
				}
				return []byte(out), nil, 0, nil
			}
		}
		return target, nil
	}

	if deps.toolClient != nil {
		target.SessionID = deps.sessionID
		if target.SessionID == "" && ctx != nil {
			target.SessionID = ctx.SessionID()
		}
		_ = deps.toolClient.EnsureReady(target.SessionID)
		type backendProvider interface {
			GetBackend() sandbox.Backend
		}
		if bp, ok := deps.toolClient.(backendProvider); ok {
			target.Backend = bp.GetBackend()
		}
		return target, nil
	}

	if deps.nodePool != nil && ctx != nil && ctx.SessionID() != "" {
		sessionID := ctx.SessionID()
		target.SessionID = sessionID
		lazy := deps.nodePool.GetOrCreate(sessionID)
		if lazy != nil {
			target.LazyClient = lazy
			if _, err := lazy.EnsureContainerReady(sessionID); err != nil {
				return target, fmt.Errorf("sandbox not ready for credential injection: %w", err)
			}
			client := lazy.GetIncusClient()
			containerName := lazy.GetContainerName()
			if client != nil && containerName != "" {
				target.ExecIncus = func(command []string, env map[string]string) ([]byte, []byte, int, error) {
					out, err := sandbox.ExecSimpleWithEnv(client, containerName, command, env)
					if err != nil {
						return nil, nil, -1, err
					}
					return []byte(out), nil, 0, nil
				}
			}
		}
		if backend := deps.nodePool.GetBackend(); backend != nil && target.ExecIncus == nil {
			target.Backend = backend
		}
	}
	return target, nil
}

// ensureDrillSandboxTemplate switches the chat sandbox to the suite's required
// template before execution. Fleet / no-sandbox sessions are no-ops. force=true
// skips the switch so the suite can run on the current container.
func ensureDrillSandboxTemplate(ctx tool.Context, deps *runDrillDeps, suiteName, suiteTemplate string, force bool) error {
	required := normalizeSandboxTemplateName(suiteTemplate)
	if required == "" || force {
		return nil
	}
	// Fleet sessions already have a provisioned container.
	if deps.lazyClient != nil || deps.toolClient != nil || deps.nodePool == nil {
		return nil
	}
	if ctx == nil || ctx.SessionID() == "" {
		return nil
	}

	sessionID := ctx.SessionID()
	lazyClient := deps.nodePool.GetOrCreate(sessionID)
	if lazyClient == nil {
		return nil
	}

	current := normalizeSandboxTemplateName(lazyClient.Template())
	if current == required {
		return nil
	}

	slog.Info("run_drill switching sandbox template",
		"component", "run-drill",
		"suite", suiteName,
		"from", templateDisplay(current),
		"to", templateDisplay(required),
	)
	if err := deps.nodePool.ReplaceSession(sessionID, required); err != nil {
		return fmt.Errorf(
			"suite %q requires template %s (current sandbox is %s) but switching failed: %v",
			suiteName, templateDisplay(required), templateDisplay(current), err,
		)
	}

	// Eagerly create the new container so setup commands have a ready sandbox,
	// then inject template bootstrap_files (same path as use_sandbox_template).
	if client := deps.nodePool.GetOrCreate(sessionID); client != nil {
		if _, err := client.GetContainerIP(sessionID); err != nil {
			slog.Warn("run_drill template switch: container IP not ready yet",
				"component", "run-drill", "suite", suiteName, "template", required, "error", err)
		}
		sandbox.InjectBootstrapFilesAfterSwitch(deps.nodePool, deps.templateRegistry, sessionID, required)
	}
	return nil
}
