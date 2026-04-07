package browser

import (
	"testing"
)

func TestEnsureSessionID_FirstCall(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.EnsureSessionID("session-abc")

	m.mu.Lock()
	got := m.sessionID
	m.mu.Unlock()

	if got != "session-abc" {
		t.Errorf("EnsureSessionID first call: got %q, want %q", got, "session-abc")
	}
}

func TestEnsureSessionID_EmptyID(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.EnsureSessionID("session-abc")
	m.EnsureSessionID("")

	m.mu.Lock()
	got := m.sessionID
	m.mu.Unlock()

	if got != "session-abc" {
		t.Errorf("EnsureSessionID with empty: got %q, want original %q", got, "session-abc")
	}
}

func TestEnsureSessionID_SameID_NoReset(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.EnsureSessionID("session-abc")

	// Simulate that a browser was launched
	m.mu.Lock()
	m.containerName = "astn-sess-abc"
	m.containerIP = "10.0.0.1"
	m.config.RemoteCDPURL = "ws://10.0.0.1:9222/devtools/browser/123"
	m.mu.Unlock()

	// Same session ID — should NOT reset
	m.EnsureSessionID("session-abc")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.containerName != "astn-sess-abc" {
		t.Error("same session ID should not reset containerName")
	}
	if m.containerIP != "10.0.0.1" {
		t.Error("same session ID should not reset containerIP")
	}
	if m.config.RemoteCDPURL != "ws://10.0.0.1:9222/devtools/browser/123" {
		t.Error("same session ID should not reset RemoteCDPURL")
	}
}

func TestEnsureSessionID_DifferentID_ResetsState(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.EnsureSessionID("session-abc")

	// Simulate that browser state was set (no real browser to close)
	m.mu.Lock()
	m.containerName = "astn-sess-abc"
	m.containerIP = "10.0.0.1"
	m.config.RemoteCDPURL = "ws://10.0.0.1:9222/devtools/browser/123"
	m.mu.Unlock()

	// Different session ID — should reset
	m.EnsureSessionID("session-def")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionID != "session-def" {
		t.Errorf("new session: got %q, want %q", m.sessionID, "session-def")
	}
	if m.containerName != "" {
		t.Error("new session should reset containerName")
	}
	if m.containerIP != "" {
		t.Error("new session should reset containerIP")
	}
	if m.config.RemoteCDPURL != "" {
		t.Error("new session should reset RemoteCDPURL")
	}
}

func TestEnsureSessionID_ResolvesAlias(t *testing.T) {
	m := NewManager(BrowserConfig{})

	// Set up alias: child-123 → parent-456
	m.AliasSession("child-123", "parent-456")

	// Set initial session to parent
	m.EnsureSessionID("parent-456")

	// Simulate browser state
	m.mu.Lock()
	m.containerName = "astn-sess-parent"
	m.containerIP = "10.0.0.1"
	m.mu.Unlock()

	// Now call with the child session ID — should resolve to parent and NOT reset
	m.EnsureSessionID("child-123")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionID != "parent-456" {
		t.Errorf("alias resolution: got session %q, want %q", m.sessionID, "parent-456")
	}
	if m.containerName != "astn-sess-parent" {
		t.Error("aliased child should not reset containerName")
	}
}

func TestEnsureSessionID_UnknownAlias_TreatedAsNewSession(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.EnsureSessionID("session-abc")

	// EnsureSessionID with an ID that has no alias — should be treated as new session
	m.EnsureSessionID("no-alias-session")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionID != "no-alias-session" {
		t.Errorf("unknown alias: got %q, want %q", m.sessionID, "no-alias-session")
	}
}

func TestAliasSession_Basic(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.AliasSession("child-1", "parent-1")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionAliases == nil {
		t.Fatal("sessionAliases should not be nil after AliasSession")
	}
	if got := m.sessionAliases["child-1"]; got != "parent-1" {
		t.Errorf("alias: got %q, want %q", got, "parent-1")
	}
}

func TestAliasSession_LazyMapInit(t *testing.T) {
	m := NewManager(BrowserConfig{})

	m.mu.Lock()
	if m.sessionAliases != nil {
		t.Error("sessionAliases should be nil before first AliasSession")
	}
	m.mu.Unlock()

	m.AliasSession("child", "parent")

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionAliases == nil {
		t.Error("sessionAliases should be initialized after AliasSession")
	}
}

func TestAliasSession_Overwrite(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.AliasSession("child", "parent-1")
	m.AliasSession("child", "parent-2")

	m.mu.Lock()
	defer m.mu.Unlock()

	if got := m.sessionAliases["child"]; got != "parent-2" {
		t.Errorf("overwritten alias: got %q, want %q", got, "parent-2")
	}
}

func TestAliasSession_MultipleChildren(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.AliasSession("child-1", "parent")
	m.AliasSession("child-2", "parent")
	m.AliasSession("child-3", "other-parent")

	m.mu.Lock()
	defer m.mu.Unlock()

	if got := m.sessionAliases["child-1"]; got != "parent" {
		t.Errorf("child-1 alias: got %q, want %q", got, "parent")
	}
	if got := m.sessionAliases["child-2"]; got != "parent" {
		t.Errorf("child-2 alias: got %q, want %q", got, "parent")
	}
	if got := m.sessionAliases["child-3"]; got != "other-parent" {
		t.Errorf("child-3 alias: got %q, want %q", got, "other-parent")
	}
}
