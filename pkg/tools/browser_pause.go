package tools

import (
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/browser"
	"google.golang.org/adk/tool"
)

const browserPauseMaxMs = 120_000 // 120s hard cap

// --- browser_pause ---

type BrowserPauseArgs struct {
	Ms int `json:"ms" jsonschema:"Duration to pause in milliseconds. Max 120000 (120s). Used for tutorial pacing so narration can finish before the next UI action."`
}

type BrowserPauseResult struct {
	PausedMs int `json:"paused_ms"`
}

// BrowserPause sleeps for the requested duration. Timing primitive for tutorial
// drills and demo scripts; does not require page interaction.
func BrowserPause(_ *browser.Manager) func(tool.Context, BrowserPauseArgs) (BrowserPauseResult, error) {
	return func(_ tool.Context, args BrowserPauseArgs) (BrowserPauseResult, error) {
		if args.Ms <= 0 {
			return BrowserPauseResult{}, fmt.Errorf("ms must be a positive duration in milliseconds")
		}
		if args.Ms > browserPauseMaxMs {
			return BrowserPauseResult{}, fmt.Errorf("ms exceeds maximum of %d (got %d)", browserPauseMaxMs, args.Ms)
		}
		time.Sleep(time.Duration(args.Ms) * time.Millisecond)
		return BrowserPauseResult{PausedMs: args.Ms}, nil
	}
}
