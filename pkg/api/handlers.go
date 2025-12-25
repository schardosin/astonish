package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

// AgentListItem represents an agent in the list response
type AgentListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Source       string `json:"source"` // "system", "local", or "store"
	HasError     bool   `json:"hasError,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	IsReadOnly   bool   `json:"isReadOnly,omitempty"` // True for store flows
	TapName      string `json:"tapName,omitempty"`    // For store flows: which tap
}

// AgentListResponse is the response for GET /api/agents
type AgentListResponse struct {
	Agents []AgentListItem `json:"agents"`
}

// AgentDetailResponse is the response for GET /api/agents/:name
type AgentDetailResponse struct {
	Name   string              `json:"name"`
	Source string              `json:"source"`
	YAML   string              `json:"yaml"`
	Config *config.AgentConfig `json:"config,omitempty"`
}

// scanAgentsDir scans a directory for agent YAML files
func scanAgentsDir(dir string, source string, agents map[string]AgentListItem) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // Ignore errors (directory might not exist)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-5] // Remove .yaml extension

		// Skip if already found (system has priority)
		if _, exists := agents[name]; exists {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		cfg, err := config.LoadAgent(path)
		if err != nil {
			// Still add the agent but mark it as having an error
			agents[name] = AgentListItem{
				ID:           name,
				Name:         name,
				Description:  "(Error loading agent)",
				Source:       source,
				HasError:     true,
				ErrorMessage: err.Error(),
			}
			continue
		}

		agents[name] = AgentListItem{
			ID:          name,
			Name:        name,
			Description: cfg.Description,
			Source:      source,
		}
	}
}

// findAgentPath finds the path to an agent YAML file
// Returns path, source ("system", "local", or "store"), error
// Handles special ID formats: "store:tapName:flowName" for store flows
func findAgentPath(name string) (string, string, error) {
	// Check for store: prefix (new format)
	if strings.HasPrefix(name, "store:") {
		parts := strings.SplitN(name, ":", 3)
		if len(parts) == 3 {
			tapName, flowName := parts[1], parts[2]
			if store, err := flowstore.NewStore(); err == nil {
				if path, ok := store.GetInstalledFlowPath(tapName, flowName); ok {
					return path, "store", nil
				}
			}
		}
		return "", "", os.ErrNotExist
	}

	// Check system directory first
	if sysDir, err := config.GetAgentsDir(); err == nil {
		path := filepath.Join(sysDir, name+".yaml")
		if _, err := os.Stat(path); err == nil {
			return path, "system", nil
		}
	}

	// Check local directory
	localPath := filepath.Join("agents", name+".yaml")
	if _, err := os.Stat(localPath); err == nil {
		return localPath, "local", nil
	}

	// Check user flows directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		path := filepath.Join(flowsDir, name+".yaml")
		if _, err := os.Stat(path); err == nil {
			return path, "system", nil
		}
	}

	// Check store (for legacy format: "flow" or "tap/flow")
	if store, err := flowstore.NewStore(); err == nil {
		var tapName, flowName string
		if !containsSlash(name) {
			// No slash - assume official
			tapName = flowstore.OfficialStoreName
			flowName = name
		} else {
			// Has slash - parse tap/flow
			parts := splitFirst(name, "/")
			if len(parts) == 2 {
				tapName = parts[0]
				flowName = parts[1]
			}
		}

		if tapName != "" && flowName != "" {
			if path, ok := store.GetInstalledFlowPath(tapName, flowName); ok {
				return path, "store", nil
			}
		}
	}

	return "", "", os.ErrNotExist
}

// containsSlash checks if string contains a slash
func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

// splitFirst splits string on first occurrence of sep
func splitFirst(s, sep string) []string {
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return []string{s}
}

// ListAgentsHandler handles GET /api/agents
func ListAgentsHandler(w http.ResponseWriter, r *http.Request) {
	agents := make(map[string]AgentListItem)

	// Scan system directory first (has priority)
	if sysDir, err := config.GetAgentsDir(); err == nil {
		scanAgentsDir(sysDir, "system", agents)
	}

	// Scan local directory
	scanAgentsDir("agents", "local", agents)

	// Scan new user flows directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		scanAgentsDir(flowsDir, "system", agents)
	}

	// Add installed store flows
	if store, err := flowstore.NewStore(); err == nil {
		for _, tap := range store.GetAllTaps() {
			// Scan the tap's store directory for installed flows
			storeDir := store.GetStoreDir()
			tapDir := filepath.Join(storeDir, tap.Name)
			entries, err := os.ReadDir(tapDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
					continue
				}
				if entry.Name() == "manifest.yaml" {
					continue // Skip manifest
				}
				name := entry.Name()[:len(entry.Name())-5]

				// Build display name: tap/flow for community, just flow for official
				displayName := name
				if tap.Name != flowstore.OfficialStoreName {
					displayName = tap.Name + "/" + name
				}

				// Use source-prefixed ID to avoid conflicts with local copies
				storeID := "store:" + tap.Name + ":" + name

				path := filepath.Join(tapDir, entry.Name())
				cfg, err := config.LoadAgent(path)
				desc := "(Installed from store)"
				if err == nil && cfg.Description != "" {
					desc = cfg.Description
				}

				agents[storeID] = AgentListItem{
					ID:          storeID,
					Name:        displayName,
					Description: desc,
					Source:      "store",
					IsReadOnly:  true,
					TapName:     tap.Name,
				}
			}
		}
	}

	// Convert map to sorted slice
	result := make([]AgentListItem, 0, len(agents))
	var names []string
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		result = append(result, agents[name])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AgentListResponse{Agents: result})
}

// GetAgentHandler handles GET /api/agents/{name}
func GetAgentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	path, source, err := findAgentPath(name)
	if err != nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Read raw YAML content
	yamlData, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "Failed to read agent file", http.StatusInternalServerError)
		return
	}

	// Parse config
	cfg, err := config.LoadAgent(path)
	if err != nil {
		// Return YAML even if parsing fails
		cfg = nil
	}

	response := AgentDetailResponse{
		Name:   name,
		Source: source,
		YAML:   string(yamlData),
		Config: cfg,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SaveAgentHandler handles PUT /api/agents/{name}
func SaveAgentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	var request struct {
		YAML string `json:"yaml"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Parse the incoming YAML to analyze tools and resolve dependencies
	finalYAML := request.YAML
	var agentConfig config.AgentConfig
	if err := yaml.Unmarshal([]byte(request.YAML), &agentConfig); err == nil {
		// Collect all tools used in the flow
		tools := CollectToolsFromNodes(agentConfig.Nodes)

		if len(tools) > 0 {
			// Get cached tools and store servers for resolution
			cachedTools := GetCachedTools()

			// Get MCP store servers (from all taps)
			storeServers, _ := loadAllServersFromTaps()

			// Resolve dependencies
			deps := ResolveMCPDependencies(tools, cachedTools, storeServers, agentConfig.MCPDependencies)

			// Check if mcp_dependencies section already exists
			hasMcpDeps := len(agentConfig.MCPDependencies) > 0

			// Only add deps if we have them AND section doesn't exist yet
			// (Avoids re-encoding when deps already exist, preserving formatting)
			if len(deps) > 0 && !hasMcpDeps {
				// Parse as yaml.Node to preserve ordering
				var rootNode yaml.Node
				if err := yaml.Unmarshal([]byte(request.YAML), &rootNode); err == nil && rootNode.Kind == yaml.DocumentNode && len(rootNode.Content) > 0 {
					mapNode := rootNode.Content[0]
					if mapNode.Kind == yaml.MappingNode {
						// Find and remove existing mcp_dependencies, and find layout index
						var layoutIndex = -1
						var mcpDepsIndex = -1
						for i := 0; i < len(mapNode.Content); i += 2 {
							if mapNode.Content[i].Value == "mcp_dependencies" {
								mcpDepsIndex = i
							}
							if mapNode.Content[i].Value == "layout" {
								layoutIndex = i
							}
						}

						// Remove existing mcp_dependencies if present
						if mcpDepsIndex >= 0 {
							mapNode.Content = append(mapNode.Content[:mcpDepsIndex], mapNode.Content[mcpDepsIndex+2:]...)
							// Adjust layout index if it was after mcp_dependencies
							if layoutIndex > mcpDepsIndex {
								layoutIndex -= 2
							}
						}

						// Marshal deps to yaml.Node
						depsBytes, _ := yaml.Marshal(deps)
						var depsNode yaml.Node
						yaml.Unmarshal(depsBytes, &depsNode)

						// Create key node
						keyNode := &yaml.Node{
							Kind:  yaml.ScalarNode,
							Tag:   "!!str",
							Value: "mcp_dependencies",
						}

						// Insert before layout if layout exists, otherwise append
						if layoutIndex >= 0 {
							// Insert before layout
							newContent := make([]*yaml.Node, 0, len(mapNode.Content)+2)
							newContent = append(newContent, mapNode.Content[:layoutIndex]...)
							newContent = append(newContent, keyNode, depsNode.Content[0])
							newContent = append(newContent, mapNode.Content[layoutIndex:]...)
							mapNode.Content = newContent
						} else {
							// Append at end
							mapNode.Content = append(mapNode.Content, keyNode, depsNode.Content[0])
						}

						// Re-marshal with 2-space indentation to match frontend
						var buf bytes.Buffer
						encoder := yaml.NewEncoder(&buf)
						encoder.SetIndent(2)
						if err := encoder.Encode(&rootNode); err == nil {
							finalYAML = buf.String()
						}
						encoder.Close()
					}
				}
			}
		} else {
			// No tools - remove mcp_dependencies if it exists
			var rootNode yaml.Node
			if err := yaml.Unmarshal([]byte(request.YAML), &rootNode); err == nil && rootNode.Kind == yaml.DocumentNode && len(rootNode.Content) > 0 {
				mapNode := rootNode.Content[0]
				if mapNode.Kind == yaml.MappingNode {
					// Find mcp_dependencies
					for i := 0; i < len(mapNode.Content); i += 2 {
						if mapNode.Content[i].Value == "mcp_dependencies" {
							// Remove it
							mapNode.Content = append(mapNode.Content[:i], mapNode.Content[i+2:]...)
							// Re-marshal
							var buf bytes.Buffer
							encoder := yaml.NewEncoder(&buf)
							encoder.SetIndent(2)
							if err := encoder.Encode(&rootNode); err == nil {
								finalYAML = buf.String()
							}
							encoder.Close()
							break
						}
					}
				}
			}
		}
	}

	// Save to system directory (~/.config/astonish/agents/)
	sysDir, err := config.GetAgentsDir()
	if err != nil {
		http.Error(w, "Failed to get agents directory", http.StatusInternalServerError)
		return
	}

	path := filepath.Join(sysDir, name+".yaml")

	// Ensure directory exists
	if err := os.MkdirAll(sysDir, 0755); err != nil {
		http.Error(w, "Failed to create agents directory", http.StatusInternalServerError)
		return
	}

	// Write file
	if err := os.WriteFile(path, []byte(finalYAML), 0644); err != nil {
		http.Error(w, "Failed to save agent file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": path, "yaml": finalYAML})
}

