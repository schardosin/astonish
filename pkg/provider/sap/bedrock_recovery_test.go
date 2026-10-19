package sap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/provider/llmerror"
	"golang.org/x/net/http2"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Exercise a real HTTP/2 RST_STREAM after flushed output, not a mocked reader.
func TestBedrockStreamHTTP2Continuation(t *testing.T) {
	var attempts atomic.Int32
	var payloads []map[string]json.RawMessage
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("wanted HTTP/2, got %s", r.Proto)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = io.WriteString(w, sapEvents(sapText, sapTool, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"lost\":"}}`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_, _ = io.WriteString(w, sapEvents(`{"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}`, sapTool, sapToolStop, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`, sapStop))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "finish the task"}}}}, Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "system unchanged"}}}}}
	before, _ := json.Marshal(request)
	var text strings.Builder
	var final *model.LLMResponse
	tools, finals := 0, 0
	for response, err := range testStreamProvider(server.Client(), server.URL).GenerateContent(ctx, request, true) {
		if err != nil {
			t.Fatal(err)
		}
		if response.Partial {
			for _, part := range response.Content.Parts {
				text.WriteString(part.Text)
			}
		} else {
			final = response
			finals++
			for _, part := range response.Content.Parts {
				if part.FunctionCall != nil {
					tools++
				}
			}
		}
	}
	after, _ := json.Marshal(request)
	if string(before) != string(after) {
		t.Fatal("caller request mutated")
	}
	if attempts.Load() != 2 || text.String() != "hello world" || tools != 1 || finals != 1 {
		t.Fatalf("attempts=%d text=%q tools=%d finals=%d", attempts.Load(), text.String(), tools, finals)
	}
	var aggregate strings.Builder
	for _, part := range final.Content.Parts {
		aggregate.WriteString(part.Text)
	}
	if aggregate.String() != text.String() {
		t.Fatalf("restore text=%q live=%q", aggregate.String(), text.String())
	}
	if final.UsageMetadata == nil || final.UsageMetadata.CandidatesTokenCount != 7 {
		t.Fatal("successful attempt usage lost")
	}
	if string(payloads[0]["system"]) != string(payloads[1]["system"]) {
		t.Fatal("system changed")
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(payloads[1]["messages"], &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[1].Role != "assistant" || string(messages[1].Content) != `"hello"` || messages[2].Role != "user" {
		t.Fatalf("not a continuation: %s", payloads[1]["messages"])
	}
	if strings.Contains(string(payloads[1]["messages"]), "lost") {
		t.Fatal("incomplete tool replayed")
	}
}

func TestBedrockContinuationPayloadRejectsNullHistory(t *testing.T) {
	for _, original := range []string{`null`, `{}`, `{"messages":null}`, `{"messages":{}}`, `{"messages":"history"}`} {
		t.Run(original, func(t *testing.T) {
			payload, err := bedrockContinuationPayload([]byte(original), "already delivered")
			if err == nil || payload != nil {
				t.Fatalf("expected rejection without a replacement payload: payload=%s err=%v", payload, err)
			}
		})
	}
}

func TestBedrockContinuationPayloadPreservesHistory(t *testing.T) {
	original := []byte(`{"anthropic_version":"bedrock-2023-05-31","max_tokens":1234,"system":"unchanged","temperature":0.2,"tools":[{"name":"run"}],"extra":{"future":true},"messages":[{"role":"user","content":"task"},{"role":"assistant","content":[{"type":"tool_use","id":"completed","name":"run","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"completed","content":"already done"}]}]}`)
	got, err := bedrockContinuationPayload(original, "partial 🧪\ntext")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(original, &before)
	_ = json.Unmarshal(got, &after)
	for key, value := range before {
		if key != "messages" && string(after[key]) != string(value) {
			t.Fatalf("changed %s", key)
		}
	}
	var oldMessages, newMessages []json.RawMessage
	_ = json.Unmarshal(before["messages"], &oldMessages)
	_ = json.Unmarshal(after["messages"], &newMessages)
	if len(newMessages) != len(oldMessages)+2 {
		t.Fatal("incorrect history length")
	}
	for i, value := range oldMessages {
		if string(value) != string(newMessages[i]) {
			t.Fatal("completed tool history changed")
		}
	}
	var textMessage struct{ Content string }
	_ = json.Unmarshal(newMessages[3], &textMessage)
	if textMessage.Content != "partial 🧪\ntext" {
		t.Fatal("text altered")
	}
	for _, bad := range []string{`bad`, `{}`, `{"messages":{}}`} {
		if _, err := bedrockContinuationPayload([]byte(bad), "text"); err == nil {
			t.Fatalf("accepted malformed payload %s", bad)
		}
	}
}

func TestBedrockStreamContinuationTransitions(t *testing.T) {
	reset := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	for _, tc := range []struct {
		name           string
		secondText     bool
		terminalStatus int
		cancel         bool
	}{
		{name: "reset before continuation output"},
		{name: "reset after more text", secondText: true},
		{name: "continuation rate limit is not outer replay", terminalStatus: 429},
		{name: "cancel continuation", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attempts := 0
			var payloads []string
			client := &http.Client{Transport: streamRoundTrip(func(r *http.Request) (*http.Response, error) {
				attempts++
				data, _ := io.ReadAll(r.Body)
				payloads = append(payloads, string(data))
				if attempts == 2 && tc.terminalStatus != 0 {
					return &http.Response{StatusCode: tc.terminalStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("overloaded"))}, nil
				}
				if attempts == 2 && tc.cancel {
					cancel()
					return nil, ctx.Err()
				}
				input := sapEvents(sapText)
				if attempts == 2 && !tc.secondText {
					input = ""
				}
				var reader io.Reader = io.MultiReader(strings.NewReader(input), streamReadFailure{reset})
				if attempts == 3 {
					reader = strings.NewReader(sapEvents(`{"type":"content_block_delta","delta":{"type":"text_delta","text":" done"}}`, sapStop))
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(reader)}, nil
			})}
			var live, aggregate strings.Builder
			var gotErr error
			for response, err := range testStreamProvider(client, "http://provider.test").GenerateContent(ctx, &model.LLMRequest{}, true) {
				if err != nil {
					gotErr = err
					continue
				}
				if response != nil && response.Content != nil {
					for _, part := range response.Content.Parts {
						if response.Partial {
							live.WriteString(part.Text)
						} else {
							aggregate.WriteString(part.Text)
						}
					}
				}
			}
			if tc.terminalStatus != 0 || tc.cancel {
				var interrupted *llmerror.InterruptedStreamError
				if attempts != 2 || !errors.As(gotErr, &interrupted) || llmerror.IsRetryable(gotErr) || aggregate.Len() != 0 {
					t.Fatalf("attempts=%d err=%v aggregate=%q", attempts, gotErr, aggregate.String())
				}
				if tc.cancel && !errors.Is(gotErr, context.Canceled) {
					t.Fatalf("lost cancel: %v", gotErr)
				}
				return
			}
			want := "hello done"
			prefix := "hello"
			if tc.secondText {
				want = "hellohello done"
				prefix = "hellohello"
			}
			if attempts != 3 || gotErr != nil || live.String() != want || aggregate.String() != want {
				t.Fatalf("attempts=%d err=%v live=%q final=%q", attempts, gotErr, live.String(), aggregate.String())
			}
			var request struct {
				Messages []struct{ Role, Content string }
			}
			if err := json.Unmarshal([]byte(payloads[2]), &request); err != nil {
				t.Fatal(err)
			}
			if len(request.Messages) != 2 || request.Messages[0].Content != prefix {
				t.Fatalf("accumulated continuation wrong: %s", payloads[2])
			}
		})
	}
}

func TestBedrockStreamContinuationBounded(t *testing.T) {
	attempts := 0
	reset := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	client := &http.Client{Transport: streamRoundTrip(func(r *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(io.MultiReader(strings.NewReader(sapEvents(sapText)), streamReadFailure{reset}))}, nil
	})}
	var finalErr error
	finals := 0
	for response, err := range testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
		if err != nil {
			finalErr = err
		}
		if response != nil && !response.Partial {
			finals++
		}
	}
	var interrupted *llmerror.InterruptedStreamError
	if attempts != 3 || finals != 0 || !errors.As(finalErr, &interrupted) || llmerror.IsRetryable(finalErr) {
		t.Fatalf("attempts=%d finals=%d error=%v", attempts, finals, finalErr)
	}
}
