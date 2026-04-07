package launcher

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// InteractiveFlowRunner implements tools.FlowRunnerAccess.
// It manages stateful flow executions that can pause on input nodes
// and resume when the user provides a response via the chat.
type InteractiveFlowRunner struct {
	AppConfig    *config.AppConfig
	ProviderName string
	ModelName    string
	DebugMode    bool

	// sessions tracks active flow executions by session key.
	sessions sync.Map // map[string]*flowSession
}

// flowSession holds the state for a single in-progress flow execution.
type flowSession struct {
	mu              sync.Mutex
	runner          *runner.Runner
	sessionService  session.Service
	sessionID       string
	userID          string
	appName         string
	agentConfig     *config.AgentConfig
	output          strings.Builder // accumulated output
	currentNode     string
	resolvedOptions []string // runtime-resolved options from the flow engine
	resolvedPrompt  string   // runtime-resolved prompt from the flow engine
	cleanupFuncs    []func() // deferred cleanup (MCP, browser, sandbox)
}

// RunFlow starts or resumes a flow execution.
func (ifr *InteractiveFlowRunner) RunFlow(
	ctx context.Context,
	flowPath string,
	parameters map[string]string,
	inputResponse string,
	sessionKey string,
) (*tools.FlowRunResult, error) {
	// Check if we're resuming an existing session
	if inputResponse != "" {
		return ifr.resumeFlow(ctx, sessionKey, inputResponse)
	}

	// Start a new flow execution
	return ifr.startFlow(ctx, flowPath, parameters, sessionKey)
}

// CleanupSession removes state for a completed/abandoned flow.
func (ifr *InteractiveFlowRunner) CleanupSession(sessionKey string) {
	if val, ok := ifr.sessions.LoadAndDelete(sessionKey); ok {
		sess := val.(*flowSession)
		sess.mu.Lock()
		defer sess.mu.Unlock()
		for _, cleanup := range sess.cleanupFuncs {
			cleanup()
		}
	}
}

// GetPausedNode returns the input node name the flow is paused on, or "".
func (ifr *InteractiveFlowRunner) GetPausedNode(sessionKey string) string {
	val, ok := ifr.sessions.Load(sessionKey)
	if !ok {
		return ""
	}
	sess := val.(*flowSession)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Check if current node is an input node
	for _, n := range sess.agentConfig.Nodes {
		if n.Name == sess.currentNode && n.Type == "input" {
			return sess.currentNode
		}
	}
	return ""
}

// GetPausedOptions returns the resolved options for the currently paused
// input node, or nil if there are no options (free text input).
func (ifr *InteractiveFlowRunner) GetPausedOptions(sessionKey string) []string {
	val, ok := ifr.sessions.Load(sessionKey)
	if !ok {
		return nil
	}
	sess := val.(*flowSession)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.resolvedOptions
}

