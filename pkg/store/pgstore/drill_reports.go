package pgstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgDrillReportStore implements store.DrillReportStore for PostgreSQL.
type pgDrillReportStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (d *pgDrillReportStore) tableName() string {
	return pgx.Identifier{d.schema, "drill_reports"}.Sanitize()
}

func (d *pgDrillReportStore) SaveReport(ctx context.Context, report *store.DrillReport) error {
	_, err := d.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())`,
		d.tableName()),
		report.Suite, report.Status, report.Summary, report.DurationMs,
		report.ReportData, report.StartedAt, report.FinishedAt, nilIfEmptyStr(report.CreatedBy),
	)
	return err
}

func (d *pgDrillReportStore) GetLatestReport(ctx context.Context, suite string) (*store.DrillReport, error) {
	row := d.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at
		 FROM %s WHERE suite = $1 ORDER BY created_at DESC LIMIT 1`,
		d.tableName()),
		suite,
	)
	return scanDrillReport(row)
}

func (d *pgDrillReportStore) ListReports(ctx context.Context) ([]*store.DrillReport, error) {
	rows, err := d.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, suite, status, summary, duration_ms, report_data, started_at, finished_at, created_by, created_at
		 FROM %s ORDER BY created_at DESC`,
		d.tableName()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*store.DrillReport
	for rows.Next() {
		r, err := scanDrillReportFromRows(rows)
		if err != nil {
			continue
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func (d *pgDrillReportStore) DeleteReportsForSuite(ctx context.Context, suite string) error {
	_, err := d.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE suite = $1`, d.tableName()),
		suite,
	)
	return err
}

func scanDrillReport(row pgx.Row) (*store.DrillReport, error) {
	var r store.DrillReport
	var createdBy *string
	err := row.Scan(&r.ID, &r.Suite, &r.Status, &r.Summary, &r.DurationMs,
		&r.ReportData, &r.StartedAt, &r.FinishedAt, &createdBy, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	return &r, nil
}

func scanDrillReportFromRows(row scannable) (*store.DrillReport, error) {
	var r store.DrillReport
	var createdBy *string
	var startedAt, finishedAt, createdAt time.Time
	err := row.Scan(&r.ID, &r.Suite, &r.Status, &r.Summary, &r.DurationMs,
		&r.ReportData, &startedAt, &finishedAt, &createdBy, &createdAt)
	if err != nil {
		return nil, err
	}
	r.StartedAt = startedAt
	r.FinishedAt = finishedAt
	r.CreatedAt = createdAt
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	return &r, nil
}

// Compile-time check.
var _ store.DrillReportStore = (*pgDrillReportStore)(nil)
