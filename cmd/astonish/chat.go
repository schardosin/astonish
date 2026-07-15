package astonish

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/client"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/launcher"
	"github.com/SAP/astonish/pkg/sandbox"
	persistentsession "github.com/SAP/astonish/pkg/session"
)

// SessionPinStore is the minimal surface the chat command needs to persist
// per-session provider/model pins. It intentionally mirrors the shape of
// store.PersonalDataStore.SetSessionPin / store.TeamDataStore.SetSessionPin
// so any production store satisfies it without adapter code.
//
// A nil implementation is allowed at runtime: personal-mode CLI without a
// wired store simply skips pin persistence (the pin is a no-op — matches
// the "empty string means inherit" contract downstream). Tests inject a
// fake to observe SetSessionPin calls.
type SessionPinStore interface {
	SetSessionPin(ctx context.Context, sessionID, provider, model string) error
}

// chatPinStore is the process-wide pin store the chat command writes to.
// Left nil in personal mode (no persistence); set by tests via
// SetChatPinStoreForTest and by platform mode wiring (future todos).
var chatPinStore SessionPinStore

// SetChatPinStoreForTest installs a fake pin store so tests can observe
// SetSessionPin calls without spinning up a real ent client. Returns a
// restore function that must be called via t.Cleanup.
func SetChatPinStoreForTest(s SessionPinStore) func() {
	prev := chatPinStore
	chatPinStore = s
	return func() { chatPinStore = prev }
}

// chatFlags holds the parsed CLI flag values for `astonish chat`.
type chatFlags struct {
	Provider     string
	Model        string
	Workspace    string
	AutoApprove  bool
	Debug        bool
	Resume       string
	NoPin        bool
	ClearModel   bool
}

// pinAction describes what pin-persistence side-effect the flag combination
// implies. Computed independently of the store so tests can assert on the
// intended action even when no store is wired.
type pinAction int

const (
	// pinActionNone: no pin write — either no -p/-m provided on a new
	// session, or --no-pin was set, or a --resume without --clear-model.
	pinActionNone pinAction = iota
	// pinActionPin: persist Provider+Model on the new session ID.
	pinActionPin
	// pinActionClear: clear the pin (SetSessionPin(id, "", "")) on the
	// resumed session.
	pinActionClear
)

// chatPlan is the decision-complete outcome of parsing `astonish chat` flags.
// It separates validation and pin-intent from the side-effects of running
// the console loop, so the planning logic is testable in isolation.
type chatPlan struct {
	Flags  chatFlags
	Action pinAction
}

// planChatFlags validates flag combinations and returns the intended pin
// action. It performs NO I/O and does NOT call any store — that is the
// caller's job (production: applyPinAction after RunChatConsole; tests:
// direct assertion against the returned plan or a call through
// applyPinAction with a fake store).
func planChatFlags(f chatFlags) (chatPlan, error) {
	if f.ClearModel && f.Resume == "" {
		return chatPlan{}, fmt.Errorf("--clear-model requires --resume")
	}

	plan := chatPlan{Flags: f, Action: pinActionNone}

	switch {
	case f.ClearModel:
		// --clear-model always wins when set (validated above to require --resume).
		plan.Action = pinActionClear
	case f.Resume != "":
		// Resumed session: -p/-m are ephemeral overrides only. Never write pin.
		// (DECISION-6: `--resume -m X` = ephemeral override for this run only.)
		plan.Action = pinActionNone
	case f.NoPin:
		// Explicit opt-out. Never write pin even if -p/-m provided.
		plan.Action = pinActionNone
	case f.Provider != "" || f.Model != "":
		// New session with pin defaults: -p or -m implies pin-by-default.
		// (DECISION-5.)
		plan.Action = pinActionPin
	}

	return plan, nil
}

// applyPinAction executes the pin side-effect described by the plan against
// the provided store. A nil store is a silent no-op (personal-mode CLI
// without a wired PersonalDataStore). The provided sessionID is required
// for pinActionPin (new-session case: the ID is only known after the
// console creates the session) and pre-known for pinActionClear (the
// resumed ID from --resume).
func applyPinAction(ctx context.Context, store SessionPinStore, plan chatPlan, sessionID string) error {
	if store == nil {
		return nil
	}
	switch plan.Action {
	case pinActionNone:
		return nil
	case pinActionPin:
		if sessionID == "" {
			return fmt.Errorf("cannot pin: session ID not available")
		}
		return store.SetSessionPin(ctx, sessionID, plan.Flags.Provider, plan.Flags.Model)
	case pinActionClear:
		return store.SetSessionPin(ctx, sessionID, "", "")
	default:
		return fmt.Errorf("unknown pin action: %d", plan.Action)
	}
}

