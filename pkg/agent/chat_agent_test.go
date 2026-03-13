package agent

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

func TestExtractInputParams_NilTrace(t *testing.T) {
	ca := &ChatAgent{}
	params := ca.extractInputParams(context.Background(), "nodes: []", nil)
	if params != nil {
		t.Errorf("expected nil for nil trace, got %v", params)
	}
}

func TestExtractInputParams_EmptyYAML(t *testing.T) {
	ca := &ChatAgent{}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	params := ca.extractInputParams(context.Background(), "", trace)
	if params != nil {
		t.Errorf("expected nil for empty YAML, got %v", params)
	}
}

func TestExtractInputParams_NoDistiller(t *testing.T) {
	ca := &ChatAgent{}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	// No FlowDistiller set — should return nil gracefully
	params := ca.extractInputParams(context.Background(), `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
    output_model:
      host: str
`, trace)
	if params != nil {
		t.Errorf("expected nil when no distiller, got %v", params)
	}
}

func TestExtractInputParams_NoInputNodes(t *testing.T) {
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				t.Fatal("LLM should not be called when there are no input nodes")
				return "", nil
			},
		},
	}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	params := ca.extractInputParams(context.Background(), `
nodes:
  - name: do_stuff
    type: llm
    prompt: "Do something"
`, trace)
	if params != nil {
		t.Errorf("expected nil for no input nodes, got %v", params)
	}
}

func TestExtractInputParams_ParsesLLMResponse(t *testing.T) {
	// Mock the LLM to return known parameter values
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				return "get_connection_info=192.168.1.200\nget_ssh_user=root\n", nil
			},
		},
	}

	trace := NewExecutionTrace("show proxmox VMs")
	trace.RecordStep("shell_command", map[string]any{
		"command": `ssh root@192.168.1.200 "qm list"`,
	}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(context.Background(), `
nodes:
  - name: get_connection_info
    type: input
    prompt: "Enter IP:"
    output_model:
      server_ip: str
  - name: get_ssh_user
    type: input
    prompt: "Enter user:"
    output_model:
      ssh_user: str
  - name: fetch
    type: llm
    prompt: "SSH as {ssh_user} to {server_ip}"
    tools: true
`, trace)

	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}

	paramMap := make(map[string]string)
	for _, p := range params {
		parts := splitFirst(p, "=")
		paramMap[parts[0]] = parts[1]
	}

	if paramMap["get_connection_info"] != "192.168.1.200" {
		t.Errorf("expected get_connection_info=192.168.1.200, got %s", paramMap["get_connection_info"])
	}
	if paramMap["get_ssh_user"] != "root" {
		t.Errorf("expected get_ssh_user=root, got %s", paramMap["get_ssh_user"])
	}
}

func TestExtractInputParams_IgnoresInvalidLLMResponse(t *testing.T) {
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				// LLM returns some correct and some garbage lines
				return "get_host=10.0.0.1\n# comment\ngarbage line\nunknown_param=nope\n", nil
			},
		},
	}

	trace := NewExecutionTrace("test")
	trace.RecordStep("some_tool", map[string]any{"x": "y"}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(context.Background(), `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
  - name: get_port
    type: input
    prompt: "Enter port:"
`, trace)

	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}

	paramMap := make(map[string]string)
	for _, p := range params {
		parts := splitFirst(p, "=")
		paramMap[parts[0]] = parts[1]
	}

	if paramMap["get_host"] != "10.0.0.1" {
		t.Errorf("expected get_host=10.0.0.1, got %s", paramMap["get_host"])
	}
	// get_port was not in the LLM response — should fall back to <value>
	if paramMap["get_port"] != "<value>" {
		t.Errorf("expected get_port=<value>, got %s", paramMap["get_port"])
	}
}

func TestExtractInputParams_LLMPromptContainsTrace(t *testing.T) {
	// Verify that the prompt sent to the LLM includes the trace data and output_model fields
	var capturedPrompt string
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				capturedPrompt = prompt
				return "get_host=10.0.0.1\n", nil
			},
		},
	}

	trace := NewExecutionTrace("connect to my server")
	trace.RecordStep("shell_command", map[string]any{
		"command": "ssh root@10.0.0.1",
	}, nil, nil)
	trace.Finalize()

	ca.extractInputParams(context.Background(), `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
    output_model:
      server_ip: str
`, trace)

	// The prompt should contain the trace tool name and args
	if capturedPrompt == "" {
		t.Fatal("LLM was not called")
	}
	if !containsAll(capturedPrompt, "shell_command", "ssh root@10.0.0.1", "get_host", "connect to my server") {
		t.Errorf("LLM prompt missing expected content:\n%s", capturedPrompt)
	}
	// The prompt should include output_model field names as context
	if !containsAll(capturedPrompt, "server_ip") {
		t.Errorf("LLM prompt should include output_model fields:\n%s", capturedPrompt)
	}
	// The prompt should include conciseness guidance
	if !containsAll(capturedPrompt, "SHORT", "EXACT LITERAL") {
		t.Errorf("LLM prompt should include conciseness instructions:\n%s", capturedPrompt)
	}
}

