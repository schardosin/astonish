package store

import (
	"errors"
	"testing"
)

func TestErrUnsupported_ErrorsIs(t *testing.T) {
	// Verbatim return satisfies errors.Is.
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Fatalf("errors.Is(ErrUnsupported, ErrUnsupported) = false; want true")
	}

	// Wrapped sentinel is still recognized.
	wrapped := errors.New("some other error")
	if errors.Is(wrapped, ErrUnsupported) {
		t.Fatalf("unrelated error unexpectedly matches ErrUnsupported")
	}
}
