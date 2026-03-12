package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	emailpkg "github.com/schardosin/astonish/pkg/email"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/provider"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/skills"
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
	IsDaemon     bool // When true, always run indexing/watchers (we ARE the daemon).

	// SessionStore is an optional pre-created FileStore for session persistence.
	// When set, the factory reuses this store instead of creating a new one.
	// This ensures a single FileStore instance across the daemon process,
	// preventing index.json race conditions between fleet sessions and
	// sub-agent sessions.
	SessionStore *persistentsession.FileStore
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
		provider.SetDebugMode(true)
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
				// Pre-sync skills to memory directory BEFORE indexing starts,
				// so skill files are included in the initial IndexAll() walk.
				if cfg.AppConfig.Skills.IsSkillsEnabled() {
					skillsCfg := &cfg.AppConfig.Skills
					workDir := cfg.WorkspaceDir
					if workDir == "" {
						workDir, _ = os.Getwd()
					}
					preSkills, psErr := skills.LoadSkills(
						skillsCfg.GetUserSkillsDir(),
						skillsCfg.ExtraDirs,
						workDir,
						skillsCfg.Allowlist,
					)
					if psErr == nil && len(preSkills) > 0 {
						if syncErr := skills.SyncSkillsToMemory(preSkills, memDir); syncErr != nil {
							if cfg.DebugMode {
								fmt.Printf("Warning: Failed to pre-sync skills to memory: %v\n", syncErr)
							}
						}
					}
				}

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

						// If the daemon is already running (and we're NOT the daemon),
						// it maintains the vector index via its own Indexer + WatchAndSync.
						// We can skip the expensive IndexAll() and file watcher — the
						// persisted DB on disk is already up to date.
						daemonActive := !cfg.IsDaemon && isDaemonRunning()
						if daemonActive {
							if cfg.DebugMode {
								fmt.Println("[Memory] Daemon is running — skipping IndexAll and file watcher")
							}
							close(indexingDone)
						} else {
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
						}

						// Start file watcher in background (only when daemon is NOT running)
						if !daemonActive && memCfg.IsWatchEnabled() {
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

							// Start skills directory watcher — watches skill source dirs
							// and re-syncs to memory/skills/ when SKILL.md files change.
							// The memory watcher above then picks up those changes.
							if cfg.AppConfig.Skills.IsSkillsEnabled() {
								skillsCfg := &cfg.AppConfig.Skills
								skillWorkDir := cfg.WorkspaceDir
								if skillWorkDir == "" {
									skillWorkDir, _ = os.Getwd()
								}
								skillWatchCtx, skillWatchCancel := context.WithCancel(context.Background())
								go func() {
									if wErr := skills.WatchSkillDirs(skillWatchCtx, skills.WatcherConfig{
										UserDir:      skillsCfg.GetUserSkillsDir(),
										ExtraDirs:    skillsCfg.ExtraDirs,
										WorkspaceDir: skillWorkDir,
										Allowlist:    skillsCfg.Allowlist,
										MemoryDir:    memDir,
										DebounceMs:   debounceMs + 500, // Slightly longer than memory watcher
										DebugMode:    cfg.DebugMode,
									}); wErr != nil {
										if cfg.DebugMode {
											fmt.Printf("Warning: Skills watcher error: %v\n", wErr)
										}
									}
								}()
								cleanups = append(cleanups, skillWatchCancel)
							}
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

	// --- 2d-ii. Initialize distill_flow tool ---
	// Registered alongside scheduler tools — enables auto-distillation when
	// the user wants to schedule a task as "routine" but no flow exists yet.
	distillTools, distillErr := tools.GetDistillTools()
	if distillErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create distill tools: %v\n", distillErr)
		}
	} else {
		internalTools = append(internalTools, distillTools...)
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

	// --- 2f. Initialize browser automation tools ---
	browserCfg := browserConfigFromApp(cfg.AppConfig)
	browserMgr := browser.NewManager(browserCfg)
	browserTools, browserErr := tools.GetBrowserTools(browserMgr)
	if browserErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create browser tools: %v\n", browserErr)
		}
	} else {
		internalTools = append(internalTools, browserTools...)
	}
	cleanups = append(cleanups, browserMgr.Cleanup)

	// --- 2f-ii. Initialize email tools ---
	// Create the email client here (before GetEmailTools) so tools are available
	// in both console and daemon modes. The daemon's initEmailTools becomes a
	// no-op when the factory has already set the client.
	if cfg.AppConfig != nil {
		emailCfg := cfg.AppConfig.Channels.Email
		emailPassword := emailCfg.Password
		if emailPassword == "" && credStore != nil {
			emailPassword = credStore.GetSecret("channels.email.password")
		}
		if emailPassword != "" && emailCfg.IMAPServer != "" &&
			emailCfg.SMTPServer != "" && emailCfg.Address != "" {
			client, clientErr := emailpkg.NewClient(&emailpkg.Config{
				Provider:     emailCfg.Provider,
				IMAPServer:   emailCfg.IMAPServer,
				SMTPServer:   emailCfg.SMTPServer,
				Address:      emailCfg.Address,
				Username:     emailCfg.Username,
				Password:     emailPassword,
				Folder:       emailCfg.Folder,
				MaxBodyChars: emailCfg.MaxBodyChars,
			})
			if clientErr != nil {
				if cfg.DebugMode {
					fmt.Printf("Warning: Failed to create email client: %v\n", clientErr)
				}
			} else {
				tools.SetEmailClient(client)
			}
		}
	}
	emailTools, emailErr := tools.GetEmailTools()
	if emailErr != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create email tools: %v\n", emailErr)
		}
	} else if len(emailTools) > 0 {
		internalTools = append(internalTools, emailTools...)
	}

	// --- 2g. Sub-agent delegation tool ---
	var subAgentMgr *agent.SubAgentManager
	var fleetOnlyTools []tool.Tool // fleet-only tools (run_fleet_phase), assigned to SubAgentManager.FleetTools
	if cfg.AppConfig.SubAgents.IsSubAgentsEnabled() {
		delegateTool, delegateErr := tools.GetDelegateTasksTool()
		if delegateErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create delegate_tasks tool: %v\n", delegateErr)
			}
		} else {
			internalTools = append(internalTools, delegateTool)

			subAgentCfg := agent.SubAgentConfig{
				MaxDepth:      cfg.AppConfig.SubAgents.MaxDepth,
				MaxConcurrent: cfg.AppConfig.SubAgents.MaxConcurrent,
				TaskTimeout:   cfg.AppConfig.SubAgents.TaskTimeout(),
			}
			subAgentMgr = agent.NewSubAgentManager(subAgentCfg)
		}
	}

	// --- 2h. Initialize skills system ---
	var loadedSkills []skills.Skill
	var skillIndex string
	if cfg.AppConfig != nil && cfg.AppConfig.Skills.IsSkillsEnabled() {
		skillsCfg := &cfg.AppConfig.Skills
		workDir := cfg.WorkspaceDir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		var skillErr error
		loadedSkills, skillErr = skills.LoadSkills(
			skillsCfg.GetUserSkillsDir(),
			skillsCfg.ExtraDirs,
			workDir,
			skillsCfg.Allowlist,
		)
		if skillErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to load skills: %v\n", skillErr)
			}
		} else {
			eligible := skills.FilterEligible(loadedSkills)
			if len(eligible) > 0 {
				// Build lightweight index for system prompt
				skillIndex = skills.BuildSkillIndex(loadedSkills)

				// Create skill_lookup tool
				skillTool, stErr := tools.NewSkillLookupTool(loadedSkills)
				if stErr != nil {
					if cfg.DebugMode {
						fmt.Printf("Warning: Failed to create skill_lookup tool: %v\n", stErr)
					}
				} else {
					internalTools = append(internalTools, skillTool)
				}
			}
			if cfg.DebugMode {
				fmt.Printf("Skills loaded: %d total, %d eligible\n", len(loadedSkills), len(eligible))
			}
		}
	}

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
	if cfg.SessionStore != nil {
		// Reuse the pre-created store (shared with fleet sessions in the daemon).
		sessionService = cfg.SessionStore
		if cfg.DebugMode {
			fmt.Println("Session storage: file (shared store)")
		}
	} else if cfg.AppConfig != nil && cfg.AppConfig.Sessions.Storage == "memory" {
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

	// Check browser availability (native browser tools)
	if browserErr == nil && len(browserTools) > 0 {
		promptBuilder.BrowserAvailable = true
	}

	// Populate agent identity from config (for web portal interactions)
	if cfg.AppConfig != nil && cfg.AppConfig.AgentIdentity.IsConfigured() {
		id := &cfg.AppConfig.AgentIdentity
		identityEmail := id.Email
		// Fall back to the channel email address if identity email is not set
		if identityEmail == "" && cfg.AppConfig.Channels.Email.Address != "" {
			identityEmail = cfg.AppConfig.Channels.Email.Address
		}
		promptBuilder.Identity = &agent.AgentIdentity{
			Name:     id.Name,
			Username: id.Username,
			Email:    identityEmail,
			Bio:      id.Bio,
			Website:  id.Website,
			Locale:   id.Locale,
			Timezone: id.Timezone,
		}
	}

	// Check memory search availability
	if memorySearchAvailable {
		promptBuilder.MemorySearchAvailable = true
	}

	// Set skill index for system prompt
	if skillIndex != "" {
		promptBuilder.SkillIndex = skillIndex
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

		selfCfg := factoryBuildSelfMDConfig(cfg, memDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable, loadedSkills)
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

	// --- 5b. Initialize fleet registry ---
	fleetsDir, flErr := config.GetFleetsDir()
	if flErr == nil {
		// Ensure bundled fleets exist on disk
		fWritten, fEnsErr := fleet.EnsureBundled(fleetsDir)
		if fEnsErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to bootstrap bundled fleets: %v\n", fEnsErr)
			}
		} else if fWritten > 0 && cfg.DebugMode {
			fmt.Printf("Bootstrapped %d bundled fleets to %s\n", fWritten, fleetsDir)
		}

		fleetReg, fRegErr := fleet.NewRegistry(fleetsDir)
		if fRegErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to load fleet registry: %v\n", fRegErr)
			}
		} else {
			// Wire fleet registry to API handlers
			api.SetFleetRegistry(fleetReg)

			// Wire registry to fleet execution tool
			tools.SetFleetRegistry(fleetReg)

			// Set up env vars declared by delegate configs (e.g. BIFROST_API_KEY for OpenCode).
			// This must happen before any fleet tool runs so the delegate subprocess inherits them.
			delegateEnvNames := fleet.CollectDelegateEnvVars(fleetReg.AllFleets())
			if len(delegateEnvNames) > 0 {
				var getSecret config.SecretGetter
				if credStore != nil {
					getSecret = credStore.GetSecret
				}
				config.SetupDelegateEnv(delegateEnvNames, getSecret)
			}

			// Register fleet tools (requires sub-agents to be enabled)
			if cfg.AppConfig.SubAgents.IsSubAgentsEnabled() {
				// Build fleet-only tools (opencode delegate). These are NOT added to
				// internalTools (so the main agent can't call them). Instead they go
				// on SubAgentManager.FleetTools, accessible only to fleet
				// worker agents via their explicit tool filter.
				var ftErr error
				fleetOnlyTools, ftErr = tools.GetFleetTools()
				if ftErr != nil {
					if cfg.DebugMode {
						fmt.Printf("Warning: Failed to create fleet internal tools: %v\n", ftErr)
					}
				}
			}

			// Build fleet awareness section for system prompt
			if fleetReg.Count() > 0 {
				summaries := fleetReg.ListFleets()
				promptBuilder.FleetSection = fleet.BuildSystemPromptSection(
					summaries,
					func(key string) (*fleet.FleetConfig, bool) {
						return fleetReg.GetFleet(key)
					},
				)
				if cfg.DebugMode {
					fmt.Printf("Fleet awareness: %d fleet(s)\n", fleetReg.Count())
				}
			}
		}

		// Initialize fleet plan registry
		fleetPlansDir, fpErr := config.GetFleetPlansDir()
		if fpErr == nil {
			planReg, prErr := fleet.NewPlanRegistry(fleetPlansDir)
			if prErr != nil {
				if cfg.DebugMode {
					fmt.Printf("Warning: Failed to load fleet plan registry: %v\n", prErr)
				}
			} else {
				// Wire plan registry to API handlers
				api.SetFleetPlanRegistry(planReg)
				// Wire plan registry to the save_fleet_plan tool
				tools.SetFleetPlanRegistry(planReg)
				if cfg.DebugMode {
					fmt.Printf("Fleet plans: %d loaded from %s\n", planReg.Count(), fleetPlansDir)
				}
			}
		}

		// Register save_fleet_plan tool (available to main chat agent)
		fleetPlanTools, fptErr := tools.GetFleetPlanTools()
		if fptErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create fleet plan tools: %v\n", fptErr)
			}
		} else {
			internalTools = append(internalTools, fleetPlanTools...)
		}

		// Register validate_fleet_plan tool (available to main chat agent)
		fleetPlanValidateTools, fpvErr := tools.GetFleetPlanValidateTools()
		if fpvErr != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to create fleet plan validate tools: %v\n", fpvErr)
			}
		} else {
			internalTools = append(internalTools, fleetPlanValidateTools...)
		}
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

	// Wire SubAgentManager — deferred until all tools and LLM are known
	if subAgentMgr != nil {
		subAgentMgr.LLM = llm
		subAgentMgr.Tools = internalTools
		subAgentMgr.FleetTools = fleetOnlyTools
		subAgentMgr.Toolsets = mcpToolsets
		subAgentMgr.SessionService = sessionService
		subAgentMgr.MemoryManager = memMgr
		subAgentMgr.AppName = "astonish"
		subAgentMgr.UserID = "console_user"
		tools.SetSubAgentManager(subAgentMgr)
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
			selfCfg := factoryBuildSelfMDConfig(cfg, selfMDMemDir, internalTools, mcpToolsets, mcpCfg, memStore, memorySearchAvailable, loadedSkills)
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
		if subAgentMgr != nil {
			subAgentMgr.Compactor = compactor
		}
		if cfg.DebugMode {
			fmt.Printf("Context compaction: enabled (window: %d tokens, threshold: %.0f%%)\n",
				contextWindow, compactor.Threshold*100)
		}
	}

	// --- 6e-bis. Wire OpenCode response summarizer ---
	// Uses the same LLM function as the compactor. When set, verbose OpenCode
	// outputs (>4KB) are replaced with concise summaries before they enter the
	// calling agent's ADK session, keeping context lean. OpenCode retains full
	// context internally via session_id continuation.
	tools.SetOpenCodeSummarizer(makeLLMFunc(llm))

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
	loadedSkills []skills.Skill,
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
					info.Category = std.Category
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
			if e.IsDir() && name != "flows" && name != "vectors" && name != "skills" {
				selfCfg.KnowledgeFiles = append(selfCfg.KnowledgeFiles, name+"/")
			} else if strings.HasSuffix(name, ".md") && name != "MEMORY.md" && name != "INSTRUCTIONS.md" && name != "SELF.md" {
				selfCfg.KnowledgeFiles = append(selfCfg.KnowledgeFiles, name)
			}
		}
	}

	// Sub-agents
	if cfg.AppConfig != nil {
		selfCfg.SubAgentsEnabled = cfg.AppConfig.SubAgents.IsSubAgentsEnabled()
	}

	// Channels
	if cfg.AppConfig != nil {
		selfCfg.ChannelsEnabled = cfg.AppConfig.Channels.IsChannelsEnabled()
		selfCfg.TelegramEnabled = cfg.AppConfig.Channels.Telegram.IsTelegramEnabled()
		selfCfg.EmailEnabled = cfg.AppConfig.Channels.Email.IsEmailEnabled()
		selfCfg.EmailAddress = cfg.AppConfig.Channels.Email.Address

		// Email tools are available when IMAP/SMTP credentials are configured,
		// regardless of whether the channel is enabled.
		if cfg.AppConfig.Channels.Email.Address != "" &&
			cfg.AppConfig.Channels.Email.IMAPServer != "" &&
			cfg.AppConfig.Channels.Email.SMTPServer != "" {
			selfCfg.EmailToolsAvail = true
		}
	}

	// Skills
	if len(loadedSkills) > 0 {
		eligible := skills.FilterEligible(loadedSkills)
		for _, s := range eligible {
			selfCfg.SkillNames = append(selfCfg.SkillNames, s.Name)
		}
	}

	// Browser handoff
	for _, t := range internalTools {
		if t.Name() == "browser_request_human" {
			selfCfg.HandoffAvailable = true
			break
		}
	}

	// Agent identity
	if cfg.AppConfig != nil && cfg.AppConfig.AgentIdentity.IsConfigured() {
		id := &cfg.AppConfig.AgentIdentity
		selfCfg.IdentityConfigured = true
		selfCfg.IdentityName = id.Name
		selfCfg.IdentityUsername = id.Username
		selfCfg.IdentityEmail = id.Email
		// Fall back to the channel email address if identity email is not set
		if selfCfg.IdentityEmail == "" && cfg.AppConfig.Channels.Email.Address != "" {
			selfCfg.IdentityEmail = cfg.AppConfig.Channels.Email.Address
		}
	}

	return selfCfg
}

