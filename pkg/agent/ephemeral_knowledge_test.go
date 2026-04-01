package agent

import (
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestEphemeralKnowledgeCallback_NilWhenEmpty(t *testing.T) {
	// No knowledge → nil callback (not registered)
	cb := EphemeralKnowledgeCallback("", false)
	if cb != nil {
		t.Error("expected nil callback when knowledge is empty")
	}
}

func TestEphemeralKnowledgeCallback_InjectsKnowledge(t *testing.T) {
	knowledge := "**MEMORY.md** (63%)\nProxmox at 192.168.1.200"
	cb := EphemeralKnowledgeCallback(knowledge, false)
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

func TestEphemeralKnowledgeCallback_NoUserMessage(t *testing.T) {
	cb := EphemeralKnowledgeCallback("some knowledge", false)
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
	cb := EphemeralKnowledgeCallback("some knowledge", false)
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
	cb := EphemeralKnowledgeCallback("knowledge", false)
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
	text := buildKnowledgeInjectionText("Some knowledge")
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

func TestBuildKnowledgeInjectionText_Empty(t *testing.T) {
	text := buildKnowledgeInjectionText("")
	if text != "" {
		t.Errorf("expected empty string, got: %q", text)
	}
}
