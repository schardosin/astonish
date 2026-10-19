package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/SAP/astonish/pkg/provider/llmerror"
)

func TestTypedTruncationRetryGate(t *testing.T) {
	truncated := &llmerror.TruncatedStreamError{Provider: "custom diagnostic"}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"typed", truncated, true},
		{"wrapped", fmt.Errorf("dispatch: %w", truncated), true},
		{"nil", nil, false},
		{"lookalike string", errors.New("OpenAI stream ended without a finish_reason"), false},
		{"other error", errors.New("connection timeout"), false},
		{"interrupted", llmerror.NewInterruptedStream(truncated), false},
		{"wrapped interrupted", fmt.Errorf("dispatch: %w", llmerror.NewInterruptedStream(truncated)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryTruncatedStream(tc.err, false); got != tc.want {
				t.Fatalf("retry without content = %v, want %v", got, tc.want)
			}
			if shouldRetryTruncatedStream(tc.err, true) {
				t.Fatal("must not retry after content")
			}
		})
	}
}
