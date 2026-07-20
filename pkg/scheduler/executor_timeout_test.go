package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecutor_Timeout(t *testing.T) {
	prev := jobTimeout
	jobTimeout = 50 * time.Millisecond
	t.Cleanup(func() { jobTimeout = prev })

	e := &Executor{
		FleetPoll: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	job := &Job{
		Name: "slow-poll",
		Mode: ModeFleetPoll,
		Payload: JobPayload{
			Flow: "fleet-plan-key",
		},
	}
	_, err := e.Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timed out error, got: %v", err)
	}
}
