package astonish

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/ui"
	"google.golang.org/adk/session"
)

func handleFlowsCommand(args []string) error {
	if len(args) < 1 || args[0] == "--help" || args[0] == "-h" {
		printFlowsUsage()
		return nil
	}

	switch args[0] {
	case "run":
		return handleRunCommand(args[1:])
	case "list":
		return handleListCommand()
	case "show":
		return handleShowCommand(args[1:])
	case "edit":
		return handleEditCommand(args[1:])
	case "store":
		return handleStoreCommand(args[1:])
	default:
		return fmt.Errorf("unknown flows command: %s", args[0])
	}
}

func printFlowsUsage() {
	fmt.Println("usage: astonish flows [-h] {run,list,show,edit,store} ...")
	fmt.Println("")
	fmt.Println("Design and run AI flows - powerful automation workflows")
	fmt.Println("powered by LLMs with visual design and CLI execution.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  run                 Execute a flow")
	fmt.Println("  list                List available flows")
	fmt.Println("  show                Visualize flow structure")
	fmt.Println("  edit                Edit a flow YAML file")
	fmt.Println("  store               Browse and install flows from stores")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help          Show this help message")
}

func handleRunCommand(args []string) error {
	// Load config first
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		// Just warn, don't fail, maybe first run
		fmt.Printf("Warning: Failed to load config: %v\n", err)
		appCfg = &config.AppConfig{}
	}

	runCmd := flag.NewFlagSet("run", flag.ExitOnError)
	providerName := runCmd.String("provider", appCfg.General.DefaultProvider, "LLM provider (gemini, openai, sap_ai_core)")
	modelName := runCmd.String("model", appCfg.General.DefaultModel, "Model name")
	useBrowser := runCmd.Bool("browser", false, "Launch with embedded web browser UI")
	port := runCmd.Int("port", 8080, "Port for web server (only used with --browser)")
	debugMode := runCmd.Bool("debug", false, "Enable debug mode to show tool inputs and responses")

	var params stringArray
	runCmd.Var(&params, "p", "Parameter to pass to the agent in key=value format (can be used multiple times)")

	// Pre-process args to allow positional agent name to be anywhere
	// We extract the first non-flag argument as the agent name
	var agentName string
	var flagArgs []string

	skipNext := false
	for _, arg := range args {
		if skipNext {
			flagArgs = append(flagArgs, arg)
			skipNext = false
			continue
		}

		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			// Check if it's a flag that takes an argument and doesn't use =
			if !strings.Contains(arg, "=") {
				name := strings.TrimLeft(arg, "-")
				if name == "provider" || name == "model" || name == "port" || name == "p" || name == "param" {
					skipNext = true
				}
			}
		} else {
			if agentName == "" {
				agentName = arg
			} else {
				// Extra positional args, keep them (flag.Parse will likely stop or error)
				flagArgs = append(flagArgs, arg)
			}
		}
	}

	if err := runCmd.Parse(flagArgs); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Parse parameters
	parameters := make(map[string]string)
	for _, p := range params {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) == 2 {
			parameters[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		} else {
			fmt.Printf("Warning: Ignoring malformed parameter: %s (missing '=')\n", p)
		}
	}

	// If provider is still empty, default to gemini
	if *providerName == "" {
		*providerName = "gemini"
	}

	// Set environment variables from config for the selected provider
	if providerCfg, ok := appCfg.Providers[*providerName]; ok {
		for k, v := range providerCfg {
			envKey := ""
			switch *providerName {
			case "gemini":
				if k == "api_key" {
					envKey = "GOOGLE_API_KEY"
				}
			case "openai":
				if k == "api_key" {
					envKey = "OPENAI_API_KEY"
				}
			case "sap_ai_core":
				switch k {
				case "client_id":
					envKey = "AICORE_CLIENT_ID"
				case "client_secret":
					envKey = "AICORE_CLIENT_SECRET"
				case "auth_url":
					envKey = "AICORE_AUTH_URL"
				case "base_url":
					envKey = "AICORE_BASE_URL"
				case "resource_group":
					envKey = "AICORE_RESOURCE_GROUP"
				}
			}
			if envKey != "" && v != "" {
				os.Setenv(envKey, v)
			}
		}
	}

	// Load MCP config and set environment variables from all servers
	// This allows internal tools to access configuration defined for MCP servers (e.g. GITHUB_HOST)
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		for _, server := range mcpCfg.MCPServers {
			for k, v := range server.Env {
				if v != "" {
					os.Setenv(k, v)
				}
			}
		}
	}

	if agentName == "" {
		// Fallback to NArg check if somehow it ended up there
		if runCmd.NArg() > 0 {
			agentName = runCmd.Arg(0)
		} else {
			fmt.Println("Usage: astonish flows run [flags] <flow_name>")
			runCmd.PrintDefaults()
			return fmt.Errorf("no agent name provided")
		}
	}

	// Try to find the agent file
	// 1. Check if it's a full path or in current dir
	agentPath := agentName
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		// 2. Check with .yaml extension
		agentPath = fmt.Sprintf("%s.yaml", agentName)
		if _, err := os.Stat(agentPath); os.IsNotExist(err) {
			// 3. Check in standard system agents directory (legacy)
			agentsDir, err := config.GetAgentsDir()
			if err == nil {
				sysAgentPath := filepath.Join(agentsDir, fmt.Sprintf("%s.yaml", agentName))
				if _, err := os.Stat(sysAgentPath); err == nil {
					agentPath = sysAgentPath
					goto Found
				}
			}

			// 4. Check in new flows directory
			flowsDir, err := flowstore.GetFlowsDir()
			if err == nil {
				flowPath := filepath.Join(flowsDir, fmt.Sprintf("%s.yaml", agentName))
				if _, err := os.Stat(flowPath); err == nil {
					agentPath = flowPath
					goto Found
				}
			}

			// 5. Check in store cache (for installed flows)
			store, err := flowstore.NewStore()
			if err == nil {
				// Parse the flow reference first
				tapName, flowName := parseFlowRef(agentName)
				
				// Check installed flows for this specific tap
				if path, ok := store.GetInstalledFlowPath(tapName, flowName); ok {
					agentPath = path
					goto Found
				}

				// Try to fetch from store
				// - Bare names (no /) only check official store
				// - Prefixed names (tap/flow) check specific tap
				fmt.Printf("Flow not found locally, checking %s store...\n", tapName)
				if err := store.InstallFlow(tapName, flowName); err == nil {
					if path, ok := store.GetInstalledFlowPath(tapName, flowName); ok {
						fmt.Printf("✓ Downloaded from %s store\n", tapName)
						agentPath = path
						goto Found
					}
				}
			}

			// 7. Check in local dev path (fallback)
			agentPath = fmt.Sprintf("agents/%s.yaml", agentName)
			if _, err := os.Stat(agentPath); os.IsNotExist(err) {
				return fmt.Errorf("flow not found: %s\nTip: Run 'astonish flows store list' to see available flows", agentName)
			}
		}
	}

