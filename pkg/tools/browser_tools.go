package tools

import (
	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// GetBrowserTools creates all browser automation tools sharing a single
// BrowserManager instance. The browser is launched lazily on first use.
// Navigation to private/localhost IPs is blocked (SSRF protection).
func GetBrowserTools(mgr *browser.Manager) ([]tool.Tool, error) {
	guard := browser.DefaultNavigationGuard()
	return getBrowserToolsWithGuard(mgr, guard)
}

// GetBrowserToolsForSandbox creates browser tools with private network
// blocking disabled. When sandbox is enabled, services run inside containers
// on private bridge IPs (e.g., 10.99.0.x) and the browser (on the host)
// must be able to navigate to them.
func GetBrowserToolsForSandbox(mgr *browser.Manager) ([]tool.Tool, error) {
	guard := &browser.NavigationGuard{BlockPrivateNetworks: false}
	return getBrowserToolsWithGuard(mgr, guard)
}

func getBrowserToolsWithGuard(mgr *browser.Manager, guard *browser.NavigationGuard) ([]tool.Tool, error) {
	refs := browser.NewRefMap()

	// --- Navigation ---

	navigateTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_navigate",
		Description: "Navigate the browser to a URL. The browser launches automatically on first use.",
	}, BrowserNavigate(mgr, guard))
	if err != nil {
		return nil, err
	}

	navigateBackTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_navigate_back",
		Description: "Go back to the previous page in the browser history.",
	}, BrowserNavigateBack(mgr))
	if err != nil {
		return nil, err
	}

	// --- Interaction ---

	clickTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_click",
		Description: "Click an element on the page. Use a ref from browser_snapshot.",
	}, BrowserClick(mgr, refs))
	if err != nil {
		return nil, err
	}

	typeTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_type",
		Description: "Type text into an editable element (input, textarea). Clears existing content first.",
	}, BrowserType(mgr, refs))
	if err != nil {
		return nil, err
	}

	hoverTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_hover",
		Description: "Hover over an element on the page to trigger hover effects or tooltips.",
	}, BrowserHover(mgr, refs))
	if err != nil {
		return nil, err
	}

	dragTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_drag",
		Description: "Drag an element from one position to another.",
	}, BrowserDrag(mgr, refs))
	if err != nil {
		return nil, err
	}

	pressKeyTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_press_key",
		Description: "Press a keyboard key (e.g. Enter, Tab, Escape, ArrowDown). Works on the focused element.",
	}, BrowserPressKey(mgr))
	if err != nil {
		return nil, err
	}

	selectOptionTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_select_option",
		Description: "Select option(s) from a dropdown select element by visible text.",
	}, BrowserSelectOption(mgr, refs))
	if err != nil {
		return nil, err
	}

	fillFormTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_fill_form",
		Description: "Fill multiple form fields at once. Pass an array of {ref, value} pairs.",
	}, BrowserFillForm(mgr, refs))
	if err != nil {
		return nil, err
	}

	// --- Observation ---

	snapshotTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_snapshot",
		Description: "Capture an accessibility snapshot of the current page. Returns a text tree with ref IDs for interaction. Use mode=\"efficient\" for large pages (compact, interactive-only, depth=6, maxChars=10000).",
	}, BrowserSnapshot(mgr, refs))
	if err != nil {
		return nil, err
	}

	screenshotTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_take_screenshot",
		Description: "Take a screenshot of the current page. You can't perform actions based on the screenshot — use browser_snapshot for that.",
	}, BrowserTakeScreenshot(mgr, refs))
	if err != nil {
		return nil, err
	}

	consoleTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_console_messages",
		Description: "Returns console messages (log, warn, error, debug) from the current page.",
	}, BrowserConsoleMessages(mgr))
	if err != nil {
		return nil, err
	}

	networkTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_network_requests",
		Description: "Returns network requests made by the current page. Useful for debugging API calls.",
	}, BrowserNetworkRequests(mgr))
	if err != nil {
		return nil, err
	}

	// --- Management ---

	tabsTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_tabs",
		Description: "Manage browser tabs: list, new, close, or select. Use targetId from list results for close/select.",
	}, BrowserTabs(mgr, guard))
	if err != nil {
		return nil, err
	}

	closeTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_close",
		Description: "Close the current browser page/tab.",
	}, BrowserClose(mgr))
	if err != nil {
		return nil, err
	}

	resizeTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_resize",
		Description: "Resize the browser viewport to the specified width and height in pixels.",
	}, BrowserResize(mgr))
	if err != nil {
		return nil, err
	}

	waitForTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_wait_for",
		Description: "Wait for a condition: text to appear/disappear, CSS selector visible, URL match, page load state, or JS expression truthy. Avoid state=\"networkidle\" on SPAs (persistent WebSocket connections cause full timeout).",
	}, BrowserWaitFor(mgr))
	if err != nil {
		return nil, err
	}

	fileUploadTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_file_upload",
		Description: "Upload files to a file input element on the page.",
	}, BrowserFileUpload(mgr, refs))
	if err != nil {
		return nil, err
	}

	handleDialogTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_handle_dialog",
		Description: "Handle a native JS dialog (alert, confirm, prompt). Must be called before the triggering action. Only works for native dialogs — use browser_snapshot for custom modals.",
	}, BrowserHandleDialog(mgr))
	if err != nil {
		return nil, err
	}

	// --- Advanced ---

	evaluateTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_evaluate",
		Description: "Evaluate a JavaScript expression in the page context. Returns the result. Optionally scope to an element via ref.",
	}, BrowserEvaluate(mgr, refs))
	if err != nil {
		return nil, err
	}

	runCodeTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_run_code",
		Description: "Run a multi-line JavaScript code snippet in the page context. Supports async/await. Has access to the full browser DOM API.",
	}, BrowserRunCode(mgr))
	if err != nil {
		return nil, err
	}

	pdfTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_pdf",
		Description: "Save the current page as a PDF file. Returns the file path.",
	}, BrowserPDF(mgr))
	if err != nil {
		return nil, err
	}

	responseBodyTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_response_body",
		Description: "Intercept and read HTTP response bodies. Workflow: action=\"listen\" urlPattern=\"*api/data*\" → trigger request → action=\"read\" to get body. Use action=\"stop\" to remove interceptor.",
	}, BrowserResponseBody(mgr))
	if err != nil {
		return nil, err
	}

	// --- State & Emulation ---

	cookiesTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_cookies",
		Description: "Get, set, or clear browser cookies. Use action=get to inspect, action=set to add/modify, action=clear to remove all.",
	}, BrowserCookies(mgr))
	if err != nil {
		return nil, err
	}

	storageTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_storage",
		Description: "Read, write, or clear localStorage/sessionStorage. Use kind=local or kind=session with action=get/getAll/set/clear.",
	}, BrowserStorage(mgr))
	if err != nil {
		return nil, err
	}

	setOfflineTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_offline",
		Description: "Enable or disable network connectivity for the browser page. Simulates offline mode.",
	}, BrowserSetOffline(mgr))
	if err != nil {
		return nil, err
	}

	setHeadersTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_headers",
		Description: "Add extra HTTP headers to all requests from this page. Pass an empty map to clear.",
	}, BrowserSetHeaders(mgr))
	if err != nil {
		return nil, err
	}

	setCredentialsTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_credentials",
		Description: "Set HTTP Basic Auth credentials. The browser will automatically respond to auth challenges.",
	}, BrowserSetCredentials(mgr))
	if err != nil {
		return nil, err
	}

	setGeolocationTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_geolocation",
		Description: "Override the browser's geolocation (latitude, longitude, accuracy).",
	}, BrowserSetGeolocation(mgr))
	if err != nil {
		return nil, err
	}

	setMediaTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_media",
		Description: "Set the preferred color scheme (dark, light, no-preference) for CSS media queries.",
	}, BrowserSetMedia(mgr))
	if err != nil {
		return nil, err
	}

	setTimezoneTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_timezone",
		Description: "Override the browser's timezone (IANA ID like 'America/New_York'). Empty to clear.",
	}, BrowserSetTimezone(mgr))
	if err != nil {
		return nil, err
	}

	setLocaleTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_locale",
		Description: "Override the browser's locale (BCP 47 like 'en-US', 'fr-FR'). Empty to clear.",
	}, BrowserSetLocale(mgr))
	if err != nil {
		return nil, err
	}

	setDeviceTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_set_device",
		Description: "Emulate a mobile/tablet device (viewport, user agent, touch, DPR). Supports iPhone, iPad, Pixel, Galaxy, etc. Use device=\"clear\" to remove emulation.",
	}, BrowserSetDevice(mgr))
	if err != nil {
		return nil, err
	}

	// --- Human-in-the-loop ---

	requestHumanTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_request_human",
		Description: "Start a browser handoff session for human intervention (CAPTCHAs, MFA, payment forms). Returns immediately with CDP connection instructions — relay these to the user, then call browser_handoff_complete.",
	}, BrowserRequestHuman(mgr))
	if err != nil {
		return nil, err
	}

	handoffCompleteTool, err := functiontool.New(functiontool.Config{
		Name:        "browser_handoff_complete",
		Description: "Wait for user to finish an active browser handoff. Call after relaying connection instructions from browser_request_human. Blocks until user signals done. Take browser_snapshot afterward.",
	}, BrowserHandoffComplete(mgr))
	if err != nil {
		return nil, err
	}

	return []tool.Tool{
		// Navigation
		navigateTool, navigateBackTool,
		// Interaction
		clickTool, typeTool, hoverTool, dragTool, pressKeyTool, selectOptionTool, fillFormTool,
		// Observation
		snapshotTool, screenshotTool, consoleTool, networkTool,
		// Management
		tabsTool, closeTool, resizeTool, waitForTool, fileUploadTool, handleDialogTool,
		// Advanced
		evaluateTool, runCodeTool, pdfTool, responseBodyTool,
		// State & Emulation
		cookiesTool, storageTool,
		setOfflineTool, setHeadersTool, setCredentialsTool,
		setGeolocationTool, setMediaTool, setTimezoneTool, setLocaleTool, setDeviceTool,
		// Human-in-the-loop
		requestHumanTool, handoffCompleteTool,
	}, nil
}
