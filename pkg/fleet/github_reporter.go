package fleet

import (
	"fmt"
	"strings"
)

// GitHubReporter posts fleet session results back to GitHub Issues as comments.
type GitHubReporter struct {
	Repo        string
	IssueNumber int
	GHToken     string // optional: injected as GH_TOKEN for gh CLI auth
}

// NewGitHubReporter creates a reporter for a specific issue.
func NewGitHubReporter(repo string, issueNumber int, ghToken string) *GitHubReporter {
	return &GitHubReporter{
		Repo:        repo,
		IssueNumber: issueNumber,
		GHToken:     ghToken,
	}
}

// PostComment posts a comment to the GitHub issue.
func (r *GitHubReporter) PostComment(body string) error {
	if r.Repo == "" || r.IssueNumber == 0 {
		return fmt.Errorf("reporter not configured: repo=%q issue=%d", r.Repo, r.IssueNumber)
	}

	_, err := ghCommand(r.GHToken, "issue", "comment",
		fmt.Sprintf("%d", r.IssueNumber),
		"--repo", r.Repo,
		"--body", body)
	if err != nil {
		return fmt.Errorf("posting comment to issue #%d: %w", r.IssueNumber, err)
	}
	return nil
}

// PostSessionSummary builds a summary from the fleet session's message thread
// and posts it as a comment on the GitHub issue.
func (r *GitHubReporter) PostSessionSummary(messages []Message, sessionID string) error {
	if len(messages) == 0 {
		return nil
	}

	body := buildSummaryComment(messages, sessionID)
	return r.PostComment(body)
}

// buildSummaryComment creates a formatted GitHub comment from fleet messages.
func buildSummaryComment(messages []Message, sessionID string) string {
	var sb strings.Builder

	sb.WriteString("## Astonish Fleet Summary\n\n")

	// Collect agent contributions (skip system and customer messages, skip intermediates)
	type contribution struct {
		agent    string
		messages []string
	}
	var contributions []contribution
	currentAgent := ""
	var currentMessages []string

	for _, msg := range messages {
		if msg.Sender == "customer" || msg.Sender == "system" {
			continue
		}

		// Skip intermediate progress messages
		if msg.Metadata != nil {
			if intermediate, ok := msg.Metadata["intermediate"]; ok {
				if b, ok := intermediate.(bool); ok && b {
					continue
				}
			}
		}

		if msg.Sender != currentAgent {
			if currentAgent != "" && len(currentMessages) > 0 {
				contributions = append(contributions, contribution{
					agent:    currentAgent,
					messages: currentMessages,
				})
			}
			currentAgent = msg.Sender
			currentMessages = nil
		}
		currentMessages = append(currentMessages, msg.Text)
	}
	if currentAgent != "" && len(currentMessages) > 0 {
		contributions = append(contributions, contribution{
			agent:    currentAgent,
			messages: currentMessages,
		})
	}

	if len(contributions) == 0 {
		sb.WriteString("No agent contributions recorded.\n")
		return sb.String()
	}

	for _, c := range contributions {
		sb.WriteString(fmt.Sprintf("### @%s\n\n", c.agent))
		// Use the last message from each agent turn as the summary
		// (earlier messages in the same turn are typically progress updates)
		lastMsg := c.messages[len(c.messages)-1]

		// Truncate very long messages
		if len(lastMsg) > 2000 {
			lastMsg = lastMsg[:2000] + "\n\n...(truncated)"
		}
		sb.WriteString(lastMsg)
		sb.WriteString("\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("*Processed by [Astonish Fleet](https://github.com/schardosin/astonish) (session: `%s`)*\n", truncateID(sessionID)))

	return sb.String()
}

// truncateID shortens a UUID for display.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
