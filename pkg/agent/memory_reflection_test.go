package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// --- Test helpers ---

// mockEvents implements session.Events for testing.
type mockEvents struct {
	events []*session.Event
}

func (m *mockEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, e := range m.events {
			if !yield(e) {
				return
			}
		}
	}
}

func (m *mockEvents) Len() int                { return len(m.events) }
func (m *mockEvents) At(i int) *session.Event { return m.events[i] }

func makeEvent(author string, parts ...*genai.Part) *session.Event {
	role := "model"
	if author == "user" {
		role = "user"
	}
	ev := &session.Event{
		Author: author,
	}
	ev.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Parts: parts,
			Role:  role,
		},
	}
	return ev
}

// testMemoryManager creates a real *memory.Manager using a temp directory.
func testMemoryManager(t *testing.T) *memory.Manager {
	t.Helper()
	dir := t.TempDir()
	memPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(memPath, []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mgr, err := memory.NewManager(memPath, false)
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

// --- countToolCallsRecursive tests ---

func TestCountToolCallsRecursive_Empty(t *testing.T) {
	trace := NewExecutionTrace("test")
	if got := countToolCallsRecursive(trace); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestCountToolCallsRecursive_FlatSteps(t *testing.T) {
	trace := NewExecutionTrace("test")
	trace.RecordStep("shell_command", nil, nil, nil)
	trace.RecordStep("read_file", nil, nil, nil)
	if got := countToolCallsRecursive(trace); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestCountToolCallsRecursive_WithSubAgents(t *testing.T) {
	child1 := NewExecutionTrace("child-1")
	child1.RecordStep("shell_command", nil, nil, nil)
	child1.RecordStep("read_file", nil, nil, nil)
	child1.RecordStep("write_file", nil, nil, nil)

	child2 := NewExecutionTrace("child-2")
	child2.RecordStep("http_request", nil, nil, nil)

	trace := NewExecutionTrace("test")
	trace.RecordStep("memory_search", nil, nil, nil)
	trace.RecordStep("delegate_tasks", nil, nil, nil)
	trace.AttachSubAgentTraces([]*ExecutionTrace{child1, child2})

	// 2 top-level + 3 child1 + 1 child2 = 6
	if got := countToolCallsRecursive(trace); got != 6 {
		t.Errorf("expected 6, got %d", got)
	}
}

func TestCountToolCallsRecursive_Nil(t *testing.T) {
	if got := countToolCallsRecursive(nil); got != 0 {
		t.Errorf("expected 0 for nil trace, got %d", got)
	}
}

// --- traceContainsMemorySave tests ---

func TestTraceContainsMemorySave_NotPresent(t *testing.T) {
	trace := NewExecutionTrace("test")
	trace.RecordStep("shell_command", nil, nil, nil)
	if traceContainsMemorySave(trace) {
		t.Error("expected false when memory_save not called")
	}
}

func TestTraceContainsMemorySave_TopLevel(t *testing.T) {
	trace := NewExecutionTrace("test")
	trace.RecordStep("shell_command", nil, nil, nil)
	trace.RecordStep("memory_save", nil, nil, nil)
	if !traceContainsMemorySave(trace) {
		t.Error("expected true when memory_save called at top level")
	}
}

func TestTraceContainsMemorySave_InSubAgent(t *testing.T) {
	child := NewExecutionTrace("child")
	child.RecordStep("memory_save", nil, nil, nil)

	trace := NewExecutionTrace("test")
	trace.RecordStep("delegate_tasks", nil, nil, nil)
	trace.AttachSubAgentTraces([]*ExecutionTrace{child})

	if !traceContainsMemorySave(trace) {
		t.Error("expected true when memory_save called in sub-agent")
	}
}

func TestTraceContainsMemorySave_Nil(t *testing.T) {
	if traceContainsMemorySave(nil) {
		t.Error("expected false for nil trace")
	}
}

// --- extractCurrentTurnConversation tests ---

func TestExtractCurrentTurnConversation_NilEvents(t *testing.T) {
	user, modelText := extractCurrentTurnConversation(nil)
	if user != "" || modelText != "" {
		t.Errorf("expected empty for nil events, got user=%q model=%q", user, modelText)
	}
}

func TestExtractCurrentTurnConversation_EmptyEvents(t *testing.T) {
	events := &mockEvents{}
	user, modelText := extractCurrentTurnConversation(events)
	if user != "" || modelText != "" {
		t.Errorf("expected empty for empty events, got user=%q model=%q", user, modelText)
	}
}

func TestExtractCurrentTurnConversation_SimpleExchange(t *testing.T) {
	events := &mockEvents{
		events: []*session.Event{
			makeEvent("user", &genai.Part{Text: "Connect to my OpenStack server"}),
			makeEvent("chat", &genai.Part{Text: "I'll help you connect. What's the auth URL?"}),
			makeEvent("user", &genai.Part{Text: "https://cloud.example.com:5000, domain Default, project admin"}),
			makeEvent("chat", &genai.Part{Text: "I've saved the connection details. Auth URL: https://cloud.example.com:5000, domain: Default, project: admin."}),
		},
	}

	user, modelText := extractCurrentTurnConversation(events)

	// Should extract the LAST user message
	if user != "https://cloud.example.com:5000, domain Default, project admin" {
		t.Errorf("unexpected user text: %q", user)
	}
	// Should extract model text after that
	if modelText != "I've saved the connection details. Auth URL: https://cloud.example.com:5000, domain: Default, project: admin." {
		t.Errorf("unexpected model text: %q", modelText)
	}
}

func TestExtractCurrentTurnConversation_SkipsThoughtsAndToolCalls(t *testing.T) {
	events := &mockEvents{
		events: []*session.Event{
			makeEvent("user", &genai.Part{Text: "Save my password"}),
			makeEvent("chat",
				&genai.Part{Text: "thinking about this", Thought: true},
				&genai.Part{FunctionCall: &genai.FunctionCall{Name: "save_credential", Args: map[string]any{"name": "test"}}},
			),
			makeEvent("chat",
				&genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "save_credential", Response: map[string]any{"status": "ok"}}},
			),
			makeEvent("chat", &genai.Part{Text: "Done! Your password has been saved."}),
		},
	}

	user, modelText := extractCurrentTurnConversation(events)

	if user != "Save my password" {
		t.Errorf("unexpected user text: %q", user)
	}
	// Should only contain the visible text, not thoughts or tool calls
	if modelText != "Done! Your password has been saved." {
		t.Errorf("unexpected model text: %q", modelText)
	}
}

func TestExtractCurrentTurnConversation_NoUserEvent(t *testing.T) {
	events := &mockEvents{
		events: []*session.Event{
			makeEvent("chat", &genai.Part{Text: "Hello"}),
		},
	}

	user, modelText := extractCurrentTurnConversation(events)
	if user != "" || modelText != "" {
		t.Errorf("expected empty when no user event, got user=%q model=%q", user, modelText)
	}
}

// --- buildReflectionContext tests ---

func TestBuildReflectionContext_EmptyTrace(t *testing.T) {
	trace := NewExecutionTrace("test request")
	ctx := buildReflectionContext(trace, nil)

	if !containsAll(ctx, "test request", "Total Tool Calls:** 0") {
		t.Errorf("unexpected context:\n%s", ctx)
	}
}

func TestBuildReflectionContext_WithSteps(t *testing.T) {
	trace := NewExecutionTrace("install deps")
	trace.RecordStep("shell_command", map[string]any{"command": "pip install requests"}, nil, nil)
	trace.RecordStep("shell_command", map[string]any{"command": "python -c 'import requests'"}, nil, nil)
	trace.FinalOutput = "Dependencies installed successfully."

	ctx := buildReflectionContext(trace, nil)

	if !containsAll(ctx, "install deps", "shell_command", "pip install requests", "Dependencies installed") {
		t.Errorf("missing expected content in context:\n%s", ctx)
	}
	if !containsAll(ctx, "Total Tool Calls:** 2") {
		t.Errorf("expected total tool calls 2 in context:\n%s", ctx)
	}
}

func TestBuildReflectionContext_WithSubAgentTraces(t *testing.T) {
	child := NewExecutionTrace("setup venv")
	child.RecordStep("shell_command", map[string]any{"command": "python3 -m venv .venv"}, nil, nil)
	child.RecordStep("shell_command", map[string]any{"command": ".venv/bin/pip install requests"}, nil, nil)
	child.FinalOutput = "venv created and requests installed"

	trace := NewExecutionTrace("install deps")
	trace.RecordStep("delegate_tasks", map[string]any{"tasks": "setup"}, nil, nil)
	trace.AttachSubAgentTraces([]*ExecutionTrace{child})

	ctx := buildReflectionContext(trace, nil)

	// Should include sub-agent tool calls in total count: 1 (delegate) + 2 (child) = 3
	if !containsAll(ctx, "Total Tool Calls:** 3") {
		t.Errorf("expected total tool calls 3:\n%s", ctx)
	}
	// Should include sub-agent steps
	if !containsAll(ctx, "Sub-agent:", "python3 -m venv", "pip install requests") {
		t.Errorf("missing sub-agent details:\n%s", ctx)
	}
}

func TestBuildReflectionContext_WithConversation(t *testing.T) {
	events := &mockEvents{
		events: []*session.Event{
			makeEvent("user", &genai.Part{Text: "The auth URL is https://cloud.example.com:5000, domain Default"}),
			makeEvent("chat", &genai.Part{Text: "I've saved the connection details for your OpenStack server."}),
		},
	}

	trace := NewExecutionTrace("setup openstack")
	trace.RecordStep("save_credential", map[string]any{"name": "openstack"}, nil, nil)

	ctx := buildReflectionContext(trace, events)

	if !containsAll(ctx, "Conversation Context", "https://cloud.example.com:5000", "domain Default") {
		t.Errorf("missing conversation context:\n%s", ctx)
	}
	if !containsAll(ctx, "I've saved the connection details") {
		t.Errorf("missing model response in context:\n%s", ctx)
	}
}

// --- Reflect gate tests ---

func TestReflect_SkipsTrivialTurn(t *testing.T) {
	// No tool calls, short output — should skip without calling LLM
	r := &MemoryReflector{
		LLM:           &panicLLM{t: t},
		MemoryManager: testMemoryManager(t),
	}
	trace := NewExecutionTrace("ok")
	trace.FinalOutput = "Done!"

	// panicLLM would fail the test if called — so this implicitly tests the skip.
	r.Reflect(context.Background(), trace, nil)
}

func TestReflect_SkipsWhenMemorySaveAlreadyCalled(t *testing.T) {
	r := &MemoryReflector{
		LLM:           &panicLLM{t: t},
		MemoryManager: testMemoryManager(t),
	}
	trace := NewExecutionTrace("test")
	trace.RecordStep("shell_command", nil, nil, nil)
	trace.RecordStep("memory_save", nil, nil, nil)
	trace.RecordStep("shell_command", nil, nil, nil)

	r.Reflect(context.Background(), trace, nil)
}

func TestReflect_RunsOnSingleToolCallTurn(t *testing.T) {
	// A single save_credential call should still trigger reflection
	// (the old minToolCallsForReflection=3 would have blocked this)
	called := false
	r := &MemoryReflector{
		LLM: &mockReflectionLLM{onCall: func() {
			called = true
		}},
		MemoryManager: testMemoryManager(t),
	}
	trace := NewExecutionTrace("save my openstack password")
	trace.RecordStep("save_credential", map[string]any{"name": "openstack"}, nil, nil)

	r.Reflect(context.Background(), trace, nil)

	if !called {
		t.Error("expected reflection LLM to be called for single-tool-call turn")
	}
}

func TestReflect_RunsOnMeaningfulOutputOnly(t *testing.T) {
	// No tool calls but meaningful output — should still run
	called := false
	r := &MemoryReflector{
		LLM: &mockReflectionLLM{onCall: func() {
			called = true
		}},
		MemoryManager: testMemoryManager(t),
	}
	trace := NewExecutionTrace("tell me about the server setup")
	trace.FinalOutput = "Your OpenStack server is at auth URL https://cloud.example.com:5000 with domain Default and project admin."

	r.Reflect(context.Background(), trace, nil)

	if !called {
		t.Error("expected reflection LLM to be called for turn with meaningful output")
	}
}

func TestReflect_SkipsWhenMemorySaveInSubAgent(t *testing.T) {
	child := NewExecutionTrace("child")
	child.RecordStep("memory_save", nil, nil, nil)

	trace := NewExecutionTrace("test")
	trace.RecordStep("delegate_tasks", nil, nil, nil)
	trace.AttachSubAgentTraces([]*ExecutionTrace{child})

	r := &MemoryReflector{
		LLM:           &panicLLM{t: t},
		MemoryManager: testMemoryManager(t),
	}
	r.Reflect(context.Background(), trace, nil)
}

// --- AttachSubAgentTraces test ---

func TestAttachSubAgentTraces(t *testing.T) {
	trace := NewExecutionTrace("test")
	trace.RecordStep("delegate_tasks", nil, nil, nil)

	child := NewExecutionTrace("child task")
	child.RecordStep("shell_command", nil, nil, nil)

	trace.AttachSubAgentTraces([]*ExecutionTrace{child})

	trace.mu.Lock()
	defer trace.mu.Unlock()

	if len(trace.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(trace.Steps))
	}
	if len(trace.Steps[0].SubAgentTraces) != 1 {
		t.Fatalf("expected 1 sub-agent trace, got %d", len(trace.Steps[0].SubAgentTraces))
	}
	if trace.Steps[0].SubAgentTraces[0].UserRequest != "child task" {
		t.Errorf("expected child trace user request 'child task', got %q", trace.Steps[0].SubAgentTraces[0].UserRequest)
	}
}

func TestAttachSubAgentTraces_NoSteps(t *testing.T) {
	// Should not panic when trace has no steps
	trace := NewExecutionTrace("test")
	child := NewExecutionTrace("child")
	trace.AttachSubAgentTraces([]*ExecutionTrace{child})
	// No assertion needed — just shouldn't panic
}

// --- Sub-agent manager trace stash tests ---

func TestSubAgentManager_StashAndPopTraces(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{
		MaxConcurrent: 1,
		TaskTimeout:   time.Minute,
	})

	traces := []*ExecutionTrace{
		NewExecutionTrace("task-1"),
		NewExecutionTrace("task-2"),
	}

	mgr.StashLastTraces(traces)

	popped := mgr.PopLastTraces()
	if len(popped) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(popped))
	}
	if popped[0].UserRequest != "task-1" {
		t.Errorf("expected 'task-1', got %q", popped[0].UserRequest)
	}

	// Second pop should return nil
	popped2 := mgr.PopLastTraces()
	if popped2 != nil {
		t.Errorf("expected nil on second pop, got %d traces", len(popped2))
	}
}

