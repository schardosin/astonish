package astonish

import (
	"fmt"
	"sort"
	"strings"
	"time"

	persistentsession "github.com/schardosin/astonish/pkg/session"
	adksession "google.golang.org/adk/session"
)

// TraceOpts controls how the session trace is rendered.
// Extends the shared TraceOpts with CLI-specific fields.
type TraceOpts struct {
	ToolsOnly  bool   // Only show tool call/response events
	Verbose    bool   // Don't truncate args/results
	LastN      int    // Only show last N events (0 = all)
	JSONOutput bool   // Output as JSON
	Recursive  bool   // Include sub-session traces inline
	Indent     string // Prefix for each output line (for nested rendering)
}

// traceEntry is an alias for the shared TraceEntry used in JSON output.
type traceEntry = persistentsession.TraceEntry

// traceSummary is an alias for the shared TraceSummary.
type traceSummary = persistentsession.TraceSummary

// traceJSON is an alias for the shared TraceJSON.
type traceJSON = persistentsession.TraceJSON

const (
	defaultTextMaxLen = 500
	defaultArgsMaxLen = 200
)

// toSharedOpts converts CLI TraceOpts to the shared pkg/session TraceOpts.
func toSharedOpts(opts TraceOpts) persistentsession.TraceOpts {
	return persistentsession.TraceOpts{
		ToolsOnly: opts.ToolsOnly,
		Verbose:   opts.Verbose,
		LastN:     opts.LastN,
	}
}

// renderSessionTrace renders events as a human-readable timeline to stdout.
func renderSessionTrace(events []*adksession.Event, opts TraceOpts) {
	if opts.LastN > 0 && len(events) > opts.LastN {
		events = events[len(events)-opts.LastN:]
	}

	indent := opts.Indent

	// Track FunctionCall timestamps for duration computation
	callTimestamps := make(map[string]time.Time) // keyed by FunctionCall.ID

	var (
		toolCalls    int
		toolErrors   int
		lastTurn     string
		turnCount    int
		modelTextBuf strings.Builder // coalesces consecutive model text events
	)

	argsMax := defaultArgsMaxLen
	textMax := defaultTextMaxLen
	if opts.Verbose {
		argsMax = 0 // no truncation
		textMax = 0
	}

	for _, ev := range events {
		if ev.Content == nil {
			continue
		}

		// Turn separator based on InvocationID
		if ev.InvocationID != "" && ev.InvocationID != lastTurn {
			// Flush any accumulated model text from the previous turn
			if modelTextBuf.Len() > 0 {
				fmt.Printf("%s[model] %s\n\n", indent, persistentsession.TruncateStr(modelTextBuf.String(), textMax))
				modelTextBuf.Reset()
			}
			lastTurn = ev.InvocationID
			turnCount++
			if !opts.ToolsOnly {
				ts := formatTimestamp(ev.Timestamp)
				fmt.Printf("%s\n%s--- Turn %d (%s) ---\n", indent, indent, turnCount, ts)
			}
		}

		for _, part := range ev.Content.Parts {
			if part == nil {
				continue
			}

			// User or model text
			if part.Text != "" && !opts.ToolsOnly {
				if part.Thought {
					// Flush model text before thinking block
					if modelTextBuf.Len() > 0 {
						fmt.Printf("%s[model] %s\n\n", indent, persistentsession.TruncateStr(modelTextBuf.String(), textMax))
						modelTextBuf.Reset()
					}
					fmt.Printf("%s[thinking] %s\n\n", indent, persistentsession.TruncateStr(part.Text, textMax))
				} else if ev.Author == "user" || ev.Content.Role == "user" {
					// Flush model text before user message
					if modelTextBuf.Len() > 0 {
						fmt.Printf("%s[model] %s\n\n", indent, persistentsession.TruncateStr(modelTextBuf.String(), textMax))
						modelTextBuf.Reset()
					}
					fmt.Printf("%s[user] %s\n\n", indent, persistentsession.TruncateStr(part.Text, textMax))
				} else {
					// Accumulate consecutive model text into one block
					modelTextBuf.WriteString(part.Text)
				}
			}

			// FunctionCall — flush model text first
			if part.FunctionCall != nil {
				if modelTextBuf.Len() > 0 {
					fmt.Printf("%s[model] %s\n\n", indent, persistentsession.TruncateStr(modelTextBuf.String(), textMax))
					modelTextBuf.Reset()
				}

				fc := part.FunctionCall
				callTimestamps[fc.ID] = ev.Timestamp
				toolCalls++

				ts := formatTimestamp(ev.Timestamp)
				argsStr := persistentsession.FormatArgs(fc.Args, argsMax)

				if opts.ToolsOnly {
					fmt.Printf("%s%s  %s %s\n", indent, ts, fc.Name, argsStr)
				} else {
					fmt.Printf("%s[tool] %s %s\n", indent, fc.Name, argsStr)
				}
			}

			// FunctionResponse
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse

				// Compute duration from matching call
				var durationMs int64
				if callTS, ok := callTimestamps[fr.ID]; ok && !ev.Timestamp.IsZero() && !callTS.IsZero() {
					durationMs = ev.Timestamp.Sub(callTS).Milliseconds()
				}

				// Detect error
				errStr := persistentsession.ExtractError(fr.Response)
				isError := errStr != ""
				if isError {
					toolErrors++
				}

				// Format result
				if opts.ToolsOnly {
					if isError {
						fmt.Printf("%s         -> ERROR: %s  (%s)\n", indent, persistentsession.TruncateStr(errStr, argsMax), persistentsession.FormatDuration(durationMs))
					} else {
						resultStr := persistentsession.FormatResult(fr.Response, argsMax)
						fmt.Printf("%s         -> %s  (%s)\n", indent, resultStr, persistentsession.FormatDuration(durationMs))
					}
				} else {
					if isError {
						fmt.Printf("%s   -> ERROR: %s  (%s)\n\n", indent, persistentsession.TruncateStr(errStr, argsMax), persistentsession.FormatDuration(durationMs))
					} else {
						resultStr := persistentsession.FormatResult(fr.Response, argsMax)
						fmt.Printf("%s   -> %s  (%s)\n\n", indent, resultStr, persistentsession.FormatDuration(durationMs))
					}
				}
			}
		}
	}

	// Flush any remaining model text
	if modelTextBuf.Len() > 0 {
		fmt.Printf("%s[model] %s\n\n", indent, persistentsession.TruncateStr(modelTextBuf.String(), textMax))
	}

	// Summary
	fmt.Println()
	parts := []string{fmt.Sprintf("%d events", len(events))}
	if toolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls", toolCalls))
	}
	if toolErrors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", toolErrors))
	}
	fmt.Printf("%s--- %s ---\n", indent, strings.Join(parts, " | "))
}

