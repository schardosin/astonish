package astonish

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/mcpstore"
	"github.com/schardosin/astonish/pkg/memory"
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
	case "search":
		return handleToolsSearchCommand(args[1:])
	case "edit":
		return handleToolsEditCommand()
	case "store":
		return handleToolsStoreCommand(args[1:])
	case "servers":
		return handleToolsServersCommand(args[1:])
	case "enable":
		return handleToolsEnableCommand(args[1:])
	case "disable":
		return handleToolsDisableCommand(args[1:])
	case "refresh":
		return handleToolsRefreshCommand(args[1:])
	default:
		return fmt.Errorf("unknown tools command: %s", args[0])
	}
}

func printToolsUsage() {
	fmt.Println("usage: astonish tools [-h] {list,search,edit,store,servers,enable,disable,refresh} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {list,search,edit,store,servers,enable,disable,refresh}")
	fmt.Println("                        Tools management commands")
	fmt.Println("    list                List available tools (internal + MCP)")
	fmt.Println("    search <query>      Semantic search across the tool index (use '*' to list all)")
	fmt.Println("    edit                Edit MCP configuration")
	fmt.Println("    store               Browse and install MCP servers from the store")
	fmt.Println("    servers             List MCP servers with their enabled/disabled status")
	fmt.Println("    enable <name>       Enable an MCP server")
	fmt.Println("    disable <name>      Disable an MCP server")
	fmt.Println("    refresh             Refresh the tools cache (connects to all MCP servers)")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
}

// handleToolsServersCommand lists all MCP servers with their enabled/disabled status
func handleToolsServersCommand(args []string) error {
	serversCmd := flag.NewFlagSet("servers", flag.ExitOnError)
	jsonOutput := serversCmd.Bool("json", false, "Output in JSON format")

	if err := serversCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	mcpConfig, err := config.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	type ServerInfo struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		Command   string `json:"command,omitempty"`
		URL       string `json:"url,omitempty"`
		Enabled   bool   `json:"enabled"`
	}

	servers := make([]ServerInfo, 0, len(mcpConfig.MCPServers))
	for name, cfg := range mcpConfig.MCPServers {
		transport := cfg.Transport
		if transport == "" {
			transport = "stdio"
		}
		servers = append(servers, ServerInfo{
			Name:      name,
			Transport: transport,
			Command:   cfg.Command,
			URL:       cfg.URL,
			Enabled:   cfg.IsEnabled(),
		})
	}

	// Sort by name
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})

	if *jsonOutput {
		data, err := json.MarshalIndent(servers, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal servers to JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Styled output
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")).
		Bold(true).
		PaddingBottom(1)

	enabledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	disabledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	fmt.Println(headerStyle.Render("MCP SERVERS"))

	if len(servers) == 0 {
		fmt.Println("  No MCP servers configured.")
		fmt.Println("  Run 'astonish tools store' to browse and install servers.")
		return nil
	}

	// Find max name length for alignment
	maxLen := 0
	for _, srv := range servers {
		if len(srv.Name) > maxLen {
			maxLen = len(srv.Name)
		}
	}

	for _, srv := range servers {
		status := enabledStyle.Render("✓")
		statusText := enabledStyle.Render("(enabled)")
		style := nameStyle
		if !srv.Enabled {
			status = disabledStyle.Render("✗")
			statusText = disabledStyle.Render("(disabled)")
			style = disabledStyle
		}

		padding := strings.Repeat(" ", maxLen-len(srv.Name)+1)
		fmt.Printf("  %s %s%s%s  %s\n", status, style.Render(srv.Name), padding, srv.Transport, statusText)
	}

	return nil
}

// handleToolsEnableCommand enables an MCP server
func handleToolsEnableCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: astonish tools enable <server-name>")
	}

	serverName := args[0]

	if err := config.SetMCPServerEnabled(serverName, true); err != nil {
		// If server not found, show available servers
		if strings.Contains(err.Error(), "not found") {
			names, namesErr := config.GetMCPServerNames()
			if namesErr != nil {
				slog.Warn("failed to get MCP server names", "error", namesErr)
			}
			if len(names) > 0 {
				return fmt.Errorf("%w\nAvailable servers: %s", err, strings.Join(names, ", "))
			}
		}
		return err
	}

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Server '%s' enabled", serverName)))
	return nil
}

