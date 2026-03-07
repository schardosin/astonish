package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// RunHeadlessFunc is a function type for running a flow headlessly.
// It is injected by the daemon to avoid import cycles (scheduler -> launcher -> api).
type RunHeadlessFunc func(ctx context.Context, cfg *HeadlessRunConfig) (string, error)

// FleetPollFunc is a function type for polling a fleet plan's external channel.
// It is injected by the daemon to avoid import cycles (scheduler -> fleet).
// The planKey identifies which fleet plan to poll. Returns a result summary.
type FleetPollFunc func(ctx context.Context, planKey string) (string, error)

// HeadlessRunConfig holds the parameters for a headless flow run.
// This mirrors launcher.HeadlessConfig but lives in the scheduler package
// to avoid the import cycle.
type HeadlessRunConfig struct {
	FlowPath     string
	AppConfig    *config.AppConfig
	ProviderName string
	ModelName    string
	Parameters   map[string]string
	DebugMode    bool
}

// Executor runs scheduled jobs. It uses an injected headless runner for routine
// mode and the shared ChatAgent for adaptive mode.
type Executor struct {
	// ChatAgent is the shared agent for adaptive mode execution.
	ChatAgent *agent.ChatAgent
	// SessionService provides session persistence.
	SessionService session.Service
	// AppConfig holds provider and general settings.
	AppConfig *config.AppConfig
	// ProviderName is the default provider for execution.
	ProviderName string
	// ModelName is the default model for execution.
	ModelName string
	// DebugMode enables verbose logging.
	DebugMode bool
	// RunHeadless is the function to call for routine (flow) execution.
	// Injected by the daemon to avoid import cycles.
	RunHeadless RunHeadlessFunc
	// FleetPoll is the function to call for fleet_poll mode execution.
	// Injected by the daemon to avoid import cycles.
	FleetPoll FleetPollFunc
}

// Execute runs a job based on its mode and returns the result text.
func (e *Executor) Execute(ctx context.Context, job *Job) (string, error) {
	switch job.Mode {
	case ModeRoutine:
		return e.executeRoutine(ctx, job)
	case ModeAdaptive:
		return e.executeAdaptive(ctx, job)
	case ModeFleetPoll:
		return e.executeFleetPoll(ctx, job)
	default:
		return "", fmt.Errorf("unknown job mode: %s", job.Mode)
	}
}

// executeRoutine runs a flow through the headless flow engine.
func (e *Executor) executeRoutine(ctx context.Context, job *Job) (string, error) {
	if job.Payload.Flow == "" {
		return "", fmt.Errorf("routine job %q has no flow specified", job.Name)
	}
	if e.RunHeadless == nil {
		return "", fmt.Errorf("headless runner not configured")
	}

	// Resolve flow path
	agentPath, err := resolveFlowPath(job.Payload.Flow)
	if err != nil {
		return "", fmt.Errorf("flow %q not found: %w", job.Payload.Flow, err)
	}

	return e.RunHeadless(ctx, &HeadlessRunConfig{
		FlowPath:     agentPath,
		AppConfig:    e.AppConfig,
		ProviderName: e.ProviderName,
		ModelName:    e.ModelName,
		Parameters:   job.Payload.Params,
		DebugMode:    e.DebugMode,
	})
}

// executeAdaptive sends stored instructions as a chat message through the ChatAgent.
func (e *Executor) executeAdaptive(ctx context.Context, job *Job) (string, error) {
	if job.Payload.Instructions == "" {
		return "", fmt.Errorf("adaptive job %q has no instructions", job.Name)
	}

	if e.ChatAgent == nil {
		return "", fmt.Errorf("no ChatAgent available for adaptive execution (enable channels to use adaptive mode)")
	}

	// Set scheduler-specific output constraints on the shared ChatAgent.
	// This tells the LLM to produce data-only output with no conversational preamble.
	if e.ChatAgent.SystemPrompt != nil {
		e.ChatAgent.SystemPrompt.SchedulerHints = `You are executing a SCHEDULED TASK automatically. Your output will be delivered directly as a notification.
CRITICAL RULES:
- Output ONLY the requested data in the format specified by the instructions
- Do NOT add preamble, greetings, or explain what you are doing
- Do NOT mention saved workflows, flows, or execution plans you found
- Do NOT add conversational filler like "Here's what I found" or "Let me check"
- Do NOT add follow-up questions or offers to help
- Just execute the task and return the formatted result`
		defer func() { e.ChatAgent.SystemPrompt.SchedulerHints = "" }()
	}

	// Create a per-job session key for isolation
	sessionKey := fmt.Sprintf("scheduler:adaptive:%s", job.ID)
	userID := "scheduler"
	appName := "astonish"

	// Get or create session
	sess, err := getOrCreateSession(ctx, e.SessionService, appName, userID, sessionKey)
	if err != nil {
		return "", fmt.Errorf("session error: %w", err)
	}

	// Create ADK agent wrapper for this execution
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_scheduler",
		Description: "Astonish scheduled adaptive task",
		Run:         e.ChatAgent.Run,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: e.SessionService,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create runner: %w", err)
	}

	// Send instructions as user message
	userContent := genai.NewContentFromText(job.Payload.Instructions, genai.RoleUser)
	var responseText strings.Builder

	for event, err := range r.Run(ctx, userID, sess.ID(), userContent, adkagent.RunConfig{}) {
		if err != nil {
			return responseText.String(), fmt.Errorf("agent error: %w", err)
		}

		if event.LLMResponse.Content == nil {
			continue
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				responseText.WriteString(part.Text)
			}
		}
	}

	return strings.TrimSpace(responseText.String()), nil
}

// getOrCreateSession retrieves or creates a session by key.
func getOrCreateSession(ctx context.Context, sessSvc session.Service, appName, userID, sessionKey string) (session.Session, error) {
	getResp, err := sessSvc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionKey,
	})
	if err == nil && getResp.Session != nil {
		return getResp.Session, nil
	}

	createResp, err := sessSvc.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionKey,
	})
	if err != nil {
		return nil, err
	}
	return createResp.Session, nil
}

// resolveFlowPath resolves a flow name to its filesystem path using the same
// resolution chain as the CLI (agents dir, flows dir, etc.).
func resolveFlowPath(name string) (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}

	// Try direct path
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}

	// Try with .yaml extension
	withYAML := name + ".yaml"
	if _, err := os.Stat(withYAML); err == nil {
		return withYAML, nil
	}

	// Try agents directory
	agentsPath := filepath.Join(configDir, "agents", name+".yaml")
	if _, err := os.Stat(agentsPath); err == nil {
		return agentsPath, nil
	}

	// Try flows directory
	flowsPath := filepath.Join(configDir, "flows", name+".yaml")
	if _, err := os.Stat(flowsPath); err == nil {
		return flowsPath, nil
	}

	return "", fmt.Errorf("flow %q not found in any search path", name)
}

// executeFleetPoll delegates to the injected FleetPollFunc.
// The job's Payload.Flow field holds the fleet plan key to poll.
func (e *Executor) executeFleetPoll(ctx context.Context, job *Job) (string, error) {
	planKey := job.Payload.Flow
	if planKey == "" {
		return "", fmt.Errorf("fleet_poll job %q has no plan key (Payload.Flow)", job.Name)
	}
	if e.FleetPoll == nil {
		return "", fmt.Errorf("fleet poll function not configured")
	}
	return e.FleetPoll(ctx, planKey)
}
