package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/session"
)

// FleetSessionTraceHandler handles GET /api/studio/fleet/sessions/{id}/trace.
// Returns a merged chronological trace of the fleet session including all
// sub-agent tool calls and text events. This is the data source for the
// Fleet Management UI's execution trace view.
//
// Query params:
//
//	tools_only=true  - only include tool_call and tool_result events
//	last_n=50        - only include the last N events
func FleetSessionTraceHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	fileStore := getFleetFileStore()
	if fileStore == nil {
		http.Error(w, "Session storage not available", http.StatusServiceUnavailable)
		return
	}

	// Load session metadata
	meta, err := fileStore.GetSessionMeta(sessionID)
	if err != nil || meta == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Parse query parameters
	opts := session.TraceOpts{}
	if r.URL.Query().Get("tools_only") == "true" {
		opts.ToolsOnly = true
	}
	if lastN := r.URL.Query().Get("last_n"); lastN != "" {
		if n, parseErr := strconv.Atoi(lastN); parseErr == nil && n > 0 {
			opts.LastN = n
		}
	}

	// Read parent session transcript
	events, err := fileStore.ReadTranscriptEvents(meta.AppName, meta.UserID, sessionID)
	if err != nil {
		http.Error(w, "Failed to read transcript: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Collect parent trace entries
	entries, toolCalls, toolErrors := session.CollectTraceEntries(events, "", opts)

	// Collect child session entries (sub-agent traces)
	sessDir := fileStore.BaseDir()
	index := fileStore.Index()
	childEntries, childTC, childTE := session.CollectChildSessionEntries(sessDir, sessionID, index, opts)
	entries = append(entries, childEntries...)
	toolCalls += childTC
	toolErrors += childTE

	// Sort all entries chronologically so parent and child events interleave
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	// Apply last_n after merging (if requested)
	if opts.LastN > 0 && len(entries) > opts.LastN {
		entries = entries[len(entries)-opts.LastN:]
	}

	output := session.TraceJSON{
		SessionID: meta.ID,
		App:       meta.AppName,
		User:      meta.UserID,
		Events:    entries,
		Summary: session.TraceSummary{
			TotalEvents: len(events),
			ToolCalls:   toolCalls,
			Errors:      toolErrors,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(output)
}
