package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

// DrillSuiteListItem represents a drill suite in listing responses.
type DrillSuiteListItem struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	File        string  `json:"file"`
	DrillCount  int     `json:"drill_count"`
	Template    string  `json:"template,omitempty"`
	LastStatus  string  `json:"last_status"`            // "passed", "failed", "error", "unknown"
	LastRunAt   *string `json:"last_run_at,omitempty"`  // ISO timestamp or null
	LastSummary string  `json:"last_summary,omitempty"` // e.g. "3/3 tests passed"
}

// DrillSuiteDetail represents full suite detail.
type DrillSuiteDetail struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	File        string          `json:"file"`
	SuiteConfig any             `json:"suite_config,omitempty"`
	Drills      []DrillListItem `json:"drills"`
	LastReport  any             `json:"last_report,omitempty"` // *adrill.SuiteReport or json.RawMessage
}

// DrillListItem represents a single drill in listing responses.
type DrillListItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	File        string   `json:"file"`
	Tags        []string `json:"tags,omitempty"`
	StepCount   int      `json:"step_count"`
	Timeout     int      `json:"timeout,omitempty"`
}

// DrillDetailResponse represents a single drill's full config.
type DrillDetailResponse struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	File        string   `json:"file"`
	Suite       string   `json:"suite"`
	Tags        []string `json:"tags,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	StepTimeout int      `json:"step_timeout,omitempty"`
	OnFail      string   `json:"on_fail,omitempty"`
	Nodes       any      `json:"nodes"`
	Flow        any      `json:"flow,omitempty"`
}

// DrillReportListItem represents a report in listing responses.
type DrillReportListItem struct {
	Suite      string `json:"suite"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Duration   int64  `json:"duration_ms"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	DrillCount int    `json:"drill_count"`
}

// drillFlowTypes are the flow types that represent drills.
var drillFlowTypes = []string{"drill", "test"}

// suiteFlowTypes are the flow types that represent drill suites.
var suiteFlowTypes = []string{"drill_suite", "test_suite"}

// ------------------------------------------------------------------
// Helpers for platform-mode drill data from the flow store
// ------------------------------------------------------------------

// flowStoreFromRequest returns the team-scoped FlowStore if running in
// platform mode, or nil if in personal mode.
func flowStoreFromRequest(r *http.Request) store.FlowStore {
	if svc := store.FromRequest(r); svc != nil && svc.Flows != nil {
		return svc.Flows
	}
	return nil
}

// drillReportStoreFromRequest returns the team-scoped DrillReportStore if
// running in platform mode, or nil if in personal mode.
func drillReportStoreFromRequest(r *http.Request) store.DrillReportStore {
	if svc := store.FromRequest(r); svc != nil && svc.DrillReports != nil {
		return svc.DrillReports
	}
	return nil
}

// parsedDrillFlow is the subset of an AgentConfig YAML that we parse
// to construct drill API responses without importing the full config
// package (which would be circular in some helpers).
type parsedDrillFlow struct {
	Type        string         `yaml:"type"`
	Description string         `yaml:"description"`
	Suite       string         `yaml:"suite"`
	Tags        []string       `yaml:"tags"`
	Timeout     int            `yaml:"timeout"`
	StepTimeout int            `yaml:"step_timeout"`
	OnFail      string         `yaml:"on_fail"`
	Nodes       any            `yaml:"nodes"`
	Flow        any            `yaml:"flow"`
	Template    string         `yaml:"template"`
	SuiteConfig map[string]any `yaml:"suite_config"`
}

// ------------------------------------------------------------------
// Suite / Drill List
// ------------------------------------------------------------------

// ListDrillSuitesHandler handles GET /api/drills
func ListDrillSuitesHandler(w http.ResponseWriter, r *http.Request) {
	// Platform mode: discover suites from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		suiteFlows := fs.ListFlowsByType(suiteFlowTypes)
		drillFlows := fs.ListFlowsByType(drillFlowTypes)

		// Count drills per suite.
		drillCounts := make(map[string]int)
		for _, d := range drillFlows {
			drillCounts[d.Suite]++
		}

		rptStore := drillReportStoreFromRequest(r)
		items := make([]DrillSuiteListItem, 0, len(suiteFlows))
		for _, sf := range suiteFlows {
			item := DrillSuiteListItem{
				Name:        sf.Name,
				Description: sf.Description,
				DrillCount:  drillCounts[sf.Name],
				LastStatus:  "unknown",
			}

			// Parse the YAML to extract template from suite_config.
			if yamlContent, err := fs.GetFlow(sf.Name); err == nil {
				var parsed parsedDrillFlow
				if yaml.Unmarshal([]byte(yamlContent), &parsed) == nil {
					if parsed.Template != "" {
						item.Template = parsed.Template
					}
					if t, ok := parsed.SuiteConfig["template"].(string); ok && t != "" {
						item.Template = t
					}
				}
			}

			// Latest report from the team-scoped report store.
			if rptStore != nil {
				if rpt, err := rptStore.GetLatestReport(sf.Name); err == nil && rpt != nil {
					item.LastStatus = rpt.Status
					item.LastSummary = rpt.Summary
					ts := rpt.FinishedAt.Format("2006-01-02T15:04:05Z")
					item.LastRunAt = &ts
				}
			}

			items = append(items, item)
		}

		respondJSON(w, http.StatusOK, items)
		return
	}

	// Personal mode fallback: filesystem discovery.
	dirs := adrill.DefaultDrillDirs()
	suites, err := adrill.DiscoverSuites(dirs)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]DrillSuiteListItem, 0, len(suites))
	for _, s := range suites {
		item := DrillSuiteListItem{
			Name:        s.Name,
			Description: s.Config.Description,
			File:        s.File,
			DrillCount:  len(s.Tests),
			LastStatus:  "unknown",
		}
		if s.Config.SuiteConfig != nil && s.Config.SuiteConfig.Template != "" {
			item.Template = s.Config.SuiteConfig.Template
		}

		report := loadLatestReport(s.Name)
		if report != nil {
			item.LastStatus = report.Status
			item.LastSummary = report.Summary
			ts := report.FinishedAt.Format("2006-01-02T15:04:05Z")
			item.LastRunAt = &ts
		}

		items = append(items, item)
	}

	respondJSON(w, http.StatusOK, items)
}

// ------------------------------------------------------------------
// Suite Detail
// ------------------------------------------------------------------

// GetDrillSuiteHandler handles GET /api/drills/{suite}
func GetDrillSuiteHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	// Platform mode: read from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		yamlContent, err := fs.GetFlow(suiteName)
		if err != nil {
			respondError(w, http.StatusNotFound, "suite not found")
			return
		}

		var parsed parsedDrillFlow
		if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to parse suite YAML")
			return
		}

		// Find child drills for this suite.
		drillFlows := fs.ListFlowsByType(drillFlowTypes)
		drills := make([]DrillListItem, 0)
		for _, d := range drillFlows {
			if d.Suite != suiteName {
				continue
			}
			item := DrillListItem{
				Name:        d.Name,
				Description: d.Description,
				Tags:        d.Tags,
			}
			// Parse drill YAML for step count / timeout.
			if dy, dErr := fs.GetFlow(d.Name); dErr == nil {
				var dp parsedDrillFlow
				if yaml.Unmarshal([]byte(dy), &dp) == nil {
					if nodes, ok := dp.Nodes.([]any); ok {
						item.StepCount = len(nodes)
					}
					item.Timeout = dp.Timeout
				}
			}
			drills = append(drills, item)
		}

		detail := DrillSuiteDetail{
			Name:        suiteName,
			Description: parsed.Description,
			SuiteConfig: parsed.SuiteConfig,
			Drills:      drills,
		}

		// Latest report from team-scoped store.
		if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
			if rpt, rptErr := rptStore.GetLatestReport(suiteName); rptErr == nil && rpt != nil {
				detail.LastReport = json.RawMessage(rpt.ReportData)
			}
		}

		respondJSON(w, http.StatusOK, detail)
		return
	}

	// Personal mode fallback.
	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	drills := make([]DrillListItem, 0, len(suite.Tests))
	for _, t := range suite.Tests {
		item := DrillListItem{
			Name:        t.Name,
			Description: t.Config.Description,
			File:        t.File,
			StepCount:   len(t.Config.Nodes),
		}
		if t.Config.DrillConfig != nil {
			item.Tags = t.Config.DrillConfig.Tags
			item.Timeout = t.Config.DrillConfig.Timeout
		}
		drills = append(drills, item)
	}

	detail := DrillSuiteDetail{
		Name:        suite.Name,
		Description: suite.Config.Description,
		File:        suite.File,
		SuiteConfig: suite.Config.SuiteConfig,
		Drills:      drills,
		LastReport:  loadLatestReport(suiteName),
	}

	respondJSON(w, http.StatusOK, detail)
}

// ------------------------------------------------------------------
// Drill Detail
// ------------------------------------------------------------------

// GetDrillHandler handles GET /api/drills/{suite}/drills/{name}
func GetDrillHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]
	drillName := vars["name"]

	// Platform mode: read from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		yamlContent, err := fs.GetFlow(drillName)
		if err != nil {
			respondError(w, http.StatusNotFound, "drill not found")
			return
		}

		var parsed parsedDrillFlow
		if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to parse drill YAML")
			return
		}

		resp := DrillDetailResponse{
			Name:        drillName,
			Description: parsed.Description,
			Suite:       suiteName,
			Tags:        parsed.Tags,
			Timeout:     parsed.Timeout,
			StepTimeout: parsed.StepTimeout,
			OnFail:      parsed.OnFail,
			Nodes:       parsed.Nodes,
			Flow:        parsed.Flow,
		}
		respondJSON(w, http.StatusOK, resp)
		return
	}

	// Personal mode fallback.
	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	for _, t := range suite.Tests {
		if t.Name == drillName {
			resp := DrillDetailResponse{
				Name:        t.Name,
				Description: t.Config.Description,
				File:        t.File,
				Suite:       t.Config.Suite,
				Nodes:       t.Config.Nodes,
				Flow:        t.Config.Flow,
			}
			if t.Config.DrillConfig != nil {
				resp.Tags = t.Config.DrillConfig.Tags
				resp.Timeout = t.Config.DrillConfig.Timeout
				resp.StepTimeout = t.Config.DrillConfig.StepTimeout
				resp.OnFail = t.Config.DrillConfig.OnFail
			}
			respondJSON(w, http.StatusOK, resp)
			return
		}
	}

	respondError(w, http.StatusNotFound, "drill not found")
}

// ------------------------------------------------------------------
// Delete Suite / Drill
// ------------------------------------------------------------------

// DeleteDrillSuiteHandler handles DELETE /api/drills/{suite}
func DeleteDrillSuiteHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	// Platform mode: delete from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		var deleted []string

		// Delete child drills first.
		drillFlows := fs.ListFlowsByType(drillFlowTypes)
		for _, d := range drillFlows {
			if d.Suite == suiteName {
				if err := fs.DeleteFlow(d.Name); err == nil {
					deleted = append(deleted, d.Name)
				}
			}
		}

		// Delete the suite itself.
		if err := fs.DeleteFlow(suiteName); err != nil {
			respondError(w, http.StatusNotFound, "suite not found")
			return
		}
		deleted = append(deleted, suiteName)

		// Also clean up drill reports.
		if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
			_ = rptStore.DeleteReportsForSuite(suiteName)
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"status":  "deleted",
			"deleted": deleted,
		})
		return
	}

	// Personal mode fallback.
	dirs := adrill.DefaultDrillDirs()
	deleted, err := adrill.DeleteSuite(dirs, suiteName, true)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"status":  "deleted",
		"deleted": deleted,
	})
}

// DeleteDrillHandler handles DELETE /api/drills/{suite}/drills/{name}
func DeleteDrillHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]
	drillName := vars["name"]

	// Platform mode: delete from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		if err := fs.DeleteFlow(drillName); err != nil {
			respondError(w, http.StatusNotFound, "drill not found")
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"status":  "deleted",
			"deleted": []string{drillName},
			"suite":   suiteName,
		})
		return
	}

	// Personal mode fallback.
	dirs := adrill.DefaultDrillDirs()
	deletedPath, fallbackSuite, err := adrill.DeleteTest(dirs, drillName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"status":  "deleted",
		"deleted": []string{deletedPath},
		"suite":   fallbackSuite,
	})
}

// ------------------------------------------------------------------
// Reports
// ------------------------------------------------------------------

// ListDrillReportsHandler handles GET /api/drill-reports
func ListDrillReportsHandler(w http.ResponseWriter, r *http.Request) {
	// Platform mode: use the team-scoped drill report store.
	if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
		reports, err := rptStore.ListReports()
		if err != nil {
			slog.Error("failed to list drill reports from store", "error", err)
			respondJSON(w, http.StatusOK, []DrillReportListItem{})
			return
		}
		items := make([]DrillReportListItem, 0, len(reports))
		for _, rpt := range reports {
			items = append(items, DrillReportListItem{
				Suite:      rpt.Suite,
				Status:     rpt.Status,
				Summary:    rpt.Summary,
				Duration:   rpt.DurationMs,
				StartedAt:  rpt.StartedAt.Format("2006-01-02T15:04:05Z"),
				FinishedAt: rpt.FinishedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		respondJSON(w, http.StatusOK, items)
		return
	}

	// Personal mode fallback: filesystem.
	reportsDir, err := config.GetReportsDir()
	if err != nil {
		slog.Error("failed to get reports dir", "component", "drill", "error", err)
		respondJSON(w, http.StatusOK, []DrillReportListItem{})
		return
	}
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		respondJSON(w, http.StatusOK, []DrillReportListItem{})
		return
	}

	items := make([]DrillReportListItem, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		reportPath := filepath.Join(reportsDir, entry.Name(), "suite_report.json")
		report, err := adrill.LoadReport(reportPath)
		if err != nil {
			continue
		}
		items = append(items, DrillReportListItem{
			Suite:      report.Suite,
			Status:     report.Status,
			Summary:    report.Summary,
			Duration:   report.Duration,
			StartedAt:  report.StartedAt.Format("2006-01-02T15:04:05Z"),
			FinishedAt: report.FinishedAt.Format("2006-01-02T15:04:05Z"),
			DrillCount: len(report.Tests),
		})
	}

	respondJSON(w, http.StatusOK, items)
}

// GetDrillReportHandler handles GET /api/drill-reports/{suite}
func GetDrillReportHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	// Platform mode: use the team-scoped drill report store.
	if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
		rpt, err := rptStore.GetLatestReport(suiteName)
		if err != nil || rpt == nil {
			respondError(w, http.StatusNotFound, "no report found for suite")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(rpt.ReportData)
		return
	}

	// Personal mode fallback.
	report := loadLatestReport(suiteName)
	if report == nil {
		respondError(w, http.StatusNotFound, "no report found for suite")
		return
	}
	respondJSON(w, http.StatusOK, report)
}

// ------------------------------------------------------------------
// YAML Editors
// ------------------------------------------------------------------

// GetDrillYAMLHandler handles GET /api/drills/{suite}/drills/{name}/yaml
func GetDrillYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	drillName := vars["name"]

	// Platform mode: read from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		yamlContent, err := fs.GetFlow(drillName)
		if err != nil {
			respondError(w, http.StatusNotFound, "drill not found")
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Write([]byte(yamlContent))
		return
	}

	// Personal mode fallback.
	suiteName := vars["suite"]
	filePath, err := findDrillFile(suiteName, drillName)
	if err != nil {
		respondError(w, http.StatusNotFound, "drill not found")
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read drill file: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// SaveDrillYAMLHandler handles PUT /api/drills/{suite}/drills/{name}/yaml
func SaveDrillYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]
	drillName := vars["name"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Validate that the YAML parses.
	var check map[string]interface{}
	if err := yaml.Unmarshal(body, &check); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid YAML: "+err.Error())
		return
	}

	// Platform mode: save to the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		if err := fs.SaveFlow(drillName, string(body)); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save drill: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"status": "saved",
			"suite":  suiteName,
			"drill":  drillName,
		})
		return
	}

	// Personal mode fallback.
	filePath, err := findDrillFile(suiteName, drillName)
	if err != nil {
		respondError(w, http.StatusNotFound, "drill not found")
		return
	}
	if err := os.WriteFile(filePath, body, 0644); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to write drill file: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status": "saved",
		"suite":  suiteName,
		"drill":  drillName,
	})
}

// GetSuiteYAMLHandler handles GET /api/drills/{suite}/yaml
func GetSuiteYAMLHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	// Platform mode: read from the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		yamlContent, err := fs.GetFlow(suiteName)
		if err != nil {
			respondError(w, http.StatusNotFound, "suite not found")
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Write([]byte(yamlContent))
		return
	}

	// Personal mode fallback.
	filePath, err := findSuiteFile(suiteName)
	if err != nil {
		respondError(w, http.StatusNotFound, "suite not found")
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read suite file: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// SaveSuiteYAMLHandler handles PUT /api/drills/{suite}/yaml
func SaveSuiteYAMLHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Validate that the YAML parses.
	var check map[string]interface{}
	if err := yaml.Unmarshal(body, &check); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid YAML: "+err.Error())
		return
	}

	// Platform mode: save to the team-scoped flow store.
	if fs := flowStoreFromRequest(r); fs != nil {
		if err := fs.SaveFlow(suiteName, string(body)); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save suite: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"status": "saved",
			"suite":  suiteName,
		})
		return
	}

	// Personal mode fallback.
	filePath, err := findSuiteFile(suiteName)
	if err != nil {
		respondError(w, http.StatusNotFound, "suite not found")
		return
	}
	if err := os.WriteFile(filePath, body, 0644); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to write suite file: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status": "saved",
		"suite":  suiteName,
	})
}

// ------------------------------------------------------------------
// Helpers (personal-mode filesystem only)
// ------------------------------------------------------------------

// loadLatestReport loads the most recent report for a suite from disk.
func loadLatestReport(suiteName string) *adrill.SuiteReport {
	reportsDir, err := config.GetReportsDir()
	if err != nil {
		return nil
	}

	dirs := []string{
		filepath.Join(reportsDir, suiteName),
	}

	for _, dir := range dirs {
		reportPath := filepath.Join(dir, "suite_report.json")
		report, err := adrill.LoadReport(reportPath)
		if err == nil {
			return report
		}
	}

	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), suiteName) {
			reportPath := filepath.Join(reportsDir, entry.Name(), "suite_report.json")
			report, err := adrill.LoadReport(reportPath)
			if err == nil {
				return report
			}
		}
	}

	return nil
}

// findSuiteFile locates a suite's file path by name (personal mode only).
func findSuiteFile(suiteName string) (string, error) {
	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		return "", err
	}
	return suite.File, nil
}

// findDrillFile locates a drill's file path by suite and drill name (personal mode only).
func findDrillFile(suiteName, drillName string) (string, error) {
	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		return "", err
	}
	for _, t := range suite.Tests {
		if t.Name == drillName {
			return t.File, nil
		}
	}
	return "", os.ErrNotExist
}
