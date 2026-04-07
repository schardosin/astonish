package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

// BrowserNavigateArgs is the input for browser_navigate.
type BrowserNavigateArgs struct {
	URL string `json:"url" jsonschema:"The URL to navigate to (http or https)"`
}

// BrowserNavigateResult is the output of browser_navigate.
type BrowserNavigateResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// BrowserNavigate navigates the browser to a URL.
func BrowserNavigate(mgr *browser.Manager, guard *browser.NavigationGuard) func(tool.Context, BrowserNavigateArgs) (BrowserNavigateResult, error) {
	return func(ctx tool.Context, args BrowserNavigateArgs) (BrowserNavigateResult, error) {
		mgr.EnsureSessionID(ctx.SessionID())

		if args.URL == "" {
			return BrowserNavigateResult{}, fmt.Errorf("url is required")
		}

		if err := guard.Check(args.URL); err != nil {
			return BrowserNavigateResult{}, err
		}

		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserNavigateResult{}, fmt.Errorf("failed to get browser page: %w", err)
		}

		if err := pg.Navigate(args.URL); err != nil {
			return BrowserNavigateResult{}, fmt.Errorf("navigation failed: %w", err)
		}

		// Wait for the page to load with a timeout.
		// Many sites (NYT, CNN, etc.) have infinite network activity from ads,
		// analytics, and websockets that prevent the load event from firing.
		// A timeout ensures we don't hang forever.
		if err := pg.Timeout(mgr.NavigationTimeout()).WaitLoad(); err != nil {
			// Non-fatal — page content is likely already usable
		}

		info, _ := pg.Info()
		result := BrowserNavigateResult{URL: args.URL}
		if info != nil {
			result.URL = info.URL
			result.Title = info.Title
		}
		return result, nil
	}
}

// BrowserNavigateBackArgs is the input for browser_navigate_back.
type BrowserNavigateBackArgs struct{}

// BrowserNavigateBack navigates the browser back in history.
func BrowserNavigateBack(mgr *browser.Manager) func(tool.Context, BrowserNavigateBackArgs) (BrowserNavigateResult, error) {
	return func(_ tool.Context, args BrowserNavigateBackArgs) (BrowserNavigateResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserNavigateResult{}, fmt.Errorf("failed to get browser page: %w", err)
		}

		if err := pg.NavigateBack(); err != nil {
			return BrowserNavigateResult{}, fmt.Errorf("navigate back failed: %w", err)
		}

		if err := pg.Timeout(mgr.NavigationTimeout()).WaitLoad(); err != nil {
			// Non-fatal
		}

		info, _ := pg.Info()
		result := BrowserNavigateResult{}
		if info != nil {
			result.URL = info.URL
			result.Title = info.Title
		}
		return result, nil
	}
}