// startFlow initializes and runs a new flow.
func (ifr *InteractiveFlowRunner) startFlow(
	ctx context.Context,
	flowPath string,
	parameters map[string]string,
	sessionKey string,
) (*tools.FlowRunResult, error) {
	// Clean up any existing session with the same key
	ifr.CleanupSession(sessionKey)

	// Load the flow config
	agentCfg, err := config.LoadAgent(flowPath)
	if err != nil {
		return &tools.FlowRunResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to load flow: %v", err),
		}, nil
	}

	// Suppress default logger in non-debug mode
	if !ifr.DebugMode {
		log.SetOutput(io.Discard)
	}

	var cleanups []func()

	// Ensure provider secrets are available
	configDir, configDirErr := config.GetConfigDir()
	if configDirErr == nil {
		if cs, csErr := credentials.Open(configDir); csErr == nil {
			if tools.GetCredentialStore() == nil {
				tools.SetCredentialStore(cs)
			}
			config.InjectProviderSecretsToConfig(ifr.AppConfig, cs.GetSecret)
			config.SetupAllProviderEnvFromStore(ifr.AppConfig, cs.GetSecret)
		}
	}

	// Initialize LLM
	if ifr.DebugMode {
		provider.SetDebugMode(true)
	}
	llm, err := provider.GetProvider(ctx, ifr.ProviderName, ifr.ModelName, ifr.AppConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Initialize tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tools: %w", err)
	}

	credTools, credErr := tools.GetCredentialTools()
	if credErr == nil {
		internalTools = append(internalTools, credTools...)
	}

	processTools, procErr := tools.GetProcessTools()
	if procErr == nil {
		internalTools = append(internalTools, processTools...)
	}

	browserMgr := browser.NewManager(browserConfigFromApp(ifr.AppConfig))
	wireBrowserContainerCallbacks(browserMgr)
	browserTools, browserErr := tools.GetBrowserTools(browserMgr)
	if browserErr == nil {
		internalTools = append(internalTools, browserTools...)
	}
	cleanups = append(cleanups, browserMgr.Cleanup)

	// Sandbox
	internalTools, sandboxCleanup, sandboxErr := setupFlowSandbox(ifr.AppConfig, internalTools, ifr.DebugMode)
	if sandboxErr != nil {
		// Run cleanup for already-initialized resources
		for _, c := range cleanups {
			c()
		}
		return nil, fmt.Errorf("sandbox is enabled but the runtime is not available: %w", sandboxErr)
	}
	cleanups = append(cleanups, sandboxCleanup)

	// MCP tools
	requiredServers := getRequiredMCPServersFromConfig(ctx, agentCfg, ifr.DebugMode)
	var mcpToolsets []tool.Toolset

	if len(requiredServers) > 0 {
		mcpManager, mcpErr := mcp.NewManager()
		if mcpErr == nil {
			if initErr := mcpManager.InitializeSelectiveToolsets(ctx, requiredServers); initErr == nil {
				mcpToolsets = mcpManager.GetToolsets()
			}
			cleanups = append(cleanups, mcpManager.Cleanup)
		}
	}

	// Session service
	sessionService := session.InMemoryService()

	// Create AstonishAgent with auto-approve (the user's decision to run the flow is the approval)
	astonishAgent := agent.NewAstonishAgentWithToolsets(agentCfg, llm, internalTools, mcpToolsets)
	astonishAgent.DebugMode = ifr.DebugMode
	astonishAgent.AutoApprove = true
	astonishAgent.SessionService = sessionService

	// Wire credential redactor and store for placeholder substitution
	if cs := tools.GetCredentialStore(); cs != nil {
		astonishAgent.Redactor = cs.Redactor()
		astonishAgent.CredentialStore = cs
		astonishAgent.PendingSecrets = credentials.NewPendingVault(cs.Redactor())
		// Attach proactive secret scanner
		if ifr.AppConfig == nil || ifr.AppConfig.Security.IsSecretScannerEnabled() {
			scanner := credentials.NewSecretScanner()
			if ifr.AppConfig != nil {
				sc := ifr.AppConfig.Security.SecretScanner
				if sc.EntropyThreshold > 0 {
					scanner.EntropyThreshold = sc.EntropyThreshold
				}
				if sc.MinTokenLength > 0 {
					scanner.MinTokenLength = sc.MinTokenLength
				}
			}
			astonishAgent.PendingSecrets.Scanner = scanner
		}
	}

	// Create ADK agent
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_interactive_flow",
		Description: agentCfg.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		for _, c := range cleanups {
			c()
		}
		return nil, fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create session
	userID, appName := "flow_runner", "astonish"
	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		for _, c := range cleanups {
			c()
		}
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Create runner
	rnr, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		for _, c := range cleanups {
			c()
		}
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// Store the session
	sess := &flowSession{
		runner:         rnr,
		sessionService: sessionService,
		sessionID:      resp.Session.ID(),
		userID:         userID,
		appName:        appName,
		agentConfig:    agentCfg,
		cleanupFuncs:   cleanups,
	}
	ifr.sessions.Store(sessionKey, sess)

	// Run the flow with parameters
	return ifr.executeFlowTurn(ctx, sess, nil, parameters)
}

