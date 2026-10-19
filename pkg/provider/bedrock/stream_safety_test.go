package bedrock

import (
	"errors"
	"github.com/SAP/astonish/pkg/provider/streaming"
	"io"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/adk/model"
)

func streamEvents(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

const toolStart = `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"run","input":{}}}`
const toolStop = `{"type":"content_block_stop","index":0}`
const messageStop = `{"type":"message_stop"}`
const textDelta = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`

func TestStreamProgressBoundedAndPrivate(t *testing.T) {
	thinking := `{"type":"content_block_start","content_block":{"type":"thinking","thinking":"secret"}}`
	delta := `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"secret","text":"must not leak"}}`
	input := streamEvents(thinking) + strings.Repeat(streamEvents(delta), 100) + streamEvents(textDelta, thinking, delta, toolStart, toolStop, messageStop)
	var phases []streaming.Phase
	var partial string
	for resp, err := range ParseStreamWithProgress(strings.NewReader(input), func(p streaming.Progress) { phases = append(phases, p.Phase) }) {
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range resp.Content.Parts {
			if strings.Contains(part.Text, "secret") || strings.Contains(part.Text, "leak") {
				t.Fatal("reasoning leaked")
			}
			if resp.Partial {
				partial += part.Text
			}
		}
	}
	if partial != "hello" {
		t.Fatalf("partial=%q", partial)
	}
	want := []streaming.Phase{streaming.PhaseThinking, streaming.PhaseThinking, streaming.PhaseToolArguments}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases=%v want=%v", phases, want)
	}
}

func TestStreamMultipleToolsCommittedTogether(t *testing.T) {
	second := strings.ReplaceAll(toolStart, "call-1", "call-2")
	input := streamEvents(toolStart, toolStop, second, toolStop, messageStop)
	count := 0
	for resp, err := range ParseStream(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		count++
		if len(resp.Content.Parts) != 2 {
			t.Fatalf("not batched: %+v", resp.Content)
		}
	}
	if count != 1 {
		t.Fatalf("responses=%d", count)
	}
}

func TestStreamRejectsIncompleteAndMalformedTools(t *testing.T) {
	cases := map[string]string{
		"empty argument delta":  streamEvents(toolStart, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`, toolStop, messageStop),
		"unexpected tool delta": streamEvents(toolStart, `{"type":"content_block_delta","index":0,"delta":{"type":"bogus","partial_json":"{}"}}`, toolStop, messageStop),
		"empty":                 "",
		"text EOF":              streamEvents(textDelta),
		"completed tool EOF":    streamEvents(toolStart, toolStop),
		"unfinished tool":       streamEvents(toolStart, messageStop),
		"invalid args":          streamEvents(toolStart, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{"}}`, toolStop, messageStop),
		"null args":             streamEvents(toolStart, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"null"}}`, toolStop, messageStop),
		"malformed JSON":        streamEvents(`{"type":`, messageStop),
		"explicit error":        streamEvents(`{"type":"error","error":{"type":"overloaded_error","message":"secret"}}`, messageStop),
		"SSE error":             "event: error\ndata: {}\n\n" + streamEvents(messageStop),
		"wrong tool index":      streamEvents(toolStart, `{"type":"content_block_stop","index":1}`, messageStop),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			var gotErr error
			for resp, err := range ParseStream(strings.NewReader(input)) {
				if err != nil {
					gotErr = err
					continue
				}
				if resp != nil && !resp.Partial {
					t.Errorf("unsafe final response: %+v", resp)
				}
			}
			if gotErr == nil {
				t.Fatal("expected terminal/protocol error")
			}
		})
	}
}

func TestStreamStopReasons(t *testing.T) {
	stopReason := func(reason string) string {
		return `{"type":"message_delta","delta":{"stop_reason":"` + reason + `"},"usage":{"output_tokens":9}}`
	}
	populated := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"echo hello\"}"}}`
	cases := []struct {
		name    string
		events  []string
		wantErr string
		tools   int
	}{
		{"truncated populated tool", []string{toolStart, populated, toolStop, stopReason("max_tokens"), messageStop}, "max_tokens", 0},
		{"truncated empty tool", []string{toolStart, toolStop, stopReason("max_tokens"), messageStop}, "max_tokens", 0},
		{"truncated text", []string{stopReason("max_tokens"), messageStop}, "max_tokens", 0},
		{"context window truncation", []string{toolStart, toolStop, stopReason("model_context_window_exceeded"), messageStop}, "model_context_window_exceeded", 0},
		{"refused tool", []string{toolStart, toolStop, stopReason("refusal"), messageStop}, "refusal", 0},
		{"refused text", []string{stopReason("refusal"), messageStop}, "refusal", 0},
		{"unknown reason", []string{toolStart, toolStop, stopReason("unknown"), messageStop}, "stop reason", 0},
		{"conflicting reason", []string{toolStart, toolStop, stopReason("max_tokens"), stopReason("tool_use"), messageStop}, "stop reason", 0},
		{"truncation then usage only", []string{toolStart, toolStop, stopReason("max_tokens"), `{"type":"message_delta","usage":{"output_tokens":10}}`, messageStop}, "max_tokens", 0},
		{"valid populated tool", []string{toolStart, populated, toolStop, stopReason("tool_use"), messageStop}, "", 1},
		{"valid empty tool", []string{toolStart, toolStop, stopReason("tool_use"), messageStop}, "", 1},
		{"valid end turn", []string{stopReason("end_turn"), messageStop}, "", 0},
		{"valid stop sequence", []string{stopReason("stop_sequence"), messageStop}, "", 0},
		{"legacy omitted reason", []string{toolStart, toolStop, messageStop}, "", 1},
		{"reason without terminal", []string{toolStart, toolStop, stopReason("tool_use")}, "message_stop", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := streamEvents(append([]string{textDelta}, tc.events...)...)
			var gotErr error
			var partial string
			finals, tools := 0, 0
			for resp, err := range ParseStream(strings.NewReader(input)) {
				if err != nil {
					gotErr = err
					continue
				}
				if !resp.Partial {
					finals++
				}
				for _, part := range resp.Content.Parts {
					if resp.Partial {
						partial += part.Text
					}
					if part.FunctionCall != nil {
						tools++
					}
				}
			}
			if partial != "hello" {
				t.Errorf("partial text lost: %q", partial)
			}
			if tools != tc.tools {
				t.Errorf("tools=%d want=%d", tools, tc.tools)
			}
			if tc.wantErr != "" {
				if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantErr) {
					t.Errorf("error=%v want containing %q", gotErr, tc.wantErr)
				}
				if finals != 0 {
					t.Errorf("unsafe success aggregates=%d", finals)
				}
			} else if gotErr != nil || finals != 1 {
				t.Errorf("error=%v aggregates=%d; want one success", gotErr, finals)
			}
		})
	}
}

