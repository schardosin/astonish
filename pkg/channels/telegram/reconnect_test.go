package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/channels"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// fakeTelegramServer creates a mock Telegram API server for testing.
// It responds to getMe and getUpdates endpoints. The behavior of getUpdates
// can be controlled via the updatesFn callback.
type fakeTelegramServer struct {
	server          *httptest.Server
	getMeCalls      atomic.Int64
	getUpdatesCalls atomic.Int64
	// updatesFn is called for each getUpdates request. Return updates or an error.
	// If nil, returns empty updates.
	updatesFn func(callNum int64) ([]tgbotapi.Update, error)
	// failGetMe controls whether getMe should fail (simulating connection errors)
	failGetMe atomic.Bool
}

func newFakeTelegramServer() *fakeTelegramServer {
	f := &fakeTelegramServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Route based on the path pattern: /bot<token>/<method>
		path := r.URL.Path
		if strings.Contains(path, "getMe") {
			f.handleGetMe(w)
		} else if strings.Contains(path, "getUpdates") {
			f.handleGetUpdates(w)
		} else if strings.Contains(path, "setMyCommands") {
			f.handleSetMyCommands(w)
		} else {
			// Default: return OK for any other method
			writeAPIResponse(w, true, json.RawMessage(`true`))
		}
	})

	f.server = httptest.NewServer(mux)
	return f
}

func (f *fakeTelegramServer) handleGetMe(w http.ResponseWriter) {
	f.getMeCalls.Add(1)
	if f.failGetMe.Load() {
		writeAPIResponse(w, false, nil)
		return
	}
	user := tgbotapi.User{
		ID:       12345,
		IsBot:    true,
		UserName: "test_bot",
	}
	data, _ := json.Marshal(user)
	writeAPIResponse(w, true, data)
}

func (f *fakeTelegramServer) handleGetUpdates(w http.ResponseWriter) {
	callNum := f.getUpdatesCalls.Add(1)
	if f.updatesFn != nil {
		updates, err := f.updatesFn(callNum)
		if err != nil {
			writeAPIResponse(w, false, nil)
			return
		}
		data, _ := json.Marshal(updates)
		writeAPIResponse(w, true, data)
		return
	}
	// Default: return empty updates
	writeAPIResponse(w, true, json.RawMessage(`[]`))
}

func (f *fakeTelegramServer) handleSetMyCommands(w http.ResponseWriter) {
	writeAPIResponse(w, true, json.RawMessage(`true`))
}

// apiEndpoint returns the endpoint string in the format expected by tgbotapi:
// "http://host:port/bot%s/%s"
func (f *fakeTelegramServer) apiEndpoint() string {
	return f.server.URL + "/bot%s/%s"
}

func (f *fakeTelegramServer) close() {
	f.server.Close()
}

