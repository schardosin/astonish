package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/store"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// MemorySaveRequest is the payload for saving a memory to a specific scope.
type MemorySaveRequest struct {
	Content    string `json:"content"`
	Category   string `json:"category,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

// MemoryPromoteRequest is the payload for promoting a team memory to org.
type MemoryPromoteRequest struct {
	MemoryID string `json:"memory_id"`
	TeamSlug string `json:"team_slug"`
}

// MemorySearchRequest is the payload for cross-scope memory search.
type MemorySearchRequest struct {
	Query      string `json:"query"`
	Category   string `json:"category,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

// MemoryListResponse is the response for memory list/search endpoints.
type MemoryListResponse struct {
	Results []store.MemorySearchResult `json:"results"`
	Count   int                        `json:"count"`
}

// --------------------------------------------------------------------------
// Share memory to team (5.3)
// --------------------------------------------------------------------------

// MemoryShareToTeamHandler saves a memory entry to the team scope.
//
//	POST /api/memories/team
//
// Platform mode only. The memory is saved to the current user's team store.
func MemoryShareToTeamHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "memory sharing requires platform mode", http.StatusBadRequest)
		return
	}

	if svc.Memory == nil {
		http.Error(w, "team memory store not available", http.StatusServiceUnavailable)
		return
	}

	var req MemorySaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	entry := store.MemoryEntry{
		Content:    req.Content,
		Category:   req.Category,
		SourcePath: req.SourcePath,
		CreatedBy:  effectiveUserID(r),
	}

	if err := svc.Memory.Add(r.Context(), entry); err != nil {
		http.Error(w, fmt.Sprintf("failed to save team memory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"saved":   true,
		"scope":   "team",
		"message": "Memory saved to team",
	})
}

// MemorySavePersonalHandler saves a memory entry to the user's personal scope.
//
//	POST /api/memories/personal
//
// Platform mode only. The memory is saved to the personal schema.
func MemorySavePersonalHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	// Resolve personal memory store via TenantRouter
	if svc.TenantRouter == nil {
		http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
		return
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}
	personalMem := orgStore.ForUser(pu.ID).Memories()

	var req MemorySaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	entry := store.MemoryEntry{
		Content:    req.Content,
		Category:   req.Category,
		SourcePath: req.SourcePath,
	}

	if err := personalMem.Add(r.Context(), entry); err != nil {
		http.Error(w, fmt.Sprintf("failed to save personal memory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"saved":   true,
		"scope":   "personal",
		"message": "Memory saved to personal store",
	})
}

// --------------------------------------------------------------------------
// Promote memory to org (5.4)
// --------------------------------------------------------------------------

// MemoryPromoteToOrgHandler copies a team memory to the org-wide store.
//
//	POST /api/memories/promote
//
// Admin-only. Requires "admin" role on the platform user.
func MemoryPromoteToOrgHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !isOrgAdmin(pu) {
		http.Error(w, "admin role required for knowledge promotion", http.StatusForbidden)
		return
	}

	var req MemoryPromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.MemoryID == "" {
		http.Error(w, "memory_id is required", http.StatusBadRequest)
		return
	}
	if req.TeamSlug == "" {
		http.Error(w, "team_slug is required", http.StatusBadRequest)
		return
	}

	if svc.TenantRouter == nil {
		http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	// Read the team memory
	teamMem := orgStore.ForTeam(req.TeamSlug).Memories()
	results, err := teamMem.List(r.Context(), "", 1000, 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read team memories: %v", err), http.StatusInternalServerError)
		return
	}

	// Find the specific memory by ID
	var source *store.MemorySearchResult
	for i, m := range results {
		if m.ID == req.MemoryID {
			source = &results[i]
			break
		}
	}
	if source == nil {
		http.Error(w, "memory not found in team store", http.StatusNotFound)
		return
	}

	// Copy to org store
	orgMem := orgStore.OrgMemories()
	entry := store.MemoryEntry{
		Content:    source.Snippet,
		Category:   source.Category,
		SourcePath: source.Path,
		Metadata: map[string]any{
			"promoted_by":        pu.ID,
			"promoted_from_team": req.TeamSlug,
		},
	}

	if err := orgMem.Add(r.Context(), entry); err != nil {
		http.Error(w, fmt.Sprintf("failed to promote memory to org: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"promoted": true,
		"scope":    "org",
		"message":  fmt.Sprintf("Memory promoted from team '%s' to org", req.TeamSlug),
	})
}

// --------------------------------------------------------------------------
// Team/org memory browsing (5.5)
// --------------------------------------------------------------------------

// MemoryListTeamHandler lists memories in the current team store.
//
//	GET /api/memories/team?category=...&limit=...&offset=...
func MemoryListTeamHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform || svc.Memory == nil {
		http.Error(w, "platform mode with team context required", http.StatusBadRequest)
		return
	}

	category := r.URL.Query().Get("category")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	results, err := svc.Memory.List(r.Context(), category, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list team memories: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MemoryListResponse{
		Results: results,
		Count:   len(results),
	})
}

