package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/channels"
)

type countingLinkBackend struct {
	LinkCodeBackend
	peeks    int
	consumes int
}

func (b *countingLinkBackend) Peek(context.Context, string) *PendingLink    { b.peeks++; return nil }
func (b *countingLinkBackend) Consume(context.Context, string) *PendingLink { b.consumes++; return nil }

func TestChannelLinkLimitBeforeLookup(t *testing.T) {
	old := channelLinkAttempts
	channelLinkAttempts = NewLinkAttemptLimiter()
	defer func() { channelLinkAttempts = old }()
	backend := &countingLinkBackend{}
	for i := 0; i < 30; i++ {
		ConsumeChannelLinkCode(context.Background(), backend, channels.LinkTelegram, "123456", "bad")
	}
	if backend.peeks != linkSenderAttempts || backend.consumes != 0 {
		t.Fatalf("lookups=%d consumes=%d", backend.peeks, backend.consumes)
	}
	ConsumeChannelLinkCode(context.Background(), backend, channels.LinkTelegram, "123457", "bad")
	if backend.peeks != linkSenderAttempts+1 {
		t.Fatal("different sender blocked")
	}
}

func TestLinkAttemptLimitsChannelScopeExpiryAndBoundedMemory(t *testing.T) {
	limiter := NewLinkAttemptLimiter()
	for i := 0; i < linkChannelAttempts; i++ {
		if !limiter.Allow(channels.LinkTelegram, fmt.Sprint(i)) {
			t.Fatal("early channel rejection")
		}
	}
	if limiter.Allow(channels.LinkTelegram, "another") {
		t.Fatal("channel budget not enforced")
	}
	if !limiter.Allow(channels.LinkSlack, "T123:U123") {
		t.Fatal("channel budgets not isolated")
	}
	limiter.window = time.Now().Add(-linkAttemptWindow)
	if !limiter.Allow(channels.LinkTelegram, "another") {
		t.Fatal("expired window not reset")
	}
	// Saturated cardinality fails closed, rather than evicting active attack budgets.
	for i := 0; i < linkMaxSenders; i++ {
		limiter.Allow(channels.LinkChannel(fmt.Sprint(i)), fmt.Sprint(i))
	}
	if len(limiter.senders) > linkMaxSenders || limiter.Allow(channels.LinkEmail, "new") {
		t.Fatal("unbounded limiter")
	}
}