func TestExtractInputParams_OutputModelMultipleFields(t *testing.T) {
	// Verify output_model with multiple fields is included in the prompt
	var capturedPrompt string
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				capturedPrompt = prompt
				return "get_connection_info=root@192.168.1.200\n", nil
			},
		},
	}

	trace := NewExecutionTrace("show proxmox VMs")
	trace.RecordStep("shell_command", map[string]any{
		"command": `ssh root@192.168.1.200 "pvesh get /cluster/resources"`,
	}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(context.Background(), `
nodes:
  - name: get_connection_info
    type: input
    prompt: "Enter SSH connection details:"
    output_model:
      ssh_user: str
      ssh_ip: str
  - name: fetch
    type: llm
    prompt: "SSH to {ssh_ip}"
    tools: true
`, trace)

	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0] != "get_connection_info=root@192.168.1.200" {
		t.Errorf("expected get_connection_info=root@192.168.1.200, got %s", params[0])
	}
	// Prompt should mention the output_model fields
	if !containsAll(capturedPrompt, "ssh_user", "ssh_ip") {
		t.Errorf("LLM prompt should include output_model field names:\n%s", capturedPrompt)
	}
}

func TestFlowYAML_ParsesInputNodes(t *testing.T) {
	// Verify the YAML parsing correctly identifies input nodes
	yamlStr := `
nodes:
  - name: get_ip
    type: input
    prompt: "Enter IP:"
    output_model:
      ip: str
  - name: process
    type: llm
    prompt: "Process {ip}"
  - name: get_user
    type: input
    prompt: "Enter user:"
    output_model:
      user: str
`
	var flow flowYAML
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	var inputNames []string
	for _, node := range flow.Nodes {
		if node.Type == "input" {
			inputNames = append(inputNames, node.Name)
		}
	}

	if len(inputNames) != 2 {
		t.Fatalf("expected 2 input nodes, got %d: %v", len(inputNames), inputNames)
	}
	if inputNames[0] != "get_ip" || inputNames[1] != "get_user" {
		t.Errorf("expected [get_ip, get_user], got %v", inputNames)
	}
}

// containsAll checks if s contains all of the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// splitFirst splits s on the first occurrence of sep.
func splitFirst(s, sep string) [2]string {
	idx := len(sep)
	for i := 0; i < len(s)-len(sep)+1; i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	return [2]string{s[:idx], s[idx+len(sep):]}
}

// --- thinkTagFilter streaming tests ---

func TestThinkTagFilter_SingleChunkComplete(t *testing.T) {
	// The entire think block arrives in one chunk.
	f := &thinkTagFilter{}
	got := f.Feed("<think>reasoning here</think>Hello!")
	if got != "Hello!" {
		t.Errorf("got %q, want %q", got, "Hello!")
	}
}

