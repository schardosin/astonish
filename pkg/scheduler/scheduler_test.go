package scheduler

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
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
		return
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
		return
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

func TestRefreshNextRunAfterScheduleChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	logger := log.New(os.Stderr, "", 0)
	sched := New(store, nil, nil, logger)

	// Create a job with "every minute" schedule
	job := &Job{
		Name:    "Test Refresh",
		Mode:    ModeAdaptive,
		Enabled: true,
		Schedule: JobSchedule{
			Cron: "* * * * *", // every minute
		},
		Payload: JobPayload{
			Instructions: "test",
		},
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Set initial NextRun to ~1 minute from now (as if computed from * * * * *)
	sched.RefreshNextRun(job.ID)
	jobAfterInit := store.Get(job.ID)
	if jobAfterInit.NextRun == nil {
		t.Fatal("expected NextRun to be set after RefreshNextRun")
	}
	initialNextRun := *jobAfterInit.NextRun

	// Now update the schedule to "every 5 minutes"
	jobAfterInit.Schedule.Cron = "*/5 * * * *"
	if err := store.Update(jobAfterInit); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Refresh NextRun — this is what the bridge/API should call after update
	sched.RefreshNextRun(job.ID)

	jobAfterUpdate := store.Get(job.ID)
	if jobAfterUpdate.NextRun == nil {
		t.Fatal("expected NextRun to be set after schedule change")
	}
	updatedNextRun := *jobAfterUpdate.NextRun

	// The updated NextRun should be >= initialNextRun because */5 has wider spacing
	// than * (every minute). Specifically, */5 aligns to 0,5,10,...55 while * fires
	// every minute. So the next */5 from now should be at or after the next * from now.
	if updatedNextRun.Before(initialNextRun) {
		t.Errorf("updated NextRun (%s) should not be before initial NextRun (%s)",
			updatedNextRun.Format(time.RFC3339), initialNextRun.Format(time.RFC3339))
	}

	// Verify it aligns to a */5 boundary (minute should be 0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55)
	min := updatedNextRun.Minute()
	if min%5 != 0 {
		t.Errorf("updated NextRun minute = %d, expected multiple of 5", min)
	}
}

func TestComputeNextRunUsesBaseTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	logger := log.New(os.Stderr, "", 0)
	sched := New(store, nil, nil, logger)

	job := &Job{
		Name:    "Base Time Test",
		Mode:    ModeAdaptive,
		Enabled: true,
		Schedule: JobSchedule{
			Cron:     "0 9 * * *", // daily at 9 AM
			Timezone: "UTC",       // explicit UTC so test is location-independent
		},
		Payload: JobPayload{
			Instructions: "test",
		},
	}

	// Compute from a known base time: 2025-01-15 08:00 UTC
	base := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	nextRun := sched.computeNextRun(job, base)
	if nextRun == nil {
		t.Fatal("expected NextRun to be computed")
	}

	// Should be 2025-01-15 09:00 UTC (same day, next 9 AM)
	expected := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	if !nextRun.Equal(expected) {
		t.Errorf("NextRun = %s, want %s", nextRun.Format(time.RFC3339), expected.Format(time.RFC3339))
	}

	// Compute from a base time AFTER 9 AM — should be next day
	base2 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	nextRun2 := sched.computeNextRun(job, base2)
	if nextRun2 == nil {
		t.Fatal("expected NextRun to be computed")
	}

	expected2 := time.Date(2025, 1, 16, 9, 0, 0, 0, time.UTC)
	if !nextRun2.Equal(expected2) {
		t.Errorf("NextRun = %s, want %s", nextRun2.Format(time.RFC3339), expected2.Format(time.RFC3339))
	}
}

func TestInFlightDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Track how many times execute is called
	var execCount atomic.Int32
	executeDone := make(chan struct{})

	logger := log.New(os.Stderr, "", 0)
	sched := New(store, func(ctx context.Context, job *Job) (string, error) {
		execCount.Add(1)
		// Block long enough for a second tick to fire
		<-executeDone
		return "done", nil
	}, nil, logger)

	// Create a job that's already due
	past := time.Now().Add(-1 * time.Minute)
	job := &Job{
		Name:    "Dedup Test",
		Mode:    ModeAdaptive,
		Enabled: true,
		NextRun: &past,
		Schedule: JobSchedule{
			Cron: "* * * * *",
		},
		Payload: JobPayload{
			Instructions: "test",
		},
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// First tick should dispatch the job
	sched.tick()

	// Second tick should skip it (already running)
	sched.tick()

	// Let the job finish
	close(executeDone)

	// Wait briefly for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	count := execCount.Load()
	if count != 1 {
		t.Errorf("execute called %d times, want 1 (dedup should prevent double dispatch)", count)
	}
}
