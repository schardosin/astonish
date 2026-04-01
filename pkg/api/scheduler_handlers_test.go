package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/schardosin/astonish/pkg/scheduler"
)

// newTestScheduler creates a scheduler backed by a temp store for testing.
func newTestScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	dir := t.TempDir()
	store, err := scheduler.NewStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return scheduler.New(store, nil, nil, log.Default())
}

func TestHandleCreateJob_FlatJSON(t *testing.T) {
	sched := newTestScheduler(t)
	SetScheduler(sched)
	defer SetScheduler(nil)

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

	handleCreateJob(rr, req, sched)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Decode the response to get the created job
	var created scheduler.Job
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
	if string(created.Mode) != "adaptive" {
		t.Errorf("Mode = %q, want %q", created.Mode, "adaptive")
	}
	if created.Payload.Instructions != "Do the thing" {
		t.Errorf("Payload.Instructions = %q, want %q", created.Payload.Instructions, "Do the thing")
	}
	if !created.Enabled {
		t.Error("expected Enabled = true")
	}

	// Verify the job was persisted in the store
	stored := sched.Store().Get(created.ID)
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
	sched := newTestScheduler(t)
	SetScheduler(sched)
	defer SetScheduler(nil)

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
	handleCreateJob(rr, req, sched)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var created scheduler.Job
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
	sched := newTestScheduler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs", bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()
	handleCreateJob(rr, req, sched)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}
