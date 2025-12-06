package launcher

import (
	"context"
	"fmt"
	"log"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/cmd/launcher/web/api"
	"google.golang.org/adk/cmd/launcher/web/webui"
	"google.golang.org/adk/session"
)

// WebConfig contains configuration for the web launcher
type WebConfig struct {
	AgentConfig    *config.AgentConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
	Port           int
}

// RunWeb runs the agent in web mode with ADK's embedded browser UI
func RunWeb(ctx context.Context, cfg *WebConfig) error {
	// Initialize LLM
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Initialize tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}

	// Create Astonish agent
	astonishAgent := agent.NewAstonishAgent(cfg.AgentConfig, llm, internalTools)

	// Create ADK agent wrapper
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_agent",
		Description: cfg.AgentConfig.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create session service
	sessionService := cfg.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

	// Create agent loader
	agentLoader := adkagent.NewSingleLoader(adkAgent)

	// Create launcher config
	launcherConfig := &launcher.Config{
		AgentLoader:    agentLoader,
		SessionService: sessionService,
	}

	// Create web launcher with API and WebUI sublaunchers
	webLauncher := web.NewLauncher(
		api.NewLauncher(),
		webui.NewLauncher(),
	)

	// Build arguments for the web launcher
	args := []string{
		fmt.Sprintf("-port=%d", cfg.Port),
		"api",
		"webui",
	}

	log.Printf("Starting web server on port %d...", cfg.Port)
	log.Printf("Open your browser to: http://localhost:%d", cfg.Port)

	// Execute the web launcher (it's a SubLauncher, so we call Run)
	_, err = webLauncher.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse web launcher args: %w", err)
	}
	
	return webLauncher.Run(ctx, launcherConfig)
}
