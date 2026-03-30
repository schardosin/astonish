package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// fleetCommentMarker is an HTML comment embedded in every GitHub comment
// posted by the fleet. The comment poller uses this to skip fleet-generated
// comments so they are not re-ingested as human messages.
const fleetCommentMarker = "<!-- astonish-fleet-msg -->"

// defaultCommentPollInterval is how often the channel polls for new human
// comments on the issue.
const defaultCommentPollInterval = 15 * time.Second

// GitHubIssueChannel implements Channel using a GitHub issue as the
// communication medium. Agent messages are posted as comments on the issue,
// and human replies (new comments without the fleet marker) are ingested
// back into the fleet session.
//
// The channel also maintains an in-memory message list and subscriber map
// so it works with the JSONL transcript, SSE streaming, and the WaitForMessage
// cursor used by the session manager's Run loop.
type GitHubIssueChannel struct {
	repo        string // "owner/repo"
	issueNumber int
	ghToken     string // optional: injected as GH_TOKEN for gh CLI auth

	// Internal ordered message list (same role as ChatChannel.messages).
	messages   []Message
	readCursor int // index of next message to return from WaitForMessage
	mu         sync.RWMutex
	cond       *sync.Cond
	closed     bool

	// Comment polling state.
	lastCommentID int64     // highest REST API comment ID we have seen
	lastPollAt    time.Time // timestamp sent as ?since= to the API
	pollInterval  time.Duration
	pollCancel    context.CancelFunc

	// Pub/sub for SSE viewers (same pattern as ChatChannel).
	subscribers   map[string]chan Message
	subscribersMu sync.Mutex
}

// NewGitHubIssueChannel creates a channel backed by a GitHub issue.
// Call StartPoller after the session is running to begin ingesting human comments.
// The optional ghToken parameter is injected as GH_TOKEN for gh CLI auth;
// pass empty string to use the ambient gh auth session.
func NewGitHubIssueChannel(repo string, issueNumber int, ghToken string) *GitHubIssueChannel {
	ch := &GitHubIssueChannel{
		repo:         repo,
		issueNumber:  issueNumber,
		ghToken:      ghToken,
		pollInterval: defaultCommentPollInterval,
		subscribers:  make(map[string]chan Message),
	}
	ch.cond = sync.NewCond(&ch.mu)
	return ch
}

// StartPoller begins the background goroutine that polls for new human
// comments on the issue. It should be called once, after the initial message
// has been posted (so lastCommentID is up to date).
func (c *GitHubIssueChannel) StartPoller(ctx context.Context) {
	pollCtx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	go c.commentPollLoop(pollCtx)
}

// ---------------------------------------------------------------------------
// Channel interface
// ---------------------------------------------------------------------------

// PostMessage adds a message to the internal list, notifies waiters and
// subscribers. It does NOT post to GitHub — the caller must use PostExternal
// for messages that should appear as GitHub issue comments. This separation
// lets the Run loop defer GitHub posting until after routing decisions.
func (c *GitHubIssueChannel) PostMessage(_ context.Context, msg Message) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("channel is closed")
	}

	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	c.messages = append(c.messages, msg)
	c.cond.Broadcast()
	c.mu.Unlock()

	// Notify SSE subscribers.
	c.notifySubscribers(msg)

	return nil
}

// PostExternal posts a message as a GitHub issue comment. This implements
// the ExternalPoster interface. It skips customer messages (they originate
// from GitHub) and intermediate messages (tagged in metadata).
func (c *GitHubIssueChannel) PostExternal(msg Message) {
	// Customer messages originate FROM GitHub; posting them back would duplicate.
	if msg.Sender == "customer" {
		return
	}

	// Skip intermediate progress messages.
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["intermediate"]; ok {
			if b, ok := v.(bool); ok && b {
				return
			}
		}
	}

	go c.postCommentAsync(msg)
}

// WaitForMessage blocks until a new message arrives that the Run loop hasn't
// seen yet. Uses the same read-cursor approach as ChatChannel.
func (c *GitHubIssueChannel) WaitForMessage(ctx context.Context) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for c.readCursor >= len(c.messages) && !c.closed {
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				c.cond.Broadcast()
			case <-done:
			}
		}()

		c.cond.Wait()
		close(done)

		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
	}

	if c.closed {
		return Message{}, fmt.Errorf("channel is closed")
	}

	msg := c.messages[c.readCursor]
	c.readCursor++
	return msg, nil
}

