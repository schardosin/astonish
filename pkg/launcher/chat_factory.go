package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/provider"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// ChatFactoryConfig holds all inputs needed to build a fully-wired ChatAgent.
type ChatFactoryConfig struct {
	AppConfig    *config.AppConfig
	ProviderName string
	ModelName    string
	DebugMode    bool
	AutoApprove  bool
	WorkspaceDir string
}

// ChatFactoryResult holds everything produced by the factory.
// Callers (console, channel manager) unpack what they need.
type ChatFactoryResult struct {
	ChatAgent             *agent.ChatAgent
	LLM                   model.LLM
	ProviderName          string
	ModelName             string
	Compactor             *persistentsession.Compactor
	InternalTools         []tool.Tool
	MCPToolsets           []tool.Toolset
	MemoryManager         *memory.Manager
	MemoryStore           *memory.Store
	MemorySearchAvailable bool
	IndexingDone          chan struct{}
	IndexingErr           *error
	SessionService        session.Service
	StartupNotices        []string
	PromptBuilder         *agent.SystemPromptBuilder
	CredentialStore       *credentials.Store // Encrypted credential store (nil if unavailable)

	// Cleanup aggregates all deferred cleanups (embedder, MCP, file watcher).
	// The caller must call this when the ChatAgent is no longer needed.
	Cleanup func()
}

