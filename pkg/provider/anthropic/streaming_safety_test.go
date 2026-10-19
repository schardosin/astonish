package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/provider/streaming"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func safetySSE(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

const safetyToolStart = `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"lookup","input":{}}}`
const safetyToolStop = `{"type":"content_block_stop","index":0}`
const safetyStop = `{"type":"message_stop"}`

func safetyArgs(value string) string {
	data, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": value}})
	return string(data)
}

func TestStreamingSafetyRejectsUncommittedTools(t *testing.T) {
	for name, events := range map[string][]string{
		"eof":              {safetyToolStart, safetyArgs(`{}`), safetyToolStop},
		"malformed_event":  {safetyToolStart, safetyArgs(`{}`), safetyToolStop, `{`, safetyStop},
		"malformed_args":   {safetyToolStart, safetyArgs(`{"x":`), safetyToolStop, safetyStop},
		"array_args":       {safetyToolStart, safetyArgs(`[]`), safetyToolStop, safetyStop},
		"null_args":        {safetyToolStart, safetyArgs(`null`), safetyToolStop, safetyStop},
		"unfinished":       {safetyToolStart, safetyStop},
		"max_tokens":       {safetyToolStart, safetyArgs(`{}`), safetyToolStop, `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`, safetyStop},
		"missing_identity": {strings.Replace(safetyToolStart, `"id":"t1"`, `"id":""`, 1), safetyToolStop, safetyStop},
		"bad_input":        {strings.Replace(safetyToolStart, `"input":{}`, `"input":null`, 1), safetyToolStop, safetyStop},
		"wrong_index":      {safetyToolStart, strings.Replace(safetyArgs(`{}`), `"index":0`, `"index":1`, 1), safetyToolStop, safetyStop},
		"api_error":        {safetyToolStart, safetyToolStop, `{"type":"error","error":{"message":"private payload"}}`, safetyStop},
	} {
		t.Run(name, func(t *testing.T) {
			count, failures := 0, 0
			NewProvider("", "").handleStream(strings.NewReader(safetySSE(events...)), func(resp *model.LLMResponse, err error) bool {
				if err != nil {
					failures++
					if strings.Contains(err.Error(), "private payload") {
						t.Fatal("leaked payload")
					}
					return true
				}
				if resp != nil && !resp.Partial {
					count++
				}
				return true
			})
			if failures != 1 || count != 0 {
				t.Fatalf("failures=%d aggregates=%d", failures, count)
			}
		})
	}
}

