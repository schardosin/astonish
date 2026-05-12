package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
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
//	agent=dev        - only include events from the specified agent's sub-sessions
func FleetSessionTraceHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

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
	agentFilter := r.URL.Query().Get("agent")

	// Platform mode: read from the team Sessions store (PG).
	if svc := store.FromRequest(r); svc != nil && svc.Sessions != nil {
		serveTraceFromStore(w, svc.Sessions, sessionID, agentFilter, opts)
		return
	}

	// Personal mode: read from file store.
	serveTraceFromFileStore(w, sessionID, agentFilter, opts)
}

// serveTraceFromStore reads trace data from a store.SessionStore (platform mode).
func serveTraceFromStore(w http.ResponseWriter, ss store.SessionStore, sessionID, agentFilter string, opts session.TraceOpts) {
	meta, err := ss.GetSessionMeta(context.TODO(), sessionID)
	if err != nil || meta == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	events, err := ss.ReadTranscriptEvents(context.TODO(), meta.AppName, meta.UserID, sessionID)
	if err != nil {
		http.Error(w, "Failed to read transcript: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var entries []session.TraceEntry
	var toolCalls, toolErrors int

	if agentFilter == "" {
		entries, toolCalls, toolErrors = session.CollectTraceEntries(events, "", opts)

		// Collect child session entries from the store.
		childEntries, childTC, childTE := collectChildEntriesFromStore(ss, sessionID, opts, nil)
		entries = append(entries, childEntries...)
		toolCalls += childTC
		toolErrors += childTE
	} else {
		suffix := "-" + agentFilter
		entries, toolCalls, toolErrors = collectChildEntriesFromStore(ss, sessionID, opts, func(title string) bool {
			return strings.HasSuffix(title, suffix)
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

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

// serveTraceFromFileStore reads trace data from the local file store (personal mode).
func serveTraceFromFileStore(w http.ResponseWriter, sessionID, agentFilter string, opts session.TraceOpts) {
	fileStore := getFleetFileStore()
	if fileStore == nil {
		http.Error(w, "Session storage not available", http.StatusServiceUnavailable)
		return
	}

	meta, err := fileStore.GetSessionMeta(sessionID)
	if err != nil || meta == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	events, err := fileStore.ReadTranscriptEvents(meta.AppName, meta.UserID, sessionID)
	if err != nil {
		http.Error(w, "Failed to read transcript: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var entries []session.TraceEntry
	var toolCalls, toolErrors int

	if agentFilter == "" {
		entries, toolCalls, toolErrors = session.CollectTraceEntries(events, "", opts)

		sessDir := fileStore.BaseDir()
		index := fileStore.Index()
		childEntries, childTC, childTE := session.CollectChildSessionEntries(sessDir, sessionID, index, opts)
		entries = append(entries, childEntries...)
		toolCalls += childTC
		toolErrors += childTE
	} else {
		sessDir := fileStore.BaseDir()
		index := fileStore.Index()
		suffix := "-" + agentFilter
		entries, toolCalls, toolErrors = session.CollectChildSessionEntriesFiltered(
			sessDir, sessionID, index, opts, func(title string) bool {
				return strings.HasSuffix(title, suffix)
			})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

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

// collectChildEntriesFromStore replicates the child session traversal logic
// from session.collectChildSessions but reads from a store.SessionStore
// instead of the filesystem. This is used in platform mode where session
// data lives in PostgreSQL.
func collectChildEntriesFromStore(ss store.SessionStore, parentID string, opts session.TraceOpts, titleFilter func(string) bool) ([]session.TraceEntry, int, int) {
	children, err := ss.ListChildren(context.TODO(), parentID)
	if err != nil || len(children) == 0 {
		return nil, 0, 0
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].CreatedAt.Before(children[j].CreatedAt)
	})

	var (
		allEntries     []session.TraceEntry
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

		if titleFilter != nil && !titleFilter(label) {
			continue
		}

		events, readErr := ss.ReadTranscriptEvents(context.TODO(), child.AppName, child.UserID, child.ID)
		if readErr != nil || len(events) == 0 {
			continue
		}

		entries, tc, te := session.CollectTraceEntries(events, label, opts)
		allEntries = append(allEntries, entries...)
		totalToolCalls += tc
		totalErrors += te

		// Recurse into grandchildren
		grandEntries, grandTC, grandTE := collectChildEntriesFromStore(ss, child.ID, opts, nil)
		allEntries = append(allEntries, grandEntries...)
		totalToolCalls += grandTC
		totalErrors += grandTE
	}

	return allEntries, totalToolCalls, totalErrors
}
