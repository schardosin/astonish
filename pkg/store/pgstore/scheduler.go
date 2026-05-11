package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// selectColumns is the column list used by all SELECT queries.
const schedulerSelectCols = `id, name, schedule, mode, payload, status, last_run_at, next_run_at, created_at, last_status, last_error, consecutive_failures`

func (s *pgSchedulerStore) List() []*store.ScheduledJob {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM %s ORDER BY name`, schedulerSelectCols, s.tableName()),
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
		`SELECT %s FROM %s WHERE id = $1`, schedulerSelectCols, s.tableName()),
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
		`SELECT %s FROM %s WHERE name = $1`, schedulerSelectCols, s.tableName()),
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
	combinedJSON := buildCombinedPayload(job)

	// Auto-generate UUID if not provided (the DB column is uuid type and
	// won't accept an empty string)
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, name, schedule, mode, payload, status, last_status, last_error, consecutive_failures, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		 ON CONFLICT (id) DO NOTHING`,
		s.tableName()),
		job.ID, job.Name, job.Schedule.Cron, job.Mode, combinedJSON,
		enabledStatusStr(job.Enabled), job.LastStatus, job.LastError, job.ConsecutiveFailures,
		nullableUUID(job.OwnerID), job.CreatedAt,
	)
	return err
}

func (s *pgSchedulerStore) Update(job *store.ScheduledJob) error {
	ctx := context.Background()
	combinedJSON := buildCombinedPayload(job)

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET name = $2, schedule = $3, mode = $4, payload = $5,
		 status = $6, last_run_at = $7, next_run_at = $8,
		 last_status = $9, last_error = $10, consecutive_failures = $11,
		 updated_at = now()
		 WHERE id = $1`, s.tableName()),
		job.ID, job.Name, job.Schedule.Cron, job.Mode, combinedJSON,
		enabledStatusStr(job.Enabled), nilTimePtrField(job.LastRun), nilTimePtrField(job.NextRun),
		job.LastStatus, job.LastError, job.ConsecutiveFailures,
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

// scanScheduledJob scans a row into a ScheduledJob.
// Column order must match schedulerSelectCols.
func scanScheduledJob(row scannable) (store.ScheduledJob, error) {
	var job store.ScheduledJob
	var schedule string
	var payloadJSON []byte
	var enabledStatus string

	err := row.Scan(
		&job.ID, &job.Name, &schedule, &job.Mode,
		&payloadJSON, &enabledStatus, &job.LastRun, &job.NextRun, &job.CreatedAt,
		&job.LastStatus, &job.LastError, &job.ConsecutiveFailures,
	)
	if err != nil {
		return job, fmt.Errorf("failed to scan scheduled job: %w", err)
	}

	job.Schedule.Cron = schedule
	job.Enabled = enabledStatus == "active"

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
			if o, ok := combined["owner_id"]; ok {
				_ = json.Unmarshal(o, &job.OwnerID)
			}
		}
	}

	return job, nil
}

// buildCombinedPayload serializes schedule, payload, delivery, and owner into a single JSONB value.
func buildCombinedPayload(job *store.ScheduledJob) []byte {
	schedJSON, _ := json.Marshal(job.Schedule)
	payloadJSON, _ := json.Marshal(job.Payload)
	deliveryJSON, _ := json.Marshal(job.Delivery)

	combined := map[string]any{
		"schedule_def": json.RawMessage(schedJSON),
		"payload":      json.RawMessage(payloadJSON),
		"delivery":     json.RawMessage(deliveryJSON),
	}
	if job.OwnerID != "" {
		combined["owner_id"] = job.OwnerID
	}
	combinedJSON, _ := json.Marshal(combined)
	return combinedJSON
}

// enabledStatusStr maps the Enabled boolean to the DB status column.
// This column tracks whether the job is active or paused.
func enabledStatusStr(enabled bool) string {
	if enabled {
		return "active"
	}
	return "paused"
}

func nilTimePtrField(t *time.Time) *time.Time {
	return t
}

// nullableUUID returns nil (SQL NULL) if the string is empty, otherwise the string.
// Used for uuid columns that allow NULL but reject empty strings.
func nullableUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
