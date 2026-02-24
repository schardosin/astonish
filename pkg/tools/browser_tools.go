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

	responseBodyTool, err := functiontool.New(functiontool.Config{
		Name: "browser_response_body",
		Description: `Intercept and read HTTP response bodies from network requests.

Three-step workflow:
1. action="listen" urlPattern="*api/data*" — start intercepting matching responses
2. Trigger the action that produces the request (click, navigate, etc.)
3. action="read" — retrieve the captured response body

Use action="stop" to remove the interceptor when done.`,
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
		Name: "browser_set_device",
		Description: `Emulate a mobile or tablet device. Sets viewport, user agent, touch, and DPR.

Available devices: iPhone 4, iPhone 5/SE, iPhone 6/7/8, iPhone 6/7/8 Plus, iPhone X, iPad, iPad Mini, iPad Pro, Pixel 2, Pixel 2 XL, Galaxy S5, Galaxy S III, Galaxy Note 3, Galaxy Fold, Nexus 5, Nexus 6, Moto G4, Surface Duo, Kindle Fire HDX, and more.

Use device="clear" to remove device emulation.`,
	}, BrowserSetDevice(mgr))
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
	}, nil
}
