// Package mcpstore provides access to the embedded MCP server catalog.
package mcpstore

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed data/store.json
var storeData embed.FS

// ServerConfig represents an MCP server configuration
type ServerConfig struct {
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport,omitempty"`
	URL       string            `json:"url,omitempty"`
}

// Server represents an MCP server from the store
type Server struct {
	McpId          string        `json:"mcpId"`
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Author         string        `json:"author"`
	GithubUrl      string        `json:"githubUrl"`
	Tags           []string      `json:"tags"`
	GithubStars    int           `json:"githubStars"`
	RequiresApiKey bool          `json:"requiresApiKey"`
	Config         *ServerConfig `json:"config,omitempty"`
	Source         string        `json:"source,omitempty"` // "official" or tap name
}

var (
	servers     []Server
	serversOnce sync.Once
	loadErr     error
)

// loadServers loads the embedded server data
func loadServers() ([]Server, error) {
	serversOnce.Do(func() {
		data, err := storeData.ReadFile("data/store.json")
		if err != nil {
			loadErr = err
			return
		}

		if err := json.Unmarshal(data, &servers); err != nil {
			loadErr = err
			return
		}

		// Sort by GitHub stars (descending) so most popular appear first
		sort.Slice(servers, func(i, j int) bool {
			return servers[i].GithubStars > servers[j].GithubStars
		})
	})
	return servers, loadErr
}

// ListServers returns all servers in the store
func ListServers() ([]Server, error) {
	return loadServers()
}

// TappedMCPInput represents MCP data from flowstore taps
type TappedMCPInput struct {
	Name        string
	Description string
	Command     string
	Args        []string
	Env         map[string]string
	Tags        []string
	TapName     string
}

// ListServersWithTapped returns all servers merged with additional MCPs from taps
// Official curated servers come first, then tapped servers grouped by tap
func ListServersWithTapped(tappedMCPs []TappedMCPInput) ([]Server, error) {
	embedded, err := loadServers()
	if err != nil {
		return nil, err
	}

	// Mark embedded servers as "official"
	result := make([]Server, 0, len(embedded)+len(tappedMCPs))
	for _, srv := range embedded {
		srv.Source = "official"
		result = append(result, srv)
	}

	// Add tapped MCPs
	for _, mcp := range tappedMCPs {
		result = append(result, Server{
			McpId:       mcp.TapName + "/" + mcp.Name,
			Name:        mcp.Name,
			Description: mcp.Description,
			Tags:        mcp.Tags,
			Source:      mcp.TapName,
			Config: &ServerConfig{
				Command: mcp.Command,
				Args:    mcp.Args,
				Env:     mcp.Env,
			},
		})
	}

	return result, nil
}

// ListInstallableServers returns only servers that have a valid config (can be installed)
func ListInstallableServers() ([]Server, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	var installable []Server
	for _, srv := range allServers {
		if srv.Config != nil && srv.Config.Command != "" {
			installable = append(installable, srv)
		}
	}

	return installable, nil
}

// SearchServers searches servers by query (matches name, description, author, or tags)
func SearchServers(query string) ([]Server, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	if query == "" {
		return allServers, nil
	}

	query = strings.ToLower(query)
	var results []Server

	for _, srv := range allServers {
		if matchesQuery(srv, query) {
			results = append(results, srv)
		}
	}

	return results, nil
}

// GetServer returns a server by its mcpId
func GetServer(mcpId string) (*Server, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	for _, srv := range allServers {
		if srv.McpId == mcpId {
			return &srv, nil
		}
	}

	return nil, nil
}

// GetServerByName returns a server by its name (case-insensitive)
func GetServerByName(name string) (*Server, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	nameLower := strings.ToLower(name)
	for _, srv := range allServers {
		if strings.ToLower(srv.Name) == nameLower {
			return &srv, nil
		}
	}

	return nil, nil
}

// GetServersByTag returns all servers with a specific tag
func GetServersByTag(tag string) ([]Server, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	tagLower := strings.ToLower(tag)
	var results []Server

	for _, srv := range allServers {
		for _, t := range srv.Tags {
			if strings.ToLower(t) == tagLower {
				results = append(results, srv)
				break
			}
		}
	}

	return results, nil
}

// GetAllTags returns all unique tags across all servers
func GetAllTags() ([]string, error) {
	allServers, err := loadServers()
	if err != nil {
		return nil, err
	}

	tagSet := make(map[string]bool)
	for _, srv := range allServers {
		for _, tag := range srv.Tags {
			tagSet[tag] = true
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	return tags, nil
}

// matchesQuery checks if a server matches the search query
func matchesQuery(srv Server, query string) bool {
	// Check name
	if strings.Contains(strings.ToLower(srv.Name), query) {
		return true
	}

	// Check description
	if strings.Contains(strings.ToLower(srv.Description), query) {
		return true
	}

	// Check author
	if strings.Contains(strings.ToLower(srv.Author), query) {
		return true
	}

	// Check tags
	for _, tag := range srv.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}

	// Check mcpId
	if strings.Contains(strings.ToLower(srv.McpId), query) {
		return true
	}

	return false
}