// parseChatFlags parses the argv slice into a chatFlags struct. Extracted
// from handleChatCommand so tests can drive flag parsing directly.
func parseChatFlags(args []string) (chatFlags, error) {
	chatCmd := flag.NewFlagSet("chat", flag.ContinueOnError)

	providerName := chatCmd.String("provider", "", "AI provider")
	modelName := chatCmd.String("model", "", "Model name")
	workspaceDir := chatCmd.String("workspace", "", "Working directory (default: current dir)")
	autoApprove := chatCmd.Bool("auto-approve", false, "Auto-approve all tool executions")
	debugMode := chatCmd.Bool("debug", false, "Enable debug mode")
	resumeSession := chatCmd.String("resume", "", "Resume an existing session by ID")
	noPin := chatCmd.Bool("no-pin", false, "Do not persist provider/model as session pin (opt-out for scripted callers)")
	clearModel := chatCmd.Bool("clear-model", false, "Clear the model pin on the resumed session (requires --resume)")

	// Short flag aliases
	chatCmd.StringVar(providerName, "p", "", "AI provider (short)")
	chatCmd.StringVar(modelName, "m", "", "Model name (short)")
	chatCmd.StringVar(workspaceDir, "w", "", "Working directory (short)")
	chatCmd.StringVar(resumeSession, "r", "", "Resume session (short)")

	if err := chatCmd.Parse(args); err != nil {
		return chatFlags{}, err
	}

	return chatFlags{
		Provider:    *providerName,
		Model:       *modelName,
		Workspace:   *workspaceDir,
		AutoApprove: *autoApprove,
		Debug:       *debugMode,
		Resume:      *resumeSession,
		NoPin:       *noPin,
		ClearModel:  *clearModel,
	}, nil
}

func handleChatCommand(args []string) error {
	// Handle --help early
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printChatUsage()
		return nil
	}

	// Sub-command: `astonish chat model <provider>:<model>` sets or clears
	// the per-session pin on the most-recent (or --session) session without
	// starting the console loop.
	if len(args) > 0 && args[0] == "model" {
		return handleChatModelCommand(args[1:])
	}

	// Remote mode: run against the remote server
	if client.IsRemoteMode() {
		return handleChatRemote(args)
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v\n", err)
		appCfg = &config.AppConfig{}
	}

	// Escalate to root on Linux when sandbox is enabled.
	if sandbox.NeedsEscalation() && sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return sandbox.Escalate()
	}

	flags, err := parseChatFlags(args)
	if err != nil {
		return err
	}

	plan, err := planChatFlags(flags)
	if err != nil {
		return err
	}

	// If --clear-model, persist the clear BEFORE starting the runner so the
	// resumed session boots with the cascade result already restored.
	ctx := context.Background()
	if plan.Action == pinActionClear {
		if err := applyPinAction(ctx, chatPinStore, plan, flags.Resume); err != nil {
			return fmt.Errorf("failed to clear session pin: %w", err)
		}
	}

	// Resolve provider: flag > config > error
	resolvedProvider := flags.Provider
	if resolvedProvider == "" {
		resolvedProvider = appCfg.General.DefaultProvider
	}
	if resolvedProvider == "" {
		fmt.Println("Error: No provider specified. Use --provider flag or set default_provider in config.")
		fmt.Println("Run 'astonish setup' to configure providers.")
		return fmt.Errorf("no provider specified")
	}

	// Resolve model: flag > config > empty (provider default)
	resolvedModel := flags.Model
	if resolvedModel == "" {
		resolvedModel = appCfg.General.DefaultModel
	}

	cfg := &launcher.ChatConsoleConfig{
		AppConfig:    appCfg,
		ProviderName: resolvedProvider,
		ModelName:    resolvedModel,
		DebugMode:    flags.Debug,
		AutoApprove:  flags.AutoApprove,
		WorkspaceDir: flags.Workspace,
		SessionID:    flags.Resume,
	}

	// Pin-by-default on new sessions is handled by RunChatConsole via a
	// callback: the console creates the session, then invokes this hook
	// with the resulting ID so the pin can be persisted against the real
	// session. Personal-mode without a wired store leaves chatPinStore nil,
	// making this a silent no-op.
	if plan.Action == pinActionPin {
		cfg.OnSessionCreated = func(sessionID string) error {
			return applyPinAction(ctx, chatPinStore, plan, sessionID)
		}
	}

	return launcher.RunChatConsole(ctx, cfg)
}