// resumeFlow resumes a paused flow with the user's input response.
func (ifr *InteractiveFlowRunner) resumeFlow(
	ctx context.Context,
	sessionKey string,
	inputResponse string,
) (*tools.FlowRunResult, error) {
	val, ok := ifr.sessions.Load(sessionKey)
	if !ok {
		return &tools.FlowRunResult{
			Status:  "error",
			Message: "No active flow session found. The flow may have timed out. Please start again with run_flow.",
		}, nil
	}

	sess := val.(*flowSession)
	userMsg := agent.NewTimestampedUserContent(inputResponse)

	// Reset accumulated output so the response only carries new output
	// produced during this turn (incremental output).
	sess.mu.Lock()
	sess.output.Reset()
	sess.mu.Unlock()

	return ifr.executeFlowTurn(ctx, sess, userMsg, nil)
}

// executeFlowTurn runs one turn of the flow engine, collecting output until
// the flow completes, hits an input node, or errors.
func (ifr *InteractiveFlowRunner) executeFlowTurn(
	ctx context.Context,
	sess *flowSession,
	userMsg *genai.Content,
	parameters map[string]string,
) (*tools.FlowRunResult, error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	for {
		isInputNode := false
		waitingForInput := false
		waitingForApproval := false
		suppressStreaming := false
		nodeJustChanged := false
		var userMessageFields []string
		var turnText strings.Builder

		for event, err := range sess.runner.Run(ctx, sess.userID, sess.sessionID, userMsg, adkagent.RunConfig{}) {
			if err != nil {
				return &tools.FlowRunResult{
					Status:  "error",
					Output:  sess.output.String(),
					Message: fmt.Sprintf("Flow error: %v", err),
				}, nil
			}

			nodeJustChanged = false

			if event.Actions.StateDelta != nil {
				delta := event.Actions.StateDelta

				// Node transition
				if node, ok := delta["current_node"].(string); ok {
					if node != sess.currentNode {
						nodeJustChanged = true

						if !suppressStreaming && turnText.Len() > 0 {
							sess.output.WriteString(turnText.String())
							turnText.Reset()
						} else {
							turnText.Reset()
						}

						sess.currentNode = node
						sess.resolvedOptions = nil
						sess.resolvedPrompt = ""
						suppressStreaming = false
						userMessageFields = nil
						isInputNode = false

						for _, n := range sess.agentConfig.Nodes {
							if n.Name == sess.currentNode {
								switch n.Type {
								case "input":
									isInputNode = true
									suppressStreaming = true
								case "output":
									suppressStreaming = false
								default:
									if len(n.UserMessage) > 0 {
										suppressStreaming = true
										userMessageFields = n.UserMessage
									} else if len(n.OutputModel) > 0 {
										suppressStreaming = true
									}
								}
								break
							}
						}
					}
				}

				// Capture runtime-resolved input options from the flow engine
				if opts, ok := delta["input_options"]; ok {
					if optList, ok := opts.([]string); ok {
						sess.resolvedOptions = optList
					} else if optList, ok := opts.([]interface{}); ok {
						sess.resolvedOptions = nil
						for _, item := range optList {
							sess.resolvedOptions = append(sess.resolvedOptions, fmt.Sprintf("%v", item))
						}
					}
				}

				// Check for waiting_for_input
				if waiting, ok := delta["waiting_for_input"].(bool); ok && waiting {
					waitingForInput = true
				}

				// Approval handling (auto-approve)
				if awaitingVal, ok := delta["awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						waitingForApproval = true
					}
				}
				if autoApprovedVal, ok := delta["auto_approved"]; ok {
					if auto, ok := autoApprovedVal.(bool); ok && auto {
						waitingForApproval = false
					}
				}

				// Capture user_message fields
				if len(userMessageFields) > 0 && suppressStreaming && !nodeJustChanged {
					for _, field := range userMessageFields {
						if val, ok := delta[field]; ok {
							if s, ok := val.(string); ok {
								sess.output.WriteString(s)
								sess.output.WriteString("\n")
							} else {
								sess.output.WriteString(fmt.Sprintf("%v", val))
								sess.output.WriteString("\n")
							}
						}
					}
				}
			}

			// Collect text
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.Text != "" {
						if isInputNode {
							// Capture the rendered prompt from the flow engine
							sess.resolvedPrompt += part.Text
						}
						if !suppressStreaming || isInputNode {
							turnText.WriteString(part.Text)
						}
					}
				}
			}
		}

		// Flush remaining text
		if !suppressStreaming && turnText.Len() > 0 {
			sess.output.WriteString(turnText.String())
		}

		// Flow completed
		if sess.currentNode == "END" {
			return &tools.FlowRunResult{
				Status: "completed",
				Output: strings.TrimSpace(sess.output.String()),
			}, nil
		}

		// Input node — check if we have a parameter for it
		if waitingForInput || isInputNode {
			if parameters != nil {
				if val, ok := parameters[sess.currentNode]; ok {
					userMsg = agent.NewTimestampedUserContent(val)
					continue // feed the parameter and continue execution
				}
			}

			// Use runtime-resolved prompt/options from the flow engine,
			// falling back to static YAML for the prompt if needed.
			prompt := sess.resolvedPrompt
			options := sess.resolvedOptions
			if prompt == "" {
				prompt, _ = ifr.getInputNodeDetails(sess.agentConfig, sess.currentNode)
			}

			// Build the guidance message based on whether this is a selection or free-text input
			msg := fmt.Sprintf("The flow needs input for node %q.", sess.currentNode)
			if len(options) > 0 {
				msg += " The user MUST select one of the listed options exactly. If the user's answer doesn't match an option, ask them to choose from the list."
			} else {
				msg += " This is a free-text input — pass the user's exact words."
			}
			msg += fmt.Sprintf(" Call run_flow again with parameters: {\"%s\": \"<value>\"}.", sess.currentNode)

			return &tools.FlowRunResult{
				Status:       "waiting_for_input",
				Output:       strings.TrimSpace(sess.output.String()),
				Message:      msg,
				InputNode:    sess.currentNode,
				InputPrompt:  prompt,
				InputOptions: options,
			}, nil
		}

		// Auto-approve
		if waitingForApproval {
			userMsg = agent.NewTimestampedUserContent("Yes")
			continue
		}

		// Agent completed a turn without needing input — done
		return &tools.FlowRunResult{
			Status: "completed",
			Output: strings.TrimSpace(sess.output.String()),
		}, nil
	}
}

