package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/SAP/astonish/pkg/store"
)

// NetworkPolicyListItem represents a network policy rule in listing responses.
type NetworkPolicyListItem struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Port      uint32 `json:"port"`
	Action    string `json:"action"` // "allow" or "deny"
	Scope     string `json:"scope"`  // "platform", "org", or "team"
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// NetworkPolicyListResponse is the response for GET /api/network-policies.
type NetworkPolicyListResponse struct {
	Rules       []NetworkPolicyListItem `json:"rules"`
	IsTeamAdmin bool                    `json:"is_team_admin"`
	IsOrgAdmin  bool                    `json:"is_org_admin"`
}

// NetworkPolicyCreateRequest is the request body for creating/updating a network policy rule.
type NetworkPolicyCreateRequest struct {
	Host   string `json:"host"`
	Port   uint32 `json:"port"`
	Action string `json:"action"` // "allow" or "deny"
}

// ListNetworkPoliciesHandler handles GET /api/network-policies
//
// Query params:
//   - scope=team: return only team rules
//   - scope=org: return only org rules
//   - scope=platform: return only platform rules
//   - (empty): return merged view (all scopes, for display)
func ListNetworkPoliciesHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	items, err := listNetworkPolicies(r.Context(), svc, scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load network policies: "+err.Error())
		return
	}

	resp := NetworkPolicyListResponse{
		Rules:       items,
		IsTeamAdmin: IsTeamAdmin(r),
		IsOrgAdmin:  CanManageOrg(GetPlatformUser(r)),
	}
	respondJSON(w, http.StatusOK, resp)
}

// CreateNetworkPolicyHandler handles POST /api/network-policies
func CreateNetworkPolicyHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveNetworkPolicyStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return
	}

	var req NetworkPolicyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Host == "" {
		respondError(w, http.StatusBadRequest, "Host is required")
		return
	}
	if req.Action != "allow" && req.Action != "deny" {
		respondError(w, http.StatusBadRequest, "Action must be 'allow' or 'deny'")
		return
	}

	createdBy := ""
	if user := GetPlatformUser(r); user != nil {
		createdBy = user.ID
	}

	rule := &store.NetworkPolicyRule{
		Host:      req.Host,
		Port:      req.Port,
		Action:    store.NetworkPolicyAction(req.Action),
		CreatedBy: createdBy,
	}

	if err := targetStore.Save(r.Context(), rule); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save network policy: "+err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// UpdateNetworkPolicyHandler handles PUT /api/network-policies/{id}
func UpdateNetworkPolicyHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveNetworkPolicyStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return
	}

	existing, err := targetStore.Get(r.Context(), id)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "Network policy rule not found")
		return
	}

	var req NetworkPolicyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Host != "" {
		existing.Host = req.Host
	}
	if req.Action != "" {
		if req.Action != "allow" && req.Action != "deny" {
			respondError(w, http.StatusBadRequest, "Action must be 'allow' or 'deny'")
			return
		}
		existing.Action = store.NetworkPolicyAction(req.Action)
	}
	existing.Port = req.Port

	if err := targetStore.Save(r.Context(), existing); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update network policy: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteNetworkPolicyHandler handles DELETE /api/network-policies/{id}
func DeleteNetworkPolicyHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveNetworkPolicyStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return
	}

	if err := targetStore.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete network policy: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- helpers ---

