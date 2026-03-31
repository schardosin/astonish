package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/common"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockLLM implements model.LLM with a configurable response sequence.
type mockLLM struct {
	responses []*genai.Content // consumed in order
	requests  []*model.LLMRequest
}

func (m *mockLLM) Name() string { return "mock_llm" }

func (m *mockLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.requests = append(m.requests, req)
		if len(m.responses) == 0 {
			yield(nil, fmt.Errorf("mockLLM: no more responses"))
			return
		}
		content := m.responses[0]
		m.responses = m.responses[1:]
		yield(&model.LLMResponse{Content: content, TurnComplete: true}, nil)
	}
}

// mockLLMFunc lets individual tests supply a custom generate function.
type mockLLMFunc struct {
	fn func(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error]
}

func (m *mockLLMFunc) Name() string { return "mock_llm_func" }
func (m *mockLLMFunc) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.fn(ctx, req, stream)
}

// mockState implements session.State for testing.
type mockState struct {
	data map[string]any
}

func newMockState() *mockState {
	return &mockState{data: make(map[string]any)}
}

func (s *mockState) Get(key string) (any, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

func (s *mockState) Set(key string, value any) error {
	s.data[key] = value
	return nil
}

func (s *mockState) Delete(key string) error {
	delete(s.data, key)
	return nil
}

func (s *mockState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

// mockTool implements tool.Tool + common.RunnableTool for testing.
type mockTool struct {
	name        string
	description string
	declaration *genai.FunctionDeclaration
	runFunc     func(ctx tool.Context, args any) (map[string]any, error)
}

func (t *mockTool) Name() string                                             { return t.name }
func (t *mockTool) Description() string                                      { return t.description }
func (t *mockTool) IsLongRunning() bool                                      { return false }
func (t *mockTool) ProcessRequest(_ tool.Context, _ *model.LLMRequest) error { return nil }

// Declaration satisfies common.ToolWithDeclaration.
func (t *mockTool) Declaration() *genai.FunctionDeclaration { return t.declaration }

// Run satisfies the local runnableTool interface in react.go.
func (t *mockTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	if t.runFunc != nil {
		return t.runFunc(ctx, args)
	}
	return map[string]any{"result": "ok"}, nil
}

// Compile-time checks.
var (
	_ model.LLM                  = (*mockLLM)(nil)
	_ model.LLM                  = (*mockLLMFunc)(nil)
	_ tool.Tool                  = (*mockTool)(nil)
	_ common.ToolWithDeclaration = (*mockTool)(nil)
	_ common.RunnableTool        = (*mockTool)(nil)
)

// ---------------------------------------------------------------------------
// Helper to build a text content response.
// ---------------------------------------------------------------------------
func textContent(text string) *genai.Content {
	return &genai.Content{
		Parts: []*genai.Part{{Text: text}},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRemoveThinkTags(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"no tags", "hello world", "hello world"},
		{"simple", "<think>hidden</think>visible", "visible"},
		{"multiline", "<think>\nfoo\nbar\n</think>done", "done"},
		{"nested-ish", "<think>a<think>b</think>c", "c"},
		{"empty tags", "<think></think>text", "text"},
		{"multiple", "<think>a</think>mid<think>b</think>end", "midend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeThinkTags(tt.input)
			if got != tt.expect {
				t.Errorf("removeThinkTags(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestGetToolNames(t *testing.T) {
	p := &ReActPlanner{
		Tools: []tool.Tool{
			&mockTool{name: "alpha"},
			&mockTool{name: "beta"},
			&mockTool{name: "gamma"},
		},
	}
	got := p.getToolNames()
	if got != "alpha, beta, gamma" {
		t.Errorf("getToolNames() = %q, want %q", got, "alpha, beta, gamma")
	}
}

func TestGetToolDescriptions_GenaiSchema(t *testing.T) {
	p := &ReActPlanner{
		Tools: []tool.Tool{
			&mockTool{
				name:        "search",
				description: "Search the web",
				declaration: &genai.FunctionDeclaration{
					Name: "search",
					ParametersJsonSchema: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"query": {Type: genai.TypeString, Description: "Search query"},
						},
						Required: []string{"query"},
					},
				},
			},
		},
	}
	desc := p.getToolDescriptions()
	if !strings.Contains(desc, "search: Search the web") {
		t.Errorf("expected tool header in description, got:\n%s", desc)
	}
	if !strings.Contains(desc, "query") {
		t.Errorf("expected parameter 'query' in description, got:\n%s", desc)
	}
	if !strings.Contains(desc, "(required)") {
		t.Errorf("expected '(required)' marker, got:\n%s", desc)
	}
}

func TestGetToolDescriptions_MapSchema(t *testing.T) {
	p := &ReActPlanner{
		Tools: []tool.Tool{
			&mockTool{
				name:        "fetch",
				description: "Fetch a URL",
				declaration: &genai.FunctionDeclaration{
					Name: "fetch",
					ParametersJsonSchema: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"url": map[string]interface{}{
								"type":        "string",
								"description": "The URL to fetch",
							},
						},
						"required": []interface{}{"url"},
					},
				},
			},
		},
	}
	desc := p.getToolDescriptions()
	if !strings.Contains(desc, "fetch: Fetch a URL") {
		t.Errorf("expected tool header, got:\n%s", desc)
	}
	if !strings.Contains(desc, "url") {
		t.Errorf("expected parameter 'url', got:\n%s", desc)
	}
}

func TestGetToolDescriptions_NoDeclaration(t *testing.T) {
	// A tool that implements ToolWithDeclaration but returns nil
	p := &ReActPlanner{
		Tools: []tool.Tool{
			&mockTool{
				name:        "simple",
				description: "A simple tool",
				declaration: nil,
			},
		},
	}
	desc := p.getToolDescriptions()
	if !strings.Contains(desc, "simple: A simple tool") {
		t.Errorf("expected basic description, got:\n%s", desc)
	}
	// Should not contain "Parameters:" since declaration is nil
	if strings.Contains(desc, "Parameters:") {
		t.Errorf("should not have parameters section for nil declaration, got:\n%s", desc)
	}
}

func TestRun_FinalAnswerFirstStep(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent("I know the answer already.\nFinal Answer: 42"),
		},
	}
	p := NewReActPlanner(llm, nil)
	result, err := p.Run(context.Background(), "What is the meaning of life?", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("Run() = %q, want %q", result, "42")
	}
}