// GetThread returns all messages in chronological order.
func (c *GitHubIssueChannel) GetThread(_ context.Context) ([]Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]Message, len(c.messages))
	copy(result, c.messages)
	return result, nil
}

// GetAgentMemory returns messages belonging to the given agent's memory,
// plus system/global messages (no MemoryKeys). If agentKey is empty, returns
// all messages (same as GetThread). Supports backward compat with old ThreadKey.
func (c *GitHubIssueChannel) GetAgentMemory(_ context.Context, agentKey string) ([]Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if agentKey == "" {
		result := make([]Message, len(c.messages))
		copy(result, c.messages)
		return result, nil
	}

	result := make([]Message, 0, len(c.messages))
	for _, msg := range c.messages {
		if msg.InAgentMemory(agentKey) {
			result = append(result, msg)
		}
	}
	return result, nil
}

// Close stops the comment poller, wakes blocked waiters, and closes
// subscriber channels.
func (c *GitHubIssueChannel) Close() error {
	if c.pollCancel != nil {
		c.pollCancel()
	}

	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()

	c.subscribersMu.Lock()
	for id, ch := range c.subscribers {
		close(ch)
		delete(c.subscribers, id)
	}
	c.subscribersMu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// Subscribable interface (for SSE streaming)
// ---------------------------------------------------------------------------

// Subscribe registers a new message subscriber.
func (c *GitHubIssueChannel) Subscribe(id string) <-chan Message {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	ch := make(chan Message, 100)
	c.subscribers[id] = ch
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (c *GitHubIssueChannel) Unsubscribe(id string) {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	if ch, ok := c.subscribers[id]; ok {
		close(ch)
		delete(c.subscribers, id)
	}
}

func (c *GitHubIssueChannel) notifySubscribers(msg Message) {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	for _, ch := range c.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

// MessageCount returns the current number of messages.
func (c *GitHubIssueChannel) MessageCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages)
}

// LastCommentID returns the highest GitHub comment ID seen by this channel.
// Used to persist the comment cursor in the monitor state file so the comment
// watcher knows where to resume polling after a daemon restart.
func (c *GitHubIssueChannel) LastCommentID() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastCommentID
}

// LoadMessages pre-loads recovered messages into the channel and advances the
// read cursor past them. This is used during session recovery so that Run()
// does not re-process messages from the transcript.
func (c *GitHubIssueChannel) LoadMessages(messages []Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = messages
	c.readCursor = len(messages)
}

// ---------------------------------------------------------------------------
// GitHub comment posting
// ---------------------------------------------------------------------------

// postCommentAsync posts a message as a GitHub issue comment. It runs in a
// goroutine so PostMessage does not block on the network call.
func (c *GitHubIssueChannel) postCommentAsync(msg Message) {
	body := formatGitHubComment(msg)

	commentID, err := c.postComment(body)
	if err != nil {
		slog.Error("failed to post comment", "component", "github-channel", "sender", msg.Sender, "issue", c.issueNumber, "error", err)
		return
	}

	// Update the cursor so the poller skips this comment.
	c.mu.Lock()
	if commentID > c.lastCommentID {
		c.lastCommentID = commentID
	}
	c.mu.Unlock()
}