// resolveNetworkPolicyStoreForWrite returns the appropriate store for a write operation
// based on the requested scope. Returns nil and writes an error if auth fails.
func resolveNetworkPolicyStoreForWrite(w http.ResponseWriter, r *http.Request, svc *store.Services, scope string) store.NetworkPolicyStore {
	switch scope {
	case "platform":
		if svc.PlatformNetworkPolicies == nil {
			respondError(w, http.StatusServiceUnavailable, "Platform network policy store not available")
			return nil
		}
		if RequirePlatformAdmin(w, r) == nil {
			return nil
		}
		return svc.PlatformNetworkPolicies
	case "org":
		if svc.NetworkPolicies == nil {
			respondError(w, http.StatusServiceUnavailable, "Org network policy store not available")
			return nil
		}
		user := GetPlatformUser(r)
		if user == nil {
			respondError(w, http.StatusUnauthorized, "Authentication required")
			return nil
		}
		if !CanManageOrg(user) {
			respondError(w, http.StatusForbidden, "Organization admin access required")
			return nil
		}
		return svc.NetworkPolicies
	case "team":
		if svc.TeamNetworkPolicies == nil {
			respondError(w, http.StatusServiceUnavailable, "Team network policy store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamNetworkPolicies
	default:
		// No scope specified — default to team
		if svc.TeamNetworkPolicies == nil {
			respondError(w, http.StatusServiceUnavailable, "Team network policy store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamNetworkPolicies
	}
}

// listNetworkPolicies loads network policy rules. When scope is empty, returns
// all three tiers for display (frontend distinguishes editable vs read-only by scope).
func listNetworkPolicies(ctx context.Context, svc *store.Services, scope string) ([]NetworkPolicyListItem, error) {
	switch scope {
	case "platform":
		return listNetworkPoliciesFromStore(ctx, svc.PlatformNetworkPolicies, "platform")
	case "org":
		return listNetworkPoliciesFromStore(ctx, svc.NetworkPolicies, "org")
	case "team":
		return listNetworkPoliciesFromStore(ctx, svc.TeamNetworkPolicies, "team")
	default:
		return listNetworkPoliciesMerged(ctx, svc)
	}
}

func listNetworkPoliciesFromStore(ctx context.Context, s store.NetworkPolicyStore, scope string) ([]NetworkPolicyListItem, error) {
	if s == nil {
		return []NetworkPolicyListItem{}, nil
	}
	rules, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]NetworkPolicyListItem, 0, len(rules))
	for _, r := range rules {
		items = append(items, networkPolicyRuleToListItem(&r, scope))
	}
	sortNetworkPolicyItems(items)
	return items, nil
}

// listNetworkPoliciesMerged returns all rules from all three scopes, tagged by scope.
// Unlike MCP servers which merge by name (lower wins), network policies show all
// rules from all tiers for the UI to display; the effective evaluation uses
// deny-wins-from-above logic which is computed client-side or in Check().
func listNetworkPoliciesMerged(ctx context.Context, svc *store.Services) ([]NetworkPolicyListItem, error) {
	var allItems []NetworkPolicyListItem

	if svc.PlatformNetworkPolicies != nil {
		platformRules, err := svc.PlatformNetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range platformRules {
			allItems = append(allItems, networkPolicyRuleToListItem(&r, "platform"))
		}
	}

	if svc.NetworkPolicies != nil {
		orgRules, err := svc.NetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range orgRules {
			allItems = append(allItems, networkPolicyRuleToListItem(&r, "org"))
		}
	}

	if svc.TeamNetworkPolicies != nil {
		teamRules, err := svc.TeamNetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range teamRules {
			allItems = append(allItems, networkPolicyRuleToListItem(&r, "team"))
		}
	}

	sortNetworkPolicyItems(allItems)
	return allItems, nil
}

func networkPolicyRuleToListItem(r *store.NetworkPolicyRule, scope string) NetworkPolicyListItem {
	item := NetworkPolicyListItem{
		ID:        r.ID,
		Host:      r.Host,
		Port:      r.Port,
		Action:    string(r.Action),
		Scope:     scope,
		CreatedBy: r.CreatedBy,
	}
	if !r.CreatedAt.IsZero() {
		item.CreatedAt = r.CreatedAt.Format("2006-01-02T15:04:05Z")
	}
	if !r.UpdatedAt.IsZero() {
		item.UpdatedAt = r.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}
	return item
}

// sortNetworkPolicyItems sorts by scope priority (platform first), then host, then port.
func sortNetworkPolicyItems(items []NetworkPolicyListItem) {
	scopeOrder := map[string]int{"platform": 0, "org": 1, "team": 2}
	sort.Slice(items, func(i, j int) bool {
		si, sj := scopeOrder[items[i].Scope], scopeOrder[items[j].Scope]
		if si != sj {
			return si < sj
		}
		if items[i].Host != items[j].Host {
			return items[i].Host < items[j].Host
		}
		return items[i].Port < items[j].Port
	})
}
