package channels

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/schardosin/astonish/pkg/agent"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ChannelManager owns the lifecycle of all registered channels and routes
// inbound messages to the shared ChatAgent.
type ChannelManager struct {
	channels map[string]Channel
	router   *Router
	agent    *agent.ChatAgent
	sessSvc  session.Service
	commands *CommandRegistry
	mu       sync.RWMutex
	running  atomic.Bool
	logger   *log.Logger

	// Info fields for command context
	providerName string
	modelName    string
	toolCount    int
}

// ChannelManagerConfig holds optional configuration for NewChannelManager.
type ChannelManagerConfig struct {
	ProviderName string
	ModelName    string
	ToolCount    int
}

// NewChannelManager creates a new ChannelManager with the given ChatAgent
// and session service. All inbound messages are processed by the shared
// ChatAgent using per-conversation persistent sessions.
func NewChannelManager(chatAgent *agent.ChatAgent, sessSvc session.Service, logger *log.Logger, cfg *ChannelManagerConfig) *ChannelManager {
	if logger == nil {
		logger = log.Default()
	}
	m := &ChannelManager{
		channels: make(map[string]Channel),
		router:   NewRouter(),
		agent:    chatAgent,
		sessSvc:  sessSvc,
		commands: DefaultCommands(),
		logger:   logger,
	}
	if cfg != nil {
		m.providerName = cfg.ProviderName
		m.modelName = cfg.ModelName
		m.toolCount = cfg.ToolCount
	}
	return m
}

// Register adds a channel to the manager. Must be called before StartAll.
func (m *ChannelManager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.ID()] = ch
}

// StartAll starts all registered channels. Each channel runs in its own
// goroutine. Returns immediately after launching all channels.
func (m *ChannelManager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.channels) == 0 {
		return nil
	}

	m.running.Store(true)

	for id, ch := range m.channels {
		go func(id string, ch Channel) {
			m.logger.Printf("[channels] Starting %s...", id)
			if err := ch.Start(ctx, m.handleInbound); err != nil {
				m.logger.Printf("[channels] %s stopped with error: %v", id, err)
			}
		}(id, ch)
	}

	return nil
}

// StopAll gracefully stops all registered channels.
func (m *ChannelManager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.running.Store(false)

	var firstErr error
	for id, ch := range m.channels {
		m.logger.Printf("[channels] Stopping %s...", id)
		if err := ch.Stop(ctx); err != nil {
			m.logger.Printf("[channels] Error stopping %s: %v", id, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Commands returns the command registry so callers (e.g. console TUI) can
// reuse the same commands.
func (m *ChannelManager) Commands() *CommandRegistry {
	return m.commands
}

// Status returns the status of all registered channels.
func (m *ChannelManager) Status() map[string]ChannelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]ChannelStatus, len(m.channels))
	for id, ch := range m.channels {
		statuses[id] = ch.Status()
	}
	return statuses
}

// handleInbound processes an inbound message from any channel.
// It routes the message to the appropriate session, runs the ChatAgent,
// collects the response, handles auto-distillation, and sends the reply.
func (m *ChannelManager) handleInbound(ctx context.Context, msg InboundMessage) error {
	// Route to determine session key
	route := m.router.Route(msg)

	m.logger.Printf("[channels] Inbound from %s (chat: %s, sender: %s): %s",
		msg.ChannelID, msg.ChatID, msg.SenderName, truncate(msg.Text, 100))

	// Intercept slash commands before sending to the agent.
	if m.commands.IsCommand(msg.Text) {
		return m.handleCommand(ctx, msg, route)
	}

	// Get or create persistent session
	userID := fmt.Sprintf("channel_%s_%s", msg.ChannelID, msg.SenderID)
	appName := "astonish"

	sess, err := m.getOrCreateSession(ctx, appName, userID, route.SessionKey)
	if err != nil {
		return fmt.Errorf("session error: %w", err)
	}

	// Set channel-specific output hints on the prompt builder for this turn.
	// This guides the LLM to produce channel-appropriate output (e.g., no tables
	// for Telegram, shorter responses for chat).
	if m.agent.SystemPrompt != nil {
		m.agent.SystemPrompt.ChannelHints = channelHints(msg.ChannelID)
		defer func() { m.agent.SystemPrompt.ChannelHints = "" }()
	}

	// Create ADK agent wrapper for this turn
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_channel",
		Description: "Astonish channel agent",
		Run:         m.agent.Run,
	})
	if err != nil {
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}

	// Create runner for this turn
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: m.sessSvc,
	})
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	// Run the agent
	userContent := genai.NewContentFromText(msg.Text, genai.RoleUser)
	var responseText strings.Builder

	for event, err := range r.Run(ctx, userID, sess.ID(), userContent, adkagent.RunConfig{}) {
		if err != nil {
			m.logger.Printf("[channels] Agent error for %s: %v", route.SessionKey, err)
			responseText.WriteString("Sorry, I encountered an error processing your message.")
			break
		}

		if event.LLMResponse.Content == nil {
			continue
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				responseText.WriteString(part.Text)
			}
		}
	}

	// Process response: strip distill marker, trigger auto-distill
	fullResponse := responseText.String()
	displayText := stripDistillMarker(fullResponse)

	if distillDesc := extractDistillMarker(fullResponse); distillDesc != "" {
		m.logger.Printf("[channels] Auto-distill triggered: %s", distillDesc)
		go func(desc string) {
			if err := m.agent.AutoDistill(context.Background(), desc); err != nil {
				m.logger.Printf("[channels] Auto-distill failed: %v", err)
			}
		}(distillDesc)
	}

	// Send response back to channel
	if strings.TrimSpace(displayText) == "" {
		displayText = "I processed your request but have nothing to say."
	}

	ch := m.getChannel(msg.ChannelID)
	if ch == nil {
		return fmt.Errorf("channel %s not found", msg.ChannelID)
	}

	target := Target{
		ChannelID: msg.ChannelID,
		ChatID:    msg.ChatID,
		ThreadID:  msg.ThreadID,
	}

	outMsg := OutboundMessage{
		Text:    displayText,
		ReplyTo: msg.ID,
		Format:  FormatHTML,
	}

	if err := ch.Send(ctx, target, outMsg); err != nil {
		m.logger.Printf("[channels] Failed to send response to %s: %v", msg.ChannelID, err)
		return fmt.Errorf("send error: %w", err)
	}

	m.logger.Printf("[channels] Response sent to %s (chat: %s, %d chars)",
		msg.ChannelID, msg.ChatID, len(displayText))

	return nil
}

