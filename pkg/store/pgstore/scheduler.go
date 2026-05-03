package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgSchedulerStore implements store.SchedulerStore for PostgreSQL.
type pgSchedulerStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *pgSchedulerStore) tableName() string {
	return pgx.Identifier{s.schema, "scheduled_jobs"}.Sanitize()
}

func (s *pgSchedulerStore) List() []*store.ScheduledJob {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, name, schedule, mode, payload, status, last_run_at, next_run_at, created_at
		 FROM %s ORDER BY name`, s.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var jobs []*store.ScheduledJob
	for rows.Next() {
		job, err := scanScheduledJob(rows)
		if err != nil {
			continue
		}
		jobs = append(jobs, &job)
	}
	return jobs
}

func (s *pgSchedulerStore) Get(id string) *store.ScheduledJob {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, name, schedule, mode, payload, status, last_run_at, next_run_at, created_at
		 FROM %s WHERE id = $1`, s.tableName()),
		id,
	)
	job, err := scanScheduledJob(row)
	if err != nil {
		return nil
	}
	return &job
}

func (s *pgSchedulerStore) GetByName(name string) *store.ScheduledJob {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, name, schedule, mode, payload, status, last_run_at, next_run_at, created_at
		 FROM %s WHERE name = $1`, s.tableName()),
		name,
	)
	job, err := scanScheduledJob(row)
	if err != nil {
		return nil
	}
	return &job
}

func (s *pgSchedulerStore) Add(job *store.ScheduledJob) error {
	ctx := context.Background()
	schedJSON, _ := json.Marshal(job.Schedule)
	payloadJSON, _ := json.Marshal(job.Payload)
	deliveryJSON, _ := json.Marshal(job.Delivery)

	// Combine payload + delivery into a single JSONB column
	combined := map[string]any{
		"schedule_def": json.RawMessage(schedJSON),
		"payload":      json.RawMessage(payloadJSON),
		"delivery":     json.RawMessage(deliveryJSON),
	}
	combinedJSON, _ := json.Marshal(combined)

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, name, schedule, mode, payload, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (id) DO NOTHING`,
		s.tableName()),
		job.ID, job.Name, job.Schedule.Cron, job.Mode, combinedJSON,
		statusStr(job.Enabled), job.CreatedAt,
	)
	return err
}

func (s *pgSchedulerStore) Update(job *store.ScheduledJob) error {
	ctx := context.Background()
	schedJSON, _ := json.Marshal(job.Schedule)
	payloadJSON, _ := json.Marshal(job.Payload)
	deliveryJSON, _ := json.Marshal(job.Delivery)

	combined := map[string]any{
		"schedule_def": json.RawMessage(schedJSON),
		"payload":      json.RawMessage(payloadJSON),
		"delivery":     json.RawMessage(deliveryJSON),
	}
	combinedJSON, _ := json.Marshal(combined)

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET name = $2, schedule = $3, mode = $4, payload = $5,
		 status = $6, last_run_at = $7, next_run_at = $8, updated_at = now()
		 WHERE id = $1`, s.tableName()),
		job.ID, job.Name, job.Schedule.Cron, job.Mode, combinedJSON,
		statusStr(job.Enabled), nilTimePtrField(job.LastRun), nilTimePtrField(job.NextRun),
	)
	return err
}

func (s *pgSchedulerStore) Remove(id string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE id = $1`, s.tableName()),
		id,
	)
	return err
}

func scanScheduledJob(row scannable) (store.ScheduledJob, error) {
	var job store.ScheduledJob
	var schedule string
	var payloadJSON []byte
	var status string

	err := row.Scan(&job.ID, &job.Name, &schedule, &job.Mode,
		&payloadJSON, &status, &job.LastRun, &job.NextRun, &job.CreatedAt)
	if err != nil {
		return job, fmt.Errorf("failed to scan scheduled job: %w", err)
	}

	job.Schedule.Cron = schedule
	job.Enabled = status == "active"
	job.LastStatus = status

	// Try to decode the combined payload JSON
	if len(payloadJSON) > 0 {
		var combined map[string]json.RawMessage
		if json.Unmarshal(payloadJSON, &combined) == nil {
			if p, ok := combined["payload"]; ok {
				_ = json.Unmarshal(p, &job.Payload)
			}
			if d, ok := combined["delivery"]; ok {
				_ = json.Unmarshal(d, &job.Delivery)
			}
			if s, ok := combined["schedule_def"]; ok {
				_ = json.Unmarshal(s, &job.Schedule)
			}
		}
	}

	return job, nil
}

func statusStr(enabled bool) string {
	if enabled {
		return "active"
	}
	return "paused"
}

func nilTimePtrField(t *time.Time) *time.Time {
	return t
}
