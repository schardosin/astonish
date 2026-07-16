package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"google.golang.org/adk/model"
)

// TestAIChatHandler_SSE_SmallPayload verifies that a small response is delivered
// as a well-formed SSE stream with chunk + complete events.
func TestAIChatHandler_SSE_SmallPayload(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(TextTurn("Hello from the AI assistant.")), nil
	}

	body := `{"message":"say hello","context":"create_flow","currentYaml":"","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	events := parseSSEEvents(t, rr.Body.String())
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event, got none")
	}

	// Should have at least a "chunk" and a "complete" event
	hasChunk := false
	hasComplete := false
	for _, ev := range events {
		if ev.Type == "chunk" {
			hasChunk = true
		}
		if ev.Type == "complete" {
			hasComplete = true
			// Verify complete event is valid JSON with expected fields
			var resp AIChatResponse
			if err := json.Unmarshal([]byte(ev.Data), &resp); err != nil {
				t.Errorf("failed to parse complete event data as AIChatResponse: %v", err)
			}
			if resp.Message == "" {
				t.Error("complete event should have non-empty Message")
			}
		}
	}

	if !hasChunk {
		t.Error("expected at least one 'chunk' SSE event")
	}
	if !hasComplete {
		t.Error("expected a 'complete' SSE event")
	}
}

// TestAIChatHandler_SSE_LargePayload verifies that a very large response (>50KB YAML)
// is correctly emitted as a single SSE event. This is the backend counterpart of the
// frontend regression test — ensures the server writes the full event correctly.
func TestAIChatHandler_SSE_LargePayload(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	// Generate a valid flow YAML that's large enough (>50KB).
	// Must have: name, description, nodes[] with valid node types, flow[] with edges.
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("name: large_flow\ndescription: A large test flow with many steps\nnodes:\n")

	nodeCount := 200
	for i := 0; i < nodeCount; i++ {
		// Use llm type nodes with long prompts to bulk up the size
		yamlBuilder.WriteString("  - name: step_")
		yamlBuilder.WriteString(fmt.Sprintf("%04d", i))
		yamlBuilder.WriteString("\n    type: llm\n    model: gemini-2.0-flash\n    prompt: \"")
		// Add a long prompt (~200 chars) to make each node substantial
		yamlBuilder.WriteString(strings.Repeat("Process the data and analyze the results carefully. ", 4))
		yamlBuilder.WriteString("\"\n")
	}

	// Add flow edges: START -> step_0000 -> step_0001 -> ... -> step_N -> END
	yamlBuilder.WriteString("flow:\n")
	yamlBuilder.WriteString("  - from: START\n    to: step_0000\n")
	for i := 0; i < nodeCount-1; i++ {
		yamlBuilder.WriteString(fmt.Sprintf("  - from: step_%04d\n    to: step_%04d\n", i, i+1))
	}
	yamlBuilder.WriteString(fmt.Sprintf("  - from: step_%04d\n    to: END\n", nodeCount-1))

	largeYAML := yamlBuilder.String()

	if len(largeYAML) < 50000 {
		t.Fatalf("test YAML should be >50KB, got %d bytes", len(largeYAML))
	}

	// Wrap in code fence as the LLM would produce
	llmResponse := "Here is the flow:\n```yaml\n" + largeYAML + "```\n"

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(TextTurn(llmResponse)), nil
	}

	body := `{"message":"create a big flow","context":"create_flow","currentYaml":"","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	events := parseSSEEvents(t, rr.Body.String())

	// Find the complete event
	var completeEvent *sseEvent
	for i := range events {
		if events[i].Type == "complete" {
			completeEvent = &events[i]
			break
		}
	}

	if completeEvent == nil {
		t.Fatal("no 'complete' SSE event found in response")
	}
	var resp AIChatResponse
	if err := json.Unmarshal([]byte(completeEvent.Data), &resp); err != nil {
		t.Fatalf("failed to parse complete event: %v\nRaw data length: %d", err, len(completeEvent.Data))
	}

	// The handler extracts YAML from the response text via extractYAML
	if resp.ProposedYAML == "" {
		t.Error("complete event should have non-empty ProposedYAML for a create_flow response containing ```yaml```")
	}
	if len(resp.ProposedYAML) < 40000 {
		t.Errorf("ProposedYAML should be large (>40KB), got %d bytes", len(resp.ProposedYAML))
	}

	// Verify the SSE wire format: the complete event should be a single data line
	rawBody := rr.Body.String()
	completeIdx := strings.Index(rawBody, "event: complete\ndata: ")
	if completeIdx == -1 {
		t.Fatal("could not find 'event: complete\\ndata: ' in raw response")
	}

	// From 'data: ' to the next '\n\n' should be one contiguous JSON line
	dataStart := completeIdx + len("event: complete\ndata: ")
	dataEnd := strings.Index(rawBody[dataStart:], "\n\n")
	if dataEnd == -1 {
		t.Fatal("could not find '\\n\\n' terminator after complete event data")
	}

	dataLine := rawBody[dataStart : dataStart+dataEnd]
	// Verify it's valid JSON
	var check json.RawMessage
	if err := json.Unmarshal([]byte(dataLine), &check); err != nil {
		t.Fatalf("complete event data line is not valid JSON: %v (length=%d)", err, len(dataLine))
	}
}

