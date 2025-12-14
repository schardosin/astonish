package astonish

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/mcpstore"
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
	case "store":
		return handleToolsStoreCommand()
	default:
		return fmt.Errorf("unknown tools command: %s", args[0])
	}
}

func printToolsUsage() {
	fmt.Println("usage: astonish tools [-h] {list,edit,store} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {list,edit,store}")
	fmt.Println("                        Tools management commands")
	fmt.Println("    list                List available tools (internal + MCP)")
	fmt.Println("    edit                Edit MCP configuration")
	fmt.Println("    store               Browse and install MCP servers from the store")
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
			// Get named toolsets with their server names
			namedToolsets := mcpManager.GetNamedToolsets()

			// Create minimal context for fetching tools
			minimalCtx := &minimalReadonlyContext{Context: ctx}

			// Iterate toolsets with their proper names
			for _, namedToolset := range namedToolsets {
				serverInfo := MCPServerInfo{
					Name:  namedToolset.Name,
					Tools: []ToolInfo{},
				}

				// Fetch tools from this toolset
				mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
				if err == nil {
					for _, tool := range mcpTools {
						serverInfo.Tools = append(serverInfo.Tools, ToolInfo{
							Name:        tool.Name(),
							Description: tool.Description(),
							Source:      namedToolset.Name,
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
				paddedName := fmt.Sprintf("%-*s", maxLen+4, tool.Name())
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
						paddedName := fmt.Sprintf("%-*s", maxLen+4, tool.Name)
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

func handleToolsStoreCommand() error {
	// Load servers from store (already sorted by stars)
	servers, err := mcpstore.ListServers()
	if err != nil {
		return fmt.Errorf("failed to load MCP store: %w", err)
	}

	if len(servers) == 0 {
		fmt.Println("No MCP servers available in store.")
		return nil
	}

	// Create options for selection
	options := make([]huh.Option[string], 0, len(servers))
	for _, srv := range servers {
		// Skip servers without config (can't be installed)
		if srv.Config == nil || srv.Config.Command == "" {
			continue
		}

		// Format: Name (★ stars) - description (truncated)
		desc := srv.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}

		starsStr := ""
		if srv.GithubStars > 0 {
			if srv.GithubStars >= 1000 {
				starsStr = fmt.Sprintf("★%.1fk", float64(srv.GithubStars)/1000)
			} else {
				starsStr = fmt.Sprintf("★%d", srv.GithubStars)
			}
		}

		label := fmt.Sprintf("%s (%s) - %s", srv.Name, starsStr, desc)
		options = append(options, huh.NewOption(label, srv.McpId))
	}

	if len(options) == 0 {
		fmt.Println("No installable MCP servers available.")
		return nil
	}

	// Server selection
	var selectedMcpId string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an MCP server to install").
				Description("Type to filter the list").
				Options(options...).
				Value(&selectedMcpId),
		),
	).Run()

	if err != nil {
		return fmt.Errorf("selection cancelled: %w", err)
	}

	// Get the selected server
	selectedServer, err := mcpstore.GetServer(selectedMcpId)
	if err != nil || selectedServer == nil {
		return fmt.Errorf("failed to get server details: %w", err)
	}

	// Verify server has config
	if selectedServer.Config == nil {
		return fmt.Errorf("server '%s' has no configuration available", selectedServer.Name)
	}

	// Prepare env variables
	envVars := make(map[string]string)
	if selectedServer.Config != nil && len(selectedServer.Config.Env) > 0 {
		// Collect env var keys and create string pointers for each
		type envVarPair struct {
			Key   string
			Value string
		}
		var envPairs []envVarPair
		for key, defaultVal := range selectedServer.Config.Env {
			envPairs = append(envPairs, envVarPair{Key: key, Value: defaultVal})
		}

		// Create input form for each env variable
		var fields []huh.Field
		for i := range envPairs {
			fields = append(fields, huh.NewInput().
				Title(envPairs[i].Key).
				Description(fmt.Sprintf("Environment variable for %s", selectedServer.Name)).
				Value(&envPairs[i].Value))
		}

		if len(fields) > 0 {
			err = huh.NewForm(
				huh.NewGroup(fields...).
					Title(fmt.Sprintf("Configure %s", selectedServer.Name)),
			).Run()

			if err != nil {
				return fmt.Errorf("configuration cancelled: %w", err)
			}

			// Copy values back to map
			for _, pair := range envPairs {
				envVars[pair.Key] = pair.Value
			}
		}
	}

	// Load existing MCP config
	mcpConfig, err := config.LoadMCPConfig()
	if err != nil {
		// Create new config if it doesn't exist
		mcpConfig = &config.MCPConfig{
			MCPServers: make(map[string]config.MCPServerConfig),
		}
	}

	// Build server config
	serverConfig := config.MCPServerConfig{
		Command: selectedServer.Config.Command,
		Args:    selectedServer.Config.Args,
	}
	if len(envVars) > 0 {
		serverConfig.Env = envVars
	}

	// Use server name as key (sanitized)
	serverKey := selectedServer.Name
	mcpConfig.MCPServers[serverKey] = serverConfig

	// Save config
	if err := config.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	// Success message
	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")). // Green
		Bold(true)

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ %s installed successfully!", selectedServer.Name)))
	fmt.Printf("  Run 'astonish tools list' to see available tools.\n")

	return nil
}

