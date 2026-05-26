package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// --- SchedulerStore ---

// schedulerSelectCols matches the column order used by scanScheduledJobRow.
const schedulerSelectCols = `id, name, schedule, mode, payload, status, last_run_at, next_run_at, created_at, last_status, last_error, consecutive_failures`

type sqliteSchedulerStore struct {
	db *sql.DB
}

func (s *sqliteSchedulerStore) List(ctx context.Context) []*store.ScheduledJob {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+schedulerSelectCols+` FROM scheduled_jobs ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var jobs []*store.ScheduledJob
	for rows.Next() {
		j, err := scanScheduledJobRow(rows)
		if err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func (s *sqliteSchedulerStore) Get(ctx context.Context, id string) *store.ScheduledJob {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+schedulerSelectCols+` FROM scheduled_jobs WHERE id = ?`, id)
	j, err := scanScheduledJobRow(row)
	if err != nil {
		return nil
	}
	return j
}

func (s *sqliteSchedulerStore) GetByName(ctx context.Context, name string) *store.ScheduledJob {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+schedulerSelectCols+` FROM scheduled_jobs WHERE name = ?`, name)
	j, err := scanScheduledJobRow(row)
	if err != nil {
		return nil
	}
	return j
}

func (s *sqliteSchedulerStore) Add(ctx context.Context, job *store.ScheduledJob) error {
	if job.ID == "" {
		job.ID = uuid.New().String()
	}
	combinedJSON := buildSchedulerPayload(job)
	now := formatTime(time.Now())
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_jobs (id, name, schedule, mode, payload, status, last_status, last_error, consecutive_failures, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO NOTHING`,
		job.ID, job.Name, job.Schedule.Cron, job.Mode, string(combinedJSON),
		enabledToStatus(job.Enabled), job.LastStatus, job.LastError, job.ConsecutiveFailures,
		nilStr(job.OwnerID), formatTime(job.CreatedAt), now)
	return err
}

func (s *sqliteSchedulerStore) Update(ctx context.Context, job *store.ScheduledJob) error {
	combinedJSON := buildSchedulerPayload(job)
	now := formatTime(time.Now())
	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_jobs SET name = ?, schedule = ?, mode = ?, payload = ?, status = ?,
		 last_run_at = ?, next_run_at = ?, last_status = ?, last_error = ?,
		 consecutive_failures = ?, updated_at = ?
		 WHERE id = ?`,
		job.Name, job.Schedule.Cron, job.Mode, string(combinedJSON),
		enabledToStatus(job.Enabled), nilTimePtr(job.LastRun), nilTimePtr(job.NextRun),
		job.LastStatus, job.LastError, job.ConsecutiveFailures, now, job.ID)
	return err
}

func (s *sqliteSchedulerStore) Remove(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_jobs WHERE id = ?`, id)
	return err
}

// scannable is satisfied by both *sql.Row and *sql.Rows.
type scannable interface {
	Scan(dest ...any) error
}

// scanScheduledJobRow scans a row into a ScheduledJob.
// Column order must match schedulerSelectCols.
func scanScheduledJobRow(row scannable) (*store.ScheduledJob, error) {
	j := &store.ScheduledJob{}
	var schedule string
	var payloadJSON string
	var enabledStatus string
	var lastRunAt, nextRunAt sql.NullString
	var createdAt string

	err := row.Scan(
		&j.ID, &j.Name, &schedule, &j.Mode,
		&payloadJSON, &enabledStatus, &lastRunAt, &nextRunAt, &createdAt,
		&j.LastStatus, &j.LastError, &j.ConsecutiveFailures,
	)
	if err != nil {
		return nil, err
	}

	j.Schedule.Cron = schedule
	j.Enabled = enabledStatus == "active"
	j.CreatedAt = parseTime(createdAt)

	if lastRunAt.Valid {
		t := parseTime(lastRunAt.String)
		j.LastRun = &t
	}
	if nextRunAt.Valid {
		t := parseTime(nextRunAt.String)
		j.NextRun = &t
	}

	// Decode the combined payload JSON (same format as pgstore).
	if payloadJSON != "" {
		var combined map[string]json.RawMessage
		if json.Unmarshal([]byte(payloadJSON), &combined) == nil {
			if p, ok := combined["payload"]; ok {
				_ = json.Unmarshal(p, &j.Payload)
			}
			if d, ok := combined["delivery"]; ok {
				_ = json.Unmarshal(d, &j.Delivery)
			}
			if s, ok := combined["schedule_def"]; ok {
				_ = json.Unmarshal(s, &j.Schedule)
			}
			if o, ok := combined["owner_id"]; ok {
				_ = json.Unmarshal(o, &j.OwnerID)
			}
		}
	}

	return j, nil
}

