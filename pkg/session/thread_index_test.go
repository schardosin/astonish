package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThreadIndex_LookupEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	_, ok := idx.Lookup("<nonexistent@example.com>")
	if ok {
		t.Error("expected Lookup to return false on empty index")
	}
}

func TestThreadIndex_AssociateAndLookup(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	sessionKey := "email:direct:alice@example.com-a1b2c3d4"
	msgID := "<msg001@example.com>"

	if err := idx.Associate([]string{msgID}, sessionKey); err != nil {
		t.Fatalf("Associate failed: %v", err)
	}

	got, ok := idx.Lookup(msgID)
	if !ok {
		t.Fatal("expected Lookup to return true after Associate")
	}
	if got != sessionKey {
		t.Errorf("Lookup = %q, want %q", got, sessionKey)
	}
}

func TestThreadIndex_AssociateMultiple(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	sessionKey := "email:direct:alice@example.com-a1b2c3d4"
	ids := []string{"<msg001@ex.com>", "<msg002@ex.com>", "<reply001@astonish.com>"}

	if err := idx.Associate(ids, sessionKey); err != nil {
		t.Fatalf("Associate failed: %v", err)
	}

	for _, id := range ids {
		got, ok := idx.Lookup(id)
		if !ok {
			t.Errorf("Lookup(%q) returned false", id)
		}
		if got != sessionKey {
			t.Errorf("Lookup(%q) = %q, want %q", id, got, sessionKey)
		}
	}
}

func TestThreadIndex_LookupChain(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	sessionKey := "email:direct:bob@example.com-deadbeef"
	if err := idx.Associate([]string{"<original@example.com>"}, sessionKey); err != nil {
		t.Fatal(err)
	}

	// Lookup chain: In-Reply-To first (not indexed), then References (has match)
	chain := []string{"<reply-unknown@example.com>", "<original@example.com>"}
	got, ok := idx.LookupChain(chain)
	if !ok {
		t.Fatal("expected LookupChain to find a match")
	}
	if got != sessionKey {
		t.Errorf("LookupChain = %q, want %q", got, sessionKey)
	}
}

func TestThreadIndex_LookupChain_FirstMatch(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	session1 := "email:direct:alice@example.com-1111"
	session2 := "email:direct:alice@example.com-2222"
	_ = idx.Associate([]string{"<msg-A@ex.com>"}, session1)
	_ = idx.Associate([]string{"<msg-B@ex.com>"}, session2)

	// Chain should return first match
	chain := []string{"<msg-B@ex.com>", "<msg-A@ex.com>"}
	got, ok := idx.LookupChain(chain)
	if !ok {
		t.Fatal("expected match")
	}
	if got != session2 {
		t.Errorf("LookupChain = %q, want %q (first match)", got, session2)
	}
}

func TestThreadIndex_LookupChain_Empty(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	_, ok := idx.LookupChain(nil)
	if ok {
		t.Error("expected empty chain to return false")
	}
	_, ok = idx.LookupChain([]string{})
	if ok {
		t.Error("expected empty chain to return false")
	}
}

func TestThreadIndex_RemoveSession(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	session1 := "email:direct:alice@example.com-1111"
	session2 := "email:direct:bob@example.com-2222"

	_ = idx.Associate([]string{"<a1@ex.com>", "<a2@ex.com>"}, session1)
	_ = idx.Associate([]string{"<b1@ex.com>"}, session2)

	// Remove session1 — should remove both a1 and a2, leave b1
	if err := idx.RemoveSession(session1); err != nil {
		t.Fatalf("RemoveSession failed: %v", err)
	}

	if _, ok := idx.Lookup("<a1@ex.com>"); ok {
		t.Error("expected <a1@ex.com> to be removed")
	}
	if _, ok := idx.Lookup("<a2@ex.com>"); ok {
		t.Error("expected <a2@ex.com> to be removed")
	}
	if got, ok := idx.Lookup("<b1@ex.com>"); !ok || got != session2 {
		t.Errorf("expected <b1@ex.com> to remain mapped to %q", session2)
	}
}

func TestThreadIndex_RemoveSession_NoOp(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	// Removing from empty index should not error
	if err := idx.RemoveSession("nonexistent"); err != nil {
		t.Errorf("RemoveSession on empty index: %v", err)
	}
}

func TestThreadIndex_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thread_index.json")

	// Write with first index instance
	idx1 := NewThreadIndex(path)
	_ = idx1.Associate([]string{"<persist@ex.com>"}, "email:direct:test-persist")

	// Read with a fresh index instance (simulates daemon restart)
	idx2 := NewThreadIndex(path)
	got, ok := idx2.Lookup("<persist@ex.com>")
	if !ok {
		t.Fatal("expected persisted entry to survive reload")
	}
	if got != "email:direct:test-persist" {
		t.Errorf("got %q after reload", got)
	}
}

func TestThreadIndex_RemoveSession_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thread_index.json")

	idx := NewThreadIndex(path)
	_ = idx.Associate([]string{"<del@ex.com>"}, "email:direct:to-delete")
	_ = idx.RemoveSession("email:direct:to-delete")

	// Reload and verify it's gone
	idx2 := NewThreadIndex(path)
	if _, ok := idx2.Lookup("<del@ex.com>"); ok {
		t.Error("expected removed entry to be gone after reload")
	}
}

func TestThreadIndex_AssociateEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	// Empty slices and keys should be no-ops
	if err := idx.Associate(nil, "session"); err != nil {
		t.Errorf("Associate(nil) = %v", err)
	}
	if err := idx.Associate([]string{}, "session"); err != nil {
		t.Errorf("Associate([]) = %v", err)
	}
	if err := idx.Associate([]string{"<id>"}, ""); err != nil {
		t.Errorf("Associate with empty session key = %v", err)
	}

	// Verify nothing was written
	_, err := os.Stat(filepath.Join(dir, "thread_index.json"))
	if err == nil {
		t.Error("expected no file to be created for empty operations")
	}
}

func TestThreadIndex_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	idx := NewThreadIndex(filepath.Join(dir, "thread_index.json"))

	_ = idx.Associate([]string{"<msg@ex.com>"}, "session-old")
	_ = idx.Associate([]string{"<msg@ex.com>"}, "session-new")

	got, ok := idx.Lookup("<msg@ex.com>")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if got != "session-new" {
		t.Errorf("expected overwrite, got %q", got)
	}
}
