package mailer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SAP/astonish/pkg/email"
)

// mockEmailClient implements email.Client for testing.
type mockEmailClient struct {
	address   string
	sendCount atomic.Int64
	lastMsg   email.OutgoingMessage
	sendErr   error
	mu        sync.Mutex
}

func (m *mockEmailClient) Connect(_ context.Context) error                   { return nil }
func (m *mockEmailClient) Close() error                                      { return nil }
func (m *mockEmailClient) IsConnected() bool                                 { return true }
func (m *mockEmailClient) Address() string                                   { return m.address }
func (m *mockEmailClient) ListMessages(_ context.Context, _ email.ListOpts) ([]email.MessageSummary, error) {
	return nil, nil
}
func (m *mockEmailClient) ReadMessage(_ context.Context, _ string) (*email.Message, error) {
	return nil, nil
}
func (m *mockEmailClient) SearchMessages(_ context.Context, _ email.SearchQuery) ([]email.MessageSummary, error) {
	return nil, nil
}
func (m *mockEmailClient) Reply(_ context.Context, _ string, _ bool, _ email.OutgoingMessage) (string, error) {
	return "", nil
}
func (m *mockEmailClient) MarkRead(_ context.Context, _ []string) error   { return nil }
func (m *mockEmailClient) MarkUnread(_ context.Context, _ []string) error { return nil }
func (m *mockEmailClient) Delete(_ context.Context, _ []string, _ bool) error {
	return nil
}

func (m *mockEmailClient) Send(_ context.Context, msg email.OutgoingMessage) (string, error) {
	m.sendCount.Add(1)
	m.mu.Lock()
	m.lastMsg = msg
	m.mu.Unlock()
	return "msg-id-1", m.sendErr
}

func (m *mockEmailClient) getSendCount() int64 {
	return m.sendCount.Load()
}

func (m *mockEmailClient) getLastMsg() email.OutgoingMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMsg
}

// testMessage implements mailer.Message for testing.
type testMessage struct {
	to       []string
	subject  string
	textBody string
	htmlBody string
}

func (m *testMessage) To() []string   { return m.to }
func (m *testMessage) Subject() string { return m.subject }
func (m *testMessage) TextBody() string { return m.textBody }
func (m *testMessage) HTMLBody() string { return m.htmlBody }

// resetMailer resets the package-level client to nil for test isolation.
func resetMailer() {
	mu.Lock()
	defer mu.Unlock()
	client = nil
}

func TestInit_NilClient(t *testing.T) {
	resetMailer()
	defer resetMailer()

	Init(nil)

	if IsConfigured() {
		t.Error("expected IsConfigured()=false after Init(nil)")
	}
}

func TestSend_NotConfigured(t *testing.T) {
	resetMailer()
	defer resetMailer()

	err := Send(context.Background(), &testMessage{
		to:      []string{"test@example.com"},
		subject: "Test",
	})

	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestInit_WithClient(t *testing.T) {
	resetMailer()
	defer resetMailer()

	mock := &mockEmailClient{address: "bot@example.com"}
	Init(mock)

	if !IsConfigured() {
		t.Error("expected IsConfigured()=true after Init with valid client")
	}

	msg := &testMessage{
		to:       []string{"user@example.com"},
		subject:  "Verification Code",
		textBody: "Your code is ABC123",
	}

	err := Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getSendCount() != 1 {
		t.Errorf("expected 1 send call, got %d", mock.getSendCount())
	}

	lastMsg := mock.getLastMsg()
	if lastMsg.Subject != "Verification Code" {
		t.Errorf("expected subject='Verification Code', got %q", lastMsg.Subject)
	}
}

// TestInit_ReplacesClient is the critical test for the reload bug fix:
// calling Init a second time with a new client must cause subsequent Send()
// calls to use the NEW client, not the old one.
func TestInit_ReplacesClient(t *testing.T) {
	resetMailer()
	defer resetMailer()

	clientA := &mockEmailClient{address: "old@example.com"}
	clientB := &mockEmailClient{address: "new@example.com"}

	// Init with client A
	Init(clientA)

	msg := &testMessage{
		to:      []string{"user@example.com"},
		subject: "First send",
	}

	err := Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}
	if clientA.getSendCount() != 1 {
		t.Fatalf("expected clientA to receive 1 send, got %d", clientA.getSendCount())
	}

	// Re-init with client B (simulates reload with new credentials)
	Init(clientB)

	msg2 := &testMessage{
		to:      []string{"user@example.com"},
		subject: "Second send",
	}

	err = Send(context.Background(), msg2)
	if err != nil {
		t.Fatalf("unexpected error on second send: %v", err)
	}

	// Client B should have received the send
	if clientB.getSendCount() != 1 {
		t.Errorf("expected clientB to receive 1 send, got %d", clientB.getSendCount())
	}

	// Client A should NOT have received the second send
	if clientA.getSendCount() != 1 {
		t.Errorf("expected clientA to still have 1 send, got %d", clientA.getSendCount())
	}

	lastMsgB := clientB.getLastMsg()
	if lastMsgB.Subject != "Second send" {
		t.Errorf("expected clientB last msg subject='Second send', got %q", lastMsgB.Subject)
	}
}

// TestInit_ReplacesClient_SendError verifies that if the new client returns
// an error, it's properly propagated (confirming new client is active).
func TestInit_ReplacesClient_SendError(t *testing.T) {
	resetMailer()
	defer resetMailer()

	clientA := &mockEmailClient{address: "old@example.com"}
	clientB := &mockEmailClient{address: "new@example.com", sendErr: errors.New("auth failed")}

	Init(clientA)
	Init(clientB) // Replace with failing client

	err := Send(context.Background(), &testMessage{
		to:      []string{"user@example.com"},
		subject: "Test",
	})

	if err == nil {
		t.Fatal("expected error from clientB, got nil")
	}
	if err.Error() != "auth failed" {
		t.Errorf("expected 'auth failed' error, got %q", err.Error())
	}
}

// TestInit_ConcurrentSafety verifies that concurrent Init and Send calls
// don't race (should pass with -race flag).
func TestInit_ConcurrentSafety(t *testing.T) {
	resetMailer()
	defer resetMailer()

	var wg sync.WaitGroup

	// Spawn multiple goroutines calling Init with different clients
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mock := &mockEmailClient{address: "client@example.com"}
			Init(mock)
		}(i)
	}

	// Spawn multiple goroutines calling Send concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Send(context.Background(), &testMessage{
				to:      []string{"user@example.com"},
				subject: "Concurrent test",
			})
		}()
	}

	wg.Wait()
	// No race condition panics = pass
}
