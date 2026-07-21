package scheduler

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/channels"
)

func TestPlatformDeliverFunc_EmptyBroadcastErrors(t *testing.T) {
	mgr := channels.NewChannelManager(nil, nil, log.Default(), nil)
	deliver := NewPlatformDeliverFunc(func() *channels.ChannelManager {
		return mgr
	}, nil, log.New(&bytes.Buffer{}, "", 0))

	job := &Job{
		Name: "empty-targets",
		Delivery: JobDelivery{
			Mode: "", // no mode → broadcast fallback
		},
	}
	err := deliver(context.Background(), job, "hello", nil)
	if err == nil {
		t.Fatal("expected error when no broadcast targets exist")
	}
	if !strings.Contains(err.Error(), "no delivery targets available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeliverFunc_EmptyBroadcastErrors(t *testing.T) {
	mgr := channels.NewChannelManager(nil, nil, log.Default(), nil)
	deliver := NewDeliverFunc(func() *channels.ChannelManager {
		return mgr
	})

	job := &Job{Name: "empty-targets"}
	err := deliver(context.Background(), job, "hello", nil)
	if err == nil {
		t.Fatal("expected error when no broadcast targets exist")
	}
	if !strings.Contains(err.Error(), "no delivery targets available") {
		t.Fatalf("unexpected error: %v", err)
	}
}
