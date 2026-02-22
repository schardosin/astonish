package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
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
	currentProvider := cfg.ProviderName
	currentModel := cfg.ModelName
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
	indexingDone := make(chan struct{}) // closed when background IndexAll completes
	var indexingErr error

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
				// Resolve embedding function (local model, auto-downloaded on first run)
				embResult, embErr := memory.ResolveEmbeddingFunc(cfg.AppConfig, memCfg, cfg.DebugMode)
				if embErr != nil {
					fmt.Printf("Note: Semantic memory unavailable (%v)\n", embErr)
				} else {
					// Clean up embedder resources on exit (e.g., Hugot session)
					if embResult.Cleanup != nil {
						defer embResult.Cleanup()
					}

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

					store, storeErr := memory.NewStore(storeCfg, embResult.EmbeddingFunc)
					if storeErr != nil {
						if cfg.DebugMode {
							fmt.Printf("Warning: Failed to create memory store: %v\n", storeErr)
						}
					} else {
						memStore = store
						memIndexer = memory.NewIndexer(store, storeCfg, false) // suppress indexer debug in chat; use 'astonish memory reindex' for diagnostics
						store.SetIndexer(memIndexer)
						memorySearchAvailable = true

						// Perform initial indexing in background
						go func() {
							indexingErr = memIndexer.IndexAll(context.Background())
							close(indexingDone)
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

	// If indexing was never started, close the channel so waits are no-ops
	if !memorySearchAvailable {
		close(indexingDone)
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

	// Track startup notices for inclusion in the welcome message
	var startupNotices []string

	// Warn if MCP servers are configured but no tools are cached
	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 && len(lazyToolsets) == 0 {
		startupNotices = append(startupNotices,
			"MCP servers are configured but no tools are cached yet. "+
				"Run 'astonish studio' or 'astonish tools refresh' to set them up.")
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
			startupNotices = append(startupNotices,
				fmt.Sprintf("I trimmed %d MCP tools to fit your provider's limit of %d — type /status for details.", droppedTools, maxTools))
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

	// Check web tool availability.
	// Config may reference an MCP server that was disabled or failed to load,
	// so we cross-check against the actually loaded mcpToolsets.
	if webSearchConfigured, serverName, toolName := api.IsWebSearchConfigured(); webSearchConfigured {
		for _, ts := range mcpToolsets {
			if ts.Name() == serverName {
				promptBuilder.WebSearchAvailable = true
				promptBuilder.WebSearchToolName = toolName
				break
			}
		}
	}
	if webExtractConfigured, serverName, toolName := api.IsWebExtractConfigured(); webExtractConfigured {
		for _, ts := range mcpToolsets {
			if ts.Name() == serverName {
				promptBuilder.WebExtractAvailable = true
				promptBuilder.WebExtractToolName = toolName
				break
			}
		}
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

	// Load INSTRUCTIONS.md (create with defaults if missing)
	var memDir string
	if cfg.AppConfig != nil && cfg.AppConfig.Memory.IsMemoryEnabled() {
		memDir, _ = config.GetMemoryDir(&cfg.AppConfig.Memory)
	}
	if memDir == "" {
		memDir, _ = config.GetMemoryDir(nil)
	}
	if memDir != "" {
		created, ensErr := memory.EnsureInstructions(memDir)
		if ensErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to ensure INSTRUCTIONS.md: %v\n", ensErr)
			}
		} else if created && cfg.DebugMode {
			fmt.Printf("Created default INSTRUCTIONS.md in %s\n", memDir)
		}
		instrContent, instrErr := memory.LoadInstructions(memDir)
		if instrErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to load INSTRUCTIONS.md: %v\n", instrErr)
			}
		} else if instrContent != "" {
			promptBuilder.InstructionsContent = instrContent
		}
	}

	// Generate SELF.md from current config state
	var selfMDMemDir string // directory where SELF.md lives
	if memDir != "" {
		selfMDMemDir = memDir

		// Build SelfMDConfig from current state
		selfCfg := buildSelfMDConfig(cfg, memDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable)

		selfContent := memory.GenerateSelfMD(selfCfg)
		if writeErr := memory.WriteSelfMD(memDir, selfContent); writeErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to write SELF.md: %v\n", writeErr)
			}
		} else {
			promptBuilder.SelfContent = selfContent
			if cfg.DebugMode {
				fmt.Printf("Generated SELF.md (%d bytes)\n", len(selfContent))
			}
		}
	}

	// Reconcile flow knowledge docs (memory/flows/) at startup
	var memFlowsDir string
	flowsDir, _ := flowstore.GetFlowsDir()
	if memDir != "" && flowsDir != "" {
		memFlowsDir = filepath.Join(memDir, "flows")
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

			// Auto-register any YAML files in flows/ that aren't in the registry
			// (e.g., flows created via Studio) and prune entries for deleted YAMLs
			if flowsDir != "" {
				added, syncErr := registry.SyncFromDirectory(flowsDir)
				if syncErr != nil {
					if cfg.DebugMode {
						fmt.Printf("Warning: Flow registry sync failed: %v\n", syncErr)
					}
				} else if added > 0 && cfg.DebugMode {
					fmt.Printf("Auto-registered %d new flows from %s\n", added, flowsDir)
				}
			}

			// Reconcile flow knowledge docs now that we have the registry
			if memFlowsDir != "" && flowsDir != "" {
				entries := registry.Entries()
				if len(entries) > 0 {
					if reconErr := agent.ReconcileFlowKnowledge(flowsDir, memFlowsDir, entries); reconErr != nil {
						if cfg.DebugMode {
							fmt.Printf("Warning: Flow knowledge reconciliation failed: %v\n", reconErr)
						}
					} else if cfg.DebugMode {
						fmt.Printf("Reconciled flow knowledge docs in %s\n", memFlowsDir)
					}
				}
			}
		}
	}

	// --- 6b2. Wire flow vector searcher ---
	if memorySearchAvailable && memStore != nil {
		chatAgent.FlowSearcher = agent.NewFlowMemorySearcher(
			func(ctx context.Context, query string, maxResults int, minScore float64) ([]agent.FlowSearchResult, error) {
				results, err := memStore.Search(ctx, query, maxResults, minScore)
				if err != nil {
					return nil, err
				}
				var flowResults []agent.FlowSearchResult
				for _, r := range results {
					flowResults = append(flowResults, agent.FlowSearchResult{
						Path:  r.Path,
						Score: r.Score,
					})
				}
				return flowResults, nil
			},
		)

		// Wire auto knowledge search for per-turn retrieval
		chatAgent.KnowledgeSearch = func(ctx context.Context, query string, maxResults int, minScore float64) ([]agent.KnowledgeSearchResult, error) {
			results, err := memStore.Search(ctx, query, maxResults, minScore)
			if err != nil {
				return nil, err
			}
			var knowledgeResults []agent.KnowledgeSearchResult
			for _, r := range results {
				knowledgeResults = append(knowledgeResults, agent.KnowledgeSearchResult{
					Path:    r.Path,
					Score:   r.Score,
					Snippet: r.Snippet,
				})
			}
			return knowledgeResults, nil
		}

		if cfg.DebugMode {
			fmt.Println("Flow vector search: enabled")
			fmt.Println("Auto knowledge retrieval: enabled")
		}
	}

	// --- 6b3. Wire flow knowledge dir and SELF.md refresher ---
	if memFlowsDir != "" {
		chatAgent.FlowKnowledgeDir = memFlowsDir
	}
	if selfMDMemDir != "" {
		chatAgent.SelfMDRefresher = func() {
			selfCfg := buildSelfMDConfig(cfg, selfMDMemDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable)
			// Include flow entries if registry is available
			if chatAgent.FlowRegistry != nil {
				for _, e := range chatAgent.FlowRegistry.Entries() {
					selfCfg.FlowEntries = append(selfCfg.FlowEntries, memory.FlowInfo{
						Name:        strings.TrimSuffix(e.FlowFile, ".yaml"),
						Description: e.Description,
					})
				}
			}
			content := memory.GenerateSelfMD(selfCfg)
			if writeErr := memory.WriteSelfMD(selfMDMemDir, content); writeErr == nil {
				promptBuilder.SelfContent = content
				if cfg.DebugMode {
					fmt.Printf("[SELF.md] Refreshed (%d bytes)\n", len(content))
				}
			}
		}
		// Do an initial refresh now that the registry is loaded
		chatAgent.SelfMDRefresher()
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

	// --- 6e. Initialize context compaction ---
	var compactor *persistentsession.Compactor
	if cfg.AppConfig == nil || cfg.AppConfig.Sessions.Compaction.IsCompactionEnabled() {
		contextWindow := provider.ResolveContextWindowCached(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
		compactor = persistentsession.NewCompactor(contextWindow)
		if cfg.AppConfig != nil {
			compCfg := &cfg.AppConfig.Sessions.Compaction
			compactor.Threshold = compCfg.GetThreshold()
			compactor.PreserveRecent = compCfg.GetPreserveRecent()
		}
		compactor.DebugMode = cfg.DebugMode
		compactor.LLM = makeLLMFunc(llm)
		chatAgent.Compactor = compactor
		if cfg.DebugMode {
			fmt.Printf("Context compaction: enabled (window: %d tokens, threshold: %.0f%%)\n",
				contextWindow, compactor.Threshold*100)
		}
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
	shortID := sess.ID()
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	if isResumed {
		// Friendly welcome back
		fmt.Printf("\n%sHey, welcome back!%s Here's where we left off:\n\n", ColorGreen, ColorReset)
		if cfg.DebugMode {
			fmt.Printf("Session: %s (resumed, %d events)\n", shortID, sess.Events().Len())
		}
	} else {
		// Friendly new session greeting
		fmt.Printf("\n%sHey! I'm Astonish, your AI assistant.%s\n", ColorGreen, ColorReset)
		if cfg.DebugMode {
			fmt.Printf("Session: %s (new)\n", shortID)
		}
	}

	// Show startup notices conversationally
	if len(startupNotices) > 0 {
		for _, notice := range startupNotices {
			fmt.Printf("%s%s%s\n", ColorYellow, notice, ColorReset)
		}
	}

	// --- 10b. Show recent history on resume ---
	if isResumed && sess.Events().Len() > 0 {
		printRecentHistory(sess, 3, ColorCyan, ColorGreen, ColorReset)
		fmt.Printf("What would you like to do next?\n")
	} else {
		fmt.Printf("What can I help you with today?\n")
	}
	fmt.Println()

	// --- 11. Spinner helpers ---
	var spinnerProgram *tea.Program
	var spinnerDone chan struct{}
	lineHasContent := false // tracks whether partial text exists on the current terminal line

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
		// If there's partial text on the current line, move to a new line
		// so the spinner gets its own line and EraseEntireLine won't destroy
		// previously streamed text.
		if lineHasContent {
			fmt.Print("\n")
			lineHasContent = false
		}
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
	indexingWaited := false // ensures we wait for indexing at most once

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
			case input == "/status":
				fmt.Printf("\n%sStatus%s\n", ColorCyan, ColorReset)
				fmt.Printf("  Provider:  %s\n", currentProvider)
				fmt.Printf("  Model:     %s\n", currentModel)
				if compactor != nil {
					est, win := compactor.TokenUsage()
					pct := float64(0)
					if win > 0 {
						pct = float64(est) / float64(win) * 100
					}
					fmt.Printf("  Context:   %d / %d tokens (%.0f%%)\n", est, win, pct)
					if cc := compactor.CompactionCount(); cc > 0 {
						fmt.Printf("  Compacted: %d time(s)\n", cc)
					}
				}
				toolCount := len(internalTools)
				mcpCount := 0
				if len(mcpToolsets) > 0 {
					minCtx := &minimalReadonlyContext{Context: ctx}
					for _, ts := range mcpToolsets {
						if t, err := ts.Tools(minCtx); err == nil {
							mcpCount += len(t)
						}
					}
				}
				if mcpCount > 0 {
					fmt.Printf("  Tools:     %d internal + %d MCP\n", toolCount, mcpCount)
				} else {
					fmt.Printf("  Tools:     %d internal\n", toolCount)
				}
				if memMgr != nil {
					fmt.Printf("  Memory:    active\n")
				} else {
					fmt.Printf("  Memory:    disabled\n")
				}
				if memorySearchAvailable {
					select {
					case <-indexingDone:
						if indexingErr != nil {
							fmt.Printf("  RAG:       error (%v)\n", indexingErr)
						} else {
							fmt.Printf("  RAG:       %d chunks indexed\n", memStore.Count())
						}
					default:
						fmt.Printf("  RAG:       indexing...\n")
					}
				} else {
					fmt.Printf("  RAG:       unavailable\n")
				}
				if chatAgent.FlowRegistry != nil {
					entries := chatAgent.FlowRegistry.Entries()
					fmt.Printf("  Flows:     %d saved\n", len(entries))
				}
				fmt.Printf("  Session:   %s\n\n", shortID)
			case input == "/compact":
				if compactor == nil {
					fmt.Printf("%sCompaction is disabled.%s\n\n", "\033[31m", ColorReset)
				} else {
					est, win := compactor.TokenUsage()
					pct := float64(est) / float64(win) * 100
					fmt.Printf("Context: %d / %d tokens (%.0f%%). ", est, win, pct)
					if est == 0 {
						fmt.Printf("No conversation data yet.\n\n")
					} else if pct < compactor.Threshold*100 {
						fmt.Printf("Under threshold (%.0f%%), no compaction needed.\n\n", compactor.Threshold*100)
					} else {
						fmt.Printf("Compaction will trigger automatically on next message.\n\n")
					}
				}
			case input == "/help":
				fmt.Printf("%sAvailable commands:%s\n", ColorCyan, ColorReset)
				fmt.Println("  /status   - Show current provider, model, tools, and memory status")
				fmt.Println("  /compact  - Show context window usage and compaction status")
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

		// Wait for background indexing to complete before the first agent call.
		// This ensures memory_search and flow matching have indexed data available.
		if !indexingWaited {
			indexingWaited = true
			select {
			case <-indexingDone:
				// Already done — no delay
			default:
				startSpinner("Preparing memory index...")
				<-indexingDone
				stopSpinner()
			}
		}

		startSpinner("Thinking...")

		aiPrefixPrinted := false
		var responseAccum strings.Builder // accumulates full response for [DISTILL:] detection
		waitingForApproval := false
		var approvalOptions []string
		inToolBox := false
		lastEventWasTool := false
		spinnerStopped := false

		// printText prints streaming text directly as it arrives.
		// Handles the Agent: prefix on first output and a single newline
		// separator when transitioning from tool events back to text.
		printText := func(text string) {
			if text == "" {
				return
			}
			if !spinnerStopped {
				stopSpinner()
				spinnerStopped = true
			}
			if !aiPrefixPrinted {
				fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
				aiPrefixPrinted = true
			} else if lastEventWasTool {
				lastEventWasTool = false
			}
			fmt.Print(text)
			lineHasContent = !strings.HasSuffix(text, "\n")
		}

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
								spinnerStopped = true
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
					spinnerStopped = false
				}
			}

			// Process content
			if event.LLMResponse.Content == nil {
				continue
			}

			// Detect tool call/response events and start spinner for tool execution
			hasTool := false
			chunk := ""
			for _, p := range event.LLMResponse.Content.Parts {
				chunk += p.Text
				if p.FunctionCall != nil {
					hasTool = true
					// Start spinner showing which tool is running
					startSpinner(fmt.Sprintf("Running %s...", p.FunctionCall.Name))
					spinnerStopped = false
				}
				if p.FunctionResponse != nil {
					hasTool = true
				}
			}
			if hasTool {
				lastEventWasTool = true
			}

			if chunk != "" {
				// Accumulate full response for [DISTILL:] detection
				responseAccum.WriteString(chunk)

				// Strip [DISTILL: ...] marker from display
				displayChunk := stripDistillMarker(chunk)

				// Detect tool box boundaries
				if strings.Contains(displayChunk, "╭") {
					inToolBox = true
				}

				if inToolBox {
					if !spinnerStopped {
						stopSpinner()
						spinnerStopped = true
					}
					fmt.Print(displayChunk)
					lineHasContent = !strings.HasSuffix(displayChunk, "\n")
					if strings.Contains(displayChunk, "╰") {
						inToolBox = false
					}
				} else {
					printText(displayChunk)
				}
			}
		}

		stopSpinner()

		// Detect [DISTILL: ...] marker in full response and trigger auto-distill
		fullResponse := responseAccum.String()
		if distillDesc := extractDistillMarker(fullResponse); distillDesc != "" {
			if cfg.DebugMode {
				fmt.Printf("\n[Auto-distill] Detected marker: %s\n", distillDesc)
			}
			// Trigger auto-distillation in background
			go func(desc string) {
				if err := chatAgent.AutoDistill(context.Background(), desc); err != nil {
					if cfg.DebugMode {
						fmt.Printf("[Auto-distill] Failed: %v\n", err)
					}
				}
			}(distillDesc)
		}

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
			inToolBox = false
			lastEventWasTool = false
			spinnerStopped = false

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

				// Detect tool call/response events and start spinner for tool execution
				hasTool := false
				chunk := ""
				for _, p := range event.LLMResponse.Content.Parts {
					chunk += p.Text
					if p.FunctionCall != nil {
						hasTool = true
						startSpinner(fmt.Sprintf("Running %s...", p.FunctionCall.Name))
						spinnerStopped = false
					}
					if p.FunctionResponse != nil {
						hasTool = true
					}
				}
				if hasTool {
					lastEventWasTool = true
				}

				if chunk != "" {
					// Accumulate for [DISTILL:] detection
					responseAccum.WriteString(chunk)
					displayChunk := stripDistillMarker(chunk)

					if strings.Contains(displayChunk, "╭") {
						inToolBox = true
					}
					if inToolBox {
						if !spinnerStopped {
							stopSpinner()
							spinnerStopped = true
						}
						fmt.Print(displayChunk)
						if strings.Contains(displayChunk, "╰") {
							inToolBox = false
							aiPrefixPrinted = false
						}
					} else {
						printText(displayChunk)
					}
				}
			}

			stopSpinner()
		}

		// --- Hot-swap: detect provider/model config changes ---
		// If the LLM edited config.yaml to switch provider or model during this turn,
		// re-initialize the LLM so the next turn uses the new one immediately.
		if updatedCfg, loadErr := config.LoadAppConfig(); loadErr == nil {
			newProvider := updatedCfg.General.DefaultProvider
			newModel := updatedCfg.General.DefaultModel
			if newProvider != "" && newModel != "" &&
				(newProvider != currentProvider || newModel != currentModel) {
				newLLM, swapErr := provider.GetProvider(ctx, newProvider, newModel, updatedCfg)
				if swapErr == nil {
					llm = newLLM
					chatAgent.LLM = newLLM
					currentProvider = newProvider
					currentModel = newModel
					cfg.AppConfig = updatedCfg
					// Rebuild distiller LLM closure to use the new provider
					if chatAgent.FlowDistiller != nil {
						chatAgent.FlowDistiller.LLM = makeLLMFunc(newLLM)
					}
					// Update compactor for new model's context window
					if compactor != nil {
						provider.InvalidateContextWindowCache()
						newWindow := provider.ResolveContextWindowCached(ctx, newProvider, newModel, updatedCfg)
						compactor.ContextWindow = newWindow
						compactor.LLM = makeLLMFunc(newLLM)
						if updatedCfg.General.ContextLength > 0 {
							compactor.ContextWindow = updatedCfg.General.ContextLength
						}
					}
					// Refresh SELF.md to reflect new provider/model
					if chatAgent.SelfMDRefresher != nil {
						chatAgent.SelfMDRefresher()
					}
					fmt.Printf("\n%s[Provider switched to %s (model: %s)]%s\n", ColorGreen, newProvider, newModel, ColorReset)
				} else if cfg.DebugMode {
					fmt.Printf("\nWarning: Failed to switch provider to %s/%s: %v\n", newProvider, newModel, swapErr)
				}
			}
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

// distillMarkerRe matches [DISTILL: description] anywhere in text.
var distillMarkerRe = regexp.MustCompile(`\[DISTILL:\s*([^\]]+)\]`)

// extractDistillMarker returns the description from a [DISTILL: ...] marker in text,
// or empty string if no marker found.
func extractDistillMarker(text string) string {
	matches := distillMarkerRe.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

// stripDistillMarker removes [DISTILL: ...] markers from a text chunk.
// This prevents the marker from being displayed to the user.
func stripDistillMarker(chunk string) string {
	return distillMarkerRe.ReplaceAllString(chunk, "")
}

// makeLLMFunc creates a simple LLM call function suitable for FlowDistiller.LLM.
// This is used during hot-swap to rebuild the distiller's closure with a new provider.
func makeLLMFunc(llm model.LLM) func(ctx context.Context, prompt string) (string, error) {
	return func(ctx context.Context, prompt string) (string, error) {
		req := &model.LLMRequest{
			Contents: []*genai.Content{
				{
					Parts: []*genai.Part{{Text: prompt}},
					Role:  "user",
				},
			},
		}
		var text string
		for resp, err := range llm.GenerateContent(ctx, req, false) {
			if err != nil {
				return text, err
			}
			if resp.Content != nil {
				for _, p := range resp.Content.Parts {
					if p.Text != "" {
						text += p.Text
					}
				}
			}
		}
		if text == "" {
			return "", fmt.Errorf("empty response from LLM")
		}
		return text, nil
	}
}

// buildSelfMDConfig constructs a SelfMDConfig from the current runtime state.
func buildSelfMDConfig(
	cfg *ChatConsoleConfig,
	memDir string,
	internalTools []tool.Tool,
	mcpToolsets []tool.Toolset,
	mcpCfg *config.MCPConfig,
	memStore *memory.Store,
	memorySearchAvailable bool,
) *memory.SelfMDConfig {
	selfCfg := &memory.SelfMDConfig{
		ProviderName:  cfg.ProviderName,
		ModelName:     cfg.ModelName,
		MemoryDir:     memDir,
		MemoryEnabled: cfg.AppConfig != nil && cfg.AppConfig.Memory.IsMemoryEnabled(),
		InternalTools: len(internalTools),
	}

	// Config file paths
	if cfgPath, err := config.GetConfigPath(); err == nil {
		selfCfg.ConfigPath = cfgPath
	}
	if mcpPath, err := config.GetMCPConfigPath(); err == nil {
		selfCfg.MCPConfigPath = mcpPath
	}

	// All providers
	if cfg.AppConfig != nil && len(cfg.AppConfig.Providers) > 0 {
		selfCfg.Providers = make(map[string]string, len(cfg.AppConfig.Providers))
		for name, prov := range cfg.AppConfig.Providers {
			provType := config.GetProviderType(name, prov)
			modelName := prov["model"]
			if modelName == "" {
				modelName = "(default)"
			}
			selfCfg.Providers[name] = fmt.Sprintf("%s: %s", provType, modelName)
		}
	}

	// MCP servers
	if mcpCfg != nil {
		for name, srv := range mcpCfg.MCPServers {
			info := memory.MCPServerInfo{
				Name:   name,
				Active: srv.IsEnabled(),
			}
			// Check if keyless standard server
			for _, std := range config.GetStandardServers() {
				if std.ID == name {
					info.Keyless = len(std.EnvVars) == 0
					if std.ID == "playwright" {
						info.Category = "browser"
					}
					break
				}
			}
			selfCfg.MCPServers = append(selfCfg.MCPServers, info)
		}
	}

	// MCP tool count
	if len(mcpToolsets) > 0 {
		minCtx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range mcpToolsets {
			if t, err := ts.Tools(minCtx); err == nil {
				selfCfg.MCPTools += len(t)
			}
		}
	}

	// Flow directory and entries (will be populated after registry init)
	if flowDir, err := flowstore.GetFlowsDir(); err == nil {
		selfCfg.FlowDir = flowDir
	}

	// Embedding info
	if cfg.AppConfig != nil {
		embCfg := cfg.AppConfig.Memory.Embedding
		if embCfg.Provider != "" || embCfg.Model != "" {
			prov := embCfg.Provider
			if prov == "" {
				prov = "auto"
			}
			model := embCfg.Model
			if model == "" {
				model = "(default)"
			}
			selfCfg.EmbeddingInfo = fmt.Sprintf("%s (%s)", prov, model)
		}
	}

	// Chunk count
	if memStore != nil {
		selfCfg.ChunkCount = memStore.Count()
	}

	// Core and knowledge files
	if memDir != "" {
		coreFiles := []string{"MEMORY.md", "INSTRUCTIONS.md", "SELF.md"}
		for _, f := range coreFiles {
			if _, err := os.Stat(filepath.Join(memDir, f)); err == nil {
				selfCfg.CoreFiles = append(selfCfg.CoreFiles, f)
			}
		}

		// Scan for knowledge files (non-core .md files and subdirectories)
		entries, _ := os.ReadDir(memDir)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() && name != "flows" && name != "vectors" {
				selfCfg.KnowledgeFiles = append(selfCfg.KnowledgeFiles, name+"/")
			} else if strings.HasSuffix(name, ".md") && name != "MEMORY.md" && name != "INSTRUCTIONS.md" && name != "SELF.md" {
				selfCfg.KnowledgeFiles = append(selfCfg.KnowledgeFiles, name)
			}
		}
	}

	return selfCfg
}
