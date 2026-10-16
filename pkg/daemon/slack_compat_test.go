package daemon

import (
	"bytes"
	"context"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/execution"
	"github.com/SAP/astonish/pkg/scheduler"
	"github.com/SAP/astonish/pkg/store"
)

type slackCompatBackend struct {
	store.PlatformBackend
	links *slackCompatLinks
}

func (b slackCompatBackend) UserChannels() store.UserChannelStore { return b.links }

type slackCompatLinks struct {
	store.UserChannelStore
	links   []*store.UserChannel
	lookups []string
}

func (s *slackCompatLinks) ListByUser(context.Context, string) ([]*store.UserChannel, error) {
	return s.links, nil
}
func (s *slackCompatLinks) GetByExternalID(_ context.Context, channel, externalID string) (*store.UserChannel, error) {
	s.lookups = append(s.lookups, channel+"/"+externalID)
	for _, link := range s.links {
		if link.ChannelType == channel && link.ExternalID == externalID {
			return link, nil
		}
	}
	return nil, nil
}
func TestSlackQualifiedInboundContextAndDelivery(t *testing.T) {
	backend, owner := setupChannelResolverFixture(t, "slackorg", "slackteam", "slack", "T123:U123")
	r := &channelPlatformResolver{backend: backend}
	ctx, uid, _, err := r.ResolveChannelUser(context.Background(), "slack", "T123:U123")
	if err != nil || uid != owner {
		t.Fatalf("lookup uid=%s err=%v", uid, err)
	}
	principal, ok := execution.PrincipalFromContext(ctx)
	if !ok || principal.Subject != owner || principal.OrgSlug != "slackorg" || principal.TeamSlug != "slackteam" {
		t.Fatalf("principal=%+v", principal)
	}
	targets, err := (&deliveryResolver{backend: backend}).ResolveUserChannels(ctx, owner)
	if err != nil || !reflect.DeepEqual(targets, []scheduler.DeliveryTarget{{ChannelID: "slack", ChatID: "T123:U123"}}) {
		t.Fatalf("delivery=%v err=%v", targets, err)
	}
	_, uid, _, err = r.ResolveChannelUser(context.Background(), "slack", "T456:U123")
	if err == nil || uid != "" {
		t.Fatalf("other workspace resolved owner: %s %v", uid, err)
	}
}

func TestSlackPlatformAllowlistIncludesMemberIDs(t *testing.T) {
	var logs bytes.Buffer
	ids := slackPlatformAllowlist([]*store.UserChannel{
		{ExternalID: "U123", Enabled: true, Verified: true},
		{ExternalID: "T123:U123", Enabled: true, Verified: true},
		{ExternalID: "T456:U123", Enabled: true, Verified: false},
		{ExternalID: "not-a-slack-id", Enabled: true, Verified: true, UserID: "bad"},
	}, log.New(&logs, "", 0))
	if !reflect.DeepEqual(ids, []string{"U123", "T123:U123"}) {
		t.Fatalf("ids=%v warning=%s", ids, logs.String())
	}
	if !strings.Contains(logs.String(), "Ignoring invalid Slack link") {
		t.Fatalf("missing invalid-link warning: %s", logs.String())
	}
}

func TestSlackDeliveryResolverCompatibility(t *testing.T) {
	for _, externalID := range []string{"T123:U123", "U123"} {
		t.Run(externalID, func(t *testing.T) {
			links := &slackCompatLinks{links: []*store.UserChannel{{UserID: "owner", ChannelType: "slack", ExternalID: externalID, Enabled: true, Verified: true}}}
			r := &deliveryResolver{backend: slackCompatBackend{links: links}}
			targets, err := r.ResolveUserChannels(context.Background(), "owner")
			want := []scheduler.DeliveryTarget{{ChannelID: "slack", ChatID: externalID}}
			if err != nil || !reflect.DeepEqual(targets, want) {
				t.Fatalf("targets=%v err=%v", targets, err)
			}
		})
	}
}

func TestSlackInboundLookupFallsBackToBareMemberID(t *testing.T) {
	links := &slackCompatLinks{links: []*store.UserChannel{{UserID: "legacy-owner", ChannelType: "slack", ExternalID: "U123", Enabled: true, Verified: true}}}
	r := &channelPlatformResolver{backend: slackCompatBackend{links: links}}
	link, err := r.lookupUserChannel(context.Background(), "slack", "T123:U123")
	if err != nil || link == nil || link.UserID != "legacy-owner" {
		t.Fatalf("qualified event should match stored member ID: link=%+v err=%v", link, err)
	}
	if !reflect.DeepEqual(links.lookups, []string{"slack/T123:U123", "slack/U123"}) {
		t.Fatalf("lookups=%v", links.lookups)
	}
	link, err = r.lookupUserChannel(context.Background(), "slack", "U123")
	if err != nil || link == nil || link.UserID != "legacy-owner" {
		t.Fatalf("bare member ID rejected: %+v %v", link, err)
	}
}