func TestSubAgentManager_StashOverwritesPrevious(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{
		MaxConcurrent: 1,
		TaskTimeout:   time.Minute,
	})

	mgr.StashLastTraces([]*ExecutionTrace{NewExecutionTrace("old")})
	mgr.StashLastTraces([]*ExecutionTrace{NewExecutionTrace("new")})

	popped := mgr.PopLastTraces()
	if len(popped) != 1 || popped[0].UserRequest != "new" {
		t.Errorf("expected stash to be overwritten with 'new', got %v", popped)
	}
}

// --- Test mock types ---

// panicLLM fails the test if GenerateContent is called.
type panicLLM struct {
	t *testing.T
}

func (p *panicLLM) Name() string { return "panic-llm" }
func (p *panicLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	p.t.Fatal("LLM should not have been called")
	return nil
}

// mockReflectionLLM tracks whether it was called and returns a "no save" response.
type mockReflectionLLM struct {
	onCall func()
}

func (m *mockReflectionLLM) Name() string { return "mock-reflection-llm" }
func (m *mockReflectionLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.onCall != nil {
			m.onCall()
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: "No durable knowledge to save."}},
				Role:  "model",
			},
		}, nil)
	}
}

// --- validateContent Thought-filtering tests ---

// responseLLM returns a fixed set of parts from GenerateContent.
type responseLLM struct {
	parts []*genai.Part
}

