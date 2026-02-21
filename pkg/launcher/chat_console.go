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
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/provider"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	"github.com/schardosin/astonish/pkg/ui"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
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

	// --- 2b. Initialize memory manager and memory_save tool ---
	memMgr, memErr := memory.NewManager("", cfg.DebugMode)
	if memErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to initialize memory manager: %v\n", memErr)
		}
	}

	// --- 2c. Initialize semantic memory (vector store, indexer) ---
	var memStore *memory.Store
	var memIndexer *memory.Indexer
	memorySearchAvailable := false

	if memMgr != nil && cfg.AppConfig != nil && cfg.AppConfig.Memory.IsMemoryEnabled() {
		memCfg := &cfg.AppConfig.Memory

		memDir, mdErr := config.GetMemoryDir(memCfg)
		vecDir, vdErr := config.GetVectorDir(memCfg)

		if mdErr == nil && vdErr == nil {
			// Ensure memory directory exists
			if err := os.MkdirAll(memDir, 0755); err != nil {
				if cfg.DebugMode {
					fmt.Printf("Warning: Failed to create memory directory: %v\n", err)
				}
			} else {
				// Resolve embedding function
				embeddingFunc, embErr := memory.ResolveEmbeddingFunc(cfg.AppConfig, memCfg)
				if embErr != nil {
					if cfg.DebugMode {
						fmt.Printf("Warning: No embedding provider available: %v\n", embErr)
					}
				} else {
					// Build store config
					storeCfg := memory.DefaultStoreConfig()
					storeCfg.MemoryDir = memDir
					storeCfg.VectorDir = vecDir
					if memCfg.Chunking.MaxChars > 0 {
						storeCfg.ChunkMaxChars = memCfg.Chunking.MaxChars
					}
					if memCfg.Chunking.Overlap > 0 {
						storeCfg.ChunkOverlap = memCfg.Chunking.Overlap
					}
					if memCfg.Search.MaxResults > 0 {
						storeCfg.MaxResults = memCfg.Search.MaxResults
					}
					if memCfg.Search.MinScore > 0 {
						storeCfg.MinScore = memCfg.Search.MinScore
					}

					store, storeErr := memory.NewStore(storeCfg, embeddingFunc)
					if storeErr != nil {
						if cfg.DebugMode {
							fmt.Printf("Warning: Failed to create memory store: %v\n", storeErr)
						}
					} else {
						memStore = store
						memIndexer = memory.NewIndexer(store, storeCfg, cfg.DebugMode)
						store.SetIndexer(memIndexer)
						memorySearchAvailable = true

						// Perform initial indexing in background
						go func() {
							if err := memIndexer.IndexAll(context.Background()); err != nil {
								if cfg.DebugMode {
									fmt.Printf("Warning: Initial memory indexing failed: %v\n", err)
								}
							}
						}()

						// Start file watcher in background
						if memCfg.IsWatchEnabled() {
							debounceMs := memCfg.Sync.DebounceMs
							if debounceMs <= 0 {
								debounceMs = 1500
							}
							watchCtx, watchCancel := context.WithCancel(context.Background())
							go func() {
								if err := memIndexer.WatchAndSync(watchCtx, debounceMs); err != nil {
									if cfg.DebugMode {
										fmt.Printf("Warning: Memory file watcher error: %v\n", err)
									}
								}
							}()
							defer watchCancel()
						}
					}
				}
			}
		}
	}

	// Create memory tools
	if memMgr != nil {
		var saveStore tools.MemorySaveStore
		if memStore != nil {
			saveStore = memStore
		}
		memorySaveTool, msErr := tools.NewMemorySaveTool(memMgr, saveStore)
		if msErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create memory_save tool: %v\n", msErr)
			}
		} else {
			internalTools = append(internalTools, memorySaveTool)
		}
	}

	if memorySearchAvailable && memStore != nil {
		searchTool, searchErr := tools.NewMemorySearchTool(memStore)
		if searchErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create memory_search tool: %v\n", searchErr)
			}
		} else {
			internalTools = append(internalTools, searchTool)
		}

		memDir, _ := config.GetMemoryDir(&cfg.AppConfig.Memory)
		getTool, getErr := tools.NewMemoryGetTool(memDir)
		if getErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create memory_get tool: %v\n", getErr)
			}
		} else {
			internalTools = append(internalTools, getTool)
		}
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
	// Chat defaults to file-based persistence so conversations survive restarts.
	// Users can opt out with `sessions: { storage: memory }` in config.
	var sessionService session.Service
	if cfg.AppConfig != nil && cfg.AppConfig.Sessions.Storage == "memory" {
		sessionService = session.InMemoryService()
		if cfg.DebugMode {
			fmt.Println("Session storage: memory")
		}
	} else {
		var sessCfg *config.SessionConfig
		if cfg.AppConfig != nil {
			sessCfg = &cfg.AppConfig.Sessions
		}
		sessDir, dirErr := config.GetSessionsDir(sessCfg)
		if dirErr != nil {
			return fmt.Errorf("failed to resolve sessions directory: %w", dirErr)
		}
		fileStore, fsErr := persistentsession.NewFileStore(sessDir)
		if fsErr != nil {
			return fmt.Errorf("failed to create file session store: %w", fsErr)
		}
		sessionService = fileStore
		if cfg.DebugMode {
			fmt.Printf("Session storage: file (%s)\n", sessDir)
		}
	}

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

		// Identify configured web tool servers so they survive the budget cut.
		// Users configure these in config.yaml (e.g., "tavily:tavily-extract"),
		// so dropping them would break the system prompt's tool routing guidance.
		priorityServers := map[string]bool{}
		if _, serverName, _ := api.IsWebSearchConfigured(); serverName != "" {
			priorityServers[serverName] = true
		}
		if _, serverName, _ := api.IsWebExtractConfigured(); serverName != "" {
			priorityServers[serverName] = true
		}
		// Keyless standard servers (e.g. Playwright) are always active and
		// referenced in the system prompt, so they must survive trimming too.
		for _, srv := range config.GetStandardServers() {
			if len(srv.EnvVars) == 0 {
				priorityServers[srv.ID] = true
			}
		}

		var trimmedToolsets []tool.Toolset
		remaining := mcpBudget
		minCtx := &minimalReadonlyContext{Context: ctx}

		// First pass: include priority toolsets (configured web tool servers)
		for _, ts := range mcpToolsets {
			if !priorityServers[ts.Name()] {
				continue
			}
			tsTools, err := ts.Tools(minCtx)
			if err != nil {
				continue
			}
			if len(tsTools) <= remaining {
				trimmedToolsets = append(trimmedToolsets, ts)
				remaining -= len(tsTools)
			}
		}

		// Second pass: fill remaining budget with other toolsets
		for _, ts := range mcpToolsets {
			if remaining <= 0 {
				break
			}
			if priorityServers[ts.Name()] {
				continue // already included
			}
			tsTools, err := ts.Tools(minCtx)
			if err != nil {
				continue
			}
			if len(tsTools) <= remaining {
				trimmedToolsets = append(trimmedToolsets, ts)
				remaining -= len(tsTools)
			}
		}

		droppedTools := totalMCPTools - (mcpBudget - remaining)
		if droppedTools > 0 {
			fmt.Printf("Warning: Tool count (%d) exceeds provider limit (%d). Dropped %d MCP tools.\n",
				totalTools, maxTools, droppedTools)
			if cfg.DebugMode {
				fmt.Printf("  Internal tools: %d (always included)\n", len(internalTools))
				fmt.Printf("  MCP tools included: %d, dropped: %d\n", mcpBudget-remaining, droppedTools)
				if len(priorityServers) > 0 {
					fmt.Printf("  Priority servers (web tools): %v\n", priorityServers)
				}
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

	// Check web tool availability
	if webSearchConfigured, _, toolName := api.IsWebSearchConfigured(); webSearchConfigured {
		promptBuilder.WebSearchAvailable = true
		promptBuilder.WebSearchToolName = toolName
	}
	if webExtractConfigured, _, toolName := api.IsWebExtractConfigured(); webExtractConfigured {
		promptBuilder.WebExtractAvailable = true
		promptBuilder.WebExtractToolName = toolName
	}

	// Check browser availability (Playwright or similar)
	// Only set BrowserAvailable if the toolset actually survived trimming,
	// otherwise the prompt would reference tools the model can't use.
	for _, ts := range mcpToolsets {
		if ts.Name() == "playwright" {
			promptBuilder.BrowserAvailable = true
			break
		}
	}

	// Check memory search availability
	if memorySearchAvailable {
		promptBuilder.MemorySearchAvailable = true
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
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.MaxToolCalls > 0 {
		chatAgent.MaxToolCalls = cfg.AppConfig.Chat.MaxToolCalls
	}

	// --- 6d. Wire memory and flow context ---
	if memMgr != nil {
		chatAgent.MemoryManager = memMgr
	}
	chatAgent.FlowContextBuilder = &agent.FlowContextBuilder{DebugMode: cfg.DebugMode}

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

	var sess session.Session
	isResumed := false
	if cfg.SessionID != "" {
		// Resolve partial session ID if using file store
		resolvedID := cfg.SessionID
		if fs, ok := sessionService.(*persistentsession.FileStore); ok {
			fullID, resolveErr := fs.ResolveSessionID(cfg.SessionID)
			if resolveErr != nil {
				return fmt.Errorf("failed to resolve session ID %q: %w", cfg.SessionID, resolveErr)
			}
			resolvedID = fullID
		}
		// Resume existing session
		getResp, getErr := sessionService.Get(ctx, &session.GetRequest{
			AppName:   appName,
			UserID:    userID,
			SessionID: resolvedID,
		})
		if getErr != nil {
			return fmt.Errorf("failed to resume session %s: %w", resolvedID, getErr)
		}
		sess = getResp.Session
		isResumed = true
		if cfg.DebugMode {
			fmt.Printf("Resumed session: %s (%d events)\n", sess.ID(), sess.Events().Len())
		}
	} else {
		// Create new session
		resp, createErr := sessionService.Create(ctx, &session.CreateRequest{
			AppName: appName,
			UserID:  userID,
		})
		if createErr != nil {
			return fmt.Errorf("failed to create session: %w", createErr)
		}
		sess = resp.Session
	}

	// --- 9. ANSI colors ---
	const (
		ColorReset  = "\033[0m"
		ColorGreen  = "\033[32m"
		ColorCyan   = "\033[36m"
		ColorYellow = "\033[33m"
	)

	// --- 10. Welcome message ---
	fmt.Printf("\n%sAstonish Chat%s - Type your message (Ctrl+C to exit)\n", ColorGreen, ColorReset)
	if isResumed {
		shortID := sess.ID()
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("Session: %s (resumed, %d events)\n", shortID, sess.Events().Len())
	} else {
		shortID := sess.ID()
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("Session: %s (new)\n", shortID)
	}
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
	fmt.Printf(" | Provider: %s", cfg.ProviderName)
	if memMgr != nil {
		memContent, _ := memMgr.Load()
		if memContent != "" {
			fmt.Print(" | Memory: active")
		}
	}
	if memorySearchAvailable {
		fmt.Printf(" | RAG: %d chunks", memStore.Count())
	}
	if chatAgent.FlowRegistry != nil {
		entries := chatAgent.FlowRegistry.Entries()
		if len(entries) > 0 {
			fmt.Printf(" | Flows: %d saved", len(entries))
		}
	}
	fmt.Println()
	fmt.Println()

	// --- 10b. Show recent history on resume ---
	if isResumed && sess.Events().Len() > 0 {
		printRecentHistory(sess, 3, ColorCyan, ColorGreen, ColorReset)
	}

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

	// --- 12b. Title generation tracking ---
	needsTitle := !isResumed // new sessions need a title; resumed ones already have one
	turnCount := 0

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

		// Slash command dispatch
		if strings.HasPrefix(input, "/") {
			switch {
			case input == "/distill":
				startSpinner("Analyzing conversation...")
				description, previewErr := chatAgent.PreviewDistill(ctx)
				stopSpinner()
				if previewErr != nil {
					fmt.Printf("%sError:%s %v\n\n", "\033[31m", ColorReset, previewErr)
					break
				}
				fmt.Printf("%sTask identified:%s %s\n", ColorGreen, ColorReset, description)
				fmt.Printf("Distill this into a reusable flow? (yes/no): ")
				confirm, _ := reader.ReadString('\n')
				confirm = strings.TrimSpace(confirm)
				if !strings.EqualFold(confirm, "yes") && !strings.EqualFold(confirm, "y") {
					fmt.Println("Cancelled.")
					break
				}
				startSpinner("Distilling flow...")
				distillErr := chatAgent.ConfirmAndDistill(ctx, func(s string) {
					stopSpinner()
					fmt.Printf("%sAgent:%s %s", ColorGreen, ColorReset, s)
				})
				stopSpinner()
				if distillErr != nil {
					fmt.Printf("%sError:%s %v\n", "\033[31m", ColorReset, distillErr)
				}
				fmt.Println()
			case input == "/help":
				fmt.Printf("%sAvailable commands:%s\n", ColorCyan, ColorReset)
				fmt.Println("  /distill  - Distill the last task into a reusable flow")
				fmt.Println("  /help     - Show this help message")
				fmt.Println("  exit      - Exit the chat")
				fmt.Println()
			default:
				fmt.Printf("Unknown command: %s. Type /help for available commands.\n\n", input)
			}
			continue
		}

		// Send message to agent
		userMsg := genai.NewContentFromText(input, genai.RoleUser)
		startSpinner("Thinking...")

		aiPrefixPrinted := false
		var textBuffer strings.Builder
		waitingForApproval := false
		var approvalOptions []string
		inToolBox := false
		lastEventWasTool := false

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

			// Detect tool call/response events to insert separators
			hasTool := false
			chunk := ""
			for _, p := range event.LLMResponse.Content.Parts {
				chunk += p.Text
				if p.FunctionCall != nil || p.FunctionResponse != nil {
					hasTool = true
				}
			}
			if hasTool {
				lastEventWasTool = true
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
						} else if lastEventWasTool {
							// Insert separator between text segments separated by tool calls
							fmt.Print("\n")
						}
						lastEventWasTool = false
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
			lastEventWasTool = false

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

				// Detect tool call/response events to insert separators
				hasTool := false
				chunk := ""
				for _, p := range event.LLMResponse.Content.Parts {
					chunk += p.Text
					if p.FunctionCall != nil || p.FunctionResponse != nil {
						hasTool = true
					}
				}
				if hasTool {
					lastEventWasTool = true
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
						} else if lastEventWasTool {
							fmt.Print("\n")
						}
						lastEventWasTool = false
						fmt.Print(chunk)
					}
				}
			}

			stopSpinner()
		}

		// Track turns and generate title after first exchange
		turnCount++
		if needsTitle && turnCount == 1 {
			needsTitle = false
			// Fire background goroutine to generate session title via LLM
			if fs, ok := sessionService.(*persistentsession.FileStore); ok {
				go generateSessionTitle(ctx, llm, fs, sess.ID(), input)
			}
		}

		// Newline after response
		fmt.Println()
	}

	return nil
}

