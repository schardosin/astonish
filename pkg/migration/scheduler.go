package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// jobsFile matches the on-disk jobs.json format from pkg/scheduler.
type jobsFile struct {
	Jobs []schedulerJob `json:"jobs"`
}

type schedulerJob struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Mode                string       `json:"mode"`
	Schedule            jobSchedule  `json:"schedule"`
	Payload             jobPayload   `json:"payload"`
	Delivery            jobDelivery  `json:"delivery"`
	Enabled             bool         `json:"enabled"`
	CreatedAt           time.Time    `json:"created_at"`
	LastRun             *time.Time   `json:"last_run,omitempty"`
	LastStatus          string       `json:"last_status"`
	LastError           string       `json:"last_error,omitempty"`
	NextRun             *time.Time   `json:"next_run,omitempty"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
}

type jobSchedule struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
}

type jobPayload struct {
	Flow         string            `json:"flow,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
}

type jobDelivery struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
}

func (m *Migrator) migrateScheduler(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	jobsPath := filepath.Join(m.configDir, "scheduler", "jobs.json")

	if _, err := os.Stat(jobsPath); os.IsNotExist(err) {
		m.emitProgress(CatScheduler, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatScheduler, 0, 0, "counting", "")

	data, err := os.ReadFile(jobsPath)
	if err != nil {
		m.emitProgress(CatScheduler, 0, 0, "error", "cannot read jobs.json")
		return 0, fmt.Errorf("cannot read jobs.json: %w", err)
	}

	var jf jobsFile
	if err := json.Unmarshal(data, &jf); err != nil {
		m.emitProgress(CatScheduler, 0, 0, "error", "invalid jobs.json format")
		return 0, fmt.Errorf("invalid jobs.json: %w", err)
	}

	total := len(jf.Jobs)
	if total == 0 {
		m.emitProgress(CatScheduler, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatScheduler, 0, total, "migrating", "")

	schedStore := teamDS.ScheduledJobs()
	count := 0

	for _, j := range jf.Jobs {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		storeJob := &store.ScheduledJob{
			ID:   j.ID,
			Name: j.Name,
			Mode: j.Mode,
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
			LastStatus:          j.LastStatus,
			LastError:           j.LastError,
			NextRun:             j.NextRun,
			ConsecutiveFailures: j.ConsecutiveFailures,
		}

		if err := schedStore.Add(storeJob); err != nil {
			return count, fmt.Errorf("failed to add job %q: %w", j.Name, err)
		}

		count++
		m.emitProgress(CatScheduler, count, total, "migrating", "")
	}

	m.emitProgress(CatScheduler, count, total, "done", "")
	return count, nil
}
