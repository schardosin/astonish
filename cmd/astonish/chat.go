package astonish

import (
	"context"
	"flag"
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
)

func handleChatCommand(args []string) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v\n", err)
		appCfg = &config.AppConfig{}
	}

	chatCmd := flag.NewFlagSet("chat", flag.ExitOnError)

	providerName := chatCmd.String("provider", "", "AI provider")
	modelName := chatCmd.String("model", "", "Model name")
	workspaceDir := chatCmd.String("workspace", "", "Working directory (default: current dir)")
	autoApprove := chatCmd.Bool("auto-approve", false, "Auto-approve all tool executions")
	debugMode := chatCmd.Bool("debug", false, "Enable debug mode")

	// Short flag aliases
	chatCmd.StringVar(providerName, "p", "", "AI provider (short)")
	chatCmd.StringVar(modelName, "m", "", "Model name (short)")
	chatCmd.StringVar(workspaceDir, "w", "", "Working directory (short)")

	// Handle --help
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printChatUsage()
		return nil
	}

	if err := chatCmd.Parse(args); err != nil {
		return err
	}

	// Resolve provider: flag > config > error
	resolvedProvider := *providerName
	if resolvedProvider == "" {
		resolvedProvider = appCfg.General.DefaultProvider
	}
	if resolvedProvider == "" {
		fmt.Println("Error: No provider specified. Use --provider flag or set default_provider in config.")
		fmt.Println("Run 'astonish setup' to configure providers.")
		return fmt.Errorf("no provider specified")
	}

	// Resolve model: flag > config > empty (provider default)
	resolvedModel := *modelName
	if resolvedModel == "" {
		resolvedModel = appCfg.General.DefaultModel
	}

	cfg := &launcher.ChatConsoleConfig{
		AppConfig:    appCfg,
		ProviderName: resolvedProvider,
		ModelName:    resolvedModel,
		DebugMode:    *debugMode,
		AutoApprove:  *autoApprove,
		WorkspaceDir: *workspaceDir,
	}

	return launcher.RunChatConsole(context.Background(), cfg)
}

func printChatUsage() {
	fmt.Println("usage: astonish chat [options]")
	fmt.Println("")
	fmt.Println("Start an interactive chat session with an AI agent that can use tools.")
	fmt.Println("The agent dynamically decides how to solve tasks using available tools.")
	fmt.Println("Complex tasks can be saved as reusable flows.")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -p, --provider      AI provider (default: from config)")
	fmt.Println("  -m, --model         Model name (default: from config)")
	fmt.Println("  -w, --workspace     Working directory (default: current dir)")
	fmt.Println("  --auto-approve      Auto-approve all tool executions")
	fmt.Println("  --debug             Enable debug output")
	fmt.Println("  -h, --help          Show this help message")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish chat")
	fmt.Println("  astonish chat -p openai -m gpt-4o")
	fmt.Println("  astonish chat --auto-approve")
	fmt.Println("")
	fmt.Println("In chat mode, the agent has access to all configured tools (internal + MCP)")
	fmt.Println("and will call them as needed to accomplish your tasks.")
}
