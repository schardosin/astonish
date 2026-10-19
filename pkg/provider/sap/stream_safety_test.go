package sap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/provider/llmerror"
	"github.com/SAP/astonish/pkg/provider/streaming"
	"golang.org/x/net/http2"
	"google.golang.org/adk/model"
)

type streamRoundTrip func(*http.Request) (*http.Response, error)

func (f streamRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type streamReadFailure struct{ err error }

func (r streamReadFailure) Read([]byte) (int, error) { return 0, r.err }
func sapEvents(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

const sapText = `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`
const sapStop = `{"type":"message_stop"}`
const sapThinking = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"private"}}`
const sapTool = `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"id","name":"run","input":{}}}`
const sapToolStop = `{"type":"content_block_stop","index":1}`

func testStreamProvider(client *http.Client, url string) *Provider {
	return &Provider{httpClient: client, baseURL: url, deploymentID: "test", modelName: "anthropic--claude-4", authConfig: &sapTransport{resourceGroup: "test"}}
}

type closeObservedBody struct {
	io.Reader
	closed bool
}

func (b *closeObservedBody) Close() error { b.closed = true; return nil }

func TestBedrockStreamYieldFalseClosesWithoutReplay(t *testing.T) {
	body := &closeObservedBody{Reader: io.MultiReader(strings.NewReader(sapEvents(sapText)), streamReadFailure{http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}})}
	attempts, calls := 0, 0
	client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	})}
	testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true)(func(*model.LLMResponse, error) bool { calls++; return false })
	if attempts != 1 || calls != 1 || !body.closed {
		t.Fatalf("attempts=%d calls=%d closed=%v", attempts, calls, body.closed)
	}
}

func TestBedrockStreamResetRecovery(t *testing.T) {
	reset := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	for _, tc := range []struct {
		name, prefix            string
		resetAlways             bool
		wantAttempts, wantParts int
		wantError               bool
	}{
		{"before output", "", false, 2, 1, false},
		{"progress and usage only", sapEvents(sapThinking, `{"type":"message_start","message":{"usage":{"input_tokens":4}}}`), false, 2, 1, false},
		{"buffered tool", sapEvents(sapTool, sapToolStop), false, 2, 1, false},
		{"after output", sapEvents(sapText), false, 1, 1, true},
		{"bounded", "", true, 3, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, partials := 0, 0
			client := &http.Client{Transport: streamRoundTrip(func(r *http.Request) (*http.Response, error) {
				attempts++
				body := io.Reader(strings.NewReader(sapEvents(sapText, sapStop)))
				if attempts == 1 || tc.resetAlways {
					body = io.MultiReader(strings.NewReader(tc.prefix), streamReadFailure{reset})
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: make(http.Header)}, nil
			})}
			var gotErr error
			for resp, err := range testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
				if err != nil {
					gotErr = err
				}
				if resp != nil && resp.Partial {
					partials++
				}
			}
			if attempts != tc.wantAttempts || partials != tc.wantParts || (gotErr != nil) != tc.wantError {
				t.Fatalf("attempts=%d partials=%d err=%v", attempts, partials, gotErr)
			}
			if tc.wantError {
				var cause http2.StreamError
				if !errors.As(gotErr, &cause) {
					t.Fatalf("lost cause: %v", gotErr)
				}
				if partials > 0 && !strings.Contains(gotErr.Error(), "interrupted") {
					t.Fatalf("not marked interrupted: %v", gotErr)
				}
			}
		})
	}
}

func TestBedrockStreamPostOutputNeverReplay(t *testing.T) {
	for _, cause := range []error{
		llmerror.NewLLMError("sap-bedrock", http.StatusServiceUnavailable, "overloaded", ""),
		llmerror.NewLLMError("sap-bedrock", http.StatusBadRequest, "context length exceeded", ""),
		context.Canceled,
		context.DeadlineExceeded,
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			attempts, partials, failures := 0, 0, 0
			client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
				attempts++
				body := io.MultiReader(strings.NewReader(sapEvents(sapText)), streamReadFailure{cause})
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body)}, nil
			})}
			var gotErr error
			for resp, err := range testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
				if err != nil {
					gotErr = err
					failures++
				}
				if resp != nil && resp.Partial {
					partials++
				}
			}
			if attempts != 1 || partials != 1 || failures != 1 {
				t.Fatalf("attempts=%d partials=%d failures=%d", attempts, partials, failures)
			}
			if !errors.Is(gotErr, cause) {
				t.Fatalf("lost cause: %v", gotErr)
			}
			var interrupted *llmerror.InterruptedStreamError
			if !errors.As(gotErr, &interrupted) {
				t.Fatalf("missing terminal interruption marker: %v", gotErr)
			}
			if llmerror.IsRetryable(gotErr) || llmerror.IsContextOverflow(gotErr) {
				t.Fatalf("post-output interruption permits outer replay: %v", gotErr)
			}
		})
	}
}

func TestBedrockStreamMalformedToolsNeverReplay(t *testing.T) {
	for _, input := range []string{
		sapEvents(sapTool),
		sapEvents(sapTool, sapToolStop),
		sapEvents(sapTool, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{"}}`, sapToolStop, sapStop),
		sapEvents(`{"type":"error","error":{"type":"overloaded_error"}}`, sapStop),
	} {
		attempts, tools := 0, 0
		client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(input))}, nil
		})}
		var gotErr error
		for resp, err := range testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
			if err != nil {
				gotErr = err
			}
			if resp != nil && resp.Content != nil {
				for _, part := range resp.Content.Parts {
					if part.FunctionCall != nil {
						tools++
					}
				}
			}
		}
		if gotErr == nil || attempts != 1 || tools != 0 {
			t.Fatalf("attempts=%d tools=%d err=%v", attempts, tools, gotErr)
		}
	}
}

