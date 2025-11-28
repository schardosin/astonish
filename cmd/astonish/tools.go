package astonish

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// minimalReadonlyContext implements agent.ReadonlyContext for tool listing
type minimalReadonlyContext struct {
	context.Context
}

func (m *minimalReadonlyContext) AgentName() string                    { return "tools-list" }
func (m *minimalReadonlyContext) AppName() string                      { return "astonish" }
func (m *minimalReadonlyContext) UserContent() *genai.Content          { return nil }
func (m *minimalReadonlyContext) InvocationID() string                 { return "" }
func (m *minimalReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalReadonlyContext) UserID() string                       { return "" }
func (m *minimalReadonlyContext) SessionID() string                    { return "" }
func (m *minimalReadonlyContext) Branch() string                       { return "" }

func handleToolsCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: astonish tools <command> [args]")
		return fmt.Errorf("no tools subcommand provided")
	}

	switch args[0] {
	case "list":
		return handleToolsListCommand(args[1:])
	case "edit":
		return handleToolsEditCommand()
	default:
		return fmt.Errorf("unknown tools command: %s", args[0])
	}
}

func handleToolsListCommand(args []string) error {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	jsonOutput := listCmd.Bool("json", false, "Output in JSON format")

	if err := listCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Define tool info structure
	type ToolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}

	ctx := context.Background()

	// Get internal tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return fmt.Errorf("failed to get internal tools: %w", err)
	}

	// Get MCP servers and their tools
	mcpManager, err := mcp.NewManager()
	type MCPServerInfo struct {
		Name  string
		Tools []ToolInfo
	}
	var mcpServers []MCPServerInfo
	
	if err == nil {
		if err := mcpManager.InitializeToolsets(ctx); err == nil {
			// Get server names from config
			mcpConfig := mcpManager.GetConfig()
			toolsets := mcpManager.GetToolsets()
			
			// Create minimal context for fetching tools
			minimalCtx := &minimalReadonlyContext{Context: ctx}
			
			// Match servers with toolsets
			serverNames := make([]string, 0, len(mcpConfig.MCPServers))
			for name := range mcpConfig.MCPServers {
				serverNames = append(serverNames, name)
			}
			
			for i, toolset := range toolsets {
				serverName := "unknown"
				if i < len(serverNames) {
					serverName = serverNames[i]
				}
				
				serverInfo := MCPServerInfo{
					Name:  serverName,
					Tools: []ToolInfo{},
				}
				
				// Fetch tools from this toolset
				mcpTools, err := toolset.Tools(minimalCtx)
				if err == nil {
					for _, tool := range mcpTools {
						serverInfo.Tools = append(serverInfo.Tools, ToolInfo{
							Name:        tool.Name(),
							Description: tool.Description(),
							Source:      serverName,
						})
					}
				}
				
				mcpServers = append(mcpServers, serverInfo)
			}
		}
	}

	// Combine all tools
	allTools := make([]ToolInfo, 0)

	// Add internal tools
	for _, tool := range internalTools {
		allTools = append(allTools, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
			Source:      "Internal",
		})
	}
	
	// Add MCP tools
	for _, server := range mcpServers {
		allTools = append(allTools, server.Tools...)
	}

	if *jsonOutput {
		// JSON output
		data, err := json.MarshalIndent(allTools, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal tools to JSON: %w", err)
		}
		fmt.Println(string(data))
	} else {
		// Human-readable output
		const (
			ColorReset   = "\033[0m"
			ColorMagenta = "\033[35m"
			ColorCyan    = "\033[36m"
			ColorYellow  = "\033[33m"
		)

		fmt.Println("\nAvailable Tools:")
		fmt.Println("================")

		if len(internalTools) > 0 {
			fmt.Printf("\n%sInternal Tools:%s\n", ColorYellow, ColorReset)
			for _, tool := range internalTools {
				fmt.Printf("  %s%s%s: %s%s%s\n",
					ColorMagenta, tool.Name(), ColorReset,
					ColorCyan, tool.Description(), ColorReset)
			}
		}

		if len(mcpServers) > 0 {
			fmt.Printf("\n%sMCP Servers:%s\n", ColorYellow, ColorReset)
			for _, server := range mcpServers {
				fmt.Printf("  %s%s%s\n", ColorMagenta, server.Name, ColorReset)
				if len(server.Tools) > 0 {
					for _, tool := range server.Tools {
						fmt.Printf("    %s%s%s: %s%s%s\n",
							ColorMagenta, tool.Name, ColorReset,
							ColorCyan, tool.Description, ColorReset)
					}
				} else {
					fmt.Printf("    %s(no tools available)%s\n", ColorCyan, ColorReset)
				}
			}
		}

		// Count total MCP tools
		totalMCPTools := 0
		for _, server := range mcpServers {
			totalMCPTools += len(server.Tools)
		}

		fmt.Printf("\nTotal: %d internal tools, %d MCP servers with %d tools\n", 
			len(internalTools), len(mcpServers), totalMCPTools)
	}

	return nil
}

func handleToolsEditCommand() error {
	// Get MCP config path
	mcpConfigPath, err := config.GetMCPConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get MCP config path: %w", err)
	}

	// Check if file exists, create if not
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		// Create empty config
		emptyConfig := &config.MCPConfig{
			MCPServers: make(map[string]config.MCPServerConfig),
		}
		if err := config.SaveMCPConfig(emptyConfig); err != nil {
			return fmt.Errorf("failed to create MCP config file: %w", err)
		}
		fmt.Printf("Created new MCP config file at: %s\n", mcpConfigPath)
	}

	// Get editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Platform-specific defaults
		switch runtime.GOOS {
		case "windows":
			editor = "notepad.exe"
		case "darwin", "linux":
			editor = "vi"
		default:
			editor = "vi"
		}
	}

	// Open editor
	cmd := exec.Command(editor, mcpConfigPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open editor: %w", err)
	}

	fmt.Printf("MCP configuration saved to: %s\n", mcpConfigPath)
	return nil
}