// handleToolsDisableCommand disables an MCP server
func handleToolsDisableCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: astonish tools disable <server-name>")
	}

	serverName := args[0]

	if err := config.SetMCPServerEnabled(serverName, false); err != nil {
		// If server not found, show available servers
		if strings.Contains(err.Error(), "not found") {
			names, namesErr := config.GetMCPServerNames()
			if namesErr != nil {
				slog.Warn("failed to get MCP server names", "error", namesErr)
			}
			if len(names) > 0 {
				return fmt.Errorf("%w\nAvailable servers: %s", err, strings.Join(names, ", "))
			}
		}
		return err
	}

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("220")).
		Bold(true)

	fmt.Println(successStyle.Render(fmt.Sprintf("✗ Server '%s' disabled", serverName)))
	return nil
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

func handleToolsStoreCommand(args []string) error {
	// If no args or "install", run interactive installer
	if len(args) == 0 || args[0] == "install" {
		return handleToolsStoreInstall()
	}

	switch args[0] {
	case "list":
		return handleToolsStoreList()
	default:
		fmt.Println("usage: astonish tools store [list|install]")
		fmt.Println("")
		fmt.Println("commands:")
		fmt.Println("  list      List all available MCP servers (official + tapped)")
		fmt.Println("  install   Interactive MCP server installer (default)")
		return nil
	}
}

func handleToolsStoreList() error {
	// Load servers from taps
	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to load flow store: %w", err)
	}
	_ = store.UpdateAllManifests()
	tappedMCPs := store.ListAllMCPs()

	// Convert to mcpstore inputs
	var inputs []mcpstore.TappedMCPInput
	for _, mcp := range tappedMCPs {
		// Skip MCPs that have neither command nor URL (not installable)
		if mcp.Command == "" && mcp.URL == "" {
			continue
		}
		inputs = append(inputs, mcpstore.TappedMCPInput{
			ID:             mcp.ID,
			Name:           mcp.Name,
			Description:    mcp.Description,
			Author:         mcp.Author,
			GithubUrl:      mcp.GithubUrl,
			GithubStars:    mcp.GithubStars,
			RequiresApiKey: mcp.RequiresApiKey,
			Command:        mcp.Command,
			Args:           mcp.Args,
			Env:            mcp.Env,
			Tags:           mcp.Tags,
			Transport:      mcp.Transport,
			URL:            mcp.URL,
			TapName:        mcp.TapName,
		})
	}

	servers := mcpstore.ListServers(inputs)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	starsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Group servers by source
	sourceGroups := make(map[string][]mcpstore.Server)
	for _, srv := range servers {
		// Skip servers without config or without command/URL (not installable)
		if srv.Config == nil || (srv.Config.Command == "" && srv.Config.URL == "") {
			continue
		}
		sourceGroups[srv.Source] = append(sourceGroups[srv.Source], srv)
	}

	for source, srvs := range sourceGroups {
		fmt.Println(headerStyle.Render(fmt.Sprintf("\n%s", strings.ToUpper(source))))
		fmt.Println(strings.Repeat("─", 60))

		for _, srv := range srvs {
			starsStr := ""
			if srv.GithubStars > 0 {
				if srv.GithubStars >= 1000 {
					starsStr = fmt.Sprintf(" ★%.1fk", float64(srv.GithubStars)/1000)
				} else {
					starsStr = fmt.Sprintf(" ★%d", srv.GithubStars)
				}
			}

			desc := srv.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}

			fmt.Printf("  %s%s %s\n", nameStyle.Render(srv.Name), starsStyle.Render(starsStr), sourceStyle.Render("["+srv.Source+"]"))
			fmt.Printf("    %s\n", descStyle.Render(desc))
		}
	}

	fmt.Println("")
	fmt.Println("Tip: Run 'astonish tools store install' to install a server")
	fmt.Println("     Run 'astonish tap add <repo>' to add more repositories")

	return nil
}

