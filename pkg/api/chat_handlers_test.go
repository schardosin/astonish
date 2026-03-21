package api

import (
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// testEvents implements session.Events for testing.
type testEvents []*session.Event

func (e testEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}
func (e testEvents) Len() int                { return len(e) }
func (e testEvents) At(i int) *session.Event { return e[i] }

// helper to build a text event.
func textEvent(invocationID, role, text string) *session.Event {
	return &session.Event{
		InvocationID: invocationID,
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  role,
				Parts: []*genai.Part{{Text: text}},
			},
		},
	}
}

func TestEventsToMessages_CoalescesSameInvocation(t *testing.T) {
	// Two model text parts in the same invocation should be coalesced.
	events := testEvents{
		textEvent("inv-1", "model", "Hello "),
		textEvent("inv-1", "model", "world"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Hello world" {
		t.Errorf("expected coalesced content 'Hello world', got %q", msgs[0].Content)
	}
	if msgs[0].Type != "agent" {
		t.Errorf("expected type 'agent', got %q", msgs[0].Type)
	}
}

func TestEventsToMessages_NoCoalesceAcrossInvocations(t *testing.T) {
	// Two user messages in different invocations must NOT be coalesced.
	// This is the bug fix for session d2255947 where consecutive user
	// messages (with no model response between them) were merged on reload.
	events := testEvents{
		textEvent("inv-1", "user", "first message"),
		textEvent("inv-2", "user", "second message"),
		textEvent("inv-3", "user", "third message"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 separate messages, got %d: %+v", len(msgs), msgs)
	}
	for i, want := range []string{"first message", "second message", "third message"} {
		if msgs[i].Content != want {
			t.Errorf("message[%d]: expected %q, got %q", i, want, msgs[i].Content)
		}
		if msgs[i].Type != "user" {
			t.Errorf("message[%d]: expected type 'user', got %q", i, msgs[i].Type)
		}
	}
}

func TestEventsToMessages_MixedInvocations(t *testing.T) {
	// Normal conversation flow: user → model (same inv), user → model (new inv).
	events := testEvents{
		textEvent("inv-1", "user", "question 1"),
		textEvent("inv-1", "model", "answer 1"),
		textEvent("inv-2", "user", "question 2"),
		textEvent("inv-2", "model", "answer 2"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	expected := []struct {
		typ     string
		content string
	}{
		{"user", "question 1"},
		{"agent", "answer 1"},
		{"user", "question 2"},
		{"agent", "answer 2"},
	}
	for i, want := range expected {
		if msgs[i].Type != want.typ {
			t.Errorf("message[%d]: expected type %q, got %q", i, want.typ, msgs[i].Type)
		}
		if msgs[i].Content != want.content {
			t.Errorf("message[%d]: expected content %q, got %q", i, want.content, msgs[i].Content)
		}
	}
}

func TestEventsToMessages_StripsTimestamp(t *testing.T) {
	// User messages with timestamp prefix should have it stripped.
	events := testEvents{
		textEvent("inv-1", "user", "[2026-03-20 14:30:05 UTC]\nHello"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello" {
		t.Errorf("expected timestamp stripped, got %q", msgs[0].Content)
	}
}

func TestEventsToMessages_ErrorEventRendersAsAgent(t *testing.T) {
	// A persisted error event (model role with "[Error: ...]" text) should
	// render as an "agent" type message so it shows up in the chat UI.
	events := testEvents{
		textEvent("inv-1", "user", "do something"),
		textEvent("", "model", "[Error: unexpected end of JSON input]"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].Type != "agent" {
		t.Errorf("expected error message type 'agent', got %q", msgs[1].Type)
	}
	if msgs[1].Content != "[Error: unexpected end of JSON input]" {
		t.Errorf("expected error content preserved, got %q", msgs[1].Content)
	}
}

func TestEventsToMessages_NilContentSkipped(t *testing.T) {
	// Events with nil Content should be silently skipped.
	events := testEvents{
		{InvocationID: "inv-1", LLMResponse: model.LLMResponse{Content: nil}},
		textEvent("inv-1", "model", "hello"),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", msgs[0].Content)
	}
}

func TestEventsToMessages_ToolCallBreaksCoalescing(t *testing.T) {
	// A tool call between two model text events in the same invocation
	// should result in separate text messages (tool_call in between).
	events := testEvents{
		textEvent("inv-1", "model", "Let me check."),
		{
			InvocationID: "inv-1",
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{{
						FunctionCall: &genai.FunctionCall{
							Name: "shell_command",
							Args: map[string]any{"command": "ls"},
						},
					}},
				},
			},
		},
		textEvent("inv-1", "model", "Here are the files."),
	}

	msgs := eventsToMessages(events, nil)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Type != "agent" || msgs[0].Content != "Let me check." {
		t.Errorf("message[0]: unexpected %+v", msgs[0])
	}
	if msgs[1].Type != "tool_call" || msgs[1].ToolName != "shell_command" {
		t.Errorf("message[1]: unexpected %+v", msgs[1])
	}
	if msgs[2].Type != "agent" || msgs[2].Content != "Here are the files." {
		t.Errorf("message[2]: unexpected %+v", msgs[2])
	}
}
