package channels

import (
	"testing"
)

func TestRouter_Route(t *testing.T) {
	r := NewRouter()

	t.Run("direct message session key", func(t *testing.T) {
		msg := InboundMessage{
			ChannelID: "telegram",
			ChatType:  ChatTypeDirect,
			ChatID:    "12345",
		}
		result := r.Route(msg)
		want := "telegram:direct:12345"
		if result.SessionKey != want {
			t.Errorf("Route() SessionKey = %q, want %q", result.SessionKey, want)
		}
	})

	t.Run("group message session key", func(t *testing.T) {
		msg := InboundMessage{
			ChannelID: "telegram",
			ChatType:  ChatTypeGroup,
			ChatID:    "67890",
		}
		result := r.Route(msg)
		want := "telegram:group:67890"
		if result.SessionKey != want {
			t.Errorf("Route() SessionKey = %q, want %q", result.SessionKey, want)
		}
	})

	t.Run("different channel IDs produce different keys", func(t *testing.T) {
		msg1 := InboundMessage{
			ChannelID: "telegram",
			ChatType:  ChatTypeDirect,
			ChatID:    "100",
		}
		msg2 := InboundMessage{
			ChannelID: "slack",
			ChatType:  ChatTypeDirect,
			ChatID:    "100",
		}
		r1 := r.Route(msg1)
		r2 := r.Route(msg2)
		if r1.SessionKey == r2.SessionKey {
			t.Errorf("different channels should produce different keys, both got %q", r1.SessionKey)
		}
	})

	t.Run("same inputs produce same key", func(t *testing.T) {
		msg := InboundMessage{
			ChannelID: "email",
			ChatType:  ChatTypeDirect,
			ChatID:    "user@example.com",
		}
		r1 := r.Route(msg)
		r2 := r.Route(msg)
		if r1.SessionKey != r2.SessionKey {
			t.Errorf("same inputs should produce same key, got %q and %q", r1.SessionKey, r2.SessionKey)
		}
	})
}