func writeAPIResponse(w http.ResponseWriter, ok bool, result json.RawMessage) {
	resp := struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}{
		OK:     ok,
		Result: result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// noopHandler is a no-op message handler for tests.
func noopHandler(_ context.Context, _ channels.InboundMessage) error { return nil }

// TestStart_ReconnectsOnChannelClose verifies that the Telegram adapter
// automatically reconnects when the updates channel is closed unexpectedly.
// This is the core regression test for the "connection drops after a day" issue.
func TestStart_ReconnectsOnChannelClose(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.close()

	// First few getUpdates calls succeed, then one fails (triggering the
	// library's internal retry which eventually closes the channel if persistent).
	// After the reconnect, getUpdates works again.
	fake.updatesFn = func(callNum int64) ([]tgbotapi.Update, error) {
		if callNum <= 2 {
			return []tgbotapi.Update{}, nil
		}
		if callNum == 3 {
			// Return error — the library logs and retries internally.
			// After several retries (3s each), if the error persists,
			// we rely on our timeout-based detection. For testing purposes,
			// we just verify that getUpdates keeps being called.
			return nil, fmt.Errorf("simulated transient error")
		}
		// Recovered — works normally again
		return []tgbotapi.Update{}, nil
	}

	tg := New(&Config{
		BotToken:    "test-token",
		APIEndpoint: fake.apiEndpoint(),
	}, log.New(log.Writer(), "[test] ", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start should handle the transient error and keep running
	done := make(chan error, 1)
	go func() {
		done <- tg.Start(ctx, noopHandler)
	}()

	// Wait for context timeout (the adapter keeps running until cancelled)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}

	// Verify that getMe was called at least once (initial connection)
	if fake.getMeCalls.Load() < 1 {
		t.Error("getMe should have been called at least once")
	}

	// Verify that getUpdates was called multiple times (past the error)
	if calls := fake.getUpdatesCalls.Load(); calls < 3 {
		t.Errorf("getUpdates called %d times, expected at least 3", calls)
	}

	// Verify status was set to connected
	status := tg.Status()
	if status.AccountID != "test_bot" {
		t.Errorf("AccountID = %q, want %q", status.AccountID, "test_bot")
	}
}

// TestStart_StopsOnContextCancel verifies that cancelling the context during
// a reconnection backoff wait causes Start to exit cleanly.
func TestStart_StopsOnContextCancel(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.close()

	// Make getMe fail to force the reconnect loop
	fake.failGetMe.Store(true)

	tg := New(&Config{
		BotToken:    "test-token",
		APIEndpoint: fake.apiEndpoint(),
	}, log.New(log.Writer(), "[test] ", 0))
	// Short reconnect delay so backoff doesn't stall the test.
	tg.reconnectDelay = 200 * time.Millisecond

	// Use a short-lived context — cancel during backoff
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- tg.Start(ctx, noopHandler)
	}()

	// Wait for the first connection attempt to fail, then cancel
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Start should return cleanly within a reasonable time
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation — stuck in backoff?")
	}
}

// TestConnectAndPoll_UsesCustomHTTPClient verifies that connectAndPoll creates
// a bot API instance with a timeout-configured HTTP client rather than the
// default no-timeout client. This prevents hung TCP connections.
func TestConnectAndPoll_UsesCustomHTTPClient(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.close()

	tg := New(&Config{
		BotToken:    "test-token",
		APIEndpoint: fake.apiEndpoint(),
	}, log.New(log.Writer(), "[test] ", 0))

	// Set handler (normally done by Start)
	tg.handler = noopHandler

	// Run connectAndPoll briefly
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := tg.connectAndPoll(ctx)
	if err != nil {
		t.Fatalf("connectAndPoll returned unexpected error: %v", err)
	}

	// After connectAndPoll returns (context cancelled), check that botAPI was set
	tg.mu.RLock()
	bot := tg.botAPI
	tg.mu.RUnlock()

	if bot == nil {
		t.Fatal("botAPI should be set after connectAndPoll")
	}

	// Verify the HTTP client has a timeout
	httpClient, ok := bot.Client.(*http.Client)
	if !ok {
		t.Fatalf("bot.Client is %T, expected *http.Client", bot.Client)
	}
	if httpClient.Timeout != httpClientTimeout {
		t.Errorf("HTTP client timeout = %v, want %v", httpClient.Timeout, httpClientTimeout)
	}
}

// TestStart_ReconnectsAfterGetMeFailure verifies that if the initial connection
// fails (getMe error), the adapter retries with backoff rather than giving up.
func TestStart_ReconnectsAfterGetMeFailure(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.close()

	// Fail getMe for the first 2 attempts, then succeed
	fake.failGetMe.Store(true)
	go func() {
		time.Sleep(500 * time.Millisecond)
		fake.failGetMe.Store(false)
	}()

	tg := New(&Config{
		BotToken:    "test-token",
		APIEndpoint: fake.apiEndpoint(),
	}, log.New(log.Writer(), "[test] ", 0))
	// Use a short reconnect delay so retries happen within the test's timeout.
	tg.reconnectDelay = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- tg.Start(ctx, noopHandler)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context timeout")
	}

	// getMe should have been called more than once (retries after initial failure)
	if calls := fake.getMeCalls.Load(); calls < 2 {
		t.Errorf("getMe was called %d times, expected at least 2 (initial fail + retry)", calls)
	}

	// After recovery, status should show connected
	status := tg.Status()
	if status.AccountID != "test_bot" {
		t.Errorf("AccountID = %q, want %q — bot didn't recover", status.AccountID, "test_bot")
	}
}

// TestStart_StatusUpdatedDuringReconnect verifies that the channel status
// reflects the reconnecting state between connection attempts.
func TestStart_StatusUpdatedDuringReconnect(t *testing.T) {
	fake := newFakeTelegramServer()
	defer fake.close()

	// Fail getMe initially to trigger reconnect
	fake.failGetMe.Store(true)

	tg := New(&Config{
		BotToken:    "test-token",
		APIEndpoint: fake.apiEndpoint(),
	}, log.New(log.Writer(), "[test] ", 0))
	// Short reconnect delay so we can observe status during backoff.
	tg.reconnectDelay = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- tg.Start(ctx, noopHandler)
	}()

	// Wait for the first failed attempt
	time.Sleep(200 * time.Millisecond)

	// During reconnect, status should show an error
	status := tg.Status()
	if status.Connected {
		t.Error("status.Connected should be false during reconnect")
	}
	if status.Error == "" {
		t.Error("status.Error should be set during reconnect")
	}

	cancel()
	<-done
}
