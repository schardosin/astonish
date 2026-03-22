package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/channels"
	emailchan "github.com/schardosin/astonish/pkg/channels/email"
	"github.com/schardosin/astonish/pkg/channels/telegram"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	emailpkg "github.com/schardosin/astonish/pkg/email"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/scheduler"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
)

// RunConfig holds the configuration for a daemon run.
type RunConfig struct {
	Port  int
	Debug bool
}

// Run starts the daemon in the foreground. It starts the Studio HTTP server,
// writes a PID file, handles signals for graceful shutdown, and cleans up on exit.
// This function blocks until a shutdown signal is received.
func Run(cfg RunConfig) error {
	// Load app config for provider/MCP setup
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Resolve port
	port := cfg.Port
	if port <= 0 {
		if appCfg.Daemon.Port > 0 {
			port = appCfg.Daemon.Port
		} else {
			port = 9393
		}
	}

	// Set up logging
	logDir := appCfg.Daemon.GetLogDir()
	logger, err := NewLogger(logDir + "/daemon.log")
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	// Redirect standard log to file
	log.SetOutput(logger)
	log.SetFlags(0) // Logger adds its own timestamps

	logger.Printf("Astonish daemon starting (port: %d, pid: %d)", port, os.Getpid())

	// Write PID file
	pidPath, err := DefaultPIDPath()
	if err != nil {
		return fmt.Errorf("failed to resolve PID path: %w", err)
	}
	if err := WritePID(pidPath); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer RemovePID(pidPath)

	// Set up provider environment variables (credential store → config → env fallback)
	configDir, _ := config.GetConfigDir()
	var credStore *credentials.Store
	if configDir != "" {
		if cs, csErr := credentials.Open(configDir); csErr == nil {
			credStore = cs
			config.SetInstalledSecretGetter(cs.GetSecret)
			api.SetAPICredentialStore(cs)

			// Auto-migrate secrets from config.yaml (one-time)
			migrated, migrateErr := credentials.MigrateFromConfig(cs, appCfg, log.Default())
			if migrateErr != nil {
				logger.Printf("Warning: Credential migration error: %v", migrateErr)
			} else if migrated > 0 {
				if saveErr := config.SaveAppConfig(appCfg); saveErr != nil {
					logger.Printf("Warning: Failed to save scrubbed config: %v", saveErr)
				} else {
					logger.Printf("Migrated %d secrets from config.yaml to encrypted store", migrated)
				}
			}
			config.SetupAllProviderEnvFromStore(appCfg, cs.GetSecret)
		} else {
			logger.Printf("Warning: Failed to open credential store: %v", csErr)
			config.SetupAllProviderEnv(appCfg)
		}
	} else {
		config.SetupAllProviderEnv(appCfg)
	}

	// Set up MCP environment variables
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// --- Generate managed OpenCode config ---
	// This creates ~/.config/astonish/opencode.json from the current provider
	// settings so that OpenCode (used as a delegate tool in fleet sessions)
	// does not need independent configuration.
	var getSecret config.SecretGetter
	if credStore != nil {
		getSecret = credStore.GetSecret
	}
	if ocResult, ocErr := config.GenerateOpenCodeConfig(appCfg, getSecret); ocErr != nil {
		logger.Printf("Warning: Failed to generate OpenCode config: %v", ocErr)
	} else {
		tools.SetOpenCodeConfig(ocResult.ConfigPath, ocResult.ProviderID, ocResult.ModelID, ocResult.ExtraEnv)
		// Also set fleet project context vars so opencode_init uses the managed config
		fleet.OpenCodeConfigPath = ocResult.ConfigPath
		fleet.OpenCodeExtraEnv = ocResult.ExtraEnv
		fleet.OpenCodeModelFlag = ocResult.FullModelID()
		logger.Printf("OpenCode config generated (%s, provider: %s, model: %s)", ocResult.ConfigPath, ocResult.ProviderID, ocResult.ModelID)
	}

	// Initialize tools cache
	ctx := context.Background()
	api.InitToolsCache(ctx)

	// --- Initialize file store for session persistence ---
	// A single shared FileStore is used for ALL session persistence in the daemon:
	// fleet session transcripts, Studio chat sub-agent sessions, and channel
	// sub-agent sessions. Using one instance prevents index.json race conditions
	// that caused child sessions to become orphaned (invisible in trace view).
	var sharedFileStore *persistentsession.FileStore
	if sessDir, dirErr := config.GetSessionsDir(&appCfg.Sessions); dirErr == nil {
		if store, fsErr := persistentsession.NewFileStore(sessDir); fsErr == nil {
			sharedFileStore = store
			api.SetFleetSessionStore(store)
			logger.Printf("Session store initialized (%s)", sessDir)
		} else {
			logger.Printf("Warning: Failed to initialize session store: %v", fsErr)
		}
	} else {
		logger.Printf("Warning: Failed to resolve sessions directory: %v", dirErr)
	}

	// --- Initialize device authorization for Studio ---
	var authManager *api.AuthManager
	if appCfg.Daemon.Auth.IsAuthEnabled() && configDir != "" {
		authStore, authErr := api.NewAuthStore(configDir, appCfg.Daemon.Auth.GetSessionTTL())
		if authErr != nil {
			logger.Printf("Warning: Failed to initialize auth store: %v", authErr)
		} else {
			authManager = api.NewAuthManager(authStore)
			ttlDays := appCfg.Daemon.Auth.SessionTTLDays
			if ttlDays == 0 {
				ttlDays = 90
			}
			logger.Printf("Studio device authorization enabled (session TTL: %d days)", ttlDays)
		}
	} else if !appCfg.Daemon.Auth.IsAuthEnabled() {
		logger.Printf("Studio device authorization disabled by config")
	}

	// --- Initialize shared ChatAgent if channels need it ---
	// The scheduler is always-on by default but doesn't require a ChatAgent at startup:
	// - Routine jobs use the headless runner (creates its own LLM)
	// - Adaptive jobs use the shared ChatAgent if available, or fail gracefully
	// The ChatAgent is expensive to create, so we only init it when channels are enabled.
	var channelMgr *channels.ChannelManager
	var factoryResult *launcher.ChatFactoryResult
	defer func() {
		if factoryResult != nil {
			factoryResult.Cleanup()
		}
	}()

	needsChatAgent := appCfg.Channels.IsChannelsEnabled()

	if needsChatAgent {
		logger.Printf("Initializing ChatAgent for channels...")

		// Build a fully-wired ChatAgent for channel/scheduler use
		fr, factoryErr := launcher.NewWiredChatAgent(ctx, &launcher.ChatFactoryConfig{
			AppConfig:    appCfg,
			ProviderName: appCfg.General.DefaultProvider,
			ModelName:    appCfg.General.DefaultModel,
			DebugMode:    cfg.Debug,
			AutoApprove:  true, // Channels/scheduler auto-approve all tools
			IsDaemon:     true, // We ARE the daemon — always run indexing/watchers.
			SessionStore: sharedFileStore,
		})
		if factoryErr != nil {
			logger.Printf("Warning: Failed to initialize ChatAgent: %v", factoryErr)
		} else {
			factoryResult = fr
			// Make distillation available to LLM tools (for auto-distill during scheduling)
			tools.SetDistillAccess(newDistillBridge(fr.ChatAgent))
		}
	}

	// initChannels creates (or recreates) the ChannelManager from fresh config.
	// It registers and starts all enabled channel adapters. The ChatAgent and
	// factoryResult are reused — only channel adapters are recycled.
	initChannels := func(freshCfg *config.AppConfig) (*channels.ChannelManager, error) {
		if !freshCfg.Channels.IsChannelsEnabled() {
			return nil, nil
		}
		if factoryResult == nil {
			return nil, fmt.Errorf("channels enabled but no ChatAgent available — restart the daemon")
		}

		mgr := channels.NewChannelManager(factoryResult.ChatAgent, factoryResult.SessionService, log.Default(), &channels.ChannelManagerConfig{
			ProviderName: factoryResult.ProviderName,
			ModelName:    factoryResult.ModelName,
			ToolCount:    len(factoryResult.InternalTools),
		})

		if factoryResult.CredentialStore != nil {
			mgr.SetRedactor(factoryResult.CredentialStore.Redactor())
		}

		// Register Telegram if enabled
		if freshCfg.Channels.Telegram.IsTelegramEnabled() {
			botToken := freshCfg.Channels.Telegram.BotToken
			if botToken == "" && factoryResult.CredentialStore != nil {
				botToken = factoryResult.CredentialStore.GetSecret("channels.telegram.bot_token")
			}
			if botToken == "" {
				logger.Printf("Warning: Telegram enabled but no bot token found")
			} else {
				tg := telegram.New(&telegram.Config{
					BotToken:  botToken,
					AllowFrom: freshCfg.Channels.Telegram.AllowFrom,
					Commands:  mgr.Commands(),
				}, log.Default())
				mgr.Register(tg)
				logger.Printf("Telegram channel registered")
			}
		}

		// Register Email channel (inbound polling) only if explicitly enabled
		if freshCfg.Channels.Email.IsEmailEnabled() {
			emailPassword := freshCfg.Channels.Email.Password
			if emailPassword == "" && factoryResult.CredentialStore != nil {
				emailPassword = factoryResult.CredentialStore.GetSecret("channels.email.password")
			}
			if emailPassword == "" {
				logger.Printf("Warning: Email channel enabled but no password found")
			} else if freshCfg.Channels.Email.IMAPServer == "" || freshCfg.Channels.Email.SMTPServer == "" {
				logger.Printf("Warning: Email channel enabled but IMAP/SMTP servers not configured")
			} else {
				pollInterval := time.Duration(freshCfg.Channels.Email.GetPollInterval()) * time.Second
				em := emailchan.New(&emailchan.Config{
					Provider:     freshCfg.Channels.Email.Provider,
					IMAPServer:   freshCfg.Channels.Email.IMAPServer,
					SMTPServer:   freshCfg.Channels.Email.SMTPServer,
					Address:      freshCfg.Channels.Email.Address,
					Username:     freshCfg.Channels.Email.Username,
					Password:     emailPassword,
					PollInterval: pollInterval,
					AllowFrom:    freshCfg.Channels.Email.AllowFrom,
					Folder:       freshCfg.Channels.Email.Folder,
					MarkRead:     freshCfg.Channels.Email.IsMarkRead(),
					MaxBodyChars: freshCfg.Channels.Email.MaxBodyChars,
					Commands:     mgr.Commands(),
				}, log.Default())
				mgr.Register(em)
				logger.Printf("Email channel registered (%s)", freshCfg.Channels.Email.Address)
			}
		}

		if err := mgr.StartAll(ctx); err != nil {
			return mgr, fmt.Errorf("failed to start channels: %w", err)
		}
		logger.Printf("Channels started")
		return mgr, nil
	}

	// initEmailTools initializes email tools whenever valid IMAP/SMTP
	// credentials are configured, regardless of whether the email channel is
	// enabled. This allows the agent to use email_list, email_read,
	// email_search, email_send, email_wait, etc. during autonomous flows
	// (e.g. web portal registration) without requiring the polling channel.
	initEmailTools := func(cfg *config.AppConfig) {
		if factoryResult == nil {
			return
		}
		// If the factory already initialized the email client (both console and
		// daemon paths), skip to avoid creating a redundant second client.
		if tools.HasEmailClient() {
			return
		}
		emailPassword := cfg.Channels.Email.Password
		if emailPassword == "" && factoryResult.CredentialStore != nil {
			emailPassword = factoryResult.CredentialStore.GetSecret("channels.email.password")
		}
		if emailPassword == "" || cfg.Channels.Email.IMAPServer == "" ||
			cfg.Channels.Email.SMTPServer == "" || cfg.Channels.Email.Address == "" {
			return
		}
		setupEmailTools(&emailToolConfig{
			Provider:     cfg.Channels.Email.Provider,
			IMAPServer:   cfg.Channels.Email.IMAPServer,
			SMTPServer:   cfg.Channels.Email.SMTPServer,
			Address:      cfg.Channels.Email.Address,
			Username:     cfg.Channels.Email.Username,
			Password:     emailPassword,
			Folder:       cfg.Channels.Email.Folder,
			MaxBodyChars: cfg.Channels.Email.MaxBodyChars,
		})
		logger.Printf("Email tools initialized (%s)", cfg.Channels.Email.Address)
	}

	// --- Initialize email tools (independent of channel enabled state) ---
	initEmailTools(appCfg)

	// --- Initialize channel manager if channels are enabled ---
	if mgr, err := initChannels(appCfg); err != nil {
		logger.Printf("Warning: %v", err)
	} else {
		channelMgr = mgr
	}
	if channelMgr != nil && authManager != nil {
		channelMgr.SetAuthorizeFunc(authManager.AuthorizeCode)
	}
	api.SetChannelManager(channelMgr)

	// reloadChannels re-reads config, stops existing channels, and starts new
	// ones. Called by the POST /api/channels/reload endpoint so that CLI
	// commands like "astonish channels setup telegram" can activate changes
	// without a full daemon restart.
	var reloadMu sync.Mutex
	reloadChannels := func() error {
		reloadMu.Lock()
		defer reloadMu.Unlock()

		logger.Printf("Reloading channel configuration...")

		// Re-read config and credential store from disk
		freshCfg, err := config.LoadAppConfig()
		if err != nil {
			return fmt.Errorf("failed to reload config: %w", err)
		}
		if factoryResult != nil && factoryResult.CredentialStore != nil {
			if err := factoryResult.CredentialStore.Reload(); err != nil {
				logger.Printf("Warning: failed to reload credential store: %v", err)
			}
		}

		// Stop existing channels
		if channelMgr != nil {
			shutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := channelMgr.StopAll(shutCtx); err != nil {
				logger.Printf("Warning: error stopping channels: %v", err)
			}
		}

		// Lazy-init ChatAgent if channels are now enabled but weren't at startup
		if freshCfg.Channels.IsChannelsEnabled() && factoryResult == nil {
			logger.Printf("Initializing ChatAgent for newly enabled channels...")
			fr, factoryErr := launcher.NewWiredChatAgent(ctx, &launcher.ChatFactoryConfig{
				AppConfig:    freshCfg,
				ProviderName: freshCfg.General.DefaultProvider,
				ModelName:    freshCfg.General.DefaultModel,
				DebugMode:    cfg.Debug,
				AutoApprove:  true,
			})
			if factoryErr != nil {
				return fmt.Errorf("failed to initialize ChatAgent: %w", factoryErr)
			}
			factoryResult = fr
			// Make distillation available to LLM tools
			tools.SetDistillAccess(newDistillBridge(fr.ChatAgent))
			// Cleanup handled by the deferred closure in Run() that reads
			// the current factoryResult at shutdown time.
		}

		// Refresh email tools (runs independently of channel state)
		initEmailTools(freshCfg)

		// Create fresh channel manager with new config
		mgr, err := initChannels(freshCfg)
		if err != nil {
			logger.Printf("Warning: %v", err)
		}
		channelMgr = mgr
		if channelMgr != nil && authManager != nil {
			channelMgr.SetAuthorizeFunc(authManager.AuthorizeCode)
		}
		api.SetChannelManager(channelMgr)

		logger.Printf("Channel reload complete")
		return nil
	}
	api.SetChannelReloadFunc(reloadChannels)

	// --- Initialize scheduler if enabled ---
	var sched *scheduler.Scheduler
	var schedExec *scheduler.Executor

	if appCfg.Scheduler.IsSchedulerEnabled() {
		storePath, storeErr := scheduler.DefaultStorePath()
		if storeErr != nil {
			logger.Printf("Warning: Failed to resolve scheduler store path: %v", storeErr)
		} else {
			store, storeErr := scheduler.NewStore(storePath)
			if storeErr != nil {
				logger.Printf("Warning: Failed to initialize scheduler store: %v", storeErr)
			} else {
				// Create executor with injected headless runner
				schedExec = &scheduler.Executor{
					AppConfig:    appCfg,
					ProviderName: appCfg.General.DefaultProvider,
					ModelName:    appCfg.General.DefaultModel,
					DebugMode:    cfg.Debug,
					RunHeadless:  makeHeadlessRunner(),
				}
				if factoryResult != nil {
					schedExec.ChatAgent = factoryResult.ChatAgent
					schedExec.SessionService = factoryResult.SessionService
				}

				// Create delivery function (only if channels are available)
				var deliver scheduler.DeliverFunc
				if channelMgr != nil {
					deliver = scheduler.NewDeliverFunc(channelMgr)
				}

				sched = scheduler.New(store, schedExec.Execute, deliver, log.Default())
				sched.Start(ctx)

				// Make scheduler available to API handlers and LLM tools
				api.SetScheduler(sched)
				tools.SetSchedulerAccess(newSchedulerBridge(sched))
			}
		}
	}

	// --- Initialize fleet plan activator ---
	// This bridges fleet plans to the scheduler for automated polling.
	// Requires both the scheduler and the plan registry to be initialized.
	if sched != nil {
		planReg := api.GetFleetPlanRegistry()
		if planReg != nil {
			fleetSchedBridge := newFleetSchedulerBridge(sched)
			fleetStarter := func(fCtx context.Context, fCfg fleet.HeadlessFleetConfig) (string, error) {
				return api.StartHeadlessFleetSession(fCtx, fCfg)
			}
			// Pass api.GetFleetPlanRegistry as a function so the activator
			// always sees the current registry instance, even if the Studio
			// lazy chat init replaces it with a new one.
			activator := fleet.NewPlanActivator(api.GetFleetPlanRegistry, fleetSchedBridge, fleetStarter)

			// Wire the poll function into the scheduler executor
			if schedExec != nil {
				schedExec.FleetPoll = activator.Poll
			}

			// Wire the recover function for session recovery after restart
			activator.SetRecoverFunc(func(rCtx context.Context, rCfg fleet.RecoverFleetConfig) error {
				return api.RecoverFleetSession(rCtx, rCfg)
			})

			// Wire credential-based GitHub token resolver for fleet plans.
			// When a plan has credentials: { github: "some-store-entry" }, this
			// resolver extracts the token from the encrypted credential store so
			// it can be injected as GH_TOKEN into gh CLI commands.
			if credStore != nil {
				activator.SetGHTokenResolver(func(plan *fleet.FleetPlan) string {
					resolved, _ := fleet.ResolveCredentials(plan, credStore)
					return fleet.GitHubToken(resolved)
				})
			}

			// Make activator available to API handlers
			api.SetPlanActivator(activator)

			// Wire auto-activation into save_fleet_plan tool so non-chat
			// plans start polling immediately after the wizard saves them.
			tools.SetPlanActivatorFunc(activator.Activate)

			// Wire the session registry so CheckForWork can detect active sessions
			activator.SetSessionRegistry(api.GetFleetSessionRegistry())

			// Wire OpenCode binary finder for project context generation.
			// Fleet templates can define a project_context section that runs
			// OpenCode /init to generate AGENTS.md before agents start.
			fleet.OpenCodeBinaryFinder = tools.FindOpenCodeBinary

			// Restore previously activated plans (re-create monitors)
			if err := activator.RestoreActivated(); err != nil {
				logger.Printf("Warning: Failed to restore activated plans: %v", err)
			}

			logger.Printf("Fleet plan activator initialized")
		}
	}

	// --- Wire fleet commands into channels ---
	// Must happen after both channelMgr and fleet registries are initialized.
	if channelMgr != nil {
		channelMgr.SetFleetDeps(&channels.FleetDeps{
			GetSessionRegistry: func() channels.FleetSessionRegistry {
				return api.GetFleetSessionRegistry()
			},
			GetPlanRegistry: func() channels.FleetPlanRegistry {
				reg := api.GetFleetPlanRegistry()
				if reg == nil {
					return nil
				}
				return &fleetPlanRegistryAdapter{reg: reg}
			},
			GetFleetRegistry: func() channels.FleetTemplateRegistry {
				reg := api.GetFleetRegistry()
				if reg == nil {
					return nil
				}
				return &fleetTemplateRegistryAdapter{reg: reg}
			},
			StartSessionFromPlan: func(planKey, initialMessage string) (*channels.FleetSessionStartResult, error) {
				result, err := api.StartFleetSessionFromPlan(planKey, initialMessage)
				if err != nil {
					return nil, err
				}
				return &channels.FleetSessionStartResult{
					SessionID: result.Session.ID,
					FleetKey:  result.FleetKey,
					FleetName: result.FleetName,
					SetOnMessagePosted: func(fn func(sender, text string)) {
						if result.SetOnMessagePosted != nil {
							result.SetOnMessagePosted(func(msg fleet.Message) {
								fn(msg.Sender, msg.Text)
							})
						}
					},
					SetOnSessionDone: func(fn func(sessionID string, err error)) {
						if result.SetOnSessionDone != nil {
							result.SetOnSessionDone(fn)
						}
					},
				}, nil
			},
			StopSession: func(sessionID string) error {
				registry := api.GetFleetSessionRegistry()
				fs := registry.Get(sessionID)
				if fs == nil {
					return fmt.Errorf("fleet session %s not found", sessionID)
				}
				fs.Stop()
				registry.Unregister(sessionID)
				return nil
			},
		})
		logger.Printf("Fleet commands wired into channels")
	}

	// Create and start the Studio server
	var studioOpts []launcher.StudioOption
	if authManager != nil {
		studioOpts = append(studioOpts, launcher.WithAuth(authManager))
	}
	if sharedFileStore != nil {
		studioOpts = append(studioOpts, launcher.WithSessionStore(sharedFileStore))
	}
	studio, err := launcher.NewStudioServer(port, studioOpts...)
	if err != nil {
		logger.Printf("Failed to start HTTP server: %v", err)
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start serving in a goroutine
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("HTTP server listening on http://localhost:%d", port)
		if err := studio.Serve(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Print to stdout for foreground mode
	fmt.Printf("Astonish daemon running (pid: %d, port: %d)\n", os.Getpid(), port)
	fmt.Printf("Log: %s/daemon.log\n", logDir)

	// Wait for signal or server error
	select {
	case sig := <-sigCh:
		logger.Printf("Received signal %v, shutting down...", sig)
		fmt.Printf("\nShutting down...\n")
	case err := <-errCh:
		if err != nil {
			logger.Printf("Server error: %v", err)
			return err
		}
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop scheduler first (finish in-flight jobs)
	if sched != nil {
		logger.Printf("Stopping scheduler...")
		sched.Stop()
	}

	// Stop channels (drain in-flight messages)
	if channelMgr != nil {
		logger.Printf("Stopping channels...")
		if err := channelMgr.StopAll(shutdownCtx); err != nil {
			logger.Printf("Warning: Channel shutdown error: %v", err)
		}
	}

	// Stop sandbox containers gracefully (preserve them for reconnection after restart).
	// Unlike Reset(), this does NOT destroy containers — sessions persist and will
	// get their containers back via EnsureSessionContainer on the next tool call.
	logger.Printf("Stopping sandbox containers...")
	api.GetChatManager().ShutdownContainers()

	if err := studio.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Shutdown error: %v", err)
		return fmt.Errorf("shutdown error: %w", err)
	}

	logger.Printf("Daemon stopped cleanly")
	return nil
}

// makeHeadlessRunner creates a RunHeadlessFunc that bridges the scheduler's
// executor to the launcher's headless runner, breaking the import cycle
// (scheduler -> launcher -> api).
func makeHeadlessRunner() scheduler.RunHeadlessFunc {
	return func(ctx context.Context, cfg *scheduler.HeadlessRunConfig) (string, error) {
		agentCfg, err := config.LoadAgent(cfg.FlowPath)
		if err != nil {
			return "", fmt.Errorf("failed to load flow %q: %w", cfg.FlowPath, err)
		}

		return launcher.RunHeadless(ctx, &launcher.HeadlessConfig{
			AgentConfig:  agentCfg,
			AppConfig:    cfg.AppConfig,
			ProviderName: cfg.ProviderName,
			ModelName:    cfg.ModelName,
			Parameters:   cfg.Parameters,
			DebugMode:    cfg.DebugMode,
		})
	}
}

// schedulerBridge adapts scheduler.Scheduler to tools.SchedulerAccess,
// bridging the two packages without creating import cycles.
type schedulerBridge struct {
	sched *scheduler.Scheduler
}

func newSchedulerBridge(s *scheduler.Scheduler) *schedulerBridge {
	return &schedulerBridge{sched: s}
}

func (b *schedulerBridge) AddJob(job *tools.SchedulerJob) error {
	sj := toolJobToSchedulerJob(job)
	if err := b.sched.Store().Add(sj); err != nil {
		return err
	}
	job.ID = sj.ID
	// Compute initial NextRun so the scheduler knows when to fire
	b.sched.RefreshNextRun(sj.ID)
	return nil
}

func (b *schedulerBridge) ListJobs() []*tools.SchedulerJob {
	jobs := b.sched.Store().List()
	result := make([]*tools.SchedulerJob, len(jobs))
	for i, j := range jobs {
		result[i] = schedulerJobToToolJob(j)
	}
	return result
}

func (b *schedulerBridge) RemoveJob(id string) error {
	return b.sched.Store().Remove(id)
}

func (b *schedulerBridge) UpdateJob(job *tools.SchedulerJob) error {
	sj := toolJobToSchedulerJob(job)
	if err := b.sched.Store().Update(sj); err != nil {
		return err
	}
	// Recompute NextRun from now so schedule changes take effect immediately
	b.sched.RefreshNextRun(sj.ID)
	return nil
}

func (b *schedulerBridge) RunNow(ctx context.Context, jobID string) (string, error) {
	return b.sched.RunNow(ctx, jobID)
}

func (b *schedulerBridge) GetJobByName(name string) *tools.SchedulerJob {
	j := b.sched.Store().GetByName(name)
	if j == nil {
		return nil
	}
	return schedulerJobToToolJob(j)
}

func (b *schedulerBridge) ValidateCron(expr string) error {
	return scheduler.ValidateCron(expr)
}

// toolJobToSchedulerJob converts a tools.SchedulerJob to scheduler.Job.
func toolJobToSchedulerJob(tj *tools.SchedulerJob) *scheduler.Job {
	sj := &scheduler.Job{
		ID:   tj.ID,
		Name: tj.Name,
		Mode: scheduler.JobMode(tj.Mode),
		Schedule: scheduler.JobSchedule{
			Cron:     tj.Cron,
			Timezone: tj.Timezone,
		},
		Payload: scheduler.JobPayload{
			Flow:         tj.Flow,
			Params:       tj.Params,
			Instructions: tj.Instr,
		},
		Delivery: scheduler.JobDelivery{
			Channel: tj.Channel,
			Target:  tj.Target,
		},
		Enabled:             tj.Enabled,
		CreatedAt:           tj.CreatedAt,
		ConsecutiveFailures: tj.Failures,
	}
	if tj.LastRun != nil {
		sj.LastRun = tj.LastRun
	}
	if tj.LastStatus != "" {
		sj.LastStatus = scheduler.JobStatus(tj.LastStatus)
	}
	if tj.NextRun != nil {
		sj.NextRun = tj.NextRun
	}
	return sj
}

// schedulerJobToToolJob converts a scheduler.Job to tools.SchedulerJob.
func schedulerJobToToolJob(sj *scheduler.Job) *tools.SchedulerJob {
	return &tools.SchedulerJob{
		ID:         sj.ID,
		Name:       sj.Name,
		Mode:       string(sj.Mode),
		Cron:       sj.Schedule.Cron,
		Timezone:   sj.Schedule.Timezone,
		Flow:       sj.Payload.Flow,
		Params:     sj.Payload.Params,
		Instr:      sj.Payload.Instructions,
		Channel:    sj.Delivery.Channel,
		Target:     sj.Delivery.Target,
		Enabled:    sj.Enabled,
		LastRun:    sj.LastRun,
		LastStatus: string(sj.LastStatus),
		NextRun:    sj.NextRun,
		Failures:   sj.ConsecutiveFailures,
		CreatedAt:  sj.CreatedAt,
	}
}

// distillBridge adapts agent.ChatAgent to tools.DistillAccess,
// bridging the two packages without creating import cycles.
type distillBridge struct {
	agent *agent.ChatAgent
}

func newDistillBridge(a *agent.ChatAgent) *distillBridge {
	return &distillBridge{agent: a}
}

func (b *distillBridge) PreviewDistill(ctx context.Context, ds tools.DistillSession) (string, error) {
	return b.agent.PreviewDistill(ctx, agent.DistillSession{
		SessionID: ds.SessionID,
		AppName:   ds.AppName,
		UserID:    ds.UserID,
	})
}

func (b *distillBridge) ConfirmAndDistill(ctx context.Context, ds tools.DistillSession, print func(string)) error {
	return b.agent.ConfirmAndDistill(ctx, agent.DistillSession{
		SessionID: ds.SessionID,
		AppName:   ds.AppName,
		UserID:    ds.UserID,
	}, print)
}

// emailToolConfig holds the info needed to create an email client for tools.
type emailToolConfig struct {
	Provider     string
	IMAPServer   string
	SMTPServer   string
	Address      string
	Username     string
	Password     string
	Folder       string
	MaxBodyChars int
}

// setupEmailTools creates an email client and registers it for the email tools.
func setupEmailTools(cfg *emailToolConfig) {
	emailCfg := &emailpkg.Config{
		Provider:     cfg.Provider,
		IMAPServer:   cfg.IMAPServer,
		SMTPServer:   cfg.SMTPServer,
		Address:      cfg.Address,
		Username:     cfg.Username,
		Password:     cfg.Password,
		Folder:       cfg.Folder,
		MaxBodyChars: cfg.MaxBodyChars,
	}
	client, err := emailpkg.NewClient(emailCfg)
	if err != nil {
		log.Printf("Warning: Failed to create email client for tools: %v", err)
		return
	}
	tools.SetEmailClient(client)
}

// fleetSchedulerBridge adapts scheduler.Scheduler to fleet.SchedulerAccess,
// bridging the two packages without import cycles.
type fleetSchedulerBridge struct {
	sched *scheduler.Scheduler
}

func newFleetSchedulerBridge(s *scheduler.Scheduler) *fleetSchedulerBridge {
	return &fleetSchedulerBridge{sched: s}
}

func (b *fleetSchedulerBridge) AddJob(job *fleet.SchedulerJob) error {
	sj := &scheduler.Job{
		Name: job.Name,
		Mode: scheduler.JobMode(job.Mode),
		Schedule: scheduler.JobSchedule{
			Cron: job.Cron,
		},
		Payload: scheduler.JobPayload{
			Flow: job.Flow,
		},
		Enabled: job.Enabled,
	}
	log.Printf("[fleet-sched-bridge] Adding job %q: mode=%s cron=%q flow=%q enabled=%v",
		job.Name, job.Mode, job.Cron, job.Flow, job.Enabled)
	if err := b.sched.Store().Add(sj); err != nil {
		return err
	}
	job.ID = sj.ID
	b.sched.RefreshNextRun(sj.ID)
	log.Printf("[fleet-sched-bridge] Job created with ID %s", sj.ID)
	return nil
}

func (b *fleetSchedulerBridge) RemoveJob(id string) error {
	return b.sched.Store().Remove(id)
}

func (b *fleetSchedulerBridge) RemoveJobByName(name string) error {
	for _, j := range b.sched.Store().List() {
		if j.Name == name {
			return b.sched.Store().Remove(j.ID)
		}
	}
	return nil // job not found is not an error
}

func (b *fleetSchedulerBridge) GetJob(id string) *fleet.SchedulerJob {
	sj := b.sched.Store().Get(id)
	if sj == nil {
		return nil
	}
	return &fleet.SchedulerJob{
		ID:      sj.ID,
		Name:    sj.Name,
		Mode:    string(sj.Mode),
		Cron:    sj.Schedule.Cron,
		Flow:    sj.Payload.Flow,
		Enabled: sj.Enabled,
	}
}

func (b *fleetSchedulerBridge) ValidateCron(expr string) error {
	return scheduler.ValidateCron(expr)
}

// fleetPlanRegistryAdapter adapts fleet.PlanRegistry to channels.FleetPlanRegistry.
type fleetPlanRegistryAdapter struct {
	reg *fleet.PlanRegistry
}

func (a *fleetPlanRegistryAdapter) ListPlans() []channels.FleetPlanSummary {
	plans := a.reg.ListPlans()
	result := make([]channels.FleetPlanSummary, len(plans))
	for i, p := range plans {
		result[i] = channels.FleetPlanSummary{
			Key:         p.Key,
			Name:        p.Name,
			Description: p.Description,
			ChannelType: p.ChannelType,
			AgentNames:  p.AgentNames,
		}
	}
	return result
}

// fleetTemplateRegistryAdapter adapts fleet.Registry to channels.FleetTemplateRegistry.
type fleetTemplateRegistryAdapter struct {
	reg *fleet.Registry
}

func (a *fleetTemplateRegistryAdapter) ListFleets() []channels.FleetTemplateSummary {
	fleets := a.reg.ListFleets()
	result := make([]channels.FleetTemplateSummary, len(fleets))
	for i, f := range fleets {
		result[i] = channels.FleetTemplateSummary{
			Key:         f.Key,
			Name:        f.Name,
			Description: f.Description,
			AgentNames:  f.AgentNames,
		}
	}
	return result
}

func (a *fleetTemplateRegistryAdapter) GetFleet(key string) (channels.FleetTemplateWithWizard, bool) {
	cfg, ok := a.reg.GetFleet(key)
	if !ok {
		return channels.FleetTemplateWithWizard{}, false
	}
	var wizardPrompt string
	if cfg.PlanWizard != nil {
		wizardPrompt = cfg.PlanWizard.SystemPrompt
	}
	return channels.FleetTemplateWithWizard{
		Name:               cfg.Name,
		WizardSystemPrompt: wizardPrompt,
	}, true
}
