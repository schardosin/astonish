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

func TestSlackPlatformAllowlistExcludesLegacyLinks(t *testing.T) {
	var logs bytes.Buffer
	ids := slackPlatformAllowlist([]*store.UserChannel{
		{ExternalID: "U123", Enabled: true, Verified: true},
		{ExternalID: "T123:U123", Enabled: true, Verified: true},
		{ExternalID: "T456:U123", Enabled: true, Verified: false},
	}, log.New(&logs, "", 0))
	if !reflect.DeepEqual(ids, []string{"T123:U123"}) || !strings.Contains(logs.String(), "relink") {
		t.Fatalf("ids=%v warning=%s", ids, logs.String())
	}
}

func TestSlackDeliveryResolverCompatibility(t *testing.T) {
	for _, externalID := range []string{"T123:U123", "U123"} {
		t.Run(externalID, func(t *testing.T) {
			links := &slackCompatLinks{links: []*store.UserChannel{{UserID: "owner", ChannelType: "slack", ExternalID: externalID, Enabled: true, Verified: true}}}
			r := &deliveryResolver{backend: slackCompatBackend{links: links}}
			targets, err := r.ResolveUserChannels(context.Background(), "owner")
			if externalID == "U123" {
				if err == nil || !strings.Contains(err.Error(), "relink") || len(targets) != 0 {
					t.Fatalf("legacy link accepted: %v %v", targets, err)
				}
				return
			}
			want := []scheduler.DeliveryTarget{{ChannelID: "slack", ChatID: externalID}}
			if err != nil || !reflect.DeepEqual(targets, want) {
				t.Fatalf("targets=%v err=%v", targets, err)
			}
		})
	}
}
func TestSlackInboundLookupNeverFallsBackToBareIdentity(t *testing.T) {
	links := &slackCompatLinks{links: []*store.UserChannel{{UserID: "legacy-owner", ChannelType: "slack", ExternalID: "U123", Enabled: true, Verified: true}}}
	r := &channelPlatformResolver{backend: slackCompatBackend{links: links}}
	_, uid, _, err := r.ResolveChannelUser(context.Background(), "slack", "T123:U123")
	if err == nil || uid != "" {
		t.Fatalf("qualified lookup inherited legacy owner: %s %v", uid, err)
	}
	if !reflect.DeepEqual(links.lookups, []string{"slack/T123:U123"}) {
		t.Fatalf("lookups=%v", links.lookups)
	}
	_, uid, _, err = r.ResolveChannelUser(context.Background(), "slack", "U123")
	if err == nil || !strings.Contains(err.Error(), "relink") || uid != "" {
		t.Fatalf("bare identity accepted: %s %v", uid, err)
	}
	_, routeErr := r.ListUserRoutes(context.Background(), "slack", "U123")
	if routeErr == nil || !strings.Contains(routeErr.Error(), "relink") {
		t.Fatalf("legacy context route accepted: %v", routeErr)
	}
	if len(links.lookups) != 1 {
		t.Fatalf("bare lookup reached store: %v", links.lookups)
	}
}