// DeleteAgentHandler handles DELETE /api/agents/{name}
func DeleteAgentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	path, _, err := findAgentPath(name)
	if err != nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Delete the file
	if err := os.Remove(path); err != nil {
		http.Error(w, "Failed to delete agent file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "deleted": name})
}

// CopyAgentToLocalHandler handles POST /api/agents/{name}/copy-to-local
// Copies a store flow to the user's local agents directory for editing
func CopyAgentToLocalHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	// Find the source file
	sourcePath, source, err := findAgentPath(name)
	if err != nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	if source != "store" {
		http.Error(w, "Agent is not from store, already editable", http.StatusBadRequest)
		return
	}

	// Read source content
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		http.Error(w, "Failed to read agent file", http.StatusInternalServerError)
		return
	}

	// Determine destination: extract just the flow name
	// Handle store:tap:name format (e.g., "store:official:my_flow" -> "my_flow")
	destName := name
	if strings.HasPrefix(name, "store:") {
		// Parse store:tap:flowName format
		parts := strings.SplitN(name, ":", 3)
		if len(parts) == 3 {
			destName = parts[2] // Just the flow name
		}
	} else if containsSlash(name) {
		// Handle legacy tap/flow format
		parts := splitFirst(name, "/")
		if len(parts) == 2 {
			destName = parts[1]
		}
	}

	// Save to system directory (~/.config/astonish/agents/)
	sysDir, err := config.GetAgentsDir()
	if err != nil {
		http.Error(w, "Failed to get agents directory", http.StatusInternalServerError)
		return
	}

	destPath := filepath.Join(sysDir, destName+".yaml")

	// Check if already exists
	if _, err := os.Stat(destPath); err == nil {
		http.Error(w, "Agent already exists locally: "+destName, http.StatusConflict)
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(sysDir, 0755); err != nil {
		http.Error(w, "Failed to create agents directory", http.StatusInternalServerError)
		return
	}

	// Write file
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		http.Error(w, "Failed to copy agent file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"newName": destName,
		"path":    destPath,
		"message": "Flow copied to local. You can now edit it.",
	})
}

