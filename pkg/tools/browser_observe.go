package tools

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

// --- browser_snapshot ---

type BrowserSnapshotArgs struct {
	Mode            string `json:"mode,omitempty" jsonschema:"Snapshot mode: full (default) or efficient (compact interactive-only with depth=6 and maxChars=10000)"`
	InteractiveOnly bool   `json:"interactive,omitempty" jsonschema:"Only show interactive elements (buttons, links, inputs, etc.)"`
	Compact         bool   `json:"compact,omitempty" jsonschema:"Remove unnamed structural elements"`
	MaxDepth        int    `json:"maxDepth,omitempty" jsonschema:"Maximum tree depth (0 = unlimited)"`
	Selector        string `json:"selector,omitempty" jsonschema:"CSS selector to scope snapshot to a subtree"`
	Frame           string `json:"frame,omitempty" jsonschema:"iframe CSS selector to scope snapshot into an iframe"`
	MaxChars        int    `json:"maxChars,omitempty" jsonschema:"Maximum characters in output (default 80000)"`
}

type BrowserSnapshotResult struct {
	Snapshot string `json:"snapshot"`
	RefCount int    `json:"ref_count"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

func BrowserSnapshot(mgr *browser.Manager, refs *browser.RefMap) func(tool.Context, BrowserSnapshotArgs) (BrowserSnapshotResult, error) {
	return func(_ tool.Context, args BrowserSnapshotArgs) (BrowserSnapshotResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserSnapshotResult{}, fmt.Errorf("failed to get browser page: %w", err)
		}

		opts := browser.SnapshotOptions{
			InteractiveOnly: args.InteractiveOnly,
			Compact:         args.Compact,
			MaxDepth:        args.MaxDepth,
			Selector:        args.Selector,
			Frame:           args.Frame,
			MaxChars:        args.MaxChars,
		}

		// "efficient" mode preset
		if strings.ToLower(args.Mode) == "efficient" {
			opts.InteractiveOnly = true
			opts.Compact = true
			if opts.MaxDepth == 0 {
				opts.MaxDepth = 6
			}
			if opts.MaxChars == 0 {
				opts.MaxChars = 10000
			}
		}

		result, err := browser.TakeSnapshot(pg, refs, opts)
		if err != nil {
			return BrowserSnapshotResult{}, err
		}

		return BrowserSnapshotResult{
			Snapshot: result.Text,
			RefCount: result.RefCount,
			URL:      result.URL,
			Title:    result.Title,
		}, nil
	}
}

// --- browser_take_screenshot ---

type BrowserScreenshotArgs struct {
	FullPage bool   `json:"fullPage,omitempty" jsonschema:"Capture full scrollable page (not just viewport)"`
	Ref      string `json:"ref,omitempty" jsonschema:"Element ref to screenshot (from snapshot)"`
	Selector string `json:"selector,omitempty" jsonschema:"CSS selector of element to screenshot"`
	Format   string `json:"format,omitempty" jsonschema:"Image format: png (default) or jpeg"`
}

type BrowserScreenshotResult struct {
	ImageBase64 string `json:"image_base64"`
	Format      string `json:"format"`
}

func BrowserTakeScreenshot(mgr *browser.Manager, refs *browser.RefMap) func(tool.Context, BrowserScreenshotArgs) (BrowserScreenshotResult, error) {
	return func(_ tool.Context, args BrowserScreenshotArgs) (BrowserScreenshotResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserScreenshotResult{}, fmt.Errorf("failed to get browser page: %w", err)
		}

		data, format, err := browser.TakeScreenshot(pg, refs, browser.ScreenshotOptions{
			FullPage: args.FullPage,
			Ref:      args.Ref,
			Selector: args.Selector,
			Format:   args.Format,
		})
		if err != nil {
			return BrowserScreenshotResult{}, err
		}

		return BrowserScreenshotResult{
			ImageBase64: base64.StdEncoding.EncodeToString(data),
			Format:      format,
		}, nil
	}
}

// --- browser_console_messages ---

type BrowserConsoleMessagesArgs struct {
	Level string `json:"level,omitempty" jsonschema:"Minimum level filter: error, warning, info, debug (default: all)"`
	Clear bool   `json:"clear,omitempty" jsonschema:"Clear the console buffer after reading"`
}

type BrowserConsoleMessagesResult struct {
	Messages []browser.ConsoleMessage `json:"messages"`
	Count    int                      `json:"count"`
}

func BrowserConsoleMessages(mgr *browser.Manager) func(tool.Context, BrowserConsoleMessagesArgs) (BrowserConsoleMessagesResult, error) {
	return func(_ tool.Context, args BrowserConsoleMessagesArgs) (BrowserConsoleMessagesResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserConsoleMessagesResult{}, err
		}

		ps := mgr.PageStateFor(pg)
		if ps == nil {
			return BrowserConsoleMessagesResult{Messages: []browser.ConsoleMessage{}, Count: 0}, nil
		}

		// Lazily attach event listeners on first use. This defers Runtime.enable
		// (a primary bot-detection signal) until the agent actually needs console data.
		ps.AttachListeners(pg)

		messages := ps.Console.Items()

		// Filter by level
		if args.Level != "" {
			minPriority := levelPriority(args.Level)
			var filtered []browser.ConsoleMessage
			for _, m := range messages {
				if levelPriority(m.Level) >= minPriority {
					filtered = append(filtered, m)
				}
			}
			messages = filtered
		}

		if args.Clear {
			ps.Console.Clear()
		}

		if messages == nil {
			messages = []browser.ConsoleMessage{}
		}

		return BrowserConsoleMessagesResult{Messages: messages, Count: len(messages)}, nil
	}
}

// --- browser_network_requests ---

type BrowserNetworkRequestsArgs struct {
	URLFilter string `json:"urlFilter,omitempty" jsonschema:"Filter requests by URL substring"`
	Clear     bool   `json:"clear,omitempty" jsonschema:"Clear the network buffer after reading"`
}

type BrowserNetworkRequestsResult struct {
	Requests []browser.NetworkRequest `json:"requests"`
	Count    int                      `json:"count"`
}

func BrowserNetworkRequests(mgr *browser.Manager) func(tool.Context, BrowserNetworkRequestsArgs) (BrowserNetworkRequestsResult, error) {
	return func(_ tool.Context, args BrowserNetworkRequestsArgs) (BrowserNetworkRequestsResult, error) {
		pg, err := mgr.CurrentPage()
		if err != nil {
			return BrowserNetworkRequestsResult{}, err
		}

		ps := mgr.PageStateFor(pg)
		if ps == nil {
			return BrowserNetworkRequestsResult{Requests: []browser.NetworkRequest{}, Count: 0}, nil
		}

		// Lazily attach event listeners on first use. This defers Runtime.enable
		// (a primary bot-detection signal) until the agent actually needs network data.
		ps.AttachListeners(pg)

		requests := ps.Network.Items()

		if args.URLFilter != "" {
			var filtered []browser.NetworkRequest
			for _, r := range requests {
				if strings.Contains(r.URL, args.URLFilter) {
					filtered = append(filtered, r)
				}
			}
			requests = filtered
		}

		if args.Clear {
			ps.Network.Clear()
		}

		if requests == nil {
			requests = []browser.NetworkRequest{}
		}

		return BrowserNetworkRequestsResult{Requests: requests, Count: len(requests)}, nil
	}
}

// levelPriority returns a numeric priority for console message levels.
func levelPriority(level string) int {
	switch strings.ToLower(level) {
	case "error":
		return 4
	case "warning", "warn":
		return 3
	case "info", "log":
		return 2
	case "debug":
		return 1
	default:
		return 0
	}
}