// collectTraceEntries delegates to the shared package.
func collectTraceEntries(events []*adksession.Event, sessionLabel string, opts TraceOpts) ([]traceEntry, int, int) {
	return persistentsession.CollectTraceEntries(events, sessionLabel, toSharedOpts(opts))
}

// collectChildSessionEntries delegates to the shared package.
func collectChildSessionEntries(sessDir, parentID string, index *persistentsession.SessionIndex, opts TraceOpts) ([]traceEntry, int, int) {
	return persistentsession.CollectChildSessionEntries(sessDir, parentID, index, toSharedOpts(opts))
}

// renderChildSessions loads and renders child session traces inline after the
// parent trace. Each child is shown with an indented header and its full trace.
// Recurses into grandchildren (e.g., orchestrator -> workers).
func renderChildSessions(sessDir string, parentID string, index *persistentsession.SessionIndex, opts TraceOpts) {
	children, err := index.ListChildren(parentID)
	if err != nil || len(children) == 0 {
		return
	}

	// Sort children by creation time for chronological order
	sort.Slice(children, func(i, j int) bool {
		return children[i].CreatedAt.Before(children[j].CreatedAt)
	})

	childIndent := opts.Indent + "  "

	for _, child := range children {
		// Print child header
		label := child.Title
		if label == "" {
			label = child.ID
			if len(label) > 12 {
				label = label[:12]
			}
		}
		fmt.Printf("\n%s=== Sub-agent: %s ===\n", childIndent, label)

		// Load and render child transcript
		transcriptPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, child.AppName, child.UserID, child.ID)
		transcript := persistentsession.NewTranscript(transcriptPath)
		if !transcript.Exists() {
			fmt.Printf("%s(no transcript file)\n", childIndent)
			continue
		}

		events, readErr := transcript.ReadEvents()
		if readErr != nil {
			fmt.Printf("%s(error reading transcript: %v)\n", childIndent, readErr)
			continue
		}

		if len(events) == 0 {
			fmt.Printf("%s(empty transcript)\n", childIndent)
			continue
		}

		childOpts := opts
		childOpts.Indent = childIndent
		renderSessionTrace(events, childOpts)

		// Recurse into grandchildren
		renderChildSessions(sessDir, child.ID, index, childOpts)
	}
}

// formatTimestamp renders a time as HH:MM:SS for compact display.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "??:??:??"
	}
	return t.Format("15:04:05")
}
