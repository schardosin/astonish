package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAddAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	job := &Job{
		Name: "Test Job",
		Mode: ModeRoutine,
		Schedule: JobSchedule{
			Cron:     "0 9 * * *",
			Timezone: "America/Sao_Paulo",
		},
		Payload: JobPayload{
			Flow:   "my_flow",
			Params: map[string]string{"key": "value"},
		},
		Delivery: JobDelivery{
			Channel: "telegram",
			Target:  "123456",
		},
		Enabled: true,
	}

	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if job.ID == "" {
		t.Fatal("expected ID to be generated")
	}
	if job.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	// Get by ID
	got := store.Get(job.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Name != "Test Job" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Job")
	}
	if got.Mode != ModeRoutine {
		t.Errorf("Mode = %q, want %q", got.Mode, ModeRoutine)
	}
	if got.Payload.Flow != "my_flow" {
		t.Errorf("Flow = %q, want %q", got.Payload.Flow, "my_flow")
	}
}

func TestStoreGetByName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	store.Add(&Job{Name: "Morning Report", Mode: ModeRoutine, Enabled: true})
	store.Add(&Job{Name: "Health Check", Mode: ModeAdaptive, Enabled: true})

	got := store.GetByName("morning report") // case-insensitive
	if got == nil {
		t.Fatal("GetByName returned nil")
	}
	if got.Name != "Morning Report" {
		t.Errorf("Name = %q, want %q", got.Name, "Morning Report")
	}

	// Not found
	if store.GetByName("nonexistent") != nil {
		t.Error("expected nil for nonexistent name")
	}
}

func TestStoreList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	store.Add(&Job{Name: "Job 1", Mode: ModeRoutine})
	store.Add(&Job{Name: "Job 2", Mode: ModeAdaptive})

	jobs := store.List()
	if len(jobs) != 2 {
		t.Errorf("List() returned %d jobs, want 2", len(jobs))
	}
}

func TestStoreUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	job := &Job{Name: "Original", Mode: ModeRoutine, Enabled: false}
	store.Add(job)

	// Update
	job.Name = "Updated"
	job.Enabled = true
	if err := store.Update(job); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := store.Get(job.ID)
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want %q", got.Name, "Updated")
	}
	if !got.Enabled {
		t.Error("expected Enabled = true")
	}

	// Update non-existent
	if err := store.Update(&Job{ID: "nonexistent"}); err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestStoreRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	job := &Job{Name: "To Remove", Mode: ModeRoutine}
	store.Add(job)

	if err := store.Remove(job.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if store.Get(job.ID) != nil {
		t.Error("expected nil after removal")
	}

	// Remove non-existent
	if err := store.Remove("nonexistent"); err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	// Create and add
	store1, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store1.Add(&Job{Name: "Persistent", Mode: ModeAdaptive, Enabled: true})

	// Reload from disk
	store2, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}

	jobs := store2.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after reload, got %d", len(jobs))
	}
	if jobs[0].Name != "Persistent" {
		t.Errorf("Name = %q, want %q", jobs[0].Name, "Persistent")
	}
}

func TestStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.Add(&Job{Name: "Test", Mode: ModeRoutine})

	// Verify no .tmp file left behind
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after save")
	}

	// Verify main file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("store file should exist: %v", err)
	}
}

func TestStoreEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	// Create an empty file
	os.WriteFile(path, []byte{}, 0644)

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore with empty file: %v", err)
	}

	jobs := store.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs from empty file, got %d", len(jobs))
	}
}

func TestValidateCron(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"0 9 * * *", false},    // daily at 9 AM
		{"*/30 * * * *", false}, // every 30 minutes
		{"0 9 * * 1-5", false},  // weekdays at 9 AM
		{"0 */2 * * *", false},  // every 2 hours
		{"invalid", true},
		{"* * *", true},       // too few fields
		{"* * * * * *", true}, // too many fields (6-field)
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			err := ValidateCron(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCron(%q) error = %v, wantErr = %v", tt.expr, err, tt.wantErr)
			}
		})
	}
}

func TestBackoffDuration(t *testing.T) {
	s := &Scheduler{}

	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 30 * time.Second},
		{1, 30 * time.Second},
		{2, 1 * time.Minute},
		{3, 5 * time.Minute},
		{4, 15 * time.Minute},
		{5, 60 * time.Minute},
		{100, 60 * time.Minute}, // caps at max
	}

	for _, tt := range tests {
		got := s.backoffDuration(tt.failures)
		if got != tt.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
}

func TestEqualFold(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello", "HELLO", true},
		{"Hello", "hello", true},
		{"foo", "bar", false},
		{"", "", true},
		{"a", "ab", false},
	}

	for _, tt := range tests {
		got := equalFold(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("equalFold(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