func TestBedrockStreamRequestResetAndToolOnce(t *testing.T) {
	attempts, tools := 0, 0
	client := &http.Client{Transport: streamRoundTrip(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("stream error: stream ID 3; INTERNAL_ERROR; received from peer")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(sapEvents(sapTool, sapToolStop, sapStop)))}, nil
	})}
	for resp, err := range testStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range resp.Content.Parts {
			if part.FunctionCall != nil {
				tools++
			}
		}
	}
	if attempts != 2 || tools != 1 {
		t.Fatalf("attempts=%d tools=%d", attempts, tools)
	}
}

func TestBedrockStreamResetClassification(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded, errors.New("connection reset by peer"), errors.New("provider: stream error: stream ID 1; INTERNAL_ERROR"), http2.StreamError{Code: http2.ErrCodeProtocol}, http2.StreamError{Code: http2.ErrCodeCancel}} {
		if isTransientStreamReset(err) {
			t.Fatalf("unsafe retry: %v", err)
		}
	}
}

func TestBedrockStreamHTTPCancelWhileOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, sapEvents(sapText))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for resp, err := range testStreamProvider(server.Client(), server.URL).GenerateContent(ctx, &model.LLMRequest{}, true) {
			if err != nil {
				done <- err
				return
			}
			if resp.Partial {
				cancel()
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "interrupted") {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel failed to stop stream")
	}
}

func TestBedrockStreamCancelBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
		attempts++
		cancel()
		return nil, http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	})}
	var gotErr error
	for _, err := range testStreamProvider(client, "http://provider.test").GenerateContent(ctx, &model.LLMRequest{}, true) {
		gotErr = err
	}
	if !errors.Is(gotErr, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, gotErr)
	}
}

const sapVertexText = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`
const sapVertexThinking = `{"candidates":[{"content":{"role":"model","parts":[{"text":"private","thought":true}]}}]}`
const sapVertexTool = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"run","args":{}}}]}}]}`
const sapVertexStop = `{"candidates":[{"finishReason":"STOP"}]}`

func testVertexStreamProvider(client *http.Client, url string) *Provider {
	p := testStreamProvider(client, url)
	p.modelName = "gemini-2.5-pro"
	return p
}

func TestVertexStreamHTTPProgressBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference/deployments/test/models/gemini-2.5-pro:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("unexpected route: %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sapEvents(sapVertexThinking, sapVertexText, sapVertexTool, sapVertexStop))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, sapEvents(`{"usageMetadata":{"candidatesTokenCount":7}}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := make(chan streaming.Progress, 4)
	responses := make(chan *model.LLMResponse, 4)
	done := make(chan error, 1)
	ctx = streaming.WithProgress(ctx, func(p streaming.Progress) { progress <- p })
	go func() {
		for resp, err := range testVertexStreamProvider(server.Client(), server.URL).GenerateContent(ctx, &model.LLMRequest{}, true) {
			if err != nil {
				done <- err
				return
			}
			responses <- resp
		}
		done <- nil
	}()
	for _, want := range []streaming.Phase{streaming.PhaseThinking, streaming.PhaseToolArguments} {
		select {
		case got := <-progress:
			if got.Phase != want {
				t.Fatalf("phase=%v want=%v", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no progress before completion")
		}
	}
	select {
	case resp := <-responses:
		if !resp.Partial || len(resp.Content.Parts) != 1 || resp.Content.Parts[0].Text != "hello" {
			t.Fatalf("not partial text: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no text before completion")
	}
	select {
	case resp := <-responses:
		t.Fatalf("premature final/tools: %+v", resp)
	case err := <-done:
		t.Fatalf("premature completion: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	select {
	case final := <-responses:
		if final.Partial || final.UsageMetadata == nil || final.UsageMetadata.CandidatesTokenCount != 7 || len(final.Content.Parts) != 3 || final.Content.Parts[2].FunctionCall == nil {
			t.Fatalf("invalid final: %+v", final)
		}
	default:
		t.Fatal("missing final")
	}
	select {
	case resp := <-responses:
		t.Fatalf("duplicate output: %+v", resp)
	default:
	}
}

func TestVertexStreamPostOutputNeverReplay(t *testing.T) {
	for _, cause := range []error{
		http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal},
		llmerror.NewLLMError("sap-vertex", http.StatusServiceUnavailable, "overloaded", ""),
		llmerror.NewLLMError("sap-vertex", http.StatusBadRequest, "context length exceeded", ""),
		context.Canceled,
		context.DeadlineExceeded,
		io.ErrUnexpectedEOF,
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			attempts, partials, failures, finals := 0, 0, 0, 0
			body := &closeObservedBody{Reader: io.MultiReader(strings.NewReader(sapEvents(sapVertexText)), streamReadFailure{cause})}
			client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
			})}
			var gotErr error
			for resp, err := range testVertexStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
				if err != nil {
					gotErr = err
					failures++
				}
				if resp != nil {
					if resp.Partial {
						partials++
					} else {
						finals++
					}
				}
			}
			if attempts != 1 || partials != 1 || failures != 1 || finals != 0 || !body.closed {
				t.Fatalf("attempts=%d partials=%d failures=%d finals=%d closed=%v", attempts, partials, failures, finals, body.closed)
			}
			var interrupted *llmerror.InterruptedStreamError
			if !errors.Is(gotErr, cause) || !errors.As(gotErr, &interrupted) {
				t.Fatalf("missing interruption marker or cause: %v", gotErr)
			}
			if llmerror.IsRetryable(gotErr) || llmerror.IsContextOverflow(gotErr) {
				t.Fatalf("post-output interruption permits outer replay: %v", gotErr)
			}
		})
	}
}