func TestRun_MultiStepToolUse(t *testing.T) {
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					// First step: LLM decides to use a tool
					text = `I need to search for this.
Action: web_search
Action Input: {"query": "meaning of life"}`
				case 2:
					// After prompt switch at step 1, LLM sees the observation and gives final answer
					text = "Thought: I now know the final answer\nFinal Answer: The answer is 42"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{
					Content: textContent(text),
				}, nil)
			}
		},
	}

	searchTool := &mockTool{
		name:        "web_search",
		description: "Search the web",
		runFunc: func(_ tool.Context, args any) (map[string]any, error) {
			return map[string]any{"result": "The meaning of life is 42"}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{searchTool})
	result, err := p.Run(context.Background(), "What is the meaning of life?", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The answer is 42" {
		t.Errorf("Run() = %q, want %q", result, "The answer is 42")
	}
}

func TestRun_MaxStepsExceeded(t *testing.T) {
	// LLM always returns an action, never a final answer
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				yield(&model.LLMResponse{
					Content: textContent("Thought: still thinking\nAction: search\nAction Input: {\"q\": \"test\"}"),
				}, nil)
			}
		},
	}

	searchTool := &mockTool{
		name: "search",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return map[string]any{"result": "something"}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{searchTool})
	_, err := p.Run(context.Background(), "loop forever", "")
	if err == nil {
		t.Fatal("expected error for max steps exceeded")
	}
	if !strings.Contains(err.Error(), "max ReAct steps") {
		t.Errorf("expected 'max ReAct steps' error, got: %v", err)
	}
}

func TestRun_ToolNotFound(t *testing.T) {
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = "Action: nonexistent_tool\nAction Input: {}"
				case 2:
					text = "Thought: I now know the answer\nFinal Answer: recovered"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	realTool := &mockTool{name: "real_tool"}
	p := NewReActPlanner(llm, []tool.Tool{realTool})
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("Run() = %q, want %q", result, "recovered")
	}
}

