package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/schardosin/astonish/pkg/client"
	"github.com/schardosin/astonish/pkg/ui"
)

// RemoteChatConfig contains configuration for a remote chat session.
type RemoteChatConfig struct {
	AutoApprove bool
	SessionID   string // Resume existing session (empty = new)
}

// RunRemoteChatConsole runs an interactive chat session against a remote
// Astonish server, providing the same TUI experience as the local mode
// (spinners, streaming text, tool call displays, approval prompts).
func RunRemoteChatConsole(ctx context.Context, cfg *RemoteChatConfig) error {
	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	remoteCfg, _ := client.LoadRemoteConfig()
	serverURL := ""
	if remoteCfg != nil {
		serverURL = remoteCfg.URL
	}

	// Print header
	fmt.Printf("%sAstonish Chat (remote: %s)%s\n", ColorCyan, serverURL, ColorReset)
	fmt.Printf("Type 'exit' to quit, '/help' for commands.\n\n")

	// Session ID tracking
	sessionID := cfg.SessionID

	// Spinner management
	var spinnerProgram *tea.Program
	var spinnerDone chan struct{}
	lineHasContent := false

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

	reader := bufio.NewReader(os.Stdin)

	// Main chat loop
	for {
		// Read user input
		fmt.Printf("%sYou:%s ", ColorCyan, ColorReset)
		input, err := reader.ReadString('\n')
		if err != nil {
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

		// Handle local-only slash commands
		if strings.HasPrefix(input, "/") {
			switch {
			case input == "/help":
				fmt.Printf("%sAvailable commands:%s\n", ColorCyan, ColorReset)
				fmt.Println("  /status      - Show connection status")
				fmt.Println("  /new         - Start a fresh conversation (new session)")
				fmt.Println("  /help        - Show this help message")
				fmt.Println("  exit         - Exit the chat")
				fmt.Println()
				continue
			case input == "/status":
				fmt.Printf("  Server:    %s\n", serverURL)
				if remoteCfg != nil {
					fmt.Printf("  Org:       %s\n", remoteCfg.Org)
					fmt.Printf("  Team:      %s\n", remoteCfg.Team)
					fmt.Printf("  User:      %s\n", remoteCfg.UserEmail)
				}
				if sessionID != "" {
					fmt.Printf("  Session:   %s\n", sessionID)
				}
				fmt.Println()
				continue
			case input == "/new":
				sessionID = ""
				fmt.Printf("%sFresh start!%s New session will be created on next message.\n\n", ColorGreen, ColorReset)
				continue
			default:
				// Send other slash commands to the server (they may be handled server-side)
			}
		}

		// Send message to server
		if err := runRemoteTurn(ctx, c, &sessionID, input, cfg.AutoApprove, startSpinner, stopSpinner, &lineHasContent, reader); err != nil {
			fmt.Printf("\n%sError:%s %v\n\n", "\033[31m", ColorReset, err)
		}
	}

	stopSpinner()
	fmt.Println("\nGoodbye!")
	return nil
}

// runRemoteTurn sends a message and processes the SSE response stream.
// If tool approval is needed, it prompts the user and sends the follow-up.
func runRemoteTurn(
	ctx context.Context,
	c *client.Client,
	sessionID *string,
	message string,
	autoApprove bool,
	startSpinner func(string),
	stopSpinner func(),
	lineHasContent *bool,
	reader *bufio.Reader,
) error {
	req := &client.ChatRequest{
		SessionID:   *sessionID,
		Message:     message,
		AutoApprove: autoApprove,
	}

	startSpinner("Thinking...")

	stream, err := c.SendChatMessage(req)
	if err != nil {
		stopSpinner()
		return err
	}
	defer stream.Close()

	aiPrefixPrinted := false
	spinnerStopped := false
	lastEventWasTool := false
	inToolBox := false
	waitingForApproval := false
	var approvalOptions []string

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
		*lineHasContent = !strings.HasSuffix(text, "\n")
	}

	for {
		event, err := stream.Next()
		if err != nil {
			break
		}

		switch event.Type {
		case "session":
			var payload struct {
				SessionID string `json:"sessionId"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil && payload.SessionID != "" {
				*sessionID = payload.SessionID
			}

		case "text":
			var payload struct {
				Text string `json:"text"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				displayChunk := payload.Text
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
					*lineHasContent = !strings.HasSuffix(displayChunk, "\n")
					if strings.Contains(displayChunk, "╰") {
						inToolBox = false
					}
				} else {
					printText(displayChunk)
				}
			}

		case "tool_call":
			var payload struct {
				Name string `json:"name"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				lastEventWasTool = true
				startSpinner(fmt.Sprintf("Running %s...", payload.Name))
				spinnerStopped = false
			}

		case "tool_result":
			lastEventWasTool = true

		case "approval":
			var payload struct {
				Tool    string   `json:"tool"`
				Options []string `json:"options"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				waitingForApproval = true
				approvalOptions = payload.Options
			}

		case "auto_approved":
			var payload struct {
				Tool string `json:"tool"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				if !spinnerStopped {
					stopSpinner()
					spinnerStopped = true
				}
				fmt.Printf("%s  ✓ Auto-approved: %s%s\n", ColorCyan, payload.Tool, ColorReset)
				*lineHasContent = false
			}

		case "thinking":
			var payload struct {
				Text string `json:"text"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				startSpinner(payload.Text)
				spinnerStopped = false
			}

		case "subtask_progress":
			var payload struct {
				EventType string `json:"event_type"`
				TaskName  string `json:"task_name"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				if payload.EventType == "task_start" && payload.TaskName != "" {
					startSpinner(fmt.Sprintf("Working: %s...", payload.TaskName))
					spinnerStopped = false
				}
			}

		case "artifact":
			var payload struct {
				Path     string `json:"path"`
				ToolName string `json:"tool_name"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				if !spinnerStopped {
					stopSpinner()
					spinnerStopped = true
				}
				fmt.Printf("%s  📄 %s%s\n", ColorCyan, payload.Path, ColorReset)
				*lineHasContent = false
			}

		case "error", "error_info":
			stopSpinner()
			spinnerStopped = true
			var payload struct {
				Error  string `json:"error"`
				Title  string `json:"title"`
				Reason string `json:"reason"`
			}
			if jsonErr := json.Unmarshal([]byte(event.Data), &payload); jsonErr == nil {
				msg := payload.Error
				if msg == "" {
					msg = payload.Reason
				}
				if payload.Title != "" {
					msg = payload.Title + ": " + msg
				}
				if msg != "" {
					fmt.Printf("\n%sError:%s %s\n", "\033[31m", ColorReset, msg)
					*lineHasContent = false
				}
			}

		case "usage":
			// Silently consume usage events (could optionally display)

		case "done":
			// Turn complete
		}
	}

	stopSpinner()

	// Ensure we end on a newline
	if *lineHasContent {
		fmt.Println()
		*lineHasContent = false
	}
	fmt.Println()

	// Handle approval if needed
	if waitingForApproval {
		opts := []string{"Yes", "No"}
		if len(approvalOptions) > 0 {
			opts = approvalOptions
		}
		selection, selErr := ui.ReadSelection(opts, "Approve tool execution?", "")
		if selErr != nil {
			return selErr
		}
		if selection == "Yes" {
			fmt.Println(ui.RenderStatusBadge("Command approved", true))
		} else {
			fmt.Println(ui.RenderStatusBadge("Command rejected", false))
		}
		// Send approval response back as a new turn
		return runRemoteTurn(ctx, c, sessionID, selection, autoApprove, startSpinner, stopSpinner, lineHasContent, reader)
	}

	return nil
}