// postComment posts a comment via the REST API and returns the new comment's ID.
func (c *GitHubIssueChannel) postComment(body string) (int64, error) {
	// Use gh api to post (returns the created comment JSON).
	out, err := ghCommand(c.ghToken, "api",
		fmt.Sprintf("repos/%s/issues/%d/comments", c.repo, c.issueNumber),
		"-f", fmt.Sprintf("body=%s", body))
	if err != nil {
		return 0, fmt.Errorf("gh api failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		return 0, fmt.Errorf("parsing created comment: %w", err)
	}
	return created.ID, nil
}

// formatGitHubComment builds the markdown body for a GitHub issue comment.
func formatGitHubComment(msg Message) string {
	var sb strings.Builder

	switch msg.Sender {
	case "system":
		sb.WriteString("**[System]:**\n\n")
	default:
		sb.WriteString(fmt.Sprintf("**@%s:**\n\n", msg.Sender))
	}

	sb.WriteString(msg.Text)
	sb.WriteString("\n\n")
	sb.WriteString(fleetCommentMarker)

	return sb.String()
}

// ---------------------------------------------------------------------------
// Comment polling (human replies)
// ---------------------------------------------------------------------------

// ghIssueComment represents a comment from the GitHub REST API.
type ghIssueComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// commentPollLoop periodically checks for new comments on the issue that
// were NOT posted by the fleet (no marker). New customer comments are injected
// into the channel as Message{Sender: "customer"}.
func (c *GitHubIssueChannel) commentPollLoop(ctx context.Context) {
	// Seed lastPollAt so the first poll doesn't fetch the entire history.
	c.mu.Lock()
	if c.lastPollAt.IsZero() {
		c.lastPollAt = time.Now()
	}
	c.mu.Unlock()

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollComments()
		}
	}
}

// pollComments fetches recent comments and injects human ones into the channel.
func (c *GitHubIssueChannel) pollComments() {
	c.mu.RLock()
	since := c.lastPollAt.UTC().Format(time.RFC3339)
	lastID := c.lastCommentID
	c.mu.RUnlock()

	out, err := ghCommand(c.ghToken, "api",
		fmt.Sprintf("repos/%s/issues/%d/comments?since=%s&per_page=100", c.repo, c.issueNumber, since))
	if err != nil {
		slog.Error("error polling comments", "component", "github-channel", "issue", c.issueNumber, "error", err)
		return
	}

	var comments []ghIssueComment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		slog.Error("error parsing comments", "component", "github-channel", "error", err)
		return
	}

	now := time.Now()

	for _, comment := range comments {
		// Skip already-seen comments (by ID).
		if comment.ID <= lastID {
			continue
		}

		// Skip fleet-generated comments (contain our marker).
		if strings.Contains(comment.Body, fleetCommentMarker) {
			// Still advance the cursor past this comment.
			c.mu.Lock()
			if comment.ID > c.lastCommentID {
				c.lastCommentID = comment.ID
			}
			c.mu.Unlock()
			continue
		}

		// This is a customer comment. Inject it into the channel.
		ts, _ := time.Parse(time.RFC3339, comment.CreatedAt)
		if ts.IsZero() {
			ts = now
		}

		msg := Message{
			ID:        uuid.New().String(),
			Sender:    "customer",
			Text:      comment.Body,
			Timestamp: ts,
			Metadata: map[string]any{
				"github_comment_id": comment.ID,
				"github_author":     comment.User.Login,
			},
		}

		c.mu.Lock()
		c.messages = append(c.messages, msg)
		if comment.ID > c.lastCommentID {
			c.lastCommentID = comment.ID
		}
		c.cond.Broadcast()
		c.mu.Unlock()

		c.notifySubscribers(msg)

		slog.Info("ingested customer comment", "component", "github-channel", "comment_id", comment.ID, "author", comment.User.Login, "issue", c.issueNumber)
	}

	// Advance the poll timestamp.
	c.mu.Lock()
	c.lastPollAt = now
	c.mu.Unlock()
}

// SeedLastCommentID sets the initial comment cursor so that existing comments
// (including the fleet's own initial comment, if any) are not re-ingested.
// Call this after posting the initial message but before starting the poller.
func (c *GitHubIssueChannel) SeedLastCommentID() {
	// Fetch all current comments to find the highest ID.
	out, err := ghCommand(c.ghToken, "api",
		fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", c.repo, c.issueNumber))
	if err != nil {
		slog.Warn("could not seed comment cursor", "component", "github-channel", "error", err)
		return
	}

	var comments []ghIssueComment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		return
	}

	c.mu.Lock()
	for _, comment := range comments {
		if comment.ID > c.lastCommentID {
			c.lastCommentID = comment.ID
		}
	}
	c.lastPollAt = time.Now()
	c.mu.Unlock()
}