func handleChatRemote(args []string) error {
	chatCmd := flag.NewFlagSet("chat", flag.ExitOnError)
	autoApprove := chatCmd.Bool("auto-approve", false, "Auto-approve all tool executions")
	debugMode := chatCmd.Bool("debug", false, "Enable debug mode")
	resumeSession := chatCmd.String("resume", "", "Resume an existing session by ID")
	chatCmd.StringVar(resumeSession, "r", "", "Resume session (short)")

	if err := chatCmd.Parse(args); err != nil {
		return err
	}

	cfg := &launcher.RemoteChatConfig{
		AutoApprove: *autoApprove,
		SessionID:   *resumeSession,
		DebugMode:   *debugMode,
	}

	return launcher.RunRemoteChatConsole(context.Background(), cfg)
}

// parseModelPin splits a `provider:model` argument on the FIRST colon so
// model names that contain colons (e.g. `openai:gpt-4o:2024-08-06`) survive
// intact. An empty input clears the pin (both provider and model empty).
func parseModelPin(arg string) (provider, model string, err error) {
	if arg == "" {
		return "", "", nil
	}
	idx := strings.IndexByte(arg, ':')
	if idx < 0 {
		return "", "", fmt.Errorf("expected provider:model, got %q", arg)
	}
	return arg[:idx], arg[idx+1:], nil
}

// resolveLastPersonalSessionID returns the most-recently-updated top-level
// (non-sub-agent) session ID from the personal-mode session index.
func resolveLastPersonalSessionID(appCfg *config.AppConfig) (string, error) {
	if appCfg.Sessions.Storage == "memory" {
		return "", fmt.Errorf("session persistence is disabled (storage: memory)")
	}
	sessDir, err := config.GetSessionsDir(&appCfg.Sessions)
	if err != nil {
		return "", fmt.Errorf("failed to resolve sessions dir: %w", err)
	}
	idx := persistentsession.NewSessionIndex(filepath.Join(sessDir, "index.json"))
	data, err := idx.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load session index: %w", err)
	}
	metas := make([]persistentsession.SessionMeta, 0, len(data.Sessions))
	for _, m := range data.Sessions {
		if m.ParentID != "" {
			continue
		}
		metas = append(metas, m)
	}
	if len(metas) == 0 {
		return "", fmt.Errorf("no sessions found; start one with 'astonish chat'")
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})
	return metas[0].ID, nil
}

// resolveLastRemoteSessionID returns the most-recently-updated session ID
// from the remote server.
func resolveLastRemoteSessionID(c *client.Client) (string, error) {
	sessions, err := c.ListSessions()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found on remote server")
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})
	return sessions[0].ID, nil
}

