package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	"github.com/schardosin/astonish/pkg/ui"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	tea "github.com/charmbracelet/bubbletea"
)

// ChatConsoleConfig contains configuration for the chat console launcher.
type ChatConsoleConfig struct {
	AppConfig    *config.AppConfig
	ProviderName string
	ModelName    string
	DebugMode    bool
	AutoApprove  bool
	SessionID    string // Resume existing session (empty = new)
	WorkspaceDir string
}

// RunChatConsole runs the agent in interactive chat mode.
// Unlike RunConsole, this does not require a flow config. The LLM drives
// behavior dynamically through tool-use loops.
func RunChatConsole(ctx context.Context, cfg *ChatConsoleConfig) error {
	// Suppress default logger unless debug mode
	if !cfg.DebugMode {
		log.SetOutput(io.Discard)
	}

	// --- 1. Initialize LLM ---
	if cfg.DebugMode {
		fmt.Println("Initializing LLM provider...")
	}
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize provider '%s' with model '%s': %w",
			cfg.ProviderName, cfg.ModelName, err)
	}
	if cfg.DebugMode {
		fmt.Printf("Provider initialized: %s (model: %s)\n", cfg.ProviderName, cfg.ModelName)
	}

	// --- 2. Initialize internal tools ---
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}

	// --- 3. Load MCP tools from cache (lazy -- no server connections at startup) ---
	mcpCfg, _ := config.LoadMCPConfig()
	var lazyToolsets []*agent.LazyMCPToolset
	var mcpToolsets []tool.Toolset

	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
		// Load cached tool metadata (instant -- just reads JSON from disk)
		if _, loadErr := cache.LoadCache(); loadErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to load tools cache: %v\n", loadErr)
			}
		}

		for name, serverCfg := range mcpCfg.MCPServers {
			if !serverCfg.IsEnabled() {
				continue
			}
			cachedTools := cache.GetToolsForServer(name)
			if len(cachedTools) == 0 {
				if cfg.DebugMode {
					fmt.Printf("[Chat Lazy] Server '%s' has no cached tools (run Studio or 'astonish tools refresh' first)\n", name)
				}
				continue
			}
			lt := agent.NewLazyMCPToolset(name, cachedTools, serverCfg, cfg.DebugMode)
			lazyToolsets = append(lazyToolsets, lt)
		}

		// Wrap lazy toolsets with schema sanitizer
		for _, lt := range lazyToolsets {
			mcpToolsets = append(mcpToolsets, agent.NewSanitizedToolset(lt, cfg.DebugMode))
		}
	}
	defer func() {
		for _, lt := range lazyToolsets {
			lt.Cleanup()
		}
	}()

	// Warn if MCP servers are configured but no tools are cached
	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 && len(lazyToolsets) == 0 {
		fmt.Println("Note: MCP servers are configured but no tools are cached.")
		fmt.Println("      Run 'astonish studio' or 'astonish tools refresh' to populate the cache.")
	}

	// --- 4. Create session service ---
	sessionService := session.InMemoryService()

	// --- 4b. Enforce tool count limit ---
	// Providers have tool limits (e.g., OpenAI: 128). Internal tools always
	// get priority; MCP toolsets are trimmed if the total exceeds the limit.
	maxTools := 128 // sensible default matching common provider limits
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.MaxTools > 0 {
		maxTools = cfg.AppConfig.Chat.MaxTools
	}

	totalMCPTools := 0
	if len(mcpToolsets) > 0 {
		minCtx := &minimalReadonlyContext{Context: ctx}
		for _, ts := range mcpToolsets {
			if t, err := ts.Tools(minCtx); err == nil {
				totalMCPTools += len(t)
			}
		}
	}

	totalTools := len(internalTools) + totalMCPTools
	if totalTools > maxTools {
		// Trim MCP toolsets to fit within the limit
		mcpBudget := maxTools - len(internalTools)
		if mcpBudget < 0 {
			mcpBudget = 0
		}

		var trimmedToolsets []tool.Toolset
		remaining := mcpBudget
		minCtx := &minimalReadonlyContext{Context: ctx}

		for _, ts := range mcpToolsets {
			if remaining <= 0 {
				break
			}
			tsTools, err := ts.Tools(minCtx)
			if err != nil {
				continue
			}
			if len(tsTools) <= remaining {
				trimmedToolsets = append(trimmedToolsets, ts)
				remaining -= len(tsTools)
			}
			// Skip toolsets that would exceed the budget entirely
			// (we don't split individual toolsets -- it's all or nothing per server)
		}

		droppedTools := totalMCPTools - (mcpBudget - remaining)
		if droppedTools > 0 {
			fmt.Printf("Warning: Tool count (%d) exceeds provider limit (%d). Dropped %d MCP tools.\n",
				totalTools, maxTools, droppedTools)
			if cfg.DebugMode {
				fmt.Printf("  Internal tools: %d (always included)\n", len(internalTools))
				fmt.Printf("  MCP tools included: %d, dropped: %d\n", mcpBudget-remaining, droppedTools)
			}
		}
		mcpToolsets = trimmedToolsets
	}

	// --- 5. Build system prompt ---
	workspaceDir := cfg.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}

	promptBuilder := &agent.SystemPromptBuilder{
		Tools:        internalTools,
		Toolsets:     mcpToolsets,
		WorkspaceDir: workspaceDir,
	}

	// Load custom prompt from app config if available
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.SystemPrompt != "" {
		promptBuilder.CustomPrompt = cfg.AppConfig.Chat.SystemPrompt
	}

	// --- 6. Create ChatAgent ---
	chatAgent := agent.NewChatAgent(
		llm, internalTools, mcpToolsets, sessionService,
		promptBuilder, cfg.DebugMode, cfg.AutoApprove,
	)

	// --- 6b. Initialize Flow Registry ---
	registryPath, regErr := agent.DefaultRegistryPath()
	if regErr == nil {
		registry, regLoadErr := agent.NewFlowRegistry(registryPath)
		if regLoadErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to load flow registry: %v\n", regLoadErr)
			}
		} else {
			chatAgent.FlowRegistry = registry
		}
	}

	// --- 6c. Initialize Flow Distiller ---
	// Bridge validation function to avoid import cycle (agent -> api)
	validateYAML := func(yamlStr string, distillerTools []agent.DistillerToolInfo) agent.FlowValidationResult {
		apiTools := make([]api.ToolInfo, len(distillerTools))
		for i, t := range distillerTools {
			apiTools[i] = api.ToolInfo{Name: t.Name, Description: t.Description, Source: t.Source}
		}
		result := api.ValidateFlowYAML(yamlStr, apiTools)
		return agent.FlowValidationResult{Valid: result.Valid, Errors: result.Errors}
	}

	chatAgent.FlowDistiller = agent.NewFlowDistiller(
		llm, internalTools, mcpToolsets,
		api.GetFlowSchema, validateYAML,
	)

	// Set flow save directory from config
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.FlowSaveDir != "" {
		chatAgent.FlowSaveDir = cfg.AppConfig.Chat.FlowSaveDir
	}
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.FlowSaveThreshold > 0 {
		chatAgent.FlowSaveThreshold = cfg.AppConfig.Chat.FlowSaveThreshold
	}
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.MaxToolCalls > 0 {
		chatAgent.MaxToolCalls = cfg.AppConfig.Chat.MaxToolCalls
	}

	// --- 7. Create ADK agent wrapper ---
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_chat",
		Description: "Astonish intelligent chat agent",
		Run:         chatAgent.Run,
	})
	if err != nil {
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// --- 8. Create runner + session ---
	userID, appName := "console_user", "astonish"
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	sess := resp.Session

	// --- 9. ANSI colors ---
	const (
		ColorReset  = "\033[0m"
		ColorGreen  = "\033[32m"
		ColorCyan   = "\033[36m"
		ColorYellow = "\033[33m"
	)

	// --- 10. Welcome message ---
	fmt.Printf("\n%sAstonish Chat%s - Type your message (Ctrl+C to exit)\n", ColorGreen, ColorReset)
	mcpToolCount := 0
	if len(mcpToolsets) > 0 {
		minCtx := &minimalReadonlyContext{Context: ctx}
		for _, ts := range mcpToolsets {
			if t, err := ts.Tools(minCtx); err == nil {
				mcpToolCount += len(t)
			}
		}
	}
	fmt.Printf("Tools: %d internal", len(internalTools))
	if mcpToolCount > 0 {
		fmt.Printf(" + %d MCP (lazy)", mcpToolCount)
	}
	fmt.Printf(" | Provider: %s\n\n", cfg.ProviderName)

	// --- 11. Spinner helpers ---
	var spinnerProgram *tea.Program
	var spinnerDone chan struct{}

	stopSpinner := func() {
		if spinnerProgram != nil {
			spinnerProgram.Quit()
			if spinnerDone != nil {
				<-spinnerDone
			}
			spinnerProgram = nil
			spinnerDone = nil
		}
	}

	startSpinner := func(text string) {
		stopSpinner()
		spinnerDone = make(chan struct{})
		spinnerModel := ui.NewSpinner(text)
		spinnerProgram = tea.NewProgram(spinnerModel, tea.WithInput(nil))
		go func() {
			spinnerProgram.Run()
			close(spinnerDone)
		}()
	}

	// --- 12. Input reader ---
	reader := bufio.NewReader(os.Stdin)

	// --- 13. Main chat loop ---
	for {
		// Read user input
		fmt.Printf("%sYou:%s ", ColorCyan, ColorReset)
		input, err := reader.ReadString('\n')
		if err != nil {
			// EOF (Ctrl+D) or error
			fmt.Println()
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if strings.EqualFold(input, "exit") || strings.EqualFold(input, "quit") {
			break
		}

		// Send message to agent
		userMsg := genai.NewContentFromText(input, genai.RoleUser)
		startSpinner("Thinking...")

		aiPrefixPrinted := false
		var textBuffer strings.Builder
		waitingForApproval := false
		var approvalOptions []string
		inToolBox := false

		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		}) {
			if err != nil {
				stopSpinner()
				fmt.Printf("\n%sError:%s %v\n", "\033[31m", ColorReset, err)
				break
			}

			// Process StateDelta
			if event.Actions.StateDelta != nil {
				// Debug tool calls
				if cfg.DebugMode {
					if event.LLMResponse.Content != nil {
						for _, part := range event.LLMResponse.Content.Parts {
							if part.FunctionCall != nil {
								argsJSON, _ := json.MarshalIndent(part.FunctionCall.Args, "", "  ")
								stopSpinner()
								fmt.Printf("\n%s[DEBUG] Tool Call: %s%s\nArgs: %s\n",
									ColorCyan, part.FunctionCall.Name, ColorReset, string(argsJSON))
							}
							if part.FunctionResponse != nil {
								respJSON, _ := json.MarshalIndent(part.FunctionResponse.Response, "", "  ")
								fmt.Printf("%s[DEBUG] Tool Response: %s%s\nResult: %s\n",
									ColorCyan, part.FunctionResponse.Name, ColorReset, string(respJSON))
							}
						}
					}
				}

				// Check for approval state
				if awaitingVal, ok := event.Actions.StateDelta["awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						waitingForApproval = true
					}
				}
				if optsVal, ok := event.Actions.StateDelta["approval_options"]; ok {
					if opts, ok := optsVal.([]string); ok {
						approvalOptions = opts
					} else if optsInterface, ok := optsVal.([]interface{}); ok {
						for _, v := range optsInterface {
							approvalOptions = append(approvalOptions, fmt.Sprintf("%v", v))
						}
					}
				}

				// Spinner text updates
				if spinnerText, ok := event.Actions.StateDelta["_spinner_text"].(string); ok {
					startSpinner(spinnerText)
				}
			}

			// Process content
			if event.LLMResponse.Content == nil {
				continue
			}

			chunk := ""
			for _, p := range event.LLMResponse.Content.Parts {
				chunk += p.Text
			}

			if chunk != "" {
				// Detect tool box boundaries
				if strings.Contains(chunk, "╭") {
					// Flush text buffer before tool box
					if textBuffer.Len() > 0 {
						stopSpinner()
						if !aiPrefixPrinted {
							fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
							aiPrefixPrinted = true
						}
						fmt.Print(textBuffer.String())
						textBuffer.Reset()
					}
					inToolBox = true
				}

				if inToolBox {
					stopSpinner()
					fmt.Print(chunk)
					if strings.Contains(chunk, "╰") {
						inToolBox = false
						aiPrefixPrinted = false
					}
				} else {
					textBuffer.WriteString(chunk)
					// Stream text to terminal
					if textBuffer.Len() > 0 {
						stopSpinner()
						if !aiPrefixPrinted {
							fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
							aiPrefixPrinted = true
						}
						fmt.Print(textBuffer.String())
						textBuffer.Reset()
					}
				}
			}
		}

		// Flush remaining text
		if textBuffer.Len() > 0 {
			stopSpinner()
			if !aiPrefixPrinted {
				fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
			}
			fmt.Print(textBuffer.String())
			textBuffer.Reset()
		}

		stopSpinner()

		// Handle approval if needed
		if waitingForApproval {
			opts := []string{"Yes", "No"}
			if len(approvalOptions) > 0 {
				opts = approvalOptions
			}
			selection, err := ui.ReadSelection(opts, "Approve tool execution?", "")
			if err != nil {
				return err
			}
			if selection == "Yes" {
				fmt.Println(ui.RenderStatusBadge("Command approved", true))
			} else {
				fmt.Println(ui.RenderStatusBadge("Command rejected", false))
			}
			// Feed approval response back
			userMsg = genai.NewContentFromText(selection, genai.RoleUser)

			// Re-run with approval response
			startSpinner("Executing...")
			aiPrefixPrinted = false
			textBuffer.Reset()
			inToolBox = false

			for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, adkagent.RunConfig{
				StreamingMode: adkagent.StreamingModeSSE,
			}) {
				if err != nil {
					stopSpinner()
					fmt.Printf("\nError: %v\n", err)
					break
				}

				if event.LLMResponse.Content == nil {
					continue
				}

				chunk := ""
				for _, p := range event.LLMResponse.Content.Parts {
					chunk += p.Text
				}
				if chunk != "" {
					if strings.Contains(chunk, "╭") {
						if textBuffer.Len() > 0 {
							stopSpinner()
							if !aiPrefixPrinted {
								fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
								aiPrefixPrinted = true
							}
							fmt.Print(textBuffer.String())
							textBuffer.Reset()
						}
						inToolBox = true
					}
					if inToolBox {
						stopSpinner()
						fmt.Print(chunk)
						if strings.Contains(chunk, "╰") {
							inToolBox = false
							aiPrefixPrinted = false
						}
					} else {
						stopSpinner()
						if !aiPrefixPrinted {
							fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
							aiPrefixPrinted = true
						}
						fmt.Print(chunk)
					}
				}
			}

			stopSpinner()
		}

		// Newline after response
		fmt.Println()
	}

	return nil
}