func TestStreamingSafetyValidToolsOnce(t *testing.T) {
	for _, args := range []string{`{}`, `{"q":"hello"}`} {
		t.Run(args, func(t *testing.T) {
			calls := 0
			NewProvider("", "").handleStream(strings.NewReader(safetySSE(safetyToolStart, safetyArgs(args), safetyToolStop, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`, safetyStop)), func(resp *model.LLMResponse, err error) bool {
				if err != nil {
					t.Fatal(err)
				}
				calls++
				if resp.Partial || len(resp.Content.Parts) != 1 || resp.Content.Parts[0].FunctionCall == nil || resp.UsageMetadata.CandidatesTokenCount != 2 {
					t.Fatalf("bad response: %+v", resp)
				}
				return false
			})
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

type safetyBrokenReader struct{}

func (safetyBrokenReader) Read([]byte) (int, error) { return 0, errors.New("broken read") }
func TestStreamingSafetyReadError(t *testing.T) {
	failures := 0
	NewProvider("", "").handleStream(io.MultiReader(strings.NewReader(safetySSE(safetyToolStart, safetyToolStop)), safetyBrokenReader{}), func(resp *model.LLMResponse, err error) bool {
		if err == nil {
			t.Fatal("unexpected response")
		}
		failures++
		return true
	})
	if failures != 1 {
		t.Fatalf("failures=%d", failures)
	}
}

func TestStreamingSafetyYieldFalse(t *testing.T) {
	calls := 0
	NewProvider("", "").handleStream(strings.NewReader(safetySSE(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`, `{`)), func(resp *model.LLMResponse, err error) bool {
		calls++
		if err != nil || !resp.Partial {
			t.Fatalf("first yield = %v %v", resp, err)
		}
		return false
	})
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

type safetyTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (s safetyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = s.target.Scheme
	req.URL.Host = s.target.Host
	return s.base.RoundTrip(req)
}

func TestStreamingSafetyHeldOpenHTTP(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, safetySSE(`{"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":3}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
		for i := 0; i < 100; i++ {
			fmt.Fprint(w, safetySSE(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private"}}`))
		}
		fmt.Fprint(w, safetySSE(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed"}}`, safetyToolStop, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"hello"}}`, `{"type":"content_block_stop","index":1}`, strings.Replace(safetyToolStart, `"index":0`, `"index":2`, 1), strings.Replace(safetyArgs(`{}`), `"index":0`, `"index":2`, 1), `{"type":"content_block_stop","index":2}`))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, safetySSE(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`, safetyStop))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	progress := make(chan streaming.Progress, 128)
	responses := make(chan *model.LLMResponse, 8)
	done := make(chan error, 1)
	ctx = streaming.WithProgress(ctx, func(p streaming.Progress) { progress <- p })
	p := NewProvider("test", "test")
	target, _ := url.Parse(server.URL)
	p.client = &http.Client{Transport: safetyTransport{target: target, base: server.Client().Transport}}
	go func() {
		for resp, err := range p.GenerateContent(ctx, &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}}}, true) {
			if err != nil {
				done <- err
				return
			}
			assertNoThinkingPayload(t, resp)
			responses <- resp
		}
		done <- nil
	}()
	for _, want := range []streaming.Phase{streaming.PhaseThinking, streaming.PhaseToolArguments} {
		select {
		case got := <-progress:
			if got.Phase != want {
				t.Fatalf("phase %v want %v", got, want)
			}
		case <-ctx.Done():
			t.Fatal("no progress before completion")
		}
	}
	select {
	case resp := <-responses:
		if !resp.Partial || resp.Content.Parts[0].Text != "hello" {
			t.Fatalf("bad partial %+v", resp)
		}
	case <-ctx.Done():
		t.Fatal("no partial before completion")
	}
	select {
	case resp := <-responses:
		t.Fatalf("early aggregate %+v", resp)
	case err := <-done:
		t.Fatalf("early completion %v", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("completion timed out")
	}
	if len(progress) != 0 {
		t.Fatalf("unbounded progress: %d", len(progress))
	}
	final := <-responses
	if final.Partial || final.UsageMetadata.PromptTokenCount != 13 || final.UsageMetadata.CandidatesTokenCount != 5 || len(final.Content.Parts) != 2 {
		t.Fatalf("bad aggregate %+v", final)
	}
	if final.Content.Parts[0].Text != "hello" || final.Content.Parts[1].FunctionCall == nil || len(responses) != 0 {
		t.Fatal("text and tools not delivered exactly once")
	}
}

func assertNoThinkingPayload(t *testing.T, resp *model.LLMResponse) {
	t.Helper()
	if resp.Content != nil {
		for _, part := range resp.Content.Parts {
			if part.Thought || len(part.ThoughtSignature) != 0 {
				t.Error("native thinking part or signature yielded")
			}
		}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Error(err)
		return
	}
	for _, secret := range []string{"private", "signed", "opaque-redacted"} {
		if strings.Contains(string(encoded), secret) {
			t.Error("private thinking payload yielded")
		}
	}
}

func TestStreamingSafetyThinkingPayloadPrivate(t *testing.T) {
	for _, block := range []string{
		`{"type":"thinking","thinking":"private-start","signature":"signed-start"}`,
		`{"type":"redacted_thinking","data":"opaque-redacted"}`,
	} {
		phases, responses := 0, 0
		ctx := streaming.WithProgress(context.Background(), func(p streaming.Progress) {
			phases++
			if p.Phase != streaming.PhaseThinking {
				t.Errorf("unexpected progress: %v", p.Phase)
			}
		})
		wire := safetySSE(`{"type":"content_block_start","index":0,"content_block":`+block+`}`, safetyToolStop,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"hello"}}`,
			`{"type":"content_block_stop","index":1}`, safetyStop)
		NewProvider("", "").handleStreamWithContext(ctx, strings.NewReader(wire), func(resp *model.LLMResponse, err error) bool {
			if err != nil {
				t.Fatal(err)
			}
			responses++
			assertNoThinkingPayload(t, resp)
			if len(resp.Content.Parts) != 1 || resp.Content.Parts[0].Text != "hello" {
				t.Error("expected only visible answer")
			}
			return true
		})
		if phases != 1 || responses != 2 {
			t.Fatalf("phases=%d responses=%d", phases, responses)
		}
	}
}

func TestAnthropicRequestDoesNotReplayThinking(t *testing.T) {
	// Signed-thinking round trips were unsupported before streaming progress.
	// Do not introduce replay of history whose text may have been redacted.
	request, err := NewProvider("", "").toAnthropicRequest(&model.LLMRequest{Contents: []*genai.Content{{
		Role: "model", Parts: []*genai.Part{
			{Thought: true, Text: "private", ThoughtSignature: []byte("signed")},
			{Thought: true, Text: "private-unsigned"},
			{Text: "hello"},
		},
	}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 1 || len(request.Messages[0].Content) != 1 || request.Messages[0].Content[0].Type != "text" || request.Messages[0].Content[0].Text != "hello" {
		t.Fatal("request must include only the visible answer")
	}
}

func TestStreamingSafetyLargeTextAndTerminalWithoutNewline(t *testing.T) {
	text := strings.Repeat("x", 100000)
	start, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": text}})
	wire := strings.TrimRight(safetySSE(string(start), safetyToolStop, safetyStop), "\n")
	partials, finals := 0, 0
	NewProvider("", "").handleStream(strings.NewReader(wire), func(resp *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Content.Parts[0].Text != text {
			t.Fatal("text changed")
		}
		if resp.Partial {
			partials++
		} else {
			finals++
		}
		return true
	})
	if partials != 1 || finals != 1 {
		t.Fatalf("partials=%d finals=%d", partials, finals)
	}
}

func TestStreamingSafetyMultipleToolsAndInitialInput(t *testing.T) {
	second := strings.ReplaceAll(strings.Replace(safetyToolStart, `"index":0`, `"index":1`, 1), `"t1"`, `"t2"`)
	second = strings.Replace(second, `"input":{}`, `"input":{"n":2}`, 1)
	calls := 0
	NewProvider("", "").handleStream(strings.NewReader(safetySSE(safetyToolStart, safetyToolStop, second, `{"type":"content_block_stop","index":1}`, safetyStop)), func(resp *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		calls++
		if len(resp.Content.Parts) != 2 || resp.Content.Parts[0].FunctionCall.ID != "t1" || resp.Content.Parts[1].FunctionCall.Args["n"] != float64(2) {
			t.Fatalf("bad tools: %+v", resp)
		}
		return false
	})
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestStreamingSafetyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	NewProvider("", "").handleStreamWithContext(ctx, strings.NewReader(safetySSE(safetyToolStart, safetyToolStop, safetyStop)), func(resp *model.LLMResponse, err error) bool {
		calls++
		if resp != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("response=%v err=%v", resp, err)
		}
		return false
	})
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