// TestAIChatHandler_SSE_ErrorPath verifies that errors produce proper SSE events.
func TestAIChatHandler_SSE_ErrorPath(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(ErrorTurn("rate_limit", "quota exceeded")), nil
	}

	body := `{"message":"hello","context":"create_flow","currentYaml":"","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	events := parseSSEEvents(t, rr.Body.String())

	// Should have an error event and a complete event
	hasError := false
	hasComplete := false
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
		}
		if ev.Type == "complete" {
			hasComplete = true
		}
	}

	if !hasError {
		t.Error("expected an 'error' SSE event")
	}
	if !hasComplete {
		t.Error("expected a 'complete' SSE event (always sent, even on error)")
	}
}

// TestAIChatHandler_NonStreaming verifies non-streaming (JSON) response mode.
func TestAIChatHandler_NonStreaming(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(TextTurn("Non-streaming response")), nil
	}

	body := `{"message":"hello","context":"create_flow","currentYaml":"","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	// No Accept: text/event-stream header → non-streaming mode

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp AIChatResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty Message in JSON response")
	}
}

// TestSendSSE_WireFormat verifies the raw wire format of sendSSE.
func TestSendSSE_WireFormat(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      interface{}
		wantFmt   string // Expected exact wire bytes
	}{
		{
			name:      "simple event",
			eventType: "chunk",
			data:      map[string]string{"content": "hi"},
			wantFmt:   "event: chunk\ndata: {\"content\":\"hi\"}\n\n",
		},
		{
			name:      "event with special chars in JSON",
			eventType: "complete",
			data:      AIChatResponse{Message: "line1\nline2\ttab", Action: "info"},
			wantFmt:   "event: complete\ndata: {\"message\":\"line1\\nline2\\ttab\",\"proposedYaml\":\"\",\"action\":\"info\"}\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			sendSSE(rr, nil, tt.eventType, tt.data)

			got := rr.Body.String()
			if got != tt.wantFmt {
				t.Errorf("wire format mismatch:\ngot:  %q\nwant: %q", got, tt.wantFmt)
			}
		})
	}
}

// TestSendSSE_LargePayload verifies sendSSE doesn't corrupt large payloads.
func TestSendSSE_LargePayload(t *testing.T) {
	// Build a large proposedYaml (~100KB)
	largeYAML := strings.Repeat("step: do_something\n", 5000)
	resp := AIChatResponse{
		Message:      "Here is your flow.",
		ProposedYAML: largeYAML,
		Action:       "apply_yaml",
	}

	rr := httptest.NewRecorder()
	sendSSE(rr, nil, "complete", resp)

	raw := rr.Body.String()

	// Must start with event line
	if !strings.HasPrefix(raw, "event: complete\ndata: ") {
		t.Fatal("missing event header")
	}
	// Must end with \n\n
	if !strings.HasSuffix(raw, "\n\n") {
		t.Fatal("missing trailing \\n\\n terminator")
	}

	// Extract the JSON between "data: " and the trailing "\n\n"
	dataStart := len("event: complete\ndata: ")
	dataEnd := len(raw) - 2 // trim trailing \n\n
	jsonStr := raw[dataStart:dataEnd]

	// Must be valid JSON
	var parsed AIChatResponse
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("JSON parse failed: %v (length=%d)", err, len(jsonStr))
	}

	// Must preserve the full YAML
	if parsed.ProposedYAML != largeYAML {
		t.Errorf("ProposedYAML mismatch: got %d bytes, want %d bytes", len(parsed.ProposedYAML), len(largeYAML))
	}
}

