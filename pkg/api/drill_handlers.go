package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	adrill "github.com/schardosin/astonish/pkg/drill"
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
	Name        string              `json:"name"`
	Description string              `json:"description"`
	File        string              `json:"file"`
	SuiteConfig any                 `json:"suite_config,omitempty"`
	Drills      []DrillListItem     `json:"drills"`
	LastReport  *adrill.SuiteReport `json:"last_report,omitempty"`
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

// ListDrillSuitesHandler handles GET /api/drills
func ListDrillSuitesHandler(w http.ResponseWriter, _ *http.Request) {
	dirs := adrill.DefaultDrillDirs()
	suites, err := adrill.DiscoverSuites(dirs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

		// Try to load the latest report for status
		report := loadLatestReport(s.Name)
		if report != nil {
			item.LastStatus = report.Status
			item.LastSummary = report.Summary
			ts := report.FinishedAt.Format("2006-01-02T15:04:05Z")
			item.LastRunAt = &ts
		}

		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// GetDrillSuiteHandler handles GET /api/drills/{suite}
func GetDrillSuiteHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]
	dirs := adrill.DefaultDrillDirs()

	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

// GetDrillHandler handles GET /api/drills/{suite}/drills/{name}
func GetDrillHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]
	drillName := vars["name"]

	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	http.Error(w, "drill not found", http.StatusNotFound)
}

// DeleteDrillSuiteHandler handles DELETE /api/drills/{suite}
func DeleteDrillSuiteHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]
	dirs := adrill.DefaultDrillDirs()

	deleted, err := adrill.DeleteSuite(dirs, suiteName, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "deleted",
		"deleted": deleted,
	})
}

// DeleteDrillHandler handles DELETE /api/drills/{suite}/drills/{name}
func DeleteDrillHandler(w http.ResponseWriter, r *http.Request) {
	drillName := mux.Vars(r)["name"]
	dirs := adrill.DefaultDrillDirs()

	deletedPath, suiteName, err := adrill.DeleteTest(dirs, drillName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "deleted",
		"deleted": []string{deletedPath},
		"suite":   suiteName,
	})
}

// ListDrillReportsHandler handles GET /api/drill-reports
func ListDrillReportsHandler(w http.ResponseWriter, _ *http.Request) {
	reportsDir, err := config.GetReportsDir()
	if err != nil {
		log.Printf("[drill] Failed to get reports dir: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]DrillReportListItem{})
		return
	}
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		// No reports directory yet — return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]DrillReportListItem{})
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// GetDrillReportHandler handles GET /api/drill-reports/{suite}
func GetDrillReportHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]
	report := loadLatestReport(suiteName)
	if report == nil {
		http.Error(w, "no report found for suite", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// loadLatestReport loads the most recent report for a suite from disk.
func loadLatestReport(suiteName string) *adrill.SuiteReport {
	reportsDir, err := config.GetReportsDir()
	if err != nil {
		return nil
	}

	// Check standard report locations
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

	// Also check if a report was saved with a sanitized name
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Match suite name case-insensitively or with common variations
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

// findSuiteFile locates a suite's file path by name.
func findSuiteFile(suiteName string) (string, error) {
	dirs := adrill.DefaultDrillDirs()
	suite, err := adrill.FindSuite(dirs, suiteName)
	if err != nil {
		return "", err
	}
	return suite.File, nil
}

// findDrillFile locates a drill's file path by suite and drill name.
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

// GetDrillYAMLHandler handles GET /api/drills/{suite}/drills/{name}/yaml
func GetDrillYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]
	drillName := vars["name"]

	filePath, err := findDrillFile(suiteName, drillName)
	if err != nil {
		http.Error(w, "drill not found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "failed to read drill file: "+err.Error(), http.StatusInternalServerError)
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

	filePath, err := findDrillFile(suiteName, drillName)
	if err != nil {
		http.Error(w, "drill not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate that the YAML parses
	var check map[string]interface{}
	if err := yaml.Unmarshal(body, &check); err != nil {
		http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Write the raw bytes to preserve user formatting/comments
	if err := os.WriteFile(filePath, body, 0644); err != nil {
		http.Error(w, "failed to write drill file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "saved",
		"suite":  suiteName,
		"drill":  drillName,
	})
}

// GetSuiteYAMLHandler handles GET /api/drills/{suite}/yaml
func GetSuiteYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]

	filePath, err := findSuiteFile(suiteName)
	if err != nil {
		http.Error(w, "suite not found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "failed to read suite file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// SaveSuiteYAMLHandler handles PUT /api/drills/{suite}/yaml
func SaveSuiteYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	suiteName := vars["suite"]

	filePath, err := findSuiteFile(suiteName)
	if err != nil {
		http.Error(w, "suite not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate that the YAML parses
	var check map[string]interface{}
	if err := yaml.Unmarshal(body, &check); err != nil {
		http.Error(w, "Invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Write the raw bytes to preserve user formatting/comments
	if err := os.WriteFile(filePath, body, 0644); err != nil {
		http.Error(w, "failed to write suite file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "saved",
		"suite":  suiteName,
	})
}