// buildSchedulerPayload serializes schedule, payload, delivery, and owner into a single JSON value.
func buildSchedulerPayload(job *store.ScheduledJob) []byte {
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

// enabledToStatus maps Enabled boolean to the DB status column.
func enabledToStatus(enabled bool) string {
	if enabled {
		return "active"
	}
	return "paused"
}

// nilTimePtr formats a *time.Time for insertion, returning nil for nil pointers.
func nilTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// --- FleetTemplateStore ---

type sqliteFleetTemplateStore struct {
	db *sql.DB
}

func (s *sqliteFleetTemplateStore) GetFleet(ctx context.Context, key string) (any, bool) {
	var definition string
	err := s.db.QueryRowContext(ctx,
		`SELECT definition FROM fleet_templates WHERE key = ?`, key).Scan(&definition)
	if err != nil {
		return nil, false
	}
	var result interface{}
	if err := json.Unmarshal([]byte(definition), &result); err != nil {
		return nil, false
	}
	return result, true
}

func (s *sqliteFleetTemplateStore) ListFleets(ctx context.Context) []store.FleetTemplateSummary {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, name, definition FROM fleet_templates ORDER BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var templates []store.FleetTemplateSummary
	for rows.Next() {
		var key string
		var name sql.NullString
		var definition string
		if err := rows.Scan(&key, &name, &definition); err != nil {
			continue
		}
		templates = append(templates, store.FleetTemplateSummary{
			Key:  key,
			Name: name.String,
		})
	}
	return templates
}

func (s *sqliteFleetTemplateStore) Save(ctx context.Context, key string, fleet any) error {
	data, err := json.Marshal(fleet)
	if err != nil {
		return err
	}
	id := uuid.New().String()
	now := formatTime(time.Now())
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO fleet_templates (id, key, definition, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET definition = excluded.definition, updated_at = excluded.updated_at`,
		id, key, string(data), now, now)
	return err
}

func (s *sqliteFleetTemplateStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM fleet_templates WHERE key = ?`, key)
	return err
}

func (s *sqliteFleetTemplateStore) Count(ctx context.Context) int {
	var count int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fleet_templates`).Scan(&count)
	return count
}

func (s *sqliteFleetTemplateStore) Reload(_ context.Context) error {
	return nil // No-op; SQLite reads are always fresh
}

// --- FleetPlanStore ---

type sqliteFleetPlanStore struct {
	db *sql.DB
}

func (s *sqliteFleetPlanStore) GetPlan(ctx context.Context, key string) (any, bool) {
	var definition string
	err := s.db.QueryRowContext(ctx,
		`SELECT definition FROM fleet_plans WHERE key = ?`, key).Scan(&definition)
	if err != nil {
		return nil, false
	}
	var result interface{}
	if err := json.Unmarshal([]byte(definition), &result); err != nil {
		return nil, false
	}
	return result, true
}

func (s *sqliteFleetPlanStore) ListPlans(ctx context.Context) []store.FleetPlanSummary {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, name, definition FROM fleet_plans ORDER BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var plans []store.FleetPlanSummary
	for rows.Next() {
		var key string
		var name sql.NullString
		var definition string
		if err := rows.Scan(&key, &name, &definition); err != nil {
			continue
		}
		plans = append(plans, store.FleetPlanSummary{
			Key:  key,
			Name: name.String,
		})
	}
	return plans
}

func (s *sqliteFleetPlanStore) Save(ctx context.Context, plan any) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	// Extract key from the plan
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("plan must be a JSON object with a 'key' field")
	}
	key, _ := parsed["key"].(string)
	if key == "" {
		return fmt.Errorf("plan must have a 'key' field")
	}

	id := uuid.New().String()
	now := formatTime(time.Now())
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO fleet_plans (id, key, definition, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET definition = excluded.definition, updated_at = excluded.updated_at`,
		id, key, string(data), now, now)
	return err
}

