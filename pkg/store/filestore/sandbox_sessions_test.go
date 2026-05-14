package filestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

func newSessionStore(t *testing.T) (store.SandboxSessionStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSandboxSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSandboxSessionStore: %v", err)
	}
	return s, dir
}

func TestSandboxSessionStore_PutGetDelete(t *testing.T) {
	s, _ := newSessionStore(t)
	ctx := context.Background()

	sess := &store.SandboxSession{
		SessionID:     "sess-1",
		ContainerName: "astn-sess-1",
		TemplateID:    "tpl-a",
		State:         store.SandboxSessionStateRunning,
		ExposedPorts:  []int{8080, 5173},
		BaseDomain:    "sandbox.local",
	}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get: nil row")
	}
	if got.ContainerName != "astn-sess-1" || got.TemplateID != "tpl-a" {
		t.Fatalf("Get: unexpected row: %+v", got)
	}
	if got.ChatSessionID != "sess-1" {
		t.Fatalf("ChatSessionID should default to SessionID; got %q", got.ChatSessionID)
	}
	if got.Backend != "incus" {
		t.Fatalf("Backend default: got %q; want incus", got.Backend)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() || got.LastActiveAt.IsZero() {
		t.Fatalf("timestamps should be populated: %+v", got)
	}

	// Put again preserves CreatedAt.
	earlier := got.CreatedAt
	time.Sleep(5 * time.Millisecond)
	sess2 := *sess
	sess2.State = store.SandboxSessionStateEvicted
	if err := s.Put(ctx, &sess2); err != nil {
		t.Fatalf("Put (replace): %v", err)
	}
	got2, _ := s.Get(ctx, "sess-1")
	if !got2.CreatedAt.Equal(earlier) {
		t.Fatalf("CreatedAt should be preserved across replace: was %v, now %v", earlier, got2.CreatedAt)
	}
	if !got2.UpdatedAt.After(earlier) {
		t.Fatalf("UpdatedAt should advance on replace: %v vs %v", got2.UpdatedAt, earlier)
	}

	// Delete is idempotent.
	if err := s.Delete(ctx, "sess-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, "sess-1"); err != nil {
		t.Fatalf("Delete (second): %v", err)
	}
	if got, _ := s.Get(ctx, "sess-1"); got != nil {
		t.Fatalf("Get after Delete: expected nil, got %+v", got)
	}
}

func TestSandboxSessionStore_GetByContainerName(t *testing.T) {
	s, _ := newSessionStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &store.SandboxSession{SessionID: "a", ContainerName: "cn-a", TemplateID: "t"})
	_ = s.Put(ctx, &store.SandboxSession{SessionID: "b", ContainerName: "cn-b", TemplateID: "t"})

	got, err := s.GetByContainerName(ctx, "cn-b")
	if err != nil || got == nil || got.SessionID != "b" {
		t.Fatalf("GetByContainerName: got %+v err=%v", got, err)
	}
	if got, _ := s.GetByContainerName(ctx, "missing"); got != nil {
		t.Fatalf("GetByContainerName missing: expected nil, got %+v", got)
	}
	if got, _ := s.GetByContainerName(ctx, ""); got != nil {
		t.Fatalf("GetByContainerName empty: expected nil, got %+v", got)
	}
}

func TestSandboxSessionStore_ListFilters(t *testing.T) {
	s, _ := newSessionStore(t)
	ctx := context.Background()

	pinned := true
	unpinned := false

	rows := []*store.SandboxSession{
		{SessionID: "r1", TemplateID: "t", State: store.SandboxSessionStateRunning, CreatedBy: "u1", Pinned: true},
		{SessionID: "r2", TemplateID: "t", State: store.SandboxSessionStateRunning, CreatedBy: "u2"},
		{SessionID: "e1", TemplateID: "t", State: store.SandboxSessionStateEvicted, CreatedBy: "u1"},
	}
	for _, r := range rows {
		if err := s.Put(ctx, r); err != nil {
			t.Fatalf("Put %s: %v", r.SessionID, err)
		}
	}

	all, _ := s.List(ctx, store.SandboxSessionFilter{})
	if len(all) != 3 {
		t.Fatalf("List all: got %d, want 3", len(all))
	}

	running, _ := s.List(ctx, store.SandboxSessionFilter{State: store.SandboxSessionStateRunning})
	if len(running) != 2 {
		t.Fatalf("List running: got %d, want 2", len(running))
	}

	byUser, _ := s.List(ctx, store.SandboxSessionFilter{CreatedBy: "u1"})
	if len(byUser) != 2 {
		t.Fatalf("List u1: got %d, want 2", len(byUser))
	}

	pinnedList, _ := s.List(ctx, store.SandboxSessionFilter{Pinned: &pinned})
	if len(pinnedList) != 1 || pinnedList[0].SessionID != "r1" {
		t.Fatalf("List pinned: got %+v, want [r1]", pinnedList)
	}

	unpinnedList, _ := s.List(ctx, store.SandboxSessionFilter{Pinned: &unpinned})
	if len(unpinnedList) != 2 {
		t.Fatalf("List unpinned: got %d, want 2", len(unpinnedList))
	}
}

