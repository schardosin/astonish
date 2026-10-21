package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

// MCPServerListItem represents an MCP server in listing responses.
type MCPServerListItem struct {
	Name        string            `json:"name"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Enabled     bool              `json:"enabled"`
	Scope       string            `json:"scope"` // "org" or "team"
	HasTools    bool              `json:"has_tools"`
	ToolCount   int               `json:"tool_count"`
	CachedTools json.RawMessage   `json:"cached_tools,omitempty"`
}

// MCPPlatformServersListResponse is the response for GET /api/mcp-platform/servers.
type MCPPlatformServersListResponse struct {
	Servers     []MCPServerListItem `json:"servers"`
	IsTeamAdmin bool                `json:"is_team_admin"`
	IsOrgAdmin  bool                `json:"is_org_admin"`
}

// MCPServerCreateRequest is the request body for creating/updating an MCP server.
type MCPServerCreateRequest struct {
	Name      string            `json:"name"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"`
	URL       string            `json:"url,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
}

// MCPServerUpdateRequest distinguishes omitted fields from explicit empty values.
// Null, like omission, leaves the existing field unchanged.
type MCPServerUpdateRequest struct {
	Name      *string           `json:"name"`
	Command   *string           `json:"command"`
	Args      *[]string         `json:"args"`
	Env       map[string]string `json:"env"`
	Transport *string           `json:"transport"`
	URL       *string           `json:"url"`
	Enabled   *bool             `json:"enabled"`
}

// ListMCPPlatformServersHandler handles GET /api/mcp-platform/servers
//
// Query params:
//   - scope=team: return only team MCP servers
//   - scope=org: return only org MCP servers
//   - (empty): return merged view (org + team, team overrides org by name)
func ListMCPPlatformServersHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	items, err := listMCPServersPlatform(svc, scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load MCP servers: "+err.Error())
		return
	}

	resp := MCPPlatformServersListResponse{
		Servers:     items,
		IsTeamAdmin: IsTeamAdmin(r),
		IsOrgAdmin:  CanManageOrg(GetPlatformUser(r)),
	}
	respondJSON(w, http.StatusOK, resp)
}

// GetMCPPlatformServerHandler handles GET /api/mcp-platform/servers/{name}
func GetMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	mcpStore := resolveMCPStoreForRead(svc, scope)
	if mcpStore == nil {
		respondError(w, http.StatusServiceUnavailable, "MCP server store not available for scope: "+scope)
		return
	}

	server, err := mcpStore.Get(r.Context(), name)
	if err != nil || server == nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	item := mcpServerToListItem(server, scope)
	respondJSON(w, http.StatusOK, item)
}

// CreateMCPPlatformServerHandler handles POST /api/mcp-platform/servers
func CreateMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var req MCPServerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if req.Transport == "stdio" && req.Command == "" {
		respondError(w, http.StatusBadRequest, "Command is required for stdio transport")
		return
	}
	if req.Transport == "sse" && req.URL == "" {
		respondError(w, http.StatusBadRequest, "URL is required for SSE transport")
		return
	}

	// Get user ID for created_by
	createdBy := ""
	if user := GetPlatformUser(r); user != nil {
		createdBy = user.ID
	}

	server := &store.MCPServer{
		Name:      req.Name,
		Command:   req.Command,
		Args:      req.Args,
		Env:       req.Env,
		Transport: req.Transport,
		URL:       req.URL,
		Enabled:   req.Enabled,
		CreatedBy: createdBy,
	}

	if err := restoreMCPServerSecrets(server, nil); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := targetStore.Save(r.Context(), server); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save MCP server: "+err.Error())
		return
	}

	// Trigger async tool discovery to populate cached_tools.
	runtimeCtx := detachedRuntimeNetworkPolicyContext(r, effectiveAppConfig(r))
	refreshMCPPlatformServer(runtimeCtx, targetStore, server, buildPGSessionRegistry(r.Context()))

	respondJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": req.Name})
}

// UpdateMCPPlatformServerHandler handles PUT /api/mcp-platform/servers/{name}
func UpdateMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var req MCPServerUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Read only from the exact store that has already authorized this write.
	existing, err := targetStore.Get(r.Context(), name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load MCP server")
		return
	}
	server := &store.MCPServer{Name: name, Transport: "stdio"}
	if existing != nil {
		*server = *existing
	}
	if req.Name != nil && *req.Name != "" {
		server.Name = *req.Name
	}
	if req.Command != nil {
		server.Command = *req.Command
	}
	if req.Args != nil {
		server.Args = *req.Args
	}
	if req.Transport != nil {
		server.Transport = *req.Transport
	}
	if req.Enabled != nil {
		server.Enabled = req.Enabled
	}
	// Omitted secrets are preserved through the same destination checks as masks.
	server.Env = req.Env
	if req.URL != nil {
		server.URL = *req.URL
	} else if existing != nil {
		server.URL = redactBrowserURL(existing.URL)
	}
	if err := restoreMCPServerSecrets(server, existing); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if user := GetPlatformUser(r); user != nil {
		server.CreatedBy = user.ID
	}

	if err := targetStore.Save(r.Context(), server); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update MCP server: "+err.Error())
		return
	}

	// Trigger async tool discovery to refresh cached_tools when config changes.
	runtimeCtx := detachedRuntimeNetworkPolicyContext(r, effectiveAppConfig(r))
	refreshMCPPlatformServer(runtimeCtx, targetStore, server, buildPGSessionRegistry(r.Context()))

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": server.Name})
}

// DeleteMCPPlatformServerHandler handles DELETE /api/mcp-platform/servers/{name}
func DeleteMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	if err := targetStore.Delete(r.Context(), name); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete MCP server: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ToggleMCPPlatformServerHandler handles PATCH /api/mcp-platform/servers/{name}
// Toggles the enabled state of an MCP server.
func ToggleMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Get existing server
	existing, err := targetStore.Get(r.Context(), name)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	existing.Enabled = body.Enabled
	if err := targetStore.Save(r.Context(), existing); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update MCP server: "+err.Error())
		return
	}

	// Reset the chat agent so it reinitializes with the updated enabled state.
	// Without this, the singleton keeps the old tool configuration.
	GetChatManager().Reset()

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RefreshMCPPlatformServerHandler handles POST /api/mcp-platform/servers/{name}/refresh
// Triggers async tool discovery for an MCP server.
func RefreshMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	// Get the server config
	server, err := targetStore.Get(r.Context(), name)
	if err != nil || server == nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	// Trigger async background refresh.
	runtimeCtx := detachedRuntimeNetworkPolicyContext(r, effectiveAppConfig(r))
	refreshMCPPlatformServer(runtimeCtx, targetStore, server, buildPGSessionRegistry(r.Context()))

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "refresh_started", "name": name})
}

// --- Platform mode helpers ---

// resolveMCPStoreForRead returns the appropriate MCPServerStore for a read operation.
func resolveMCPStoreForRead(svc *store.Services, scope string) store.MCPServerStore {
	switch scope {
	case "team":
		return svc.TeamMCPServers
	case "org":
		return svc.MCPServers
	case "platform":
		return svc.PlatformMCPServers
	default:
		// For merged reads, caller should use listMCPServersPlatform
		return svc.TeamMCPServers
	}
}

type mcpWriteScope string

const (
	mcpScopeTeam     mcpWriteScope = "team"
	mcpScopeOrg      mcpWriteScope = "org"
	mcpScopePlatform mcpWriteScope = "platform"
)

// resolveMCPStoreForWrite authorizes the exact write scope before returning a
// store. Omitted scope means team; effective/unknown scopes are never writable.
func resolveMCPStoreForWrite(w http.ResponseWriter, r *http.Request, svc *store.Services, scope string) store.MCPServerStore {
	selected := mcpWriteScope(scope)
	if selected == "" {
		selected = mcpScopeTeam
	}
	if selected != mcpScopeTeam && selected != mcpScopeOrg && selected != mcpScopePlatform {
		respondError(w, http.StatusBadRequest, "Invalid MCP write scope")
		return nil
	}
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP server store not available")
		return nil
	}
	var target store.MCPServerStore
	switch selected {
	case mcpScopePlatform:
		if RequirePlatformAdmin(w, r) == nil {
			return nil
		}
		target = svc.PlatformMCPServers
	case mcpScopeOrg:
		if RequireOrgAdmin(w, r) == nil {
			return nil
		}
		target = svc.MCPServers
	case mcpScopeTeam:
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		target = svc.TeamMCPServers
	}
	if target == nil {
		respondError(w, http.StatusServiceUnavailable, "MCP server store not available for scope: "+string(selected))
	}
	return target
}

// mcpStoreForWrite is the request-level gate used before decoding an install
// body, fetching a catalog, discovering tools, or changing persisted state.
func mcpStoreForWrite(w http.ResponseWriter, r *http.Request) store.MCPServerStore {
	return resolveMCPStoreForWrite(w, r, store.FromRequest(r), r.URL.Query().Get("scope"))
}

// listMCPServersPlatform loads MCP servers with three-tier merge: platform → org → team.
// Lower tiers override higher tiers by name.
func listMCPServersPlatform(svc *store.Services, scope string) ([]MCPServerListItem, error) {
	switch scope {
	case "platform":
		return listMCPServersFromStore(svc.PlatformMCPServers, "platform")
	case "team":
		return listMCPServersFromStore(svc.TeamMCPServers, "team")
	case "org":
		return listMCPServersFromStore(svc.MCPServers, "org")
	default:
		return listMCPServersMerged(svc)
	}
}

// listMCPServersFromStore lists MCP servers from a single store.
func listMCPServersFromStore(mcpStore store.MCPServerStore, scope string) ([]MCPServerListItem, error) {
	if mcpStore == nil {
		return []MCPServerListItem{}, nil
	}
	servers, err := mcpStore.List(context.TODO())
	if err != nil {
		return nil, err
	}
	items := make([]MCPServerListItem, 0, len(servers))
	for _, s := range servers {
		item := mcpServerToListItem(&s, scope)
		items = append(items, item)
	}
	sortMCPServerItems(items)
	return items, nil
}

// listMCPServersMerged returns all MCP servers merged: platform → org → team (lower wins by name).
func listMCPServersMerged(svc *store.Services) ([]MCPServerListItem, error) {
	byName := make(map[string]MCPServerListItem)

	// 1. Platform servers as base (inherited by all)
	if svc.PlatformMCPServers != nil {
		platformServers, err := svc.PlatformMCPServers.List(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, s := range platformServers {
			item := mcpServerToListItem(&s, "platform")
			byName[s.Name] = item
		}
	}

	// 2. Org servers override platform by name
	if svc.MCPServers != nil {
		orgServers, err := svc.MCPServers.List(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, s := range orgServers {
			item := mcpServerToListItem(&s, "org")
			byName[s.Name] = item
		}
	}

	// 3. Team servers override org+platform by name
	if svc.TeamMCPServers != nil {
		teamServers, err := svc.TeamMCPServers.List(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, s := range teamServers {
			item := mcpServerToListItem(&s, "team")
			byName[s.Name] = item
		}
	}

	items := make([]MCPServerListItem, 0, len(byName))
	for _, item := range byName {
		items = append(items, item)
	}
	sortMCPServerItems(items)
	return items, nil
}

// mcpSecretMask never exposes even a suffix of an environment literal.
const mcpSecretMask = "****"

// Only a complete reference is public: a literal prefix/suffix may itself be secret.
var mcpCredentialReference = regexp.MustCompile(`^\{\{CREDENTIAL:[^:}]+:[^}]+\}\}$`)

// redactMCPEnv returns an independent response copy. Nil and explicit empty maps
// remain distinct; credential references are never resolved for API responses.
func redactMCPEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	redacted := make(map[string]string, len(env))
	for key, value := range env {
		if value != "" && !mcpCredentialReference.MatchString(value) {
			value = mcpSecretMask
		}
		redacted[key] = value
	}
	return redacted
}

// restoreMCPServerSecrets validates a proposed write before persistence/discovery.
// existing MUST come from the exact authorized target store/name, never an
// effective/inherited read. Nil existing denotes creation and rejects masks.
// A nil Env preserves existing entries, an empty map clears them. URL omission
// must be resolved by the caller (use the stored redacted URL, not the raw URL).
// Restoration is atomic: neither input changes on error; on success only updated
// receives a new Env map and restored URL. Explicit replacements need no match.
func restoreMCPServerSecrets(updated, existing *store.MCPServer) error {
	storedURL := ""
	var storedEnv map[string]string
	var storedCommand string
	var storedArgs []string
	if existing != nil {
		storedURL, storedEnv = existing.URL, existing.Env
		storedCommand, storedArgs = existing.Command, existing.Args
	}
	resolvedURL, err := restoreBrowserURL(&updated.URL, storedURL)
	if err != nil {
		return errors.New("MCP URL placeholder has no matching stored value")
	}
	inherited := resolvedURL != updated.URL

	// Command and Args are redacted in the GET/list responses, so the Studio
	// settings page echoes them back masked. Restore each masked value from the
	// exact stored raw value when the surrounding shape (executable + flag
	// names) is unchanged; a changed executable with a retained mask, or a mask
	// with no matching stored secret, is rejected rather than persisting "****"
	// or silently leaking the stored secret to a new command.
	restoredCommand, cmdInherited, err := restoreMCPMaskedCommand(updated.Command, storedCommand)
	if err != nil {
		return err
	}
	inherited = inherited || cmdInherited

	restoredArgs, argsInherited, err := restoreMCPMaskedArgs(updated.Args, storedArgs)
	if err != nil {
		return err
	}
	inherited = inherited || argsInherited
	env := updated.Env
	if env == nil && existing != nil {
		env = redactMCPEnv(storedEnv)
		for _, value := range storedEnv {
			if value != "" {
				inherited = true
			}
		}
	}
	var resolvedEnv map[string]string
	if env != nil {
		resolvedEnv = make(map[string]string, len(env))
	}
	for key, value := range env {
		if strings.Contains(value, mcpSecretMask) {
			old, ok := storedEnv[key]
			if value != mcpSecretMask || !ok || old == "" || strings.Contains(old, mcpSecretMask) || mcpCredentialReference.MatchString(old) {
				return errors.New("MCP environment placeholder has no matching stored value")
			}
			value = old
			inherited = true
		}
		resolvedEnv[key] = value
	}
	if inherited && (existing == nil || updated.Name != existing.Name || restoredCommand != existing.Command || !slices.Equal(restoredArgs, existing.Args) || updated.Transport != existing.Transport || resolvedURL != existing.URL) {
		return errors.New("MCP executable or destination changed; replace or clear retained credentials")
	}
	// Restoration is atomic: the caller's struct is only mutated once every
	// field validates, so an error never leaves a decoded plaintext secret in
	// a struct the caller might log or echo.
	updated.Command, updated.Args, updated.Env, updated.URL = restoredCommand, restoredArgs, resolvedEnv, resolvedURL
	return nil
}

// restoreMCPMaskedCommand restores a redacted Command string to its stored raw
// value. If the incoming command contains no mask it is returned verbatim (a
// fresh, explicit value). If it contains the mask it is accepted only when it
// is byte-for-byte the redaction of the stored command (shape unchanged and the
// stored command actually carried a secret); otherwise the mask has no matching
// stored secret or the executable changed, and it is rejected. Returns the
// restored value and whether a stored secret was inherited.
func restoreMCPMaskedCommand(updated, stored string) (string, bool, error) {
	if !strings.Contains(updated, mcpSecretMask) {
		return updated, false, nil
	}
	masked := redactMCPCommand(stored)
	if stored == "" || masked == stored || updated != masked {
		return "", false, errors.New("MCP command placeholder has no matching stored value")
	}
	return stored, true, nil
}

// restoreMCPMaskedArgs restores a redacted Args slice element-wise to its stored
// raw values. An element with no mask is kept verbatim. A masked element is
// accepted only when the whole slice is byte-for-byte the redaction of the
// stored argv (same length, same flag shape, stored actually carried a secret);
// otherwise it is rejected. The returned slice is a fresh copy. Returns the
// restored slice and whether a stored secret was inherited.
func restoreMCPMaskedArgs(updated, stored []string) ([]string, bool, error) {
	// No element carries a mask: accept verbatim (fresh/explicit argv).
	hasMask := false
	for _, a := range updated {
		if strings.Contains(a, mcpSecretMask) {
			hasMask = true
			break
		}
	}
	if !hasMask {
		return updated, false, nil
	}
	// A masked element is only legitimate if the whole incoming argv is exactly
	// the redaction of the stored argv — same length and same flag shape — and
	// the stored argv actually differed (carried a secret that was masked).
	maskedStored := redactMCPArgs(stored)
	if len(updated) != len(maskedStored) || len(stored) == 0 {
		return nil, false, errors.New("MCP args placeholder has no matching stored value")
	}
	for i := range updated {
		if updated[i] != maskedStored[i] {
			return nil, false, errors.New("MCP args placeholder has no matching stored value")
		}
	}
	if slices.Equal(maskedStored, stored) {
		// Stored argv had nothing to mask, so a mask cannot be inherited.
		return nil, false, errors.New("MCP args placeholder has no matching stored value")
	}
	out := make([]string, len(stored))
	copy(out, stored)
	return out, true, nil
}

// mcpServerToListItem converts a store.MCPServer to a MCPServerListItem.
func mcpServerToListItem(s *store.MCPServer, scope string) MCPServerListItem {
	enabled := true
	if s.Enabled != nil {
		enabled = *s.Enabled
	}

	toolCount := 0
	hasTools := false
	if len(s.CachedTools) > 0 {
		var tools []json.RawMessage
		if err := json.Unmarshal(s.CachedTools, &tools); err == nil {
			toolCount = len(tools)
			hasTools = toolCount > 0
		}
	}

	return MCPServerListItem{
		Name:        s.Name,
		Command:     redactMCPCommand(s.Command),
		Args:        redactMCPArgs(s.Args),
		Env:         redactMCPEnv(s.Env),
		Transport:   s.Transport,
		URL:         redactBrowserURL(s.URL),
		Enabled:     enabled,
		Scope:       scope,
		HasTools:    hasTools,
		ToolCount:   toolCount,
		CachedTools: s.CachedTools,
	}
}

// sortMCPServerItems sorts by name alphabetically.
func sortMCPServerItems(items []MCPServerListItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
}

// refreshMCPPlatformServer performs async tool discovery for a platform MCP server.
// It routes through asyncDiscoverAndCacheTools which runs in a goroutine with
// a dedicated timeout, using sandbox for stdio servers and direct network for SSE.
func refreshMCPPlatformServer(runtimeCtx context.Context, mcpStore store.MCPServerStore, server *store.MCPServer, sessRegistry *sandbox.SessionRegistry) {
	cfg := mcpServerToConfig(server)
	asyncDiscoverAndCacheTools(runtimeCtx, mcpStore, server.Name, cfg, sessRegistry)
}

// MCPDiscoveredTool represents a tool discovered from an MCP server.
type MCPDiscoveredTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// mcpServerConfigUnchanged returns true if the executable config of two MCP servers
// is the same (command, args, transport, url). Used to skip redundant tool discovery.
func mcpServerConfigUnchanged(existing, updated *store.MCPServer) bool {
	if existing.Command != updated.Command {
		return false
	}
	if existing.Transport != updated.Transport {
		return false
	}
	if existing.URL != updated.URL {
		return false
	}
	if len(existing.Args) != len(updated.Args) {
		return false
	}
	for i := range existing.Args {
		if existing.Args[i] != updated.Args[i] {
			return false
		}
	}
	// If existing has no cached tools, treat as changed so discovery runs
	if len(existing.CachedTools) == 0 {
		return false
	}
	return true
}
