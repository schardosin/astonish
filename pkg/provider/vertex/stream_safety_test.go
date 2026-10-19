package vertex

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/provider/streaming"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func vertexEvent(payload string) string { return "data: " + payload + "\n\n" }

const vertexStop = `{"candidates":[{"finishReason":"STOP"}]}`
const vertexTool = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search","args":{"q":"secret"}},"thoughtSignature":"c2ln"}]}}]}`
const vertexText = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`

func TestVertexHeldOpenToolsAndUsage(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	type result struct {
		response *model.LLMResponse
		err      error
	}
	results := make(chan result, 8)
	progress := make(chan streaming.Progress, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for resp, err := range ParseStreamWithProgress(r, func(p streaming.Progress) { progress <- p }) {
			results <- result{resp, err}
		}
	}()
	write := func(payload string) {
		t.Helper()
		if _, err := io.WriteString(w, vertexEvent(payload)); err != nil {
			t.Fatal(err)
		}
	}
	write(vertexText)
	select {
	case got := <-results:
		if got.err != nil || got.response == nil || !got.response.Partial || got.response.Content.Parts[0].Text != "hello" {
			t.Fatalf("partial = %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("partial text was buffered")
	}
	write(vertexTool)
	select {
	case p := <-progress:
		if p.Phase != streaming.PhaseToolArguments {
			t.Fatalf("phase = %q", p.Phase)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tool progress was buffered")
	}
	// A subsequent successful write proves the parser consumed the tool event.
	write(`{"usageMetadata":{"promptTokenCount":10}}`)
	select {
	case got := <-results:
		t.Fatalf("premature tool/final = %+v", got)
	default:
	}
	write(vertexStop)
	write(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12,"cachedContentTokenCount":4}}`)
	select {
	case got := <-results:
		t.Fatalf("committed before clean EOF = %+v", got)
	default:
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("parser did not finish")
	}
	if len(results) != 1 {
		t.Fatalf("got %d final events, want one", len(results))
	}
	got := <-results
	if got.err != nil || got.response == nil || got.response.Partial {
		t.Fatalf("final = %+v", got)
	}
	parts := got.response.Content.Parts
	if len(parts) != 2 || parts[0].Text != "hello" || parts[1].FunctionCall == nil || parts[1].FunctionCall.Args["q"] != "secret" || string(parts[1].ThoughtSignature) != "sig" {
		t.Fatalf("parts = %+v", parts)
	}
	if got.response.UsageMetadata == nil || got.response.UsageMetadata.TotalTokenCount != 12 || got.response.UsageMetadata.CachedContentTokenCount != 4 {
		t.Fatalf("usage = %+v", got.response.UsageMetadata)
	}
}

func TestVertexRejectsUnsafeStreams(t *testing.T) {
	for name, input := range map[string]string{
		"empty":                     "",
		"text EOF":                  vertexEvent(vertexText),
		"tool EOF":                  vertexEvent(vertexTool),
		"unspecified":               vertexEvent(vertexTool) + vertexEvent(`{"candidates":[{"finishReason":"FINISH_REASON_UNSPECIFIED"}]}`),
		"unknown reason":            vertexEvent(vertexTool) + vertexEvent(`{"candidates":[{"finishReason":"NEW_UNKNOWN_REASON"}]}`),
		"max tokens":                vertexEvent(vertexTool) + vertexEvent(`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`),
		"safety tools":              vertexEvent(vertexTool) + vertexEvent(`{"candidates":[{"finishReason":"SAFETY"}]}`),
		"malformed function reason": vertexEvent(`{"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}`),
		"malformed JSON":            vertexEvent(vertexTool) + vertexEvent(`{"secret":`) + vertexEvent(vertexStop),
		"null JSON":                 vertexEvent(vertexTool) + vertexEvent(`null`) + vertexEvent(vertexStop),
		"malformed trailer":         vertexEvent(vertexTool) + vertexEvent(vertexStop) + vertexEvent(`invalid secret`),
		"null args":                 vertexEvent(strings.Replace(vertexTool, `{"q":"secret"}`, `null`, 1)) + vertexEvent(vertexStop),
		"array args":                vertexEvent(strings.Replace(vertexTool, `{"q":"secret"}`, `[]`, 1)) + vertexEvent(vertexStop),
		"string args":               vertexEvent(strings.Replace(vertexTool, `{"q":"secret"}`, `"secret"`, 1)) + vertexEvent(vertexStop),
		"missing args":              vertexEvent(strings.Replace(vertexTool, `,"args":{"q":"secret"}`, ``, 1)) + vertexEvent(vertexStop),
		"missing name":              vertexEvent(strings.Replace(vertexTool, `"name":"search",`, ``, 1)) + vertexEvent(vertexStop),
		"content after finish":      vertexEvent(vertexTool) + vertexEvent(vertexStop) + vertexEvent(vertexText),
		"conflicting finish":        vertexEvent(vertexTool) + vertexEvent(vertexStop) + vertexEvent(`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`),
		"other candidate finished":  vertexEvent(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search","args":{}}}]}},{"finishReason":"STOP"}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			errorsSeen := 0
			for resp, err := range ParseStream(strings.NewReader(input)) {
				if err != nil {
					errorsSeen++
					if strings.Contains(err.Error(), "secret") {
						t.Fatal("payload leaked in error")
					}
					continue
				}
				if !resp.Partial {
					t.Fatal("unsafe final response")
				}
				for _, part := range resp.Content.Parts {
					if part.FunctionCall != nil {
						t.Fatal("unsafe tool released")
					}
				}
			}
			if errorsSeen != 1 {
				t.Fatalf("got %d errors, want one", errorsSeen)
			}
		})
	}
}

