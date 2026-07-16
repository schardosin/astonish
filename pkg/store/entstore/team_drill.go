package entstore

import (
	"context"
	"encoding/json"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	teament "github.com/SAP/astonish/ent/team"
	"github.com/SAP/astonish/ent/team/drillreport"
	"github.com/SAP/astonish/pkg/store"
)

// teamDrillReportStore implements store.DrillReportStore using the Ent team client.
type teamDrillReportStore struct {
	client *teament.Client
}

var _ store.DrillReportStore = (*teamDrillReportStore)(nil)

func (s *teamDrillReportStore) SaveReport(ctx context.Context, report *store.DrillReport) error {
	var reportData map[string]any
	if report.ReportData != nil {
		if err := json.Unmarshal(report.ReportData, &reportData); err != nil {
			reportData = map[string]any{"raw": string(report.ReportData)}
		}
	} else {
		reportData = map[string]any{}
	}

	create := s.client.DrillReport.Create().
		SetSuite(report.Suite).
		SetStatus(report.Status).
		SetSummary(report.Summary).
		SetDurationMs(report.DurationMs).
		SetReportData(reportData).
		SetStartedAt(report.StartedAt).
		SetFinishedAt(report.FinishedAt)

	if report.ID != "" {
		if id, err := uuid.Parse(report.ID); err == nil {
			create.SetID(id)
		}
	}

	if report.CreatedBy != "" {
		if uid, err := uuid.Parse(report.CreatedBy); err == nil {
			create.SetCreatedBy(uid)
		}
	}

	_, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: DrillReportStore.SaveReport: %w", err)
	}
	return nil
}

func (s *teamDrillReportStore) GetLatestReport(ctx context.Context, suite string) (*store.DrillReport, error) {
	ent, err := s.client.DrillReport.Query().
		Where(drillreport.SuiteEQ(suite)).
		Order(drillreport.ByCreatedAt(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entstore: DrillReportStore.GetLatestReport: %w", err)
	}
	return entDrillReportToStore(ent), nil
}

func (s *teamDrillReportStore) ListReports(ctx context.Context) ([]*store.DrillReport, error) {
	reports, err := s.client.DrillReport.Query().
		Order(drillreport.ByCreatedAt(sql.OrderDesc())).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entstore: DrillReportStore.ListReports: %w", err)
	}

	result := make([]*store.DrillReport, len(reports))
	for i, r := range reports {
		result[i] = entDrillReportToStore(r)
	}
	return result, nil
}

func (s *teamDrillReportStore) DeleteReportsForSuite(ctx context.Context, suite string) error {
	_, err := s.client.DrillReport.Delete().
		Where(drillreport.SuiteEQ(suite)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: DrillReportStore.DeleteReportsForSuite: %w", err)
	}
	return nil
}

// --- Helpers ---

func entDrillReportToStore(r *teament.DrillReport) *store.DrillReport {
	report := &store.DrillReport{
		ID:         r.ID.String(),
		Suite:      r.Suite,
		Status:     r.Status,
		Summary:    r.Summary,
		DurationMs: r.DurationMs,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		CreatedAt:  r.CreatedAt,
	}
	if r.CreatedBy != nil {
		report.CreatedBy = r.CreatedBy.String()
	}
	if r.ReportData != nil {
		if data, err := json.Marshal(r.ReportData); err == nil {
			report.ReportData = data
		}
	}
	return report
}
