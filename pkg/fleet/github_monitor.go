package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GitHubIssue represents a GitHub issue returned by the gh CLI.
type GitHubIssue struct {
	Number    int               `json:"number"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Labels    []GitHubLabel     `json:"labels"`
	Assignees []GitHubAssignee  `json:"assignees"`
	URL       string            `json:"url"`
	State     string            `json:"state"`
	CreatedAt time.Time         `json:"createdAt"`
	Author    GitHubIssueAuthor `json:"author"`
}

// GitHubLabel is a label on a GitHub issue.
type GitHubLabel struct {
	Name string `json:"name"`
}

// GitHubAssignee is an assignee on a GitHub issue.
type GitHubAssignee struct {
	Login string `json:"login"`
}

// GitHubIssueAuthor is the author of a GitHub issue.
type GitHubIssueAuthor struct {
	Login string `json:"login"`
}

// SeenIssueState tracks who currently holds the "ball" for an issue.
//
// Status values:
//   - "agents": an agent was working or about to act. Recovered on restart.
//   - "customer": the last action was from agents (presenting results, asking questions).
//     The session waits for a customer reply. NOT recovered on restart; instead a
//     lightweight comment watcher polls for new customer comments and triggers
//     recovery when one arrives.
//   - "failed": agents had the ball but hit fatal errors (consecutive failures).
//     Requires manual intervention via the Fleet Management UI's "Continue" button.
type SeenIssueState struct {
	FirstSeenAt   time.Time `json:"first_seen_at"`
	Status        string    `json:"status"`                    // "agents", "customer", or "failed"
	SessionID     string    `json:"session_id,omitempty"`      // fleet session ID (for recovery)
	LastCommentID int64     `json:"last_comment_id,omitempty"` // highest GitHub comment ID seen (for polling)
	FailedAt      time.Time `json:"failed_at,omitempty"`       // when the session failed (status=failed only)
	Error         string    `json:"error,omitempty"`           // error message (status=failed only)
}

// AgentBallIssue is returned by GetAgentBallIssues for recovery on restart.
type AgentBallIssue struct {
	IssueNumber int
	SessionID   string
}

// CustomerBallIssue is returned by GetCustomerBallIssues for comment watching on restart.
type CustomerBallIssue struct {
	IssueNumber   int
	SessionID     string
	LastCommentID int64
}

// FailedIssue is returned by GetFailedIssues for the Fleet Management UI.
type FailedIssue struct {
	IssueNumber int       `json:"issue_number"`
	SessionID   string    `json:"session_id"`
	Error       string    `json:"error"`
	FailedAt    time.Time `json:"failed_at"`
}

// GitHubMonitorState is the persisted state for a GitHub monitor.
type GitHubMonitorState struct {
	SeenIssues map[int]*SeenIssueState `json:"seen_issues"`
	LastPollAt time.Time               `json:"last_poll_at"`
}

// GitHubMonitor polls a GitHub repository for new issues matching configured
// filters. It tracks which issues have already been seen to avoid duplicate
// fleet session triggers.
type GitHubMonitor struct {
	PlanKey  string
	Repo     string
	Labels   []string // filter: only issues with these labels
	StateDir string   // directory for persisting seen-issue state
	GHToken  string   // optional: injected as GH_TOKEN for gh CLI auth

	mu    sync.Mutex
	state GitHubMonitorState
}

// NewGitHubMonitor creates a monitor for a fleet plan's GitHub Issues channel.
func NewGitHubMonitor(planKey string, channelConfig map[string]any, stateDir string) *GitHubMonitor {
	repo := GetConfigString(channelConfig, "repo")
	labels := getConfigStringSlice(channelConfig, "labels")

	return &GitHubMonitor{
		PlanKey:  planKey,
		Repo:     repo,
		Labels:   labels,
		StateDir: stateDir,
		state: GitHubMonitorState{
			SeenIssues: make(map[int]*SeenIssueState),
		},
	}
}

// LoadState loads the persisted state from disk.
func (m *GitHubMonitor) LoadState() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.statePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no state yet, start fresh
		}
		return fmt.Errorf("reading monitor state: %w", err)
	}

	var state GitHubMonitorState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parsing monitor state: %w", err)
	}

	if state.SeenIssues == nil {
		state.SeenIssues = make(map[int]*SeenIssueState)
	}
	m.state = state
	return nil
}

// SaveState persists the current state to disk.
func (m *GitHubMonitor) SaveState() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveStateLocked()
}

func (m *GitHubMonitor) saveStateLocked() error {
	path := m.statePath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling monitor state: %w", err)
	}

	// Atomic write via temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing monitor state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming monitor state: %w", err)
	}

	return nil
}

func (m *GitHubMonitor) statePath() string {
	return filepath.Join(m.StateDir, ".state", m.PlanKey+".json")
}

// Poll checks for new GitHub issues matching the configured filters.
// Returns only issues that haven't been seen before.
func (m *GitHubMonitor) Poll() ([]GitHubIssue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Repo == "" {
		return nil, fmt.Errorf("no repo configured for GitHub monitor")
	}

	// Build gh command
	args := []string{"issue", "list", "--repo", m.Repo, "--state", "open",
		"--json", "number,title,body,labels,assignees,url,state,createdAt,author"}

	// Add label filters
	for _, label := range m.Labels {
		args = append(args, "--label", label)
	}

	// Limit to recent issues (avoid processing the entire backlog)
	args = append(args, "--limit", "20")

	out, err := ghCommand(m.GHToken, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue list failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	var issues []GitHubIssue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}

	// Filter out already-seen issues (both in_progress and completed)
	var newIssues []GitHubIssue
	for _, issue := range issues {
		if _, seen := m.state.SeenIssues[issue.Number]; !seen {
			newIssues = append(newIssues, issue)
		}
	}

	// Update poll timestamp
	m.state.LastPollAt = time.Now()

	return newIssues, nil
}

// MarkAgents marks an issue as having the ball with agents. Called when a new
// session starts or when a customer comment triggers re-activation. The issue will
// not be re-triggered by future polls. If the daemon restarts before the ball
// moves to "customer", GetAgentBallIssues returns it for recovery.
func (m *GitHubMonitor) MarkAgents(issueNumber int, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.SeenIssues[issueNumber] = &SeenIssueState{
		FirstSeenAt: time.Now(),
		Status:      "agents",
		SessionID:   sessionID,
	}

	if err := m.saveStateLocked(); err != nil {
		log.Printf("[github-monitor] Warning: failed to persist state after marking issue #%d agents: %v", issueNumber, err)
	}
}

// MarkCustomer marks an issue as having the ball with the customer. The agents have
// finished their current work (or asked a question) and the session is waiting
// for a customer reply on the GitHub issue. On daemon restart, these issues are
// NOT fully recovered; instead a lightweight comment watcher polls for new
// customer comments and triggers recovery when one arrives.
func (m *GitHubMonitor) MarkCustomer(issueNumber int, lastCommentID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.state.SeenIssues[issueNumber]; ok {
		s.Status = "customer"
		s.LastCommentID = lastCommentID
		s.Error = ""
		s.FailedAt = time.Time{}
	} else {
		m.state.SeenIssues[issueNumber] = &SeenIssueState{
			FirstSeenAt:   time.Now(),
			Status:        "customer",
			LastCommentID: lastCommentID,
		}
	}

	if err := m.saveStateLocked(); err != nil {
		log.Printf("[github-monitor] Warning: failed to persist state after marking issue #%d customer: %v", issueNumber, err)
	}
}

// MarkFailed marks an issue as failed due to session errors. Unlike completed
// issues, failed issues are NOT auto-recovered on daemon restart. They require
// manual intervention via the Fleet Management UI's "Continue" button.
func (m *GitHubMonitor) MarkFailed(issueNumber int, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.state.SeenIssues[issueNumber]; ok {
		s.Status = "failed"
		s.Error = errMsg
		s.FailedAt = time.Now()
	} else {
		m.state.SeenIssues[issueNumber] = &SeenIssueState{
			FirstSeenAt: time.Now(),
			Status:      "failed",
			Error:       errMsg,
			FailedAt:    time.Now(),
		}
	}

	if err := m.saveStateLocked(); err != nil {
		log.Printf("[github-monitor] Warning: failed to persist state after marking issue #%d failed: %v", issueNumber, err)
	}
}

// GetFailedIssues returns all issues that failed during processing.
// Used by the Fleet Management UI to display failures with a "Continue" button.
func (m *GitHubMonitor) GetFailedIssues() []FailedIssue {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []FailedIssue
	for num, state := range m.state.SeenIssues {
		if state.Status == "failed" && state.SessionID != "" {
			result = append(result, FailedIssue{
				IssueNumber: num,
				SessionID:   state.SessionID,
				Error:       state.Error,
				FailedAt:    state.FailedAt,
			})
		}
	}
	return result
}

// ResetToAgents changes a failed issue back to "agents" so recovery
// can resume it. Called by the retry handler when the user clicks "Continue".
func (m *GitHubMonitor) ResetToAgents(issueNumber int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		return fmt.Errorf("issue #%d not found in state", issueNumber)
	}
	if s.Status != "failed" {
		return fmt.Errorf("issue #%d is not in failed state (current: %s)", issueNumber, s.Status)
	}

	s.Status = "agents"
	s.Error = ""
	s.FailedAt = time.Time{}

	if err := m.saveStateLocked(); err != nil {
		return fmt.Errorf("persisting state: %w", err)
	}
	return nil
}

// GetAgentBallIssues returns all issues where agents had the ball when the
// daemon stopped. Used during restart to resume interrupted sessions.
func (m *GitHubMonitor) GetAgentBallIssues() []AgentBallIssue {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []AgentBallIssue
	for num, state := range m.state.SeenIssues {
		if state.Status == "agents" && state.SessionID != "" {
			result = append(result, AgentBallIssue{
				IssueNumber: num,
				SessionID:   state.SessionID,
			})
		}
	}
	return result
}

// GetCustomerBallIssues returns all issues where the ball is with the customer
// (waiting for a reply on the GitHub issue). Used during restart to set up
// lightweight comment watchers that trigger recovery when a reply arrives.
func (m *GitHubMonitor) GetCustomerBallIssues() []CustomerBallIssue {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []CustomerBallIssue
	for num, state := range m.state.SeenIssues {
		if state.Status == "customer" && state.SessionID != "" {
			result = append(result, CustomerBallIssue{
				IssueNumber:   num,
				SessionID:     state.SessionID,
				LastCommentID: state.LastCommentID,
			})
		}
	}
	return result
}

// MarkAllCurrentAsSeen polls current issues and marks them all as seen.
// This is used during activation to avoid processing the existing backlog.
func (m *GitHubMonitor) MarkAllCurrentAsSeen() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Repo == "" {
		return fmt.Errorf("no repo configured")
	}

	args := []string{"issue", "list", "--repo", m.Repo, "--state", "open",
		"--json", "number", "--limit", "100"}

	for _, label := range m.Labels {
		args = append(args, "--label", label)
	}

	out, err := ghCommand(m.GHToken, args...)
	if err != nil {
		return fmt.Errorf("gh issue list failed: %w", err)
	}

	var issues []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return fmt.Errorf("parsing gh output: %w", err)
	}

	now := time.Now()
	for _, issue := range issues {
		if _, exists := m.state.SeenIssues[issue.Number]; !exists {
			m.state.SeenIssues[issue.Number] = &SeenIssueState{
				FirstSeenAt: now,
				Status:      "customer", // pre-existing issues: ball is with customer (no action needed)
			}
		}
	}

	m.state.LastPollAt = now
	log.Printf("[github-monitor] Marked %d existing issues as seen for plan %q", len(issues), m.PlanKey)

	return m.saveStateLocked()
}

// ClearState removes the persisted state file.
func (m *GitHubMonitor) ClearState() error {
	path := m.statePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// FormatIssueContext builds a human-readable context string from a GitHub issue
// that serves as the initial message for a fleet session.
func FormatIssueContext(issue GitHubIssue, repo string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## New GitHub Issue #%d\n\n", issue.Number))
	sb.WriteString(fmt.Sprintf("**Repository:** %s\n", repo))
	sb.WriteString(fmt.Sprintf("**Title:** %s\n", issue.Title))
	sb.WriteString(fmt.Sprintf("**Author:** @%s\n", issue.Author.Login))
	sb.WriteString(fmt.Sprintf("**URL:** %s\n", issue.URL))

	if len(issue.Labels) > 0 {
		labels := make([]string, len(issue.Labels))
		for i, l := range issue.Labels {
			labels[i] = l.Name
		}
		sb.WriteString(fmt.Sprintf("**Labels:** %s\n", strings.Join(labels, ", ")))
	}

	if len(issue.Assignees) > 0 {
		assignees := make([]string, len(issue.Assignees))
		for i, a := range issue.Assignees {
			assignees[i] = "@" + a.Login
		}
		sb.WriteString(fmt.Sprintf("**Assignees:** %s\n", strings.Join(assignees, ", ")))
	}

	sb.WriteString(fmt.Sprintf("\n### Description\n\n%s\n", issue.Body))

	sb.WriteString("\n---\n")
	sb.WriteString("This issue was automatically picked up from GitHub. ")
	sb.WriteString("Work on it according to the team workflow. ")
	sb.WriteString("When done, the results will be posted back as a comment on the issue.\n")

	return sb.String()
}

// UpdateLastCommentID updates the persisted comment cursor for an issue.
// Called by the session's OnBallChange callback so that after a daemon restart
// the comment watcher knows which comments have already been processed.
func (m *GitHubMonitor) UpdateLastCommentID(issueNumber int, commentID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.state.SeenIssues[issueNumber]; ok {
		if commentID > s.LastCommentID {
			s.LastCommentID = commentID
			if err := m.saveStateLocked(); err != nil {
				log.Printf("[github-monitor] Warning: failed to persist comment cursor for issue #%d: %v", issueNumber, err)
			}
		}
	}
}

// customerReplyPollInterval is how often the comment watcher checks for new
// customer replies on issues where the ball is with the customer.
const customerReplyPollInterval = 60 * time.Second

// CustomerReplyCallback is called when a new customer comment is detected on an
// issue that was waiting for a customer reply. The monitor transitions the issue
// to "agents" status and the callback should trigger session recovery.
type CustomerReplyCallback func(issueNumber int, sessionID string, commentBody string)

// WatchForCustomerReplies starts a background goroutine that periodically checks
// all "customer" ball issues for new comments. When a new customer comment is found,
// it transitions the issue to "agents" and calls the callback to trigger
// session recovery.
//
// This is the lightweight alternative to full session recovery: instead of
// spinning up a complete FleetSession with channel and agent activation on
// restart, we just poll for new comments. When one arrives, we then do the
// full recovery.
func (m *GitHubMonitor) WatchForCustomerReplies(ctx context.Context, callback CustomerReplyCallback) {
	go func() {
		ticker := time.NewTicker(customerReplyPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkCustomerReplies(callback)
			}
		}
	}()
}

// checkCustomerReplies checks all "customer" ball issues for new comments.
func (m *GitHubMonitor) checkCustomerReplies(callback CustomerReplyCallback) {
	customerIssues := m.GetCustomerBallIssues()
	if len(customerIssues) == 0 {
		return
	}

	for _, issue := range customerIssues {
		comments := m.fetchNewComments(issue.IssueNumber, issue.LastCommentID)
		for _, comment := range comments {
			// Skip fleet-generated comments (contain our marker)
			if strings.Contains(comment.Body, fleetCommentMarker) {
				m.UpdateLastCommentID(issue.IssueNumber, comment.ID)
				continue
			}

			// Customer comment found. Transition to "agents" and notify.
			log.Printf("[github-monitor] New customer comment #%d on issue #%d, transitioning ball to agents",
				comment.ID, issue.IssueNumber)

			m.mu.Lock()
			if s, ok := m.state.SeenIssues[issue.IssueNumber]; ok {
				s.Status = "agents"
				s.LastCommentID = comment.ID
				_ = m.saveStateLocked()
			}
			m.mu.Unlock()

			callback(issue.IssueNumber, issue.SessionID, comment.Body)

			// Only process the first new human comment per issue per poll.
			// The recovered session will pick up any subsequent comments
			// via its own channel poller.
			break
		}
	}
}

// fetchNewComments fetches comments on an issue that are newer than lastCommentID.
func (m *GitHubMonitor) fetchNewComments(issueNumber int, lastCommentID int64) []ghIssueComment {
	out, err := ghCommand(m.GHToken, "api",
		fmt.Sprintf("repos/%s/issues/%d/comments?per_page=20&sort=created&direction=asc", m.Repo, issueNumber))
	if err != nil {
		log.Printf("[github-monitor] Error fetching comments for issue #%d: %v", issueNumber, err)
		return nil
	}

	var comments []ghIssueComment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		log.Printf("[github-monitor] Error parsing comments for issue #%d: %v", issueNumber, err)
		return nil
	}

	// Filter to only comments newer than the cursor
	var newer []ghIssueComment
	for _, c := range comments {
		if c.ID > lastCommentID {
			newer = append(newer, c)
		}
	}
	return newer
}

// ghCommand runs a gh CLI command and returns the output.
// If ghToken is non-empty, it is injected as GH_TOKEN in the command
// environment, overriding the ambient gh auth session.
func ghCommand(ghToken string, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if ghToken != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+ghToken)
	}
	err := cmd.Run()
	return out.String(), err
}

// GetConfigString extracts a string from a config map.
func GetConfigString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	v, ok := config[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// getConfigStringSlice extracts a string slice from a config map.
func getConfigStringSlice(config map[string]any, key string) []string {
	if config == nil {
		return nil
	}
	v, ok := config[key]
	if !ok {
		return nil
	}

	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
