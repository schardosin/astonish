package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestEnsureEphemeralAdaptiveSandbox_CallsDestroyTwice(t *testing.T) {
	var calls atomic.Int32
	e := &Executor{
		DestroySandbox: func(ctx context.Context, sessionID string) error {
			calls.Add(1)
			if sessionID != "scheduler-adaptive-job1" {
				t.Errorf("sessionID = %q", sessionID)
			}
			return nil
		},
	}

	cleanup := e.ensureEphemeralAdaptiveSandbox(context.Background(), "scheduler-adaptive-job1")
	if got := calls.Load(); got != 1 {
		t.Fatalf("after ensure: destroy calls = %d, want 1", got)
	}
	cleanup()
	if got := calls.Load(); got != 2 {
		t.Fatalf("after cleanup: destroy calls = %d, want 2", got)
	}
}

func TestEnsureEphemeralAdaptiveSandbox_NilDestroyIsNoop(t *testing.T) {
	e := &Executor{}
	cleanup := e.ensureEphemeralAdaptiveSandbox(context.Background(), "any")
	cleanup() // must not panic
}

func TestEnsureEphemeralAdaptiveSandbox_IgnoresDestroyErrors(t *testing.T) {
	var calls atomic.Int32
	e := &Executor{
		DestroySandbox: func(ctx context.Context, sessionID string) error {
			calls.Add(1)
			return context.DeadlineExceeded
		},
	}
	cleanup := e.ensureEphemeralAdaptiveSandbox(context.Background(), "sess")
	cleanup()
	if got := calls.Load(); got != 2 {
		t.Fatalf("destroy calls = %d, want 2 even when errors", got)
	}
}
