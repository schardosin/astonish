package tools

import (
	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// GetBrowserTools creates all browser automation tools sharing a single
// BrowserManager instance. The browser is launched lazily on first use.
func GetBrowserTools(mgr *browser.Manager) ([]tool.Tool, error) {
	guard := browser.DefaultNavigationGuard()
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
		Name: "browser_snapshot",
		Description: `Capture an accessibility snapshot of the current page. Returns a text tree with ref IDs that can be used for interaction (browser_click, browser_type, etc.). This is better than screenshots for understanding page structure.

Use mode="efficient" for large pages (compact, interactive-only, depth=6, maxChars=10000).`,
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
		Description: "Wait for a condition: text to appear/disappear, CSS selector to be visible, URL to match, page load state, or a JavaScript expression to be truthy.",
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
		Description: "Handle a JavaScript dialog (alert, confirm, prompt). Must be called before the action that triggers the dialog.",
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
		evaluateTool, runCodeTool, pdfTool,
	}, nil
}
