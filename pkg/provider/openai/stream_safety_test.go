package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/provider/llmerror"
	"github.com/SAP/astonish/pkg/provider/streaming"
	openailib "github.com/sashabaranov/go-openai"
	"google.golang.org/adk/model"
)

type safetyTransport func(*http.Request) (*http.Response, error)

func (f safetyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type safetyBody struct {
	io.Reader
	closed bool
}

func (b *safetyBody) Close() error { b.closed = true; return nil }
func safetyProvider(body string, responses bool) (*Provider, *safetyBody) {
	b := &safetyBody{Reader: strings.NewReader(body)}
	cfg := openailib.DefaultConfig("test")
	cfg.HTTPClient = &http.Client{Transport: safetyTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: b}, nil
	})}
	p := NewProvider(openailib.NewClientWithConfig(cfg), "test", false)
	p.useResponsesAPI = responses
	return p, b
}
func safetyEvent(v any) string { b, _ := json.Marshal(v); return "data: " + string(b) + "\n\n" }
func safetyChat(delta map[string]any, finish string) string {
	return safetyEvent(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
}
func safetyTool(index int, id, name, args string) map[string]any {
	return map[string]any{"index": index, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}
}
func safetyCollect(p *Provider, ctx context.Context) (responses []*model.LLMResponse, errs []error) {
	for r, e := range p.GenerateContent(ctx, &model.LLMRequest{}, true) {
		if e != nil {
			errs = append(errs, e)
		} else {
			responses = append(responses, r)
		}
	}
	return
}
func safetyCalls(rs []*model.LLMResponse) int {
	n := 0
	for _, r := range rs {
		if r.Content != nil {
			for _, p := range r.Content.Parts {
				if p.FunctionCall != nil {
					n++
				}
			}
		}
	}
	return n
}

func TestOpenAIStreamSafetyInvalid(t *testing.T) {
	for _, tt := range []struct{ name, finish, args, id, tool string }{
		{"tool_only_eof", "", "{}", "call", "lookup"},
		{"length", "length", "{}", "call", "lookup"},
		{"max_tokens", "max_tokens", "{}", "call", "lookup"},
		{"filtered", "content_filter", "{}", "call", "lookup"},
		{"malformed", "tool_calls", "{", "call", "lookup"},
		{"null", "tool_calls", "null", "call", "lookup"},
		{"array", "tool_calls", "[]", "call", "lookup"},
		{"empty", "tool_calls", "", "call", "lookup"},
		{"missing_id", "tool_calls", "{}", "", "lookup"},
		{"missing_name", "tool_calls", "{}", "call", " "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, b := safetyProvider(safetyChat(map[string]any{"tool_calls": []any{safetyTool(0, tt.id, tt.tool, tt.args)}}, tt.finish)+"data: [DONE]\n\n", false)
			rs, es := safetyCollect(p, context.Background())
			if len(es) != 1 || safetyCalls(rs) != 0 {
				t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
			}
			if !b.closed {
				t.Fatal("body not closed")
			}
		})
	}
}
func TestOpenAIStreamSafetyProgressAndUsage(t *testing.T) {
	body := safetyChat(map[string]any{"reasoning_content": "private reasoning"}, "") + safetyChat(map[string]any{"reasoning_content": " more private"}, "")
	body += safetyChat(map[string]any{"tool_calls": []any{safetyTool(1, "b", "second", "{"), safetyTool(0, "a", "first", "{}")}}, "")
	body += safetyChat(map[string]any{"tool_calls": []any{safetyTool(1, "", "", "}")}}, "tool_calls")
	body += safetyEvent(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}) + "data: [DONE]\n\n"
	p, _ := safetyProvider(body, false)
	var phases []streaming.Phase
	rs, es := safetyCollect(p, streaming.WithProgress(context.Background(), func(p streaming.Progress) { phases = append(phases, p.Phase) }))
	if len(es) != 0 || safetyCalls(rs) != 2 {
		t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
	}
	if fmt.Sprint(phases) != "[thinking tool_arguments]" {
		t.Errorf("phases=%v", phases)
	}
	final := rs[len(rs)-1]
	if final.UsageMetadata == nil || final.UsageMetadata.TotalTokenCount != 15 {
		t.Errorf("usage=%v", final.UsageMetadata)
	}
	var ids []string
	for _, r := range rs {
		for _, part := range r.Content.Parts {
			if part.FunctionCall != nil {
				ids = append(ids, part.FunctionCall.ID)
			}
		}
	}
	if fmt.Sprint(ids) != "[a b]" {
		t.Errorf("ids=%v", ids)
	}
}
func TestOpenAIStreamSafetyYieldFalse(t *testing.T) {
	// Stop on the persisted text at an incomplete EOF, not the initial partial.
	p, b := safetyProvider(safetyChat(map[string]any{"content": "partial"}, "")+"data: [DONE]\n\n", false)
	n := 0
	p.GenerateContent(context.Background(), &model.LLMRequest{}, true)(func(r *model.LLMResponse, e error) bool { n++; return n < 2 })
	if n != 2 || !b.closed {
		t.Fatalf("yields=%d closed=%v", n, b.closed)
	}
}
func TestResponsesStreamSafetyInvalid(t *testing.T) {
	for _, tt := range []struct{ name, args, id, tool string }{
		{"null", "null", "call", "lookup"}, {"empty", "", "call", "lookup"}, {"malformed", "{", "call", "lookup"}, {"missing_id", "{}", "", "lookup"}, {"missing_name", "{}", "call", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := safetyEvent(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{map[string]any{"type": "function_call", "call_id": tt.id, "name": tt.tool, "arguments": tt.args}}}})
			p, _ := safetyProvider(body, true)
			rs, es := safetyCollect(p, context.Background())
			if len(es) != 1 || safetyCalls(rs) != 0 {
				t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
			}
		})
	}
}
func TestOpenAIStreamSafetyTerminalAndBatch(t *testing.T) {
	tool := safetyChat(map[string]any{"tool_calls": []any{safetyTool(0, "a", "lookup", "{}")}}, "")
	for name, body := range map[string]string{
		"empty_eof":        "",
		"reasoning_eof":    safetyChat(map[string]any{"reasoning_content": "private"}, ""),
		"no_tool":          safetyChat(nil, "tool_calls"),
		"missing_index":    safetyChat(map[string]any{"tool_calls": []any{map[string]any{"id": "a", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}, "tool_calls"),
		"invalid_second":   tool + safetyChat(map[string]any{"tool_calls": []any{safetyTool(1, "b", "lookup", "null")}}, "tool_calls"),
		"identity_changed": tool + safetyChat(map[string]any{"tool_calls": []any{safetyTool(0, "b", "", "")}}, "tool_calls"),
		"late_length":      tool + safetyChat(nil, "tool_calls") + safetyChat(nil, "length"),
		"late_delta":       tool + safetyChat(nil, "tool_calls") + safetyChat(map[string]any{"content": "late"}, ""),
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := safetyProvider(body, false)
			rs, es := safetyCollect(p, context.Background())
			if len(es) != 1 || safetyCalls(rs) != 0 {
				t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
			}
		})
	}
	p, _ := safetyProvider(tool+safetyChat(map[string]any{"content": "preamble"}, "stop"), false)
	rs, es := safetyCollect(p, context.Background())
	if len(es) != 0 || safetyCalls(rs) != 1 || rs[len(rs)-1].Content.Parts[0].Text != "preamble" {
		t.Fatalf("responses=%v errors=%v", rs, es)
	}
}

