package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// LLMFunc is a function that calls an LLM with a prompt and returns text.
type LLMFunc func(ctx context.Context, prompt string) (string, error)

// Compactor handles context window management by estimating token usage
// and compacting old messages when the context gets too full.
type Compactor struct {
	mu sync.Mutex

	// ContextWindow is the total token budget for the model.
	ContextWindow int
	// Threshold is the fraction (0-1) at which compaction triggers.
	// Default 0.8 (compact when 80% full).
	Threshold float64
	// PreserveRecent is how many recent messages to keep uncompacted.
	// Default 4.
	PreserveRecent int
	// LLM is the summarization function. If nil, compaction uses truncation.
	LLM LLMFunc
	// DebugMode enables verbose logging.
	DebugMode bool

	// Stats tracking
	lastEstimatedTokens int
	compactionCount     int
}

// NewCompactor creates a Compactor with the given context window size.
func NewCompactor(contextWindow int) *Compactor {
	return &Compactor{
		ContextWindow:  contextWindow,
		Threshold:      0.8,
		PreserveRecent: 4,
	}
}

// EstimateTokens estimates the token count for a slice of Contents.
// Uses a simple heuristic: ~4 characters per token for English text,
// adjusted upward for function calls/responses which have JSON overhead.
func EstimateTokens(contents []*genai.Content) int {
	total := 0
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			if p.Text != "" {
				// ~4 chars per token for natural language
				total += len(p.Text) / 4
			}
			if p.FunctionCall != nil {
				// Function call: name + JSON args, estimate generously
				total += 20 // name + overhead
				for k, v := range p.FunctionCall.Args {
					total += len(k)/4 + estimateValueTokens(v)
				}
			}
			if p.FunctionResponse != nil {
				// Function response: name + JSON response
				total += 20 // name + overhead
				for k, v := range p.FunctionResponse.Response {
					total += len(k)/4 + estimateValueTokens(v)
				}
			}
		}
	}
	return total
}

// estimateValueTokens estimates token count for a generic JSON value.
func estimateValueTokens(v any) int {
	switch val := v.(type) {
	case string:
		return len(val) / 4
	case map[string]any:
		total := 0
		for k, inner := range val {
			total += len(k)/4 + estimateValueTokens(inner)
		}
		return total
	case []any:
		total := 0
		for _, inner := range val {
			total += estimateValueTokens(inner)
		}
		return total
	default:
		return 2 // numbers, bools, null
	}
}

// ShouldCompact returns true if the given contents exceed the compaction threshold.
func (c *Compactor) ShouldCompact(contents []*genai.Content) bool {
	if c.ContextWindow <= 0 {
		return false
	}
	estimated := EstimateTokens(contents)
	c.mu.Lock()
	c.lastEstimatedTokens = estimated
	c.mu.Unlock()
	threshold := int(float64(c.ContextWindow) * c.Threshold)
	return estimated > threshold
}

// TokenUsage returns the last estimated token count and the context window size.
func (c *Compactor) TokenUsage() (estimated, window int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastEstimatedTokens, c.ContextWindow
}

// CompactionCount returns how many times compaction has been performed.
func (c *Compactor) CompactionCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compactionCount
}

// CompactContents compacts the contents by summarizing older messages and
// keeping the most recent ones intact. Returns the new contents slice.
//
// Strategy:
//  1. Split contents into [old | recent] at the PreserveRecent boundary
//  2. Summarize the old portion into a single system message
//  3. Prepend the summary to the recent messages
func (c *Compactor) CompactContents(ctx context.Context, contents []*genai.Content) ([]*genai.Content, error) {
	if len(contents) <= c.PreserveRecent {
		return contents, nil // nothing to compact
	}

	preserve := c.PreserveRecent
	if preserve > len(contents) {
		preserve = len(contents)
	}

	splitIdx := len(contents) - preserve
	oldContents := contents[:splitIdx]
	recentContents := contents[splitIdx:]

	// Build summary
	summary, err := c.summarize(ctx, oldContents)
	if err != nil {
		if c.DebugMode {
			fmt.Printf("[Compactor] Summarization failed, falling back to truncation: %v\n", err)
		}
		// Fallback: just keep recent messages with a note
		summary = c.truncationSummary(oldContents)
	}

	// Create the summary message as a "user" message that the model will see
	summaryContent := &genai.Content{
		Parts: []*genai.Part{{
			Text: fmt.Sprintf("[Context Summary — %d earlier messages compacted]\n\n%s",
				len(oldContents), summary),
		}},
		Role: "user",
	}

	// Build compacted result: summary + recent
	result := make([]*genai.Content, 0, 1+len(recentContents))
	result = append(result, summaryContent)
	result = append(result, recentContents...)

	c.mu.Lock()
	c.compactionCount++
	c.lastEstimatedTokens = EstimateTokens(result)
	c.mu.Unlock()

	if c.DebugMode {
		fmt.Printf("[Compactor] Compacted %d → %d messages (estimated %d tokens)\n",
			len(contents), len(result), EstimateTokens(result))
	}

	return result, nil
}