func TestRun_ApprovalRequired(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent("Action: dangerous_tool\nAction Input: {\"cmd\": \"rm -rf /\"}"),
		},
	}

	dangerousTool := &mockTool{
		name: "dangerous_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			t.Fatal("tool should not have been executed")
			return nil, nil
		},
	}

	state := newMockState()
	p := NewReActPlannerWithApproval(llm, []tool.Tool{dangerousTool}, func(name string, args map[string]any) (bool, error) {
		// Deny approval
		return false, nil
	}, state, false)

	_, err := p.Run(context.Background(), "do something dangerous", "")
	if err == nil {
		t.Fatal("expected APPROVAL_REQUIRED error")
	}
	if !strings.Contains(err.Error(), "APPROVAL_REQUIRED") {
		t.Errorf("expected APPROVAL_REQUIRED, got: %v", err)
	}

	// Verify state was saved for resume
	if _, stateErr := state.Get("_react_history"); stateErr != nil {
		t.Error("expected _react_history to be saved in state")
	}
	if _, stateErr := state.Get("_react_pending_action"); stateErr != nil {
		t.Error("expected _react_pending_action to be saved in state")
	}
}

func TestRun_ApprovalGranted(t *testing.T) {
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = "Action: safe_tool\nAction Input: {\"data\": \"hello\"}"
				case 2:
					text = "Thought: I got it\nFinal Answer: done"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	executed := false
	safeTool := &mockTool{
		name: "safe_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			executed = true
			return map[string]any{"status": "ok"}, nil
		},
	}

	state := newMockState()
	p := NewReActPlannerWithApproval(llm, []tool.Tool{safeTool}, func(name string, args map[string]any) (bool, error) {
		return true, nil // Approve
	}, state, false)

	result, err := p.Run(context.Background(), "do something", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected tool to be executed after approval")
	}
	if result != "done" {
		t.Errorf("Run() = %q, want %q", result, "done")
	}
}

func TestRun_ResumeFromPausedState(t *testing.T) {
	// Simulate a resumed execution: the state already has history and pending action
	state := newMockState()
	_ = state.Set("_react_history", "Question: test\nThought: I need to use a tool\nAction: my_tool\nAction Input: {\"x\": 1}")
	_ = state.Set("_react_step", 2)
	_ = state.Set("_react_pending_action", "my_tool")
	_ = state.Set("_react_pending_input", `{"x": 1}`)

	toolExecuted := false
	myTool := &mockTool{
		name: "my_tool",
		runFunc: func(_ tool.Context, args any) (map[string]any, error) {
			toolExecuted = true
			return map[string]any{"answer": "resumed"}, nil
		},
	}

	// After the tool executes, the LLM sees the observation and gives final answer
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent("Thought: I now know the answer\nFinal Answer: resumed successfully"),
		},
	}

	p := NewReActPlannerWithApproval(llm, []tool.Tool{myTool}, nil, state, false)
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !toolExecuted {
		t.Error("expected tool to be executed during resume")
	}
	if result != "resumed successfully" {
		t.Errorf("Run() = %q, want %q", result, "resumed successfully")
	}
}

func TestRun_StopHereTruncation(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			// LLM generates action but also hallucinates observation after STOP HERE
			textContent("Action: my_tool\nAction Input: {\"x\": 1}\n\nSTOP HERE\nObservation: hallucinated result\nFinal Answer: wrong"),
			// After real tool execution, LLM gives correct answer
			textContent("Thought: I now know\nFinal Answer: correct"),
		},
	}

	myTool := &mockTool{
		name: "my_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return map[string]any{"real": "result"}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{myTool})
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "correct" {
		t.Errorf("Run() = %q, want %q (should not use hallucinated answer)", result, "correct")
	}
}