func TestVertexFinalBytesMultipleToolsAndCandidates(t *testing.T) {
	input := "data:" + `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"one","args":{}}},{"functionCall":{"name":"two","args":{"n":2}}}]},"finishReason":"STOP"},{"content":{"parts":[{"text":"ignored"}]}}]}`
	count := 0
	for resp, err := range ParseStream(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		count++
		if resp.Partial || len(resp.Content.Parts) != 2 || resp.Content.Parts[0].FunctionCall.Name != "one" || resp.Content.Parts[1].FunctionCall.Name != "two" {
			t.Fatalf("response = %+v", resp)
		}
	}
	if count != 1 {
		t.Fatalf("got %d events", count)
	}
}

func TestVertexProgressBoundedAndThoughtRoundtrip(t *testing.T) {
	thought := vertexEvent(`{"candidates":[{"content":{"parts":[{"text":"private reasoning","thought":true,"thoughtSignature":"c2ln"}]}}]}`)
	input := strings.Repeat(thought+vertexEvent(vertexText), 100) + vertexEvent(vertexTool) + vertexEvent(vertexStop)
	var phases []streaming.Progress
	var final *model.LLMResponse
	for resp, err := range ParseStreamWithProgress(strings.NewReader(input), func(p streaming.Progress) { phases = append(phases, p) }) {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Partial {
			for _, p := range resp.Content.Parts {
				if p.Thought {
					t.Fatal("thought exposed as visible partial")
				}
			}
		} else {
			final = resp
		}
	}
	if len(phases) != 2 || phases[0].Phase != streaming.PhaseThinking || phases[1].Phase != streaming.PhaseToolArguments {
		t.Fatalf("progress = %+v", phases)
	}
	if strings.Contains(fmt.Sprint(phases), "private") || strings.Contains(fmt.Sprint(phases), "secret") {
		t.Fatal("payload leaked into progress")
	}
	if final == nil || !final.Content.Parts[0].Thought || string(final.Content.Parts[0].ThoughtSignature) != "sig" {
		t.Fatal("thought/signature lost")
	}
	req, err := ConvertRequest(&model.LLMRequest{Contents: []*genai.Content{final.Content}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !req.Contents[0].Parts[0].Thought || !bytes.Equal(req.Contents[0].Parts[0].ThoughtSignature, []byte("sig")) {
		t.Fatal("request dropped signature")
	}
	parsed, err := ParseResponse([]byte(`{"candidates":[{"content":{"parts":[{"thoughtSignature":"c2ln"}]}}]}`))
	if err != nil || parsed == nil || len(parsed.Content.Parts) != 1 || string(parsed.Content.Parts[0].ThoughtSignature) != "sig" {
		t.Fatalf("signature-only response = %+v, %v", parsed, err)
	}
}

type vertexFailReader struct{ err error }

func (r vertexFailReader) Read([]byte) (int, error) { return 0, r.err }

func TestVertexReaderErrorAndYieldFalse(t *testing.T) {
	failure := errors.New("reader failed")
	for resp, err := range ParseStream(io.MultiReader(strings.NewReader(vertexEvent(vertexTool)+vertexEvent(vertexStop)), vertexFailReader{failure})) {
		if resp != nil || !errors.Is(err, failure) {
			t.Fatalf("response=%v error=%v", resp, err)
		}
	}
	for _, input := range []string{vertexEvent(vertexText) + "data: broken\n", vertexEvent(vertexTool) + vertexEvent(vertexStop), "data: broken\n"} {
		calls := 0
		ParseStream(strings.NewReader(input))(func(*model.LLMResponse, error) bool { calls++; return false })
		if calls != 1 {
			t.Fatalf("yield called %d times", calls)
		}
	}
	for _, err := range ParseStream(strings.NewReader(vertexEvent(vertexText))) {
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestVertexTextLimitIsInterruption(t *testing.T) {
	for name, input := range map[string]string{
		"separate finish": vertexEvent(vertexText) + vertexEvent(`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`),
		"same chunk":      vertexEvent(`{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"MAX_TOKENS"}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			var text strings.Builder
			errorsSeen := 0
			for resp, err := range ParseStream(strings.NewReader(input)) {
				if err != nil {
					errorsSeen++
					if resp != nil || !strings.Contains(err.Error(), "truncated") {
						t.Fatalf("response=%v error=%v", resp, err)
					}
					continue
				}
				if resp == nil || !resp.Partial || resp.Content == nil || errorsSeen != 0 {
					t.Fatalf("unexpected successful response: %+v", resp)
				}
				for _, part := range resp.Content.Parts {
					text.WriteString(part.Text)
				}
			}
			if text.String() != "hello" || errorsSeen != 1 {
				t.Fatalf("partial text=%q errors=%d", text.String(), errorsSeen)
			}
		})
	}
}

func TestVertexNonStopTextIsInterruption(t *testing.T) {
	for _, reason := range []genai.FinishReason{
		genai.FinishReasonSafety, genai.FinishReasonRecitation,
		genai.FinishReasonLanguage, genai.FinishReasonOther,
		genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent,
		genai.FinishReasonSPII, genai.FinishReasonImageSafety,
		genai.FinishReasonImageProhibitedContent, genai.FinishReasonNoImage,
		genai.FinishReasonImageRecitation, genai.FinishReasonImageOther,
		genai.FinishReasonUnspecified, genai.FinishReasonMalformedFunctionCall,
		"NEW_UNKNOWN_REASON",
	} {
		t.Run(string(reason), func(t *testing.T) {
			input := vertexEvent(vertexText) + vertexEvent(`{"candidates":[{"finishReason":"`+string(reason)+`"}]}`)
			errorsSeen, partials := 0, 0
			for resp, err := range ParseStream(strings.NewReader(input)) {
				if err != nil {
					errorsSeen++
					if resp != nil {
						t.Fatal("error included a final response")
					}
					continue
				}
				if resp == nil || !resp.Partial || resp.Content == nil || len(resp.Content.Parts) != 1 || resp.Content.Parts[0].Text != "hello" || errorsSeen != 0 {
					t.Fatalf("unexpected response: %+v", resp)
				}
				partials++
			}
			if errorsSeen != 1 || partials != 1 {
				t.Fatalf("errors=%d partials=%d", errorsSeen, partials)
			}
		})
	}
}

func TestVertexStopTextCompletes(t *testing.T) {
	input := vertexEvent(vertexText) + vertexEvent(vertexStop) + vertexEvent(`{"usageMetadata":{"candidatesTokenCount":1}}`)
	partials, finals := 0, 0
	for resp, err := range ParseStream(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		if resp == nil || resp.Content == nil || len(resp.Content.Parts) != 1 || resp.Content.Parts[0].Text != "hello" {
			t.Fatalf("unexpected response: %+v", resp)
		}
		if resp.Partial {
			partials++
		} else {
			finals++
			if resp.UsageMetadata == nil || resp.UsageMetadata.CandidatesTokenCount != 1 {
				t.Fatalf("usage trailer lost: %+v", resp.UsageMetadata)
			}
		}
	}
	if partials != 1 || finals != 1 {
		t.Fatalf("partials=%d finals=%d", partials, finals)
	}
}
