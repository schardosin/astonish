package daemon

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/store"
)

type linkingBackend struct {
	store.PlatformBackend
	links *linkingChannels
}

func (b linkingBackend) UserChannels() store.UserChannelStore { return b.links }

type linkingChannels struct {
	store.UserChannelStore
	linked *store.UserChannel
}

func (s *linkingChannels) GetByExternalID(context.Context, string, string) (*store.UserChannel, error) {
	return nil, nil
}
func (s *linkingChannels) Link(_ context.Context, ch *store.UserChannel) error {
	s.linked = ch
	return nil
}

func TestChannelLinkRejectsWrongPrincipalWithoutConsume(t *testing.T) {
	for _, channel := range []string{"telegram", "slack"} {
		t.Run(channel, func(t *testing.T) {
			ctx := context.Background()
			codes := api.NewLinkCodeStore()
			principal := "12345"
			if channel == "slack" {
				principal = "T123:U123"
			}
			code, err := codes.Generate(ctx, "owner", "owner@example.com", "link:v1:"+channel+":"+principal)
			if err != nil {
				t.Fatal(err)
			}
			links := &linkingChannels{}
			backend := linkingBackend{links: links}
			handler := buildTelegramLinkHandler(backend, codes, nil)
			wrongChannel := buildSlackLinkHandler(backend, codes, nil)
			if channel == "slack" {
				handler, wrongChannel = wrongChannel, handler
			}
			for _, sender := range []string{"99999", "T999:U123", "T123:U999"} {
				if ok, _ := handler(ctx, sender, "intruder", code); ok {
					t.Fatal("wrong sender linked")
				}
				if codes.Peek(ctx, code) == nil {
					t.Fatal("wrong sender consumed code")
				}
			}
			if ok, _ := wrongChannel(ctx, principal, "owner", code); ok {
				t.Fatal("wrong channel linked")
			}
			if codes.Peek(ctx, code) == nil {
				t.Fatal("wrong channel consumed code")
			}
			if ok, message := handler(ctx, principal, "owner", code); !ok {
				t.Fatalf("owner rejected: %s", message)
			}
			if links.linked == nil || links.linked.ExternalID != principal || links.linked.UserID != "owner" {
				t.Fatalf("incorrect binding: %+v", links.linked)
			}
			if ok, _ := handler(ctx, principal, "owner", code); ok {
				t.Fatal("replayed code linked")
			}
		})
	}
}
