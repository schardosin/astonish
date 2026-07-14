package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
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
	Force     bool   `json:"force,omitempty" jsonschema:"Stay on the current sandbox even when the suite template name differs. Preferred when a prepared workspace already has start-services.sh; do not use force to recover a missing script (rewrite/restore the script instead)."`
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
			"Preserves a prepared sandbox that already has the suite's start-services.sh. " +
			"Only auto-switches from @base when the suite template is registered and the start " +
			"script is missing on the current container. Pass force=true to stay on the current " +
			"sandbox when the suite template name differs. If start-services.sh is missing, restore " +
			"or rewrite it (and ensure template bootstrap_files) — do not switch templates to fix it. " +
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
	// Prefer preserving a prepared workspace; only switch from @base when needed.
	suiteTemplate := ""
	var suiteCfg *config.DrillSuiteConfig
	if suite.Config != nil && suite.Config.SuiteConfig != nil {
		suiteCfg = suite.Config.SuiteConfig
		suiteTemplate = suiteCfg.Template
	}
	if suiteTemplate == "" && suite.Config != nil {
		suiteTemplate = suite.Config.Template
	}
	if err := ensureDrillSandboxTemplate(ctx, deps, suiteName, suiteTemplate, suiteCfg, args.Force); err != nil {
		return RunDrillResult{
			Status:  "error",
			Summary: err.Error(),
		}, nil
	}

	// Ensure setup will find start-services.sh (inject bootstrap or clear error).
	if err := preflightDrillStartServices(ctx, deps, suiteName, suiteTemplate, suiteCfg); err != nil {
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
	return ExecuteTool(ctx, name, args, currentCaller)
}

