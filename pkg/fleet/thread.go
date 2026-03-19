package fleet

import (
	"context"
	"fmt"
	"strings"
)

const (
	// maxThreadChars is the overall character budget for the thread context.
	// ~12.5K tokens at ~4 chars/token. This leaves room for the system prompt
	// and the agent's own tool call context within the model's context window.
	maxThreadChars = 50000

	// summaryMaxChars is the max length for summarized older messages.
	summaryMaxChars = 300

	// recentMsgMaxChars is the per-message truncation limit for recent messages
	// (excluding the last message, which is never truncated).
	recentMsgMaxChars = 3000

	// recoverySummaryMarker is text that identifies daemon restart recovery
	// summaries. These are deduplicated so only the most recent is kept.
	recoverySummaryMarker = "Fleet session resumed after daemon restart"
)

// BuildThreadContext builds the conversation context for an agent activation.
// It includes the full recent thread and summarized older messages.
//
// The agent sees all messages (from all participants), tagged with sender identity,
// so it has full awareness of the conversation state.
//
// Key guarantees:
//   - The last message is ALWAYS included in full (never truncated).
//   - Duplicate recovery summaries are deduplicated (only the most recent kept).
//   - A total character budget (maxThreadChars) prevents the context from
//     exceeding the model's context window.
//   - When over budget, older messages are summarized and recent messages are
//     progressively truncated — but never the last message.
func BuildThreadContext(ctx context.Context, channel Channel, agentKey string) (string, error) {
	thread, err := channel.GetThread(ctx)
	if err != nil {
		return "", fmt.Errorf("getting thread: %w", err)
	}

	if len(thread) == 0 {
		return "", nil
	}

	// Deduplicate recovery summaries — keep only the most recent one.
	thread = deduplicateRecoverySummaries(thread)

	return buildThreadWithBudget(thread), nil
}

// buildThreadWithBudget builds the thread context string with budget management.
//
// Algorithm:
//  1. Reserve the last message (always full, never truncated).
//  2. Fill backwards with recent messages (truncated to recentMsgMaxChars if needed).
//  3. Summarize remaining older messages.
//  4. If still over budget, reduce the number of recent messages.
func buildThreadWithBudget(thread []Message) string {
	if len(thread) == 0 {
		return ""
	}

	// The last message is always included in full.
	lastMsg := thread[len(thread)-1]
	lastMsgText := formatMessage(lastMsg, 0)
	remaining := thread[:len(thread)-1]

	// Budget for everything except the last message.
	otherBudget := maxThreadChars - len(lastMsgText)
	if otherBudget < 0 {
		otherBudget = 0
	}

	// If there are no other messages, just return the last one.
	if len(remaining) == 0 {
		var sb strings.Builder
		sb.WriteString("## Conversation Thread\n\n")
		writeMessage(&sb, lastMsg, 0)
		return sb.String()
	}

	// Try with progressively fewer recent messages until we fit the budget.
	// The "recent" messages get recentMsgMaxChars per message; older ones
	// are summarized.
	for _, recentCount := range []int{30, 20, 15, 10, 5, 3} {
		if recentCount > len(remaining) {
			recentCount = len(remaining)
		}

		result := buildSections(remaining, recentCount, lastMsg, otherBudget)
		if result != "" {
			return result
		}
	}

	// Last resort: only the last message + summarized history.
	return buildSections(remaining, 0, lastMsg, otherBudget)
}

// buildSections tries to build thread context with the given recent count.
// Returns "" if the result exceeds otherBudget (signal to try fewer recent messages).
func buildSections(remaining []Message, recentCount int, lastMsg Message, otherBudget int) string {
	var sb strings.Builder
	sb.WriteString("## Conversation Thread\n\n")

	olderCount := len(remaining) - recentCount
	if olderCount > 0 {
		sb.WriteString("### Earlier in the conversation (summary)\n\n")
		for _, msg := range remaining[:olderCount] {
			writeSummarizedMessage(&sb, msg)
		}
		sb.WriteString("\n")
	}

	if recentCount > 0 {
		sb.WriteString("### Recent messages\n\n")
		for _, msg := range remaining[olderCount:] {
			writeMessage(&sb, msg, recentMsgMaxChars)
		}
	}

	// Check if the non-last-message portion fits the budget.
	if sb.Len() > otherBudget {
		return ""
	}

	// Append the last message (always in full).
	sb.WriteString("### Message you must respond to\n\n")
	writeMessage(&sb, lastMsg, 0)

	return sb.String()
}

