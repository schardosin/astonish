//go:build integration

package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

func TestSessionStore_MetaCRUD(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := &pgSessionStore{pool: pool, schema: schema, sessions: make(map[string]*pgSession)}

	sessionID := uuid.New().String()
	now := time.Now().Truncate(time.Microsecond) // PG has microsecond precision

	// AddSessionMeta
	meta := store.SessionMeta{
		ID:           sessionID,
		AppName:      "test-app",
		UserID:       uuid.New().String(),
		Title:        "My Test Session",
		MessageCount: 5,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := ss.AddSessionMeta(ctx, meta); err != nil {
		t.Fatalf("AddSessionMeta() failed: %v", err)
	}

	// GetSessionMeta
	got, err := ss.GetSessionMeta(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionMeta() failed: %v", err)
	}
	if got.ID != sessionID {
		t.Errorf("ID = %q, want %q", got.ID, sessionID)
	}
	if got.Title != "My Test Session" {
		t.Errorf("Title = %q, want %q", got.Title, "My Test Session")
	}
	if got.MessageCount != 5 {
		t.Errorf("MessageCount = %d, want 5", got.MessageCount)
	}

	// SetSessionTitle
	if err := ss.SetSessionTitle(ctx, sessionID, "Updated Title"); err != nil {
		t.Fatalf("SetSessionTitle() failed: %v", err)
	}
	got, err = ss.GetSessionMeta(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionMeta() after title update failed: %v", err)
	}
	if got.Title != "Updated Title" {
		t.Errorf("Title after update = %q, want %q", got.Title, "Updated Title")
	}

	// RemoveSessionMeta
	if err := ss.RemoveSessionMeta(ctx, sessionID); err != nil {
		t.Fatalf("RemoveSessionMeta() failed: %v", err)
	}

	// Verify gone
	_, err = ss.GetSessionMeta(ctx, sessionID)
	if err == nil {
		t.Error("GetSessionMeta() after Remove should return error")
	}
}

func TestSessionStore_ListSessionMetas(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := &pgSessionStore{pool: pool, schema: schema, sessions: make(map[string]*pgSession)}

	userID := uuid.New().String()
	now := time.Now().Truncate(time.Microsecond)

	// Add multiple sessions (no parent_id so they show in listing)
	for i := 0; i < 3; i++ {
		meta := store.SessionMeta{
			ID:        uuid.New().String(),
			AppName:   "myapp",
			UserID:    userID,
			Title:     "Session " + uuid.New().String()[:8],
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := ss.AddSessionMeta(ctx, meta); err != nil {
			t.Fatalf("AddSessionMeta() #%d failed: %v", i, err)
		}
	}

	// Add a child session (has parent_id — should NOT appear in ListSessionMetas)
	parentID := uuid.New().String()
	childMeta := store.SessionMeta{
		ID:        uuid.New().String(),
		AppName:   "myapp",
		UserID:    userID,
		Title:     "Child Session",
		ParentID:  parentID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := ss.AddSessionMeta(ctx, childMeta); err != nil {
		t.Fatalf("AddSessionMeta(child) failed: %v", err)
	}

	// ListSessionMetas should return only the 3 root sessions
	metas, err := ss.ListSessionMetas(ctx, "myapp", userID)
	if err != nil {
		t.Fatalf("ListSessionMetas() failed: %v", err)
	}
	if len(metas) != 3 {
		t.Errorf("ListSessionMetas() returned %d sessions, want 3 (excluding child)", len(metas))
	}
}

func TestSessionStore_ResolveSessionID(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := &pgSessionStore{pool: pool, schema: schema, sessions: make(map[string]*pgSession)}

	// Use a known ID for prefix matching
	knownID := "abcdef12-3456-7890-abcd-ef1234567890"
	now := time.Now().Truncate(time.Microsecond)

	meta := store.SessionMeta{
		ID:        knownID,
		AppName:   "testapp",
		UserID:    uuid.New().String(),
		Title:     "Known Session",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := ss.AddSessionMeta(ctx, meta); err != nil {
		t.Fatalf("AddSessionMeta() failed: %v", err)
	}

	// Resolve with full ID
	resolved, err := ss.ResolveSessionID(ctx, knownID)
	if err != nil {
		t.Fatalf("ResolveSessionID(full) failed: %v", err)
	}
	if resolved != knownID {
		t.Errorf("ResolveSessionID(full) = %q, want %q", resolved, knownID)
	}

	// Resolve with partial prefix
	resolved, err = ss.ResolveSessionID(ctx, "abcdef12")
	if err != nil {
		t.Fatalf("ResolveSessionID(partial) failed: %v", err)
	}
	if resolved != knownID {
		t.Errorf("ResolveSessionID(partial) = %q, want %q", resolved, knownID)
	}

	// Resolve with non-matching prefix should error
	_, err = ss.ResolveSessionID(ctx, "zzzzz")
	if err == nil {
		t.Error("ResolveSessionID(nonexistent) should return error")
	}
}
