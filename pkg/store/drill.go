package store

import "time"

// DrillReport is a drill suite execution report stored in the database.
type DrillReport struct {
	ID         string    `json:"id"`
	Suite      string    `json:"suite"`
	Status     string    `json:"status"`     // "passed", "failed", "error"
	Summary    string    `json:"summary"`    // e.g., "3/3 tests passed"
	DurationMs int64     `json:"duration_ms"`
	ReportData []byte    `json:"report_data"` // full SuiteReport JSON
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// DrillReportStore manages drill test report persistence.
//
// In personal mode, reports are stored in the filesystem (config.GetReportsDir()).
// In platform mode, reports are stored in the team's schema.
type DrillReportStore interface {
	// SaveReport persists a drill report.
	SaveReport(report *DrillReport) error

	// GetLatestReport returns the most recent report for a suite.
	// Returns nil, nil if no report exists.
	GetLatestReport(suite string) (*DrillReport, error)

	// ListReports returns all drill reports, ordered by creation time (newest first).
	ListReports() ([]*DrillReport, error)

	// DeleteReportsForSuite removes all reports for a given suite.
	DeleteReportsForSuite(suite string) error
}