func (s *sqliteFleetPlanStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM fleet_plans WHERE key = ?`, key)
	return err
}

func (s *sqliteFleetPlanStore) Count(ctx context.Context) int {
	var count int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fleet_plans`).Scan(&count)
	return count
}

func (s *sqliteFleetPlanStore) Reload(_ context.Context) error {
	return nil
}

func (s *sqliteFleetPlanStore) GetPlanYAML(ctx context.Context, key string) (string, error) {
	var yaml sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT yaml_content FROM fleet_plans WHERE key = ?`, key).Scan(&yaml)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("plan %q not found", key)
	}
	if err != nil {
		return "", err
	}
	return yaml.String, nil
}

func (s *sqliteFleetPlanStore) SavePlanYAML(ctx context.Context, key string, yamlContent string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE fleet_plans SET yaml_content = ?, updated_at = ? WHERE key = ?`,
		yamlContent, formatTime(time.Now()), key)
	return err
}

// --- DrillReportStore ---

type sqliteDrillReportStore struct {
	db *sql.DB
}

func (s *sqliteDrillReportStore) SaveReport(ctx context.Context, report *store.DrillReport) error {
	if report.ID == "" {
		report.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO drill_reports (id, suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.ID, report.Suite, report.Status, nilStr(report.Summary),
		report.DurationMs, report.ReportData,
		nilTime(report.StartedAt), nilTime(report.FinishedAt),
		nilStr(report.CreatedBy), formatTime(time.Now()))
	return err
}

func (s *sqliteDrillReportStore) GetLatestReport(ctx context.Context, suite string) (*store.DrillReport, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at
		 FROM drill_reports WHERE suite = ? ORDER BY created_at DESC LIMIT 1`, suite)

	r := &store.DrillReport{}
	var summary, startedAt, finishedAt, createdBy sql.NullString
	var createdAt string
	err := row.Scan(&r.ID, &r.Suite, &r.Status, &summary, &r.DurationMs, &r.ReportData,
		&startedAt, &finishedAt, &createdBy, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Summary = summary.String
	r.CreatedBy = createdBy.String
	r.CreatedAt = parseTime(createdAt)
	if startedAt.Valid {
		r.StartedAt = parseTime(startedAt.String)
	}
	if finishedAt.Valid {
		r.FinishedAt = parseTime(finishedAt.String)
	}
	return r, nil
}

func (s *sqliteDrillReportStore) ListReports(ctx context.Context) ([]*store.DrillReport, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at
		 FROM drill_reports ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*store.DrillReport
	for rows.Next() {
		r := &store.DrillReport{}
		var summary, startedAt, finishedAt, createdBy sql.NullString
		var createdAt string
		if err := rows.Scan(&r.ID, &r.Suite, &r.Status, &summary, &r.DurationMs, &r.ReportData,
			&startedAt, &finishedAt, &createdBy, &createdAt); err != nil {
			return nil, err
		}
		r.Summary = summary.String
		r.CreatedBy = createdBy.String
		r.CreatedAt = parseTime(createdAt)
		if startedAt.Valid {
			r.StartedAt = parseTime(startedAt.String)
		}
		if finishedAt.Valid {
			r.FinishedAt = parseTime(finishedAt.String)
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

func (s *sqliteDrillReportStore) DeleteReportsForSuite(ctx context.Context, suite string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM drill_reports WHERE suite = ?`, suite)
	return err
}