// deduplicateRecoverySummaries removes duplicate daemon restart recovery
// summaries, keeping only the most recent one. These summaries are generated
// by RecoverFleetSession and accumulate when a session is recovered multiple
// times. They bloat the thread context with redundant information.
func deduplicateRecoverySummaries(thread []Message) []Message {
	// Find the index of the last recovery summary.
	lastRecoveryIdx := -1
	for i := len(thread) - 1; i >= 0; i-- {
		if isRecoverySummary(thread[i]) {
			lastRecoveryIdx = i
			break
		}
	}

	// No recovery summaries, or only one — nothing to deduplicate.
	if lastRecoveryIdx < 0 {
		return thread
	}

	// Filter: keep everything that isn't a recovery summary, plus the last one.
	result := make([]Message, 0, len(thread))
	for i, msg := range thread {
		if isRecoverySummary(msg) && i != lastRecoveryIdx {
			continue // skip older recovery summaries
		}
		result = append(result, msg)
	}
	return result
}

// isRecoverySummary returns true if the message is a daemon restart recovery summary.
func isRecoverySummary(msg Message) bool {
	return msg.Sender == "system" && strings.Contains(msg.Text, recoverySummaryMarker)
}

// formatMessage formats a message as a string (for budget calculation).
func formatMessage(msg Message, maxChars int) string {
	var sb strings.Builder
	writeMessage(&sb, msg, maxChars)
	return sb.String()
}

// writeMessage writes a message to the string builder, optionally truncating
// the text to maxChars. If maxChars is 0, the full text is written.
func writeMessage(sb *strings.Builder, msg Message, maxChars int) {
	sender := formatSender(msg.Sender)
	text := msg.Text
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars] + "\n\n[... message truncated ...]"
	}
	sb.WriteString(fmt.Sprintf("**%s:**\n", sender))
	sb.WriteString(text)
	sb.WriteString("\n\n")

	if len(msg.Artifacts) > 0 {
		sb.WriteString("_Artifacts:_ ")
		parts := make([]string, 0, len(msg.Artifacts))
		for name, path := range msg.Artifacts {
			parts = append(parts, fmt.Sprintf("%s → `%s`", name, path))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("\n\n")
	}
}

// writeSummarizedMessage writes a condensed single-line summary of a message.
// Preserves the sender, first meaningful content, and any @mentions.
func writeSummarizedMessage(sb *strings.Builder, msg Message) {
	sender := formatSender(msg.Sender)
	text := msg.Text

	// Extract @mentions before truncating.
	mentions := ParseMentions(text)
	mentionSuffix := ""
	if len(mentions) > 0 {
		mentionSuffix = " [mentions: " + strings.Join(mentions, ", ") + "]"
	}

	// Truncate to summaryMaxChars, trying to break at sentence boundary.
	if len(text) > summaryMaxChars {
		text = truncateToSentence(text, summaryMaxChars)
	}

	// Collapse to single line.
	text = strings.ReplaceAll(text, "\n", " ")
	// Collapse multiple spaces.
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	text = strings.TrimSpace(text)

	sb.WriteString(fmt.Sprintf("- **%s:** %s%s\n", sender, text, mentionSuffix))
}

// formatSender returns a display name for a message sender.
func formatSender(sender string) string {
	switch sender {
	case "customer":
		return "Customer"
	case "system":
		return "System"
	default:
		return fmt.Sprintf("@%s", sender)
	}
}
