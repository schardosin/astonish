// Package channels — command registry for cross-channel slash commands.
//
// Commands registered here work identically in the console TUI and in
// any channel adapter (Telegram, Slack, etc.). Each command receives a
// CommandContext with enough information to inspect or mutate the current
// session, and returns a plain-text response string.
package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/adk/session"
)

// CommandContext provides everything a command handler needs to inspect
// or mutate the current conversation state.
type CommandContext struct {
	// ChannelID is the channel adapter that received the command
	// (e.g. "telegram", "console"). Empty for console.
	ChannelID string
	// ChatID is the chat/conversation where the command was issued.
	ChatID string
	// SenderID is the normalized sender identifier.
	SenderID string
	// SenderName is the human-readable sender name.
	SenderName string
	// SessionKey is the persistent session key for this conversation.
	SessionKey string
	// UserID is the ADK user ID for session operations.
	UserID string
	// AppName is the ADK app name (always "astonish").
	AppName string

	// SessionService is the session store for session operations.
	SessionService session.Service
	// ProviderName is the active LLM provider name.
	ProviderName string
	// ModelName is the active LLM model name.
	ModelName string
	// ToolCount is the number of available tools.
	ToolCount int
}

// CommandFunc is the handler signature for a slash command.
// It returns the text response to send back to the user.
type CommandFunc func(ctx context.Context, cc CommandContext) (string, error)

// Command describes a single slash command.
type Command struct {
	// Name is the slash command name without the leading slash (e.g. "status").
	Name string
	// Description is a short human-readable description for /help.
	Description string
	// Handler is the function that executes the command.
	Handler CommandFunc
}

// CommandRegistry holds all registered slash commands.
type CommandRegistry struct {
	mu       sync.RWMutex
	commands map[string]*Command
	order    []string // insertion order for /help
}

// NewCommandRegistry creates a new empty command registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]*Command),
	}
}

// Register adds a command to the registry. Panics if the name is already taken.
func (r *CommandRegistry) Register(cmd *Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.commands[cmd.Name]; exists {
		panic(fmt.Sprintf("command /%s already registered", cmd.Name))
	}
	r.commands[cmd.Name] = cmd
	r.order = append(r.order, cmd.Name)
}

// Lookup returns the command for the given name (without leading slash).
// Returns nil if not found.
func (r *CommandRegistry) Lookup(name string) *Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.commands[name]
}

// IsCommand returns true if the text starts with "/" and matches a registered command.
func (r *CommandRegistry) IsCommand(text string) bool {
	name := parseCommandName(text)
	if name == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.commands[name]
	return exists
}

// Execute parses the command name from text and runs the matching handler.
// Returns the response text and any error.
func (r *CommandRegistry) Execute(ctx context.Context, text string, cc CommandContext) (string, error) {
	name := parseCommandName(text)
	if name == "" {
		return "", fmt.Errorf("not a command: %q", text)
	}

	cmd := r.Lookup(name)
	if cmd == nil {
		return fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", name), nil
	}

	return cmd.Handler(ctx, cc)
}

// List returns all registered commands in registration order.
func (r *CommandRegistry) List() []*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Command, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.commands[name])
	}
	return result
}

// parseCommandName extracts the command name from "/name" or "/name args".
// Returns empty string if text doesn't start with "/".
func parseCommandName(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	// Remove leading slash and take the first word
	rest := text[1:]
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.ToLower(rest)
}

// DefaultCommands creates a CommandRegistry pre-loaded with the standard
// cross-channel commands: /status, /new, /help.
func DefaultCommands() *CommandRegistry {
	r := NewCommandRegistry()
	r.Register(statusCommand())
	r.Register(newSessionCommand())
	r.Register(helpCommand(r))
	return r
}

// --- Built-in command implementations ---

func statusCommand() *Command {
	return &Command{
		Name:        "status",
		Description: "Show provider, model, and session info",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			var b strings.Builder
			b.WriteString("Status\n")
			b.WriteString(fmt.Sprintf("  Provider: %s\n", cc.ProviderName))
			b.WriteString(fmt.Sprintf("  Model:    %s\n", cc.ModelName))
			if cc.ToolCount > 0 {
				b.WriteString(fmt.Sprintf("  Tools:    %d\n", cc.ToolCount))
			}
			if cc.SessionKey != "" {
				b.WriteString(fmt.Sprintf("  Session:  %s\n", cc.SessionKey))
			}
			if cc.ChannelID != "" {
				b.WriteString(fmt.Sprintf("  Channel:  %s\n", cc.ChannelID))
			}
			return b.String(), nil
		},
	}
}

func newSessionCommand() *Command {
	return &Command{
		Name:        "new",
		Description: "Start a fresh conversation (new session)",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			if cc.SessionService == nil {
				return "Session reset is not available.", nil
			}

			// Delete the existing session so the next message creates a fresh one.
			err := cc.SessionService.Delete(ctx, &session.DeleteRequest{
				AppName:   cc.AppName,
				UserID:    cc.UserID,
				SessionID: cc.SessionKey,
			})
			if err != nil {
				// Not fatal — session may not exist yet (first message).
				return "Starting fresh. Send your next message to begin a new conversation.", nil
			}

			return "Session cleared. Send your next message to start a new conversation.", nil
		},
	}
}

func helpCommand(registry *CommandRegistry) *Command {
	return &Command{
		Name:        "help",
		Description: "Show available commands",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			var b strings.Builder
			b.WriteString("Available commands:\n")
			for _, cmd := range registry.List() {
				b.WriteString(fmt.Sprintf("  /%s — %s\n", cmd.Name, cmd.Description))
			}
			return b.String(), nil
		},
	}
}