func TestStreamToolsAndUsageOnlyAtMessageStop(t *testing.T) {
	input := streamEvents(toolStart, toolStop, `{"type":"message_delta","usage":{"output_tokens":9}}`) + "data: " + messageStop
	var got []*model.LLMResponse
	for resp, err := range ParseStream(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, resp)
	}
	if len(got) != 1 || got[0].Partial || got[0].UsageMetadata == nil || got[0].UsageMetadata.CandidatesTokenCount != 9 {
		t.Fatalf("want one tool response with usage, got %+v", got)
	}
	if len(got[0].Content.Parts) != 1 || got[0].Content.Parts[0].FunctionCall == nil {
		t.Fatal("missing tool")
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
func TestStreamReaderErrorAndYieldFalse(t *testing.T) {
	sentinel := errors.New("read failed")
	gotError := false
	for _, err := range ParseStream(io.MultiReader(strings.NewReader(streamEvents(textDelta)), failingReader{sentinel})) {
		if err != nil {
			gotError = true
		}
		if err != nil && !errors.Is(err, sentinel) {
			t.Fatalf("lost reader cause: %v", err)
		}
	}
	if !gotError {
		t.Fatal("reader error was swallowed")
	}
	calls := 0
	ParseStream(strings.NewReader(streamEvents(textDelta, toolStart, toolStop, messageStop)))(func(*model.LLMResponse, error) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("yield called %d times", calls)
	}
}
