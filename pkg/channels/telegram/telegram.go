// Package telegram implements the Telegram channel adapter for Astonish.
// It connects to the Telegram Bot API via long polling, normalizes inbound
// messages, and delivers outbound responses with HTML formatting and chunking.
package telegram

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schardosin/astonish/pkg/channels"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Reconnection constants for exponential backoff.
const (
	initialReconnectDelay = 3 * time.Second
	maxReconnectDelay     = 5 * time.Minute
	// httpClientTimeout covers the 30s long-poll timeout plus a buffer for
	// network overhead. Without this, a hung TCP connection can block the
	// polling goroutine indefinitely.
	httpClientTimeout = 60 * time.Second
)

// maxMessageLength is the Telegram API limit for a single message.
const maxMessageLength = 4096

// maxCaptionLength is the Telegram API limit for a photo caption.
const maxCaptionLength = 1024

// Config holds configuration for the Telegram channel adapter.
type Config struct {
	BotToken  string                    // Telegram bot token from BotFather
	AllowFrom []string                  // Allowed sender IDs (empty = allow all)
	Commands  *channels.CommandRegistry // Slash commands to register with Telegram's menu
}

// TelegramChannel implements the channels.Channel interface for Telegram.
type TelegramChannel struct {
	config   *Config
	botAPI   *tgbotapi.BotAPI
	handler  channels.MessageHandler
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logger   *log.Logger
	mu       sync.RWMutex
	status   channels.ChannelStatus
	msgCount atomic.Int64

	// allowSet is built from config.AllowFrom for fast lookup.
	allowMu  sync.RWMutex
	allowSet map[string]bool

	// LinkHandler is called when a user sends /link <code> to the bot.
	// It is set by the ChannelManager after construction to bridge the
	// Telegram channel with the link code store. The handler should:
	//   1. Validate the code
	//   2. Create/verify the user_channel link
	//   3. Return (success bool, replyMessage string)
	// When nil, /link commands are ignored.
	LinkHandler func(ctx context.Context, senderID, senderUsername string, code string) (bool, string)
}

// New creates a new Telegram channel adapter.
func New(cfg *Config, logger *log.Logger) *TelegramChannel {
	if logger == nil {
		logger = log.Default()
	}

	allowSet := make(map[string]bool, len(cfg.AllowFrom))
	for _, id := range cfg.AllowFrom {
		allowSet[id] = true
	}

	return &TelegramChannel{
		config:   cfg,
		logger:   logger,
		allowSet: allowSet,
	}
}

// ID returns the channel identifier.
func (t *TelegramChannel) ID() string { return "telegram" }

// Name returns a human-readable name.
func (t *TelegramChannel) Name() string { return "Telegram Bot" }

// BotUsername returns the Telegram bot's @username (without the @ prefix).
// Returns empty string if the bot is not yet connected.
func (t *TelegramChannel) BotUsername() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.botAPI == nil {
		return ""
	}
	return t.botAPI.Self.UserName
}

