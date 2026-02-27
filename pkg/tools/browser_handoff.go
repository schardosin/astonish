package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

// --- browser_request_human (non-blocking: starts handoff, returns immediately) ---

// BrowserRequestHumanArgs is the input for browser_request_human.
type BrowserRequestHumanArgs struct {
	Reason         string `json:"reason" jsonschema:"required,Why you need human help. Shown to the user. Be specific about what they should do (e.g. 'solve the CAPTCHA and click Submit')."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Max seconds the handoff session stays open. Default: 300 (5 minutes). Max: 600."`
}

// BrowserRequestHumanResult is the output of browser_request_human.
type BrowserRequestHumanResult struct {
	Success       bool   `json:"success"`
	ListenAddress string `json:"listen_address"` // e.g. "127.0.0.1:9222"
	PageURL       string `json:"page_url"`
	PageTitle     string `json:"page_title"`
	Message       string `json:"message"` // Full instructions for the user
}

// BrowserRequestHuman starts a CDP handoff proxy and returns immediately with
// connection instructions. The agent MUST relay these instructions to the user,
// then call browser_handoff_complete to wait for the user to finish.
func BrowserRequestHuman(mgr *browser.Manager) func(tool.Context, BrowserRequestHumanArgs) (BrowserRequestHumanResult, error) {
	return func(_ tool.Context, args BrowserRequestHumanArgs) (BrowserRequestHumanResult, error) {
		if args.Reason == "" {
			return BrowserRequestHumanResult{}, fmt.Errorf("reason is required")
		}

		timeout := args.TimeoutSeconds
		if timeout <= 0 {
			timeout = 300
		}
		if timeout > 600 {
			timeout = 600
		}

		// Ensure the browser is running
		cdpURL := mgr.CDPURL()
		if cdpURL == "" {
			if _, err := mgr.GetOrLaunch(); err != nil {
				return BrowserRequestHumanResult{}, fmt.Errorf("failed to launch browser: %w", err)
			}
			cdpURL = mgr.CDPURL()
		}
		if cdpURL == "" {
			return BrowserRequestHumanResult{}, fmt.Errorf("browser CDP endpoint not available")
		}

		// Capture current page state for context
		pageURL := ""
		pageTitle := ""
		page, err := mgr.CurrentPage()
		if err == nil {
			if pageInfo, infoErr := page.Info(); infoErr == nil && pageInfo != nil {
				pageURL = pageInfo.URL
				pageTitle = pageInfo.Title
			}
		}

		// Start the handoff server (stored on the Manager, persists across tool calls)
		handoffCfg := mgr.HandoffConfig()
		info, err := mgr.StartHandoff(browser.HandoffOpts{
			CDPURL:      cdpURL,
			Port:        handoffCfg.Port,
			BindAddress: handoffCfg.BindAddress,
			Timeout:     time.Duration(timeout) * time.Second,
			Reason:      args.Reason,
		})
		if err != nil {
			return BrowserRequestHumanResult{}, fmt.Errorf("failed to start handoff: %w", err)
		}

		// Build user-facing instructions that the agent MUST relay.
		// If the proxy binds to 0.0.0.0, guide the user to use the machine's IP.
		connectAddr := info.ListenAddress
		addrNote := ""
		if strings.HasPrefix(connectAddr, "0.0.0.0:") || strings.HasPrefix(connectAddr, "[::]:") {
			port := connectAddr[strings.LastIndex(connectAddr, ":")+1:]
			addrNote = fmt.Sprintf(
				"\nNOTE: The proxy is listening on all interfaces (port %s). "+
					"Use this machine's IP address instead of 0.0.0.0 "+
					"(e.g. 192.168.x.x:%s or your server's IP:%s).\n", port, port, port)
			connectAddr = "<this-machine-ip>:" + port
		}
		message := fmt.Sprintf(
			"HUMAN ASSISTANCE NEEDED\n\n"+
				"Reason: %s\n\n"+
				"Current page: %s\n\n"+
				"To take over the browser:\n"+
				"1. Open Chrome and go to chrome://inspect\n"+
				"2. Click 'Configure...' and add: %s\n"+
				"3. The browser tab should appear under 'Remote Target'\n"+
				"4. Click 'inspect' to open DevTools with full control\n"+
				"%s\n"+
				"When you're done, either:\n"+
				"- Close the DevTools window (auto-detected after 10s)\n"+
				"- Visit http://%s/handoff/done in any browser\n\n"+
				"IMPORTANT: After relaying these instructions to the user, call browser_handoff_complete to wait for them to finish.",
			args.Reason,
			pageURL,
			connectAddr,
			addrNote,
			info.ListenAddress,
		)

		return BrowserRequestHumanResult{
			Success:       true,
			ListenAddress: info.ListenAddress,
			PageURL:       pageURL,
			PageTitle:     pageTitle,
			Message:       message,
		}, nil
	}
}

// --- browser_handoff_complete (blocking: waits for user to finish) ---

// BrowserHandoffCompleteArgs is the input for browser_handoff_complete.
type BrowserHandoffCompleteArgs struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"Max seconds to wait for the user to finish. Default: 300 (5 minutes). Max: 600."`
}

// BrowserHandoffCompleteResult is the output of browser_handoff_complete.
type BrowserHandoffCompleteResult struct {
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	PageURL    string `json:"page_url"`
	PageTitle  string `json:"page_title"`
	Message    string `json:"message"`
}

// BrowserHandoffComplete waits for the active handoff session to complete.
// The user signals completion by closing DevTools or visiting /handoff/done.
// After completion, the agent should take a browser_snapshot to see the result.
func BrowserHandoffComplete(mgr *browser.Manager) func(tool.Context, BrowserHandoffCompleteArgs) (BrowserHandoffCompleteResult, error) {
	return func(ctx tool.Context, args BrowserHandoffCompleteArgs) (BrowserHandoffCompleteResult, error) {
		if !mgr.HandoffActive() {
			return BrowserHandoffCompleteResult{}, fmt.Errorf("no active handoff session. Call browser_request_human first to start a handoff")
		}

		timeout := args.TimeoutSeconds
		if timeout <= 0 {
			timeout = 300
		}
		if timeout > 600 {
			timeout = 600
		}

		startTime := time.Now()

		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		waitErr := mgr.WaitHandoff(waitCtx)
		duration := time.Since(startTime)

		// Capture post-handoff page state
		postURL := ""
		postTitle := ""
		page, pgErr := mgr.CurrentPage()
		if pgErr == nil {
			if postInfo, infoErr := page.Info(); infoErr == nil && postInfo != nil {
				postURL = postInfo.URL
				postTitle = postInfo.Title
			}
		}

		if waitErr != nil {
			return BrowserHandoffCompleteResult{
				Success:    false,
				DurationMs: duration.Milliseconds(),
				PageURL:    postURL,
				PageTitle:  postTitle,
				Message:    fmt.Sprintf("Handoff timed out after %d seconds. The user may not have connected or finished in time.", timeout),
			}, nil
		}

		return BrowserHandoffCompleteResult{
			Success:    true,
			DurationMs: duration.Milliseconds(),
			PageURL:    postURL,
			PageTitle:  postTitle,
			Message:    "User completed the handoff. Take a browser_snapshot to see the current page state.",
		}, nil
	}
}