func (r *responseLLM) Name() string { return "response-llm" }
func (r *responseLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Parts: r.parts,
				Role:  "model",
			},
		}, nil)
	}
}

func TestValidateContent_SkipsThoughtParts(t *testing.T) {
	// Simulate DeepSeek returning reasoning_content (Thought) before actual content.
	reflector := &MemoryReflector{
		LLM: &responseLLM{
			parts: []*genai.Part{
				{Text: "Let me analyze each line carefully...\nLine 1 is new because...", Thought: true},
				{Text: "- New fact: server resolves to 192.168.1.134"},
			},
		},
	}

	result := reflector.validateContent(context.Background(), "Test Section",
		"- Existing fact", "- New fact: server resolves to 192.168.1.134")

	if result != "- New fact: server resolves to 192.168.1.134" {
		t.Errorf("validateContent should skip Thought part and return actual content, got %q", result)
	}
}

func TestValidateContent_ThoughtOnly_ReturnsEmpty(t *testing.T) {
	// When the model returns ONLY a Thought part (no actual content), we should
	// get empty string (meaning "all content was duplicate" per the NONE path).
	reflector := &MemoryReflector{
		LLM: &responseLLM{
			parts: []*genai.Part{
				{Text: "After analysis, everything is already present.", Thought: true},
			},
		},
	}

	result := reflector.validateContent(context.Background(), "Test Section",
		"- Existing fact", "- Same fact rephrased")

	if result != "" {
		t.Errorf("expected empty string when only Thought parts present, got %q", result)
	}
}

