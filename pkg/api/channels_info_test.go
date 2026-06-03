package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schardosin/astonish/pkg/channels"
)

// mockChannel is a minimal Channel implementation for testing.
type mockChannel struct {
	id     string
	status channels.ChannelStatus
}

func (m *mockChannel) ID() string { return m.id }
func (m *mockChannel) Name() string { return m.id }
func (m *mockChannel) Start(_ context.Context, _ channels.MessageHandler) error { return nil }
func (m *mockChannel) Stop(_ context.Context) error { return nil }
func (m *mockChannel) Send(_ context.Context, _ channels.Target, _ channels.OutboundMessage) error {
	return nil
}
func (m *mockChannel) BroadcastTargets() []channels.Target          { return nil }
func (m *mockChannel) SendTyping(_ context.Context, _ channels.Target) error { return nil }
func (m *mockChannel) Status() channels.ChannelStatus               { return m.status }

// For Telegram: implements BotUsernameProvider
func (m *mockChannel) BotUsername() string { return m.status.AccountID }

// channelInfoResponse matches the JSON returned by handleGetChannelInfo.
type channelInfoResponse struct {
	Telegram struct {
		BotUsername string `json:"bot_username"`
		Configured  bool   `json:"configured"`
		Connected   bool   `json:"connected"`
		Enabled     bool   `json:"enabled"`
		Error       string `json:"error"`
	} `json:"telegram"`
	Email struct {
		Configured bool   `json:"configured"`
		Connected  bool   `json:"connected"`
		Enabled    bool   `json:"enabled"`
		Error      string `json:"error"`
		Address    string `json:"address"`
	} `json:"email"`
	Slack struct {
		BotUserID  string `json:"bot_user_id"`
		Configured bool   `json:"configured"`
		Connected  bool   `json:"connected"`
		Enabled    bool   `json:"enabled"`
		Error      string `json:"error"`
	} `json:"slack"`
}

func callChannelInfo(t *testing.T) channelInfoResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/channels/info", nil)
	w := httptest.NewRecorder()
	handleGetChannelInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp channelInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	return resp
}

// TestChannelInfo_EmailEnabledNotConnected tests the primary bug scenario:
// email channel is enabled in config (admin set it up) but IMAP hasn't
// connected yet. The "configured" field should be true so the UI shows
// the "Link Email" button.
func TestChannelInfo_EmailEnabledNotConnected(t *testing.T) {
	// Save and restore global state
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	// Simulate: email enabled in config, no channel manager connected
	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"email": {Enabled: true, Error: ""},
	})
	SetChannelManager(nil) // No channel manager yet (or email not connected)

	resp := callChannelInfo(t)

	if !resp.Email.Configured {
		t.Error("expected email.configured=true when enabled in config (even if not connected)")
	}
	if resp.Email.Connected {
		t.Error("expected email.connected=false when channel manager is nil")
	}
	if !resp.Email.Enabled {
		t.Error("expected email.enabled=true")
	}
}

// TestChannelInfo_EmailEnabledAndConnected tests steady state:
// email is enabled and the IMAP connection has completed.
func TestChannelInfo_EmailEnabledAndConnected(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"email": {Enabled: true, Error: ""},
	})

	// Create a channel manager with a mock email channel that is connected
	mgr := channels.NewChannelManager(nil, nil, log.Default(), nil)
	mgr.Register(&mockChannel{
		id:     "email",
		status: channels.ChannelStatus{Connected: true, AccountID: "bot@example.com"},
	})
	SetChannelManager(mgr)

	resp := callChannelInfo(t)

	if !resp.Email.Configured {
		t.Error("expected email.configured=true")
	}
	if !resp.Email.Connected {
		t.Error("expected email.connected=true")
	}
	if !resp.Email.Enabled {
		t.Error("expected email.enabled=true")
	}
	if resp.Email.Address != "bot@example.com" {
		t.Errorf("expected email.address='bot@example.com', got %q", resp.Email.Address)
	}
}

// TestChannelInfo_EmailNotConfigured tests when no email config exists at all.
func TestChannelInfo_EmailNotConfigured(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	// No email in config statuses at all
	SetChannelConfigStatuses(map[string]ChannelConfigStatus{})
	SetChannelManager(nil)

	resp := callChannelInfo(t)

	if resp.Email.Configured {
		t.Error("expected email.configured=false when not in config")
	}
	if resp.Email.Connected {
		t.Error("expected email.connected=false")
	}
	if resp.Email.Enabled {
		t.Error("expected email.enabled=false")
	}
}

// TestChannelInfo_EmailEnabledWithError tests when email is enabled but has
// a config error (e.g., "password not configured"). Should still show
// configured=true so the UI recognizes the admin has set it up.
func TestChannelInfo_EmailEnabledWithError(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"email": {Enabled: true, Error: "password not configured"},
	})
	SetChannelManager(nil)

	resp := callChannelInfo(t)

	if !resp.Email.Configured {
		t.Error("expected email.configured=true even with error (admin set it up)")
	}
	if resp.Email.Connected {
		t.Error("expected email.connected=false when there's a config error")
	}
	if !resp.Email.Enabled {
		t.Error("expected email.enabled=true")
	}
	if resp.Email.Error != "password not configured" {
		t.Errorf("expected error='password not configured', got %q", resp.Email.Error)
	}
}

// TestChannelInfo_TelegramEnabledNotConnected tests the same race condition
// for Telegram: enabled but bot hasn't authenticated yet.
func TestChannelInfo_TelegramEnabledNotConnected(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"telegram": {Enabled: true, Error: ""},
	})
	SetChannelManager(nil)

	resp := callChannelInfo(t)

	if !resp.Telegram.Configured {
		t.Error("expected telegram.configured=true when enabled in config")
	}
	if resp.Telegram.Connected {
		t.Error("expected telegram.connected=false when not yet authenticated")
	}
	if !resp.Telegram.Enabled {
		t.Error("expected telegram.enabled=true")
	}
}

// TestChannelInfo_TelegramConnected tests Telegram fully connected state.
func TestChannelInfo_TelegramConnected(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"telegram": {Enabled: true, Error: ""},
	})

	mgr := channels.NewChannelManager(nil, nil, log.Default(), nil)
	mgr.Register(&mockChannel{
		id:     "telegram",
		status: channels.ChannelStatus{Connected: true, AccountID: "testbot"},
	})
	SetChannelManager(mgr)

	resp := callChannelInfo(t)

	if !resp.Telegram.Configured {
		t.Error("expected telegram.configured=true")
	}
	if !resp.Telegram.Connected {
		t.Error("expected telegram.connected=true")
	}
	if resp.Telegram.BotUsername != "testbot" {
		t.Errorf("expected bot_username='testbot', got %q", resp.Telegram.BotUsername)
	}
}

// TestChannelInfo_SlackEnabledNotConnected tests Slack enabled but not connected.
func TestChannelInfo_SlackEnabledNotConnected(t *testing.T) {
	origStatuses := getChannelConfigStatuses()
	origMgr := GetChannelManager()
	defer func() {
		SetChannelConfigStatuses(origStatuses)
		SetChannelManager(origMgr)
	}()

	SetChannelConfigStatuses(map[string]ChannelConfigStatus{
		"slack": {Enabled: true, Error: ""},
	})
	SetChannelManager(nil)

	resp := callChannelInfo(t)

	if !resp.Slack.Configured {
		t.Error("expected slack.configured=true when enabled in config")
	}
	if resp.Slack.Connected {
		t.Error("expected slack.connected=false")
	}
	if !resp.Slack.Enabled {
		t.Error("expected slack.enabled=true")
	}
}
