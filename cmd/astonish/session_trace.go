package astonish

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	adksession "google.golang.org/adk/session"
)

// TraceOpts controls how the session trace is rendered.
type TraceOpts struct {
	ToolsOnly  bool // Only show tool call/response events
	Verbose    bool // Don't truncate args/results
	LastN      int  // Only show last N events (0 = all)
	JSONOutput bool // Output as JSON
}

// traceEntry is used for JSON output.
type traceEntry struct {
	Type       string         `json:"type"`
	Timestamp  string         `json:"timestamp"`
	Author     string         `json:"author,omitempty"`
	Text       string         `json:"text,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Success    *bool          `json:"success,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// traceSummary is the summary block in JSON output.
type traceSummary struct {
	TotalEvents int `json:"total_events"`
	ToolCalls   int `json:"tool_calls"`
	Errors      int `json:"errors"`
}

// traceJSON is the top-level JSON output structure.
type traceJSON struct {
	SessionID string       `json:"session_id"`
	App       string       `json:"app"`
	User      string       `json:"user"`
	Events    []traceEntry `json:"events"`
	Summary   traceSummary `json:"summary"`
}

const (
	defaultTextMaxLen = 500
	defaultArgsMaxLen = 200
)

// renderSessionTrace renders events as a human-readable timeline to stdout.
func renderSessionTrace(events []*adksession.Event, opts TraceOpts) {
	if opts.LastN > 0 && len(events) > opts.LastN {
		events = events[len(events)-opts.LastN:]
	}

	// Track FunctionCall timestamps for duration computation
	callTimestamps := make(map[string]time.Time) // keyed by FunctionCall.ID
	callNames := make(map[string]string)         // keyed by FunctionCall.ID -> tool name

	var (
		toolCalls  int
		toolErrors int
		lastTurn   string
		turnCount  int
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
			lastTurn = ev.InvocationID
			turnCount++
			if !opts.ToolsOnly {
				ts := formatTimestamp(ev.Timestamp)
				fmt.Printf("\n--- Turn %d (%s) ---\n", turnCount, ts)
			}
		}

		for _, part := range ev.Content.Parts {
			if part == nil {
				continue
			}

			// User or model text
			if part.Text != "" && !opts.ToolsOnly {
				if part.Thought {
					fmt.Printf("[thinking] %s\n\n", truncateStr(part.Text, textMax))
				} else if ev.Author == "user" || ev.Content.Role == "user" {
					fmt.Printf("[user] %s\n\n", truncateStr(part.Text, textMax))
				} else {
					fmt.Printf("[model] %s\n\n", truncateStr(part.Text, textMax))
				}
			}

			// FunctionCall
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				callTimestamps[fc.ID] = ev.Timestamp
				callNames[fc.ID] = fc.Name
				toolCalls++

				ts := formatTimestamp(ev.Timestamp)
				argsStr := formatArgs(fc.Args, argsMax)

				if opts.ToolsOnly {
					fmt.Printf("%s  %s %s\n", ts, fc.Name, argsStr)
				} else {
					fmt.Printf("[tool] %s %s\n", fc.Name, argsStr)
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
				errStr := extractError(fr.Response)
				isError := errStr != ""
				if isError {
					toolErrors++
				}

				// Format result
				if opts.ToolsOnly {
					if isError {
						fmt.Printf("         -> ERROR: %s  (%s)\n", truncateStr(errStr, argsMax), formatDuration(durationMs))
					} else {
						resultStr := formatResult(fr.Response, argsMax)
						fmt.Printf("         -> %s  (%s)\n", resultStr, formatDuration(durationMs))
					}
				} else {
					if isError {
						fmt.Printf("   -> ERROR: %s  (%s)\n\n", truncateStr(errStr, argsMax), formatDuration(durationMs))
					} else {
						resultStr := formatResult(fr.Response, argsMax)
						fmt.Printf("   -> %s  (%s)\n\n", resultStr, formatDuration(durationMs))
					}
				}
			}
		}
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
	fmt.Printf("--- %s ---\n", strings.Join(parts, " | "))
}

// renderSessionTraceJSON outputs events as structured JSON.
func renderSessionTraceJSON(sessionID, app, user string, events []*adksession.Event, opts TraceOpts) {
	if opts.LastN > 0 && len(events) > opts.LastN {
		events = events[len(events)-opts.LastN:]
	}

	callTimestamps := make(map[string]time.Time)

	var (
		entries    []traceEntry
		toolCalls  int
		toolErrors int
	)

	for _, ev := range events {
		if ev.Content == nil {
			continue
		}

		for _, part := range ev.Content.Parts {
			if part == nil {
				continue
			}

			ts := ev.Timestamp.Format(time.RFC3339)

			// Text
			if part.Text != "" && !opts.ToolsOnly {
				entryType := "model"
				if ev.Author == "user" || ev.Content.Role == "user" {
					entryType = "user"
				}
				if part.Thought {
					entryType = "thinking"
				}
				entries = append(entries, traceEntry{
					Type:      entryType,
					Timestamp: ts,
					Author:    ev.Author,
					Text:      part.Text,
				})
			}

			// FunctionCall
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				callTimestamps[fc.ID] = ev.Timestamp
				toolCalls++

				entries = append(entries, traceEntry{
					Type:       "tool_call",
					Timestamp:  ts,
					ToolName:   fc.Name,
					ToolCallID: fc.ID,
					Args:       fc.Args,
				})
			}

			// FunctionResponse
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse

				var durationMs int64
				if callTS, ok := callTimestamps[fr.ID]; ok && !ev.Timestamp.IsZero() && !callTS.IsZero() {
					durationMs = ev.Timestamp.Sub(callTS).Milliseconds()
				}

				errStr := extractError(fr.Response)
				isError := errStr != ""
				if isError {
					toolErrors++
				}

				success := !isError
				entry := traceEntry{
					Type:       "tool_result",
					Timestamp:  ts,
					ToolName:   fr.Name,
					ToolCallID: fr.ID,
					DurationMs: durationMs,
					Success:    &success,
					Result:     fr.Response,
				}
				if isError {
					entry.Error = errStr
				}
				entries = append(entries, entry)
			}
		}
	}

	output := traceJSON{
		SessionID: sessionID,
		App:       app,
		User:      user,
		Events:    entries,
		Summary: traceSummary{
			TotalEvents: len(events),
			ToolCalls:   toolCalls,
			Errors:      toolErrors,
		},
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Printf("Error serializing trace: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

// extractError checks if a FunctionResponse contains an error.
// ADK tools report errors as {"error": "message"} in the response map.
func extractError(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	// Check for "error" key (string)
	if errVal, ok := resp["error"]; ok {
		switch v := errVal.(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			// Some tools return {"error": {"message": "..."}}
			if msg, ok := v["message"]; ok {
				return fmt.Sprintf("%v", msg)
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// formatArgs renders a map as compact JSON, truncated to maxLen.
func formatArgs(args map[string]any, maxLen int) string {
	if len(args) == 0 {
		return "{}"
	}
	if maxLen == 0 {
		// Verbose: pretty print
		data, err := json.MarshalIndent(args, "  ", "  ")
		if err != nil {
			return fmt.Sprintf("%v", args)
		}
		return string(data)
	}
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return truncateStr(string(data), maxLen)
}

// formatResult renders a tool result map as a compact string.
func formatResult(result map[string]any, maxLen int) string {
	if len(result) == 0 {
		return "OK"
	}

	// For simple success results, show a compact form
	if len(result) == 1 {
		if success, ok := result["success"]; ok {
			if b, ok := success.(bool); ok && b {
				return "OK"
			}
		}
	}

	if maxLen == 0 {
		data, err := json.MarshalIndent(result, "  ", "  ")
		if err != nil {
			return fmt.Sprintf("%v", result)
		}
		return string(data)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return truncateStr(string(data), maxLen)
}

// formatDuration renders milliseconds as a human-readable duration.
func formatDuration(ms int64) string {
	if ms <= 0 {
		return "n/a"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// formatTimestamp renders a time as HH:MM:SS for compact display.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "??:??:??"
	}
	return t.Format("15:04:05")
}

// truncateStr truncates a string to maxLen characters, appending "..." if truncated.
// If maxLen is 0, no truncation is applied.
func truncateStr(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