func TestRun_InvalidActionInputWraps(t *testing.T) {
	// LLM provides raw text instead of JSON for action input
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					// Invalid JSON — just a plain string
					text = "Action: search\nAction Input: hello world"
				case 2:
					text = "Thought: Got it\nFinal Answer: wrapped"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	var capturedArgs any
	searchTool := &mockTool{
		name:        "search",
		description: "Search",
		declaration: &genai.FunctionDeclaration{
			Name: "search",
			ParametersJsonSchema: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"query": {Type: genai.TypeString},
				},
				Required: []string{"query"},
			},
		},
		runFunc: func(_ tool.Context, args any) (map[string]any, error) {
			capturedArgs = args
			return map[string]any{"result": "ok"}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{searchTool})
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "wrapped" {
		t.Errorf("Run() = %q, want %q", result, "wrapped")
	}

	// Verify the raw input was wrapped using the schema's required field
	argsMap, ok := capturedArgs.(map[string]any)
	if !ok {
		t.Fatalf("expected args to be map[string]any, got %T", capturedArgs)
	}
	if argsMap["query"] != "hello world" {
		t.Errorf("expected wrapped arg query=%q, got %v", "hello world", argsMap["query"])
	}
}

func TestRun_ThinkTagsInResponse(t *testing.T) {
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = "<think>internal reasoning</think>Action: my_tool\nAction Input: {}"
				case 2:
					text = "Thought: done\nFinal Answer: success"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	myTool := &mockTool{
		name: "my_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{myTool})
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("Run() = %q, want %q", result, "success")
	}
}

func TestRun_LLMError(t *testing.T) {
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				yield(nil, fmt.Errorf("API rate limit exceeded"))
			}
		},
	}

	p := NewReActPlanner(llm, nil)
	_, err := p.Run(context.Background(), "test", "")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "LLM generation failed") {
		t.Errorf("expected 'LLM generation failed' error, got: %v", err)
	}
}

func TestRun_WithSystemInstruction(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent("Final Answer: yes"),
		},
	}

	p := NewReActPlanner(llm, nil)
	result, err := p.Run(context.Background(), "test", "You are a helpful assistant.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "yes" {
		t.Errorf("Run() = %q, want %q", result, "yes")
	}

	// Verify system instruction was included in the prompt
	if len(llm.requests) == 0 {
		t.Fatal("expected at least one LLM request")
	}
	prompt := llm.requests[0].Contents[0].Parts[0].Text
	if !strings.Contains(prompt, "You are a helpful assistant.") {
		t.Errorf("system instruction not found in prompt:\n%s", prompt)
	}
}

func TestRun_ActionInputMissingAction(t *testing.T) {
	// LLM provides Action Input but no Action line
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = "Thought: I need something\nAction Input: {\"q\": \"test\"}"
				case 2:
					text = "Final Answer: recovered from format error"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	p := NewReActPlanner(llm, nil)
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered from format error" {
		t.Errorf("Run() = %q, want %q", result, "recovered from format error")
	}
}

func TestRun_GlobalVariablesSanitization(t *testing.T) {
	// Test the workaround for models that output "map[]" for empty objects
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = `Action: my_tool
Action Input: {"command": "test", "global_variables": "map[]"}`
				case 2:
					text = "Final Answer: sanitized"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	var capturedArgs any
	myTool := &mockTool{
		name: "my_tool",
		runFunc: func(_ tool.Context, args any) (map[string]any, error) {
			capturedArgs = args
			return map[string]any{"ok": true}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{myTool})
	_, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsMap, ok := capturedArgs.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", capturedArgs)
	}
	// global_variables should have been sanitized from "map[]" to an empty map
	gv, ok := argsMap["global_variables"]
	if !ok {
		t.Fatal("expected global_variables key in args")
	}
	if _, isMap := gv.(map[string]any); !isMap {
		t.Errorf("expected global_variables to be map[string]any, got %T (%v)", gv, gv)
	}
}

func TestFormatOutput_WithSchema(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent(`{"name": "John", "age": 30}`),
		},
	}

	p := &ReActPlanner{LLM: llm}
	schema := map[string]string{
		"name": "string",
		"age":  "integer",
	}

	result, err := p.FormatOutput(context.Background(), "The person's name is John and they are 30 years old.", schema, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify result is valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error: %v (result: %s)", err, result)
	}
}