func handleToolsStoreInstall() error {
	// Load servers from taps
	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to load flow store: %w", err)
	}
	_ = store.UpdateAllManifests()
	tappedMCPs := store.ListAllMCPs()

	// Convert to mcpstore inputs
	var inputs []mcpstore.TappedMCPInput
	for _, mcp := range tappedMCPs {
		// Skip MCPs that have neither command nor URL (not installable)
		if mcp.Command == "" && mcp.URL == "" {
			continue
		}
		inputs = append(inputs, mcpstore.TappedMCPInput{
			ID:             mcp.ID,
			Name:           mcp.Name,
			Description:    mcp.Description,
			Author:         mcp.Author,
			GithubUrl:      mcp.GithubUrl,
			GithubStars:    mcp.GithubStars,
			RequiresApiKey: mcp.RequiresApiKey,
			Command:        mcp.Command,
			Args:           mcp.Args,
			Env:            mcp.Env,
			Tags:           mcp.Tags,
			Transport:      mcp.Transport,
			URL:            mcp.URL,
			TapName:        mcp.TapName,
		})
	}

	servers := mcpstore.ListServers(inputs)

	if len(servers) == 0 {
		fmt.Println("No MCP servers available in store.")
		return nil
	}

	// Create options for selection
	options := make([]huh.Option[string], 0, len(servers))
	for _, srv := range servers {
		// Skip servers without config or without command/URL (not installable)
		if srv.Config == nil || (srv.Config.Command == "" && srv.Config.URL == "") {
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

		// Add source indicator for tapped MCPs
		sourceIndicator := ""
		if srv.Source != "" && srv.Source != "official" {
			sourceIndicator = fmt.Sprintf(" [%s]", srv.Source)
		}

		label := fmt.Sprintf("%s%s (%s) - %s", srv.Name, sourceIndicator, starsStr, desc)
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

	// Find the selected server
	var selectedServer *mcpstore.Server
	for i := range servers {
		if servers[i].McpId == selectedMcpId {
			selectedServer = &servers[i]
			break
		}
	}
	if selectedServer == nil {
		return fmt.Errorf("failed to find selected server")
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

// handleToolsRefreshCommand connects to all MCP servers and refreshes the tools cache.
// This populates cached tool schemas needed for lazy loading in chat mode.
func handleToolsRefreshCommand(args []string) error {
	refreshCmd := flag.NewFlagSet("refresh", flag.ExitOnError)
	verbose := refreshCmd.Bool("v", false, "Verbose output")
	refreshCmd.Parse(args)

	ctx := context.Background()

	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	if mcpCfg == nil || len(mcpCfg.MCPServers) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}

	fmt.Println("Refreshing tools cache...")

	mcpManager, err := mcp.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create MCP manager: %w", err)
	}
	defer mcpManager.Cleanup()

	totalTools := 0
	successCount := 0
	failCount := 0

	for name, serverCfg := range mcpCfg.MCPServers {
		if !serverCfg.IsEnabled() {
			if *verbose {
				fmt.Printf("  [skip] %s (disabled)\n", name)
			}
			continue
		}

		fmt.Printf("  [....] %s", name)

		namedToolset, initErr := mcpManager.InitializeSingleToolset(ctx, name)
		if initErr != nil {
			fmt.Printf("\r  [FAIL] %s: %v\n", name, initErr)
			failCount++
			continue
		}

		minCtx := &minimalReadonlyContext{Context: ctx}
		mcpTools, toolErr := namedToolset.Toolset.Tools(minCtx)
		if toolErr != nil {
			fmt.Printf("\r  [FAIL] %s: %v\n", name, toolErr)
			failCount++
			continue
		}

		// Build cache entries with schemas
		entries := make([]cache.ToolEntry, 0, len(mcpTools))
		for _, t := range mcpTools {
			entries = append(entries, cache.ToolEntry{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      name,
				InputSchema: common.ExtractToolInputSchema(t),
			})
		}

		checksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cache.AddServerTools(name, entries, checksum)

		fmt.Printf("\r  [ OK ] %s (%d tools)\n", name, len(mcpTools))
		totalTools += len(mcpTools)
		successCount++
	}

	if err := cache.SaveCache(); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}

	fmt.Printf("\nDone: %d servers refreshed, %d tools cached", successCount, totalTools)
	if failCount > 0 {
		fmt.Printf(", %d failed", failCount)
	}
	fmt.Println()

	return nil
}

