package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// HeadlessConfig contains configuration for a headless (non-interactive) flow run.
type HeadlessConfig struct {
	AgentConfig    *config.AgentConfig
	AppConfig      *config.AppConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
	Parameters     map[string]string
	DebugMode      bool
}

// RunHeadless executes a flow without a TUI. It runs the flow engine with
// auto-approve enabled, injects parameters for input nodes, and collects
// all output text into a string. Returns the collected output and any error.
//
// This is used by the scheduler for "routine" mode jobs.
func RunHeadless(ctx context.Context, cfg *HeadlessConfig) (string, error) {
	// NOTE: We intentionally do NOT suppress log output here.
	// Previously log.SetOutput(io.Discard) was used to hide ADK warnings,
	// but it also silenced all slog diagnostics (slog delegates through the
	// standard log package). Visibility into headless runs is more important
	// than suppressing noisy ADK log lines.

	// Ensure provider secrets are available. When called from the daemon,
	// secrets are typically already injected into AppConfig. But for safety
	// (and if called from other contexts), initialize the credential store
	// and inject secrets if they're missing.
	//
	// In multi-tenant platform mode, the credential store is injected into
	// the context by the multi-tenant scheduler. Tools will read it from
	// context via store.CredentialStoreFromContext(). We skip the global
	// file-based credential store initialization to avoid overwriting the
	// tenant-scoped store.
	if store.CredentialStoreFromContext(ctx) == nil {
		configDir, configDirErr := config.GetConfigDir()
		if configDirErr == nil {
			if cs, csErr := credentials.Open(configDir); csErr == nil {
				if tools.GetCredentialStore() == nil {
					tools.SetCredentialStore(cs)
				}
				config.InjectProviderSecretsToConfig(cfg.AppConfig, cs.GetSecret)
				config.SetupAllProviderEnvFromStore(cfg.AppConfig, cs.GetSecret)
			}
		}
	}

	// Initialize LLM
	if cfg.DebugMode {
		provider.SetDebugMode(true)
	}
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
	if err != nil {
		return "", fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Initialize internal tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return "", fmt.Errorf("failed to initialize tools: %w", err)
	}

	// Register credential tools (resolve_credential, etc.)
	credTools, credErr := tools.GetCredentialTools()
	if credErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create credential tools", "component", "headless", "error", credErr)
		}
	} else {
		internalTools = append(internalTools, credTools...)
	}

	// Register process management tools (process_start, process_write, etc.)
	processTools, procErr := tools.GetProcessTools()
	if procErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create process tools", "component", "headless", "error", procErr)
		}
	} else {
		internalTools = append(internalTools, processTools...)
	}

	// Register browser automation tools
	browserMgr := browser.NewManager(browserConfigFromApp(cfg.AppConfig))
	wireBrowserContainerCallbacks(browserMgr)
	browserTools, browserErr := tools.GetBrowserTools(browserMgr)
	if browserErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create browser tools", "component", "headless", "error", browserErr)
		}
	} else {
		internalTools = append(internalTools, browserTools...)
	}
	defer browserMgr.Cleanup()

	// Wrap tools with sandbox if enabled (container isolation for file/shell/network tools)
	internalTools, sandboxCleanup, sandboxErr := setupFlowSandbox(cfg.AppConfig, internalTools, cfg.DebugMode)
	if sandboxErr != nil {
		return "", fmt.Errorf("sandbox is enabled but the runtime is not available: %w", sandboxErr)
	}
	defer sandboxCleanup()

	// Initialize MCP tools needed for this flow
	requiredServers := getRequiredMCPServersFromConfig(ctx, cfg.AgentConfig, cfg.DebugMode)

	var mcpManager *mcp.Manager
	var mcpToolsets []tool.Toolset

	if len(requiredServers) > 0 {
		mcpManager, err = mcp.NewManager()
		if err != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create mcp manager", "component", "headless", "error", err)
			}
		} else {
			if err := mcpManager.InitializeSelectiveToolsets(ctx, requiredServers); err != nil {
				if cfg.DebugMode {
					slog.Warn("failed to initialize mcp toolsets", "component", "headless", "error", err)
				}
			} else {
				mcpToolsets = mcpManager.GetToolsets()
			}
		}
	}
	if mcpManager != nil {
		defer mcpManager.Cleanup()
	}

	// Session service
	sessionService := cfg.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

	// Create the AstonishAgent with auto-approve
	astonishAgent := agent.NewAstonishAgentWithToolsets(cfg.AgentConfig, llm, internalTools, mcpToolsets)
	astonishAgent.DebugMode = cfg.DebugMode
	astonishAgent.AutoApprove = true
	astonishAgent.SessionService = sessionService

	// Wire credential redactor and store for placeholder substitution
	if cs := tools.GetCredentialStore(); cs != nil {
		astonishAgent.Redactor = cs.Redactor()
		astonishAgent.CredentialStore = cs
		astonishAgent.PendingSecrets = credentials.NewPendingVault(cs.Redactor())
		// Inject Redactor into context so memory_save can Placeholderize()
		ctx = credentials.WithRedactor(ctx, cs.Redactor())
		// Attach proactive secret scanner
		if cfg.AppConfig == nil || cfg.AppConfig.Security.IsSecretScannerEnabled() {
			scanner := credentials.NewSecretScanner()
			if cfg.AppConfig != nil {
				sc := cfg.AppConfig.Security.SecretScanner
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

	// Create ADK agent wrapper
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_headless",
		Description: cfg.AgentConfig.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create session
	userID, appName := "headless_scheduler", "astonish"
	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	sess := resp.Session

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create runner: %w", err)
	}

	// Main execution loop — mirrors console.go logic but collects text instead of rendering
	var userMsg *genai.Content
	var currentNodeName string
	var output strings.Builder

	for {
		isInputNode := false
		isOutputNode := false
		waitingForInput := false
		waitingForApproval := false
		suppressStreaming := false
		var userMessageFields []string
		nodeJustChanged := false

		var turnText strings.Builder

		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, adkagent.RunConfig{}) {
			if err != nil {
				slog.Error("[headless] agent returned error", "node", currentNodeName, "error", err)
				return output.String(), fmt.Errorf("agent error: %w", err)
			}

			nodeJustChanged = false

			// Process state delta
			if event.Actions.StateDelta != nil {
				// Node transition
				if node, ok := event.Actions.StateDelta["current_node"].(string); ok {
					if node != currentNodeName {
						nodeJustChanged = true

						// Flush turn text for non-suppressed previous node
						if !suppressStreaming && turnText.Len() > 0 {
							output.WriteString(turnText.String())
							turnText.Reset()
						} else {
							turnText.Reset()
						}

						currentNodeName = node
						suppressStreaming = false
						userMessageFields = nil
						isInputNode = false
						isOutputNode = false

						for _, n := range cfg.AgentConfig.Nodes {
							if n.Name == currentNodeName {
								switch n.Type {
								case "input":
									isInputNode = true
									suppressStreaming = true
								case "output":
									isOutputNode = true
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

						slog.Info("[headless] node transition",
							"node", currentNodeName,
							"suppressStreaming", suppressStreaming,
							"userMessageFields", userMessageFields,
							"isInput", isInputNode,
							"isOutput", isOutputNode,
						)
					}
				}

				// Approval — auto-approve everything in headless mode
				if awaitingVal, ok := event.Actions.StateDelta["awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						waitingForApproval = true
					}
				}
				if autoApprovedVal, ok := event.Actions.StateDelta["auto_approved"]; ok {
					if auto, ok := autoApprovedVal.(bool); ok && auto {
						waitingForApproval = false
						// Continue processing — auto-approval is informational.
						// The tool still needs to execute and produce results.
					}
				}

				// Capture user_message fields for output
				if len(userMessageFields) > 0 && suppressStreaming && !nodeJustChanged {
					for _, field := range userMessageFields {
						if val, ok := event.Actions.StateDelta[field]; ok {
							if s, ok := val.(string); ok {
								slog.Info("[headless] captured user_message field", "field", field, "len", len(s))
								output.WriteString(s)
								output.WriteString("\n")
							} else {
								slog.Info("[headless] captured user_message field (non-string)", "field", field, "value", fmt.Sprintf("%v", val))
								output.WriteString(fmt.Sprintf("%v", val))
								output.WriteString("\n")
							}
						}
					}
				}

				// Track internal flow errors for diagnostics.
				// _failure_info is emitted via StateDelta when retries exhaust.
				if failInfo, ok := event.Actions.StateDelta["_failure_info"]; ok {
					slog.Warn("[headless] node failure detected", "node", currentNodeName, "info", failInfo)
				}
			}

			// Collect text from LLM response
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.Text != "" {
						if !suppressStreaming || isInputNode || isOutputNode {
							turnText.WriteString(part.Text)
						}
					}
				}
			}
		}

		// Flush remaining text from this turn
		if !suppressStreaming && turnText.Len() > 0 {
			output.WriteString(turnText.String())
		}

		// Handle end of turn
		if currentNodeName == "END" {
			break
		}

		// Handle input node — inject parameter
		if waitingForInput || isInputNode {
			if cfg.Parameters != nil {
				if val, ok := cfg.Parameters[currentNodeName]; ok {
					userMsg = agent.NewTimestampedUserContent(val)
					continue
				}
			}
			// No parameter available for this input node
			return output.String(), fmt.Errorf("input node %q requires a value but no parameter was provided", currentNodeName)
		}

		// Handle approval — always approve
		if waitingForApproval {
			userMsg = agent.NewTimestampedUserContent("Yes")
			continue
		}

		// Agent completed a turn without needing input — we're done
		break
	}

	result := strings.TrimSpace(output.String())

	// Check if the flow failed internally (all retries exhausted).
	// The flow transitions to END with _has_error=true but RunHeadless
	// previously returned ("", nil), masking the failure from the scheduler.
	getResp, getErr := sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sess.ID(),
	})
	if getErr == nil && getResp != nil && getResp.Session != nil {
		state := getResp.Session.State()
		if hasErr, _ := state.Get("_has_error"); hasErr != nil {
			if b, ok := hasErr.(bool); ok && b {
				lastErr, _ := state.Get("_last_error")
				errNode, _ := state.Get("_error_node")
				errMsg := fmt.Sprintf("flow node %v failed: %v", errNode, lastErr)
				slog.Warn("[headless] flow completed with internal error", "node", errNode, "error", lastErr)
				if result == "" {
					return "", fmt.Errorf("%s", errMsg)
				}
				// If we have partial output (from prior nodes), return it with the error
				return result, fmt.Errorf("%s", errMsg)
			}
		}
	}

	slog.Info("[headless] flow completed", "output_len", len(result), "final_node", currentNodeName)
	return result, nil
}
