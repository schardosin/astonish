package agent

import (
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestEphemeralKnowledgeCallback_NilWhenEmpty(t *testing.T) {
	// No knowledge, no plan → nil callback (not registered)
	cb := EphemeralKnowledgeCallback("", "", false)
	if cb != nil {
		t.Error("expected nil callback when both plan and knowledge are empty")
	}
}

func TestEphemeralKnowledgeCallback_InjectsKnowledge(t *testing.T) {
	knowledge := "**MEMORY.md** (63%)\nProxmox at 192.168.1.200"
	cb := EphemeralKnowledgeCallback("", knowledge, false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "check proxmox"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Sure!"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "show containers"}}},
		},
	}

	resp, err := cb(nil, req)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if resp != nil {
		t.Fatal("callback should return nil response to proceed")
	}

	// The last user message should now have 2 parts: knowledge + original text
	lastUser := req.Contents[2]
	if len(lastUser.Parts) != 2 {
		t.Fatalf("expected 2 parts in last user content, got %d", len(lastUser.Parts))
	}

	// First part should be the knowledge injection
	if !strings.Contains(lastUser.Parts[0].Text, "[Knowledge For This Task]") {
		t.Errorf("expected knowledge header, got: %s", lastUser.Parts[0].Text[:80])
	}
	if !strings.Contains(lastUser.Parts[0].Text, "Proxmox at 192.168.1.200") {
		t.Error("expected knowledge content in first part")
	}

	// Second part should be the original user message (untouched)
	if lastUser.Parts[1].Text != "show containers" {
		t.Errorf("expected original user message, got: %s", lastUser.Parts[1].Text)
	}

	// Earlier messages should be untouched
	if len(req.Contents[0].Parts) != 1 {
		t.Error("earlier user message should not be modified")
	}
}

func TestEphemeralKnowledgeCallback_InjectsExecutionPlan(t *testing.T) {
	plan := "Step 1: SSH into proxmox\nStep 2: Run pct list"
	cb := EphemeralKnowledgeCallback(plan, "", false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "list containers"}}},
		},
	}

	_, err := cb(nil, req)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}

	lastUser := req.Contents[0]
	if len(lastUser.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(lastUser.Parts))
	}

	if !strings.Contains(lastUser.Parts[0].Text, "[Execution Plan]") {
		t.Errorf("expected execution plan header, got: %s", lastUser.Parts[0].Text[:50])
	}
	if !strings.Contains(lastUser.Parts[0].Text, "Step 1: SSH") {
		t.Error("expected plan steps in injection")
	}
}

func TestEphemeralKnowledgeCallback_PlanWithKnowledge(t *testing.T) {
	plan := "Step 1: Do something"
	knowledge := "Use --verbose flag"
	cb := EphemeralKnowledgeCallback(plan, knowledge, false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "go"}}},
		},
	}

	_, err := cb(nil, req)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}

	injected := req.Contents[0].Parts[0].Text
	if !strings.Contains(injected, "[Execution Plan]") {
		t.Error("expected plan header")
	}
	if !strings.Contains(injected, "### Knowledge From Previous Experience") {
		t.Error("expected knowledge sub-section within plan")
	}
	if !strings.Contains(injected, "Use --verbose flag") {
		t.Error("expected knowledge content")
	}
	if !strings.Contains(injected, "Step 1: Do something") {
		t.Error("expected plan steps")
	}
}

func TestEphemeralKnowledgeCallback_NoUserMessage(t *testing.T) {
	cb := EphemeralKnowledgeCallback("", "some knowledge", false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	// Request with only model messages (edge case)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "model", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	resp, err := cb(nil, req)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}

	// Model message should be untouched
	if len(req.Contents[0].Parts) != 1 {
		t.Error("model message should not be modified")
	}
}

func TestEphemeralKnowledgeCallback_EmptyContents(t *testing.T) {
	cb := EphemeralKnowledgeCallback("plan", "", false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	req := &model.LLMRequest{Contents: nil}
	resp, err := cb(nil, req)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestEphemeralKnowledgeCallback_NilRequest(t *testing.T) {
	cb := EphemeralKnowledgeCallback("plan", "knowledge", false)
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}

	resp, err := cb(nil, nil)
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestBuildKnowledgeInjectionText_KnowledgeOnly(t *testing.T) {
	text := buildKnowledgeInjectionText("", "Some knowledge")
	if !strings.HasPrefix(text, "[Knowledge For This Task]") {
		t.Errorf("expected knowledge header prefix, got: %s", text[:40])
	}
	if !strings.Contains(text, "CRITICAL") {
		t.Error("expected CRITICAL preamble")
	}
	if !strings.Contains(text, "Some knowledge") {
		t.Error("expected knowledge content")
	}
}

func TestBuildKnowledgeInjectionText_PlanOnly(t *testing.T) {
	text := buildKnowledgeInjectionText("Step 1: Do it", "")
	if !strings.HasPrefix(text, "[Execution Plan]") {
		t.Errorf("expected plan header prefix, got: %s", text[:30])
	}
	if !strings.Contains(text, "Step 1: Do it") {
		t.Error("expected plan content")
	}
	if strings.Contains(text, "Knowledge") {
		t.Error("should not contain knowledge section when knowledge is empty")
	}
}

func TestBuildKnowledgeInjectionText_BothEmpty(t *testing.T) {
	text := buildKnowledgeInjectionText("", "")
	if text != "" {
		t.Errorf("expected empty string, got: %q", text)
	}
}
