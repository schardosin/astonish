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

func TestTryParseAppPreviewMessage_WithAppID(t *testing.T) {
	text := `[app_preview]{"code":"function App() { return <div>hi</div> }","title":"My App","version":1,"appId":"uuid-123"}`
	msg := tryParseAppPreviewMessage(text)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Type != "app_preview" {
		t.Errorf("expected type app_preview, got %q", msg.Type)
	}
	if msg.AppID != "uuid-123" {
		t.Errorf("expected appId uuid-123, got %q", msg.AppID)
	}
	if msg.AppVersion != 1 {
		t.Errorf("expected version 1, got %d", msg.AppVersion)
	}
}

func TestTryParseAppPreviewMessage_WithoutAppID(t *testing.T) {
	// Backward compatibility: old format without appId
	text := `[app_preview]{"code":"function Old() {}","title":"Old App","version":2}`
	msg := tryParseAppPreviewMessage(text)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.AppID != "" {
		t.Errorf("expected empty appId, got %q", msg.AppID)
	}
	if msg.AppVersion != 2 {
		t.Errorf("expected version 2, got %d", msg.AppVersion)
	}
}

func TestReconstructActiveApp(t *testing.T) {
	events := testEvents{
		textEvent("inv-1", "model", `[app_preview]{"code":"function V1() {}","title":"App","version":1,"appId":"uuid-abc"}`),
		textEvent("inv-2", "model", `[app_preview]{"code":"function V2() {}","title":"App","version":2,"appId":"uuid-abc"}`),
	}
	app := reconstructActiveApp(events)
	if app == nil {
		t.Fatal("expected non-nil active app")
	}
	if app.AppID != "uuid-abc" {
		t.Errorf("expected appId uuid-abc, got %q", app.AppID)
	}
	if app.Version != 2 {
		t.Errorf("expected version 2, got %d", app.Version)
	}
	if app.Code != "function V2() {}" {
		t.Errorf("expected V2 code, got %q", app.Code)
	}
	if len(app.Versions) != 1 {
		t.Fatalf("expected 1 version in history, got %d", len(app.Versions))
	}
	if app.Versions[0] != "function V1() {}" {
		t.Errorf("expected V1 in history, got %q", app.Versions[0])
	}
}

func TestReconstructActiveApp_NoAppPreviews(t *testing.T) {
	events := testEvents{
		textEvent("inv-1", "model", "Hello world"),
	}
	app := reconstructActiveApp(events)
	if app != nil {
		t.Errorf("expected nil, got %+v", app)
	}
}

func TestExtractAppFromSystemContext(t *testing.T) {
	tests := []struct {
		name       string
		ctx        string
		wantCode   string
		wantTitle  string
	}{
		{
			name:     "valid refinement context",
			ctx:      "## Active App Refinement\n\nSome text.\n\n### Current Source Code\n\n```jsx\nfunction WeatherApp() {\n  return <div>Hello</div>\n}\nexport default WeatherApp\n```\n",
			wantCode: "function WeatherApp() {\n  return <div>Hello</div>\n}\nexport default WeatherApp",
			wantTitle: "Weather App",
		},
		{
			name:     "no refinement marker",
			ctx:      "Some random system context without the marker",
			wantCode: "",
			wantTitle: "",
		},
		{
			name:     "refinement marker but no code block",
			ctx:      "## Active App Refinement\n\nNo code here.",
			wantCode: "",
			wantTitle: "",
		},
		{
			name:     "empty system context",
			ctx:      "",
			wantCode: "",
			wantTitle: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, title := extractAppFromSystemContext(tt.ctx)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}
