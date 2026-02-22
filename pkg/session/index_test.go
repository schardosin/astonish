package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIndex_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	data, err := idx.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if data.Version != 1 {
		t.Errorf("Version = %d, want 1", data.Version)
	}
	if len(data.Sessions) != 0 {
		t.Errorf("Sessions len = %d, want 0", len(data.Sessions))
	}
}

func TestIndex_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	meta := SessionMeta{
		ID:        "sess-001",
		AppName:   "test-app",
		UserID:    "user-1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Title:     "Test Session",
	}

	if err := idx.Add(meta); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, err := idx.Get("sess-001")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != "sess-001" {
		t.Errorf("ID = %q, want %q", got.ID, "sess-001")
	}
	if got.AppName != "test-app" {
		t.Errorf("AppName = %q, want %q", got.AppName, "test-app")
	}
	if got.Title != "Test Session" {
		t.Errorf("Title = %q, want %q", got.Title, "Test Session")
	}
}

func TestIndex_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	_, err := idx.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
}

func TestIndex_Remove(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	meta := SessionMeta{
		ID:      "sess-002",
		AppName: "test-app",
		UserID:  "user-1",
	}

	if err := idx.Add(meta); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := idx.Remove("sess-002"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	_, err := idx.Get("sess-002")
	if err == nil {
		t.Fatal("expected error after Remove, got nil")
	}
}

func TestIndex_Update(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	meta := SessionMeta{
		ID:      "sess-003",
		AppName: "test-app",
		UserID:  "user-1",
		Title:   "Original Title",
	}

	if err := idx.Add(meta); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := idx.Update("sess-003", func(m *SessionMeta) {
		m.Title = "Updated Title"
		m.MessageCount = 5
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := idx.Get("sess-003")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", got.Title, "Updated Title")
	}
	if got.MessageCount != 5 {
		t.Errorf("MessageCount = %d, want 5", got.MessageCount)
	}
}

func TestIndex_UpdateNotFound(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	err := idx.Update("nonexistent", func(m *SessionMeta) {
		m.Title = "test"
	})
	if err == nil {
		t.Fatal("expected error updating nonexistent session, got nil")
	}
}

func TestIndex_ListFilters(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	sessions := []SessionMeta{
		{ID: "s1", AppName: "app1", UserID: "user-a"},
		{ID: "s2", AppName: "app1", UserID: "user-b"},
		{ID: "s3", AppName: "app2", UserID: "user-a"},
	}
	for _, s := range sessions {
		if err := idx.Add(s); err != nil {
			t.Fatalf("Add(%s) error = %v", s.ID, err)
		}
	}

	// List all for app1
	result, err := idx.List("app1", "")
	if err != nil {
		t.Fatalf("List(app1, '') error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("List(app1, '') len = %d, want 2", len(result))
	}

	// List for app1, user-a only
	result, err = idx.List("app1", "user-a")
	if err != nil {
		t.Fatalf("List(app1, user-a) error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("List(app1, user-a) len = %d, want 1", len(result))
	}

	// List for app2
	result, err = idx.List("app2", "")
	if err != nil {
		t.Fatalf("List(app2, '') error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("List(app2, '') len = %d, want 1", len(result))
	}

	// List for nonexistent app
	result, err = idx.List("app3", "")
	if err != nil {
		t.Fatalf("List(app3, '') error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("List(app3, '') len = %d, want 0", len(result))
	}
}

func TestIndex_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	idx := NewSessionIndex(path)

	meta := SessionMeta{
		ID:           "sess-persist",
		AppName:      "app1",
		UserID:       "user-1",
		Title:        "Persistent",
		MessageCount: 10,
	}
	if err := idx.Add(meta); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Create a new index pointing to the same file
	idx2 := NewSessionIndex(path)
	got, err := idx2.Get("sess-persist")
	if err != nil {
		t.Fatalf("Get() on new index error = %v", err)
	}
	if got.Title != "Persistent" {
		t.Errorf("Title = %q, want %q", got.Title, "Persistent")
	}
	if got.MessageCount != 10 {
		t.Errorf("MessageCount = %d, want 10", got.MessageCount)
	}
}

func TestIndex_ConcurrentAdd(t *testing.T) {
	dir := t.TempDir()
	idx := NewSessionIndex(filepath.Join(dir, "index.json"))

	var wg sync.WaitGroup
	n := 10
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			meta := SessionMeta{
				ID:      fmt.Sprintf("concurrent-%d", i),
				AppName: "test-app",
				UserID:  "user-1",
			}
			_ = idx.Add(meta)
		}(i)
	}
	wg.Wait()

	data, err := idx.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(data.Sessions) != n {
		t.Errorf("Sessions len = %d, want %d", len(data.Sessions), n)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "test.json")

	content := []byte(`{"test": true}`)
	if err := atomicWrite(path, content, 0644); err != nil {
		t.Fatalf("atomicWrite() error = %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content = %q, want %q", string(data), string(content))
	}
}