func TestVertexStreamBeforeOutputDoesNotRetryOrMarkInterrupted(t *testing.T) {
	for _, prefix := range []string{"", sapEvents(sapVertexThinking), sapEvents(sapVertexTool)} {
		attempts, responses := 0, 0
		cause := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
		body := &closeObservedBody{Reader: io.MultiReader(strings.NewReader(prefix), streamReadFailure{cause})}
		client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		})}
		var gotErr error
		for resp, err := range testVertexStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true) {
			if resp != nil {
				responses++
			}
			gotErr = err
		}
		var interrupted *llmerror.InterruptedStreamError
		if attempts != 1 || responses != 0 || !body.closed || !errors.Is(gotErr, cause) || errors.As(gotErr, &interrupted) {
			t.Fatalf("attempts=%d responses=%d closed=%v err=%v", attempts, responses, body.closed, gotErr)
		}
	}
}

func TestVertexStreamYieldFalseClosesWithoutReplay(t *testing.T) {
	body := &closeObservedBody{Reader: strings.NewReader(sapEvents(sapVertexText, sapVertexStop))}
	attempts, calls := 0, 0
	client := &http.Client{Transport: streamRoundTrip(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})}
	testVertexStreamProvider(client, "http://provider.test").GenerateContent(context.Background(), &model.LLMRequest{}, true)(func(*model.LLMResponse, error) bool { calls++; return false })
	if attempts != 1 || calls != 1 || !body.closed {
		t.Fatalf("attempts=%d calls=%d closed=%v", attempts, calls, body.closed)
	}
}

func TestBedrockStreamHTTPProgressBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sapEvents(sapThinking, sapText, sapTool, sapToolStop))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, sapEvents(`{"type":"message_delta","usage":{"output_tokens":7}}`, sapStop))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := make(chan streaming.Progress, 4)
	responses := make(chan *model.LLMResponse, 4)
	done := make(chan error, 1)
	ctx = streaming.WithProgress(ctx, func(p streaming.Progress) { progress <- p })
	go func() {
		for resp, err := range testStreamProvider(server.Client(), server.URL).GenerateContent(ctx, &model.LLMRequest{}, true) {
			if err != nil {
				done <- err
				return
			}
			responses <- resp
		}
		done <- nil
	}()
	for _, want := range []streaming.Phase{streaming.PhaseThinking, streaming.PhaseToolArguments} {
		select {
		case got := <-progress:
			if got.Phase != want {
				t.Fatalf("phase=%v want=%v", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no progress before completion")
		}
	}
	select {
	case resp := <-responses:
		if !resp.Partial || resp.Content.Parts[0].Text != "hello" {
			t.Fatalf("not partial: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no partial before completion")
	}
	select {
	case resp := <-responses:
		t.Fatalf("premature final/tools: %+v", resp)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish")
	}
	final := <-responses
	if final.Partial || final.UsageMetadata == nil || final.UsageMetadata.CandidatesTokenCount != 7 || len(final.Content.Parts) != 2 || final.Content.Parts[1].FunctionCall == nil {
		t.Fatalf("invalid final: %+v", final)
	}
	select {
	case resp := <-responses:
		t.Fatalf("duplicate output: %+v", resp)
	default:
	}
}