// SetLinkHandler sets the callback for /link <code> commands.
func (t *TelegramChannel) SetLinkHandler(fn func(ctx context.Context, senderID, senderUsername, code string) (bool, string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.LinkHandler = fn
}

// Start connects to Telegram via long polling and begins processing updates.
// It calls handler for each normalized inbound message. Blocks until ctx
// is cancelled or Stop is called.
//
// If the polling loop exits abnormally (network error, Telegram API issue),
// Start will automatically reconnect with exponential backoff (3s → 5min cap).
// This ensures the channel survives transient outages without manual intervention.
func (t *TelegramChannel) Start(ctx context.Context, handler channels.MessageHandler) error {
	t.mu.Lock()
	t.handler = handler
	t.mu.Unlock()

	// Create cancellable context for the entire Start lifecycle
	pollCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	t.wg.Add(1)
	defer t.wg.Done()

	delay := initialReconnectDelay

	for {
		// Check if we should stop before attempting connection
		select {
		case <-pollCtx.Done():
			t.logger.Printf("[telegram] Polling stopped")
			return nil
		default:
		}

		err := t.connectAndPoll(pollCtx)
		if err == nil {
			// Clean exit (context cancelled)
			return nil
		}

		// Abnormal exit — attempt reconnect with backoff
		t.logger.Printf("[telegram] Connection lost: %v — reconnecting in %v", err, delay)
		t.setError(fmt.Sprintf("reconnecting: %v", err))

		select {
		case <-pollCtx.Done():
			return nil
		case <-time.After(delay):
		}

		// Exponential backoff
		delay = delay * 2
		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
	}
}

// connectAndPoll creates a bot API connection and polls for updates until the
// context is cancelled or an unrecoverable error occurs. Returns nil on clean
// shutdown (context done), or an error on abnormal exit.
func (t *TelegramChannel) connectAndPoll(ctx context.Context) error {
	// Use a custom HTTP client with a timeout to prevent hung connections.
	// The default http.Client{} has no timeout, which means a stale TCP
	// connection (e.g., after NAT table expiry) can block forever.
	httpClient := &http.Client{Timeout: httpClientTimeout}
	bot, err := tgbotapi.NewBotAPIWithClient(t.config.BotToken, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		return fmt.Errorf("failed to create bot API: %w", err)
	}

	t.mu.Lock()
	t.botAPI = bot
	t.status = channels.ChannelStatus{
		Connected:   true,
		AccountID:   bot.Self.UserName,
		ConnectedAt: time.Now(),
	}
	t.mu.Unlock()

	t.logger.Printf("[telegram] Connected as @%s (ID: %d)", bot.Self.UserName, bot.Self.ID)

	// Register slash commands with Telegram's bot menu so they appear
	// in the "/" autocomplete picker. This is persistent on Telegram's
	// servers — survives restarts. Idempotent on subsequent starts.
	t.registerBotCommands(bot)

	// Configure long polling
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 30

	updates := bot.GetUpdatesChan(updateConfig)

	for {
		select {
		case <-ctx.Done():
			bot.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				// Channel closed unexpectedly (library shutdown or error)
				return fmt.Errorf("updates channel closed unexpectedly")
			}
			if update.Message == nil {
				continue
			}
			t.processUpdate(ctx, update)
		}
	}
}

// Stop gracefully shuts down the Telegram adapter.
func (t *TelegramChannel) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}

	// Wait for the polling goroutine to finish, with a timeout
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-ctx.Done():
		t.logger.Printf("[telegram] Forced stop (context deadline)")
	}

	t.mu.Lock()
	t.status.Connected = false
	t.mu.Unlock()

	t.logger.Printf("[telegram] Stopped")
	return nil
}