// ToolInfo represents a tool in the list response
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "internal" or MCP server name
}

// ToolsListResponse is the response for GET /api/tools
type ToolsListResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// ListToolsHandler handles GET /api/tools
func ListToolsHandler(w http.ResponseWriter, r *http.Request) {
	// Use cached tools (initialized at startup)
	allTools := GetCachedTools()

	// If cache not ready, return empty list
	if allTools == nil {
		allTools = []ToolInfo{}
	}

	// Sort tools by name
	sort.Slice(allTools, func(i, j int) bool {
		return allTools[i].Name < allTools[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToolsListResponse{Tools: allTools})
}

// minimalReadonlyContext implements agent.ReadonlyContext for tool listing
type minimalReadonlyContext struct {
	context.Context
}

func (m *minimalReadonlyContext) AgentName() string                    { return "tools-api" }
func (m *minimalReadonlyContext) AppName() string                      { return "astonish" }
func (m *minimalReadonlyContext) UserContent() *genai.Content          { return nil }
func (m *minimalReadonlyContext) InvocationID() string                 { return "" }
func (m *minimalReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalReadonlyContext) UserID() string                       { return "" }
func (m *minimalReadonlyContext) SessionID() string                    { return "" }
func (m *minimalReadonlyContext) Branch() string                       { return "" }

// RegisterRoutes registers the API routes on a router
func RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/api/agents", ListAgentsHandler).Methods("GET")
	router.HandleFunc("/api/agents/{name}", GetAgentHandler).Methods("GET")
	router.HandleFunc("/api/agents/{name}", SaveAgentHandler).Methods("PUT")
	router.HandleFunc("/api/agents/{name}", DeleteAgentHandler).Methods("DELETE")
	router.HandleFunc("/api/agents/{name:.*}/copy-to-local", CopyAgentToLocalHandler).Methods("POST")
	router.HandleFunc("/api/tools", ListToolsHandler).Methods("GET")
	router.HandleFunc("/api/tools/web-capable", WebCapableToolsHandler).Methods("GET")
	router.HandleFunc("/api/ai/chat", AIChatHandler).Methods("POST")
	router.HandleFunc("/api/ai/tool-search", AIToolSearchHandler).Methods("POST")
	router.HandleFunc("/api/ai/tool-search-internet", AIToolSearchInternetHandler).Methods("POST")
	router.HandleFunc("/api/ai/url-extract", URLExtractHandler).Methods("POST")
	router.HandleFunc("/api/mcp-internet-install", InternetMCPInstallHandler).Methods("POST")
	router.HandleFunc("/api/mcp-dependencies/check", CheckMCPDependenciesHandler).Methods("POST")

	// Settings endpoints
	router.HandleFunc("/api/settings/config", GetSettingsHandler).Methods("GET")
	router.HandleFunc("/api/settings/config", UpdateSettingsHandler).Methods("PUT")
	router.HandleFunc("/api/settings/mcp", GetMCPSettingsHandler).Methods("GET")
	router.HandleFunc("/api/settings/mcp", UpdateMCPSettingsHandler).Methods("PUT")
	router.HandleFunc("/api/mcp/install-inline", InstallInlineMCPServerHandler).Methods("POST")
	router.HandleFunc("/api/settings/status", GetSetupStatusHandler).Methods("GET")

	// Provider endpoints
	router.HandleFunc("/api/providers/{providerId}/models", ListProviderModelsHandler).Methods("GET")
	router.HandleFunc("/api/providers/{providerId}/models-metadata", ListProviderModelsWithMetadataHandler).Methods("GET")

	// MCP Store endpoints
	router.HandleFunc("/api/mcp-store", ListMCPStoreHandler).Methods("GET")
	router.HandleFunc("/api/mcp-store/tags", GetMCPStoreTagsHandler).Methods("GET")
	router.HandleFunc("/api/mcp-store/{id:.*}/install", InstallMCPStoreServerHandler).Methods("POST")
	router.HandleFunc("/api/mcp-store/{id:.*}", GetMCPStoreServerHandler).Methods("GET")

	// Unified Taps endpoints (new)
	router.HandleFunc("/api/taps", ListTapsHandler).Methods("GET")
	router.HandleFunc("/api/taps", AddTapHandler).Methods("POST")
	router.HandleFunc("/api/taps/{name}", RemoveTapHandler).Methods("DELETE")
	router.HandleFunc("/api/taps/update", UpdateFlowStoreHandler).Methods("POST")

	// Flow Store endpoints
	router.HandleFunc("/api/flow-store", ListFlowStoreHandler).Methods("GET")
	router.HandleFunc("/api/flow-store/update", UpdateFlowStoreHandler).Methods("POST")
	router.HandleFunc("/api/flow-store/taps", ListTapsHandler).Methods("GET")
	router.HandleFunc("/api/flow-store/taps", AddTapHandler).Methods("POST")
	router.HandleFunc("/api/flow-store/taps/{name}", RemoveTapHandler).Methods("DELETE")
	router.HandleFunc("/api/flow-store/{tap}/{flow}/install", InstallFlowHandler).Methods("POST")
	router.HandleFunc("/api/flow-store/{tap}/{flow}", UninstallFlowHandler).Methods("DELETE")

	// Execution endpoints
	router.HandleFunc("/api/chat", HandleChat).Methods("POST")
	router.HandleFunc("/api/session/{id}/stop", HandleStopSession).Methods("POST")
	router.HandleFunc("/api/session/{id}/keepalive", HandleSessionKeepalive).Methods("POST")
}
