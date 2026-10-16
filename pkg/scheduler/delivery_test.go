package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/channels"
)

type failClosedDeliveryChannel struct {
	sends      int
	broadcasts int
}

func (c *failClosedDeliveryChannel) ID() string                                           { return "slack" }
func (c *failClosedDeliveryChannel) Name() string                                         { return "Slack mock" }
func (c *failClosedDeliveryChannel) Start(context.Context, channels.MessageHandler) error { return nil }
func (c *failClosedDeliveryChannel) Stop(context.Context) error                           { return nil }
func (c *failClosedDeliveryChannel) Send(context.Context, channels.Target, channels.OutboundMessage) error {
	c.sends++
	return nil
}
func (c *failClosedDeliveryChannel) BroadcastTargets() []channels.Target {
	c.broadcasts++
	return []channels.Target{{ChannelID: "slack", ChatID: "T999:U999"}}
}
func (c *failClosedDeliveryChannel) SendTyping(context.Context, channels.Target) error { return nil }
func (c *failClosedDeliveryChannel) Status() channels.ChannelStatus {
	return channels.ChannelStatus{Connected: true}
}

type failClosedDeliveryResolver struct{}

func (failClosedDeliveryResolver) ResolveUserChannels(context.Context, string) ([]DeliveryTarget, error) {
	return nil, fmt.Errorf("legacy Slack link: unlink and relink")
}
func (failClosedDeliveryResolver) ResolveTeamMembers(context.Context, string, string) ([]string, error) {
	return []string{"owner"}, nil
}
func TestPlatformDeliveryResolutionFailsClosed(t *testing.T) {
	for _, resolver := range []DeliveryResolver{failClosedDeliveryResolver{}, nil} {
		t.Run(fmt.Sprintf("%T", resolver), func(t *testing.T) {
			ch := &failClosedDeliveryChannel{}
			mgr := channels.NewChannelManager(nil, nil, log.Default(), nil)
			mgr.Register(ch)
			deliver := NewPlatformDeliverFunc(func() *channels.ChannelManager { return mgr }, resolver, nil)
			job := &Job{Name: "private-result", OwnerID: "owner", Delivery: JobDelivery{Mode: DeliveryModeOwner}}
			err := deliver(context.Background(), job, "secret", nil)
			if err == nil {
				t.Fatal("resolution failure was hidden")
			}
			if ch.sends != 0 || ch.broadcasts != 0 {
				t.Fatalf("unsafe fallback: sends=%d broadcasts=%d", ch.sends, ch.broadcasts)
			}
			if resolver != nil && !strings.Contains(err.Error(), "relink") {
				t.Fatalf("lost migration guidance: %v", err)
			}
		})
	}
}

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
