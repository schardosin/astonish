package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
)

// AgentListItem represents an agent in the list response
type AgentListItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "system" or "local"
}

// AgentListResponse is the response for GET /api/agents
type AgentListResponse struct {
	Agents []AgentListItem `json:"agents"`
}

// AgentDetailResponse is the response for GET /api/agents/:name
type AgentDetailResponse struct {
	Name        string              `json:"name"`
	Source      string              `json:"source"`
	YAML        string              `json:"yaml"`
	Config      *config.AgentConfig `json:"config,omitempty"`
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
			continue // Skip invalid agents
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
func findAgentPath(name string) (string, string, error) {
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

	return "", "", os.ErrNotExist
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
	if err := os.WriteFile(path, []byte(request.YAML), 0644); err != nil {
		http.Error(w, "Failed to save agent file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": path})
}

// RegisterRoutes registers the API routes on a router
func RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/api/agents", ListAgentsHandler).Methods("GET")
	router.HandleFunc("/api/agents/{name}", GetAgentHandler).Methods("GET")
	router.HandleFunc("/api/agents/{name}", SaveAgentHandler).Methods("PUT")
}