// NewWiredChatAgent creates a fully-wired ChatAgent ready for use by any caller
// (interactive console, channel manager, daemon, etc.).
//
// It performs the entire initialization sequence: LLM provider, internal tools,
// memory (store, indexer, memory tools), MCP toolsets, session service, system
// prompt, flow registry, flow distiller, compactor, and knowledge search.
//
// The returned ChatFactoryResult contains the ChatAgent and all auxiliary objects
// that callers may need. Callers MUST call result.Cleanup() when done.
func NewWiredChatAgent(ctx context.Context, cfg *ChatFactoryConfig) (*ChatFactoryResult, error) {
	var cleanups []func()
	cleanup := func() {
		// Run cleanups in reverse order (LIFO, like defer)
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// --- 0. Initialize credential store (must happen before LLM — secrets are scrubbed from config after migration) ---
	var credStore *credentials.Store
	configDir, configDirErr := config.GetConfigDir()
	if configDirErr == nil {
		cs, csErr := credentials.Open(configDir)
		if csErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to open credential store: %v\n", csErr)
			}
		} else {
			credStore = cs
			tools.SetCredentialStore(cs)

			// Wire credential store into config package for standard server lookups
			config.SetInstalledSecretGetter(cs.GetSecret)

			// Wire credential store into API handlers
			api.SetAPICredentialStore(cs)

			// Auto-migrate secrets from config.yaml to the encrypted store (one-time)
			migrated, migrateErr := credentials.MigrateFromConfig(cs, cfg.AppConfig, nil)
			if migrateErr != nil {
				if cfg.DebugMode {
					fmt.Printf("Warning: Credential migration error: %v\n", migrateErr)
				}
			} else if migrated > 0 {
				// Re-save config.yaml with secrets scrubbed
				if saveErr := config.SaveAppConfig(cfg.AppConfig); saveErr != nil {
					if cfg.DebugMode {
						fmt.Printf("Warning: Failed to save scrubbed config: %v\n", saveErr)
					}
				} else if cfg.DebugMode {
					fmt.Printf("Migrated %d secrets from config.yaml to encrypted store\n", migrated)
				}
			}

			// Inject secrets back into config map so GetProvider() can read them.
			// This is needed because migration scrubs secrets from the config map,
			// and some providers (e.g. openai_compat) only read from the config map
			// with no env var fallback.
			config.InjectProviderSecretsToConfig(cfg.AppConfig, cs.GetSecret)

			// Setup provider env vars from credential store
			config.SetupAllProviderEnvFromStore(cfg.AppConfig, cs.GetSecret)
		}
	}
	// Fallback: if credential store unavailable, use legacy env setup
	if credStore == nil {
		config.SetupAllProviderEnv(cfg.AppConfig)
	}

	// --- 1. Initialize LLM ---
	if cfg.DebugMode {
		fmt.Println("Initializing LLM provider...")
	}
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize provider '%s' with model '%s': %w",
			cfg.ProviderName, cfg.ModelName, err)
	}
	if cfg.DebugMode {
		fmt.Printf("Provider initialized: %s (model: %s)\n", cfg.ProviderName, cfg.ModelName)
	}

	// --- 2. Initialize internal tools ---
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize internal tools: %w", err)
	}

	// Register credential tools (gracefully handle "store not available" like scheduler)
	credTools, credErr := tools.GetCredentialTools()
	if credErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create credential tools: %v\n", credErr)
		}
	} else {
		internalTools = append(internalTools, credTools...)
	}

	// --- 2b. Initialize memory manager ---
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
	indexingDone := make(chan struct{})
	var indexingErr error

	if memMgr != nil && cfg.AppConfig != nil && cfg.AppConfig.Memory.IsMemoryEnabled() {
		memCfg := &cfg.AppConfig.Memory

		memDir, mdErr := config.GetMemoryDir(memCfg)
		vecDir, vdErr := config.GetVectorDir(memCfg)

		if mdErr == nil && vdErr == nil {
			if err := os.MkdirAll(memDir, 0755); err != nil {
				if cfg.DebugMode {
					fmt.Printf("Warning: Failed to create memory directory: %v\n", err)
				}
			} else {
				var embGetSecret config.SecretGetter
				if credStore != nil {
					embGetSecret = credStore.GetSecret
				}
				embResult, embErr := memory.ResolveEmbeddingFunc(cfg.AppConfig, memCfg, cfg.DebugMode, embGetSecret)
				if embErr != nil {
					fmt.Printf("Note: Semantic memory unavailable (%v)\n", embErr)
				} else {
					if embResult.Cleanup != nil {
						cleanups = append(cleanups, func() { _ = embResult.Cleanup() })
					}

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
					memIndexer = memory.NewIndexer(store, storeCfg, false)
					store.SetIndexer(memIndexer)
					memorySearchAvailable = true

					// Perform initial indexing in background
					go func() {
						defer close(indexingDone)
						defer func() {
							if r := recover(); r != nil {
								indexingErr = fmt.Errorf("indexing panicked: %v", r)
							}
						}()
						indexingErr = memIndexer.IndexAll(context.Background())
					}()

						// Start file watcher in background
						if memCfg.IsWatchEnabled() {
							debounceMs := memCfg.Sync.DebounceMs
							if debounceMs <= 0 {
								debounceMs = 1500
							}
							watchCtx, watchCancel := context.WithCancel(context.Background())
							go func() {
								if wErr := memIndexer.WatchAndSync(watchCtx, debounceMs); wErr != nil {
									if cfg.DebugMode {
										fmt.Printf("Warning: Memory file watcher error: %v\n", wErr)
									}
								}
							}()
							cleanups = append(cleanups, watchCancel)
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

	// --- 2d. Initialize scheduler tools ---
	// These are always registered — they gracefully handle "scheduler not available"
	// when the daemon isn't running or scheduler isn't enabled.
	schedTools, schedErr := tools.GetSchedulerTools()
	if schedErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create scheduler tools: %v\n", schedErr)
		}
	} else {
		internalTools = append(internalTools, schedTools...)
	}

	// --- 2e. Initialize process management tools ---
	processTools, procErr := tools.GetProcessTools()
	if procErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create process tools: %v\n", procErr)
		}
	} else {
		internalTools = append(internalTools, processTools...)
	}
	cleanups = append(cleanups, tools.CleanupProcessManager)

	// --- 3. Load MCP tools from cache (lazy) ---
	mcpCfg, _ := config.LoadMCPConfig()
	var lazyToolsets []*agent.LazyMCPToolset
	var mcpToolsets []tool.Toolset

	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
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

		for _, lt := range lazyToolsets {
			mcpToolsets = append(mcpToolsets, agent.NewSanitizedToolset(lt, cfg.DebugMode))
		}
	}
	cleanups = append(cleanups, func() {
		for _, lt := range lazyToolsets {
			lt.Cleanup()
		}
	})

	// Track startup notices
	var startupNotices []string

	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 && len(lazyToolsets) == 0 {
		startupNotices = append(startupNotices,
			"MCP servers are configured but no tools are cached yet. "+
				"Run 'astonish studio' or 'astonish tools refresh' to set them up.")
	}

	// --- 4. Create session service ---
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
			cleanup()
			return nil, fmt.Errorf("failed to resolve sessions directory: %w", dirErr)
		}
		fileStore, fsErr := persistentsession.NewFileStore(sessDir)
		if fsErr != nil {
			cleanup()
			return nil, fmt.Errorf("failed to create file session store: %w", fsErr)
		}
		sessionService = fileStore
		if cfg.DebugMode {
			fmt.Printf("Session storage: file (%s)\n", sessDir)
		}
	}

	// --- 4b. Enforce tool count limit ---
	maxTools := 128
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
		mcpBudget := maxTools - len(internalTools)
		if mcpBudget < 0 {
			mcpBudget = 0
		}

		priorityServers := map[string]bool{}
		if _, serverName, _ := api.IsWebSearchConfigured(); serverName != "" {
			priorityServers[serverName] = true
		}
		if _, serverName, _ := api.IsWebExtractConfigured(); serverName != "" {
			priorityServers[serverName] = true
		}
		for _, srv := range config.GetStandardServers() {
			if len(srv.EnvVars) == 0 {
				priorityServers[srv.ID] = true
			}
		}

		var trimmedToolsets []tool.Toolset
		remaining := mcpBudget
		minCtx := &minimalReadonlyContext{Context: ctx}

		// First pass: priority toolsets
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

		// Second pass: fill remaining budget
		for _, ts := range mcpToolsets {
			if remaining <= 0 {
				break
			}
			if priorityServers[ts.Name()] {
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

	// Check web tool availability
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

	// Check browser availability
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

	// Load INSTRUCTIONS.md
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
	var selfMDMemDir string
	if memDir != "" {
		selfMDMemDir = memDir

		selfCfg := factoryBuildSelfMDConfig(cfg, memDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable)
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

	// Reconcile flow knowledge docs
	var memFlowsDir string
	flowsDir, _ := flowstore.GetFlowsDir()
	if memDir != "" && flowsDir != "" {
		memFlowsDir = filepath.Join(memDir, "flows")
	}

	// Load custom prompt from app config
	if cfg.AppConfig != nil && cfg.AppConfig.Chat.SystemPrompt != "" {
		promptBuilder.CustomPrompt = cfg.AppConfig.Chat.SystemPrompt
	}

	// --- 6. Create ChatAgent ---
	chatAgent := agent.NewChatAgent(
		llm, internalTools, mcpToolsets, sessionService,
		promptBuilder, cfg.DebugMode, cfg.AutoApprove,
	)

	// Wire credential redactor to ChatAgent and session service
	if credStore != nil {
		redactor := credStore.Redactor()
		chatAgent.Redactor = redactor
		// Also wire to file-based session store for transcript redaction
		if fs, ok := sessionService.(*persistentsession.FileStore); ok {
			fs.RedactFunc = redactor.Redact
		}
	}

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
			selfCfg := factoryBuildSelfMDConfig(cfg, selfMDMemDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable)
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
		chatAgent.SelfMDRefresher()
	}

	// --- 6c. Initialize Flow Distiller ---
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

	return &ChatFactoryResult{
		ChatAgent:             chatAgent,
		LLM:                   llm,
		ProviderName:          cfg.ProviderName,
		ModelName:             cfg.ModelName,
		Compactor:             compactor,
		InternalTools:         internalTools,
		MCPToolsets:           mcpToolsets,
		MemoryManager:         memMgr,
		MemoryStore:           memStore,
		MemorySearchAvailable: memorySearchAvailable,
		IndexingDone:          indexingDone,
		IndexingErr:           &indexingErr,
		SessionService:        sessionService,
		StartupNotices:        startupNotices,
		PromptBuilder:         promptBuilder,
		CredentialStore:       credStore,
		Cleanup:               cleanup,
	}, nil
}

// MakeLLMFunc creates a simple prompt->response function from an LLM.
// Exported for use by the channel manager and other callers.
func MakeLLMFunc(llm model.LLM) func(ctx context.Context, prompt string) (string, error) {
	return makeLLMFunc(llm)
}

// factoryBuildSelfMDConfig constructs a SelfMDConfig from factory config state.
// This mirrors buildSelfMDConfig in chat_console.go but uses ChatFactoryConfig.
func factoryBuildSelfMDConfig(
	cfg *ChatFactoryConfig,
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

	if cfgPath, err := config.GetConfigPath(); err == nil {
		selfCfg.ConfigPath = cfgPath
	}
	if mcpPath, err := config.GetMCPConfigPath(); err == nil {
		selfCfg.MCPConfigPath = mcpPath
	}

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

	if mcpCfg != nil {
		for name, srv := range mcpCfg.MCPServers {
			info := memory.MCPServerInfo{
				Name:   name,
				Active: srv.IsEnabled(),
			}
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

	if len(mcpToolsets) > 0 {
		minCtx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range mcpToolsets {
			if t, err := ts.Tools(minCtx); err == nil {
				selfCfg.MCPTools += len(t)
			}
		}
	}

	if flowDir, err := flowstore.GetFlowsDir(); err == nil {
		selfCfg.FlowDir = flowDir
	}

	if cfg.AppConfig != nil {
		embCfg := cfg.AppConfig.Memory.Embedding
		if embCfg.Provider != "" || embCfg.Model != "" {
			prov := embCfg.Provider
			if prov == "" {
				prov = "auto"
			}
			mdl := embCfg.Model
			if mdl == "" {
				mdl = "(default)"
			}
			selfCfg.EmbeddingInfo = fmt.Sprintf("%s (%s)", prov, mdl)
		}
	}

	if memStore != nil {
		selfCfg.ChunkCount = memStore.Count()
	}

	if memDir != "" {
		coreFiles := []string{"MEMORY.md", "INSTRUCTIONS.md", "SELF.md"}
		for _, f := range coreFiles {
			if _, err := os.Stat(filepath.Join(memDir, f)); err == nil {
				selfCfg.CoreFiles = append(selfCfg.CoreFiles, f)
			}
		}

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
