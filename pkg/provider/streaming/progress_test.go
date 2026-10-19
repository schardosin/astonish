package streaming

import (
	"context"
	"reflect"
	"testing"
)

func TestProgressContextIsolationAndSynchronousDelivery(t *testing.T) {
	var got []Progress
	parent := context.Background()
	ctx := WithProgress(parent, func(p Progress) { got = append(got, p) })
	Report(parent, Progress{Phase: PhaseThinking})
	Report(ctx, Progress{Phase: PhaseThinking})
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	Report(child, Progress{Phase: PhaseToolArguments})
	Report(WithProgress(ctx, nil), Progress{Phase: PhaseThinking})
	want := []Progress{{Phase: PhaseThinking}, {Phase: PhaseToolArguments}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if reflect.TypeOf(Progress{}).NumField() != 1 {
		t.Fatal("progress must carry phase only")
	}
}
