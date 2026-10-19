package llmerror

import "fmt"

// TruncatedStreamError marks a stream that ended without its required completion
// signal. It does not itself authorize replay: callers must also check for
// InterruptedStreamError and any content already delivered.
type TruncatedStreamError struct {
	Provider string
}

func (e *TruncatedStreamError) Error() string {
	return fmt.Sprintf("%s stream ended without a finish_reason", e.Provider)
}
