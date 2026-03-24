package sandbox

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// newTestRegistry creates a SessionRegistry backed by a temp file.
func newTestRegistry(t *testing.T) *SessionRegistry {
	t.Helper()
	dir := t.TempDir()
	return &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filepath.Join(dir, "sessions.json"),
	}
}

func TestSessionRegistryPutGet(t *testing.T) {
	r := newTestRegistry(t)

	err := r.Put("sess-1", "astn-sess-abcd1234", "base")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	entry := r.Get("sess-1")
	if entry == nil {
		t.Fatal("Get returned nil")
	}
	if entry.ContainerName != "astn-sess-abcd1234" {
		t.Errorf("ContainerName = %q, want astn-sess-abcd1234", entry.ContainerName)
	}
	if entry.TemplateName != "base" {
		t.Errorf("TemplateName = %q, want base", entry.TemplateName)
	}
}

func TestSessionRegistryRemove(t *testing.T) {
	r := newTestRegistry(t)

	_ = r.Put("sess-1", "astn-sess-abcd1234", "base")
	_ = r.Put("sess-2", "astn-sess-efgh5678", "base")

	if err := r.Remove("sess-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if r.Get("sess-1") != nil {
		t.Error("sess-1 should be removed")
	}
	if r.Get("sess-2") == nil {
		t.Error("sess-2 should still exist")
	}
}

func TestSessionRegistryGetContainerName(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "astn-sess-abcd1234", "base")

	if got := r.GetContainerName("sess-1"); got != "astn-sess-abcd1234" {
		t.Errorf("got %q, want astn-sess-abcd1234", got)
	}
	if got := r.GetContainerName("nonexistent"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestSessionRegistryList(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "c1", "base")
	_ = r.Put("sess-2", "c2", "custom")

	entries := r.List()
	if len(entries) != 2 {
		t.Fatalf("List() returned %d entries, want 2", len(entries))
	}
}

func TestSessionRegistrySessionIDs(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "c1", "base")
	_ = r.Put("sess-2", "c2", "base")

	ids := r.SessionIDs()
	if len(ids) != 2 {
		t.Fatalf("SessionIDs() returned %d, want 2", len(ids))
	}

	found := make(map[string]bool)
	for _, id := range ids {
		found[id] = true
	}
	if !found["sess-1"] || !found["sess-2"] {
		t.Errorf("expected sess-1 and sess-2, got %v", ids)
	}
}

func TestSessionRegistryResolveSessionID(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("abc12345-6789", "astn-sess-abc12345", "base")

	tests := []struct {
		name  string
		input string
		want  string
		found bool
	}{
		{"exact session ID", "abc12345-6789", "abc12345-6789", true},
		{"container name", "astn-sess-abc12345", "abc12345-6789", true},
		{"session ID prefix", "abc12", "abc12345-6789", true},
		{"container prefix", "astn-sess-abc", "abc12345-6789", true},
		{"no match", "zzz", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.ResolveSessionID(tt.input)
			if ok != tt.found {
				t.Errorf("found = %v, want %v", ok, tt.found)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionRegistrySaveLoad(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sessions.json")

	// Create and populate
	r1 := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filePath,
	}
	_ = r1.Put("sess-1", "c1", "base")
	_ = r1.Put("sess-2", "c2", "custom")

	// Load into a fresh registry
	r2 := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filePath,
	}
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if e := r2.Get("sess-1"); e == nil || e.ContainerName != "c1" {
		t.Error("sess-1 not loaded correctly")
	}
	if e := r2.Get("sess-2"); e == nil || e.ContainerName != "c2" {
		t.Error("sess-2 not loaded correctly")
	}
}

func TestSessionRegistryLoadMissing(t *testing.T) {
	r := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filepath.Join(t.TempDir(), "nonexistent.json"),
	}
	err := r.Load()
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestSessionRegistryConcurrentPutRemove(t *testing.T) {
	r := newTestRegistry(t)
	var wg sync.WaitGroup

	// Concurrent puts
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "sess-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = r.Put(id, "c-"+id, "base")
		}(i)
	}
	wg.Wait()

	// Concurrent removes
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "sess-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = r.Remove(id)
		}(i)
	}
	wg.Wait()

	// Should not have panicked; verify the registry is in a consistent state
	entries := r.List()
	ids := r.SessionIDs()
	if len(entries) != len(ids) {
		t.Errorf("List() has %d entries but SessionIDs() has %d", len(entries), len(ids))
	}
}
