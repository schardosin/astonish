package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/schardosin/astonish/pkg/scheduler"
	"github.com/schardosin/astonish/pkg/store"
)

// testSchedulerStore adapts scheduler.Store to store.SchedulerStore for tests.
type testSchedulerStore struct {
	ss *scheduler.Store
	mu sync.Mutex
}

func (s *testSchedulerStore) List(_ context.Context) []*store.ScheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := s.ss.List()
	out := make([]*store.ScheduledJob, len(jobs))
	for i, j := range jobs {
		out[i] = s.toStoreJob(j)
	}
	return out
}

func (s *testSchedulerStore) Get(_ context.Context, id string) *store.ScheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.ss.Get(id)
	if j == nil {
		return nil
	}
	return s.toStoreJob(j)
}

func (s *testSchedulerStore) GetByName(_ context.Context, name string) *store.ScheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.ss.List() {
		if j.Name == name {
			return s.toStoreJob(j)
		}
	}
	return nil
}

func (s *testSchedulerStore) Add(_ context.Context, job *store.ScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sj := storeJobToSchedulerJob(job)
	err := s.ss.Add(sj)
	if err == nil {
		// Copy back generated ID
		job.ID = sj.ID
	}
	return err
}

func (s *testSchedulerStore) Update(_ context.Context, job *store.ScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ss.Update(storeJobToSchedulerJob(job))
}

func (s *testSchedulerStore) Remove(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ss.Remove(id)
}

func (s *testSchedulerStore) toStoreJob(j *scheduler.Job) *store.ScheduledJob {
	return &store.ScheduledJob{
		ID:   j.ID,
		Name: j.Name,
		Mode: string(j.Mode),
		Schedule: store.JobSchedule{
			Cron:     j.Schedule.Cron,
			Timezone: j.Schedule.Timezone,
		},
		Payload: store.JobPayload{
			Flow:         j.Payload.Flow,
			Params:       j.Payload.Params,
			Instructions: j.Payload.Instructions,
		},
		Delivery: store.JobDelivery{
			Channel: j.Delivery.Channel,
			Target:  j.Delivery.Target,
			Mode:    string(j.Delivery.Mode),
		},
		Enabled:   j.Enabled,
		CreatedAt: j.CreatedAt,
		LastRun:   j.LastRun,
	}
}

// newTestSchedulerStore creates a store.SchedulerStore backed by a temp file for testing.
func newTestSchedulerStore(t *testing.T) store.SchedulerStore {
	t.Helper()
	dir := t.TempDir()
	ss, err := scheduler.NewStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return &testSchedulerStore{ss: ss}
}

func TestHandleCreateJob_FlatJSON(t *testing.T) {
	ss := newTestSchedulerStore(t)

	// This is the flat JSON format sent by SchedulerHTTPAccess.AddJob(),
	// which serializes a tools.SchedulerJob directly.
	flatJSON := `{
		"name": "Test Report",
		"mode": "adaptive",
		"cron": "30 18 * * *",
		"timezone": "America/New_York",
		"instructions": "Do the thing",
		"enabled": true
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs", bytes.NewBufferString(flatJSON))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handleCreateJob(rr, req, ss)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Decode the response to get the created job
	var created apiJob
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify the nested schedule fields were populated correctly
	if created.Schedule.Cron != "30 18 * * *" {
		t.Errorf("Schedule.Cron = %q, want %q", created.Schedule.Cron, "30 18 * * *")
	}
	if created.Schedule.Timezone != "America/New_York" {
		t.Errorf("Schedule.Timezone = %q, want %q", created.Schedule.Timezone, "America/New_York")
	}
	if created.Name != "Test Report" {
		t.Errorf("Name = %q, want %q", created.Name, "Test Report")
	}
	if created.Mode != "adaptive" {
		t.Errorf("Mode = %q, want %q", created.Mode, "adaptive")
	}
	if created.Payload.Instructions != "Do the thing" {
		t.Errorf("Payload.Instructions = %q, want %q", created.Payload.Instructions, "Do the thing")
	}
	if !created.Enabled {
		t.Error("expected Enabled = true")
	}

	// Verify the job was persisted in the store
	stored := ss.Get(context.Background(), created.ID)
	if stored == nil {
		t.Fatal("job not found in store after creation")
	}
	if stored.Schedule.Cron != "30 18 * * *" {
		t.Errorf("stored Schedule.Cron = %q, want %q", stored.Schedule.Cron, "30 18 * * *")
	}
	if stored.Schedule.Timezone != "America/New_York" {
		t.Errorf("stored Schedule.Timezone = %q, want %q", stored.Schedule.Timezone, "America/New_York")
	}
}

func TestHandleCreateJob_RoutineMode(t *testing.T) {
	ss := newTestSchedulerStore(t)

	flatJSON := `{
		"name": "Daily Flow",
		"mode": "routine",
		"cron": "0 9 * * 1-5",
		"timezone": "UTC",
		"flow": "my_flow",
		"params": {"symbol": "AAPL"},
		"channel": "telegram",
		"target": "12345",
		"enabled": true
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs", bytes.NewBufferString(flatJSON))
	rr := httptest.NewRecorder()
	handleCreateJob(rr, req, ss)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var created apiJob
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if created.Schedule.Cron != "0 9 * * 1-5" {
		t.Errorf("Schedule.Cron = %q, want %q", created.Schedule.Cron, "0 9 * * 1-5")
	}
	if created.Payload.Flow != "my_flow" {
		t.Errorf("Payload.Flow = %q, want %q", created.Payload.Flow, "my_flow")
	}
	if created.Payload.Params["symbol"] != "AAPL" {
		t.Errorf("Payload.Params[symbol] = %q, want %q", created.Payload.Params["symbol"], "AAPL")
	}
	if created.Delivery.Channel != "telegram" {
		t.Errorf("Delivery.Channel = %q, want %q", created.Delivery.Channel, "telegram")
	}
	if created.Delivery.Target != "12345" {
		t.Errorf("Delivery.Target = %q, want %q", created.Delivery.Target, "12345")
	}
}

func TestHandleCreateJob_NoScheduler(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs", bytes.NewBufferString("{}"))
	rr := httptest.NewRecorder()
	handleCreateJob(rr, req, nil)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rr.Code)
	}
}

func TestHandleCreateJob_InvalidJSON(t *testing.T) {
	ss := newTestSchedulerStore(t)

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs", bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()
	handleCreateJob(rr, req, ss)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}