func TestResponsesStreamSafetyTerminal(t *testing.T) {
	for _, event := range []string{"response.failed", "response.incomplete", "error", "response.completed"} {
		t.Run(event, func(t *testing.T) {
			p, _ := safetyProvider(safetyEvent(map[string]any{"type": event, "response": map[string]any{"status": "incomplete", "output": []any{map[string]any{"type": "function_call", "call_id": "a", "name": "lookup", "arguments": "{}"}}}}), true)
			rs, es := safetyCollect(p, context.Background())
			if len(es) != 1 || safetyCalls(rs) != 0 {
				t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
			}
		})
	}
}

func TestOpenAIStreamSafetyPostOutputError(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, output := range []bool{false, true} {
			t.Run(fmt.Sprintf("responses=%v/output=%v", responses, output), func(t *testing.T) {
				body := ""
				if output {
					if responses {
						body = safetyEvent(map[string]any{"type": "response.output_text.delta", "delta": "hello"})
					} else {
						body = safetyChat(map[string]any{"content": "hello"}, "")
					}
				}
				p, b := safetyProvider("", responses)
				cause := llmerror.NewLLMError("openai", 503, "unavailable", "")
				b.Reader = io.MultiReader(strings.NewReader(body), safetyFailure{cause})
				rs, es := safetyCollect(p, context.Background())
				if len(es) != 1 || safetyCalls(rs) != 0 {
					t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
				}
				var marker *llmerror.InterruptedStreamError
				if errors.As(es[0], &marker) != output || llmerror.IsRetryable(es[0]) == output || !errors.Is(es[0], cause) {
					t.Fatalf("classification=%v output=%v", es[0], output)
				}
				if !b.closed {
					t.Fatal("body not closed")
				}
			})
		}
	}
}