// generateSessionTitle calls the LLM to produce a short, meaningful session title
// from the user's first message. Runs in a background goroutine.
func generateSessionTitle(ctx context.Context, llm model.LLM, store *persistentsession.FileStore, sessionID, userMessage string) {
	prompt := fmt.Sprintf(
		"Generate a concise title (5-7 words max) for a conversation that starts with this message. "+
			"Return ONLY the title, no quotes, no punctuation at the end.\n\nUser message: %s", userMessage)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(float32(0.3)),
			MaxOutputTokens: 30,
		},
	}

	var title string
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				title += part.Text
			}
		}
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	// Truncate if somehow too long
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	_ = store.SetSessionTitle(sessionID, title)
}

// printRecentHistory displays the last N user/assistant exchanges from the session.
// It coalesces consecutive same-author events into single messages (since SSE
// streaming produces many small events per turn) and skips tool call/response events.
func printRecentHistory(sess session.Session, maxExchanges int, colorCyan, colorGreen, colorReset string) {
	events := sess.Events()

	// Coalesce consecutive same-author text events into single messages.
	type message struct {
		role string // "user" or "agent"
		text string
	}
	var messages []message

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		// Extract text, skipping function call/response parts
		hasOnlyFuncParts := true
		var text string
		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionCall != nil || part.FunctionResponse != nil {
				continue
			}
			hasOnlyFuncParts = false
			text += part.Text
		}
		if hasOnlyFuncParts || text == "" {
			continue
		}

		role := "agent"
		if event.Author == "user" {
			role = "user"
		}

		// Coalesce with previous message if same author
		if len(messages) > 0 && messages[len(messages)-1].role == role {
			messages[len(messages)-1].text += text
		} else {
			messages = append(messages, message{role: role, text: text})
		}
	}

	if len(messages) == 0 {
		return
	}

	// Take last N exchanges (each exchange = user + agent pair)
	// Walk backwards to find where to start
	exchangeCount := 0
	startIdx := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].role == "user" {
			exchangeCount++
			if exchangeCount > maxExchanges {
				break
			}
			startIdx = i
		}
	}

	const colorGray = "\033[90m"
	divider := colorGray + "── Recent history ──────────────────────────" + colorReset
	dividerEnd := colorGray + "────────────────────────────────────────────" + colorReset

	fmt.Println(divider)
	for _, msg := range messages[startIdx:] {
		display := strings.TrimSpace(msg.text)

		if msg.role == "user" {
			fmt.Printf("%sYou:%s %s\n", colorCyan, colorReset, display)
		} else {
			fmt.Printf("%sAgent:%s\n%s\n", colorGreen, colorReset, display)
		}
	}
	fmt.Println(dividerEnd)
	fmt.Println()
}