func TestSandboxSessionStore_Mutators(t *testing.T) {
	s, _ := newSessionStore(t)
	ctx := context.Background()

	if err := s.Put(ctx, &store.SandboxSession{SessionID: "m", TemplateID: "t"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.UpdateState(ctx, "m", store.SandboxSessionStateEvicted); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := s.UpdatePorts(ctx, "m", []int{80, 443}); err != nil {
		t.Fatalf("UpdatePorts: %v", err)
	}
	if err := s.SetBaseDomain(ctx, "m", "x.example"); err != nil {
		t.Fatalf("SetBaseDomain: %v", err)
	}
	if err := s.SetPinned(ctx, "m", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if err := s.SetUpperLayer(ctx, "m", "layer-abc"); err != nil {
		t.Fatalf("SetUpperLayer: %v", err)
	}

	got, _ := s.Get(ctx, "m")
	if got.State != store.SandboxSessionStateEvicted {
		t.Fatalf("State: got %q", got.State)
	}
	if len(got.ExposedPorts) != 2 || got.ExposedPorts[0] != 80 || got.ExposedPorts[1] != 443 {
		t.Fatalf("ExposedPorts: got %+v", got.ExposedPorts)
	}
	if got.BaseDomain != "x.example" {
		t.Fatalf("BaseDomain: got %q", got.BaseDomain)
	}
	if !got.Pinned {
		t.Fatal("Pinned should be true")
	}
	if got.UpperLayerID != "layer-abc" {
		t.Fatalf("UpperLayerID: got %q", got.UpperLayerID)
	}

	// Clearing upper layer
	if err := s.SetUpperLayer(ctx, "m", ""); err != nil {
		t.Fatalf("SetUpperLayer clear: %v", err)
	}
	got, _ = s.Get(ctx, "m")
	if got.UpperLayerID != "" {
		t.Fatalf("UpperLayerID clear: got %q", got.UpperLayerID)
	}

	// Missing session
	if err := s.UpdateState(ctx, "nope", store.SandboxSessionStateRunning); err == nil {
		t.Fatal("UpdateState on missing should error")
	}
	if err := s.UpdatePorts(ctx, "nope", nil); err == nil {
		t.Fatal("UpdatePorts on missing should error")
	}
}

func TestSandboxSessionStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s1, err := NewSandboxSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSandboxSessionStore: %v", err)
	}
	if err := s1.Put(ctx, &store.SandboxSession{
		SessionID:     "p",
		ContainerName: "cn-p",
		TemplateID:    "t",
		State:         store.SandboxSessionStateRunning,
		ExposedPorts:  []int{3000},
		Pinned:        true,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Verify the file is actually on disk.
	path := filepath.Join(dir, "sandbox_sessions.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sandbox_sessions.json should exist: %v", err)
	}

	s2, err := NewSandboxSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSandboxSessionStore (reload): %v", err)
	}
	got, err := s2.Get(ctx, "p")
	if err != nil || got == nil {
		t.Fatalf("Get after reload: got=%+v err=%v", got, err)
	}
	if got.ContainerName != "cn-p" || !got.Pinned || len(got.ExposedPorts) != 1 || got.ExposedPorts[0] != 3000 {
		t.Fatalf("reload mismatch: %+v", got)
	}
}

func TestSandboxSessionStore_PutValidation(t *testing.T) {
	s, _ := newSessionStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, nil); err == nil {
		t.Fatal("Put(nil) should error")
	}
	if err := s.Put(ctx, &store.SandboxSession{}); err == nil {
		t.Fatal("Put empty should error (missing SessionID)")
	}
}

func TestNewSandboxSessionStore_EmptyDir(t *testing.T) {
	if _, err := NewSandboxSessionStore(""); err == nil {
		t.Fatal("NewSandboxSessionStore(\"\") should error")
	}
}
