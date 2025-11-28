package mcp

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/schardosin/astonish/pkg/config"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// Manager handles MCP server lifecycle and toolset creation
type Manager struct {
	config   *config.MCPConfig
	toolsets []tool.Toolset
}

// NewManager creates a new MCP manager
func NewManager() (*Manager, error) {
	cfg, err := config.LoadMCPConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}

	return &Manager{
		config:   cfg,
		toolsets: make([]tool.Toolset, 0),
	}, nil
}

// InitializeToolsets creates ADK mcptoolset instances for all configured servers
func (m *Manager) InitializeToolsets(ctx context.Context) error {
	if len(m.config.MCPServers) == 0 {
		log.Println("No MCP servers configured")
		return nil
	}

	for serverName, serverConfig := range m.config.MCPServers {
		transport, err := createTransport(serverConfig)
		if err != nil {
			log.Printf("Warning: Failed to create transport for MCP server '%s': %v", serverName, err)
			continue
		}

		// Create ADK mcptoolset - it handles everything automatically
		toolset, err := mcptoolset.New(mcptoolset.Config{
			Transport: transport,
			// ToolFilter can be added here if needed to filter specific tools
		})
		if err != nil {
			log.Printf("Warning: Failed to create toolset for MCP server '%s': %v", serverName, err)
			continue
		}

		m.toolsets = append(m.toolsets, toolset)
		log.Printf("Successfully initialized MCP server: %s", serverName)
	}

	return nil
}

// GetToolsets returns all initialized MCP toolsets
func (m *Manager) GetToolsets() []tool.Toolset {
	return m.toolsets
}

// createTransport creates the appropriate MCP transport based on configuration
func createTransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	// Default to stdio if not specified
	transportType := cfg.Transport
	if transportType == "" {
		transportType = "stdio"
	}

	switch transportType {
	case "stdio":
		return createStdioTransport(cfg)
	case "sse":
		return createSSETransport(cfg)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", transportType)
	}
}

// createStdioTransport creates a CommandTransport for stdio-based MCP servers
func createStdioTransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("command is required for stdio transport")
	}

	// Create the command
	cmd := exec.Command(cfg.Command, cfg.Args...)

	// Set environment variables
	if len(cfg.Env) > 0 {
		// Start with current environment
		cmd.Env = append(cmd.Env, cmd.Environ()...)
		
		// Add custom environment variables
		for key, value := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Create CommandTransport - ADK will manage the subprocess lifecycle
	return &mcp.CommandTransport{
		Command: cmd,
	}, nil
}

// createSSETransport creates an SSE transport for remote MCP servers
func createSSETransport(cfg config.MCPServerConfig) (mcp.Transport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("URL is required for SSE transport")
	}

	// Create SSE client transport
	return &mcp.SSEClientTransport{
		Endpoint: cfg.URL,
		// HTTPClient can be customized here if needed (e.g., for auth)
	}, nil
}

// GetConfig returns the MCP configuration
func (m *Manager) GetConfig() *config.MCPConfig {
	return m.config
}