func TestThinkTagFilter_StreamedAcrossChunks(t *testing.T) {
	// Simulates the real streaming scenario: tags split across many small chunks.
	f := &thinkTagFilter{}
	chunks := []string{
		"<thi",
		"nk>",
		"The user said ",
		"\"Hi\".",
		" This is a simple",
		" greeting.",
		"</thi",
		"nk>",
		"Hello! ",
		"How can I help?",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Hello! How can I help?"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_MultipleThinkBlocks(t *testing.T) {
	// Multiple think blocks interleaved with visible text.
	f := &thinkTagFilter{}
	chunks := []string{
		"<think>first thought</think>",
		"Visible 1",
		"<think>second thought</think>",
		" Visible 2",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Visible 1 Visible 2"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_ThinkingTag(t *testing.T) {
	// <thinking> variant.
	f := &thinkTagFilter{}
	chunks := []string{
		"<thinking>deep reasoning</thinking>",
		"The answer is 42.",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "The answer is 42."
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_ThinkingTagStreamed(t *testing.T) {
	// <thinking> tag split across chunks.
	f := &thinkTagFilter{}
	chunks := []string{
		"<thinkin",
		"g>",
		"reasoning",
		"</thinkin",
		"g>",
		"Result",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Result"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_NoThinkTags(t *testing.T) {
	// Normal text without think tags passes through unchanged.
	f := &thinkTagFilter{}
	chunks := []string{"Hello ", "world! ", "How are you?"}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Hello world! How are you?"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_OnlyThinkContent(t *testing.T) {
	// Entire response is inside think tags — output should be empty.
	f := &thinkTagFilter{}
	chunks := []string{
		"<think>",
		"all reasoning, no answer",
		"</think>",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	if out.String() != "" {
		t.Errorf("got %q, want empty", out.String())
	}
}

func TestThinkTagFilter_TagSplitAtEveryByte(t *testing.T) {
	// Worst case: every byte arrives as a separate chunk.
	input := "<think>hidden</think>visible"
	f := &thinkTagFilter{}
	var out strings.Builder
	for _, b := range []byte(input) {
		out.WriteString(f.Feed(string(b)))
	}
	want := "visible"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_TextBeforeThinkTag(t *testing.T) {
	// Text appears before the think tag.
	f := &thinkTagFilter{}
	chunks := []string{
		"Sure! ",
		"<think>let me reason</think>",
		"Here is the answer.",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Sure! Here is the answer."
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_NemotronExample(t *testing.T) {
	// Simulates the real Nemotron output from the issue.
	f := &thinkTagFilter{}
	chunks := []string{
		"<think>",
		"We need to produce a market report ",
		"for today's date.",
		"\n\n",
		"</think>",
		"\n",
		"Here's today's stock market recap",
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "\nHere's today's stock market recap"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestThinkTagFilter_PartialOpenTagNotConsumed(t *testing.T) {
	// A '<' that isn't part of a think tag should still be emitted.
	f := &thinkTagFilter{}
	got := f.Feed("a < b and c > d")
	if got != "a < b and c > d" {
		t.Errorf("got %q, want %q", got, "a < b and c > d")
	}
}

func TestThinkTagFilter_AngleBracketInNormalText(t *testing.T) {
	// HTML-like content that isn't a think tag.
	f := &thinkTagFilter{}
	chunks := []string{"Use <b>", "bold</b>", " text"}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(f.Feed(c))
	}
	want := "Use <b>bold</b> text"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestFilterEventThinkContent_DropsThoughtParts(t *testing.T) {
	// Parts with Thought=true should be dropped entirely.
	f := &thinkTagFilter{}
	event := &session.Event{}
	event.LLMResponse.Content = &genai.Content{
		Role: "model",
		Parts: []*genai.Part{
			{Text: "thinking internally", Thought: true},
			{Text: "visible answer"},
		},
	}
	filterEventThinkContent(f, event)
	if len(event.LLMResponse.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(event.LLMResponse.Content.Parts))
	}
	if event.LLMResponse.Content.Parts[0].Text != "visible answer" {
		t.Errorf("got %q, want %q", event.LLMResponse.Content.Parts[0].Text, "visible answer")
	}
}

func TestFilterEventThinkContent_NilEvent(t *testing.T) {
	// Should not panic on nil.
	f := &thinkTagFilter{}
	filterEventThinkContent(f, nil)
}

func TestFilterEventThinkContent_NilContent(t *testing.T) {
	// Should not panic when content is nil.
	f := &thinkTagFilter{}
	event := &session.Event{}
	filterEventThinkContent(f, event)
}

func TestFilterEventThinkContent_WhitespaceOnlyAfterStrip(t *testing.T) {
	// When think tags are stripped and only whitespace remains, the part
	// should be dropped entirely — not yielded as an empty message.
	f := &thinkTagFilter{}

	// Simulate streaming: first chunk opens think block
	event1 := &session.Event{}
	event1.LLMResponse.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: "<think>reasoning"}},
	}
	filterEventThinkContent(f, event1)
	if len(event1.LLMResponse.Content.Parts) != 0 {
		t.Errorf("expected 0 parts during think block, got %d", len(event1.LLMResponse.Content.Parts))
	}

	// Second chunk closes think block with trailing whitespace
	event2 := &session.Event{}
	event2.LLMResponse.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: "</think>\n\n"}},
	}
	filterEventThinkContent(f, event2)
	if len(event2.LLMResponse.Content.Parts) != 0 {
		t.Errorf("expected 0 parts for whitespace-only remnant, got %d", len(event2.LLMResponse.Content.Parts))
	}

	// Third chunk is real content — should pass through
	event3 := &session.Event{}
	event3.LLMResponse.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: "Hello!"}},
	}
	filterEventThinkContent(f, event3)
	if len(event3.LLMResponse.Content.Parts) != 1 {
		t.Fatalf("expected 1 part for real content, got %d", len(event3.LLMResponse.Content.Parts))
	}
	if event3.LLMResponse.Content.Parts[0].Text != "Hello!" {
		t.Errorf("got %q, want %q", event3.LLMResponse.Content.Parts[0].Text, "Hello!")
	}
}

func TestFilterEventThinkContent_ToolCallPartPreserved(t *testing.T) {
	// Parts with FunctionCall should pass through even when adjacent to
	// think-tagged text parts that get dropped.
	f := &thinkTagFilter{}
	event := &session.Event{}
	event.LLMResponse.Content = &genai.Content{
		Role: "model",
		Parts: []*genai.Part{
			{Text: "\n\n"},
			{FunctionCall: &genai.FunctionCall{Name: "shell_command", Args: map[string]any{"command": "ls"}}},
		},
	}
	filterEventThinkContent(f, event)
	// The whitespace-only text part should be dropped, but the function call part must remain.
	if len(event.LLMResponse.Content.Parts) != 1 {
		t.Fatalf("expected 1 part (function call), got %d", len(event.LLMResponse.Content.Parts))
	}
	if event.LLMResponse.Content.Parts[0].FunctionCall == nil {
		t.Error("expected function call part to be preserved")
	}
}
