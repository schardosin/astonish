package filestore

import (
	"github.com/schardosin/astonish/pkg/scheduler"
	"github.com/schardosin/astonish/pkg/store"
)

// SchedulerStoreWrapper wraps the existing scheduler.Store behind the
// store.SchedulerStore interface.
type SchedulerStoreWrapper struct {
	inner *scheduler.Store
}

// NewSchedulerStore creates a SchedulerStore backed by the existing file-based scheduler store.
func NewSchedulerStore(ss *scheduler.Store) store.SchedulerStore {
	return &SchedulerStoreWrapper{inner: ss}
}

// Inner returns the underlying scheduler.Store for code that still needs
// direct access during the transition period.
func (w *SchedulerStoreWrapper) Inner() *scheduler.Store {
	return w.inner
}

func (w *SchedulerStoreWrapper) List() []*store.ScheduledJob {
	jobs := w.inner.List()
	result := make([]*store.ScheduledJob, len(jobs))
	for i, j := range jobs {
		result[i] = convertJob(j)
	}
	return result
}

func (w *SchedulerStoreWrapper) Get(id string) *store.ScheduledJob {
	j := w.inner.Get(id)
	if j == nil {
		return nil
	}
	return convertJob(j)
}

func (w *SchedulerStoreWrapper) GetByName(name string) *store.ScheduledJob {
	j := w.inner.GetByName(name)
	if j == nil {
		return nil
	}
	return convertJob(j)
}

func (w *SchedulerStoreWrapper) Add(job *store.ScheduledJob) error {
	return w.inner.Add(convertToInternalJob(job))
}

func (w *SchedulerStoreWrapper) Update(job *store.ScheduledJob) error {
	return w.inner.Update(convertToInternalJob(job))
}

func (w *SchedulerStoreWrapper) Remove(id string) error {
	return w.inner.Remove(id)
}

func convertJob(j *scheduler.Job) *store.ScheduledJob {
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
		},
		Enabled:             j.Enabled,
		CreatedAt:           j.CreatedAt,
		LastRun:             j.LastRun,
		LastStatus:          string(j.LastStatus),
		LastError:           j.LastError,
		NextRun:             j.NextRun,
		ConsecutiveFailures: j.ConsecutiveFailures,
	}
}

func convertToInternalJob(j *store.ScheduledJob) *scheduler.Job {
	return &scheduler.Job{
		ID:   j.ID,
		Name: j.Name,
		Mode: scheduler.JobMode(j.Mode),
		Schedule: scheduler.JobSchedule{
			Cron:     j.Schedule.Cron,
			Timezone: j.Schedule.Timezone,
		},
		Payload: scheduler.JobPayload{
			Flow:         j.Payload.Flow,
			Params:       j.Payload.Params,
			Instructions: j.Payload.Instructions,
		},
		Delivery: scheduler.JobDelivery{
			Channel: j.Delivery.Channel,
			Target:  j.Delivery.Target,
		},
		Enabled:             j.Enabled,
		CreatedAt:           j.CreatedAt,
		LastRun:             j.LastRun,
		LastStatus:          scheduler.JobStatus(j.LastStatus),
		LastError:           j.LastError,
		NextRun:             j.NextRun,
		ConsecutiveFailures: j.ConsecutiveFailures,
	}
}

// Compile-time check.
var _ store.SchedulerStore = (*SchedulerStoreWrapper)(nil)
