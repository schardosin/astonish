package session

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func makeContent(role, text string) *genai.Content {
	return genai.NewContentFromText(text, genai.Role(role))
}

func makeFuncCallContent(role, funcName string, args map[string]any) *genai.Content {
	return &genai.Content{
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				Name: funcName,
				Args: args,
			},
		}},
		Role: role,
	}
}

func TestEstimateTokens_TextOnly(t *testing.T) {
	// 400 chars / 4 = ~100 tokens
	text := strings.Repeat("a", 400)
	contents := []*genai.Content{makeContent("user", text)}
	tokens := EstimateTokens(contents)
	if tokens != 100 {
		t.Errorf("EstimateTokens() = %d, want 100", tokens)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	tokens := EstimateTokens(nil)
	if tokens != 0 {
		t.Errorf("EstimateTokens(nil) = %d, want 0", tokens)
	}

	tokens = EstimateTokens([]*genai.Content{})
	if tokens != 0 {
		t.Errorf("EstimateTokens([]) = %d, want 0", tokens)
	}
}

func TestEstimateTokens_WithFunctionCall(t *testing.T) {
	contents := []*genai.Content{
		makeContent("user", "hello"),
		makeFuncCallContent("model", "shell_command", map[string]any{
			"command": "ls -la",
		}),
	}
	tokens := EstimateTokens(contents)
	// "hello" = 1 token + func call overhead + args
	if tokens < 5 {
		t.Errorf("EstimateTokens() = %d, want >= 5", tokens)
	}
}

func TestEstimateTokens_NilContent(t *testing.T) {
	contents := []*genai.Content{nil, makeContent("user", "test"), nil}
	tokens := EstimateTokens(contents)
	if tokens != 1 { // "test" = 4 chars / 4 = 1
		t.Errorf("EstimateTokens() = %d, want 1", tokens)
	}
}

func TestNewCompactor(t *testing.T) {
	c := NewCompactor(200000)
	if c.ContextWindow != 200000 {
		t.Errorf("ContextWindow = %d, want 200000", c.ContextWindow)
	}
	if c.Threshold != 0.8 {
		t.Errorf("Threshold = %f, want 0.8", c.Threshold)
	}
	if c.PreserveRecent != 4 {
		t.Errorf("PreserveRecent = %d, want 4", c.PreserveRecent)
	}
}

func TestShouldCompact_UnderThreshold(t *testing.T) {
	c := NewCompactor(100000) // 100K tokens
	// Small conversation — way under threshold
	contents := []*genai.Content{
		makeContent("user", "Hello"),
		makeContent("model", "Hi there"),
	}
	if c.ShouldCompact(contents) {
		t.Error("ShouldCompact() = true for small conversation")
	}
}

func TestShouldCompact_OverThreshold(t *testing.T) {
	c := NewCompactor(100) // tiny window: 100 tokens
	c.Threshold = 0.5      // compact at 50%
	// Each message ~100 chars = ~25 tokens, 4 messages = ~100 tokens > 50
	contents := []*genai.Content{
		makeContent("user", strings.Repeat("x", 100)),
		makeContent("model", strings.Repeat("y", 100)),
		makeContent("user", strings.Repeat("z", 100)),
		makeContent("model", strings.Repeat("w", 100)),
	}
	if !c.ShouldCompact(contents) {
		t.Error("ShouldCompact() = false for large conversation in small window")
	}
}

func TestShouldCompact_ZeroWindow(t *testing.T) {
	c := NewCompactor(0) // disabled
	contents := []*genai.Content{
		makeContent("user", strings.Repeat("x", 100000)),
	}
	if c.ShouldCompact(contents) {
		t.Error("ShouldCompact() = true with zero context window")
	}
}

func TestCompactContents_FewMessages(t *testing.T) {
	c := NewCompactor(100)
	c.PreserveRecent = 4

	// Only 3 messages — nothing to compact (less than PreserveRecent)
	contents := []*genai.Content{
		makeContent("user", "a"),
		makeContent("model", "b"),
		makeContent("user", "c"),
	}

	result, err := c.CompactContents(context.Background(), contents)
	if err != nil {
		t.Fatalf("CompactContents() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("len(result) = %d, want 3 (unchanged)", len(result))
	}
}

func TestCompactContents_WithTruncation(t *testing.T) {
	c := NewCompactor(100)
	c.PreserveRecent = 2
	// No LLM — will use truncation fallback

	contents := []*genai.Content{
		makeContent("user", "First question about setup"),
		makeContent("model", "Here's how to set things up"),
		makeContent("user", "Now let's talk about deployment"),
		makeContent("model", "Deployment steps are..."),
		makeContent("user", "What about monitoring?"),
		makeContent("model", "For monitoring, use..."),
	}

	result, err := c.CompactContents(context.Background(), contents)
	if err != nil {
		t.Fatalf("CompactContents() error = %v", err)
	}

	// Should be: 1 summary + 2 recent = 3
	if len(result) != 3 {
		t.Errorf("len(result) = %d, want 3", len(result))
	}

	// First message should be the summary
	if result[0].Role != "user" {
		t.Errorf("summary role = %q, want %q", result[0].Role, "user")
	}
	summaryText := result[0].Parts[0].Text
	if !strings.Contains(summaryText, "Context Summary") {
		t.Errorf("summary should contain 'Context Summary', got %q", summaryText[:min(80, len(summaryText))])
	}
}

func TestCompactContents_WithLLM(t *testing.T) {
	c := NewCompactor(100)
	c.PreserveRecent = 2
	c.LLM = func(ctx context.Context, prompt string) (string, error) {
		return "Summary: The user asked about Go programming and the model provided examples.", nil
	}

	contents := []*genai.Content{
		makeContent("user", "What is Go?"),
		makeContent("model", "Go is a programming language..."),
		makeContent("user", "Show me an example"),
		makeContent("model", "Here's an example: func main() {}"),
		makeContent("user", "Thanks, now let's do testing"),
		makeContent("model", "For testing in Go, use the testing package"),
	}

	result, err := c.CompactContents(context.Background(), contents)
	if err != nil {
		t.Fatalf("CompactContents() error = %v", err)
	}

	if len(result) != 3 {
		t.Errorf("len(result) = %d, want 3", len(result))
	}

	summaryText := result[0].Parts[0].Text
	if !strings.Contains(summaryText, "Go programming") {
		t.Errorf("summary should contain LLM output, got %q", summaryText[:min(80, len(summaryText))])
	}
}

func TestCompactContents_IncrementsCount(t *testing.T) {
	c := NewCompactor(100)
	c.PreserveRecent = 1

	contents := []*genai.Content{
		makeContent("user", "old1"),
		makeContent("model", "old2"),
		makeContent("user", "recent"),
	}

	if c.CompactionCount() != 0 {
		t.Errorf("initial CompactionCount() = %d, want 0", c.CompactionCount())
	}

	_, err := c.CompactContents(context.Background(), contents)
	if err != nil {
		t.Fatalf("CompactContents() error = %v", err)
	}

	if c.CompactionCount() != 1 {
		t.Errorf("CompactionCount() = %d, want 1", c.CompactionCount())
	}
}

func TestTokenUsage(t *testing.T) {
	c := NewCompactor(200000)
	contents := []*genai.Content{
		makeContent("user", strings.Repeat("x", 4000)), // ~1000 tokens
	}

	// Before ShouldCompact, usage is 0
	est, win := c.TokenUsage()
	if est != 0 {
		t.Errorf("initial estimated = %d, want 0", est)
	}
	if win != 200000 {
		t.Errorf("window = %d, want 200000", win)
	}

	// After ShouldCompact, usage is updated
	c.ShouldCompact(contents)
	est, win = c.TokenUsage()
	if est != 1000 {
		t.Errorf("estimated = %d, want 1000", est)
	}
	if win != 200000 {
		t.Errorf("window = %d, want 200000", win)
	}
}

func TestCompactContents_LLMFailureFallback(t *testing.T) {
	c := NewCompactor(100)
	c.PreserveRecent = 1
	c.LLM = func(ctx context.Context, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	}

	contents := []*genai.Content{
		makeContent("user", "old message 1"),
		makeContent("model", "old response 1"),
		makeContent("user", "recent message"),
	}

	result, err := c.CompactContents(context.Background(), contents)
	if err != nil {
		t.Fatalf("CompactContents() error = %v (should fallback, not fail)", err)
	}

	// Should still compact using truncation fallback
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	summaryText := result[0].Parts[0].Text
	if !strings.Contains(summaryText, "Context Summary") {
		t.Errorf("fallback summary should contain 'Context Summary'")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