// getInputNodeDetails extracts prompt and options for an input node.
func (ifr *InteractiveFlowRunner) getInputNodeDetails(agentCfg *config.AgentConfig, nodeName string) (string, []string) {
	for _, n := range agentCfg.Nodes {
		if n.Name == nodeName && n.Type == "input" {
			prompt := n.Prompt
			if prompt == "" {
				prompt = fmt.Sprintf("Please provide input for: %s", nodeName)
			}
			return prompt, n.Options
		}
	}
	return fmt.Sprintf("Please provide input for: %s", nodeName), nil
}

// NewInteractiveFlowRunner creates a new flow runner instance.
func NewInteractiveFlowRunner(appCfg *config.AppConfig, providerName, modelName string, debugMode bool) *InteractiveFlowRunner {
	return &InteractiveFlowRunner{
		AppConfig:    appCfg,
		ProviderName: providerName,
		ModelName:    modelName,
		DebugMode:    debugMode,
	}
}

// Verify interface compliance at compile time.
var _ tools.FlowRunnerAccess = (*InteractiveFlowRunner)(nil)

// LogActiveFlowSessions logs the number of active flow sessions for debugging.
func (ifr *InteractiveFlowRunner) LogActiveFlowSessions() int {
	count := 0
	ifr.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if ifr.DebugMode && count > 0 {
		slog.Debug("active flow sessions", "component", "interactive-flow-runner", "count", count)
	}
	return count
}
