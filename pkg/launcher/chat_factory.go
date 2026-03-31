package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	chromem "github.com/philippgille/chromem-go"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	adrill "github.com/schardosin/astonish/pkg/drill"
	emailpkg "github.com/schardosin/astonish/pkg/email"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/sandbox"
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

	// ShutdownSandbox stops sandbox containers without destroying them.
	// Used during graceful daemon shutdown to preserve containers across restarts.
	// Nil when sandbox is not enabled.
	ShutdownSandbox func()
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
				slog.Warn("failed to open credential store", "error", csErr)
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
					slog.Warn("credential migration error", "error", migrateErr)
				}
			} else if migrated > 0 {
				// Re-save config.yaml with secrets scrubbed
				if saveErr := config.SaveAppConfig(cfg.AppConfig); saveErr != nil {
					if cfg.DebugMode {
						slog.Warn("failed to save scrubbed config", "error", saveErr)
					}
				} else if cfg.DebugMode {
					slog.Debug("migrated secrets to encrypted store", "component", "chat-factory", "count", migrated)
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
		slog.Debug("initializing LLM provider", "component", "chat-factory")
	}
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize provider '%s' with model '%s': %w",
			cfg.ProviderName, cfg.ModelName, err)
	}
	if cfg.DebugMode {
		slog.Debug("provider initialized", "component", "chat-factory", "provider", cfg.ProviderName, "model", cfg.ModelName)
	}

	// --- 2. Initialize internal tools ---
	// Tools are organized into groups. The main thread gets only essential tools
	// (read, write, edit, shell, search, memory, delegate). All other tools are
	// available to sub-agents via named groups in delegate_tasks.
	coreTools, err := tools.GetInternalTools()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize internal tools: %w", err)
	}

	// Credential tools → deferred category
	var credToolsSlice []tool.Tool
	credTools, credErr := tools.GetCredentialTools()
	if credErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create credential tools", "error", credErr)
		}
	} else {
		credToolsSlice = credTools
	}

	// --- 2b. Initialize memory manager ---
	memMgr, memErr := memory.NewManager("", cfg.DebugMode)
	if memErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to initialize memory manager", "error", memErr)
		}
	}

	// --- 2c. Initialize semantic memory (vector store, indexer) ---
	var memStore *memory.Store
	var memIndexer *memory.Indexer
	var memEmbeddingFunc chromem.EmbeddingFunc // saved for ToolIndex creation
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
					slog.Warn("failed to create memory directory", "error", err)
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
								slog.Warn("failed to pre-sync skills to memory", "error", syncErr)
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
					memEmbeddingFunc = embResult.EmbeddingFunc

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
							slog.Warn("failed to create memory store", "error", storeErr)
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
								slog.Debug("daemon is running, skipping IndexAll and file watcher", "component", "memory")
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
										slog.Warn("memory file watcher error", "error", wErr)
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
											slog.Warn("skills watcher error", "error", wErr)
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

	// Create memory tools → core category (always available)
	if memMgr != nil {
		var saveStore tools.MemorySaveStore
		if memStore != nil {
			saveStore = memStore
		}
		memorySaveTool, msErr := tools.NewMemorySaveTool(memMgr, saveStore)
		if msErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create memory_save tool", "error", msErr)
			}
		} else {
			coreTools = append(coreTools, memorySaveTool)
		}
	}

	if memorySearchAvailable && memStore != nil {
		searchTool, searchErr := tools.NewMemorySearchTool(memStore)
		if searchErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create memory_search tool", "error", searchErr)
			}
		} else {
			coreTools = append(coreTools, searchTool)
		}

		memDir, err := config.GetMemoryDir(&cfg.AppConfig.Memory)
		if err != nil {
			slog.Warn("failed to get memory directory", "error", err)
		}
		getTool, getErr := tools.NewMemoryGetTool(memDir)
		if getErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create memory_get tool", "error", getErr)
			}
		} else {
			coreTools = append(coreTools, getTool)
		}
	}

	// --- 2d. Initialize scheduler tools → deferred category ---
	var schedToolsSlice []tool.Tool
	schedTools, schedErr := tools.GetSchedulerTools()
	if schedErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create scheduler tools", "error", schedErr)
		}
	} else {
		schedToolsSlice = schedTools
	}

	// --- 2d-ii. Initialize distill_flow tool → deferred category ---
	var distillToolsSlice []tool.Tool
	distillTools, distillErr := tools.GetDistillTools()
	if distillErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create distill tools", "error", distillErr)
		}
	} else {
		distillToolsSlice = distillTools
	}

	// --- 2e. Initialize process management tools → core category ---
	processTools, procErr := tools.GetProcessTools()
	if procErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create process tools", "error", procErr)
		}
	} else {
		coreTools = append(coreTools, processTools...)
	}
	cleanups = append(cleanups, tools.CleanupProcessManager)

	// --- 2f. Initialize browser automation tools → deferred category ---
	browserCfg := browserConfigFromApp(cfg.AppConfig)
	browserMgr := browser.NewManager(browserCfg)
	var browserToolsSlice []tool.Tool
	var browserErr error
	if cfg.AppConfig != nil && sandbox.IsSandboxEnabled(&cfg.AppConfig.Sandbox) {
		browserToolsSlice, browserErr = tools.GetBrowserToolsForSandbox(browserMgr)
	} else {
		browserToolsSlice, browserErr = tools.GetBrowserTools(browserMgr)
	}
	if browserErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create browser tools", "error", browserErr)
		}
		browserToolsSlice = nil
	}
	cleanups = append(cleanups, browserMgr.Cleanup)

	// --- 2f-ii. Initialize email tools → deferred category ---
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
					slog.Warn("failed to create email client", "error", clientErr)
				}
			} else {
				tools.SetEmailClient(client)
			}
		}
	}
	var emailToolsSlice []tool.Tool
	emailTools, emailErr := tools.GetEmailTools()
	if emailErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create email tools", "error", emailErr)
		}
	} else if len(emailTools) > 0 {
		emailToolsSlice = emailTools
	}

	// --- 2g. Sub-agent delegation tool → core category ---
	var subAgentMgr *agent.SubAgentManager
	var fleetOnlyTools []tool.Tool // fleet-only tools (run_fleet_phase), assigned to SubAgentManager.FleetTools
	if cfg.AppConfig.SubAgents.IsSubAgentsEnabled() {
		delegateTool, delegateErr := tools.GetDelegateTasksTool()
		if delegateErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create delegate_tasks tool", "error", delegateErr)
			}
		} else {
			coreTools = append(coreTools, delegateTool)

			subAgentCfg := agent.SubAgentConfig{
				MaxDepth:      cfg.AppConfig.SubAgents.MaxDepth,
				MaxConcurrent: cfg.AppConfig.SubAgents.MaxConcurrent,
				TaskTimeout:   cfg.AppConfig.SubAgents.TaskTimeout(),
			}
			subAgentMgr = agent.NewSubAgentManager(subAgentCfg)
		}
	}

	// --- 2h. Initialize skills system → deferred category ---
	var loadedSkills []skills.Skill
	var skillIndex string
	var skillToolSlice []tool.Tool
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
				slog.Warn("failed to load skills", "error", skillErr)
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
						slog.Warn("failed to create skill_lookup tool", "error", stErr)
					}
				} else {
					skillToolSlice = append(skillToolSlice, skillTool)
				}
			}
			if cfg.DebugMode {
				slog.Debug("skills loaded", "component", "chat-factory", "total", len(loadedSkills), "eligible", len(eligible))
			}
		}
	}

	// --- 3. Load MCP tools from cache (lazy) ---
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		slog.Warn("failed to load MCP config", "error", err)
	}
	var lazyToolsets []*agent.LazyMCPToolset

	if mcpCfg != nil && len(mcpCfg.MCPServers) > 0 {
		if _, loadErr := cache.LoadCache(); loadErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to load tools cache", "error", loadErr)
			}
		}

		for name, serverCfg := range mcpCfg.MCPServers {
			if !serverCfg.IsEnabled() {
				continue
			}
			cachedTools := cache.GetToolsForServer(name)
			if len(cachedTools) == 0 {
				if cfg.DebugMode {
					slog.Debug("MCP server has no cached tools", "component", "lazy-mcp", "server", name)
				}
				continue
			}
			lt := agent.NewLazyMCPToolset(name, cachedTools, serverCfg, cfg.DebugMode)
			lazyToolsets = append(lazyToolsets, lt)
		}

		// Note: MCP toolsets are NOT collected into a separate slice anymore.
		// Each server is registered as its own tool group (mcp:<name>)
		// in section 4b below, wrapped in SanitizedToolset at that point.
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

	// --- 3b. Initialize sandbox (session container isolation) ---
	// When sandbox is enabled, all internal tools are wrapped with NodeTool
	// proxies that route execution to an astonish node inside an Incus
	// container. Container creation is lazy — the first tool call triggers
	// cloning from the template and starting the node process.
	var sandboxNodePool *sandbox.NodeClientPool      // hoisted for save_sandbox_template tool
	var sandboxIncusClient *sandbox.IncusClient      // hoisted for save_sandbox_template tool
	var sandboxTplRegistry *sandbox.TemplateRegistry // hoisted for save_sandbox_template tool
	var sandboxSessRegistry *sandbox.SessionRegistry // hoisted for save_sandbox_template tool
	if cfg.AppConfig != nil && sandbox.IsSandboxEnabled(&cfg.AppConfig.Sandbox) {
		sandboxClient, sandboxErr := sandbox.SetupSandboxRuntime()
		if sandboxErr != nil {
			return nil, fmt.Errorf("sandbox is enabled but the runtime is not available: %w\n\nTo disable sandbox, set 'sandbox.enabled: false' in ~/.config/astonish/config.yaml", sandboxErr)
		}

		sessRegistry, regErr := sandbox.NewSessionRegistry()
		if regErr != nil {
			return nil, fmt.Errorf("sandbox is enabled but session registry failed: %w", regErr)
		}

		tplRegistry, tplErr := sandbox.NewTemplateRegistry()
		if tplErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create template registry", "error", tplErr)
			}
		}

		// Create a pool that manages per-session LazyNodeClients.
		// Each chat session gets its own container, created lazily
		// on the first tool call for that session.
		limits := sandbox.EffectiveLimits(&cfg.AppConfig.Sandbox)
		nodePool := sandbox.NewNodeClientPool(sandboxClient, sessRegistry, tplRegistry, "", &limits)

		// Wrap all tool category slices with NodeTool proxies (pool-backed).
		// Browser tools are NOT wrapped — they run on the host and need direct
		// access to Chrome. Each category slice is wrapped independently so
		// deferred tools are already sandbox-ready when activated later.
		coreTools = sandbox.WrapToolsWithNode(coreTools, nodePool)
		if len(credToolsSlice) > 0 {
			credToolsSlice = sandbox.WrapToolsWithNode(credToolsSlice, nodePool)
		}
		if len(schedToolsSlice) > 0 {
			schedToolsSlice = sandbox.WrapToolsWithNode(schedToolsSlice, nodePool)
		}
		if len(distillToolsSlice) > 0 {
			distillToolsSlice = sandbox.WrapToolsWithNode(distillToolsSlice, nodePool)
		}
		if len(emailToolsSlice) > 0 {
			emailToolsSlice = sandbox.WrapToolsWithNode(emailToolsSlice, nodePool)
		}
		if len(skillToolSlice) > 0 {
			skillToolSlice = sandbox.WrapToolsWithNode(skillToolSlice, nodePool)
		}
		// Note: browserToolsSlice is intentionally NOT wrapped — browser runs on host

		// Hoist references for template tool registration
		sandboxNodePool = nodePool
		sandboxIncusClient = sandboxClient
		sandboxTplRegistry = tplRegistry
		sandboxSessRegistry = sessRegistry

		// Wire sandbox pool to all lazy MCP toolsets so stdio MCP servers
		// start inside the session's container instead of on the host.
		// SSE transport servers are unaffected (isSSETransport check inside).
		for _, lt := range lazyToolsets {
			lt.SetSandboxPool(nodePool)
		}

		// Async refresh: check all templates for stale binaries in the background.
		// Must NOT block startup (was the cause of the 502 bug).
		if tplRegistry != nil {
			go sandbox.RefreshAllIfNeeded(sandboxClient, tplRegistry)
		}

		// Auto-prune stale session containers from previous daemon runs.
		// Only destroy containers whose sessions no longer exist in the store.
		existingSessionIDs := make(map[string]bool)
		if cfg.SessionStore != nil {
			// Preferred: use the shared store's index (daemon/studio path).
			if indexData, err := cfg.SessionStore.Index().Load(); err == nil {
				for id := range indexData.Sessions {
					existingSessionIDs[id] = true
				}
			}
		} else if cfg.AppConfig != nil {
			// Fallback: read the index file directly (console/CLI path).
			// Without this, existingSessionIDs is empty and prune would
			// destroy every stopped container — even valid ones.
			var sessCfg *config.SessionConfig
			sessCfg = &cfg.AppConfig.Sessions
			if sessDir, dirErr := config.GetSessionsDir(sessCfg); dirErr == nil {
				idx := persistentsession.NewSessionIndex(filepath.Join(sessDir, "index.json"))
				if indexData, err := idx.Load(); err == nil {
					for id := range indexData.Sessions {
						existingSessionIDs[id] = true
					}
				}
			}
		}
		if pruned := sandbox.PruneStaleOnStartup(sandboxClient, sessRegistry, existingSessionIDs); pruned > 0 {
			startupNotices = append(startupNotices,
				fmt.Sprintf("Cleaned up %d stale session container(s) from previous run", pruned))
		}

		// Start idle watchdog: stops containers that have been inactive for the
		// configured timeout (default 10 min), preserving them for fast restart.
		idleTimeout := sandbox.EffectiveIdleTimeout(&cfg.AppConfig.Sandbox)
		if idleTimeout > 0 {
			idleCtx, idleCancel := context.WithCancel(context.Background())
			nodePool.StartIdleWatchdog(idleCtx, idleTimeout)
			cleanups = append(cleanups, idleCancel)
			if cfg.DebugMode {
				slog.Debug("sandbox idle watchdog enabled", "component", "chat-factory", "timeout", idleTimeout)
			}
		}

		cleanups = append(cleanups, func() {
			// Cleanup destroys all per-session containers
			nodePool.Cleanup()
		})
	}

	// --- 4. Create session service ---
	var sessionService session.Service
	if cfg.SessionStore != nil {
		// Reuse the pre-created store (shared with fleet sessions in the daemon).
		sessionService = cfg.SessionStore
		if cfg.DebugMode {
			slog.Debug("session storage: file (shared store)", "component", "chat-factory")
		}
	} else if cfg.AppConfig != nil && cfg.AppConfig.Sessions.Storage == "memory" {
		sessionService = session.InMemoryService()
		if cfg.DebugMode {
			slog.Debug("session storage: memory", "component", "chat-factory")
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
			slog.Debug("session storage: file", "component", "chat-factory", "dir", sessDir)
		}
	}

	// --- 4b. Build tool groups for sub-agent delegation ---
	// Tools are organized into named groups. The main thread gets only essential
	// tools (file ops, shell, search, memory, delegate). All other tools are
	// available to sub-agents via named groups in the delegate_tasks tool.

	// Split coreTools into main-thread essentials and the full "core" group
	// for sub-agents. Main thread tools: read_file, write_file, edit_file,
	// shell_command, grep_search, find_files, memory_save, memory_search,
	// delegate_tasks, opencode.
	mainThreadToolNames := map[string]bool{
		"read_file":      true,
		"write_file":     true,
		"edit_file":      true,
		"shell_command":  true,
		"grep_search":    true,
		"find_files":     true,
		"memory_save":    true,
		"memory_search":  true,
		"delegate_tasks": true,
		"opencode":       true,
	}

	var mainThreadTools []tool.Tool
	for _, t := range coreTools {
		if mainThreadToolNames[t.Name()] {
			mainThreadTools = append(mainThreadTools, t)
		}
	}

	// Separate web-oriented tools from core into their own group
	webToolNames := map[string]bool{
		"web_fetch":    true,
		"read_pdf":     true,
		"http_request": true,
	}
	processToolNames := map[string]bool{
		"process_read":  true,
		"process_write": true,
		"process_list":  true,
		"process_kill":  true,
	}

	var coreGroupTools []tool.Tool
	var webGroupTools []tool.Tool
	var processGroupTools []tool.Tool
	for _, t := range coreTools {
		name := t.Name()
		if webToolNames[name] {
			webGroupTools = append(webGroupTools, t)
		} else if processToolNames[name] {
			processGroupTools = append(processGroupTools, t)
		} else {
			coreGroupTools = append(coreGroupTools, t)
		}
	}

	// Build tool groups map
	toolGroups := map[string]*agent.ToolGroup{}

	toolGroups["core"] = &agent.ToolGroup{
		Name:        "core",
		Description: "File operations, shell commands, search, memory, git diff, file tree",
		Tools:       coreGroupTools,
	}
	if len(webGroupTools) > 0 {
		toolGroups["web"] = &agent.ToolGroup{
			Name:        "web",
			Description: "Fetch web pages, read PDFs, make HTTP API requests",
			Tools:       webGroupTools,
		}
	}
	if len(processGroupTools) > 0 {
		toolGroups["process"] = &agent.ToolGroup{
			Name:        "process",
			Description: "Read, write, list, and kill background processes",
			Tools:       processGroupTools,
		}
	}
	if len(browserToolsSlice) > 0 {
		toolGroups["browser"] = &agent.ToolGroup{
			Name:        "browser",
			Description: "Web automation, screenshots, form filling, page interaction",
			Tools:       browserToolsSlice,
		}
	}
	if len(credToolsSlice) > 0 {
		toolGroups["credentials"] = &agent.ToolGroup{
			Name:        "credentials",
			Description: "Save, retrieve, list, test, and resolve credentials",
			Tools:       credToolsSlice,
		}
	}
	if len(schedToolsSlice) > 0 {
		toolGroups["scheduler"] = &agent.ToolGroup{
			Name:        "scheduler",
			Description: "Schedule and manage recurring jobs",
			Tools:       schedToolsSlice,
		}
	}
	if len(emailToolsSlice) > 0 {
		toolGroups["email"] = &agent.ToolGroup{
			Name:        "email",
			Description: "Read, send, search, and wait for email",
			Tools:       emailToolsSlice,
		}
	}
	if len(distillToolsSlice) > 0 {
		toolGroups["distill"] = &agent.ToolGroup{
			Name:        "distill",
			Description: "Distill conversation flows into reusable YAML",
			Tools:       distillToolsSlice,
		}
	}
	if len(skillToolSlice) > 0 {
		toolGroups["skill"] = &agent.ToolGroup{
			Name:        "skill",
			Description: "Look up available CLI tool skills",
			Tools:       skillToolSlice,
		}
	}

	// MCP servers — each is its own group
	for _, lt := range lazyToolsets {
		sanitized := agent.NewSanitizedToolset(lt, cfg.DebugMode)
		groupName := "mcp:" + lt.Name()
		serverDesc := fmt.Sprintf("MCP server: %s (%d tools)", lt.Name(), lt.ToolCount())
		toolGroups[groupName] = &agent.ToolGroup{
			Name:        groupName,
			Description: serverDesc,
			Toolsets:    []tool.Toolset{sanitized},
		}
	}

	if cfg.DebugMode {
		totalTools := 0
		for _, g := range toolGroups {
			totalTools += len(g.Tools)
		}
		slog.Debug("tool groups configured", "component", "chat-factory",
			"groups", len(toolGroups), "totalTools", totalTools, "mainThreadTools", len(mainThreadTools))
	}

	// --- 5. Build system prompt ---
	workspaceDir := cfg.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir, _ = os.Getwd()
	}

	// The prompt builder uses all tools for capability detection.
	// It receives sorted tool groups for the delegation guidance section.
	var allToolsForPrompt []tool.Tool
	var allToolsetsForPrompt []tool.Toolset
	sortedGroups := make([]*agent.ToolGroup, 0, len(toolGroups))
	for _, g := range toolGroups {
		allToolsForPrompt = append(allToolsForPrompt, g.Tools...)
		allToolsetsForPrompt = append(allToolsetsForPrompt, g.Toolsets...)
		sortedGroups = append(sortedGroups, g)
	}
	sort.Slice(sortedGroups, func(i, j int) bool {
		return sortedGroups[i].Name < sortedGroups[j].Name
	})

	promptBuilder := &agent.SystemPromptBuilder{
		Tools:        allToolsForPrompt,
		Toolsets:     allToolsetsForPrompt,
		WorkspaceDir: workspaceDir,
		Catalog:      sortedGroups,
	}

	// Check web tool availability (across all MCP toolsets, not just active)
	if webSearchConfigured, serverName, toolName := api.IsWebSearchConfigured(); webSearchConfigured {
		for _, lt := range lazyToolsets {
			if lt.Name() == serverName {
				promptBuilder.WebSearchAvailable = true
				promptBuilder.WebSearchToolName = toolName
				break
			}
		}
	}
	if webExtractConfigured, serverName, toolName := api.IsWebExtractConfigured(); webExtractConfigured {
		for _, lt := range lazyToolsets {
			if lt.Name() == serverName {
				promptBuilder.WebExtractAvailable = true
				promptBuilder.WebExtractToolName = toolName
				break
			}
		}
	}

	// Check browser availability (native browser tools)
	if browserErr == nil && len(browserToolsSlice) > 0 {
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

	// Resolve timezone: general.timezone -> agent_identity.timezone -> system default
	if cfg.AppConfig != nil {
		tz := cfg.AppConfig.General.Timezone
		if tz == "" && cfg.AppConfig.AgentIdentity.Timezone != "" {
			tz = cfg.AppConfig.AgentIdentity.Timezone
		}
		if tz != "" {
			promptBuilder.Timezone = tz
		}
	}

	// Set skill index for system prompt
	if skillIndex != "" {
		promptBuilder.SkillIndex = skillIndex
	}

	// Load INSTRUCTIONS.md
	var memDir string
	if cfg.AppConfig != nil && cfg.AppConfig.Memory.IsMemoryEnabled() {
		var err error
		memDir, err = config.GetMemoryDir(&cfg.AppConfig.Memory)
		if err != nil {
			slog.Warn("failed to get memory directory from config", "error", err)
		}
	}
	if memDir == "" {
		var err error
		memDir, err = config.GetMemoryDir(nil)
		if err != nil {
			slog.Warn("failed to get default memory directory", "error", err)
		}
	}
	if memDir != "" {
		created, ensErr := memory.EnsureInstructions(memDir)
		if ensErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to ensure INSTRUCTIONS.md", "error", ensErr)
			}
		} else if created && cfg.DebugMode {
			slog.Debug("created default INSTRUCTIONS.md", "component", "chat-factory", "dir", memDir)
		}
		instrContent, instrErr := memory.LoadInstructions(memDir)
		if instrErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to load INSTRUCTIONS.md", "error", instrErr)
			}
		} else if instrContent != "" {
			promptBuilder.InstructionsContent = instrContent
		}
	}

	// Generate SELF.md from current config state
	var selfMDMemDir string
	if memDir != "" {
		selfMDMemDir = memDir

		selfCfg := factoryBuildSelfMDConfig(cfg, memDir, allToolsForPrompt, allToolsetsForPrompt, mcpCfg, memStore, memorySearchAvailable, loadedSkills)
		selfContent := memory.GenerateSelfMD(selfCfg)
		if writeErr := memory.WriteSelfMD(memDir, selfContent); writeErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to write SELF.md", "error", writeErr)
			}
		} else if cfg.DebugMode {
			slog.Debug("generated SELF.md", "component", "chat-factory", "bytes", len(selfContent))
		}

		// Sync guidance documents to memory/guidance/ for vector indexing.
		// Guidance docs replace the hardcoded system prompt sections —
		// they are retrieved via auto-knowledge search per turn.
		if syncErr := agent.SyncGuidanceToMemory(memDir); syncErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to sync guidance docs", "error", syncErr)
			}
		} else if cfg.DebugMode {
			slog.Debug("synced guidance docs to memory/guidance/")
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
				slog.Warn("failed to bootstrap bundled fleets", "error", fEnsErr)
			}
		} else if fWritten > 0 && cfg.DebugMode {
			slog.Debug("bootstrapped bundled fleets", "count", fWritten, "dir", fleetsDir)
		}

		fleetReg, fRegErr := fleet.NewRegistry(fleetsDir)
		if fRegErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to load fleet registry", "error", fRegErr)
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
						slog.Warn("failed to create fleet internal tools", "error", ftErr)
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
					slog.Debug("fleet awareness", "fleets", fleetReg.Count())
				}
			}
		}

		// Initialize fleet plan registry
		fleetPlansDir, fpErr := config.GetFleetPlansDir()
		if fpErr == nil {
			planReg, prErr := fleet.NewPlanRegistry(fleetPlansDir)
			if prErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to load fleet plan registry", "error", prErr)
				}
			} else {
				// Wire plan registry to API handlers
				api.SetFleetPlanRegistry(planReg)
				// Wire plan registry to the save_fleet_plan tool
				tools.SetFleetPlanRegistry(planReg)
				if cfg.DebugMode {
					slog.Debug("fleet plans loaded", "count", planReg.Count(), "dir", fleetPlansDir)
				}
			}
		}

		// Fleet plan tools → deferred category
		var fleetToolsSlice []tool.Tool
		fleetPlanTools, fptErr := tools.GetFleetPlanTools()
		if fptErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create fleet plan tools", "error", fptErr)
			}
		} else {
			fleetToolsSlice = append(fleetToolsSlice, fleetPlanTools...)
		}

		fleetPlanValidateTools, fpvErr := tools.GetFleetPlanValidateTools()
		if fpvErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create fleet plan validate tools", "error", fpvErr)
			}
		} else {
			fleetToolsSlice = append(fleetToolsSlice, fleetPlanValidateTools...)
		}

		if len(fleetToolsSlice) > 0 {
			toolGroups["fleet"] = &agent.ToolGroup{
				Name:        "fleet",
				Description: "Create and validate fleet plans",
				Tools:       fleetToolsSlice,
			}
		}

		// opencode tool → add to main thread tools
		if cfg.AppConfig.SubAgents.IsSubAgentsEnabled() {
			ocTool, ocErr := tools.NewOpenCodeTool()
			if ocErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to create opencode tool for wizard", "error", ocErr)
				}
			} else {
				mainThreadTools = append(mainThreadTools, ocTool)
				// Also add to core group for sub-agents
				if g, ok := toolGroups["core"]; ok {
					g.Tools = append(g.Tools, ocTool)
				}
			}
		}

		// Sandbox template tools → deferred category
		if sandboxNodePool != nil && sandboxIncusClient != nil && sandboxTplRegistry != nil {
			var sandboxTplTools []tool.Tool
			tplTool, tplErr := tools.NewSaveSandboxTemplateTool(sandboxNodePool, sandboxIncusClient, sandboxTplRegistry, sandboxSessRegistry)
			if tplErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to create save_sandbox_template tool", "error", tplErr)
				}
			} else {
				sandboxTplTools = append(sandboxTplTools, tplTool)
			}

			listTplTool, listErr := tools.NewListSandboxTemplatesTool(sandboxTplRegistry)
			if listErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to create list_sandbox_templates tool", "error", listErr)
				}
			} else {
				sandboxTplTools = append(sandboxTplTools, listTplTool)
			}

			useTplTool, useErr := tools.NewUseSandboxTemplateTool(sandboxNodePool, sandboxTplRegistry)
			if useErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to create use_sandbox_template tool", "error", useErr)
				}
			} else {
				sandboxTplTools = append(sandboxTplTools, useTplTool)
			}

			if len(sandboxTplTools) > 0 {
				toolGroups["sandbox_templates"] = &agent.ToolGroup{
					Name:        "sandbox_templates",
					Description: "Save, list, and use sandbox container templates",
					Tools:       sandboxTplTools,
				}
			}
		}
	}

	// Drill tools → deferred category
	var drillToolsSlice []tool.Tool
	drillTools, drillErr := tools.GetDrillTools()
	if drillErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create drill tools", "error", drillErr)
		}
	} else {
		drillToolsSlice = append(drillToolsSlice, drillTools...)
	}

	runDrillTool, runDrillErr := tools.NewRunDrillTool(sandboxNodePool, adrill.NewLLMProviderFromModel(llm))
	if runDrillErr != nil {
		if cfg.DebugMode {
			slog.Warn("failed to create run_drill tool", "error", runDrillErr)
		}
	} else {
		drillToolsSlice = append(drillToolsSlice, runDrillTool)
	}

	if len(drillToolsSlice) > 0 {
		toolGroups["drill"] = &agent.ToolGroup{
			Name:        "drill",
			Description: "Create, validate, and run test drills",
			Tools:       drillToolsSlice,
		}
	}

	// Create the dedicated tool index for retrieval-based tool discovery.
	// One document per tool (name + description) enables accurate semantic
	// search without the chunking problems of the general memory store.
	var toolIndex *agent.ToolIndex
	if memStore != nil && memEmbeddingFunc != nil {
		var tiErr error
		toolIndex, tiErr = agent.NewToolIndex(memStore.DB(), memEmbeddingFunc)
		if tiErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to create tool index", "error", tiErr)
			}
		} else {
			sortedGroups := agent.SortedGroups(toolGroups)
			if syncErr := toolIndex.SyncTools(context.Background(), mainThreadTools, sortedGroups); syncErr != nil {
				if cfg.DebugMode {
					slog.Warn("failed to sync tool index", "error", syncErr)
				}
			} else if cfg.DebugMode {
				slog.Debug("tool index ready", "tools_indexed", toolIndex.Count())
			}
		}
	}

	// Create search_tools and add to main thread tools if tool index is available.
	// The onResults callback uses a forward reference to chatAgent (set after
	// ChatAgent creation) so that search_tools discoveries feed into the
	// dynamic tool injection system.
	var chatAgentRef *agent.ChatAgent
	var searchToolsTool tool.Tool
	if toolIndex != nil {
		var stErr error
		searchToolsTool, stErr = tools.NewSearchToolsTool(toolIndex, func(names []string) {
			if chatAgentRef != nil {
				chatAgentRef.RegisterSearchToolsResults(names)
			}
		})
		if stErr == nil {
			mainThreadTools = append(mainThreadTools, searchToolsTool)
		} else if cfg.DebugMode {
			slog.Warn("failed to create search_tools", "error", stErr)
		}
	}

	// --- 6. Create ChatAgent ---
	// Main thread gets essential tools (file ops, shell, search, memory,
	// delegate). Additional tools are dynamically injected per-turn based
	// on hybrid search relevance and search_tools discoveries.
	chatAgent := agent.NewChatAgent(
		llm, mainThreadTools, nil, sessionService,
		promptBuilder, cfg.DebugMode, cfg.AutoApprove,
	)
	chatAgentRef = chatAgent // wire the forward reference for search_tools callback

	// Wire tool index to ChatAgent for per-turn auto-discovery
	if toolIndex != nil {
		chatAgent.ToolIndex = toolIndex
	}

	// Wire credential redactor to ChatAgent, session service, and sub-agents
	if credStore != nil {
		redactor := credStore.Redactor()
		chatAgent.Redactor = redactor
		// Also wire to file-based session store for transcript redaction
		if fs, ok := sessionService.(*persistentsession.FileStore); ok {
			fs.RedactFunc = redactor.Redact
			// Wire retroactive redaction callback so that after save_credential
			// completes, the current session's transcript is scrubbed of any
			// secrets that were persisted before the credential was registered.
			chatAgent.RedactSessionFunc = fs.RedactSession
		}
	}

	// Wire SubAgentManager — deferred until all tools and LLM are known.
	// Sub-agents get tool groups for group-based tool resolution.
	if subAgentMgr != nil {
		subAgentMgr.LLM = llm
		subAgentMgr.ToolGroups = toolGroups
		subAgentMgr.FleetTools = fleetOnlyTools
		subAgentMgr.SessionService = sessionService
		subAgentMgr.MemoryManager = memMgr
		subAgentMgr.Redactor = chatAgent.Redactor
		subAgentMgr.EventForwarder = chatAgent.ForwardSubTaskEvent
		subAgentMgr.AppName = "astonish"
		subAgentMgr.UserID = "console_user"
		// Wire tool discovery so sub-agents can auto-discover their tools
		if toolIndex != nil {
			subAgentMgr.ToolIndex = toolIndex
		}
		if searchToolsTool != nil {
			subAgentMgr.SearchToolsTool = searchToolsTool
		}
		// Alias sub-agent sessions to the parent's sandbox container so they
		// share the same container instead of each creating a new one.
		if sandboxNodePool != nil {
			pool := sandboxNodePool
			subAgentMgr.OnChildSession = func(parentSessionID, childSessionID string) {
				pool.Alias(childSessionID, parentSessionID)
			}
		}
		tools.SetSubAgentManager(subAgentMgr)
	}

	// --- 6b. Initialize Flow Registry ---
	registryPath, regErr := agent.DefaultRegistryPath()
	if regErr == nil {
		registry, regLoadErr := agent.NewFlowRegistry(registryPath)
		if regLoadErr != nil {
			if cfg.DebugMode {
				slog.Warn("failed to load flow registry", "error", regLoadErr)
			}
		} else {
			chatAgent.FlowRegistry = registry

			if flowsDir != "" {
				added, syncErr := registry.SyncFromDirectory(flowsDir)
				if syncErr != nil {
					if cfg.DebugMode {
						slog.Warn("flow registry sync failed", "error", syncErr)
					}
				} else if added > 0 && cfg.DebugMode {
					slog.Debug("auto-registered new flows", "count", added, "dir", flowsDir)
				}
			}

			if memFlowsDir != "" && flowsDir != "" {
				entries := registry.Entries()
				if len(entries) > 0 {
					if reconErr := agent.ReconcileFlowKnowledge(flowsDir, memFlowsDir, entries); reconErr != nil {
						if cfg.DebugMode {
							slog.Warn("flow knowledge reconciliation failed", "error", reconErr)
						}
					} else if cfg.DebugMode {
						slog.Debug("reconciled flow knowledge docs", "dir", memFlowsDir)
					}
				}
			}
		}
	}

	// --- 6b2. Wire knowledge search callbacks ---
	if memorySearchAvailable && memStore != nil {
		chatAgent.KnowledgeSearch = func(ctx context.Context, query string, maxResults int, minScore float64) ([]agent.KnowledgeSearchResult, error) {
			results, err := memStore.Search(ctx, query, maxResults, minScore)
			if err != nil {
				return nil, err
			}
			var knowledgeResults []agent.KnowledgeSearchResult
			for _, r := range results {
				knowledgeResults = append(knowledgeResults, agent.KnowledgeSearchResult{
					Path:     r.Path,
					Score:    r.Score,
					Snippet:  r.Snippet,
					Category: r.Category,
				})
			}
			return knowledgeResults, nil
		}

		chatAgent.KnowledgeSearchByCategory = func(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]agent.KnowledgeSearchResult, error) {
			results, err := memStore.SearchByCategory(ctx, query, maxResults, minScore, category)
			if err != nil {
				return nil, err
			}
			var knowledgeResults []agent.KnowledgeSearchResult
			for _, r := range results {
				knowledgeResults = append(knowledgeResults, agent.KnowledgeSearchResult{
					Path:     r.Path,
					Score:    r.Score,
					Snippet:  r.Snippet,
					Category: r.Category,
				})
			}
			return knowledgeResults, nil
		}

		if cfg.DebugMode {
			slog.Debug("auto knowledge retrieval: enabled")
		}
	}

	// --- 6b3. Wire SELF.md refresher ---
	if selfMDMemDir != "" {
		chatAgent.SelfMDRefresher = func() {
			selfCfg := factoryBuildSelfMDConfig(cfg, selfMDMemDir, allToolsForPrompt, allToolsetsForPrompt, mcpCfg, memStore, memorySearchAvailable, loadedSkills)
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
				if cfg.DebugMode {
					slog.Debug("SELF.md refreshed", "bytes", len(content))
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
		llm, allToolsForPrompt, allToolsetsForPrompt,
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
			slog.Debug("context compaction enabled", "window_tokens", contextWindow, "threshold_pct", compactor.Threshold*100)
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

		// Post-task memory reflection: silent LLM call after non-trivial
		// tasks to save any discovered knowledge the model forgot to persist.
		reflector := &agent.MemoryReflector{
			LLM:           llm,
			MemoryManager: memMgr,
			DebugMode:     cfg.DebugMode,
		}
		if memStore != nil {
			reflector.MemoryStore = memStore
		}
		chatAgent.MemoryReflector = reflector
	}
	chatAgent.FlowContextBuilder = &agent.FlowContextBuilder{DebugMode: cfg.DebugMode}

	// Build ShutdownSandbox callback (nil when sandbox is not enabled)
	var shutdownSandbox func()
	if sandboxNodePool != nil {
		pool := sandboxNodePool
		shutdownSandbox = func() {
			pool.CleanupForShutdown()
		}
	}

	return &ChatFactoryResult{
		ChatAgent:             chatAgent,
		LLM:                   llm,
		ProviderName:          cfg.ProviderName,
		ModelName:             cfg.ModelName,
		Compactor:             compactor,
		InternalTools:         allToolsForPrompt,
		MCPToolsets:           allToolsetsForPrompt,
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
		ShutdownSandbox:       shutdownSandbox,
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
