package astonish

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/sandbox"
)

func handleStudioCommand(args []string) error {
	studioCmd := flag.NewFlagSet("studio", flag.ExitOnError)
	port := studioCmd.Int("port", 9393, "Port to run the studio server on")

	if err := studioCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Escalate to root on Linux when sandbox is enabled (needs overlay
	// mounts, UID shifting, and Incus socket access).
	if sandbox.NeedsEscalation() {
		if cfg, err := config.LoadAppConfig(); err == nil && cfg != nil {
			if sandbox.IsSandboxEnabled(&cfg.Sandbox) {
				return sandbox.Escalate()
			}
		}
	}

	// Set up provider environment variables from config
	// Prefer credential store for secrets, fall back to legacy config
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
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
					if err := config.SaveAppConfig(appCfg); err != nil {
						slog.Warn("failed to save config after credential migration", "error", err)
					}
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
