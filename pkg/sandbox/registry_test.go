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

func TestSessionRegistryGetByContainerName(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "astn-sess-abc123", "base")
	_ = r.Put("sess-2", "astn-sess-def456", "custom")

	// Found
	entry := r.GetByContainerName("astn-sess-abc123")
	if entry == nil {
		t.Fatal("expected entry for astn-sess-abc123")
	}
	if entry.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", entry.SessionID)
	}

	// Found second
	entry = r.GetByContainerName("astn-sess-def456")
	if entry == nil {
		t.Fatal("expected entry for astn-sess-def456")
	}
	if entry.SessionID != "sess-2" {
		t.Errorf("SessionID = %q, want sess-2", entry.SessionID)
	}

	// Not found
	entry = r.GetByContainerName("nonexistent")
	if entry != nil {
		t.Errorf("expected nil for nonexistent container, got %+v", entry)
	}
}

func TestSessionRegistryExposePort(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "astn-sess-abc123", "base")

	// Expose port 3000
	added, err := r.ExposePort("astn-sess-abc123", 3000)
	if err != nil {
		t.Fatalf("ExposePort: %v", err)
	}
	if !added {
		t.Error("expected port to be newly added")
	}

	// Verify it's exposed
	if !r.IsPortExposed("astn-sess-abc123", 3000) {
		t.Error("port 3000 should be exposed")
	}

	// Expose same port again — should not add duplicate
	added, err = r.ExposePort("astn-sess-abc123", 3000)
	if err != nil {
		t.Fatalf("ExposePort duplicate: %v", err)
	}
	if added {
		t.Error("expected port to NOT be newly added (duplicate)")
	}

	// Expose a second port
	added, err = r.ExposePort("astn-sess-abc123", 8080)
	if err != nil {
		t.Fatalf("ExposePort 8080: %v", err)
	}
	if !added {
		t.Error("expected port 8080 to be newly added")
	}

	// Verify both are exposed
	entry := r.GetByContainerName("astn-sess-abc123")
	if entry == nil {
		t.Fatal("entry not found")
	}
	if len(entry.ExposedPorts) != 2 {
		t.Fatalf("ExposedPorts has %d entries, want 2", len(entry.ExposedPorts))
	}

	// Expose on nonexistent container
	_, err = r.ExposePort("nonexistent", 3000)
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestSessionRegistryUnexposePort(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "astn-sess-abc123", "base")

	// Expose two ports
	_, _ = r.ExposePort("astn-sess-abc123", 3000)
	_, _ = r.ExposePort("astn-sess-abc123", 8080)

	// Unexpose port 3000
	removed, err := r.UnexposePort("astn-sess-abc123", 3000)
	if err != nil {
		t.Fatalf("UnexposePort: %v", err)
	}
	if !removed {
		t.Error("expected port 3000 to be removed")
	}

	// Verify 3000 is gone
	if r.IsPortExposed("astn-sess-abc123", 3000) {
		t.Error("port 3000 should no longer be exposed")
	}

	// 8080 should remain
	if !r.IsPortExposed("astn-sess-abc123", 8080) {
		t.Error("port 8080 should still be exposed")
	}

	// Unexpose port that isn't exposed
	removed, err = r.UnexposePort("astn-sess-abc123", 9999)
	if err != nil {
		t.Fatalf("UnexposePort 9999: %v", err)
	}
	if removed {
		t.Error("expected port 9999 to NOT be removed (not exposed)")
	}

	// Unexpose on nonexistent container
	_, err = r.UnexposePort("nonexistent", 3000)
	if err == nil {
		t.Error("expected error for nonexistent container")
	}
}

func TestSessionRegistryIsPortExposed(t *testing.T) {
	r := newTestRegistry(t)
	_ = r.Put("sess-1", "astn-sess-abc123", "base")

	// Not exposed yet
	if r.IsPortExposed("astn-sess-abc123", 3000) {
		t.Error("port 3000 should not be exposed initially")
	}

	// Not exposed on nonexistent container
	if r.IsPortExposed("nonexistent", 3000) {
		t.Error("should return false for nonexistent container")
	}

	// Expose and check
	_, _ = r.ExposePort("astn-sess-abc123", 3000)
	if !r.IsPortExposed("astn-sess-abc123", 3000) {
		t.Error("port 3000 should be exposed after ExposePort")
	}

	// Different port on same container
	if r.IsPortExposed("astn-sess-abc123", 8080) {
		t.Error("port 8080 should not be exposed")
	}
}

func TestSessionRegistryExposedPortsPersistence(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sessions.json")

	// Create registry, add entry with exposed ports
	r1 := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filePath,
	}
	_ = r1.Put("sess-1", "astn-sess-abc123", "base")
	_, _ = r1.ExposePort("astn-sess-abc123", 3000)
	_, _ = r1.ExposePort("astn-sess-abc123", 8080)

	// Load into fresh registry
	r2 := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filePath,
	}
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify ports persisted
	if !r2.IsPortExposed("astn-sess-abc123", 3000) {
		t.Error("port 3000 should persist across save/load")
	}
	if !r2.IsPortExposed("astn-sess-abc123", 8080) {
		t.Error("port 8080 should persist across save/load")
	}
	if r2.IsPortExposed("astn-sess-abc123", 9999) {
		t.Error("port 9999 should not be exposed")
	}
}
