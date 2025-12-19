// Package mcpstore provides access to MCP servers from taps.
package mcpstore

import (
	"sort"
	"strings"
)

// ServerConfig represents an MCP server configuration
type ServerConfig struct {
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport,omitempty"`
	URL       string            `json:"url,omitempty"`
}

// Server represents an MCP server from a tap
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

// TappedMCPInput represents MCP data from flowstore taps
type TappedMCPInput struct {
	ID             string
	Name           string
	Description    string
	Author         string
	GithubUrl      string
	GithubStars    int
	RequiresApiKey bool
	Command        string
	Args           []string
	Env            map[string]string
	Tags           []string
	Transport      string
	URL            string
	TapName        string
}

// ListServers returns all servers from tapped MCPs (sorted by stars)
func ListServers(tappedMCPs []TappedMCPInput) []Server {
	result := make([]Server, 0, len(tappedMCPs))

	for _, mcp := range tappedMCPs {
		// Use ID if provided, otherwise construct from tapName/name
		mcpId := mcp.ID
		if mcpId == "" {
			mcpId = mcp.TapName + "/" + mcp.Name
		}
		result = append(result, Server{
			McpId:          mcpId,
			Name:           mcp.Name,
			Description:    mcp.Description,
			Author:         mcp.Author,
			GithubUrl:      mcp.GithubUrl,
			GithubStars:    mcp.GithubStars,
			RequiresApiKey: mcp.RequiresApiKey,
			Tags:           mcp.Tags,
			Source:         mcp.TapName,
			Config: &ServerConfig{
				Command:   mcp.Command,
				Args:      mcp.Args,
				Env:       mcp.Env,
				Transport: mcp.Transport,
				URL:       mcp.URL,
			},
		})
	}

	// Sort by GitHub stars (descending) so most popular appear first
	sort.Slice(result, func(i, j int) bool {
		return result[i].GithubStars > result[j].GithubStars
	})

	return result
}

// SearchServers searches servers by query (matches name, description, author, or tags)
func SearchServers(servers []Server, query string) []Server {
	if query == "" {
		return servers
	}

	query = strings.ToLower(query)
	var results []Server

	for _, srv := range servers {
		if matchesQuery(srv, query) {
			results = append(results, srv)
		}
	}

	return results
}

// GetServer returns a server by its mcpId
func GetServer(servers []Server, mcpId string) *Server {
	for _, srv := range servers {
		if srv.McpId == mcpId {
			return &srv
		}
	}
	return nil
}

// GetServerByName returns a server by its name (case-insensitive)
func GetServerByName(servers []Server, name string) *Server {
	nameLower := strings.ToLower(name)
	for _, srv := range servers {
		if strings.ToLower(srv.Name) == nameLower {
			return &srv
		}
	}
	return nil
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