type safetyFailure struct{ err error }

func (f safetyFailure) Read([]byte) (int, error) { return 0, f.err }

func TestOpenAIStreamSafetyCancelYield(t *testing.T) {
	for _, responses := range []bool{false, true} {
		t.Run(fmt.Sprint(responses), func(t *testing.T) {
			body := safetyChat(map[string]any{"reasoning_content": "private"}, "") + safetyChat(map[string]any{"tool_calls": []any{safetyTool(0, "a", "lookup", "{}")}}, "tool_calls")
			if responses {
				body = safetyEvent(map[string]any{"type": "response.reasoning_text.delta", "delta": "private"}) + safetyEvent(map[string]any{"type": "response.function_call_arguments.delta", "delta": "{}"})
			}
			p, b := safetyProvider(body, responses)
			phases := 0
			ctx := streaming.WithProgress(context.Background(), func(streaming.Progress) { phases++ })
			yields := 0
			p.GenerateContent(ctx, &model.LLMRequest{}, true)(func(*model.LLMResponse, error) bool { yields++; return false })
			if yields != 1 || phases != 1 || !b.closed {
				t.Fatalf("yields=%d phases=%d closed=%v", yields, phases, b.closed)
			}
		})
	}
}

func TestResponsesStreamSafetyProgress(t *testing.T) {
	body := ""
	for i := 0; i < 3; i++ {
		body += safetyEvent(map[string]any{"type": "response.reasoning_text.delta", "delta": "secret"})
	}
	for i := 0; i < 3; i++ {
		body += safetyEvent(map[string]any{"type": "response.function_call_arguments.delta", "delta": "secret"})
	}
	body += safetyEvent(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{map[string]any{"type": "function_call", "call_id": "a", "name": "lookup", "arguments": "{}"}}}})
	p, _ := safetyProvider(body, true)
	var phases []streaming.Phase
	rs, es := safetyCollect(p, streaming.WithProgress(context.Background(), func(p streaming.Progress) { phases = append(phases, p.Phase) }))
	if len(es) != 0 || safetyCalls(rs) != 1 {
		t.Fatalf("errors=%v tools=%d", es, safetyCalls(rs))
	}
	if fmt.Sprint(phases) != "[thinking tool_arguments]" {
		t.Fatalf("phases=%v", phases)
	}
}