// Send delivers an outbound message to a Telegram chat.
// Images are sent as photos first (with optional caption), then any remaining
// text is sent as message chunks respecting the 4096-char limit, and finally
// document attachments (file artifacts) are sent.
func (t *TelegramChannel) Send(ctx context.Context, target channels.Target, msg channels.OutboundMessage) error {
	t.mu.RLock()
	bot := t.botAPI
	t.mu.RUnlock()

	if bot == nil {
		return fmt.Errorf("telegram: bot not connected")
	}

	chatID, err := strconv.ParseInt(target.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", target.ChatID, err)
	}

	// --- Phase 1: Send images as photos ---
	// If we have images and the text is short enough, use the text as a
	// caption on the first photo (Telegram allows up to 1024 chars).
	// Otherwise, send photos without captions and text separately.
	textForCaption := ""
	remainingText := msg.Text

	if len(msg.Images) > 0 && remainingText != "" {
		// Convert markdown for caption if needed
		caption := remainingText
		if msg.Format == channels.FormatHTML {
			caption = MarkdownToHTML(remainingText)
		}
		if len(caption) <= maxCaptionLength {
			textForCaption = caption
			remainingText = "" // consumed as caption
		}
	}

	for i, img := range msg.Images {
		ext := img.Format
		if ext == "" {
			ext = "png"
		}
		photoBytes := tgbotapi.FileBytes{
			Name:  fmt.Sprintf("image.%s", ext),
			Bytes: img.Data,
		}
		photo := tgbotapi.NewPhoto(chatID, photoBytes)

		// Use text as caption on the first photo only
		if i == 0 && textForCaption != "" {
			photo.Caption = textForCaption
			if msg.Format == channels.FormatHTML {
				photo.ParseMode = "HTML"
			}
		}

		// Use image's own caption if set (overrides text caption)
		if img.Caption != "" {
			photo.Caption = img.Caption
		}

		// Reply-to on the first photo
		if i == 0 && msg.ReplyTo != "" {
			if replyID, err := strconv.Atoi(msg.ReplyTo); err == nil {
				photo.ReplyToMessageID = replyID
			}
		}

		if _, err := bot.Send(photo); err != nil {
			t.logger.Printf("[telegram] Failed to send photo: %v", err)
			// Non-fatal — continue with text
		}
	}

	// --- Phase 2: Send remaining text (if not consumed as caption) ---
	if strings.TrimSpace(remainingText) != "" {
		text := remainingText
		parseMode := ""
		if msg.Format == channels.FormatHTML {
			text = MarkdownToHTML(remainingText)
			parseMode = "HTML"
		}

		chunks := splitMessage(text, maxMessageLength)

		for i, chunk := range chunks {
			teleMsg := tgbotapi.NewMessage(chatID, chunk)
			teleMsg.ParseMode = parseMode

			// Set reply-to on the first chunk only (if not already set on a photo)
			if i == 0 && msg.ReplyTo != "" && len(msg.Images) == 0 {
				if replyID, err := strconv.Atoi(msg.ReplyTo); err == nil {
					teleMsg.ReplyToMessageID = replyID
				}
			}

			_, sendErr := bot.Send(teleMsg)
			if sendErr != nil {
				// If HTML parsing fails, strip tags and retry as plain text
				if parseMode == "HTML" && strings.Contains(sendErr.Error(), "can't parse") {
					t.logger.Printf("[telegram] HTML parse failed, retrying as plain text: %v", sendErr)
					teleMsg.Text = StripHTMLTags(chunk)
					teleMsg.ParseMode = ""
					_, retryErr := bot.Send(teleMsg)
					if retryErr != nil {
						return fmt.Errorf("telegram: send failed: %w", retryErr)
					}
					continue
				}
				return fmt.Errorf("telegram: send failed: %w", sendErr)
			}
		}
	}

	// --- Phase 3: Send document attachments (file artifacts) ---
	for _, doc := range msg.Documents {
		if len(doc.Data) == 0 {
			continue
		}
		filename := doc.Filename
		if filename == "" {
			filename = "file"
		}
		docBytes := tgbotapi.FileBytes{
			Name:  filename,
			Bytes: doc.Data,
		}
		teleDoc := tgbotapi.NewDocument(chatID, docBytes)
		if doc.Caption != "" {
			teleDoc.Caption = doc.Caption
		}
		if _, err := bot.Send(teleDoc); err != nil {
			t.logger.Printf("[telegram] Failed to send document %s: %v", filename, err)
			// Non-fatal — continue with remaining documents
		}
	}

	return nil
}

// SendTyping sends a "typing" chat action to the Telegram chat.
// This shows a "typing..." indicator in the chat for ~5 seconds.
func (t *TelegramChannel) SendTyping(ctx context.Context, target channels.Target) error {
	t.mu.RLock()
	bot := t.botAPI
	t.mu.RUnlock()

	if bot == nil {
		return nil
	}

	chatID, err := strconv.ParseInt(target.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", target.ChatID, err)
	}

	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, err = bot.Request(action)
	return err
}

// Status returns the current channel status.
func (t *TelegramChannel) Status() channels.ChannelStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := t.status
	status.MessageCount = t.msgCount.Load()
	return status
}

// BroadcastTargets returns a Target for each allowed user.
// In direct messages, Telegram chat ID == user ID, so each AllowFrom
// entry becomes a delivery target.
func (t *TelegramChannel) BroadcastTargets() []channels.Target {
	t.allowMu.RLock()
	defer t.allowMu.RUnlock()
	targets := make([]channels.Target, 0, len(t.allowSet))
	for id := range t.allowSet {
		targets = append(targets, channels.Target{
			ChannelID: "telegram",
			ChatID:    id,
		})
	}
	return targets
}

