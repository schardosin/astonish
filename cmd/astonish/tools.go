package astonish

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
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
	if len(args) < 1 || args[0] == "-h" || args[0] == "--help" {
		printToolsUsage()
		return nil
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

func printToolsUsage() {
	fmt.Println("usage: astonish tools [-h] {list,edit} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {list,edit}")
	fmt.Println("                        Tools management commands")
	fmt.Println("    list                List available tools (internal + MCP)")
	fmt.Println("    edit                Edit MCP configuration")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
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
			sort.Strings(serverNames) // Sort server names
			
			// Map toolsets by name for easier lookup if possible, or just iterate
			// The mcpManager.GetToolsets() returns a slice, likely in order of initialization
			// But we want to group by server name. 
			// Let's iterate toolsets and try to match names or just list them.
			
			// Actually, let's just iterate the toolsets we have
			for _, toolset := range toolsets {
				serverName := toolset.Name()
				
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
				// Sort tools within server
				sort.Slice(serverInfo.Tools, func(i, j int) bool {
					return serverInfo.Tools[i].Name < serverInfo.Tools[j].Name
				})
				
				mcpServers = append(mcpServers, serverInfo)
			}
			// Sort servers by name
			sort.Slice(mcpServers, func(i, j int) bool {
				return mcpServers[i].Name < mcpServers[j].Name
			})
		}
	}

	// Combine all tools for JSON output or max length calculation
	allTools := make([]ToolInfo, 0)
	var maxLen int

	// Add internal tools
	for _, tool := range internalTools {
		t := ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
			Source:      "Internal",
		}
		allTools = append(allTools, t)
		if len(t.Name) > maxLen {
			maxLen = len(t.Name)
		}
	}
	
	// Add MCP tools
	for _, server := range mcpServers {
		allTools = append(allTools, server.Tools...)
		for _, tool := range server.Tools {
			if len(tool.Name) > maxLen {
				maxLen = len(tool.Name)
			}
		}
	}

	if *jsonOutput {
		// JSON output
		data, err := json.MarshalIndent(allTools, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal tools to JSON: %w", err)
		}
		fmt.Println(string(data))
	} else {
		// Styles
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")). // Purple
			Bold(true).
			PaddingBottom(1)

		sectionStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("220")). // Yellow
			Bold(true).
			PaddingTop(1)

		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")). // Pinkish
			Bold(true)

		descStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")) // White/Grey

		fmt.Println(headerStyle.Render("AVAILABLE TOOLS"))

		// Internal Tools
		if len(internalTools) > 0 {
			fmt.Println(sectionStyle.Render("Internal Tools"))
			
			// Sort internal tools
			sortedInternal := make([]tool.Tool, len(internalTools))
			copy(sortedInternal, internalTools)
			sort.Slice(sortedInternal, func(i, j int) bool {
				return sortedInternal[i].Name() < sortedInternal[j].Name()
			})

			for _, tool := range sortedInternal {
				paddedName := fmt.Sprintf("%-*s", maxLen + 4, tool.Name())
				row := lipgloss.JoinHorizontal(lipgloss.Left,
					nameStyle.Render(paddedName),
					descStyle.Render(tool.Description()),
				)
				fmt.Println(row)
			}
		}

		// MCP Servers
		if len(mcpServers) > 0 {
			for _, server := range mcpServers {
				fmt.Println(sectionStyle.Render(fmt.Sprintf("MCP Server: %s", server.Name)))
				
				if len(server.Tools) > 0 {
					for _, tool := range server.Tools {
						paddedName := fmt.Sprintf("%-*s", maxLen + 4, tool.Name)
						row := lipgloss.JoinHorizontal(lipgloss.Left,
							nameStyle.Render(paddedName),
							descStyle.Render(tool.Description),
						)
						fmt.Println(row)
					}
				} else {
					fmt.Println(descStyle.Render("  (no tools available)"))
				}
			}
		}

		// Summary footer
		totalMCPTools := 0
		for _, server := range mcpServers {
			totalMCPTools += len(server.Tools)
		}
		
		footerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")). // Dark Grey
			PaddingTop(1)
			
		fmt.Println(footerStyle.Render(fmt.Sprintf("Total: %d internal tools, %d MCP servers with %d tools", 
			len(internalTools), len(mcpServers), totalMCPTools)))
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

	fmt.Printf("Opening MCP configuration at: %s\n", mcpConfigPath)
	return openInEditor(mcpConfigPath)
}
