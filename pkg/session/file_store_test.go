package session

import (
	"context"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	return store
}

func createTestSession(t *testing.T, store *FileStore, appName, userID string) adksession.Session {
	t.Helper()
	resp, err := store.Create(context.Background(), &adksession.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return resp.Session
}

func testEvent(id, author, text string) *adksession.Event {
	return &adksession.Event{
		ID:        id,
		Author:    author,
		Timestamp: time.Now(),
		Actions:   adksession.EventActions{},
		LLMResponse: adkmodel.LLMResponse{
			Content: genai.NewContentFromText(text, genai.RoleUser),
		},
	}
}

func TestFileStore_CreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	resp, err := store.Create(ctx, &adksession.CreateRequest{
		AppName: "myapp",
		UserID:  "user1",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	sess := resp.Session
	if sess.ID() == "" {
		t.Error("Session ID is empty")
	}
	if sess.AppName() != "myapp" {
		t.Errorf("AppName = %q, want %q", sess.AppName(), "myapp")
	}
	if sess.UserID() != "user1" {
		t.Errorf("UserID = %q, want %q", sess.UserID(), "user1")
	}

	// Get the session back
	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	got := getResp.Session
	if got.ID() != sess.ID() {
		t.Errorf("Get().ID = %q, want %q", got.ID(), sess.ID())
	}
}

func TestFileStore_CreateWithCustomID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	resp, err := store.Create(ctx, &adksession.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "custom-id-123",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if resp.Session.ID() != "custom-id-123" {
		t.Errorf("ID = %q, want %q", resp.Session.ID(), "custom-id-123")
	}
}

func TestFileStore_CreateRequiresAppAndUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		appName string
		userID  string
	}{
		{"empty app", "", "user1"},
		{"empty user", "app1", ""},
		{"both empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Create(ctx, &adksession.CreateRequest{
				AppName: tt.appName,
				UserID:  tt.userID,
			})
			if err == nil {
				t.Error("expected error for missing required fields, got nil")
			}
		})
	}
}

func TestFileStore_CreateDuplicate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, &adksession.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "dup-id",
	})
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	_, err = store.Create(ctx, &adksession.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "dup-id",
	})
	if err == nil {
		t.Error("expected error for duplicate session ID, got nil")
	}
}

func TestFileStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "nonexistent",
	})
	if err == nil {
		t.Error("expected error for nonexistent session, got nil")
	}
}

func TestFileStore_GetWrongAppUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "app1", "user1")

	_, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "app2",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err == nil {
		t.Error("expected error for wrong app name, got nil")
	}
}

func TestFileStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	err := store.Delete(ctx, &adksession.DeleteRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err == nil {
		t.Error("expected error after Delete, got nil")
	}
}

func TestFileStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	createTestSession(t, store, "app1", "userA")
	createTestSession(t, store, "app1", "userB")
	createTestSession(t, store, "app2", "userA")

	// List all for app1
	resp, err := store.List(ctx, &adksession.ListRequest{AppName: "app1"})
	if err != nil {
		t.Fatalf("List(app1) error = %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Errorf("List(app1) len = %d, want 2", len(resp.Sessions))
	}

	// List app1, userA
	resp, err = store.List(ctx, &adksession.ListRequest{AppName: "app1", UserID: "userA"})
	if err != nil {
		t.Fatalf("List(app1, userA) error = %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Errorf("List(app1, userA) len = %d, want 1", len(resp.Sessions))
	}

	// List for app3 (no sessions)
	resp, err = store.List(ctx, &adksession.ListRequest{AppName: "app3"})
	if err != nil {
		t.Fatalf("List(app3) error = %v", err)
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("List(app3) len = %d, want 0", len(resp.Sessions))
	}
}

func TestFileStore_AppendEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	ev := testEvent("ev1", "user", "Hello agent")
	if err := store.AppendEvent(ctx, sess, ev); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	// Get session and check event
	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	eventCount := getResp.Session.Events().Len()
	if eventCount != 1 {
		t.Errorf("Events().Len() = %d, want 1", eventCount)
	}
}

func TestFileStore_AppendEventPartialSkipped(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	ev := testEvent("ev-partial", "model", "streaming chunk")
	ev.Partial = true

	if err := store.AppendEvent(ctx, sess, ev); err != nil {
		t.Fatalf("AppendEvent(partial) error = %v", err)
	}

	// No event should have been stored
	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	eventCount := getResp.Session.Events().Len()
	if eventCount != 0 {
		t.Errorf("Events().Len() = %d, want 0 (partial should be skipped)", eventCount)
	}
}

func TestFileStore_AppendEventNilErrors(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	// Nil event
	if err := store.AppendEvent(ctx, sess, nil); err == nil {
		t.Error("expected error for nil event, got nil")
	}

	// Nil session
	ev := testEvent("ev1", "user", "hello")
	if err := store.AppendEvent(ctx, nil, ev); err == nil {
		t.Error("expected error for nil session, got nil")
	}
}

