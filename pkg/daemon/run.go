package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/channels"
	emailchan "github.com/schardosin/astonish/pkg/channels/email"
	slackchan "github.com/schardosin/astonish/pkg/channels/slack"
	"github.com/schardosin/astonish/pkg/channels/telegram"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	emailpkg "github.com/schardosin/astonish/pkg/email"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/mailer"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/sandbox"
	incus "github.com/schardosin/astonish/pkg/sandbox/incus"
	k8sbackend "github.com/schardosin/astonish/pkg/sandbox/k8s"
	"github.com/schardosin/astonish/pkg/scheduler"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/filestore"
	"github.com/schardosin/astonish/pkg/store/pgstore"
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
	// Determine runtime mode (default/api/worker) from ASTONISH_MODE env var.
	daemonMode := config.GetDaemonMode()

	// Load app config for provider/MCP setup
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate sandbox config early so invalid values are caught at startup
	// rather than producing cryptic Incus errors during container creation.
	if err := sandbox.ValidateSandboxConfig(&appCfg.Sandbox); err != nil {
		slog.Warn("invalid sandbox config, using defaults", "error", err)
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
	// In api/worker modes, fall back to stdout if file logging is unavailable
	// (stateless containers may not have a writable config directory).
	logDir := appCfg.Daemon.GetLogDir()
	var logger *Logger
	if daemonMode != config.DaemonModeDefault {
		var logErr error
		logger, logErr = NewLogger(logDir + "/daemon.log")
		if logErr != nil {
			// Container mode: fall back to stdout (captured by orchestrator)
			logger = NewStdoutLogger()
		}
	} else {
		var logErr error
		logger, logErr = NewLogger(logDir + "/daemon.log")
		if logErr != nil {
			return fmt.Errorf("failed to initialize logger: %w", logErr)
		}
	}
	defer logger.Close()

	// Redirect standard log to file
	log.SetOutput(logger)
	log.SetFlags(0) // Logger adds its own timestamps

	logger.Printf("Astonish daemon starting (port: %d, pid: %d, mode: %s)", port, os.Getpid(), daemonMode)

	// Write PID file (only in default mode — multi-instance modes skip it)
	var pidPath string
	if daemonMode == config.DaemonModeDefault {
		pidPath, err = DefaultPIDPath()
		if err != nil {
			return fmt.Errorf("failed to resolve PID path: %w", err)
		}
		if err := WritePID(pidPath); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
		defer RemovePID(pidPath)
	}

	// Set up provider environment variables (credential store → config → env fallback)
	configDir, err := config.GetConfigDir()
	if err != nil {
		slog.Warn("failed to get config directory", "error", err)
	}
	var credStore *credentials.Store
	isPlatformMode := appCfg.Storage.Backend == "postgres"

	// In platform container modes (api/worker), skip the file-based credential store
	// entirely when ASTONISH_MASTER_KEY is set. All secrets come from PG — the file
	// store would only produce warnings about unwritable filesystem.
	skipFileCredStore := isPlatformMode && daemonMode != config.DaemonModeDefault && os.Getenv("ASTONISH_MASTER_KEY") != ""

	if configDir != "" && !skipFileCredStore {
		if cs, csErr := credentials.Open(configDir); csErr == nil {
			credStore = cs
			config.SetInstalledSecretGetter(cs.GetSecret)
			api.SetAPICredentialStore(cs)

			if !isPlatformMode {
				// Personal mode only: auto-migrate secrets from config.yaml (one-time)
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
				// Personal mode: set up provider env vars from config.yaml + filesystem credential store
				config.SetupAllProviderEnvFromStore(appCfg, cs.GetSecret)
			}
		} else {
			logger.Printf("Warning: Failed to open credential store: %v", csErr)
			if !isPlatformMode {
				config.SetupAllProviderEnv(appCfg)
			}
		}
	} else if !isPlatformMode {
		config.SetupAllProviderEnv(appCfg)
	}

	// --- Initialize store.Services for dependency injection ---
	// Backend selection: "postgres" → platform mode, everything else → personal mode.
	var svc *store.Services
	var pgStore *pgstore.PGStore // non-nil only in platform mode

	if appCfg.Storage.Backend == "postgres" {
		// Platform mode: multi-tenant PostgreSQL storage.
		var pgErr error
		svc, pgStore, pgErr = pgstore.NewPlatformServices(context.Background(), appCfg.Storage.Postgres)
		if pgErr != nil {
			// Auto-init: if the platform DB is not yet initialized, attempt
			// BootstrapPlatform() and retry. This handles the case where
			// config was saved (e.g. by the setup wizard) but the user didn't
			// run `astonish platform init` separately.
			logger.Printf("Platform store init failed, attempting auto-bootstrap: %v", pgErr)
			if bootstrapErr := pgstore.BootstrapPlatform(context.Background(), appCfg.Storage.Postgres.GetPlatformDSN(), appCfg.Storage.Postgres.InstanceSuffix); bootstrapErr != nil {
				return fmt.Errorf("failed to initialize platform storage: %w (auto-bootstrap also failed: %v)", pgErr, bootstrapErr)
			}
			logger.Printf("Auto-bootstrap succeeded, retrying platform store init")
			svc, pgStore, pgErr = pgstore.NewPlatformServices(context.Background(), appCfg.Storage.Postgres)
			if pgErr != nil {
				return fmt.Errorf("failed to initialize platform storage after auto-bootstrap: %w", pgErr)
			}
		}
		defer pgStore.Close()
		logger.Printf("Storage backend: PostgreSQL (platform mode)")

		// Run pending migrations on all existing team/personal schemas.
		// This ensures that schema changes from new migrations (e.g., new columns)
		// are applied to schemas created before those migrations existed.
		if err := pgStore.MigrateAllSchemas(context.Background()); err != nil {
			logger.Printf("Warning: schema migration encountered errors: %v", err)
		}

		// Initialize embedding model for PG memory stores (hybrid vector+keyword search).
		// Uses the same HugotEmbedder (all-MiniLM-L6-v2, 384-dim) as personal mode.
		// Non-fatal: if embedding fails, PG stores fall back to keyword-only search.
		{
			embGetSecret := daemonSecretGetter(pgStore, appCfg, credStore)
			embResult, embErr := memory.ResolveEmbeddingFunc(appCfg, &appCfg.Memory, cfg.Debug, embGetSecret)
			if embErr != nil {
				logger.Printf("Warning: PG memory embedding unavailable (keyword-only search): %v", embErr)
			} else {
				pgStore.SetEmbedFunc(func(ctx context.Context, text string) ([]float32, error) {
					return embResult.EmbeddingFunc(ctx, text)
				})
				if embResult.Cleanup != nil {
					defer embResult.Cleanup()
				}
				logger.Printf("PG memory stores: hybrid vector+keyword search enabled")
			}
		}
	} else {
		// Personal mode: all stores backed by local filesystem.
		// The Services struct is populated incrementally as subsystems come online.
		svc = filestore.NewPersonalServices()
		logger.Printf("Storage backend: filesystem (personal mode)")
	}

	if credStore != nil && svc.Mode == store.ModePersonal {
		filestore.SetCredentialStore(svc, credStore)
	}

	// In platform mode, cascade platform and default-org provider settings
	// into appCfg so the channel/fleet agent sees all configured providers.
	// This is the daemon-level equivalent of effectiveAppConfig() in HTTP handlers.
	// Provider env vars are set here (not earlier) because in platform mode
	// providers come exclusively from the database, not config.yaml.
	if pgStore != nil {
		cascadePlatformProviders(context.Background(), pgStore, appCfg, logger)
		// Set up provider env vars from the DB-sourced config
		getSecret := daemonSecretGetter(pgStore, appCfg, credStore)
		config.SetupAllProviderEnvFromStore(appCfg, getSecret)
		// Override the installed secret getter to use platform_secrets.
		// This ensures IsStandardServerInstalled() and mergeStandardServers()
		// resolve API keys from the DB in platform mode (not the file-based store).
		config.SetInstalledSecretGetter(getSecret)
	}

	// Create the pgvector-backed ToolVectorStore for platform mode.
	// This enables dynamic tool injection (semantic tool discovery) in platform mode.
	var platformToolVectorStore agent.ToolVectorStore
	var platformEmbedFunc agent.EmbedFunc
	if pgStore != nil {
		if embedFunc := pgStore.GetEmbedFunc(); embedFunc != nil {
			pool, poolErr := pgStore.PoolManager().PlatformPool(context.Background())
			if poolErr == nil {
				vs, vsErr := pgstore.NewPGToolVectorStore(pool, embedFunc)
				if vsErr == nil {
					platformToolVectorStore = vs
					platformEmbedFunc = agent.EmbedFunc(embedFunc)
					logger.Printf("Tool discovery: pgvector-backed (platform mode)")
				} else if cfg.Debug {
					logger.Printf("Warning: failed to create PG tool vector store: %v", vsErr)
				}
			} else if cfg.Debug {
				logger.Printf("Warning: failed to get platform pool for tool index: %v", poolErr)
			}
		}
	}

	// Set up MCP environment variables
	if mcpCfg, err := config.LoadMCPConfig(); err == nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// --- Generate managed OpenCode config ---
	// This creates ~/.config/astonish/opencode.json from the current provider
	// settings so that OpenCode (used as a delegate tool in fleet sessions)
	// does not need independent configuration.
	// Skipped in API mode — API pods don't run fleet delegates.
	getSecret := daemonSecretGetter(pgStore, appCfg, credStore)
	if daemonMode != config.DaemonModeAPI {
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
	}

	// Initialize tools cache (personal mode only — platform mode reads from DB per-request)
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	if svc.Mode != store.ModePlatform {
		api.InitToolsCache(ctx)
	}

	// --- Initialize session persistence ---
	// In personal mode: a single shared FileStore handles all sessions.
	// In platform mode: sessions are in PostgreSQL (per-team schema).
	// The file store is only needed in personal mode.
	var sharedFileStore *persistentsession.FileStore
	if pgStore == nil {
		// Personal mode: file-based session persistence.
		if sessDir, dirErr := config.GetSessionsDir(&appCfg.Sessions); dirErr == nil {
			if store, fsErr := persistentsession.NewFileStore(sessDir); fsErr == nil {
				sharedFileStore = store
				api.SetFleetSessionStore(store)
				filestore.SetSessionStore(svc, store)
				logger.Printf("Session store initialized (%s)", sessDir)
			} else {
				logger.Printf("Warning: Failed to initialize session store: %v", fsErr)
			}
		} else {
			logger.Printf("Warning: Failed to resolve sessions directory: %v", dirErr)
		}
	} else {
		logger.Printf("Session store: PostgreSQL (platform mode)")
	}

	// --- Initialize device authorization for Studio ---
	var authManager *api.AuthManager
	var platformAuth *api.PlatformAuth

	if svc.Mode == store.ModePlatform && pgStore != nil {
		// Platform mode: JWT-based authentication
		platformAuth = api.NewPlatformAuth(appCfg.Storage.Auth, pgStore, appCfg.Storage)
		if appCfg.Storage.Auth.GetJWTSecret() == "" {
			logger.Printf("WARNING: No JWT secret configured (ASTONISH_JWT_SECRET env or storage.auth.jwt_secret config)")
			logger.Printf("A random key has been generated — tokens will not survive daemon restarts")
		}
		logger.Printf("Platform authentication enabled (mode: %s)", appCfg.Storage.Auth.Mode)
	} else if appCfg.Daemon.Auth.IsAuthEnabled() && configDir != "" {
		// Personal mode: device authorization flow
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

	// --- Start memory indexer (personal mode only) ---
	// In personal mode: the daemon maintains a chromem-go vector index on the
	// filesystem (~/.config/astonish/memory/vectors/). Studio's lazy-init skips
	// IndexAll when the daemon is running, trusting it to keep the index current.
	// In platform mode: memory is fully managed by PostgreSQL (per-team pgvector
	// tables). The embedding function for PG is already initialized above.
	var daemonIndexer *memory.DaemonIndexerResult
	if pgStore == nil {
		embGetSecret := daemonSecretGetter(pgStore, appCfg, credStore)
		di, diErr := memory.StartDaemonIndexer(ctx, appCfg, cfg.Debug, embGetSecret)
		if diErr != nil {
			logger.Printf("Warning: Memory indexer failed to start: %v", diErr)
		} else if di != nil {
			daemonIndexer = di
			defer di.Cleanup()
			// Wire memory store into Services (Store for vector search)
			filestore.SetMemoryStores(svc, di.Store, nil)
		}
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
			// Preserve sandbox containers for reconnection after restart,
			// then clean up everything else (LLM, embedder, MCP, etc.).
			// ShutdownSandbox marks containers as "already shut down" so
			// Cleanup() skips destructive container removal.
			if factoryResult.ShutdownSandbox != nil {
				factoryResult.ShutdownSandbox()
			}
			factoryResult.Cleanup()
		}
	}()

	needsChatAgent := appCfg.Channels.IsChannelsEnabled() && daemonMode != config.DaemonModeAPI

	if needsChatAgent {
		logger.Printf("Initializing ChatAgent for channels...")

		// Build a fully-wired ChatAgent for channel/scheduler use
		fr, factoryErr := launcher.NewWiredChatAgent(ctx, &launcher.ChatFactoryConfig{
			AppConfig:               appCfg,
			ProviderName:            appCfg.General.DefaultProvider,
			ModelName:               appCfg.General.DefaultModel,
			DebugMode:               cfg.Debug,
			AutoApprove:             true, // Channels/scheduler auto-approve all tools
			IsDaemon:                true, // We ARE the daemon — always run indexing/watchers.
			PlatformMode:            pgStore != nil,
			SessionStore:            sharedFileStore,
			DaemonIndexer:           daemonIndexer,
			PlatformToolVectorStore: platformToolVectorStore,
			PlatformEmbedFunc:       platformEmbedFunc,
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

		// Wire sandbox-aware file reader so channel document attachments can
		// read files from inside sandbox containers. Without this, os.ReadFile
		// fails for container-internal paths (e.g., /tmp/report.md).
		if sandbox.IsSandboxEnabled(&freshCfg.Sandbox) {
			mgr.SetReadFileFunc(func(sessionID, path string) ([]byte, error) {
				// Tier 1: try reading from the host filesystem.
				if data, err := os.ReadFile(path); err == nil {
					return data, nil
				}

				// Tier 2: pull from the session's sandbox container.
				registry, err := sandbox.NewSessionRegistry()
				if err != nil {
					return nil, fmt.Errorf("sandbox session registry unavailable: %w", err)
				}
				entry := registry.Get(sessionID)
				if entry == nil || entry.ContainerName == "" {
					return nil, fmt.Errorf("no sandbox container for session %s", sessionID)
				}
				client, err := incus.Connect(incus.DetectPlatform())
				if err != nil {
					return nil, fmt.Errorf("failed to connect to sandbox: %w", err)
				}
				reader, _, err := client.PullFile(entry.ContainerName, path)
				if err != nil {
					return nil, fmt.Errorf("failed to pull %s from container %s: %w", path, entry.ContainerName, err)
				}
				defer reader.Close()
				return io.ReadAll(reader)
			})
		}

		// Wire a browser provider for Chrome-based PDF generation. This gives
		// channel document attachments (e.g., Telegram) full Unicode/emoji support
		// and professional CSS-based layout when converting markdown to PDF.
		// Uses a dedicated headless browser to avoid interfering with the user's
		// browser session. Falls back to goldmark-pdf if Chrome is unavailable.
		pdfBrowserMgr := browser.NewManager(browser.DefaultConfig())
		mgr.BrowserPDF = pdfBrowserMgr
		defer pdfBrowserMgr.Cleanup()

		// In platform mode, overlay DB-stored channel configuration onto
		// the file-based config. PlatformSettings.Channels is the authoritative
		// source; config.yaml serves only as fallback for backward compatibility.
		if pgStore != nil {
			applyPlatformChannelConfig(pgStore, freshCfg, logger)
		}

		// Register Telegram if enabled
		var tgConfigError string
		if freshCfg.Channels.Telegram.IsTelegramEnabled() {
			botToken := freshCfg.Channels.Telegram.BotToken
			if botToken == "" {
				botToken = resolveDaemonSecret(pgStore, freshCfg, factoryResult.CredentialStore, "channels.telegram.bot_token")
			}
			if botToken == "" {
				tgConfigError = "bot token not configured"
				logger.Printf("Warning: Telegram enabled but no bot token found")
			} else {
				// In platform mode, the DB is the sole authority for allowlists.
				// Pass empty AllowFrom here; the background refresh populates it
				// from user_channels immediately after channel init.
				tgAllowFrom := freshCfg.Channels.Telegram.AllowFrom
				if pgStore != nil {
					tgAllowFrom = nil
				}
				tg := telegram.New(&telegram.Config{
					BotToken:  botToken,
					AllowFrom: tgAllowFrom,
					Commands:  mgr.Commands(),
				}, log.Default())
				mgr.Register(tg)
				logger.Printf("Telegram channel registered")
			}
		}

		// Register Email channel (inbound polling) only if explicitly enabled
		var emailConfigError string
		if freshCfg.Channels.Email.IsEmailEnabled() {
			emailPassword := freshCfg.Channels.Email.Password
			if emailPassword == "" {
				emailPassword = resolveDaemonSecret(pgStore, freshCfg, factoryResult.CredentialStore, "channels.email.password")
			}
			if emailPassword == "" {
				emailConfigError = "password not configured"
				logger.Printf("Warning: Email channel enabled but no password found")
			} else if freshCfg.Channels.Email.IMAPServer == "" || freshCfg.Channels.Email.SMTPServer == "" {
				emailConfigError = "IMAP/SMTP servers not configured"
				logger.Printf("Warning: Email channel enabled but IMAP/SMTP servers not configured")
			} else {
				pollInterval := time.Duration(freshCfg.Channels.Email.GetPollInterval()) * time.Second
				emailAllowFrom := freshCfg.Channels.Email.AllowFrom
				if pgStore != nil {
					emailAllowFrom = nil
				}
				em := emailchan.New(&emailchan.Config{
					Provider:     freshCfg.Channels.Email.Provider,
					IMAPServer:   freshCfg.Channels.Email.IMAPServer,
					SMTPServer:   freshCfg.Channels.Email.SMTPServer,
					Address:      freshCfg.Channels.Email.Address,
					Username:     freshCfg.Channels.Email.Username,
					Password:     emailPassword,
					PollInterval: pollInterval,
					AllowFrom:    emailAllowFrom,
					Folder:       freshCfg.Channels.Email.Folder,
					MarkRead:     freshCfg.Channels.Email.IsMarkRead(),
					MaxBodyChars: freshCfg.Channels.Email.MaxBodyChars,
					Commands:     mgr.Commands(),
				}, log.Default())
				// Inject thread index for per-thread email sessions.
				// Each email thread gets its own session; replies chain back
				// to the same session via In-Reply-To / References headers.
				if sharedFileStore != nil {
					em.SetThreadIndex(sharedFileStore.ThreadIndex())
				} else if pgStore != nil {
					// Platform mode: use PG-backed thread index
					if pool, poolErr := pgStore.PoolManager().PlatformPool(context.Background()); poolErr == nil {
						em.SetThreadIndex(pgstore.NewPGThreadIndex(pool))
					} else {
						logger.Printf("Warning: Failed to get platform pool for thread index: %v", poolErr)
					}
				}
				mgr.Register(em)
				logger.Printf("Email channel registered (%s)", freshCfg.Channels.Email.Address)
			}
		}

		// Register Slack if enabled
		var slackConfigError string
		if freshCfg.Channels.Slack.IsSlackEnabled() {
			botToken := freshCfg.Channels.Slack.BotToken
			if botToken == "" {
				botToken = resolveDaemonSecret(pgStore, freshCfg, factoryResult.CredentialStore, "channels.slack.bot_token")
			}
			appToken := freshCfg.Channels.Slack.AppToken
			if appToken == "" {
				appToken = resolveDaemonSecret(pgStore, freshCfg, factoryResult.CredentialStore, "channels.slack.app_token")
			}
			signingSecret := freshCfg.Channels.Slack.SigningSecret
			if signingSecret == "" {
				signingSecret = resolveDaemonSecret(pgStore, freshCfg, factoryResult.CredentialStore, "channels.slack.signing_secret")
			}

			mode := freshCfg.Channels.Slack.GetMode()
			if mode == "socket" && botToken == "" {
				slackConfigError = "bot_token not configured"
				logger.Printf("Warning: Slack channel enabled (socket mode) but no bot token found")
			} else if mode == "socket" && appToken == "" {
				slackConfigError = "app_token not configured for socket mode"
				logger.Printf("Warning: Slack channel enabled (socket mode) but no app token found")
			} else if mode == "events" && signingSecret == "" {
				slackConfigError = "signing_secret not configured for events mode"
				logger.Printf("Warning: Slack channel enabled (events mode) but no signing secret found")
			} else {
				slAllowFrom := freshCfg.Channels.Slack.AllowFrom
				if pgStore != nil {
					slAllowFrom = nil
				}
				sl := slackchan.New(&slackchan.Config{
					Mode:         mode,
					BotToken:     botToken,
					AppToken:     appToken,
					SigningSecret: signingSecret,
					AllowFrom:    slAllowFrom,
					Commands:     mgr.Commands(),
				}, log.Default())
				mgr.Register(sl)
				logger.Printf("Slack channel registered (mode: %s)", mode)
			}
		}

		if err := mgr.StartAll(ctx); err != nil {
			return mgr, fmt.Errorf("failed to start channels: %w", err)
		}
		logger.Printf("Channels started")

		// Report channel config statuses to the API layer so that
		// GET /api/channels/status includes enabled-but-not-started channels.
		cfgStatuses := map[string]api.ChannelConfigStatus{}
		if freshCfg.Channels.Telegram.IsTelegramEnabled() {
			cfgStatuses["telegram"] = api.ChannelConfigStatus{Enabled: true, Error: tgConfigError}
		}
		if freshCfg.Channels.Email.IsEmailEnabled() {
			cfgStatuses["email"] = api.ChannelConfigStatus{Enabled: true, Error: emailConfigError}
		}
		if freshCfg.Channels.Slack.IsSlackEnabled() {
			cfgStatuses["slack"] = api.ChannelConfigStatus{Enabled: true, Error: slackConfigError}
		}
		api.SetChannelConfigStatuses(cfgStatuses)

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
		if emailPassword == "" {
			emailPassword = resolveDaemonSecret(pgStore, cfg, factoryResult.CredentialStore, "channels.email.password")
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

	// --- Dynamic allowlist from user_channels (platform mode) ---
	// In platform mode, channel allowlists are built exclusively from the
	// user_channels table (DB is the sole authority). Static config.yaml
	// allow_from entries are ignored — they are a personal-mode concept.
	// A background goroutine refreshes the allowlists every 60 seconds.
	var refreshAllowlistStop context.CancelFunc
	if pgStore != nil && channelMgr != nil {
		refreshAllowlist := func() {
			bgCtx := context.Background()
			allowlists := make(map[string][]string)

			// Telegram allowlist (from DB only)
			tgLinks, err := pgStore.UserChannels().ListByChannelType(bgCtx, "telegram")
			if err != nil {
				logger.Printf("[channels] Failed to refresh Telegram allowlist from DB: %v", err)
			} else {
				ids := make([]string, 0, len(tgLinks))
				for _, link := range tgLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["telegram"] = ids
			}

			// Slack allowlist (from DB only)
			slLinks, err := pgStore.UserChannels().ListByChannelType(bgCtx, "slack")
			if err != nil {
				logger.Printf("[channels] Failed to refresh Slack allowlist from DB: %v", err)
			} else {
				ids := make([]string, 0, len(slLinks))
				for _, link := range slLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["slack"] = ids
			}

			// Email allowlist (from DB only)
			emLinks, err := pgStore.UserChannels().ListByChannelType(bgCtx, "email")
			if err != nil {
				logger.Printf("[channels] Failed to refresh Email allowlist from DB: %v", err)
			} else {
				ids := make([]string, 0, len(emLinks))
				for _, link := range emLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["email"] = ids
			}

			channelMgr.UpdateAllowlists(allowlists)
		}

		// Initial refresh
		refreshAllowlist()

		// Periodic refresh every 60s
		refreshCtx, refreshCancel := context.WithCancel(ctx)
		refreshAllowlistStop = refreshCancel
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-refreshCtx.Done():
					return
				case <-ticker.C:
					refreshAllowlist()
				}
			}
		}()
	}
	_ = refreshAllowlistStop // used at shutdown

	// Set platform resolver for inbound channel messages (platform mode only).
	// This enables team-scoped context injection when users message via Telegram.
	if pgStore != nil && channelMgr != nil {
		resolver := &channelPlatformResolver{pgStore: pgStore}
		// Wire the cross-session memory merge function if PlatformReflector is available.
		if factoryResult != nil && factoryResult.ChatAgent != nil && factoryResult.ChatAgent.PlatformReflector != nil {
			resolver.memorySaveOrMerge = factoryResult.ChatAgent.PlatformReflector.MemorySaveOrMergeFunc()
		}
		channelMgr.SetPlatformResolver(resolver)
	}

	// --- Link code store for code-based channel linking (platform mode) ---
	// Allows users to link their Telegram/Slack by sending /link <code> to the bot.
	// In platform mode, use PG-backed store for stateless horizontal scaling.
	if pgStore != nil && channelMgr != nil {
		var linkStore api.LinkCodeBackend
		if pool, poolErr := pgStore.PoolManager().PlatformPool(context.Background()); poolErr == nil {
			linkStore = api.NewPGLinkCodeBackend(pgstore.NewPGLinkCodeStore(pool))
		} else {
			linkStore = api.NewLinkCodeStore()
		}
		api.SetLinkCodeStore(linkStore)

		// Set the link handler on the Telegram channel — bridges /link commands
		// to the link code store and user_channels DB.
		channelMgr.SetTelegramLinkHandler(buildTelegramLinkHandler(pgStore, linkStore, channelMgr))

		// Set the link handler on the Slack channel.
		channelMgr.SetSlackLinkHandler(buildSlackLinkHandler(pgStore, linkStore, channelMgr))
	}

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
				AppConfig:               freshCfg,
				ProviderName:            freshCfg.General.DefaultProvider,
				ModelName:               freshCfg.General.DefaultModel,
				DebugMode:               cfg.Debug,
				AutoApprove:             true,
				PlatformMode:            pgStore != nil,
				SessionStore:            sharedFileStore,
				PlatformToolVectorStore: platformToolVectorStore,
				PlatformEmbedFunc:       platformEmbedFunc,
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

		// Refresh dynamic allowlist after channel reload (platform mode).
		// DB is the sole authority — static config allow_from is ignored.
		if pgStore != nil && channelMgr != nil {
			bgCtx := context.Background()
			allowlists := make(map[string][]string)

			// Telegram
			tgLinks, linkErr := pgStore.UserChannels().ListByChannelType(bgCtx, "telegram")
			if linkErr == nil {
				ids := make([]string, 0, len(tgLinks))
				for _, link := range tgLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["telegram"] = ids
			}

			// Slack
			slLinks, linkErr := pgStore.UserChannels().ListByChannelType(bgCtx, "slack")
			if linkErr == nil {
				ids := make([]string, 0, len(slLinks))
				for _, link := range slLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["slack"] = ids
			}

			// Email
			emLinks, linkErr := pgStore.UserChannels().ListByChannelType(bgCtx, "email")
			if linkErr == nil {
				ids := make([]string, 0, len(emLinks))
				for _, link := range emLinks {
					ids = append(ids, link.ExternalID)
				}
				allowlists["email"] = ids
			}

			channelMgr.UpdateAllowlists(allowlists)
		}

		// Re-attach platform resolver after reload
		if pgStore != nil && channelMgr != nil {
			resolver := &channelPlatformResolver{pgStore: pgStore}
			if factoryResult != nil && factoryResult.ChatAgent != nil && factoryResult.ChatAgent.PlatformReflector != nil {
				resolver.memorySaveOrMerge = factoryResult.ChatAgent.PlatformReflector.MemorySaveOrMergeFunc()
			}
			channelMgr.SetPlatformResolver(resolver)
		}

		// Re-attach link handlers after reload
		if pgStore != nil && channelMgr != nil {
			linkStore := api.GetLinkCodeStore()
			if linkStore != nil {
				channelMgr.SetTelegramLinkHandler(buildTelegramLinkHandler(pgStore, linkStore, channelMgr))
				channelMgr.SetSlackLinkHandler(buildSlackLinkHandler(pgStore, linkStore, channelMgr))
			}
		}

		logger.Printf("Channel reload complete")
		return nil
	}
	api.SetChannelReloadFunc(reloadChannels)

	// --- Start config file watcher for hot-reload ---
	configPath, configPathErr := config.GetConfigPath()
	if configPathErr != nil {
		logger.Printf("Warning: Failed to resolve config path for watcher: %v", configPathErr)
	} else {
		go func() {
			if err := WatchConfig(ctx, configPath, ConfigWatcherOpts{
				DebounceMs:     1500,
				Logger:         logger,
				GetManager:     func() *channels.ChannelManager { return channelMgr },
				ReloadChannels: reloadChannels,
				LastConfig:     appCfg,
			}); err != nil {
				logger.Printf("Warning: Config watcher stopped: %v", err)
			}
		}()
	}

	// --- Initialize scheduler if enabled ---
	// Skipped in API mode — only default and worker modes run the scheduler.
	var sched *scheduler.Scheduler
	var mtSched *MultiTenantScheduler
	var schedExec *scheduler.Executor

	// fleetSessionStore holds the PG-backed session store used for fleet
	// sessions in platform mode. In personal mode it remains nil, and
	// fleet session metadata is written to the file-based store instead.
	var fleetSessionStore store.SessionStore

	if appCfg.Scheduler.IsSchedulerEnabled() && daemonMode != config.DaemonModeAPI {
		var jobStore scheduler.JobStore

		if svc.Mode == store.ModePlatform && pgStore != nil {
			// Platform mode: use the multi-tenant scheduler that iterates
			// all orgs → all teams on every tick. Individual job CRUD goes
			// through the request-scoped store.SchedulerStore in API handlers.

			// Create executor with injected headless runner
			schedExec = &scheduler.Executor{
				AppConfig:    appCfg,
				ProviderName: appCfg.General.DefaultProvider,
				ModelName:    appCfg.General.DefaultModel,
				DebugMode:    cfg.Debug,
				RunHeadless:  makeHeadlessRunner(),
				// FlowResolver is nil — multi-tenant path uses context-injected FlowStore
			}
			if factoryResult != nil {
				schedExec.ChatAgent = factoryResult.ChatAgent
				schedExec.SessionService = factoryResult.SessionService
			}

		// Create delivery function — uses a getter to always resolve the
		// current channelMgr, surviving channel reloads without stale closures.
		// In platform mode, use the full delivery resolver for owner/team/members modes.
		deliver := scheduler.NewPlatformDeliverFunc(func() *channels.ChannelManager {
			return channelMgr
		}, &pgDeliveryResolver{pgStore: pgStore}, log.Default())

			// Create and start the multi-tenant scheduler
			mtSched = NewMultiTenantScheduler(pgStore, schedExec, deliver, log.Default())
			mtSched.Start(ctx)

			// Register executor for API RunNow handler
			api.SetExecutor(schedExec)

			// Resolve default org/team for fleet session store (backward compat)
			orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
			if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
				teamStore := orgStore.ForTeam("general")
				fleetSessionStore = teamStore.Sessions()
			}

			logger.Printf("Scheduler: multi-tenant (all orgs/teams)")
		}

		if svc.Mode != store.ModePlatform || pgStore == nil {
			// Personal mode: use file-based JSON store with single-instance scheduler.
			storePath, storeErr := scheduler.DefaultStorePath()
			if storeErr != nil {
				logger.Printf("Warning: Failed to resolve scheduler store path: %v", storeErr)
			} else {
				fileStore, storeErr := scheduler.NewStore(storePath)
				if storeErr != nil {
					logger.Printf("Warning: Failed to initialize scheduler store: %v", storeErr)
				} else {
					filestore.SetSchedulerStore(svc, fileStore)
					jobStore = fileStore
				}
			}

			if jobStore != nil {
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

			// Create delivery function — uses a getter to always resolve the
			// current channelMgr, surviving channel reloads without stale closures.
			deliver := scheduler.NewDeliverFunc(func() *channels.ChannelManager {
				return channelMgr
			})

				sched = scheduler.New(jobStore, schedExec.Execute, deliver, log.Default())
				sched.Start(ctx)

				// Make scheduler available to API handlers and LLM tools (personal mode)
				api.SetScheduler(sched)
				api.SetExecutor(schedExec)
				tools.SetSchedulerAccess(newSchedulerBridge(sched))
			}
		}
	}

	// --- Initialize fleet plan activator ---
	// This bridges fleet plans to the scheduler for automated polling.
	// Requires the scheduler to be initialized.
	// In personal mode, also requires the plan registry to be initialized.
	// In platform mode, fleet plans are read from the database.
	// Skipped in API mode — only default and worker modes run fleet monitors.
	schedulerAvailable := sched != nil || mtSched != nil
	if schedulerAvailable && daemonMode != config.DaemonModeAPI {
		planRegAvailable := api.GetFleetPlanRegistry() != nil || pgStore != nil
		if planRegAvailable {
			// In platform mode without a personal-mode scheduler, create a
			// temporary single-team scheduler bridge for fleet plan management.
			// Fleet plans are org-level resources that get stored in the default team.
			var fleetSchedBridge *fleetSchedulerBridge
			if sched != nil {
				fleetSchedBridge = newFleetSchedulerBridge(sched)
			} else {
				// Platform mode: create a scheduler backed by the default team's store
				// for fleet plan job management.
				orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
				if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
					teamStore := orgStore.ForTeam("general")
					fleetJobStore := newPGSchedulerAdapter(teamStore.ScheduledJobs())
					fleetSched := scheduler.New(fleetJobStore, schedExec.Execute, nil, log.Default())
					fleetSchedBridge = newFleetSchedulerBridge(fleetSched)
				}
			}

			if fleetSchedBridge != nil {
				fleetStarter := func(fCtx context.Context, fCfg fleet.HeadlessFleetConfig) (string, error) {
					// Resolve tenant-scoped stores for the fleet session so sub-agents
					// can access team drills, credentials, skills, etc. in platform mode.
					var fleetStores *api.FleetStores
					if pgStore != nil && fCfg.TeamSlug != "" {
						orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
						if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
							teamStore := orgStore.ForTeam(fCfg.TeamSlug)
							fleetStores = api.FleetStoresFromTeam(teamStore, orgStore, pgStore.PlatformMCPServers())
						}
					}
					return api.StartHeadlessFleetSession(fCtx, fCfg, fleetSessionStore, fleetStores)
				}
				// Wrap the plan registry getter to satisfy fleet.PlanAccess interface.
				// In personal mode, returns the file-based PlanRegistry.
				// In platform mode (when pgStore != nil), returns a DB-backed adapter.
				planAccessFn := func() fleet.PlanAccess {
					if pgStore != nil {
						orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
						if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
							teamStore := orgStore.ForTeam("general")
							// Get the org pool for the monitor state store
							orgPool, poolErr := pgStore.PoolManager().OrgPool(context.Background(), orgSlug)
							if poolErr != nil {
								slog.Warn("failed to get org pool for monitor state", "error", poolErr)
								return nil
							}
							return &dbPlanAccessAdapter{
								store:        teamStore.FleetPlans(),
								monitorStore: pgstore.NewPGMonitorStateStore(orgPool, pgstore.TeamSchemaName("general")),
							}
						}
					}
					reg := api.GetFleetPlanRegistry()
					if reg == nil {
						return nil
					}
					return reg
				}
				activator := fleet.NewPlanActivator(planAccessFn, fleetSchedBridge, fleetStarter)
				// Set the team slug so headless fleet sessions know which team's
				// stores to resolve. Matches the team used for plan storage above.
				activator.TeamSlug = "general"

				// Wire the poll function into the scheduler executor
				if schedExec != nil {
					schedExec.FleetPoll = activator.Poll
				}

				// Wire the recover function for session recovery after restart
				activator.SetRecoverFunc(func(rCtx context.Context, rCfg fleet.RecoverFleetConfig) error {
					// Resolve tenant-scoped stores for the recovered fleet session.
					var fleetStores *api.FleetStores
					if pgStore != nil && rCfg.TeamSlug != "" {
						orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
						if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
							teamStore := orgStore.ForTeam(rCfg.TeamSlug)
							fleetStores = api.FleetStoresFromTeam(teamStore, orgStore, pgStore.PlatformMCPServers())
						}
					}
					return api.RecoverFleetSession(rCtx, rCfg, fleetSessionStore, fleetStores)
				})

				// Wire credential-based GitHub token resolver for fleet plans.
				// When a plan has credentials: { github: "some-store-entry" }, this
				// resolver extracts the token from the encrypted credential store so
				// it can be injected as GH_TOKEN into gh CLI commands.
				if pgStore != nil {
					// Platform mode: resolve from PG team credential store.
					pgStoreRef := pgStore
					activator.SetGHTokenResolver(func(plan *fleet.FleetPlan) string {
						orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
						orgStore, orgErr := pgStoreRef.ForOrg(orgSlug)
						if orgErr != nil {
							slog.Warn("failed to get org store for fleet credentials", "plan", plan.Key, "error", orgErr)
							return ""
						}
						teamStore := orgStore.ForTeam("general")
						cs := teamStore.Credentials()
						resolved, err := fleet.ResolveCredentialsPlatform(context.Background(), plan, cs)
						if err != nil {
							slog.Warn("failed to resolve fleet credentials (platform)", "plan", plan.Key, "error", err)
						}
						return fleet.GitHubToken(resolved)
					})
				} else if credStore != nil {
					// Personal mode: resolve from file-based credential store.
					activator.SetGHTokenResolver(func(plan *fleet.FleetPlan) string {
						resolved, err := fleet.ResolveCredentials(plan, credStore)
						if err != nil {
							slog.Warn("failed to resolve fleet credentials", "plan", plan.Key, "error", err)
						}
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
	}

	// --- Wire fleet commands into channels ---
	// Must happen after both channelMgr and fleet registries are initialized.
	if channelMgr != nil {
		channelMgr.SetFleetDeps(&channels.FleetDeps{
			GetSessionRegistry: func() channels.FleetSessionRegistry {
				return api.GetFleetSessionRegistry()
			},
			GetPlanRegistry: func() channels.FleetPlanRegistry {
				// Platform mode: use DB store
				if pgStore != nil {
					orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
					if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
						teamStore := orgStore.ForTeam("general")
						return &dbFleetPlanRegistryAdapter{store: teamStore.FleetPlans()}
					}
				}
				// Personal mode: use file-based registry
				reg := api.GetFleetPlanRegistry()
				if reg == nil {
					return nil
				}
				return &fleetPlanRegistryAdapter{reg: reg}
			},
			GetFleetRegistry: func() channels.FleetTemplateRegistry {
				// Platform mode: use DB store
				if pgStore != nil {
					orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
					if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
						teamStore := orgStore.ForTeam("general")
						return &dbFleetTemplateRegistryAdapter{store: teamStore.FleetTemplates()}
					}
				}
				// Personal mode: use file-based registry
				reg := api.GetFleetRegistry()
				if reg == nil {
					return nil
				}
				return &fleetTemplateRegistryAdapter{reg: reg}
			},
			StartSessionFromPlan: func(planKey, initialMessage string) (*channels.FleetSessionStartResult, error) {
				// Resolve fleet plan store for platform mode
				var fleetPlanStore store.FleetPlanStore
				var fleetStores *api.FleetStores
				if pgStore != nil {
					orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
					if orgStore, orgErr := pgStore.ForOrg(orgSlug); orgErr == nil {
						teamStore := orgStore.ForTeam("general")
						fleetPlanStore = teamStore.FleetPlans()
						fleetStores = api.FleetStoresFromTeam(teamStore, orgStore, pgStore.PlatformMCPServers())
					}
				}
				result, err := api.StartFleetSessionFromPlan(planKey, initialMessage, api.DefaultUserID(), "", nil, nil, "", fleetPlanStore, fleetStores)
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

	// Start periodic cleanup goroutine (session expiry + orphan container pruning)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		runPeriodicCleanup(ctx, appCfg, sharedFileStore, logger)
	}()

	// Start transient table cleanup in platform mode (device_sessions, pending_link_codes).
	// Runs every 5 minutes — these tables have short TTLs (5-10 min) and expired rows
	// accumulate if no cleanup runs. Safe to run on any/all pods (DELETE is idempotent).
	if pgStore != nil {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					bgCtx := context.Background()
					pool, poolErr := pgStore.PoolManager().PlatformPool(bgCtx)
					if poolErr != nil {
						continue
					}
					_, _ = pool.Exec(bgCtx, `DELETE FROM device_sessions WHERE expires_at < now()`)
					_, _ = pool.Exec(bgCtx, `DELETE FROM pending_link_codes WHERE expires_at < now()`)
				}
			}
		}()
	}

	// Start sandbox GC reconciler in platform mode with K8s backend.
	// Periodically reclaims orphan layers, uppers, and staging directories.
	if pgStore != nil && sandbox.BackendKind(appCfg.Sandbox.BackendKind()) == sandbox.BackendKindK8s {
		platformPool, poolErr := pgStore.PoolManager().PlatformPool(ctx)
		if poolErr == nil {
			cs, _, csErr := k8sbackend.NewClientFromOptions(k8sbackend.LoadConfigOptions{
				KubeconfigPath: appCfg.Sandbox.Kubernetes.KubeconfigPath,
				Context:        appCfg.Sandbox.Kubernetes.Context,
				InCluster:      appCfg.Sandbox.Kubernetes.InCluster,
			})
			if csErr == nil {
				gcNamespace := appCfg.Sandbox.Kubernetes.Namespace
				if gcNamespace == "" {
					gcNamespace = "astonish-sandbox"
				}
				go k8sbackend.RunGCReconciler(ctx, k8sbackend.GCReconcilerConfig{
					Interval:         time.Hour,
					LayerGracePeriod: 24 * time.Hour,
					UpperGracePeriod: time.Hour,
					Namespace:        gcNamespace,
					LayersPVCName:    appCfg.Sandbox.Kubernetes.LayersPVCName,
					UppersPVCName:    appCfg.Sandbox.Kubernetes.UppersPVCName,
					Client:           cs,
					PlatformPool:     platformPool,
					PGStore:          pgStore,
					Layers:           pgStore.SandboxLayers(),
				})
			} else {
				logger.Printf("[gc-reconciler] Failed to create K8s client: %v (GC reconciler disabled)", csErr)
			}
		}
	}

	// Create and start the Studio server
	var studioOpts []launcher.StudioOption
	studioOpts = append(studioOpts, launcher.WithServices(svc))
	if platformAuth != nil && pgStore != nil {
		studioOpts = append(studioOpts, launcher.WithPlatformAuth(platformAuth, pgStore))
	} else if authManager != nil {
		studioOpts = append(studioOpts, launcher.WithAuth(authManager))
	}
	if configDir != "" {
		studioOpts = append(studioOpts, launcher.WithConfigDir(configDir))
	}
	if sharedFileStore != nil {
		studioOpts = append(studioOpts, launcher.WithSessionStore(sharedFileStore))
	}
	if daemonIndexer != nil {
		studioOpts = append(studioOpts, launcher.WithDaemonIndexer(daemonIndexer))
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

	// Cancel the main context to stop background goroutines (cleanup, etc.)
	ctxCancel()

	// Stop scheduler first (finish in-flight jobs)
	if sched != nil {
		logger.Printf("Stopping scheduler...")
		sched.Stop()
	}
	if mtSched != nil {
		logger.Printf("Stopping multi-tenant scheduler...")
		mtSched.Stop()
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
		var agentCfg *config.AgentConfig
		var err error

		if cfg.FlowYAML != "" {
			// Platform mode: flow YAML was resolved from the store.
			agentCfg, err = config.LoadAgentFromBytes([]byte(cfg.FlowYAML))
			if err != nil {
				return "", fmt.Errorf("failed to parse flow YAML: %w", err)
			}
		} else {
			// Personal mode: load from filesystem path.
			agentCfg, err = config.LoadAgent(cfg.FlowPath)
			if err != nil {
				return "", fmt.Errorf("failed to load flow %q: %w", cfg.FlowPath, err)
			}
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
		ID:             sj.ID,
		Name:           sj.Name,
		Mode:           string(sj.Mode),
		Cron:           sj.Schedule.Cron,
		Timezone:       sj.Schedule.Timezone,
		Flow:           sj.Payload.Flow,
		Params:         sj.Payload.Params,
		Instr:          sj.Payload.Instructions,
		Channel:        sj.Delivery.Channel,
		Target:         sj.Delivery.Target,
		DeliveryMode:   string(sj.Delivery.Mode),
		MemberIDs:      sj.Delivery.MemberIDs,
		ChannelFilter:  sj.Delivery.ChannelFilter,
		MemberChannels: sj.Delivery.MemberChannels,
		Enabled:        sj.Enabled,
		LastRun:        sj.LastRun,
		LastStatus:     string(sj.LastStatus),
		NextRun:        sj.NextRun,
		Failures:       sj.ConsecutiveFailures,
		CreatedAt:      sj.CreatedAt,
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
		slog.Warn("failed to create email client for tools", "error", err)
		return
	}
	tools.SetEmailClient(client)
	mailer.Init(client)
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
	slog.Info("adding fleet scheduler job", "component", "fleet-sched-bridge", "name", job.Name, "mode", job.Mode, "cron", job.Cron, "flow", job.Flow, "enabled", job.Enabled)
	if err := b.sched.Store().Add(sj); err != nil {
		return err
	}
	job.ID = sj.ID
	b.sched.RefreshNextRun(sj.ID)
	slog.Info("fleet scheduler job created", "component", "fleet-sched-bridge", "id", sj.ID)
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

func (b *fleetSchedulerBridge) GetJobByName(name string) *fleet.SchedulerJob {
	sj := b.sched.Store().GetByName(name)
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

// dbPlanAccessAdapter wraps a store.FleetPlanStore to satisfy fleet.PlanAccess
// for use by the PlanActivator in platform mode.
type dbPlanAccessAdapter struct {
	store        store.FleetPlanStore
	monitorStore fleet.MonitorStateStore
}

func (a *dbPlanAccessAdapter) GetPlan(key string) (*fleet.FleetPlan, bool) {
	planAny, ok := a.store.GetPlan(context.Background(), key)
	if !ok {
		return nil, false
	}
	plan, ok := planAny.(*fleet.FleetPlan)
	return plan, ok
}

func (a *dbPlanAccessAdapter) Save(plan *fleet.FleetPlan) error {
	return a.store.Save(context.Background(), plan)
}

func (a *dbPlanAccessAdapter) ListPlans() []fleet.PlanSummary {
	summaries := a.store.ListPlans(context.Background())
	result := make([]fleet.PlanSummary, len(summaries))
	for i, s := range summaries {
		result[i] = fleet.PlanSummary{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			CreatedFrom: s.CreatedFrom,
			ChannelType: s.ChannelType,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
		}
	}
	return result
}

func (a *dbPlanAccessAdapter) MonitorStateStore() fleet.MonitorStateStore {
	return a.monitorStore
}

// Compile-time check
var _ fleet.PlanAccess = (*dbPlanAccessAdapter)(nil)

// dbFleetPlanRegistryAdapter adapts store.FleetPlanStore to channels.FleetPlanRegistry.
// Used in platform mode when FleetDeps needs to list plans from the DB.
type dbFleetPlanRegistryAdapter struct {
	store store.FleetPlanStore
}

func (a *dbFleetPlanRegistryAdapter) ListPlans() []channels.FleetPlanSummary {
	plans := a.store.ListPlans(context.Background())
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

// dbFleetTemplateRegistryAdapter adapts store.FleetTemplateStore to channels.FleetTemplateRegistry.
// Used in platform mode when FleetDeps needs to list/get fleet templates from the DB.
type dbFleetTemplateRegistryAdapter struct {
	store store.FleetTemplateStore
}

func (a *dbFleetTemplateRegistryAdapter) ListFleets() []channels.FleetTemplateSummary {
	fleets := a.store.ListFleets(context.Background())
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

func (a *dbFleetTemplateRegistryAdapter) GetFleet(key string) (channels.FleetTemplateWithWizard, bool) {
	cfgAny, ok := a.store.GetFleet(context.Background(), key)
	if !ok {
		return channels.FleetTemplateWithWizard{}, false
	}
	cfg, ok := cfgAny.(*fleet.FleetConfig)
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

// runPeriodicCleanup runs session expiry and orphan container pruning on a timer.
// It runs once shortly after startup, then periodically based on config.
func runPeriodicCleanup(ctx context.Context, appCfg *config.AppConfig, sessionStore *persistentsession.FileStore, logger *Logger) {
	// Initial delay to avoid slowing down startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	// Run once immediately after the initial delay
	runCleanupCycle(appCfg, sessionStore, logger)

	// Then run periodically
	interval := time.Duration(appCfg.Sandbox.Prune.OrphanCheckHours) * time.Hour
	if interval <= 0 {
		interval = 6 * time.Hour // default
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCleanupCycle(appCfg, sessionStore, logger)
		}
	}
}

// runCleanupCycle performs a single cleanup pass: expired sessions + orphan containers.
func runCleanupCycle(appCfg *config.AppConfig, sessionStore *persistentsession.FileStore, logger *Logger) {
	// Reload config to pick up changes without restart
	freshCfg, err := config.LoadAppConfig()
	if err == nil {
		appCfg = freshCfg
	}

	// 1. Clean up expired sessions
	if sessionStore != nil {
		maxAge := appCfg.Sessions.Cleanup.EffectiveMaxAgeDays()
		if maxAge > 0 {
			deletedIDs := sessionStore.CleanupExpiredSessions(maxAge)
			// Destroy sandbox containers for deleted sessions
			for _, id := range deletedIDs {
				sandbox.TryDestroySession(appCfg, id)
			}
		}
	}

	// 2. Clean up orphaned app databases (no matching .yaml, older than 7 days)
	cleaned := api.CleanupOrphanAppDBs(7 * 24 * time.Hour)
	if cleaned > 0 {
		logger.Printf("[cleanup] Removed %d orphaned app database(s)", cleaned)
	}

	// 3. Prune orphan sandbox containers
	if sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		var liveSessionIDs map[string]bool
		if sessionStore != nil {
			liveSessionIDs = sessionStore.AllSessionIDs()
		}

		kind := sandbox.BackendKind(appCfg.Sandbox.BackendKind())
		switch kind {
		case sandbox.BackendKindK8s:
			b, cleanup, bErr := sandbox.BackendFromAppConfig(appCfg)
			if bErr == nil {
				if cleanup != nil {
					defer cleanup()
				}
				registry, regErr := sandbox.NewSessionRegistry()
				if regErr == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					pruned, _ := sandbox.PruneOrphansForBackend(ctx, b, registry, liveSessionIDs)
					cancel()
					if pruned > 0 {
						logger.Printf("[cleanup] Pruned %d orphaned sandbox pod(s)", pruned)
					}
				}
			}
		default:
			// Incus path (backward-compatible default)
			platform := incus.DetectPlatform()
			if platform != incus.PlatformUnsupported {
				incus.SetActivePlatform(platform)
				client, connErr := incus.Connect(platform)
				if connErr == nil {
					registry, regErr := sandbox.NewSessionRegistry()
					if regErr == nil {
						pruned, _ := sandbox.PruneOrphans(client, registry, liveSessionIDs)
						if pruned > 0 {
							logger.Printf("[cleanup] Pruned %d orphaned sandbox container(s)", pruned)
						}
					}
				}
			}
		}
	}
}

// cascadePlatformProviders merges platform-level and default-org provider
// settings from the PostgreSQL store into appCfg. This gives the daemon-level
// channel/fleet agent access to providers configured via the admin UI.
func cascadePlatformProviders(ctx context.Context, pgStore *pgstore.PGStore, appCfg *config.AppConfig, logger *Logger) {
	// Platform mode: providers come exclusively from the database.
	// Clear any config.yaml residue so the DB is the sole source of truth.
	appCfg.Providers = nil
	appCfg.General.DefaultProvider = ""
	appCfg.General.DefaultModel = ""

	// Layer 1: Platform settings
	psStore := pgStore.PlatformSettings()
	if psStore != nil {
		if ps, err := psStore.Get(ctx); err == nil && ps != nil {
			applyProviderLayer(appCfg, ps.Providers, ps.DefaultProvider, ps.DefaultModel)
			logger.Printf("Platform providers cascaded (%d providers)", len(ps.Providers))
		}
	}

	// Layer 2: Default org settings
	orgSlug := appCfg.Storage.Auth.GetDefaultOrgSlug()
	if orgSlug != "" {
		osStore := pgStore.OrgSettings(orgSlug)
		if osStore != nil {
			if os, err := osStore.Get(ctx); err == nil && os != nil {
				applyProviderLayer(appCfg, os.Providers, os.DefaultProvider, os.DefaultModel)
				logger.Printf("Org '%s' providers cascaded (%d providers)", orgSlug, len(os.Providers))
			}
		}
	}
}

// applyProviderLayer merges a provider configuration layer into the app config.
// Providers are additive by name; defaults override only if non-empty.
func applyProviderLayer(appCfg *config.AppConfig, providers map[string]store.ProviderConfig, defaultProvider, defaultModel string) {
	if defaultProvider != "" {
		appCfg.General.DefaultProvider = defaultProvider
	}
	if defaultModel != "" {
		appCfg.General.DefaultModel = defaultModel
	}
	if len(providers) > 0 {
		if appCfg.Providers == nil {
			appCfg.Providers = make(map[string]config.ProviderConfig)
		}
		for name, provCfg := range providers {
			appCfg.Providers[name] = config.ProviderConfig(provCfg)
		}
	}
}