func TestValidateContent_NONE_Response(t *testing.T) {
	reflector := &MemoryReflector{
		LLM: &responseLLM{
			parts: []*genai.Part{{Text: "NONE"}},
		},
	}

	result := reflector.validateContent(context.Background(), "Test Section",
		"- Existing fact", "- Existing fact rephrased")

	if result != "" {
		t.Errorf("expected empty string for NONE response, got %q", result)
	}
}

// --- Reflection timeout tests ---

// hangingLLM blocks until the context is cancelled.
type hangingLLM struct{}

func (h *hangingLLM) Name() string { return "hanging-llm" }
func (h *hangingLLM) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

func TestReflect_RespectsTimeout(t *testing.T) {
	reflector := &MemoryReflector{
		LLM:           &hangingLLM{},
		MemoryManager: testMemoryManager(t),
	}

	trace := NewExecutionTrace("test")
	trace.RecordStep("shell_command", nil, nil, nil)
	trace.Finalize()

	// The Reflect method now uses a 45s internal timeout. We use a short
	// parent context to verify it returns promptly on cancellation.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		reflector.Reflect(ctx, trace, nil)
		close(done)
	}()

	select {
	case <-done:
		// Good — Reflect returned within the timeout
	case <-time.After(5 * time.Second):
		t.Fatal("Reflect did not return within 5s — timeout mechanism is broken")
	}
}

func TestValidateContent_RespectsTimeout(t *testing.T) {
	reflector := &MemoryReflector{
		LLM: &hangingLLM{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var result string
	go func() {
		result = reflector.validateContent(ctx, "Test Section", "existing", "proposed")
		close(done)
	}()

	select {
	case <-done:
		// On error/timeout, validateContent returns the proposed content as fallback
		if result != "proposed" {
			t.Errorf("expected proposed content as fallback on timeout, got %q", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("validateContent did not return within 5s — timeout mechanism is broken")
	}
}