func TestFileStore_AppendEventWithStateDelta(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	ev := testEvent("ev1", "model", "response")
	ev.Actions.StateDelta = map[string]any{
		"topic": "testing",
	}

	if err := store.AppendEvent(ctx, sess, ev); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	// Get and check state
	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	val, stateErr := getResp.Session.State().Get("topic")
	if stateErr != nil {
		t.Fatalf("State().Get(topic) error = %v", stateErr)
	}
	if v, ok := val.(string); !ok || v != "testing" {
		t.Errorf("State[topic] = %v, want %q", val, "testing")
	}
}

func TestFileStore_GetWithNumRecentEvents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	for i := 0; i < 10; i++ {
		ev := testEvent("", "user", "msg")
		ev.Timestamp = time.Now().Add(time.Duration(i) * time.Millisecond)
		if err := store.AppendEvent(ctx, sess, ev); err != nil {
			t.Fatalf("AppendEvent() error = %v", err)
		}
	}

	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:         "myapp",
		UserID:          "user1",
		SessionID:       sess.ID(),
		NumRecentEvents: 3,
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	eventCount := getResp.Session.Events().Len()
	if eventCount != 3 {
		t.Errorf("Events().Len() = %d, want 3", eventCount)
	}
}

func TestFileStore_ResolveSessionID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	resp, err := store.Create(ctx, &adksession.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "abc-123-def",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Exact match
	resolved, err := store.ResolveSessionID(resp.Session.ID())
	if err != nil {
		t.Fatalf("ResolveSessionID(exact) error = %v", err)
	}
	if resolved != "abc-123-def" {
		t.Errorf("resolved = %q, want %q", resolved, "abc-123-def")
	}

	// Prefix match
	resolved, err = store.ResolveSessionID("abc-123")
	if err != nil {
		t.Fatalf("ResolveSessionID(prefix) error = %v", err)
	}
	if resolved != "abc-123-def" {
		t.Errorf("resolved = %q, want %q", resolved, "abc-123-def")
	}

	// No match
	_, err = store.ResolveSessionID("zzz")
	if err == nil {
		t.Error("expected error for no match, got nil")
	}
}

func TestFileStore_ResolveSessionIDAmbiguous(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"abc-111", "abc-222"} {
		_, err := store.Create(ctx, &adksession.CreateRequest{
			AppName:   "myapp",
			UserID:    "user1",
			SessionID: id,
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}

	_, err := store.ResolveSessionID("abc")
	if err == nil {
		t.Error("expected error for ambiguous prefix, got nil")
	}
}

func TestFileStore_SetSessionTitle(t *testing.T) {
	store := newTestStore(t)
	sess := createTestSession(t, store, "myapp", "user1")

	if err := store.SetSessionTitle(sess.ID(), "My Title"); err != nil {
		t.Fatalf("SetSessionTitle() error = %v", err)
	}

	meta, err := store.index.Get(sess.ID())
	if err != nil {
		t.Fatalf("index.Get() error = %v", err)
	}
	if meta.Title != "My Title" {
		t.Errorf("Title = %q, want %q", meta.Title, "My Title")
	}
}

func TestFileStore_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create store and session, append events
	store1, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	resp, err := store1.Create(ctx, &adksession.CreateRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "persist-test",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ev := testEvent("ev1", "user", "persisted message")
	if err := store1.AppendEvent(ctx, resp.Session, ev); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	// Create a new store pointing to the same directory (simulating restart)
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	getResp, err := store2.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: "persist-test",
	})
	if err != nil {
		t.Fatalf("Get() on new store error = %v", err)
	}

	eventCount := getResp.Session.Events().Len()
	if eventCount != 1 {
		t.Errorf("Events().Len() = %d, want 1", eventCount)
	}
}

func TestFileStore_TempStateTrimmed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := createTestSession(t, store, "myapp", "user1")

	ev := testEvent("ev1", "model", "response")
	ev.Actions.StateDelta = map[string]any{
		"persistent_key":                       "stays",
		adksession.KeyPrefixTemp + "ephemeral": "should be trimmed",
	}

	if err := store.AppendEvent(ctx, sess, ev); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	getResp, err := store.Get(ctx, &adksession.GetRequest{
		AppName:   "myapp",
		UserID:    "user1",
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	state := getResp.Session.State()

	// Persistent key should be present
	val, stateErr := state.Get("persistent_key")
	if stateErr != nil {
		t.Errorf("State().Get(persistent_key) error = %v", stateErr)
	}
	if v, ok := val.(string); !ok || v != "stays" {
		t.Errorf("persistent_key = %v, want %q", val, "stays")
	}

	// Temp key should NOT be in state
	_, stateErr = state.Get(adksession.KeyPrefixTemp + "ephemeral")
	if stateErr == nil {
		t.Error("expected error for temp key after trim, got nil")
	}
}