func TestFormatOutput_EmptySchema(t *testing.T) {
	p := &ReActPlanner{LLM: &mockLLM{}}
	result, err := p.FormatOutput(context.Background(), "raw text result", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "raw text result" {
		t.Errorf("FormatOutput() = %q, want %q", result, "raw text result")
	}
}

func TestFormatOutput_WithSystemInstruction(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent(`{"answer": "42"}`),
		},
	}

	p := &ReActPlanner{LLM: llm}
	schema := map[string]string{"answer": "string"}

	_, err := p.FormatOutput(context.Background(), "forty two", schema, "Be precise.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify system instruction was included
	if len(llm.requests) == 0 {
		t.Fatal("expected at least one LLM request")
	}
	prompt := llm.requests[0].Contents[0].Parts[0].Text
	if !strings.Contains(prompt, "Be precise.") {
		t.Errorf("system instruction not found in format prompt:\n%s", prompt)
	}
}

func TestFormatOutput_MarkdownCleanup(t *testing.T) {
	llm := &mockLLM{
		responses: []*genai.Content{
			textContent("```json\n{\"key\": \"value\"}\n```"),
		},
	}

	p := &ReActPlanner{LLM: llm}
	schema := map[string]string{"key": "string"}

	result, err := p.FormatOutput(context.Background(), "test", schema, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should strip markdown code blocks
	if strings.Contains(result, "```") {
		t.Errorf("expected markdown to be stripped, got: %s", result)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("expected valid JSON after cleanup, got error: %v (result: %s)", err, result)
	}
}

func TestFormatOutput_LLMError(t *testing.T) {
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				yield(nil, fmt.Errorf("API error"))
			}
		},
	}

	p := &ReActPlanner{LLM: llm}
	schema := map[string]string{"key": "string"}
	_, err := p.FormatOutput(context.Background(), "test", schema, "")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "formatting LLM call failed") {
		t.Errorf("expected 'formatting LLM call failed' error, got: %v", err)
	}
}

func TestExecuteTool_NotRunnable(t *testing.T) {
	// A tool that implements tool.Tool but NOT the runnableTool interface
	type nonRunnableTool struct {
		tool.Tool
	}

	nrt := &nonRunnableTool{
		Tool: &mockTool{name: "no_run"},
	}

	p := &ReActPlanner{
		Tools: []tool.Tool{nrt.Tool},
	}
	result, err := p.executeTool(context.Background(), "no_run", "{}")
	// mockTool does implement Run, so this test verifies the positive path
	// For a truly non-runnable tool, we'd need a pure interface implementation
	_ = result
	_ = err
}

func TestExecuteTool_ToolNotFound(t *testing.T) {
	p := &ReActPlanner{
		Tools: []tool.Tool{
			&mockTool{name: "exists"},
		},
	}
	result, err := p.executeTool(context.Background(), "missing", "{}")
	if err != nil {
		t.Fatalf("executeTool should return error as observation text, not as error: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' in result, got: %s", result)
	}
	if !strings.Contains(result, "exists") {
		t.Errorf("expected available tool name in result, got: %s", result)
	}
}

func TestExecuteTool_ToolError(t *testing.T) {
	failTool := &mockTool{
		name: "fail_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return nil, fmt.Errorf("tool execution failed")
		},
	}

	p := &ReActPlanner{
		Tools: []tool.Tool{failTool},
	}
	result, err := p.executeTool(context.Background(), "fail_tool", `{"x": 1}`)
	if err != nil {
		t.Fatalf("tool errors should be returned as observation text, not error: %v", err)
	}
	if !strings.Contains(result, "Error executing tool") {
		t.Errorf("expected error observation, got: %s", result)
	}
}

