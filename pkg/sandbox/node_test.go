package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestLazyNodeClientCloseBeforeBindSession(t *testing.T) {
	// Close() before BindSession() should not deadlock on subsequent Call()
	lnc := &LazyNodeClient{}

	if err := lnc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Call() should return an error immediately, not deadlock
	done := make(chan struct{})
	var callErr error
	go func() {
		defer close(done)
		_, callErr = lnc.Call("some-session", "some-tool", nil)
	}()

	select {
	case <-done:
		if callErr == nil {
			t.Fatal("expected error from Call after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call() deadlocked after Close()")
	}
}

func TestLazyNodeClientCloseBeforeEnsureReady(t *testing.T) {
	lnc := &LazyNodeClient{}

	if err := lnc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	var readyErr error
	go func() {
		defer close(done)
		_, readyErr = lnc.EnsureReady("some-session")
	}()

	select {
	case <-done:
		if readyErr == nil {
			t.Fatal("expected error from EnsureReady after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureReady() deadlocked after Close()")
	}
}

func TestLazyNodeClientCloseBeforeEnsureContainerReady(t *testing.T) {
	lnc := &LazyNodeClient{}

	if err := lnc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	var readyErr error
	go func() {
		defer close(done)
		_, readyErr = lnc.EnsureContainerReady("some-session")
	}()

	select {
	case <-done:
		if readyErr == nil {
			t.Fatal("expected error from EnsureContainerReady after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureContainerReady() deadlocked after Close()")
	}
}

func TestLazyNodeClientCallEmptySessionID(t *testing.T) {
	lnc := &LazyNodeClient{}

	done := make(chan struct{})
	var callErr error
	go func() {
		defer close(done)
		_, callErr = lnc.Call("", "some-tool", nil)
	}()

	select {
	case <-done:
		if callErr == nil {
			t.Fatal("expected error from Call with empty session ID")
		}
		if !strings.Contains(callErr.Error(), "no session bound") {
			t.Errorf("unexpected error: %v", callErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call() deadlocked with empty session ID")
	}
}

func TestLazyNodeClientDoubleClose(t *testing.T) {
	lnc := &LazyNodeClient{}

	if err := lnc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lnc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestLazyNodeClientBindSessionIdempotent(t *testing.T) {
	// BindSession with a closed client should not panic
	lnc := &LazyNodeClient{}
	_ = lnc.Close()

	// Should not panic, even when closed
	lnc.BindSession("session-1")
	lnc.BindSession("session-2")
}

func TestLazyNodeClientBindSessionEmpty(t *testing.T) {
	lnc := &LazyNodeClient{}

	// Empty session ID should be no-op
	lnc.BindSession("")

	lnc.mu.Lock()
	hasInitDone := lnc.initDone != nil
	lnc.mu.Unlock()

	if hasInitDone {
		t.Error("BindSession('') should not create initDone")
	}

	_ = lnc.Close()
}

func TestNodeClientPoolGetOrCreateNilForEmpty(t *testing.T) {
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	client := pool.GetOrCreate("")
	if client != nil {
		t.Error("expected nil for empty session ID")
	}
}

func TestNodeClientPoolGetOrCreateReuse(t *testing.T) {
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	c1 := pool.GetOrCreate("sess-1")
	c2 := pool.GetOrCreate("sess-1")

	if c1 != c2 {
		t.Error("expected same client for same session ID")
	}
}

func TestNodeClientPoolGetOrCreateClosed(t *testing.T) {
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
		closed:  true,
	}

	client := pool.GetOrCreate("sess-1")
	if client != nil {
		t.Error("expected nil from closed pool")
	}
}

func TestNodeClientPoolRemove(t *testing.T) {
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	_ = pool.GetOrCreate("sess-1")
	pool.Remove("sess-1")

	// Next GetOrCreate should create a new one
	c := pool.GetOrCreate("sess-1")
	if c == nil {
		t.Error("expected new client after Remove")
	}
}

func TestNodeClientPoolAlias(t *testing.T) {
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	parent := pool.GetOrCreate("parent")
	pool.Alias("child", "parent")

	child := pool.GetOrCreate("child")
	if child != parent {
		t.Error("expected alias to return same client as parent")
	}
}

func TestNodeClientPoolReplaceSession_UpdatesAliases(t *testing.T) {
	// This is the core regression test for the "lazy node client is closed" bug.
	// When use_sandbox_template runs in a sub-agent (child session), ReplaceSession
	// must update ALL session IDs that alias to the same client — including the parent.
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	// Create parent client
	parentClient := pool.GetOrCreate("parent")
	if parentClient == nil {
		t.Fatal("expected non-nil parent client")
	}

	// Alias child to parent (simulating sub-agent creation)
	pool.Alias("child", "parent")

	// Verify both point to the same client
	childClient := pool.GetOrCreate("child")
	if childClient != parentClient {
		t.Fatal("expected child to alias to parent client")
	}

	// ReplaceSession called with child's session ID (as use_sandbox_template does
	// when running in a sub-agent)
	err := pool.ReplaceSession("child", "new-template")
	if err != nil {
		t.Fatalf("ReplaceSession: %v", err)
	}

	// The old client should be closed
	if !parentClient.IsClosed() {
		t.Error("old client should be closed after ReplaceSession")
	}

	// Both parent AND child should now point to the NEW client
	newParent := pool.GetOrCreate("parent")
	newChild := pool.GetOrCreate("child")

	if newParent == nil || newChild == nil {
		t.Fatal("expected non-nil clients after ReplaceSession")
	}
	if newParent == parentClient {
		t.Error("parent should have a new client, not the destroyed old one")
	}
	if newChild == parentClient {
		t.Error("child should have a new client, not the destroyed old one")
	}
	if newParent != newChild {
		t.Error("parent and child should share the same new client after ReplaceSession")
	}
}

func TestNodeClientPoolReplaceSession_NoAlias(t *testing.T) {
	// ReplaceSession on a non-aliased session should work normally
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	old := pool.GetOrCreate("sess-1")
	_ = pool.GetOrCreate("sess-2") // unrelated session

	err := pool.ReplaceSession("sess-1", "new-template")
	if err != nil {
		t.Fatalf("ReplaceSession: %v", err)
	}

	// sess-1 should have a new client
	newClient := pool.GetOrCreate("sess-1")
	if newClient == old {
		t.Error("expected new client after ReplaceSession")
	}

	// sess-2 should be unaffected
	sess2 := pool.GetOrCreate("sess-2")
	if sess2 == nil {
		t.Error("sess-2 should still have a client")
	}
}

func TestNodeClientPoolGetOrCreate_DiscardsClosedClient(t *testing.T) {
	// Safety net: GetOrCreate should discard a closed client and create a new one
	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	client := pool.GetOrCreate("sess-1")
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Manually close the client (simulating the bug scenario where an alias
	// was destroyed but the pool entry wasn't updated)
	_ = client.Close()

	// GetOrCreate should detect the closed client and create a fresh one
	newClient := pool.GetOrCreate("sess-1")
	if newClient == nil {
		t.Fatal("expected new client after closed detection")
	}
	if newClient == client {
		t.Error("expected a different client, not the closed one")
	}
}

func TestLazyNodeClientIsClosed(t *testing.T) {
	lnc := &LazyNodeClient{}

	if lnc.IsClosed() {
		t.Error("new client should not be closed")
	}

	_ = lnc.Close()

	if !lnc.IsClosed() {
		t.Error("client should be closed after Close()")
	}
}