// processUpdate handles a single Telegram update.
func (t *TelegramChannel) processUpdate(ctx context.Context, update tgbotapi.Update) {
	msg := update.Message
	if msg == nil || msg.Text == "" {
		return
	}

	senderID := strconv.FormatInt(msg.From.ID, 10)

	// Handle /link command BEFORE allowlist check — users trying to link
	// their account are not yet in the allowlist.
	if msg.IsCommand() && msg.Command() == "link" {
		t.handleLinkCommand(ctx, msg, senderID)
		return
	}

	// Allowlist check — only explicitly allowed senders can interact.
	// An empty allowlist blocks everyone (safe default for a bot with tool access).
	t.allowMu.RLock()
	allowed := t.allowSet[senderID]
	t.allowMu.RUnlock()
	if !allowed {
		t.logger.Printf("[telegram] Blocked message from unauthorized sender %s (%s)",
			senderID, msg.From.UserName)
		return
	}

	t.msgCount.Add(1)

	// Handle /start command with a welcome message
	if msg.IsCommand() && msg.Command() == "start" {
		t.sendWelcome(msg)
		return
	}

	// Determine chat type
	chatType := channels.ChatTypeDirect
	if msg.Chat.IsGroup() || msg.Chat.IsSuperGroup() {
		chatType = channels.ChatTypeGroup
	} else if msg.Chat.IsChannel() {
		chatType = channels.ChatTypeChannel
	}

	// Build sender name
	senderName := msg.From.FirstName
	if msg.From.LastName != "" {
		senderName += " " + msg.From.LastName
	}
	if senderName == "" {
		senderName = msg.From.UserName
	}

	// Build reply-to
	var replyTo string
	if msg.ReplyToMessage != nil {
		replyTo = strconv.Itoa(msg.ReplyToMessage.MessageID)
	}

	inbound := channels.InboundMessage{
		ID:         strconv.Itoa(msg.MessageID),
		ChannelID:  "telegram",
		SenderID:   senderID,
		SenderName: senderName,
		ChatID:     strconv.FormatInt(msg.Chat.ID, 10),
		ChatType:   chatType,
		Text:       msg.Text,
		ReplyTo:    replyTo,
		Timestamp:  msg.Time(),
		Raw:        update,
	}

	if err := t.handler(ctx, inbound); err != nil {
		t.logger.Printf("[telegram] Handler error for message %d: %v", msg.MessageID, err)
	}
}

// sendWelcome sends a greeting message when a user sends /start.
func (t *TelegramChannel) sendWelcome(msg *tgbotapi.Message) {
	t.mu.RLock()
	bot := t.botAPI
	t.mu.RUnlock()

	if bot == nil {
		return
	}

	name := msg.From.FirstName
	if name == "" {
		name = msg.From.UserName
	}
	if name == "" {
		name = "there"
	}

	text := fmt.Sprintf("Hey %s! I'm Astonish, your AI assistant. Send me a message and I'll help you out.", name)
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	if _, err := bot.Send(reply); err != nil {
		t.logger.Printf("[telegram] Failed to send welcome message: %v", err)
	}
}

// setError updates the status with an error message.
func (t *TelegramChannel) setError(errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Error = errMsg
	t.status.Connected = false
}

// UpdateAllowlist replaces the sender allowlist at runtime without requiring
// a channel restart. Implements the channels.AllowlistUpdater interface.
func (t *TelegramChannel) UpdateAllowlist(allowFrom []string) {
	newSet := make(map[string]bool, len(allowFrom))
	for _, id := range allowFrom {
		newSet[id] = true
	}

	t.allowMu.Lock()
	t.allowSet = newSet
	t.allowMu.Unlock()

	t.logger.Printf("[telegram] Allowlist updated (%d entries)", len(allowFrom))
}

// registerBotCommands calls Telegram's setMyCommands API to populate the
// "/" command menu. Includes all commands from the registry plus /start.
// Best-effort: logs errors but does not fail the bot startup.
func (t *TelegramChannel) registerBotCommands(bot *tgbotapi.BotAPI) {
	t.registerBotCommandsFromRegistry(bot, t.config.Commands)
}

// RefreshCommands implements channels.CommandRefresher. It re-registers bot
// commands with Telegram after new commands have been added (e.g., fleet commands).
func (t *TelegramChannel) RefreshCommands(commands *channels.CommandRegistry) {
	t.mu.RLock()
	bot := t.botAPI
	t.mu.RUnlock()
	if bot == nil {
		return
	}
	t.registerBotCommandsFromRegistry(bot, commands)
}

// registerBotCommandsFromRegistry calls Telegram's setMyCommands API with
// all commands from the given registry plus /start.
func (t *TelegramChannel) registerBotCommandsFromRegistry(bot *tgbotapi.BotAPI, commands *channels.CommandRegistry) {
	cmds := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start a conversation with Astonish"},
	}

	if commands != nil {
		for _, cmd := range commands.List() {
			cmds = append(cmds, tgbotapi.BotCommand{
				Command:     cmd.Name,
				Description: cmd.Description,
			})
		}
	}

	cfg := tgbotapi.NewSetMyCommands(cmds...)
	if _, err := bot.Request(cfg); err != nil {
		t.logger.Printf("[telegram] Failed to register bot commands: %v", err)
	} else {
		t.logger.Printf("[telegram] Registered %d bot commands", len(cmds))
	}
}

