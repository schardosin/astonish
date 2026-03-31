package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
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

// SeenIssueState tracks the fleet's knowledge of a GitHub issue.
// This is a stateless model — there is no "agents"/"customer"/"failed" status.
// GitHub is the single source of truth (open + labeled = needs attention).
// The state file only stores the comment cursor and retry/failure tracking.
type SeenIssueState struct {
	SessionID     string    `json:"session_id,omitempty"`
	IssueTitle    string    `json:"issue_title,omitempty"`
	LastCommentID int64     `json:"last_comment_id,omitempty"`
	RetryCount    int       `json:"retry_count,omitempty"`
	LastFailedAt  time.Time `json:"last_failed_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`

	// Legacy fields (ignored, kept for backward compatibility with old state files)
	FirstSeenAt time.Time `json:"first_seen_at,omitempty"`
	Status      string    `json:"status,omitempty"`
	FailedAt    time.Time `json:"failed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// WorkItem represents an issue that needs a fleet session started or recovered.
// Returned by CheckForWork to the plan activator callback.
type WorkItem struct {
	IssueNumber   int
	IssueTitle    string
	SessionID     string // non-empty if this is a recovery (existing session)
	CustomerReply string // non-empty if triggered by a customer comment
	IsNewIssue    bool   // true if this issue has never been seen before
	LastCommentID int64  // current cursor value (for the callback to pass through)
}

// IssueNeedingAttention represents a failed issue shown in the Fleet UI.
// Replaces the old FailedIssue type.
type IssueNeedingAttention struct {
	IssueNumber  int       `json:"issue_number"`
	SessionID    string    `json:"session_id"`
	Error        string    `json:"error"`
	RetryCount   int       `json:"retry_count"`
	LastFailedAt time.Time `json:"last_failed_at"`
}

// maxRetryCount is the number of consecutive failures before an issue stops
// being auto-retried. After this, manual intervention via the Fleet UI is required.
const maxRetryCount = 3

// retryBackoffDurations defines the backoff intervals for each retry attempt.
var retryBackoffDurations = []time.Duration{
	1 * time.Minute,  // after 1st failure
	5 * time.Minute,  // after 2nd failure
	15 * time.Minute, // after 3rd failure (final)
}

// GitHubMonitorState is the persisted state for a GitHub monitor.
type GitHubMonitorState struct {
	SeenIssues map[int]*SeenIssueState `json:"seen_issues"`
	LastPollAt time.Time               `json:"last_poll_at"`
}

// GitHubMonitor polls a GitHub repository for new issues matching configured
// filters. It uses a stateless, label-based model where GitHub is the single
// source of truth: every poll cycle fetches open labeled issues and checks for
// customer replies.
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
	if len(labels) == 0 {
		// Support singular "label" key as well (common in YAML configs).
		labels = getConfigStringSlice(channelConfig, "label")
	}

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
		_ = os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("renaming monitor state: %w", err)
	}

	return nil
}

func (m *GitHubMonitor) statePath() string {
	return filepath.Join(m.StateDir, ".state", m.PlanKey+".json")
}

// CheckForWork is the unified poll function. On each call it fetches all open
// issues with the fleet label from GitHub and checks whether the latest
// non-fleet human comment has an ID higher than the stored cursor. If so, the
// callback is invoked. For brand-new issues (no state entry), cursor is
// effectively 0 so the issue body itself triggers.
//
// The isSessionActive function should check the in-memory session registry.
func (m *GitHubMonitor) CheckForWork(isSessionActive func(sessionID string) bool, callback func(WorkItem)) error {
	if m.Repo == "" {
		return fmt.Errorf("no repo configured for GitHub monitor")
	}

	// Fetch all open labeled issues from GitHub (the source of truth).
	issues, err := m.fetchOpenLabeledIssues()
	if err != nil {
		return fmt.Errorf("fetching open issues: %w", err)
	}

	m.mu.Lock()
	m.state.LastPollAt = time.Now()
	m.mu.Unlock()

	for _, issue := range issues {
		m.mu.Lock()
		seen := m.state.SeenIssues[issue.Number]
		m.mu.Unlock()

		if seen == nil {
			// Brand new issue — never seen before. Trigger immediately.
			callback(WorkItem{
				IssueNumber: issue.Number,
				IssueTitle:  issue.Title,
				IsNewIssue:  true,
			})
			continue
		}

		// Issue is known. Skip if a session is already running for it.
		if seen.SessionID != "" && isSessionActive(seen.SessionID) {
			continue
		}

		// Skip if in retry backoff after failures.
		if seen.RetryCount > 0 && !m.isBackoffExpired(seen) {
			continue
		}

		// Skip if max retries exceeded (needs manual "Retry" from Fleet UI).
		if seen.RetryCount >= maxRetryCount {
			continue
		}

		// Check for a new human comment past the cursor.
		customerComment, latestCommentID, fetchErr := m.fetchLatestCustomerComment(issue.Number, seen.LastCommentID)
		if fetchErr != nil {
			slog.Error("error fetching comments for issue", "component", "github-monitor", "issue", issue.Number, "error", fetchErr)
			continue
		}

		// Advance cursor to highest comment ID so fleet-posted comments
		// don't cause false positives on the next cycle.
		if latestCommentID > seen.LastCommentID {
			m.UpdateCursor(issue.Number, latestCommentID)
		}

		if customerComment == nil {
			continue // no new human comment — nothing to do
		}

		slog.Info("new human comment detected", "component", "github-monitor", "comment_id", customerComment.ID, "issue", issue.Number, "cursor", seen.LastCommentID)

		callback(WorkItem{
			IssueNumber:   issue.Number,
			IssueTitle:    seen.IssueTitle,
			SessionID:     seen.SessionID,
			CustomerReply: customerComment.Body,
			LastCommentID: seen.LastCommentID,
		})
	}

	if err := m.SaveState(); err != nil {
		slog.Warn("failed to save state after poll", "component", "github-monitor", "error", err)
	}

	return nil
}

// fetchOpenLabeledIssues fetches open issues with the configured labels.
func (m *GitHubMonitor) fetchOpenLabeledIssues() ([]GitHubIssue, error) {
	args := []string{"issue", "list", "--repo", m.Repo, "--state", "open",
		"--json", "number,title,body,labels,assignees,url,state,createdAt,author"}

	for _, label := range m.Labels {
		args = append(args, "--label", label)
	}

	args = append(args, "--limit", "50")

	out, err := ghCommand(m.GHToken, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue list failed: %w (output: %s)", err, strings.TrimSpace(out))
	}

	var issues []GitHubIssue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}

	return issues, nil
}

// fetchLatestCustomerComment fetches comments on an issue and returns the
// latest non-fleet human comment with ID > cursor. Also returns the highest
// comment ID across ALL comments (for cursor advancement).
//
// Returns (nil, highestID, nil) if no new customer comment was found.
func (m *GitHubMonitor) fetchLatestCustomerComment(issueNumber int, cursor int64) (*ghIssueComment, int64, error) {
	out, err := ghCommand(m.GHToken, "api",
		fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", m.Repo, issueNumber))
	if err != nil {
		return nil, 0, fmt.Errorf("fetching comments for issue #%d: %w", issueNumber, err)
	}

	var comments []ghIssueComment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		return nil, 0, fmt.Errorf("parsing comments for issue #%d: %w", issueNumber, err)
	}

	var highestID int64
	var latestCustomer *ghIssueComment

	for i, c := range comments {
		if c.ID > highestID {
			highestID = c.ID
		}
		// Skip fleet-generated comments
		if strings.Contains(c.Body, fleetCommentMarker) {
			continue
		}
		// Only consider comments newer than the cursor
		if c.ID > cursor {
			latestCustomer = &comments[i]
		}
	}

	// If >100 comments and we didn't find anything yet, paginate.
	if len(comments) == 100 && latestCustomer == nil {
		out, err = ghCommand(m.GHToken, "api", "--paginate",
			fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100&page=2", m.Repo, issueNumber))
		if err != nil {
			return nil, highestID, fmt.Errorf("fetching paginated comments for issue #%d: %w", issueNumber, err)
		}

		var moreComments []ghIssueComment
		if err := json.Unmarshal([]byte(out), &moreComments); err != nil {
			return nil, highestID, fmt.Errorf("parsing paginated comments for issue #%d: %w", issueNumber, err)
		}

		for i, c := range moreComments {
			if c.ID > highestID {
				highestID = c.ID
			}
			if strings.Contains(c.Body, fleetCommentMarker) {
				continue
			}
			if c.ID > cursor {
				latestCustomer = &moreComments[i]
			}
		}
	}

	return latestCustomer, highestID, nil
}

// isBackoffExpired returns true if enough time has passed since the last failure
// for the issue to be retried.
func (m *GitHubMonitor) isBackoffExpired(s *SeenIssueState) bool {
	if s.RetryCount <= 0 || s.LastFailedAt.IsZero() {
		return true
	}

	idx := s.RetryCount - 1
	if idx >= len(retryBackoffDurations) {
		idx = len(retryBackoffDurations) - 1
	}

	return time.Since(s.LastFailedAt) >= retryBackoffDurations[idx]
}

// MarkSeen records that an issue has been seen and a session started for it.
// Called when a new fleet session is created for an issue.
func (m *GitHubMonitor) MarkSeen(issueNumber int, sessionID string, issueTitle string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.SeenIssues[issueNumber] = &SeenIssueState{
		SessionID:  sessionID,
		IssueTitle: issueTitle,
	}

	if err := m.saveStateLocked(); err != nil {
		slog.Warn("failed to persist state after marking issue seen", "component", "github-monitor", "issue", issueNumber, "error", err)
	}
}

// UpdateCursor advances the comment cursor for an issue. The cursor only
// moves forward (never regresses). Called by BallChangeFunc callbacks and
// internally after fetching comments.
func (m *GitHubMonitor) UpdateCursor(issueNumber int, commentID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		return
	}

	if commentID > s.LastCommentID {
		s.LastCommentID = commentID
		if err := m.saveStateLocked(); err != nil {
			slog.Warn("failed to persist cursor", "component", "github-monitor", "issue", issueNumber, "error", err)
		}
	}
}

// IncrementRetryCount records a failure for an issue. After maxRetryCount
// consecutive failures, the issue stops being auto-retried.
func (m *GitHubMonitor) IncrementRetryCount(issueNumber int, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		s = &SeenIssueState{}
		m.state.SeenIssues[issueNumber] = s
	}

	s.RetryCount++
	s.LastFailedAt = time.Now()
	s.LastError = errMsg

	if err := m.saveStateLocked(); err != nil {
		slog.Warn("failed to persist retry count", "component", "github-monitor", "issue", issueNumber, "error", err)
	}

	slog.Error("issue failed", "component", "github-monitor", "issue", issueNumber, "retry", s.RetryCount, "max_retries", maxRetryCount, "error", errMsg)
}

// ResetRetryCount clears the failure state for an issue, allowing it to be
// picked up again on the next poll cycle. Called by the "Retry" button in the
// Fleet UI.
func (m *GitHubMonitor) ResetRetryCount(issueNumber int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		return fmt.Errorf("issue #%d not found in state", issueNumber)
	}

	s.RetryCount = 0
	s.LastFailedAt = time.Time{}
	s.LastError = ""

	if err := m.saveStateLocked(); err != nil {
		return fmt.Errorf("persisting state: %w", err)
	}

	slog.Info("issue retry count reset", "component", "github-monitor", "issue", issueNumber)
	return nil
}

// ClearRetryOnSuccess resets retry tracking after a successful session completion.
// Called by CompletionFunc when err == nil.
func (m *GitHubMonitor) ClearRetryOnSuccess(issueNumber int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		return
	}

	if s.RetryCount > 0 {
		s.RetryCount = 0
		s.LastFailedAt = time.Time{}
		s.LastError = ""
		if err := m.saveStateLocked(); err != nil {
			slog.Warn("failed to clear retry state", "component", "github-monitor", "issue", issueNumber, "error", err)
		}
	}
}

// GetIssuesNeedingAttention returns issues that have exceeded maxRetryCount
// and need manual intervention via the Fleet UI. Replaces GetFailedIssues.
func (m *GitHubMonitor) GetIssuesNeedingAttention() []IssueNeedingAttention {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []IssueNeedingAttention
	for num, s := range m.state.SeenIssues {
		if s.RetryCount >= maxRetryCount {
			result = append(result, IssueNeedingAttention{
				IssueNumber:  num,
				SessionID:    s.SessionID,
				Error:        s.LastError,
				RetryCount:   s.RetryCount,
				LastFailedAt: s.LastFailedAt,
			})
		}
	}
	return result
}

// GetIssueState returns the stored state for a tracked issue, or nil if not tracked.
func (m *GitHubMonitor) GetIssueState(issueNumber int) *SeenIssueState {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.state.SeenIssues[issueNumber]
	if !ok {
		return nil
	}
	// Return a copy to avoid data races
	copy := *s
	return &copy
}

// GetIssueTitle returns the stored title for a tracked issue, or empty string
// if the issue is not tracked or the title was not recorded.
func (m *GitHubMonitor) GetIssueTitle(issueNumber int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.state.SeenIssues[issueNumber]; ok {
		return s.IssueTitle
	}
	return ""
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

	for _, issue := range issues {
		if _, exists := m.state.SeenIssues[issue.Number]; !exists {
			m.state.SeenIssues[issue.Number] = &SeenIssueState{}
		}
	}

	m.state.LastPollAt = time.Now()
	slog.Info("marked existing issues as seen", "component", "github-monitor", "count", len(issues), "plan", m.PlanKey)

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
// Handles []string, []any (from YAML list), and plain string (single value).
func getConfigStringSlice(config map[string]any, key string) []string {
	if config == nil {
		return nil
	}
	v, ok := config[key]
	if !ok {
		return nil
	}

	switch val := v.(type) {
	case string:
		if val != "" {
			return []string{val}
		}
		return nil
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