// testBrowserExecutor dispatches browser tool calls through a Manager that is
// already wired for in-container Chromium (same path as Studio chat). It never
// launches host Chrome.
type testBrowserExecutor struct {
	mu          sync.Mutex
	mgr         *browser.Manager
	guard       *browser.NavigationGuard
	refs        *browser.RefMap
	sessionID   string
	initialized bool
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

// drillTemplateSwitchAction is the decision result for ensureDrillSandboxTemplate.
type drillTemplateSwitchAction int

const (
	drillSwitchNoop drillTemplateSwitchAction = iota
	drillSwitchPreserve
	drillSwitchReplace
	drillSwitchSkipMissingTemplate
)

// decideDrillTemplateSwitch chooses whether to ReplaceSession.
//
// Policy: never tear down a prepared workspace. Only switch when the current
// sandbox is @base/empty, the required template is registered, and the suite's
// start-services.sh is not already on the current filesystem.
func decideDrillTemplateSwitch(current, required string, requiredExists, startScriptPresent bool) drillTemplateSwitchAction {
	if required == "" || current == required {
		return drillSwitchNoop
	}
	if startScriptPresent {
		return drillSwitchPreserve
	}
	if !requiredExists {
		return drillSwitchSkipMissingTemplate
	}
	// current != "" means a non-base template is already bound — preserve it.
	if current != "" {
		return drillSwitchPreserve
	}
	return drillSwitchReplace
}

var startServicesPathRE = regexp.MustCompile(`/[^\s;'"]+/\.astonish/start-services\.sh`)

// extractStartServicesPaths returns absolute paths to start-services.sh referenced
// by suite setup (legacy) or services[].setup commands.
func extractStartServicesPaths(sc *config.DrillSuiteConfig) []string {
	if sc == nil {
		return nil
	}
	var cmds []string
	cmds = append(cmds, sc.Setup...)
	for _, svc := range sc.Services {
		if strings.TrimSpace(svc.Setup) != "" {
			cmds = append(cmds, svc.Setup)
		}
	}
	seen := make(map[string]bool)
	var paths []string
	for _, cmd := range cmds {
		for _, p := range startServicesPathRE.FindAllString(cmd, -1) {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// suiteReferencesStartServices reports whether setup/services mention start-services.sh
// (absolute path preferred; also catches relative references).
func suiteReferencesStartServices(sc *config.DrillSuiteConfig) bool {
	if sc == nil {
		return false
	}
	if len(extractStartServicesPaths(sc)) > 0 {
		return true
	}
	check := func(s string) bool {
		return strings.Contains(s, "start-services.sh")
	}
	for _, c := range sc.Setup {
		if check(c) {
			return true
		}
	}
	for _, svc := range sc.Services {
		if check(svc.Setup) {
			return true
		}
	}
	return false
}

func drillLazyPathExists(lazy *sandbox.LazyNodeClient, sessionID, path string) bool {
	if lazy == nil || path == "" {
		return false
	}
	containerName, err := lazy.EnsureContainerReady(sessionID)
	if err != nil || containerName == "" {
		return false
	}
	client := lazy.GetIncusClient()
	if client == nil {
		return false
	}
	exitCode, err := client.ExecSimple(containerName, []string{"test", "-f", path})
	return err == nil && exitCode == 0
}

func drillPathExists(ctx tool.Context, deps *runDrillDeps, path string) bool {
	if deps == nil || path == "" {
		return false
	}
	sessionID := ""
	if ctx != nil {
		sessionID = ctx.SessionID()
	}
	if deps.sessionID != "" {
		sessionID = deps.sessionID
	}
	if deps.lazyClient != nil {
		return drillLazyPathExists(deps.lazyClient, sessionID, path)
	}
	if deps.toolClient != nil && sessionID != "" {
		raw, err := deps.toolClient.Call(sessionID, "shell_command", map[string]interface{}{
			"command": fmt.Sprintf("test -f %s", shellSingleQuote(path)),
		})
		if err != nil {
			return false
		}
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) != nil {
			return false
		}
		if code, ok := parsed["exit_code"].(float64); ok {
			return code == 0
		}
		// Some adapters omit exit_code on success.
		if errMsg, ok := parsed["error"].(string); ok && errMsg != "" {
			return false
		}
		return true
	}
	if deps.nodePool != nil && sessionID != "" {
		lazy := deps.nodePool.GetOrCreate(sessionID)
		return drillLazyPathExists(lazy, sessionID, path)
	}
	return false
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func anyStartServicesPathExists(ctx tool.Context, deps *runDrillDeps, paths []string) bool {
	for _, p := range paths {
		if drillPathExists(ctx, deps, p) {
			return true
		}
	}
	return false
}

func templateRegistered(deps *runDrillDeps, name string) bool {
	name = normalizeSandboxTemplateName(name)
	if name == "" || deps == nil || deps.templateRegistry == nil {
		return false
	}
	_ = deps.templateRegistry.Load()
	return deps.templateRegistry.Exists(name)
}

func injectDrillBootstrapFiles(ctx tool.Context, deps *runDrillDeps, templateName string) error {
	if deps == nil {
		return nil
	}
	required := normalizeSandboxTemplateName(templateName)
	if required == "" {
		return nil
	}
	goCtx := context.Background()
	if ctx != nil {
		goCtx = ctx
	}
	files := sandbox.LookupBootstrapFiles(goCtx, deps.templateRegistry, nil, required)
	if len(files) == 0 {
		return nil
	}

	target, err := buildDrillInjectionTarget(ctx, deps)
	if err != nil {
		return err
	}
	if target.ExecIncus != nil {
		return sandbox.MaterializeBootstrapFilesIncus(goCtx, target.ExecIncus, files)
	}
	if target.Backend != nil && target.SessionID != "" {
		return sandbox.MaterializeBootstrapFiles(goCtx, target.Backend, target.SessionID, files)
	}
	if deps.nodePool != nil {
		sessionID := target.SessionID
		if sessionID == "" && ctx != nil {
			sessionID = ctx.SessionID()
		}
		if sessionID != "" {
			sandbox.InjectBootstrapFilesAfterSwitch(deps.nodePool, deps.templateRegistry, sessionID, required)
		}
	}
	return nil
}

// preflightDrillStartServices ensures start-services.sh exists when the suite
// references it. Tries bootstrap_files injection first; otherwise returns a
// clear error (do not suggest template switching).
func preflightDrillStartServices(ctx tool.Context, deps *runDrillDeps, suiteName, suiteTemplate string, sc *config.DrillSuiteConfig) error {
	if !suiteReferencesStartServices(sc) {
		return nil
	}
	// No sandbox at all — local runners resolve paths differently; skip.
	if deps == nil || (deps.nodePool == nil && deps.lazyClient == nil && deps.toolClient == nil) {
		return nil
	}

	paths := extractStartServicesPaths(sc)
	if anyStartServicesPathExists(ctx, deps, paths) {
		return nil
	}
	// Relative references: still attempt bootstrap inject for the suite template.
	if err := injectDrillBootstrapFiles(ctx, deps, suiteTemplate); err != nil {
		slog.Warn("run_drill preflight bootstrap inject failed",
			"component", "run-drill", "suite", suiteName, "error", err)
	}
	if len(paths) > 0 && anyStartServicesPathExists(ctx, deps, paths) {
		return nil
	}
	// Bootstrap may have injected under a known path even when YAML used relative refs.
	if len(paths) == 0 {
		// Re-check common bootstrap targets from the registry.
		required := normalizeSandboxTemplateName(suiteTemplate)
		if files := sandbox.LookupBootstrapFiles(context.Background(), deps.templateRegistry, nil, required); len(files) > 0 {
			for _, f := range files {
				if strings.HasSuffix(f.Path, "start-services.sh") && drillPathExists(ctx, deps, f.Path) {
					return nil
				}
			}
		}
	}

	hintPath := "<workspace>/.astonish/start-services.sh"
	if len(paths) > 0 {
		hintPath = paths[0]
	}
	return fmt.Errorf(
		"suite %q setup references start-services.sh but %s is missing in the sandbox. "+
			"Write or restore that script (and save it on the template via bootstrap_files) before run_drill. "+
			"Do not switch sandbox templates to fix a missing script",
		suiteName, hintPath,
	)
}

// ensureDrillSandboxTemplate switches the chat sandbox to the suite's required
// template only when safe. Fleet / no-sandbox sessions are no-ops. force=true
// skips the switch so the suite can run on the current container.
func ensureDrillSandboxTemplate(ctx tool.Context, deps *runDrillDeps, suiteName, suiteTemplate string, sc *config.DrillSuiteConfig, force bool) error {
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
	paths := extractStartServicesPaths(sc)
	startPresent := anyStartServicesPathExists(ctx, deps, paths)
	requiredExists := templateRegistered(deps, required)

	switch decideDrillTemplateSwitch(current, required, requiredExists, startPresent) {
	case drillSwitchNoop:
		return nil
	case drillSwitchPreserve:
		slog.Info("run_drill preserving prepared sandbox",
			"component", "run-drill",
			"suite", suiteName,
			"current", templateDisplay(current),
			"required", templateDisplay(required),
			"start_script_present", startPresent,
		)
		return nil
	case drillSwitchSkipMissingTemplate:
		slog.Info("run_drill skipping template switch: required template not registered",
			"component", "run-drill",
			"suite", suiteName,
			"current", templateDisplay(current),
			"required", templateDisplay(required),
		)
		return nil
	case drillSwitchReplace:
		// fall through
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

	// Eagerly create with the required template (never bare GetOrCreate → @base).
	client := deps.nodePool.GetOrCreateWithTemplate(sessionID, required)
	if client == nil {
		return fmt.Errorf(
			"suite %q switched to template %s but sandbox client is unavailable",
			suiteName, templateDisplay(required),
		)
	}
	if _, err := client.GetContainerIP(sessionID); err != nil {
		return fmt.Errorf(
			"suite %q switched to template %s but the sandbox failed to become ready: %v. "+
				"Fix the template or restore start-services.sh on the prepared workspace — "+
				"do not continue on an empty @base container",
			suiteName, templateDisplay(required), err,
		)
	}
	if err := injectDrillBootstrapFiles(ctx, deps, required); err != nil {
		return fmt.Errorf("suite %q: bootstrap_files injection after template switch failed: %w", suiteName, err)
	}
	return nil
}