// splitMessage splits text into chunks that fit within maxLen,
// preferring to split at paragraph boundaries, then line boundaries,
// then spaces. Preserves code blocks across chunks.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find the best split point within maxLen
		chunk := remaining[:maxLen]
		splitAt := maxLen

		// Try to split at paragraph boundary
		if idx := strings.LastIndex(chunk, "\n\n"); idx > maxLen/4 {
			splitAt = idx + 2
		} else if idx := strings.LastIndex(chunk, "\n"); idx > maxLen/4 {
			// Try line boundary
			splitAt = idx + 1
		} else if idx := strings.LastIndex(chunk, " "); idx > maxLen/4 {
			// Try word boundary
			splitAt = idx + 1
		}

		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// --- Markdown to Telegram HTML conversion ---

var (
	// fencedCodeBlockRe matches fenced code blocks with optional language.
	fencedCodeBlockRe = regexp.MustCompile("(?s)```(\\w*)\\n?(.*?)```")
	// inlineCodeRe matches inline code spans.
	inlineCodeRe = regexp.MustCompile("`([^`]+)`")
	// boldRe matches **text** bold markers.
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	// underscoreItalicRe matches _text_ italic markers.
	underscoreItalicRe = regexp.MustCompile(`(?:^|[^\w])_([^_]+?)_(?:[^\w]|$)`)
	// headingRe matches markdown headings (##+ text).
	headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	// htmlTagRe matches any HTML tag for stripping.
	htmlTagRe = regexp.MustCompile(`<[^>]+>`)
	// tableRowRe matches markdown table rows (lines with | delimiters).
	tableRowRe = regexp.MustCompile(`(?m)^\|(.+)\|$`)
	// tableSepRe matches table separator rows (|---|---|).
	tableSepRe = regexp.MustCompile(`(?m)^\|[\s\-:|]+\|$`)
)

