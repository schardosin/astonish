package fleet

import (
	"context"
	"fmt"
	"strings"
)

const (
	// maxRecentMessages is the default number of recent messages shown in full.
	maxRecentMessages = 20
	// summaryMaxChars is the max length for summarized older messages.
	summaryMaxChars = 200
	// maxThreadChars is the overall character budget for the thread context.
	// ~10K tokens at ~4 chars/token. This leaves room for the system prompt
	// and the agent's own tool call context within the model's context window.
	maxThreadChars = 40000
	// recentMsgMaxChars is the per-message character limit applied to recent
	// messages when the thread exceeds the budget.
	recentMsgMaxChars = 2000
)

// BuildThreadContext builds the conversation context for an agent activation.
// It includes the full recent thread and summarized older messages.
//
// The agent sees all messages (from all participants), tagged with sender identity,
// so it has full awareness of the conversation state.
//
// A total character budget (maxThreadChars) prevents the context from exceeding
// the model's context window. When over budget, recent messages are truncated
// and the recent window is reduced.
func BuildThreadContext(ctx context.Context, channel Channel, agentKey string) (string, error) {
	thread, err := channel.GetThread(ctx)
	if err != nil {
		return "", fmt.Errorf("getting thread: %w", err)
	}

	if len(thread) == 0 {
		return "", nil
	}

	// First pass: build with full recent messages
	result := buildThread(thread, maxRecentMessages, 0)

	// If within budget, return as-is
	if len(result) <= maxThreadChars {
		return result, nil
	}

	// Over budget: truncate individual recent messages
	result = buildThread(thread, maxRecentMessages, recentMsgMaxChars)
	if len(result) <= maxThreadChars {
		return result, nil
	}

	// Still over budget: reduce the recent window progressively
	for _, recentCount := range []int{10, 5, 3} {
		result = buildThread(thread, recentCount, recentMsgMaxChars)
		if len(result) <= maxThreadChars {
			return result, nil
		}
	}

	// Last resort: minimal context with aggressive truncation
	result = buildThread(thread, 3, 1000)
	if len(result) > maxThreadChars {
		result = result[:maxThreadChars] + "\n\n[... thread truncated due to length ...]"
	}

	return result, nil
}

// buildThread constructs the thread context string with the given parameters.
// recentCount is how many recent messages to show in full.
// msgMaxChars is the per-message truncation limit for recent messages (0 = no limit).
func buildThread(thread []Message, recentCount, msgMaxChars int) string {
	var sb strings.Builder
	sb.WriteString("## Conversation Thread\n\n")

	if len(thread) <= recentCount {
		for _, msg := range thread {
			writeMessage(&sb, msg, msgMaxChars)
		}
		return sb.String()
	}

	// Split into older (summarized) and recent (full/truncated)
	olderMessages := thread[:len(thread)-recentCount]
	recentMessages := thread[len(thread)-recentCount:]

	sb.WriteString("### Earlier in the conversation (summary)\n\n")
	for _, msg := range olderMessages {
		writeSummarizedMessage(&sb, msg)
	}
	sb.WriteString("\n### Recent messages\n\n")
	for _, msg := range recentMessages {
		writeMessage(&sb, msg, msgMaxChars)
	}

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

// writeSummarizedMessage writes a truncated message for older context.
func writeSummarizedMessage(sb *strings.Builder, msg Message) {
	sender := formatSender(msg.Sender)
	text := msg.Text
	if len(text) > summaryMaxChars {
		text = text[:summaryMaxChars] + "..."
	}
	// Collapse to single line
	text = strings.ReplaceAll(text, "\n", " ")
	sb.WriteString(fmt.Sprintf("- **%s:** %s\n", sender, text))
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
