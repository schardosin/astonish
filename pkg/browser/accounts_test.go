package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewAccountStore_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	accounts := store.List()
	if len(accounts) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(accounts))
	}
}

func TestAccountStore_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	acct := &Account{
		Portal:         "reddit.com",
		Username:       "testbot",
		Email:          "bot@example.com",
		CredentialName: "reddit-cred",
		Status:         AccountPending,
		ProfileURL:     "https://reddit.com/u/testbot",
		Notes:          "initial registration",
	}

	if err := store.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify timestamps were set
	if acct.RegisteredAt.IsZero() {
		t.Error("expected RegisteredAt to be set")
	}
	if acct.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	// Get by portal + username
	got := store.Get("reddit.com", "testbot")
	if got == nil {
		t.Fatal("expected account, got nil")
		return
	}
	if got.Portal != "reddit.com" {
		t.Errorf("expected portal reddit.com, got %s", got.Portal)
	}
	if got.Username != "testbot" {
		t.Errorf("expected username testbot, got %s", got.Username)
	}
	if got.Email != "bot@example.com" {
		t.Errorf("expected email bot@example.com, got %s", got.Email)
	}
	if got.Status != AccountPending {
		t.Errorf("expected status pending, got %s", got.Status)
	}

	// Get non-existent
	missing := store.Get("reddit.com", "nonexistent")
	if missing != nil {
		t.Error("expected nil for missing account")
	}
}

func TestAccountStore_GetByPortal(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	accounts := []*Account{
		{Portal: "reddit.com", Username: "bot1", Status: AccountActive},
		{Portal: "reddit.com", Username: "bot2", Status: AccountPending},
		{Portal: "hackernews.com", Username: "bot1", Status: AccountActive},
	}
	for _, a := range accounts {
		if err := store.Save(a); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	redditAccounts := store.GetByPortal("reddit.com")
	if len(redditAccounts) != 2 {
		t.Errorf("expected 2 reddit accounts, got %d", len(redditAccounts))
	}

	hnAccounts := store.GetByPortal("hackernews.com")
	if len(hnAccounts) != 1 {
		t.Errorf("expected 1 hackernews account, got %d", len(hnAccounts))
	}

	noAccounts := store.GetByPortal("nonexistent.com")
	if len(noAccounts) != 0 {
		t.Errorf("expected 0 accounts for nonexistent portal, got %d", len(noAccounts))
	}
}

func TestAccountStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	// Empty list
	if len(store.List()) != 0 {
		t.Error("expected empty list")
	}

	// Add some accounts
	for _, portal := range []string{"a.com", "b.com", "c.com"} {
		if err := store.Save(&Account{Portal: portal, Username: "user", Status: AccountActive}); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	all := store.List()
	if len(all) != 3 {
		t.Errorf("expected 3 accounts, got %d", len(all))
	}
}

func TestAccountStore_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	acct := &Account{
		Portal:   "reddit.com",
		Username: "testbot",
		Status:   AccountPending,
	}
	if err := store.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update status
	if err := store.UpdateStatus("reddit.com", "testbot", AccountActive, "email verified"); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	got := store.Get("reddit.com", "testbot")
	if got == nil {
		t.Fatal("expected account, got nil")
		return
	}
	if got.Status != AccountActive {
		t.Errorf("expected status active, got %s", got.Status)
	}
	if got.Notes != "email verified" {
		t.Errorf("expected notes 'email verified', got %q", got.Notes)
	}

	// Update with empty notes should preserve existing
	if err := store.UpdateStatus("reddit.com", "testbot", AccountSuspended, ""); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	got = store.Get("reddit.com", "testbot")
	if got == nil {
		t.Fatal("expected account, got nil")
		return
	}
	if got.Status != AccountSuspended {
		t.Errorf("expected suspended, got %s", got.Status)
	}
	if got.Notes != "email verified" {
		t.Errorf("expected notes preserved, got %q", got.Notes)
	}

	// Update non-existent
	err = store.UpdateStatus("reddit.com", "nonexistent", AccountActive, "")
	if err == nil {
		t.Error("expected error for non-existent account")
	}
}

func TestAccountStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	acct := &Account{
		Portal:   "reddit.com",
		Username: "testbot",
		Status:   AccountActive,
	}
	if err := store.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.Delete("reddit.com", "testbot"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if got := store.Get("reddit.com", "testbot"); got != nil {
		t.Error("expected nil after delete")
	}

	if len(store.List()) != 0 {
		t.Error("expected empty list after delete")
	}

	// Delete non-existent
	err = store.Delete("reddit.com", "testbot")
	if err == nil {
		t.Error("expected error for non-existent delete")
	}
}

func TestAccountStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create store and save an account
	store1, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	acct := &Account{
		Portal:         "hackernews.com",
		Username:       "astonish_ai",
		Email:          "bot@example.com",
		CredentialName: "hn-cred",
		Status:         AccountActive,
		ProfileURL:     "https://news.ycombinator.com/user?id=astonish_ai",
		Notes:          "registered successfully",
	}
	if err := store1.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the JSON file exists
	fp := filepath.Join(dir, "accounts.json")
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("failed to read accounts.json: %v", err)
	}

	var rawAccounts []map[string]interface{}
	if err := json.Unmarshal(data, &rawAccounts); err != nil {
		t.Fatalf("failed to parse accounts.json: %v", err)
	}
	if len(rawAccounts) != 1 {
		t.Fatalf("expected 1 account in JSON, got %d", len(rawAccounts))
	}

	// Create a new store from the same directory (simulates restart)
	store2, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore (reload) failed: %v", err)
	}

	got := store2.Get("hackernews.com", "astonish_ai")
	if got == nil {
		t.Fatal("expected account after reload, got nil")
		return
	}
	if got.Email != "bot@example.com" {
		t.Errorf("expected email preserved, got %s", got.Email)
	}
	if got.CredentialName != "hn-cred" {
		t.Errorf("expected credential name preserved, got %s", got.CredentialName)
	}
	if got.Status != AccountActive {
		t.Errorf("expected status active, got %s", got.Status)
	}
}

func TestAccountStore_SaveUpdate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	// Save initial
	acct := &Account{
		Portal:   "reddit.com",
		Username: "testbot",
		Status:   AccountPending,
		Notes:    "initial",
	}
	if err := store.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	firstUpdate := acct.UpdatedAt

	// Save again (should overwrite)
	acct.Status = AccountActive
	acct.Notes = "updated"
	if err := store.Save(acct); err != nil {
		t.Fatalf("Save (update) failed: %v", err)
	}

	// Should still be 1 account, not 2
	if len(store.List()) != 1 {
		t.Errorf("expected 1 account after update, got %d", len(store.List()))
	}

	got := store.Get("reddit.com", "testbot")
	if got == nil {
		t.Fatal("expected account, got nil")
		return
	}
	if got.Status != AccountActive {
		t.Errorf("expected active, got %s", got.Status)
	}
	if got.Notes != "updated" {
		t.Errorf("expected 'updated', got %q", got.Notes)
	}
	// RegisteredAt should be preserved (not reset)
	if got.RegisteredAt != acct.RegisteredAt {
		t.Error("RegisteredAt should not change on update")
	}
	// UpdatedAt should be newer
	if !got.UpdatedAt.After(firstUpdate) || got.UpdatedAt.Equal(firstUpdate) {
		// The granularity of time.Now() may cause equality in fast tests,
		// so we only fail if it went backwards.
		if got.UpdatedAt.Before(firstUpdate) {
			t.Error("UpdatedAt should not go backwards")
		}
	}
}

func TestAccountStore_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(fp, []byte("not json{{{"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err := NewAccountStore(dir)
	if err == nil {
		t.Error("expected error for corrupt JSON file")
	}
}

func TestAccountStore_NestedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested", "dir")
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	acct := &Account{
		Portal:   "example.com",
		Username: "user",
		Status:   AccountActive,
	}
	if err := store.Save(acct); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file was created in the nested directory
	fp := filepath.Join(dir, "accounts.json")
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Error("expected accounts.json to be created in nested directory")
	}
}

func TestAccountStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore failed: %v", err)
	}

	var wg sync.WaitGroup
	portals := []string{"a.com", "b.com", "c.com", "d.com", "e.com"}

	// Concurrent writes
	for _, portal := range portals {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				acct := &Account{
					Portal:   p,
					Username: "user",
					Status:   AccountActive,
				}
				_ = store.Save(acct)
			}
		}(portal)
	}

	// Concurrent reads
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_ = store.List()
				_ = store.GetByPortal("a.com")
				_ = store.Get("b.com", "user")
			}
		}()
	}

	wg.Wait()

	// Should have exactly 5 accounts (one per portal, deduplicated by key)
	all := store.List()
	if len(all) != 5 {
		t.Errorf("expected 5 accounts after concurrent writes, got %d", len(all))
	}
}

func TestAccountKey(t *testing.T) {
	tests := []struct {
		portal, username, want string
	}{
		{"reddit.com", "bot", "reddit.com:bot"},
		{"hackernews.com", "astonish_ai", "hackernews.com:astonish_ai"},
		{"", "user", ":user"},
		{"portal.com", "", "portal.com:"},
	}
	for _, tt := range tests {
		got := accountKey(tt.portal, tt.username)
		if got != tt.want {
			t.Errorf("accountKey(%q, %q) = %q, want %q", tt.portal, tt.username, got, tt.want)
		}
	}
}
