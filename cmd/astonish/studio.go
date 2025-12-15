package astonish

import (
	"context"
	"flag"
	"fmt"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
)

func handleStudioCommand(args []string) error {
	studioCmd := flag.NewFlagSet("studio", flag.ExitOnError)
	port := studioCmd.Int("port", 9393, "Port to run the studio server on")

	if err := studioCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Set up provider environment variables from config
	// This matches what agents.go does for CLI commands
	if appCfg, err := config.LoadAppConfig(); err == nil {
		config.SetupAllProviderEnv(appCfg)
	}

	// Set up MCP environment variables
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// Initialize tools cache (MCP tools loaded once at startup)
	api.InitToolsCache(context.Background())

	return launcher.RunStudio(*port)
}
