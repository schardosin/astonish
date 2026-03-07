package fleet

import (
	"bytes"
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

// SeenIssueState tracks an individual issue's processing lifecycle.
type SeenIssueState struct {
	FirstSeenAt time.Time `json:"first_seen_at"`
	Status      string    `json:"status"`                 // "in_progress" or "completed"
	SessionID   string    `json:"session_id,omitempty"`   // fleet session ID (for recovery)
	CompletedAt time.Time `json:"completed_at,omitempty"` // when the session finished
}

// InProgressIssue is returned by GetInProgressIssues for recovery on restart.
type InProgressIssue struct {
	IssueNumber int
	SessionID   string
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

	mu    sync.Mutex
	state GitHubMonitorState
}

// NewGitHubMonitor creates a monitor for a fleet plan's GitHub Issues channel.
func NewGitHubMonitor(planKey string, channelConfig map[string]any, stateDir string) *GitHubMonitor {
	repo := getConfigString(channelConfig, "repo")
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

	out, err := ghCommand(args...)
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

// MarkInProgress marks an issue as claimed by a fleet session. The issue will
// not be re-triggered by future polls. If the daemon restarts before the
// session completes, GetInProgressIssues returns it for recovery.
func (m *GitHubMonitor) MarkInProgress(issueNumber int, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.SeenIssues[issueNumber] = &SeenIssueState{
		FirstSeenAt: time.Now(),
		Status:      "in_progress",
		SessionID:   sessionID,
	}

	if err := m.saveStateLocked(); err != nil {
		log.Printf("[github-monitor] Warning: failed to persist state after marking issue #%d in-progress: %v", issueNumber, err)
	}
}

// MarkCompleted marks an issue as fully processed. Called when the fleet
// session finishes (success or terminal error).
func (m *GitHubMonitor) MarkCompleted(issueNumber int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.state.SeenIssues[issueNumber]; ok {
		s.Status = "completed"
		s.CompletedAt = time.Now()
	} else {
		m.state.SeenIssues[issueNumber] = &SeenIssueState{
			FirstSeenAt: time.Now(),
			Status:      "completed",
			CompletedAt: time.Now(),
		}
	}

	if err := m.saveStateLocked(); err != nil {
		log.Printf("[github-monitor] Warning: failed to persist state after marking issue #%d completed: %v", issueNumber, err)
	}
}

// GetInProgressIssues returns all issues that were claimed but not yet
// completed. Used during daemon restart to resume interrupted sessions.
func (m *GitHubMonitor) GetInProgressIssues() []InProgressIssue {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []InProgressIssue
	for num, state := range m.state.SeenIssues {
		if state.Status == "in_progress" && state.SessionID != "" {
			result = append(result, InProgressIssue{
				IssueNumber: num,
				SessionID:   state.SessionID,
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

	out, err := ghCommand(args...)
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
				Status:      "completed",
				CompletedAt: now,
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

// ghCommand runs a gh CLI command and returns the output.
func ghCommand(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// getConfigString extracts a string from a config map.
func getConfigString(config map[string]any, key string) string {
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
