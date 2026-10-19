package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/provider/llmerror"
	"google.golang.org/adk/model"
)

func TestStreamingSafetyTerminalReasons(t *testing.T) {
	for _, reason := range []string{"", "end_turn", "stop_sequence", "tool_use", "pause_turn", "max_tokens", "model_context_window_exceeded", "refusal", "unknown_reason"} {
		for _, tool := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tool=%t", reason, tool), func(t *testing.T) {
				start := `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`
				if tool {
					start = safetyToolStart
				}
				terminal := fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q}}`, reason)
				wantError := reason == "max_tokens" || reason == "model_context_window_exceeded" || reason == "refusal" || reason == "unknown_reason"
				finals, failures := 0, 0
				NewProvider("", "").handleStream(strings.NewReader(safetySSE(start, safetyToolStop, terminal, safetyStop)), func(resp *model.LLMResponse, err error) bool {
					if err != nil {
						failures++
						return true
					}
					if !resp.Partial {
						finals++
					}
					return true
				})
				if wantError {
					if failures != 1 || finals != 0 {
						t.Fatalf("failures=%d finals=%d; want one error and no committed content", failures, finals)
					}
				} else if failures != 0 || finals != 1 {
					t.Fatalf("failures=%d finals=%d; want one committed response", failures, finals)
				}
			})
		}
	}
}

type terminalErrorReader struct{ err error }

func (r terminalErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestStreamingSafetyInterruptedErrors(t *testing.T) {
	for _, visible := range []bool{false, true} {
		for _, mode := range []string{"reader", "cancel", "eof"} {
			t.Run(fmt.Sprintf("%s/visible=%t", mode, visible), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cause := error(llmerror.NewLLMError("anthropic", 503, "unavailable", ""))
				if mode == "cancel" {
					cause = context.Canceled
				} else if mode == "eof" {
					cause = io.ErrUnexpectedEOF
				}
				var reader io.Reader = terminalErrorReader{err: cause}
				if mode == "eof" {
					reader = strings.NewReader("")
				}
				if visible {
					reader = io.MultiReader(strings.NewReader(safetySSE(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`)), reader)
				} else if mode == "cancel" {
					cancel()
				}
				partials, failures := 0, 0
				NewProvider("", "").handleStreamWithContext(ctx, reader, func(resp *model.LLMResponse, err error) bool {
					if err != nil {
						failures++
						if !errors.Is(err, cause) {
							t.Fatalf("lost cause: %v", err)
						}
						var interrupted *llmerror.InterruptedStreamError
						if errors.As(err, &interrupted) != visible {
							t.Errorf("interrupted marker=%t, want %t: %v", interrupted != nil, visible, err)
						}
						if mode == "reader" && llmerror.IsRetryable(err) == visible {
							t.Errorf("retryable=%t, visible=%t", llmerror.IsRetryable(err), visible)
						}
						return true
					}
					if resp == nil || !resp.Partial {
						t.Fatal("unexpected committed response")
					}
					partials++
					if mode == "cancel" {
						cancel()
					}
					return true
				})
				if failures != 1 || (visible && partials != 1) || (!visible && partials != 0) {
					t.Fatalf("failures=%d partials=%d", failures, partials)
				}
			})
		}
	}
}
