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
