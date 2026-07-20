package entstore

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	personalent "github.com/SAP/astonish/ent/personal"
	"github.com/SAP/astonish/ent/personal/scheduledjob"
	"github.com/SAP/astonish/pkg/store"
)

// personalSchedulerStore implements store.SchedulerStore using the Ent personal client.
type personalSchedulerStore struct {
	client *personalent.Client
}

var _ store.SchedulerStore = (*personalSchedulerStore)(nil)

func (s *personalSchedulerStore) List(ctx context.Context) []*store.ScheduledJob {
	jobs, err := s.client.ScheduledJob.Query().
		Order(scheduledjob.ByName()).
		All(ctx)
	if err != nil {
		slog.Error("list personal scheduled jobs failed", "error", err)
		return nil
	}

	result := make([]*store.ScheduledJob, len(jobs))
	for i, j := range jobs {
		result[i] = personalEntScheduledJobToStore(j)
	}
	return result
}

func (s *personalSchedulerStore) Get(ctx context.Context, id string) *store.ScheduledJob {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	j, err := s.client.ScheduledJob.Get(ctx, uid)
	if err != nil {
		return nil
	}
	return personalEntScheduledJobToStore(j)
}

func (s *personalSchedulerStore) GetByName(ctx context.Context, name string) *store.ScheduledJob {
	j, err := s.client.ScheduledJob.Query().
		Where(scheduledjob.NameEQ(name)).
		Only(ctx)
	if err != nil {
		return nil
	}
	return personalEntScheduledJobToStore(j)
}

func (s *personalSchedulerStore) Add(ctx context.Context, job *store.ScheduledJob) error {
	create := s.client.ScheduledJob.Create().
		SetName(job.Name).
		SetSchedule(job.Schedule.Cron).
		SetMode(scheduledjob.Mode(job.Mode)).
		SetStatus(scheduledjob.StatusActive)

	if job.ID != "" {
		if id, err := uuid.Parse(job.ID); err == nil {
			create.SetID(id)
		}
	}

	payload := jobToPayloadMap(job)
	create.SetPayload(payload)

	if job.OwnerID != "" {
		if uid, err := uuid.Parse(job.OwnerID); err == nil {
			create.SetCreatedBy(uid)
		}
	}

	if job.NextRun != nil {
		create.SetNextRunAt(*job.NextRun)
	}

	created, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: personal SchedulerStore.Add: %w", err)
	}
	job.ID = created.ID.String()
	job.CreatedAt = created.CreatedAt
	job.Scope = store.JobScopePersonal
	return nil
}

func (s *personalSchedulerStore) Update(ctx context.Context, job *store.ScheduledJob) error {
	uid, err := uuid.Parse(job.ID)
	if err != nil {
		return fmt.Errorf("entstore: personal SchedulerStore.Update: invalid ID: %w", err)
	}

	update := s.client.ScheduledJob.UpdateOneID(uid).
		SetName(job.Name).
		SetSchedule(job.Schedule.Cron).
		SetMode(scheduledjob.Mode(job.Mode)).
		SetLastStatus(job.LastStatus).
		SetLastError(job.LastError).
		SetConsecutiveFailures(job.ConsecutiveFailures)

	if job.Enabled {
		update.SetStatus(scheduledjob.StatusActive)
	} else {
		update.SetStatus(scheduledjob.StatusPaused)
	}

	payload := jobToPayloadMap(job)
	update.SetPayload(payload)

	if job.LastRun != nil {
		update.SetLastRunAt(*job.LastRun)
	} else {
		update.ClearLastRunAt()
	}

	if job.NextRun != nil {
		update.SetNextRunAt(*job.NextRun)
	} else {
		update.ClearNextRunAt()
	}

	return update.Exec(ctx)
}

func (s *personalSchedulerStore) Remove(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("entstore: personal SchedulerStore.Remove: invalid ID: %w", err)
	}
	return s.client.ScheduledJob.DeleteOneID(uid).Exec(ctx)
}

func personalEntScheduledJobToStore(j *personalent.ScheduledJob) *store.ScheduledJob {
	job := &store.ScheduledJob{
		ID:                  j.ID.String(),
		Name:                j.Name,
		Mode:                string(j.Mode),
		Enabled:             j.Status == scheduledjob.StatusActive,
		CreatedAt:           j.CreatedAt,
		LastStatus:          j.LastStatus,
		LastError:           j.LastError,
		ConsecutiveFailures: j.ConsecutiveFailures,
		LastRun:             j.LastRunAt,
		NextRun:             j.NextRunAt,
		Scope:               store.JobScopePersonal,
	}
	if j.CreatedBy != nil {
		job.OwnerID = j.CreatedBy.String()
	}

	job.Schedule = store.JobSchedule{Cron: j.Schedule}
	if tz, ok := j.Payload["timezone"].(string); ok {
		job.Schedule.Timezone = tz
	}
	if teamSlug, ok := j.Payload["team_slug"].(string); ok {
		job.TeamSlug = teamSlug
	}

	job.Payload = payloadMapToJobPayload(j.Payload)
	job.Delivery = payloadMapToJobDelivery(j.Payload)

	return job
}
