package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/provider"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	"github.com/schardosin/astonish/pkg/ui"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	tea "github.com/charmbracelet/bubbletea"
)

// consoleThinkTagRe strips <think>/<thinking> blocks that some models emit in
// title-generation responses.
var consoleThinkTagRe = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

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

	// --- Build fully-wired ChatAgent via factory ---
	result, err := NewWiredChatAgent(ctx, &ChatFactoryConfig{
		AppConfig:    cfg.AppConfig,
		ProviderName: cfg.ProviderName,
		ModelName:    cfg.ModelName,
		DebugMode:    cfg.DebugMode,
		AutoApprove:  cfg.AutoApprove,
		WorkspaceDir: cfg.WorkspaceDir,
	})
	if err != nil {
		return err
	}
	defer result.Cleanup()

	// Set up scheduler access via daemon HTTP API (if daemon is running)
	if cfg.AppConfig != nil {
		daemonPort := cfg.AppConfig.Daemon.GetPort()
		tools.SetSchedulerAccess(&tools.SchedulerHTTPAccess{
			BaseURL: fmt.Sprintf("http://localhost:%d", daemonPort),
		})
	}

	// Make distillation available to LLM tools (for auto-distill during scheduling)
	tools.SetDistillAccess(newDistillBridgeConsole(result.ChatAgent))

	// Unpack factory result into local variables used by the TUI loop
	llm := result.LLM
	currentProvider := result.ProviderName
	currentModel := result.ModelName
	chatAgent := result.ChatAgent
	compactor := result.Compactor
	internalTools := result.InternalTools
	mcpToolsets := result.MCPToolsets
	memMgr := result.MemoryManager
	memStore := result.MemoryStore
	memorySearchAvailable := result.MemorySearchAvailable
	indexingDone := result.IndexingDone
	indexingErr := result.IndexingErr
	sessionService := result.SessionService
	startupNotices := result.StartupNotices

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
			slog.Debug("resumed session", "sessionID", sess.ID(), "events", sess.Events().Len())
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

		// /drill-add <suite>: add new drills to an existing suite
		if strings.HasPrefix(input, "/drill-add") {
			suiteName := strings.TrimSpace(strings.TrimPrefix(input, "/drill-add"))
			if suiteName == "" {
				fmt.Printf("%sUsage: /drill-add <suite_name>%s\n", ColorYellow, ColorReset)
				continue
			}
			dirs := adrill.DefaultDrillDirs()
			suite, err := adrill.FindSuite(dirs, suiteName)
			if err != nil {
				fmt.Printf("%sSuite %q not found: %v%s\n", ColorYellow, suiteName, err, ColorReset)
				continue
			}
			suiteContext := adrill.BuildSuiteContext(suite)
			addPrompt := tools.GetDrillAddPrompt(suiteName, suiteContext)
			chatAgent.SystemPrompt.SessionContext = agent.EscapeCurlyPlaceholders(addPrompt)
			fmt.Printf("%sStarting drill-add wizard for suite %q (%d existing drills)...%s\n\n",
				ColorCyan, suiteName, len(suite.Tests), ColorReset)
			input = fmt.Sprintf("I'd like to add new drills to the %q suite.", suiteName)
			// Fall through to send as regular message
		}

		// /drill: inject wizard prompt and convert to agent message
		if strings.HasPrefix(input, "/drill") && !strings.HasPrefix(input, "/drill-add") {
			hint := strings.TrimSpace(strings.TrimPrefix(input, "/drill"))
			wizardPrompt := tools.GetDrillWizardPrompt()
			chatAgent.SystemPrompt.SessionContext = agent.EscapeCurlyPlaceholders(wizardPrompt)
			fmt.Printf("%sStarting drill suite creation wizard...%s\n\n", ColorCyan, ColorReset)
			if hint != "" {
				input = fmt.Sprintf("I'd like to create a drill suite. Here's what I want to test: %s", hint)
			} else {
				input = "I'd like to create a drill suite for my project."
			}
			// Fall through to send this as a regular message to the agent
		}

		// Slash command dispatch
		if strings.HasPrefix(input, "/") {
			switch {
			case input == "/distill":
				startSpinner("Analyzing conversation...")
				ds := agent.DistillSession{
					SessionID: sess.ID(),
					AppName:   appName,
					UserID:    userID,
				}
				description, previewErr := chatAgent.PreviewDistill(ctx, ds)
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
				distillErr := chatAgent.ConfirmAndDistill(ctx, ds, func(s string) {
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
						if *indexingErr != nil {
							fmt.Printf("  RAG:       error (%v)\n", *indexingErr)
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
			case input == "/new":
				// Delete current session and create a fresh one
				if delErr := sessionService.Delete(ctx, &session.DeleteRequest{
					AppName:   appName,
					UserID:    userID,
					SessionID: sess.ID(),
				}); delErr != nil {
					slog.Warn("failed to delete session during /new", "session", sess.ID(), "error", delErr)
				}
				newResp, newErr := sessionService.Create(ctx, &session.CreateRequest{
					AppName: appName,
					UserID:  userID,
				})
				if newErr != nil {
					fmt.Printf("%sError:%s Failed to create new session: %v\n\n", "\033[31m", ColorReset, newErr)
				} else {
					sess = newResp.Session
					shortID = sess.ID()
					if len(shortID) > 8 {
						shortID = shortID[:8]
					}
					fmt.Printf("%sFresh start!%s New session: %s\n\n", ColorGreen, ColorReset, shortID)
				}
			case input == "/help":
				fmt.Printf("%sAvailable commands:%s\n", ColorCyan, ColorReset)
				fmt.Println("  /status      - Show current provider, model, tools, and memory status")
				fmt.Println("  /new         - Start a fresh conversation (new session)")
				fmt.Println("  /compact     - Show context window usage and compaction status")
				fmt.Println("  /distill     - Distill the last task into a reusable flow")
				fmt.Println("  /drill       - Create a drill suite with guided wizard")
				fmt.Println("  /drill-add   - Add new drills to an existing suite")
				fmt.Println("  /fleet       - Show available fleets and CLI commands")
				fmt.Println("  /fleet-plan  - Create a fleet plan (use Studio UI for guided conversation)")
				fmt.Println("  /help        - Show this help message")
				fmt.Println("  exit         - Exit the chat")
				fmt.Println()
			case strings.HasPrefix(input, "/fleet-plan"):
				// Fleet plans require the guided conversation in Studio UI
				fmt.Printf("%sFleet plan creation requires the Studio UI for the guided conversation.%s\n", ColorCyan, ColorReset)
				fmt.Printf("Run: astonish studio, then type /fleet-plan in the chat.\n")
				fmt.Printf("To manage existing plans: astonish fleet list\n\n")
			case strings.HasPrefix(input, "/fleet"):
				// Show available fleets; fleet sessions are started via Studio UI
				fmt.Println(tools.ListAvailableFleets())
				fmt.Printf("%sFleet CLI commands:%s\n", ColorCyan, ColorReset)
				fmt.Printf("  astonish fleet list               List fleet plans\n")
				fmt.Printf("  astonish fleet show <key>         Show plan details\n")
				fmt.Printf("  astonish fleet activate <key>     Start polling\n")
				fmt.Printf("  astonish fleet deactivate <key>   Stop polling\n")
				fmt.Printf("  astonish fleet status <key>       Check status\n")
				fmt.Printf("  astonish fleet templates          List templates\n\n")
			default:
				fmt.Printf("Unknown command: %s. Type /help for available commands.\n\n", input)
			}
			continue
		}

		// Send message to agent (with absolute timestamp for temporal context;
		// see agent.NewTimestampedUserContent for cache-stability rationale).
		userMsg := agent.NewTimestampedUserContent(input)

		// Wait for background indexing to complete before the first agent call.
		// This ensures memory_search and flow matching have indexed data available.
		if !indexingWaited {
			indexingWaited = true
			select {
			case <-indexingDone:
				// Already done — no delay
			default:
				startSpinner("Preparing memory index...")
				select {
				case <-indexingDone:
					// Completed
				case <-time.After(60 * time.Second):
					// Timed out — continue without blocking, indexing may finish in background
					fmt.Println("\nMemory indexing is taking too long — continuing without it.")
				}
				stopSpinner()
			}
		}

		startSpinner("Thinking...")

		aiPrefixPrinted := false
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

		// Wire transparent sub-agent streaming for console: sub-agent tool calls
		// and text output are rendered in real-time. Uses a mutex since sub-agent
		// goroutines call this concurrently while the main loop also writes.
		var consoleMu sync.Mutex
		chatAgent.UIEventCallback = func(event *session.Event) {
			if event == nil || event.LLMResponse.Content == nil {
				return
			}
			consoleMu.Lock()
			defer consoleMu.Unlock()
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					// Show sub-agent tool call as a brief status line
					if !spinnerStopped {
						stopSpinner()
						spinnerStopped = true
					}
					fmt.Printf("%s  ↳ %s%s\n", ColorCyan, part.FunctionCall.Name, ColorReset)
					lineHasContent = false
					lastEventWasTool = true
				}
				if part.Text != "" && !part.Thought {
					printText(part.Text)
				}
			}
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
								slog.Debug("tool call", "tool", part.FunctionCall.Name, "args", string(argsJSON))
							}
							if part.FunctionResponse != nil {
								respJSON, _ := json.MarshalIndent(part.FunctionResponse.Response, "", "  ")
								slog.Debug("tool response", "tool", part.FunctionResponse.Name, "result", string(respJSON))
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
					// Clear wizard context after test suite is saved
					if p.FunctionCall.Name == "save_drill" {
						chatAgent.SystemPrompt.SessionContext = ""
					}
				}
				if p.FunctionResponse != nil {
					hasTool = true
				}
			}
			if hasTool {
				lastEventWasTool = true
			}

			if chunk != "" {
				displayChunk := chunk

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
		chatAgent.UIEventCallback = nil

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
			userMsg = agent.NewTimestampedUserContent(selection)

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
					displayChunk := chunk

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
					slog.Warn("failed to switch provider", "provider", newProvider, "model", newModel, "error", swapErr)
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

	title = consoleThinkTagRe.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	// Truncate if somehow too long
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	if err := store.SetSessionTitle(sessionID, title); err != nil {
		slog.Warn("failed to set session title", "session_id", sessionID, "error", err)
	}
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

// distillBridgeConsole adapts agent.ChatAgent to tools.DistillAccess for the
// console launcher, bridging the two packages without import cycles.
type distillBridgeConsole struct {
	chatAgent *agent.ChatAgent
}

func newDistillBridgeConsole(a *agent.ChatAgent) *distillBridgeConsole {
	return &distillBridgeConsole{chatAgent: a}
}

func (b *distillBridgeConsole) PreviewDistill(ctx context.Context, ds tools.DistillSession) (string, error) {
	return b.chatAgent.PreviewDistill(ctx, agent.DistillSession{
		SessionID: ds.SessionID,
		AppName:   ds.AppName,
		UserID:    ds.UserID,
	})
}

func (b *distillBridgeConsole) ConfirmAndDistill(ctx context.Context, ds tools.DistillSession, print func(string)) error {
	return b.chatAgent.ConfirmAndDistill(ctx, agent.DistillSession{
		SessionID: ds.SessionID,
		AppName:   ds.AppName,
		UserID:    ds.UserID,
	}, print)
}