// MemoryListOrgHandler lists memories in the org-wide store.
//
//	GET /api/memories/org?category=...&limit=...&offset=...
func MemoryListOrgHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	orgMem := orgStore.OrgMemories()
	category := r.URL.Query().Get("category")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	results, err := orgMem.List(r.Context(), category, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list org memories: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MemoryListResponse{
		Results: results,
		Count:   len(results),
	})
}

// MemorySearchCrossTierHandler performs a cross-tier memory search.
//
//	POST /api/memories/search
//
// In platform mode, searches across personal + team + org tiers.
// In personal mode, searches the local memory store.
func MemorySearchCrossTierHandler(w http.ResponseWriter, r *http.Request) {
	var req MemorySearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}

	svc := store.FromRequest(r)

	// Platform mode: use three-tier searcher
	if svc != nil && svc.Mode == store.ModePlatform && svc.MemorySearcher != nil {
		var results []store.MemorySearchResult
		var err error
		if req.Category == "" {
			results, err = svc.MemorySearcher.SearchAllTiers(r.Context(), req.Query, req.MaxResults, 0)
		} else {
			results, err = svc.MemorySearcher.SearchAllTiersByCategory(r.Context(), req.Query, req.MaxResults, 0, req.Category)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("cross-tier search failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MemoryListResponse{Results: results, Count: len(results)})
		return
	}

	// Personal mode: use local memory store
	if svc != nil && svc.Memory != nil {
		var results []store.MemorySearchResult
		var err error
		if req.Category == "" {
			results, err = svc.Memory.Search(r.Context(), req.Query, req.MaxResults, 0)
		} else {
			results, err = svc.Memory.SearchByCategory(r.Context(), req.Query, req.MaxResults, 0, req.Category)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("memory search failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MemoryListResponse{Results: results, Count: len(results)})
		return
	}

	http.Error(w, "memory store not available", http.StatusServiceUnavailable)
}

// MemoryDeleteTeamHandler deletes a memory from the team store.
//
//	DELETE /api/memories/team/{id}
func MemoryDeleteTeamHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform || svc.Memory == nil {
		http.Error(w, "platform mode with team context required", http.StatusBadRequest)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "memory ID required", http.StatusBadRequest)
		return
	}

	if err := svc.Memory.Delete(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete team memory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"deleted": true,
		"id":      id,
	})
}

// MemoryDeleteOrgHandler deletes a memory from the org store.
//
//	DELETE /api/memories/org/{id}
//
// Admin-only.
func MemoryDeleteOrgHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !isOrgAdmin(pu) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "memory ID required", http.StatusBadRequest)
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	orgMem := orgStore.OrgMemories()
	if err := orgMem.Delete(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete org memory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"deleted": true,
		"id":      id,
	})
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// queryInt extracts an integer query parameter with a default value.
func queryInt(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return defaultVal
	}
	return n
}
