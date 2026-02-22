package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/channels"
	"github.com/schardosin/astonish/pkg/channels/telegram"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
)

// RunConfig holds the configuration for a daemon run.
type RunConfig struct {
	Port  int
	Debug bool
}

// Run starts the daemon in the foreground. It starts the Studio HTTP server,
// writes a PID file, handles signals for graceful shutdown, and cleans up on exit.
// This function blocks until a shutdown signal is received.
func Run(cfg RunConfig) error {
	// Load app config for provider/MCP setup
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Resolve port
	port := cfg.Port
	if port <= 0 {
		if appCfg.Daemon.Port > 0 {
			port = appCfg.Daemon.Port
		} else {
			port = 9393
		}
	}

	// Set up logging
	logDir := appCfg.Daemon.GetLogDir()
	logger, err := NewLogger(logDir + "/daemon.log")
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	// Redirect standard log to file
	log.SetOutput(logger)
	log.SetFlags(0) // Logger adds its own timestamps

	logger.Printf("Astonish daemon starting (port: %d, pid: %d)", port, os.Getpid())

	// Write PID file
	pidPath, err := DefaultPIDPath()
	if err != nil {
		return fmt.Errorf("failed to resolve PID path: %w", err)
	}
	if err := WritePID(pidPath); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer RemovePID(pidPath)

	// Set up provider environment variables
	config.SetupAllProviderEnv(appCfg)

	// Set up MCP environment variables
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// Initialize tools cache
	ctx := context.Background()
	api.InitToolsCache(ctx)

	// --- Initialize channel manager if channels are enabled ---
	var channelMgr *channels.ChannelManager

	if appCfg.Channels.IsChannelsEnabled() {
		logger.Printf("Channels enabled, initializing ChatAgent...")

		// Build a fully-wired ChatAgent for channel use
		factoryResult, factoryErr := launcher.NewWiredChatAgent(ctx, &launcher.ChatFactoryConfig{
			AppConfig:    appCfg,
			ProviderName: appCfg.General.DefaultProvider,
			ModelName:    appCfg.General.DefaultModel,
			DebugMode:    cfg.Debug,
			AutoApprove:  true, // Channels auto-approve all tools (no interactive approval)
		})
		if factoryErr != nil {
			logger.Printf("Warning: Failed to initialize ChatAgent for channels: %v", factoryErr)
		} else {
			defer factoryResult.Cleanup()

			channelMgr = channels.NewChannelManager(factoryResult.ChatAgent, factoryResult.SessionService, log.Default(), &channels.ChannelManagerConfig{
				ProviderName: factoryResult.ProviderName,
				ModelName:    factoryResult.ModelName,
				ToolCount:    len(factoryResult.InternalTools),
			})

			// Register Telegram if enabled
			if appCfg.Channels.Telegram.IsTelegramEnabled() {
				tg := telegram.New(&telegram.Config{
					BotToken:  appCfg.Channels.Telegram.BotToken,
					AllowFrom: appCfg.Channels.Telegram.AllowFrom,
					Commands:  channelMgr.Commands(),
				}, log.Default())
				channelMgr.Register(tg)
				logger.Printf("Telegram channel registered")
			}

			// Start all channels
			if err := channelMgr.StartAll(ctx); err != nil {
				logger.Printf("Warning: Failed to start channels: %v", err)
			} else {
				logger.Printf("Channels started")
			}

			// Make channel manager available to API handlers
			api.SetChannelManager(channelMgr)
		}
	}

	// Create and start the Studio server
	studio, err := launcher.NewStudioServer(port)
	if err != nil {
		logger.Printf("Failed to start HTTP server: %v", err)
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start serving in a goroutine
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("HTTP server listening on http://localhost:%d", port)
		if err := studio.Serve(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Print to stdout for foreground mode
	fmt.Printf("Astonish daemon running (pid: %d, port: %d)\n", os.Getpid(), port)
	fmt.Printf("Log: %s/daemon.log\n", logDir)

	// Wait for signal or server error
	select {
	case sig := <-sigCh:
		logger.Printf("Received signal %v, shutting down...", sig)
		fmt.Printf("\nShutting down...\n")
	case err := <-errCh:
		if err != nil {
			logger.Printf("Server error: %v", err)
			return err
		}
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop channels first (drain in-flight messages)
	if channelMgr != nil {
		logger.Printf("Stopping channels...")
		if err := channelMgr.StopAll(shutdownCtx); err != nil {
			logger.Printf("Warning: Channel shutdown error: %v", err)
		}
	}

	if err := studio.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Shutdown error: %v", err)
		return fmt.Errorf("shutdown error: %w", err)
	}

	logger.Printf("Daemon stopped cleanly")
	return nil
}