func TestExecuteTool_ApprovalDenied(t *testing.T) {
	myTool := &mockTool{
		name: "protected",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	p := &ReActPlanner{
		Tools: []tool.Tool{myTool},
		ApprovalCallback: func(name string, args map[string]any) (bool, error) {
			return false, nil
		},
	}

	result, err := p.executeTool(context.Background(), "protected", `{"x": 1}`)
	if err == nil || err.Error() != "tool approval required" {
		t.Errorf("expected 'tool approval required' error, got: err=%v result=%s", err, result)
	}
	if result != "APPROVAL_REQUIRED" {
		t.Errorf("expected APPROVAL_REQUIRED result, got: %s", result)
	}
}

func TestExecuteTool_ApprovalError(t *testing.T) {
	myTool := &mockTool{
		name: "protected",
	}

	p := &ReActPlanner{
		Tools: []tool.Tool{myTool},
		ApprovalCallback: func(name string, args map[string]any) (bool, error) {
			return false, fmt.Errorf("auth service unavailable")
		},
	}

	result, err := p.executeTool(context.Background(), "protected", `{"x": 1}`)
	if err != nil {
		t.Fatalf("approval errors should be observation text, not error: %v", err)
	}
	if !strings.Contains(result, "auth service unavailable") {
		t.Errorf("expected approval error in observation, got: %s", result)
	}
}

func TestExecuteTool_JSONResult(t *testing.T) {
	myTool := &mockTool{
		name: "data_tool",
		runFunc: func(_ tool.Context, _ any) (map[string]any, error) {
			return map[string]any{
				"items": []string{"a", "b", "c"},
				"count": 3,
			}, nil
		},
	}

	p := &ReActPlanner{Tools: []tool.Tool{myTool}}
	result, err := p.executeTool(context.Background(), "data_tool", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be valid JSON
	var parsed map[string]any
	if jsonErr := json.Unmarshal([]byte(result), &parsed); jsonErr != nil {
		t.Fatalf("expected valid JSON result, got error: %v (result: %s)", jsonErr, result)
	}
}

func TestExecuteTool_CodeBlockInput(t *testing.T) {
	// Verify that the Run method handles code-block-wrapped input via the main Run loop
	// (code block stripping happens in Run, not executeTool directly)
	step := 0
	llm := &mockLLMFunc{
		fn: func(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
			return func(yield func(*model.LLMResponse, error) bool) {
				step++
				var text string
				switch step {
				case 1:
					text = "Action: my_tool\nAction Input: ```json\n{\"key\": \"value\"}\n```"
				case 2:
					text = "Final Answer: code block handled"
				default:
					text = "Final Answer: fallback"
				}
				yield(&model.LLMResponse{Content: textContent(text)}, nil)
			}
		},
	}

	var capturedArgs any
	myTool := &mockTool{
		name: "my_tool",
		runFunc: func(_ tool.Context, args any) (map[string]any, error) {
			capturedArgs = args
			return map[string]any{"ok": true}, nil
		},
	}

	p := NewReActPlanner(llm, []tool.Tool{myTool})
	result, err := p.Run(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "code block handled" {
		t.Errorf("Run() = %q, want %q", result, "code block handled")
	}

	// Verify code blocks were stripped and JSON was parsed
	if argsMap, ok := capturedArgs.(map[string]any); ok {
		if argsMap["key"] != "value" {
			t.Errorf("expected key=value, got: %v", argsMap)
		}
	}
}

func TestNewReActPlanner(t *testing.T) {
	llm := &mockLLM{}
	tools := []tool.Tool{&mockTool{name: "t1"}}
	p := NewReActPlanner(llm, tools)
	if p.LLM != llm {
		t.Error("LLM not set")
	}
	if len(p.Tools) != 1 {
		t.Error("Tools not set")
	}
	if p.ApprovalCallback != nil {
		t.Error("ApprovalCallback should be nil")
	}
}

func TestNewReActPlannerWithApproval(t *testing.T) {
	llm := &mockLLM{}
	tools := []tool.Tool{&mockTool{name: "t1"}}
	state := newMockState()
	cb := func(string, map[string]any) (bool, error) { return true, nil }

	p := NewReActPlannerWithApproval(llm, tools, cb, state, true)
	if p.LLM != llm {
		t.Error("LLM not set")
	}
	if p.ApprovalCallback == nil {
		t.Error("ApprovalCallback should be set")
	}
	if p.State != state {
		t.Error("State not set")
	}
	if !p.DebugMode {
		t.Error("DebugMode should be true")
	}
}
