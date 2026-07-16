package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

// AuditQueryHandler returns audit log entries matching the given filters.
//
//	GET /api/audit?action=POST&resource=...&since=2024-01-01&limit=100&offset=0
//
// Platform mode only, admin role required.
func AuditQueryHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireOrgAdmin(w, r)
	if pu == nil {
		return
	}

	if svc.Audit == nil {
		respondError(w, http.StatusServiceUnavailable, "audit store not available")
		return
	}

	// Parse query parameters into AuditFilter
	q := r.URL.Query()
	filter := store.AuditFilter{
		UserID:   q.Get("user_id"),
		Action:   q.Get("action"),
		Resource: q.Get("resource"),
	}

	if since := q.Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = t
		}
	}
	if until := q.Get("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = t
		}
	}
	if limit := q.Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 100 // Default
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000 // Cap
	}
	if offset := q.Get("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	entries, err := svc.Audit.Query(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query audit logs")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"count":   len(entries),
		"filter": map[string]any{
			"limit":  filter.Limit,
			"offset": filter.Offset,
		},
	})
}