// MarkdownToHTML converts standard markdown to Telegram-supported HTML.
// Telegram supports: <b>, <i>, <code>, <pre>, <a href="...">.
// Unsupported constructs (tables, images) are converted to plain text.
// This function is also reused by the email channel for HTML rendering.
func MarkdownToHTML(text string) string {
	// Step 1: Extract fenced code blocks and replace with placeholders.
	// This prevents HTML-escaping and formatting inside code.
	var codeBlocks []string
	text = fencedCodeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := fencedCodeBlockRe.FindStringSubmatch(match)
		lang := parts[1]
		code := parts[2]
		// Code block content gets HTML-escaped but NO other formatting
		escaped := html.EscapeString(code)
		escaped = strings.TrimRight(escaped, "\n")
		var block string
		if lang != "" {
			block = fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", lang, escaped)
		} else {
			block = fmt.Sprintf("<pre>%s</pre>", escaped)
		}
		placeholder := fmt.Sprintf("\x00CODE%d\x00", len(codeBlocks))
		codeBlocks = append(codeBlocks, block)
		return placeholder
	})

	// Step 2: Extract inline code and replace with placeholders.
	var inlineCodes []string
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := inlineCodeRe.FindStringSubmatch(match)
		escaped := html.EscapeString(parts[1])
		block := fmt.Sprintf("<code>%s</code>", escaped)
		placeholder := fmt.Sprintf("\x00INLINE%d\x00", len(inlineCodes))
		inlineCodes = append(inlineCodes, block)
		return placeholder
	})

	// Step 3: HTML-escape the remaining text (outside code).
	text = html.EscapeString(text)

	// Step 4: Convert markdown tables to plain text bullet lists.
	// Remove separator rows first, then convert data rows.
	text = tableSepRe.ReplaceAllString(text, "")
	text = tableRowRe.ReplaceAllStringFunc(text, func(match string) string {
		// Strip leading/trailing pipes and split cells
		inner := strings.Trim(match, "|")
		cells := strings.Split(inner, "|")
		var parts []string
		for _, cell := range cells {
			cell = strings.TrimSpace(cell)
			if cell != "" {
				parts = append(parts, cell)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return "• " + strings.Join(parts, " — ")
	})

	// Step 5: Convert headings to bold text.
	text = headingRe.ReplaceAllString(text, "<b>$1</b>")

	// Step 6: Convert bold (**text**) — must come before italic.
	// The ** was escaped to **** by html.EscapeString (stars aren't escaped),
	// but &amp; etc might be inside. We need to match on the escaped text.
	// Actually, html.EscapeString doesn't escape * or _, so patterns still match.
	text = boldRe.ReplaceAllString(text, "<b>$1</b>")

	// Step 7: Convert italic (*text*). Need to be careful not to match inside bold tags.
	// Simple approach: convert remaining single * pairs.
	text = regexp.MustCompile(`(?:^|[^*<])\*([^*<>]+?)\*`).ReplaceAllStringFunc(text, func(match string) string {
		// Preserve leading character if present
		idx := strings.Index(match, "*")
		prefix := match[:idx]
		inner := match[idx+1 : len(match)-1]
		return prefix + "<i>" + inner + "</i>"
	})

	// Step 8: Convert _italic_ (only at word boundaries, not inside HTML attributes).
	text = underscoreItalicRe.ReplaceAllStringFunc(text, func(match string) string {
		idx := strings.Index(match, "_")
		lastIdx := strings.LastIndex(match, "_")
		if idx == lastIdx {
			return match
		}
		prefix := match[:idx]
		inner := match[idx+1 : lastIdx]
		suffix := match[lastIdx+1:]
		return prefix + "<i>" + inner + "</i>" + suffix
	})

	// Step 9: Convert list items (- item or * item) to bullet points.
	text = regexp.MustCompile(`(?m)^[\-\*]\s+`).ReplaceAllString(text, "• ")

	// Step 10: Restore inline code placeholders.
	for i, code := range inlineCodes {
		placeholder := fmt.Sprintf("\x00INLINE%d\x00", i)
		// Placeholders were HTML-escaped in step 3, so match escaped version
		escapedPlaceholder := html.EscapeString(placeholder)
		text = strings.Replace(text, escapedPlaceholder, code, 1)
	}

	// Step 11: Restore code block placeholders.
	for i, block := range codeBlocks {
		placeholder := fmt.Sprintf("\x00CODE%d\x00", i)
		escapedPlaceholder := html.EscapeString(placeholder)
		text = strings.Replace(text, escapedPlaceholder, block, 1)
	}

	// Step 12: Clean up excess blank lines.
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// StripHTMLTags removes all HTML tags from text, used as a fallback when
// Telegram rejects the HTML.
func StripHTMLTags(text string) string {
	text = htmlTagRe.ReplaceAllString(text, "")
	return html.UnescapeString(text)
}

// handleLinkCommand processes the /link <code> command from a user.
// This is called BEFORE the allowlist check so unlinked users can link their account.
func (t *TelegramChannel) handleLinkCommand(ctx context.Context, msg *tgbotapi.Message, senderID string) {
	t.mu.RLock()
	bot := t.botAPI
	t.mu.RUnlock()
	if bot == nil {
		return
	}

	chatID := msg.Chat.ID

	// Extract code from command arguments
	code := strings.TrimSpace(msg.CommandArguments())
	if code == "" {
		reply := tgbotapi.NewMessage(chatID,
			"To link your account, use: /link <code>\n\n"+
				"Get your link code from the web UI:\n"+
				"Settings → Connected Channels → Link Telegram")
		reply.ParseMode = ""
		_, _ = bot.Send(reply)
		return
	}

	// Check if LinkHandler is set
	if t.LinkHandler == nil {
		reply := tgbotapi.NewMessage(chatID,
			"Account linking is not available. Please contact your administrator.")
		_, _ = bot.Send(reply)
		return
	}

	// Call the link handler
	senderUsername := msg.From.UserName
	success, replyText := t.LinkHandler(ctx, senderID, senderUsername, code)

	if success {
		t.logger.Printf("[telegram] Account linked: TG user %s (%s)", senderID, senderUsername)
		// Add the sender to the allowlist immediately so they can start chatting
		t.allowMu.Lock()
		t.allowSet[senderID] = true
		t.allowMu.Unlock()
	}

	reply := tgbotapi.NewMessage(chatID, replyText)
	reply.ParseMode = ""
	_, _ = bot.Send(reply)
}

