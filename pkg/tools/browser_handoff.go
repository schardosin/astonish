package tools

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

// BrowserRequestHumanArgs is the input for browser_request_human.
type BrowserRequestHumanArgs struct {
	Reason         string `json:"reason" jsonschema:"required,Why you need human help. Shown to the user. Be specific about what they should do (e.g. 'solve the CAPTCHA and click Submit')."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Max seconds to wait for the user. Default: 300 (5 minutes). Max: 600."`
	Screenshot     *bool  `json:"screenshot,omitempty" jsonschema:"Take a screenshot before handoff to show the user what you see. Default: true."`
}

// BrowserRequestHumanResult is the output of browser_request_human.
type BrowserRequestHumanResult struct {
	Success        bool   `json:"success"`
	DurationMs     int64  `json:"duration_ms"`
	PageURL        string `json:"page_url"`
	PageTitle      string `json:"page_title"`
	ChangesSummary string `json:"changes_summary"`
	Message        string `json:"message"`
}

// BrowserRequestHuman pauses agent execution and exposes the browser to a
// human via CDP (Chrome DevTools Protocol). The user connects with
// chrome://inspect, interacts with the browser (solve CAPTCHAs, navigate
// complex flows, etc.), and signals completion. The agent then resumes with
// a fresh view of the page state.
func BrowserRequestHuman(mgr *browser.Manager) func(tool.Context, BrowserRequestHumanArgs) (BrowserRequestHumanResult, error) {
	return func(ctx tool.Context, args BrowserRequestHumanArgs) (BrowserRequestHumanResult, error) {
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

		takeScreenshot := true
		if args.Screenshot != nil {
			takeScreenshot = *args.Screenshot
		}

		// Get the CDP URL from the browser manager
		cdpURL := mgr.CDPURL()
		if cdpURL == "" {
			// Browser hasn't been launched yet — launch it
			if _, err := mgr.GetOrLaunch(); err != nil {
				return BrowserRequestHumanResult{}, fmt.Errorf("failed to launch browser: %w", err)
			}
			cdpURL = mgr.CDPURL()
		}
		if cdpURL == "" {
			return BrowserRequestHumanResult{}, fmt.Errorf("browser CDP endpoint not available")
		}

		// Capture pre-handoff state
		page, err := mgr.CurrentPage()
		if err != nil {
			return BrowserRequestHumanResult{}, fmt.Errorf("failed to get current page: %w", err)
		}

		preURL := ""
		preTitle := ""
		info := page.MustInfo()
		if info != nil {
			preURL = info.URL
			preTitle = info.Title
		}

		// Optionally take a screenshot before handoff
		if takeScreenshot {
			// The screenshot is captured but we don't block on it —
			// the agent can include it in its response to the user.
			// We just want to make sure the page state is captured.
			_ = page.MustWaitStable()
		}

		// Get handoff config from manager
		handoffCfg := mgr.HandoffConfig()

		// Start handoff server
		handoff := browser.NewHandoffServer(log.Default())
		handoffInfo, err := handoff.Start(browser.HandoffOpts{
			CDPURL:      cdpURL,
			Port:        handoffCfg.Port,
			BindAddress: handoffCfg.BindAddress,
			Timeout:     time.Duration(timeout) * time.Second,
			Reason:      args.Reason,
		})
		if err != nil {
			return BrowserRequestHumanResult{}, fmt.Errorf("failed to start handoff server: %w", err)
		}
		defer handoff.Stop()

		startTime := time.Now()

		// Build the message to show the user
		message := fmt.Sprintf(
			"I need human assistance with the browser.\n\n"+
				"Reason: %s\n\n"+
				"Current page: %s\n\n"+
				"To connect:\n"+
				"1. Open Chrome and go to chrome://inspect\n"+
				"2. Click 'Configure...' and add: %s\n"+
				"3. The browser tab should appear under 'Remote Target'\n"+
				"4. Click 'inspect' to open DevTools with full control\n\n"+
				"When you're done, either:\n"+
				"- Close the DevTools tab (auto-detected after 10s)\n"+
				"- Visit http://%s/handoff/done\n"+
				"- Or wait for the %d-second timeout",
			args.Reason,
			preURL,
			handoffInfo.ListenAddress,
			handoffInfo.ListenAddress,
			timeout,
		)

		// Wait for user to finish
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		waitErr := handoff.WaitForCompletion(waitCtx)

		duration := time.Since(startTime)

		// Capture post-handoff state
		postURL := ""
		postTitle := ""
		page, pgErr := mgr.CurrentPage()
		if pgErr == nil {
			postInfo := page.MustInfo()
			if postInfo != nil {
				postURL = postInfo.URL
				postTitle = postInfo.Title
			}
		}

		// Build changes summary
		changes := ""
		if preURL != postURL {
			changes = fmt.Sprintf("URL changed: %s → %s", preURL, postURL)
		} else {
			changes = "URL unchanged"
		}
		if preTitle != postTitle && postTitle != "" {
			changes += fmt.Sprintf("; Title changed: %q → %q", preTitle, postTitle)
		}

		if waitErr != nil {
			return BrowserRequestHumanResult{
				Success:        false,
				DurationMs:     duration.Milliseconds(),
				PageURL:        postURL,
				PageTitle:      postTitle,
				ChangesSummary: changes,
				Message:        fmt.Sprintf("Handoff timed out after %d seconds. %s", timeout, message),
			}, nil
		}

		return BrowserRequestHumanResult{
			Success:        true,
			DurationMs:     duration.Milliseconds(),
			PageURL:        postURL,
			PageTitle:      postTitle,
			ChangesSummary: changes,
			Message:        message,
		}, nil
	}
}
