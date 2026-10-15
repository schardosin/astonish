package telegram

import (
	"context"
	"fmt"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestLinkOnlyRealPrivateSenderBeforeAllowlist(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.server.Close()
	bot, err := tgbotapi.NewBotAPIWithClient("test", fake.server.URL+"/bot%s/%s", fake.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	channel := New(&Config{}, nil)
	channel.botAPI = bot
	calls := 0
	channel.SetLinkHandler(func(_ context.Context, sender, name, code string) (bool, string) {
		calls++
		if sender != "12345" || code != "aB_9cD-eF0gH1iJ2kL3mNw" {
			t.Fatalf("wrong principal/code: %q %q", sender, code)
		}
		return false, "not linked"
	})
	for _, tt := range []struct {
		chatType string
		bot      bool
		chatID   int64
		want     int
	}{
		{"group", false, -12345, 0}, {"supergroup", false, -12345, 0}, {"channel", false, -12345, 0},
		{"private", true, 12345, 0}, {"private", false, 99999, 0}, {"private", false, 12345, 1},
	} {
		msg := &tgbotapi.Message{Text: "/link aB_9cD-eF0gH1iJ2kL3mNw", From: &tgbotapi.User{ID: 12345, IsBot: tt.bot}, Chat: &tgbotapi.Chat{ID: tt.chatID, Type: tt.chatType}, Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}}}
		channel.processUpdate(context.Background(), tgbotapi.Update{Message: msg})
		if calls != tt.want {
			t.Fatal(fmt.Sprintf("%+v: handler calls=%d", tt, calls))
		}
	}
	// Incomplete updates are ignored, not panics.
	channel.processUpdate(context.Background(), tgbotapi.Update{Message: &tgbotapi.Message{Text: "/link token"}})
}