// handleToolsSearchCommand performs a semantic search across the tool index.
// This is a diagnostic command that mirrors how the chat agent discovers tools,
// allowing users to verify tool index behavior, check similarity scores, and
// diagnose tool discovery issues.
func handleToolsSearchCommand(args []string) error {
	searchCmd := flag.NewFlagSet("search", flag.ExitOnError)
	maxResults := searchCmd.Int("max-results", 10, "Maximum number of results")
	minScore := searchCmd.Float64("min-score", 0, "Minimum similarity score 0.0-1.0 (default: 0 = show all)")
	verbose := searchCmd.Bool("verbose", false, "Show debug output")
	searchCmd.Parse(args)

	remaining := searchCmd.Args()
	if len(remaining) == 0 {
		fmt.Println("usage: astonish tools search [--max-results N] [--min-score F] [--verbose] <query>")
		fmt.Println("")
		fmt.Println("Semantic search across the tool index. Shows which tools match a query")
		fmt.Println("and their similarity scores — the same search the chat agent performs.")
		fmt.Println("")
		fmt.Println("Use '*' or 'all' to list every indexed tool.")
		fmt.Println("")
		fmt.Println("examples:")
		fmt.Println("  astonish tools search 'activate container'")
		fmt.Println("  astonish tools search --min-score 0.1 'take a screenshot'")
		fmt.Println("  astonish tools search '*'")
		return fmt.Errorf("no search query provided")
	}

	query := strings.Join(remaining, " ")

	// --- Initialize embedding function ---
	fmt.Printf("Initializing embedding model...\n")
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	memCfg := &appCfg.Memory
	if !memCfg.IsMemoryEnabled() {
		return fmt.Errorf("memory system is disabled — tool index requires memory.enabled: true in config.yaml")
	}

	vecDir, err := config.GetVectorDir(memCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve vector directory: %w", err)
	}

	var embGetSecret config.SecretGetter
	if cfgDir, err := config.GetConfigDir(); err == nil {
		if store, err := credentials.Open(cfgDir); err == nil {
			embGetSecret = store.GetSecret
		}
	}
	embResult, err := memory.ResolveEmbeddingFunc(appCfg, memCfg, *verbose, embGetSecret)
	if err != nil {
		return fmt.Errorf("failed to initialize embedder: %w", err)
	}
	if embResult.Cleanup != nil {
		defer embResult.Cleanup()
	}

	// --- Open the persistent DB (same as memory store uses) ---
	storeCfg := memory.DefaultStoreConfig()
	storeCfg.VectorDir = vecDir
	store, err := memory.NewStore(storeCfg, embResult.EmbeddingFunc)
	if err != nil {
		return fmt.Errorf("failed to open vector store: %w", err)
	}

	// --- Create tool index on the shared DB ---
	toolIndex, err := agent.NewToolIndex(store.DB(), embResult.EmbeddingFunc)
	if err != nil {
		return fmt.Errorf("failed to create tool index: %w", err)
	}

	// --- Gather tools ---
	ctx := context.Background()
	minCtx := &minimalReadonlyContext{Context: ctx}

	// Internal tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return fmt.Errorf("failed to get internal tools: %w", err)
	}

	// Build tool groups: main thread + groups for MCP servers
	var mainThreadTools []tool.Tool
	var toolGroups []*agent.ToolGroup

	// All internal tools go into a single "internal" group for indexing
	for _, t := range internalTools {
		mainThreadTools = append(mainThreadTools, t)
	}

	// MCP tools
	mcpManager, mcpErr := mcp.NewManager()
	if mcpErr == nil {
		if initErr := mcpManager.InitializeToolsets(ctx); initErr == nil {
			namedToolsets := mcpManager.GetNamedToolsets()
			for _, nt := range namedToolsets {
				mcpTools, err := nt.Toolset.Tools(minCtx)
				if err != nil {
					if *verbose {
						fmt.Printf("  Warning: Failed to get tools from MCP server %s: %v\n", nt.Name, err)
					}
					continue
				}
				groupName := "mcp:" + nt.Name
				group := &agent.ToolGroup{
					Name:        groupName,
					Description: fmt.Sprintf("MCP server: %s", nt.Name),
					Tools:       mcpTools,
				}
				toolGroups = append(toolGroups, group)
			}
		}
		defer mcpManager.Cleanup()
	}

	// --- Sync tools into the index ---
	fmt.Printf("Syncing tools to index...\n")
	start := time.Now()
	if err := toolIndex.SyncTools(ctx, mainThreadTools, toolGroups); err != nil {
		return fmt.Errorf("failed to sync tools: %w", err)
	}
	syncTime := time.Since(start)
	fmt.Printf("Indexed %d tools in %s.\n\n", toolIndex.Count(), syncTime.Round(time.Millisecond))

	if toolIndex.Count() == 0 {
		fmt.Println("Tool index is empty — no tools were indexed.")
		return nil
	}

	// --- Handle list-all mode ---
	isListAll := query == "*" || query == "all" || query == "list all"
	if isListAll {
		groups := toolIndex.ListAll()
		groupNames := make([]string, 0, len(groups))
		for g := range groups {
			groupNames = append(groupNames, g)
		}
		sort.Strings(groupNames)

		total := 0
		for _, gName := range groupNames {
			tools := groups[gName]
			total += len(tools)
			var label string
			if gName == "_main" {
				label = "main thread (call directly)"
			} else {
				label = fmt.Sprintf("%s (call directly)", gName)
			}
			fmt.Printf("  %s (%d tools)\n", label, len(tools))
			for _, m := range tools {
				desc := truncateToolDesc(m.Description, 80)
				fmt.Printf("    %-35s %s\n", m.ToolName, desc)
			}
			fmt.Println()
		}
		fmt.Printf("Total: %d tools across %d groups\n", total, len(groups))
		return nil
	}

	// --- Semantic search ---
	fmt.Printf("Searching for: %q\n\n", query)

	// Use minScore 0 to show ALL results with their scores (diagnostic mode)
	searchMinScore := *minScore
	matches, err := toolIndex.SearchHybrid(ctx, query, *maxResults, searchMinScore)
	if err != nil {
		fmt.Printf("ERROR: search failed: %v\n", err)
		return fmt.Errorf("search failed: %w", err)
	}
	if *verbose {
		fmt.Printf("[debug] Hybrid search returned %d matches (minScore=%.4f)\n", len(matches), searchMinScore)
	}

	if len(matches) == 0 {
		fmt.Printf("No matches found for: %q\n", query)
		if searchMinScore > 0 {
			fmt.Printf("Tip: Try --min-score 0 to see all results regardless of score.\n")
		} else {
			fmt.Println("This means the tool index is empty or the query produced no cosine similarity above 0.")
			fmt.Println("Check that the embedding model is working correctly.")
		}
		return nil
	}

	fmt.Printf("Found %d result(s):\n\n", len(matches))

	for i, m := range matches {
		access := fmt.Sprintf("call directly (%s group)", m.GroupName)
		if m.IsMainTool {
			access = "call directly (main thread)"
		}
		desc := truncateToolDesc(m.Description, 100)
		fmt.Printf("  %d. [%.4f] %s (%s)\n", i+1, m.Score, m.ToolName, m.GroupName)
		fmt.Printf("     %s\n", desc)
		fmt.Printf("     Access: %s\n", access)
		if i < len(matches)-1 {
			fmt.Println()
		}
	}

	// Show threshold summary
	fmt.Println()
	if len(matches) > 0 {
		topScore := matches[0].Score
		botScore := matches[len(matches)-1].Score
		fmt.Printf("Score range: %.4f - %.4f (RRF scores, max possible ~0.0328)\n", botScore, topScore)
		fmt.Printf("Hybrid search threshold (prompt injection, search_tools, sub-agent): 0.005\n")
		aboveThreshold := 0
		for _, m := range matches {
			if m.Score >= 0.005 {
				aboveThreshold++
			}
		}
		fmt.Printf("Results above threshold: %d\n", aboveThreshold)
	}

	return nil
}

// truncateToolDesc shortens a tool description for display.
func truncateToolDesc(s string, maxLen int) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
