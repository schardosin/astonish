package entstore

import (
	"context"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

func TestPersonalSchedulerStore_CRUD(t *testing.T) {
	_, client := setupPersonalStore(t)
	ss := &personalSchedulerStore{client: client}
	ctx := context.Background()

	job := &store.ScheduledJob{
		Name: "morning-digest",
		Mode: "adaptive",
		Schedule: store.JobSchedule{
			Cron:     "0 9 * * *",
			Timezone: "UTC",
		},
		Payload: store.JobPayload{
			Instructions: "Summarize my inbox",
		},
		Delivery: store.JobDelivery{
			Mode: "owner",
		},
		Enabled:    true,
		OwnerID:    "11111111-1111-1111-1111-111111111111",
		TeamSlug:   "engineering",
		LastStatus: "pending",
	}

	if err := ss.Add(ctx, job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if job.Scope != store.JobScopePersonal {
		t.Errorf("Scope = %q, want personal", job.Scope)
	}

	got := ss.Get(ctx, job.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Name != "morning-digest" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.TeamSlug != "engineering" {
		t.Errorf("TeamSlug = %q, want engineering", got.TeamSlug)
	}
	if got.OwnerID != job.OwnerID {
		t.Errorf("OwnerID = %q, want %q", got.OwnerID, job.OwnerID)
	}
	if got.Scope != store.JobScopePersonal {
		t.Errorf("Scope = %q, want personal", got.Scope)
	}

	byName := ss.GetByName(ctx, "morning-digest")
	if byName == nil || byName.ID != job.ID {
		t.Fatal("GetByName failed")
	}

	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	got.Enabled = false
	got.NextRun = &next
	got.LastStatus = "success"
	if err := ss.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated := ss.Get(ctx, job.ID)
	if updated.Enabled {
		t.Error("expected Enabled=false after update")
	}
	if updated.LastStatus != "success" {
		t.Errorf("LastStatus = %q", updated.LastStatus)
	}
	if updated.TeamSlug != "engineering" {
		t.Errorf("TeamSlug lost on update: %q", updated.TeamSlug)
	}

	list := ss.List(ctx)
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := ss.Remove(ctx, job.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if ss.Get(ctx, job.ID) != nil {
		t.Error("expected nil after Remove")
	}
}
