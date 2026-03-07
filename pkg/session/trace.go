package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	adksession "google.golang.org/adk/session"
)

// TraceOpts controls how the session trace is collected.
type TraceOpts struct {
	ToolsOnly bool // Only show tool call/response events
	Verbose   bool // Don't truncate args/results
	LastN     int  // Only show last N events (0 = all)
}

// TraceEntry is a single event in a session trace.
type TraceEntry struct {
	Type       string         `json:"type"`
	Timestamp  string         `json:"timestamp"`
	Author     string         `json:"author,omitempty"`
	Session    string         `json:"session,omitempty"` // sub-session label (only set for recursive child events)
	Text       string         `json:"text,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Success    *bool          `json:"success,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// TraceSummary is the summary block in trace output.
type TraceSummary struct {
	TotalEvents int `json:"total_events"`
	ToolCalls   int `json:"tool_calls"`
	Errors      int `json:"errors"`
}

// TraceJSON is the top-level JSON output structure for a session trace.
type TraceJSON struct {
	SessionID string       `json:"session_id"`
	App       string       `json:"app"`
	User      string       `json:"user"`
	Events    []TraceEntry `json:"events"`
	Summary   TraceSummary `json:"summary"`
}

// CollectTraceEntries converts ADK events into TraceEntry objects for JSON output.
// The sessionLabel parameter, when non-empty, is set on each entry to identify
// which sub-session the event belongs to (used by recursive rendering).
// Returns entries, tool call count, and error count.
func CollectTraceEntries(events []*adksession.Event, sessionLabel string, opts TraceOpts) ([]TraceEntry, int, int) {
	callTimestamps := make(map[string]time.Time)

	var (
		entries    []TraceEntry
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
				entry := TraceEntry{
					Type:      entryType,
					Timestamp: ts,
					Author:    ev.Author,
					Text:      part.Text,
				}
				if sessionLabel != "" {
					entry.Session = sessionLabel
				}
				entries = append(entries, entry)
			}

			// FunctionCall
			if part.FunctionCall != nil {
				fc := part.FunctionCall
				callTimestamps[fc.ID] = ev.Timestamp
				toolCalls++

				entry := TraceEntry{
					Type:       "tool_call",
					Timestamp:  ts,
					ToolName:   fc.Name,
					ToolCallID: fc.ID,
					Args:       fc.Args,
				}
				if sessionLabel != "" {
					entry.Session = sessionLabel
				}
				entries = append(entries, entry)
			}

			// FunctionResponse
			if part.FunctionResponse != nil {
				fr := part.FunctionResponse

				var durationMs int64
				if callTS, ok := callTimestamps[fr.ID]; ok && !ev.Timestamp.IsZero() && !callTS.IsZero() {
					durationMs = ev.Timestamp.Sub(callTS).Milliseconds()
				}

				errStr := ExtractError(fr.Response)
				isError := errStr != ""
				if isError {
					toolErrors++
				}

				success := !isError
				entry := TraceEntry{
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
				if sessionLabel != "" {
					entry.Session = sessionLabel
				}
				entries = append(entries, entry)
			}
		}
	}

	return entries, toolCalls, toolErrors
}

// CollectChildSessionEntries loads child session transcripts and converts them
// to TraceEntry objects. Recurses into grandchildren.
// Returns all collected entries, total tool calls, and total errors.
func CollectChildSessionEntries(sessDir, parentID string, index *SessionIndex, opts TraceOpts) ([]TraceEntry, int, int) {
	children, err := index.ListChildren(parentID)
	if err != nil || len(children) == 0 {
		return nil, 0, 0
	}

	// Sort children by creation time for chronological order
	sort.Slice(children, func(i, j int) bool {
		return children[i].CreatedAt.Before(children[j].CreatedAt)
	})

	var (
		allEntries     []TraceEntry
		totalToolCalls int
		totalErrors    int
	)

	for _, child := range children {
		label := child.Title
		if label == "" {
			label = child.ID
			if len(label) > 12 {
				label = label[:12]
			}
		}

		transcriptPath := fmt.Sprintf("%s/%s/%s/%s.jsonl", sessDir, child.AppName, child.UserID, child.ID)
		transcript := NewTranscript(transcriptPath)
		if !transcript.Exists() {
			continue
		}

		events, readErr := transcript.ReadEvents()
		if readErr != nil || len(events) == 0 {
			continue
		}

		entries, tc, te := CollectTraceEntries(events, label, opts)
		allEntries = append(allEntries, entries...)
		totalToolCalls += tc
		totalErrors += te

		// Recurse into grandchildren
		grandEntries, grandTC, grandTE := CollectChildSessionEntries(sessDir, child.ID, index, opts)
		allEntries = append(allEntries, grandEntries...)
		totalToolCalls += grandTC
		totalErrors += grandTE
	}

	return allEntries, totalToolCalls, totalErrors
}

// ExtractError checks if a FunctionResponse contains an error.
// ADK tools report errors as {"error": "message"} in the response map.
func ExtractError(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if errVal, ok := resp["error"]; ok {
		switch v := errVal.(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if msg, ok := v["message"]; ok {
				return fmt.Sprintf("%v", msg)
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// FormatArgs renders a map as compact JSON, truncated to maxLen.
// If maxLen is 0, no truncation is applied (verbose/pretty-print).
func FormatArgs(args map[string]any, maxLen int) string {
	if len(args) == 0 {
		return "{}"
	}
	if maxLen == 0 {
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
	return TruncateStr(string(data), maxLen)
}

// FormatResult renders a tool result map as a compact string.
func FormatResult(result map[string]any, maxLen int) string {
	if len(result) == 0 {
		return "OK"
	}
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
	return TruncateStr(string(data), maxLen)
}

// FormatDuration renders milliseconds as a human-readable duration.
func FormatDuration(ms int64) string {
	if ms <= 0 {
		return "n/a"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// TruncateStr truncates a string to maxLen characters, appending "..." if truncated.
// If maxLen is 0, no truncation is applied.
func TruncateStr(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// SortTraceEntriesChronologically sorts trace entries by timestamp.
func SortTraceEntriesChronologically(entries []TraceEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})
}
