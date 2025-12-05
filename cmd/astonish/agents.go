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
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/ui"
	"google.golang.org/adk/session"
)

func handleAgentsCommand(args []string) error {
	if len(args) < 1 || args[0] == "--help" || args[0] == "-h" {
		printAgentsUsage()
		return nil
	}

	switch args[0] {
	case "run":
		return handleRunCommand(args[1:])
	case "list":
		return handleListCommand()
	case "flow":
		return handleFlowCommand(args[1:])
	default:
		return fmt.Errorf("unknown agents command: %s", args[0])
	}
}

func printAgentsUsage() {
	fmt.Println("usage: astonish agents [-h] {run,list,flow} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {run,list,flow}")
	fmt.Println("                        Agent management commands")
	fmt.Println("    run                 Run an agent")
	fmt.Println("    list                List available agents")
	fmt.Println("    flow                Visualize the agent flow")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
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
			fmt.Println("Usage: astonish agents run [flags] <agent_name>")
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
			agentPath = fmt.Sprintf("astonish/agents/%s.yaml", agentName)
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
	}
	agents := make(map[string]AgentInfo)

	// Helper to scan directory
	scanDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return // Ignore errors
		}
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
				name := entry.Name()[:len(entry.Name())-5]
				// Only process if not already found (prioritize system over local or vice versa? 
				// The previous logic prioritized System then Local, but map overwrite would mean Local wins if processed second.
				// Let's check existence to respect priority order: System first, then Local.
				if _, exists := agents[name]; !exists {
					path := filepath.Join(dir, entry.Name())
					cfg, err := config.LoadAgent(path)
					if err != nil {
						continue // Skip invalid agents
					}
					agents[name] = AgentInfo{
						Name:        name,
						Description: cfg.Description,
					}
				}
			}
		}
	}

	// 1. Scan System Directory
	if sysDir, err := config.GetAgentsDir(); err == nil {
		scanDir(sysDir)
	}

	// 2. Scan Local Directory
	scanDir("astonish/agents")

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	// 1. Styles
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")). // Purple
		Bold(true).
		PaddingBottom(1) // Space after header

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")). // Pinkish
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")) // White/Grey

	// 2. Find longest name for padding
	var maxLen int
	var keys []string
	for k := range agents {
		if len(k) > maxLen {
			maxLen = len(k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys) // Always sort!

	// 3. Render
	// Print Header
	fmt.Println(headerStyle.Render("AVAILABLE AGENTS"))

	for _, name := range keys {
		info := agents[name]
		
		// Render the name with fixed width padding
		// %-*s means: Left justify (-), pad to width (*), string (s)
		paddedName := fmt.Sprintf("%-*s", maxLen + 4, name) 
		
		row := lipgloss.JoinHorizontal(lipgloss.Left,
			nameStyle.Render(paddedName),
			descStyle.Render(info.Description),
		)
		
		fmt.Println(row)
	}

	return nil
}

func handleFlowCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: astonish agents flow <agent_name>")
		return fmt.Errorf("no agent name provided")
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
			agentPath = fmt.Sprintf("astonish/agents/%s.yaml", agentName)
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
