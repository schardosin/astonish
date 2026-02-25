package astonish

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/launcher"
)

func handleStudioCommand(args []string) error {
	studioCmd := flag.NewFlagSet("studio", flag.ExitOnError)
	port := studioCmd.Int("port", 9393, "Port to run the studio server on")

	if err := studioCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Set up provider environment variables from config
	// Prefer credential store for secrets, fall back to legacy config
	appCfg, _ := config.LoadAppConfig()
	if appCfg != nil {
		if cfgDir, err := config.GetConfigDir(); err == nil {
			if cs, csErr := credentials.Open(cfgDir); csErr == nil {
				config.SetInstalledSecretGetter(cs.GetSecret)
				api.SetAPICredentialStore(cs)

				// Auto-migrate (one-time)
				migrated, migrateErr := credentials.MigrateFromConfig(cs, appCfg, log.Default())
				if migrateErr != nil {
					fmt.Printf("Warning: Credential migration error: %v\n", migrateErr)
				} else if migrated > 0 {
					_ = config.SaveAppConfig(appCfg)
				}
				config.SetupAllProviderEnvFromStore(appCfg, cs.GetSecret)
			} else {
				config.SetupAllProviderEnv(appCfg)
			}
		} else {
			config.SetupAllProviderEnv(appCfg)
		}
	}

	// Set up MCP environment variables
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// Initialize tools cache (MCP tools loaded once at startup)
	api.InitToolsCache(context.Background())

	return launcher.RunStudio(*port)
}