// handleChatModelCommand implements `astonish chat model <provider>:<model>`.
//
// Positional arg: `provider:model` (split on FIRST colon). Empty string
// clears the pin. Optional `--session <id>` targets a specific session;
// otherwise the most-recent session is used.
//
// Remote mode: calls PATCH /api/studio/sessions/{id}/model.
// Personal mode: writes via chatPinStore if wired; prints a note otherwise
// (chatPinStore is nil in vanilla personal CLI — see Todo 14 handoff).
func handleChatModelCommand(args []string) error {
	fs := flag.NewFlagSet("chat model", flag.ContinueOnError)
	session := fs.String("session", "", "Target session ID (default: most recent)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: astonish chat model [--session <id>] <provider>:<model>")
	}
	provider, model, err := parseModelPin(rest[0])
	if err != nil {
		return err
	}

	if client.IsRemoteMode() {
		c, err := client.New()
		if err != nil {
			return err
		}
		sessionID := *session
		if sessionID == "" {
			sessionID, err = resolveLastRemoteSessionID(c)
			if err != nil {
				return err
			}
		}
		resp, err := c.PatchSessionModel(sessionID, provider, model)
		if err != nil {
			return fmt.Errorf("failed to patch session model: %w", err)
		}
		printModelResult(sessionID, resp.PinnedProvider, resp.PinnedModel,
			resp.EffectiveProvider, resp.EffectiveModel, resp.CredentialsAvailable)
		return nil
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	sessionID := *session
	if sessionID == "" {
		sessionID, err = resolveLastPersonalSessionID(appCfg)
		if err != nil {
			return err
		}
	}

	if chatPinStore == nil {
		return fmt.Errorf("pin store not available in personal mode (session %s left unchanged)", sessionID)
	}
	ctx := context.Background()
	if err := chatPinStore.SetSessionPin(ctx, sessionID, provider, model); err != nil {
		return fmt.Errorf("failed to set session pin: %w", err)
	}

	effectiveProvider := provider
	if effectiveProvider == "" {
		effectiveProvider = appCfg.General.DefaultProvider
	}
	effectiveModel := model
	if effectiveModel == "" {
		effectiveModel = appCfg.General.DefaultModel
	}
	printModelResult(sessionID, provider, model, effectiveProvider, effectiveModel, true)
	return nil
}

func printModelResult(sessionID, pinnedProvider, pinnedModel, effectiveProvider, effectiveModel string, credentialsAvailable bool) {
	fmt.Printf("Session: %s\n", sessionID)
	if pinnedProvider == "" && pinnedModel == "" {
		fmt.Println("Pin cleared; using cascade default.")
	} else {
		fmt.Printf("Pinned:    %s / %s\n", pinnedProvider, pinnedModel)
	}
	fmt.Printf("Effective: %s / %s\n", effectiveProvider, effectiveModel)
	if !credentialsAvailable {
		fmt.Println("Warning: no credential configured for the pinned provider (pin persisted, hot-swap skipped)")
	}
}

func printChatUsage() {
	fmt.Println("usage: astonish chat [options]")
	fmt.Println("")
	fmt.Println("Start an interactive chat session with an AI agent that can use tools.")
	fmt.Println("The agent dynamically decides how to solve tasks using available tools.")
	fmt.Println("Complex tasks can be saved as reusable flows.")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -p, --provider      AI provider (default: from config)")
	fmt.Println("  -m, --model         Model name (default: from config)")
	fmt.Println("  -w, --workspace     Working directory (default: current dir)")
	fmt.Println("  -r, --resume        Resume an existing session by ID")
	fmt.Println("  --no-pin            Do not persist -p/-m as the session's pinned model")
	fmt.Println("                      (default: -p/-m on a new session pins the session)")
	fmt.Println("  --clear-model       Clear the model pin on the resumed session")
	fmt.Println("                      (requires --resume; falls back to the cascade default)")
	fmt.Println("  --auto-approve      Auto-approve all tool executions")
	fmt.Println("  --debug             Enable debug output")
	fmt.Println("  -h, --help          Show this help message")
	fmt.Println("")
	fmt.Println("model-pin semantics:")
	fmt.Println("  New session with -p/-m       → pinned to the session (persists across resumes)")
	fmt.Println("  New session with -p/-m --no-pin → ephemeral for this run only")
	fmt.Println("  --resume <id> -m X           → override for this run only (no pin rewrite)")
	fmt.Println("  --resume <id> --clear-model  → clears the pin, restores cascade default")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish chat")
	fmt.Println("  astonish chat -p openai -m gpt-4o")
	fmt.Println("  astonish chat -p openai -m gpt-4o --no-pin")
	fmt.Println("  astonish chat --auto-approve")
	fmt.Println("  astonish chat --resume <session-id>")
	fmt.Println("  astonish chat --resume <session-id> --clear-model")
	fmt.Println("  astonish chat model openai:gpt-4o")
	fmt.Println("  astonish chat model \"\"                     # clear pin")
	fmt.Println("  astonish chat model --session <id> anthropic:claude-sonnet-4")
	fmt.Println("")
	fmt.Println("In chat mode, the agent has access to all configured tools (internal + MCP)")
	fmt.Println("and will call them as needed to accomplish your tasks.")
}
