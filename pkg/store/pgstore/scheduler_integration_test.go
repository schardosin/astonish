//go:build integration

package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

func TestSchedulerStore_CRUD(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := &pgSchedulerStore{pool: pool, schema: schema}

	jobID := uuid.New().String()
	now := time.Now().Truncate(time.Microsecond)

	// Add a job
	job := &store.ScheduledJob{
		ID:   jobID,
		Name: "daily-report",
		Mode: "routine",
		Schedule: store.JobSchedule{
			Cron:     "0 9 * * *",
			Timezone: "UTC",
		},
		Payload: store.JobPayload{
			Flow:         "generate-report",
			Instructions: "Generate the daily status report",
		},
		Enabled:   true,
		CreatedAt: now,
	}
	if err := ss.Add(ctx, job); err != nil {
		t.Fatalf("Add() failed: %v", err)
	}

	// Get by ID
	got := ss.Get(ctx, jobID)
	if got == nil {
		t.Fatal("Get() returned nil after Add()")
	}
	if got.ID != jobID {
		t.Errorf("ID = %q, want %q", got.ID, jobID)
	}
	if got.Name != "daily-report" {
		t.Errorf("Name = %q, want %q", got.Name, "daily-report")
	}
	if got.Mode != "routine" {
		t.Errorf("Mode = %q, want %q", got.Mode, "routine")
	}
	if got.Schedule.Cron != "0 9 * * *" {
		t.Errorf("Schedule.Cron = %q, want %q", got.Schedule.Cron, "0 9 * * *")
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}

	// GetByName
	gotByName := ss.GetByName(ctx, "daily-report")
	if gotByName == nil {
		t.Fatal("GetByName() returned nil")
	}
	if gotByName.ID != jobID {
		t.Errorf("GetByName().ID = %q, want %q", gotByName.ID, jobID)
	}

	// Update
	nextRun := time.Now().Add(24 * time.Hour).Truncate(time.Microsecond)
	job.Name = "daily-report-v2"
	job.Enabled = false
	job.NextRun = &nextRun
	job.LastStatus = "success"
	if err := ss.Update(ctx, job); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	got = ss.Get(ctx, jobID)
	if got == nil {
		t.Fatal("Get() after Update returned nil")
	}
	if got.Name != "daily-report-v2" {
		t.Errorf("Name after update = %q, want %q", got.Name, "daily-report-v2")
	}
	if got.Enabled {
		t.Error("Enabled after update = true, want false")
	}
	if got.LastStatus != "success" {
		t.Errorf("LastStatus after update = %q, want %q", got.LastStatus, "success")
	}

	// Remove
	if err := ss.Remove(ctx, jobID); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Verify gone
	if got := ss.Get(ctx, jobID); got != nil {
		t.Errorf("Get() after Remove should return nil, got %+v", got)
	}
	if got := ss.GetByName(ctx, "daily-report-v2"); got != nil {
		t.Errorf("GetByName() after Remove should return nil, got %+v", got)
	}
}

func TestSchedulerStore_List(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := &pgSchedulerStore{pool: pool, schema: schema}

	// Initial state: empty
	if jobs := ss.List(ctx); len(jobs) != 0 {
		t.Errorf("initial List() has %d jobs, want 0", len(jobs))
	}

	// Add multiple jobs
	names := []string{"job-alpha", "job-beta", "job-gamma"}
	for _, name := range names {
		job := &store.ScheduledJob{
			ID:   uuid.New().String(),
			Name: name,
			Mode: "routine",
			Schedule: store.JobSchedule{
				Cron: "*/5 * * * *",
			},
			Enabled:   true,
			CreatedAt: time.Now(),
		}
		if err := ss.Add(ctx, job); err != nil {
			t.Fatalf("Add(%s) failed: %v", name, err)
		}
	}

	// List should return all 3
	jobs := ss.List(ctx)
	if len(jobs) != 3 {
		t.Fatalf("List() returned %d jobs, want 3", len(jobs))
	}

	// Verify jobs are ordered by name
	for i, expected := range names {
		if jobs[i].Name != expected {
			t.Errorf("List()[%d].Name = %q, want %q", i, jobs[i].Name, expected)
		}
	}
}
