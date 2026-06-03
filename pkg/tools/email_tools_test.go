package tools

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/schardosin/astonish/pkg/email"
)

// mockEmailClient implements email.Client for testing.
type mockEmailClient struct {
	address   string
	sendCount atomic.Int64
}

func (m *mockEmailClient) Connect(_ context.Context) error   { return nil }
func (m *mockEmailClient) Close() error                      { return nil }
func (m *mockEmailClient) IsConnected() bool                 { return true }
func (m *mockEmailClient) Address() string                   { return m.address }
func (m *mockEmailClient) ListMessages(_ context.Context, _ email.ListOpts) ([]email.MessageSummary, error) {
	return nil, nil
}
func (m *mockEmailClient) ReadMessage(_ context.Context, _ string) (*email.Message, error) {
	return nil, nil
}
func (m *mockEmailClient) SearchMessages(_ context.Context, _ email.SearchQuery) ([]email.MessageSummary, error) {
	return nil, nil
}
func (m *mockEmailClient) Send(_ context.Context, _ email.OutgoingMessage) (string, error) {
	m.sendCount.Add(1)
	return "msg-id", nil
}
func (m *mockEmailClient) Reply(_ context.Context, _ string, _ bool, _ email.OutgoingMessage) (string, error) {
	return "", nil
}
func (m *mockEmailClient) MarkRead(_ context.Context, _ []string) error   { return nil }
func (m *mockEmailClient) MarkUnread(_ context.Context, _ []string) error { return nil }
func (m *mockEmailClient) Delete(_ context.Context, _ []string, _ bool) error {
	return nil
}

func resetEmailClient() {
	emailClient = nil
}

func TestHasEmailClient_InitiallyFalse(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	if HasEmailClient() {
		t.Error("expected HasEmailClient()=false before any SetEmailClient call")
	}
}

func TestSetEmailClient_MakesHasEmailClientTrue(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	mock := &mockEmailClient{address: "test@example.com"}
	SetEmailClient(mock)

	if !HasEmailClient() {
		t.Error("expected HasEmailClient()=true after SetEmailClient")
	}
}

func TestSetEmailClient_NilResetsState(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	mock := &mockEmailClient{address: "test@example.com"}
	SetEmailClient(mock)

	if !HasEmailClient() {
		t.Fatal("precondition failed: expected HasEmailClient()=true")
	}

	SetEmailClient(nil)

	if HasEmailClient() {
		t.Error("expected HasEmailClient()=false after SetEmailClient(nil)")
	}
}

// TestSetEmailClient_ReplacesClient is the critical test for the reload fix:
// calling SetEmailClient a second time must replace the underlying client
// so that tools use the new credentials.
func TestSetEmailClient_ReplacesClient(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	clientA := &mockEmailClient{address: "old@example.com"}
	clientB := &mockEmailClient{address: "new@example.com"}

	SetEmailClient(clientA)

	// Verify clientA is active
	if emailClient.Address() != "old@example.com" {
		t.Fatalf("expected emailClient address to be 'old@example.com', got %q", emailClient.Address())
	}

	// Replace with clientB (simulates config reload)
	SetEmailClient(clientB)

	// Verify clientB is now active
	if emailClient.Address() != "new@example.com" {
		t.Errorf("expected emailClient address to be 'new@example.com', got %q", emailClient.Address())
	}
}

// TestGetEmailTools_NilClient verifies no tools are returned when client is nil.
func TestGetEmailTools_NilClient(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	tools, err := GetEmailTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tools != nil {
		t.Errorf("expected nil tools when no client configured, got %d tools", len(tools))
	}
}

// TestGetEmailTools_WithClient verifies tools are returned when client is set.
func TestGetEmailTools_WithClient(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	mock := &mockEmailClient{address: "bot@example.com"}
	SetEmailClient(mock)

	tools, err := GetEmailTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tools == nil || len(tools) == 0 {
		t.Error("expected non-empty tools list when client is configured")
	}
}

// TestGetEmailTools_AfterReplace verifies that GetEmailTools picks up the
// new client after SetEmailClient is called again (reload scenario).
func TestGetEmailTools_AfterReplace(t *testing.T) {
	resetEmailClient()
	defer resetEmailClient()

	clientA := &mockEmailClient{address: "old@example.com"}
	SetEmailClient(clientA)

	toolsA, err := GetEmailTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolsA == nil {
		t.Fatal("expected tools with clientA")
	}

	// Replace client
	clientB := &mockEmailClient{address: "new@example.com"}
	SetEmailClient(clientB)

	toolsB, err := GetEmailTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolsB == nil {
		t.Fatal("expected tools with clientB")
	}

	// The tools should still be functional (non-nil)
	// Note: the tools created by GetEmailTools capture emailClient at call time,
	// but the package variable is what matters for new calls to GetEmailTools
	if !HasEmailClient() {
		t.Error("expected HasEmailClient()=true after replacement")
	}
}