Found:

	cfg, err := config.LoadAgent(agentPath)
	if err != nil {
		return fmt.Errorf("failed to load agent: %w", err)
	}

	ctx := context.Background()

	// Create the base session service and wrap it to fix state initialization bug
	baseService := session.InMemoryService()
	safeService := NewAutoInitService(baseService)

	// Choose launcher based on --browser flag
	if *useBrowser {
		// Use simple web launcher with chat-only UI
		return launcher.RunSimpleWeb(ctx, &launcher.SimpleWebConfig{
			AgentConfig:    cfg,
			ProviderName:   *providerName,
			ModelName:      *modelName,
			SessionService: safeService,
			Port:           *port,
		})
	}

	// Use our custom console launcher
	return launcher.RunConsole(ctx, &launcher.ConsoleConfig{
		AgentConfig:    cfg,
		AppConfig:      appCfg,
		ProviderName:   *providerName,
		ModelName:      *modelName,
		SessionService: safeService,
		DebugMode:      *debugMode,
		Parameters:     parameters,
	})
}

// stringArray implements flag.Value interface for multiple string flags
type stringArray []string

func (i *stringArray) String() string {
	return strings.Join(*i, ", ")
}

func (i *stringArray) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func handleListCommand() error {
	type AgentInfo struct {
		Name        string
		Description string
		Source      string // "local", "official", or tap name
	}
	
	var localAgents []AgentInfo
	var officialAgents []AgentInfo
	tapAgents := make(map[string][]AgentInfo) // tapName -> agents

	// Helper to scan directory for local agents
	scanDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		seen := make(map[string]bool)
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
				name := entry.Name()[:len(entry.Name())-5]
				if seen[name] {
					continue
				}
				seen[name] = true
				path := filepath.Join(dir, entry.Name())
				cfg, err := config.LoadAgent(path)
				if err != nil {
					continue
				}
				localAgents = append(localAgents, AgentInfo{
					Name:        name,
					Description: cfg.Description,
					Source:      "local",
				})
			}
		}
	}

	// 1. Scan System Directory
	if sysDir, err := config.GetAgentsDir(); err == nil {
		scanDir(sysDir)
	}

	// 2. Scan Local Directory
	scanDir("agents")

	// 3. Scan User Flows Directory
	if flowsDir, err := flowstore.GetFlowsDir(); err == nil {
		scanDir(flowsDir)
	}

	// 4. Scan Store (installed flows)
	if store, err := flowstore.NewStore(); err == nil {
		for _, tap := range store.GetAllTaps() {
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
					continue
				}
				name := entry.Name()[:len(entry.Name())-5]
				path := filepath.Join(tapDir, entry.Name())
				cfg, err := config.LoadAgent(path)
				desc := "(Installed from store)"
				if err == nil && cfg.Description != "" {
					desc = cfg.Description
				}
				
				info := AgentInfo{
					Name:        name,
					Description: desc,
					Source:      tap.Name,
				}
				
				if tap.Name == flowstore.OfficialStoreName {
					officialAgents = append(officialAgents, info)
				} else {
					tapAgents[tap.Name] = append(tapAgents[tap.Name], info)
				}
			}
		}
	}

	// Styles
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")).
		Bold(true)

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	storeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true)

	tapStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	// Print sections
	printSection := func(title string, agents []AgentInfo, style lipgloss.Style) {
		if len(agents) == 0 {
			return
		}
		
		// Sort by name
		sort.Slice(agents, func(i, j int) bool {
			return agents[i].Name < agents[j].Name
		})
		
		fmt.Println(style.Render(fmt.Sprintf("\n%s (%d)", title, len(agents))))
		
		for _, a := range agents {
			fmt.Printf("  %s\n", nameStyle.Render(a.Name))
			if a.Description != "" {
				fmt.Printf("    %s\n", descStyle.Render(a.Description))
			}
		}
	}

	// Check if anything exists
	totalCount := len(localAgents) + len(officialAgents)
	for _, t := range tapAgents {
		totalCount += len(t)
	}
	
	if totalCount == 0 {
		fmt.Println("No flows found.")
		fmt.Println("\nTip: Run 'astonish flows store list' to browse available flows")
		return nil
	}

	fmt.Println(lipgloss.NewStyle().Bold(true).Render("AVAILABLE FLOWS"))

	// Print Local first
	printSection("📁 LOCAL", localAgents, sectionStyle)
	
	// Print Official Store
	printSection("🏪 OFFICIAL STORE", officialAgents, storeStyle)
	
	// Print Custom Taps
	var tapNames []string
	for tapName := range tapAgents {
		tapNames = append(tapNames, tapName)
	}
	sort.Strings(tapNames)
	for _, tapName := range tapNames {
		printSection("📦 "+tapName, tapAgents[tapName], tapStyle)
	}

	fmt.Println()
	return nil
}

func handleShowCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: astonish flows show <flow_name>")
		return fmt.Errorf("no flow name provided")
	}

	agentName := args[0]
	// Reuse the logic to find the agent file (duplicated from handleRunCommand for now, could be refactored)
	// 1. Check if it's a full path or in current dir
	agentPath := agentName
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		// 2. Check with .yaml extension
		agentPath = fmt.Sprintf("%s.yaml", agentName)
		if _, err := os.Stat(agentPath); os.IsNotExist(err) {
			// 3. Check in standard system agents directory
			agentsDir, err := config.GetAgentsDir()
			if err == nil {
				sysAgentPath := filepath.Join(agentsDir, fmt.Sprintf("%s.yaml", agentName))
				if _, err := os.Stat(sysAgentPath); err == nil {
					agentPath = sysAgentPath
					goto Found
				}
			}

			// 4. Check in local dev path (fallback)
			agentPath = fmt.Sprintf("agents/%s.yaml", agentName)
			if _, err := os.Stat(agentPath); os.IsNotExist(err) {
				return fmt.Errorf("agent file not found: %s", agentName)
			}
		}
	}

Found:
	cfg, err := config.LoadAgent(agentPath)
	if err != nil {
		return fmt.Errorf("failed to load agent: %w", err)
	}

	ui.RenderCharmFlow(cfg)
	return nil
}

func handleEditCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: astonish flows edit <flow_name>")
		return fmt.Errorf("no agent name provided")
	}

	agentName := args[0]

	// Find the agent file path (reusing same logic as run/flow)
	agentPath := agentName
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		// Check with .yaml extension
		agentPath = fmt.Sprintf("%s.yaml", agentName)
		if _, err := os.Stat(agentPath); os.IsNotExist(err) {
			// Check in standard system agents directory
			agentsDir, err := config.GetAgentsDir()
			if err == nil {
				sysAgentPath := filepath.Join(agentsDir, fmt.Sprintf("%s.yaml", agentName))
				if _, err := os.Stat(sysAgentPath); err == nil {
					agentPath = sysAgentPath
					goto Found
				}
			}

			// Check in local dev path (fallback)
			agentPath = fmt.Sprintf("agents/%s.yaml", agentName)
			if _, err := os.Stat(agentPath); os.IsNotExist(err) {
				return fmt.Errorf("agent file not found: %s", agentName)
			}
		}
	}

Found:
	fmt.Printf("Opening %s in editor...\n", agentPath)
	return openInEditor(agentPath)
}

// Store command handlers

func handleStoreCommand(args []string) error {
	if len(args) < 1 || args[0] == "--help" || args[0] == "-h" {
		printStoreUsage()
		return nil
	}

	switch args[0] {
	case "tap":
		return handleStoreTapCommand(args[1:])
	case "list":
		return handleStoreListCommand()
	case "install":
		return handleStoreInstallCommand(args[1:])
	case "uninstall":
		return handleStoreUninstallCommand(args[1:])
	case "update":
		return handleStoreUpdateCommand()
	case "search":
		return handleStoreSearchCommand(args[1:])
	default:
		return fmt.Errorf("unknown store command: %s", args[0])
	}
}

func printStoreUsage() {
	fmt.Println("usage: astonish flows store [-h] {tap,list,install,uninstall,update,search} ...")
	fmt.Println("")
	fmt.Println("Browse and install flows from community stores.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  tap                 Manage flow store taps (add/remove/list)")
	fmt.Println("  list                List all available flows from stores")
	fmt.Println("  install             Install a flow from a store")
	fmt.Println("  uninstall           Remove an installed flow")
	fmt.Println("  update              Update all store manifests")
	fmt.Println("  search              Search for flows")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help          Show this help message")
}

func handleStoreTapCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("usage: astonish flows store tap {add,remove,list} ...")
		return nil
	}

	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			fmt.Println("usage: astonish flows store tap add <owner>[/repo] [--as <alias>]")
			fmt.Println("       astonish flows store tap add <alias> <url>")
			fmt.Println("")
			fmt.Println("Examples:")
			fmt.Println("  tap add company                  # assumes company/astonish-flows, tap name: company")
			fmt.Println("  tap add company/my-flows         # tap name: company-my-flows")
			fmt.Println("  tap add company/flows --as c     # tap name: c")
			fmt.Println("  tap add myalias github.company.com/team/flows  # enterprise with alias")
			return fmt.Errorf("no repository specified")
		}
		
		urlArg, alias := parseTapAddArgs(args[1:])
		
		tapName, err := store.AddTap(urlArg, alias)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Added tap: %s\n", tapName)
		fmt.Printf("  Use flows with: astonish flows run %s/<flow>\n", tapName)
		return nil

	case "remove":
		if len(args) < 2 {
			fmt.Println("usage: astonish flows store tap remove <tap-name>")
			return fmt.Errorf("no tap name specified")
		}
		if err := store.RemoveTap(args[1]); err != nil {
			return err
		}
		fmt.Printf("✓ Removed tap: %s\n", args[1])
		return nil

	case "list":
		taps := store.GetAllTaps()
		
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")).
			Bold(true)
		
		fmt.Println(headerStyle.Render("FLOW STORE TAPS"))
		fmt.Println()
		
		for _, tap := range taps {
			marker := ""
			if tap.Name == flowstore.OfficialStoreName {
				marker = " (official)"
			}
			fmt.Printf("  %s%s\n", tap.Name, marker)
			fmt.Printf("    └─ %s\n", tap.URL)
		}
		return nil

	default:
		return fmt.Errorf("unknown tap command: %s", args[0])
	}
}

func handleStoreListCommand() error {
	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	// Update manifests
	fmt.Println("Fetching flow store manifests...")
	if err := store.UpdateAllManifests(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	flows := store.ListAllFlows()
	if len(flows) == 0 {
		fmt.Println("No flows found in stores.")
		fmt.Println("Tip: Make sure the store repositories have a valid manifest.yaml")
		return nil
	}

	// Group by tap
	byTap := make(map[string][]flowstore.Flow)
	for _, f := range flows {
		byTap[f.TapName] = append(byTap[f.TapName], f)
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")).
		Bold(true)

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	installedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	// Print official first, then others
	printTapFlows := func(tapName string, flows []flowstore.Flow, isOfficial bool) {
		var label string
		if isOfficial {
			label = "OFFICIAL STORE (use bare flow names)"
		} else {
			label = fmt.Sprintf("%s (use: %s/<flow>)", tapName, tapName)
		}
		fmt.Println(headerStyle.Render(label))
		
		for _, f := range flows {
			status := ""
			if f.Installed {
				status = installedStyle.Render(" [installed]")
			}
			
			// Show full name for community, bare name for official
			displayName := f.Name
			if !isOfficial {
				displayName = fmt.Sprintf("%s/%s", tapName, f.Name)
			}
			
			fmt.Printf("  %s%s\n", nameStyle.Render(displayName), status)
			fmt.Printf("    %s\n", descStyle.Render(f.Description))
		}
		fmt.Println()
	}

	// Official first
	if official, ok := byTap[flowstore.OfficialStoreName]; ok {
		printTapFlows(flowstore.OfficialStoreName, official, true)
	}

	// Then custom taps
	for tapName, tapFlows := range byTap {
		if tapName != flowstore.OfficialStoreName {
			printTapFlows(tapName, tapFlows, false)
		}
	}

	fmt.Println("Tip: Run 'astonish flows run <flow>' for official or '<tap>/<flow>' for community")
	return nil
}

func handleStoreInstallCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("usage: astonish flows store install <tap>/<flow>")
		fmt.Println("       astonish flows store install <flow>  (from official store)")
		return fmt.Errorf("no flow specified")
	}

	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	// Parse tap/flow
	tapName, flowName := parseFlowRef(args[0])

	fmt.Printf("Installing %s/%s...\n", tapName, flowName)
	if err := store.InstallFlow(tapName, flowName); err != nil {
		return fmt.Errorf("failed to install flow: %w", err)
	}

	// Show appropriate run command based on tap
	runName := flowName
	if tapName != flowstore.OfficialStoreName {
		runName = tapName + "/" + flowName
	}
	fmt.Printf("✓ Installed flow: %s\n", runName)
	fmt.Printf("  Run with: astonish flows run %s\n", runName)
	return nil
}

func handleStoreUninstallCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("usage: astonish flows store uninstall <tap>/<flow>")
		return fmt.Errorf("no flow specified")
	}

	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	// Parse tap/flow
	tapName, flowName := parseFlowRef(args[0])

	if err := store.UninstallFlow(tapName, flowName); err != nil {
		return err
	}

	fmt.Printf("✓ Uninstalled flow: %s\n", flowName)
	return nil
}

func handleStoreUpdateCommand() error {
	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	fmt.Println("Updating all store manifests...")
	if err := store.UpdateAllManifests(); err != nil {
		return err
	}

	fmt.Println("✓ All stores updated")
	return nil
}

func handleStoreSearchCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("usage: astonish flows store search <query>")
		return fmt.Errorf("no search query specified")
	}

	store, err := flowstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	// Update manifests
	if err := store.UpdateAllManifests(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	query := strings.ToLower(strings.Join(args, " "))
	flows := store.ListAllFlows()

	var matches []flowstore.Flow
	for _, f := range flows {
		matched := false
		
		// Check name and description
		if strings.Contains(strings.ToLower(f.Name), query) ||
			strings.Contains(strings.ToLower(f.Description), query) {
			matched = true
		}
		
		// Check tags
		if !matched {
			for _, tag := range f.Tags {
				if strings.Contains(strings.ToLower(tag), query) {
					matched = true
					break
				}
			}
		}
		
		if matched {
			matches = append(matches, f)
		}
	}

	if len(matches) == 0 {
		fmt.Printf("No flows found matching '%s'\n", query)
		return nil
	}

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	fmt.Printf("Flows matching '%s':\n\n", query)
	for _, f := range matches {
		fmt.Printf("  %s/%s\n", f.TapName, nameStyle.Render(f.Name))
		fmt.Printf("    %s\n", f.Description)
	}

	return nil
}

// parseFlowRef parses "tap/flow" or "flow" (defaults to official)
func parseFlowRef(ref string) (tapName, flowName string) {
	if strings.Contains(ref, "/") {
		parts := strings.SplitN(ref, "/", 2)
		return parts[0], parts[1]
	}
	return flowstore.OfficialStoreName, ref
}

// parseTapAddArgs parses the arguments for "tap add" command
// Supports two formats:
// 1. add <url> [--as <alias>]
// 2. add <alias> <url> (when second arg contains a dot, indicating a hostname)
func parseTapAddArgs(args []string) (urlArg, alias string) {
	if len(args) == 0 {
		return "", ""
	}
	
	urlArg = args[0]
	alias = ""
	
	// Check if second arg exists and looks like a URL (contains a dot)
	if len(args) >= 2 && strings.Contains(args[1], ".") {
		// Format: <alias> <url>
		alias = args[0]
		urlArg = args[1]
	} else {
		// Format: <url> [--as <alias>]
		for i := 1; i < len(args); i++ {
			if args[i] == "--as" && i+1 < len(args) {
				alias = args[i+1]
				break
			}
		}
	}
	
	return urlArg, alias
}