// browserConfigFromApp builds a browser.BrowserConfig by merging user settings
// from AppConfig with sensible defaults from browser.DefaultConfig().
func browserConfigFromApp(appCfg *config.AppConfig) browser.BrowserConfig {
	if appCfg == nil {
		return browser.DefaultConfig()
	}
	b := &appCfg.Browser
	return browser.OverrideConfig(browser.ConfigOverrides{
		Headless:            b.Headless,
		ViewportWidth:       b.ViewportWidth,
		ViewportHeight:      b.ViewportHeight,
		NoSandbox:           b.NoSandbox,
		ChromePath:          b.ChromePath,
		UserDataDir:         b.UserDataDir,
		NavigationTimeout:   b.NavigationTimeout,
		Proxy:               b.Proxy,
		RemoteCDPURL:        b.RemoteCDPURL,
		FingerprintSeed:     b.FingerprintSeed,
		FingerprintPlatform: b.FingerprintPlatform,
		HandoffBindAddress:  b.HandoffBindAddress,
		HandoffPort:         b.HandoffPort,
	})
}

// isDaemonRunning checks whether the astonish daemon is currently running
// by reading the PID file and probing the process with signal 0.
// This avoids importing the daemon package (which would create an import cycle).
func isDaemonRunning() bool {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return false
	}
	pidPath := filepath.Join(configDir, "astonish", "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return isProcessRunning(pid)
}
