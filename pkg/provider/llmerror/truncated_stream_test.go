package llmerror

import (
	"errors"
	"fmt"
	"testing"
)

func TestTruncatedStreamError(t *testing.T) {
	cause := &TruncatedStreamError{Provider: "OpenAI"}
	if cause.Error() != "OpenAI stream ended without a finish_reason" {
		t.Fatalf("unexpected diagnostic: %s", cause)
	}
	for _, err := range []error{cause, fmt.Errorf("provider: %w", cause), NewInterruptedStream(cause)} {
		var truncated *TruncatedStreamError
		if !errors.As(err, &truncated) || truncated != cause || !errors.Is(err, cause) {
			t.Fatalf("lost truncation identity: %v", err)
		}
		if IsRetryable(err) || IsContextOverflow(err) {
			t.Fatalf("truncation must not enable generic replay: %v", err)
		}
	}
}