// summarize uses the LLM to create a concise summary of old messages.
func (c *Compactor) summarize(ctx context.Context, contents []*genai.Content) (string, error) {
	if c.LLM == nil {
		return c.truncationSummary(contents), nil
	}

	// Build a text representation of the old conversation
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation history concisely. ")
	sb.WriteString("Preserve key facts, decisions, file paths, variable names, and outcomes. ")
	sb.WriteString("Omit redundant tool call details but keep what tools accomplished.\n\n")

	for _, content := range contents {
		if content == nil {
			continue
		}
		role := content.Role
		if role == "" {
			role = "system"
		}
		for _, p := range content.Parts {
			if p == nil {
				continue
			}
			if p.Text != "" {
				sb.WriteString(fmt.Sprintf("[%s]: %s\n", role, truncateText(p.Text, 500)))
			}
			if p.FunctionCall != nil {
				sb.WriteString(fmt.Sprintf("[%s] Called tool: %s\n", role, p.FunctionCall.Name))
			}
			if p.FunctionResponse != nil {
				sb.WriteString(fmt.Sprintf("[tool] %s responded\n", p.FunctionResponse.Name))
			}
		}
	}

	prompt := sb.String()

	// Cap the prompt to avoid sending a huge summarization request
	if len(prompt) > 30000 {
		prompt = prompt[:30000] + "\n\n[... truncated for summarization ...]"
	}

	return c.LLM(ctx, prompt)
}

// truncationSummary creates a basic summary without LLM, extracting key info.
func (c *Compactor) truncationSummary(contents []*genai.Content) string {
	var sb strings.Builder
	sb.WriteString("Previous conversation context:\n")

	messageCount := 0
	toolCallCount := 0

	for _, content := range contents {
		if content == nil {
			continue
		}
		for _, p := range content.Parts {
			if p == nil {
				continue
			}
			if p.Text != "" {
				messageCount++
				// Keep first and last text snippets
				if messageCount <= 2 || messageCount == len(contents) {
					text := truncateText(p.Text, 200)
					sb.WriteString(fmt.Sprintf("- [%s]: %s\n", content.Role, text))
				}
			}
			if p.FunctionCall != nil {
				toolCallCount++
			}
		}
	}

	if toolCallCount > 0 {
		sb.WriteString(fmt.Sprintf("\n(%d messages, %d tool calls compacted)\n", messageCount, toolCallCount))
	}

	return sb.String()
}

// truncateText shortens text to maxLen characters, appending "..." if truncated.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// BeforeModelCallback returns a callback suitable for llmagent.Config.BeforeModelCallbacks.
// It checks token usage and compacts the request contents if needed.
func (c *Compactor) BeforeModelCallback() llmagent.BeforeModelCallback {
	return func(ctx adkagent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if req == nil || len(req.Contents) == 0 {
			return nil, nil // proceed normally
		}

		if !c.ShouldCompact(req.Contents) {
			return nil, nil // under threshold, proceed normally
		}

		if c.DebugMode {
			est := EstimateTokens(req.Contents)
			fmt.Printf("[Compactor] Threshold exceeded (%d/%d tokens, %.0f%%). Compacting...\n",
				est, c.ContextWindow, float64(est)/float64(c.ContextWindow)*100)
		}

		compacted, err := c.CompactContents(ctx, req.Contents)
		if err != nil {
			if c.DebugMode {
				fmt.Printf("[Compactor] Compaction failed: %v\n", err)
			}
			return nil, nil // proceed with original contents
		}

		// Replace the request contents in place
		req.Contents = compacted
		return nil, nil // proceed with compacted contents
	}
}