// --- Test Helpers ---

type sseEvent struct {
	Type string
	Data string
}

// parseSSEEvents parses a raw SSE response body into individual events.
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent

	// Split on double-newline (same logic the frontend uses)
	blocks := strings.Split(body, "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var eventType, data string
		// Use strings.Split instead of bufio.Scanner to avoid buffer size limits
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}

		if eventType != "" && data != "" {
			events = append(events, sseEvent{Type: eventType, Data: data})
		}
	}

	return events
}

// TestAIChatHandler_SSE_StreamingChunks verifies that streaming produces
// multiple chunk events followed by a complete event.
func TestAIChatHandler_SSE_StreamingChunks(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(
			StreamChunk("First "),
			StreamChunk("second "),
			StreamFinal("third."),
		), nil
	}

	body := `{"message":"test","context":"create_flow","currentYaml":"","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	events := parseSSEEvents(t, rr.Body.String())

	// Count chunk events
	chunkCount := 0
	for _, ev := range events {
		if ev.Type == "chunk" {
			chunkCount++
		}
	}

	if chunkCount < 3 {
		t.Errorf("expected at least 3 chunk events, got %d", chunkCount)
	}

	// Last event should be complete
	if len(events) == 0 {
		t.Fatal("no events")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.Type != "complete" {
		t.Errorf("last event should be 'complete', got %q", lastEvent.Type)
	}
}

// TestAIChatHandler_SSE_ModifyFlow verifies the modify_flow context
// correctly extracts YAML from LLM response.
func TestAIChatHandler_SSE_ModifyFlow(t *testing.T) {
	original := getProviderFn
	defer func() { getProviderFn = original }()

	responseYAML := "name: modified_flow\ndescription: A modified test flow\nnodes:\n  - name: step1\n    type: agent\n    model: gemini-2.0-flash\n    prompt: do stuff\n  - name: step2\n    type: agent\n    model: gemini-2.0-flash\n    prompt: do more stuff\n"
	llmResponse := "Here is the modified flow:\n```yaml\n" + responseYAML + "```\n"

	getProviderFn = func(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
		return NewMockLLM(
			TextTurn(llmResponse),
			TextTurn(llmResponse),
			TextTurn(llmResponse),
		), nil
	}

	reqBody := `{"message":"add a step","context":"modify_flow","currentYaml":"name: test\ndescription: test flow\nnodes:\n  - name: step1\n    type: agent\n    model: gemini-2.0-flash\n    prompt: do stuff\n","selectedNodes":[],"history":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	events := parseSSEEvents(t, rr.Body.String())

	var completeEvent *sseEvent
	for i := range events {
		if events[i].Type == "complete" {
			completeEvent = &events[i]
			break
		}
	}

	if completeEvent == nil {
		t.Fatal("no complete event found")
	}

	var resp AIChatResponse
	if err := json.Unmarshal([]byte(completeEvent.Data), &resp); err != nil {
		t.Fatalf("failed to parse complete event: %v", err)
	}

	if resp.ProposedYAML == "" {
		t.Error("expected non-empty ProposedYAML for modify_flow response")
	}

	// Verify the extracted YAML matches what was in the code fence
	if !strings.Contains(resp.ProposedYAML, "modified_flow") {
		t.Errorf("ProposedYAML should contain 'modified_flow', got: %s", resp.ProposedYAML[:min(100, len(resp.ProposedYAML))])
	}
}

// TestAIChatHandler_InvalidRequest verifies error handling for bad request bodies.
func TestAIChatHandler_InvalidRequest(t *testing.T) {
	body := "not valid json"
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	rr := httptest.NewRecorder()
	AIChatHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