// handleCommand executes a slash command and sends the response back to the channel.
func (m *ChannelManager) handleCommand(ctx context.Context, msg InboundMessage, route RouteResult) error {
	userID := fmt.Sprintf("channel_%s_%s", msg.ChannelID, msg.SenderID)
	appName := "astonish"

	cc := CommandContext{
		ChannelID:      msg.ChannelID,
		ChatID:         msg.ChatID,
		SenderID:       msg.SenderID,
		SenderName:     msg.SenderName,
		SessionKey:     route.SessionKey,
		UserID:         userID,
		AppName:        appName,
		SessionService: m.sessSvc,
		ProviderName:   m.providerName,
		ModelName:      m.modelName,
		ToolCount:      m.toolCount,
	}

	response, err := m.commands.Execute(ctx, msg.Text, cc)
	if err != nil {
		m.logger.Printf("[channels] Command error: %v", err)
		response = "Sorry, that command failed."
	}

	ch := m.getChannel(msg.ChannelID)
	if ch == nil {
		return fmt.Errorf("channel %s not found", msg.ChannelID)
	}

	target := Target{
		ChannelID: msg.ChannelID,
		ChatID:    msg.ChatID,
		ThreadID:  msg.ThreadID,
	}

	outMsg := OutboundMessage{
		Text:    response,
		ReplyTo: msg.ID,
		Format:  FormatText, // Command responses are plain text
	}

	return ch.Send(ctx, target, outMsg)
}

// getOrCreateSession retrieves an existing session by key or creates a new one.
// Session keys are used as session IDs for deterministic mapping.
func (m *ChannelManager) getOrCreateSession(ctx context.Context, appName, userID, sessionKey string) (session.Session, error) {
	// Try to get existing session
	getResp, err := m.sessSvc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionKey,
	})
	if err == nil && getResp.Session != nil {
		return getResp.Session, nil
	}

	// Create new session with the key as ID
	createResp, err := m.sessSvc.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionKey,
	})
	if err != nil {
		return nil, err
	}
	return createResp.Session, nil
}

// getChannel returns a registered channel by ID.
func (m *ChannelManager) getChannel(id string) Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channels[id]
}

// distillMarkerRe matches [DISTILL: description] anywhere in text.
var distillMarkerRe = regexp.MustCompile(`\[DISTILL:\s*([^\]]+)\]`)

// extractDistillMarker returns the description from a [DISTILL: ...] marker.
func extractDistillMarker(text string) string {
	matches := distillMarkerRe.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

// stripDistillMarker removes [DISTILL: ...] markers from text.
func stripDistillMarker(text string) string {
	return distillMarkerRe.ReplaceAllString(text, "")
}

// channelHints returns LLM output guidance for a given channel.
// These hints are injected into the system prompt so the model produces
// output suited to the channel's formatting capabilities.
func channelHints(channelID string) string {
	switch channelID {
	case "telegram":
		return `You are responding via Telegram chat.
- Keep responses concise (under 300 words when possible)
- NEVER use markdown tables — use plain bullet lists instead
- Use simple formatting only: **bold**, *italic*, ` + "`code`" + `, and fenced code blocks
- Break long responses into short paragraphs
- Be conversational — this is a chat, not a terminal`
	default:
		return ""
	}
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
