package tools

import (
	"context"
	"fmt"
	"testing"
)

// mockSchedulerAccess is a minimal mock for testing schedule tool functions.
type mockSchedulerAccess struct {
	jobs []*SchedulerJob
}

func (m *mockSchedulerAccess) AddJob(job *SchedulerJob) error {
	job.ID = fmt.Sprintf("mock-%d", len(m.jobs)+1)
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *mockSchedulerAccess) ListJobs() []*SchedulerJob {
	return m.jobs
}

func (m *mockSchedulerAccess) RemoveJob(id string) error {
	for i, j := range m.jobs {
		if j.ID == id {
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockSchedulerAccess) UpdateJob(job *SchedulerJob) error {
	for i, j := range m.jobs {
		if j.ID == job.ID {
			m.jobs[i] = job
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockSchedulerAccess) RunNow(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func (m *mockSchedulerAccess) GetJobByName(name string) *SchedulerJob {
	for _, j := range m.jobs {
		if j.Name == name {
			return j
		}
	}
	return nil
}

func (m *mockSchedulerAccess) ValidateCron(_ string) error {
	return nil
}

func TestUpdateScheduledJob_ByID(t *testing.T) {
	mock := &mockSchedulerAccess{}
	schedulerAccessVar = mock
	defer func() { schedulerAccessVar = nil }()

	// Create a job
	mock.AddJob(&SchedulerJob{
		Name:    "My Report",
		Mode:    "adaptive",
		Cron:    "0 9 * * *",
		Enabled: false,
	})

	enabled := true
	result, err := updateScheduledJob(nil, UpdateScheduledJobArgs{
		JobID:   "mock-1",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "updated" {
		t.Errorf("Status = %q, want %q (message: %s)", result.Status, "updated", result.Message)
	}
	if !mock.jobs[0].Enabled {
		t.Error("expected job to be enabled after update")
	}
}

func TestUpdateScheduledJob_ByName(t *testing.T) {
	mock := &mockSchedulerAccess{}
	schedulerAccessVar = mock
	defer func() { schedulerAccessVar = nil }()

	mock.AddJob(&SchedulerJob{
		Name:    "Proxmox Memory Report",
		Mode:    "adaptive",
		Cron:    "30 18 * * *",
		Enabled: false,
	})

	// Pass the name in the job_id field — the LLM sometimes does this
	enabled := true
	result, err := updateScheduledJob(nil, UpdateScheduledJobArgs{
		JobID:   "Proxmox Memory Report",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "updated" {
		t.Errorf("Status = %q, want %q (message: %s)", result.Status, "updated", result.Message)
	}
	if !mock.jobs[0].Enabled {
		t.Error("expected job to be enabled after name-based lookup")
	}
}

func TestUpdateScheduledJob_NotFound(t *testing.T) {
	mock := &mockSchedulerAccess{}
	schedulerAccessVar = mock
	defer func() { schedulerAccessVar = nil }()

	enabled := true
	result, err := updateScheduledJob(nil, UpdateScheduledJobArgs{
		JobID:   "nonexistent",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestUpdateScheduledJob_ScheduleChange(t *testing.T) {
	mock := &mockSchedulerAccess{}
	schedulerAccessVar = mock
	defer func() { schedulerAccessVar = nil }()

	mock.AddJob(&SchedulerJob{
		Name:    "Test Job",
		Mode:    "adaptive",
		Cron:    "0 9 * * *",
		Enabled: true,
	})

	result, err := updateScheduledJob(nil, UpdateScheduledJobArgs{
		JobID:    "mock-1",
		Schedule: "0 18 * * *",
		Timezone: "America/Sao_Paulo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "updated" {
		t.Errorf("Status = %q, want %q (message: %s)", result.Status, "updated", result.Message)
	}
	if mock.jobs[0].Cron != "0 18 * * *" {
		t.Errorf("Cron = %q, want %q", mock.jobs[0].Cron, "0 18 * * *")
	}
	if mock.jobs[0].Timezone != "America/Sao_Paulo" {
		t.Errorf("Timezone = %q, want %q", mock.jobs[0].Timezone, "America/Sao_Paulo")
	}
}

func TestUpdateScheduledJob_NoScheduler(t *testing.T) {
	schedulerAccessVar = nil

	result, err := updateScheduledJob(nil, UpdateScheduledJobArgs{
		JobID: "anything",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestScheduleJob_RejectsDuplicateName(t *testing.T) {
	mock := &mockSchedulerAccess{}
	schedulerAccessVar = mock
	defer func() { schedulerAccessVar = nil }()

	// Create a job (simulates the test_first job that's already in the store)
	mock.AddJob(&SchedulerJob{
		Name:    "Daily Report",
		Mode:    "adaptive",
		Cron:    "0 9 * * *",
		Enabled: false,
	})

	// Attempt to create another job with the same name — should be rejected
	result, err := scheduleJob(nil, ScheduleJobArgs{
		Name:         "Daily Report",
		Mode:         "adaptive",
		Schedule:     "0 18 * * *",
		Instructions: "Do something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
	if len(mock.jobs) != 1 {
		t.Errorf("expected 1 job, got %d (duplicate was created)", len(mock.jobs))
	}
}
