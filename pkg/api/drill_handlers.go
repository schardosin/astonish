package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/SAP/astonish/pkg/store"
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
	Template    string          `json:"template,omitempty"`
	SuiteConfig any             `json:"suite_config,omitempty"`
	Drills      []DrillListItem `json:"drills"`
	LastReport  any             `json:"last_report,omitempty"` // json.RawMessage from report store
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
	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	suiteFlows := fs.ListFlowsByType(r.Context(), suiteFlowTypes)
	drillFlows := fs.ListFlowsByType(r.Context(), drillFlowTypes)

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
		if yamlContent, err := fs.GetFlow(r.Context(), sf.Name); err == nil {
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
			if rpt, err := rptStore.GetLatestReport(r.Context(), sf.Name); err == nil && rpt != nil {
				item.LastStatus = rpt.Status
				item.LastSummary = rpt.Summary
				ts := rpt.FinishedAt.Format("2006-01-02T15:04:05Z")
				item.LastRunAt = &ts
			}
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	yamlContent, err := fs.GetFlow(r.Context(), suiteName)
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
	drillFlows := fs.ListFlowsByType(r.Context(), drillFlowTypes)
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
		if dy, dErr := fs.GetFlow(r.Context(), d.Name); dErr == nil {
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

	suiteConfig := parsed.SuiteConfig
	if suiteConfig == nil {
		suiteConfig = map[string]any{}
	}
	template := strings.TrimSpace(parsed.Template)
	if t, ok := suiteConfig["template"].(string); ok && strings.TrimSpace(t) != "" {
		template = strings.TrimSpace(t)
	}
	if template != "" {
		suiteConfig["template"] = template
	}

	detail := DrillSuiteDetail{
		Name:        suiteName,
		Description: parsed.Description,
		Template:    template,
		SuiteConfig: suiteConfig,
		Drills:      drills,
	}

	// Latest report from team-scoped store.
	if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
		if rpt, rptErr := rptStore.GetLatestReport(r.Context(), suiteName); rptErr == nil && rpt != nil {
			detail.LastReport = json.RawMessage(rpt.ReportData)
		}
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	yamlContent, err := fs.GetFlow(r.Context(), drillName)
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
}

// ------------------------------------------------------------------
// Delete Suite / Drill
// ------------------------------------------------------------------

// DeleteDrillSuiteHandler handles DELETE /api/drills/{suite}
func DeleteDrillSuiteHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	var deleted []string

	// Delete child drills first.
	drillFlows := fs.ListFlowsByType(r.Context(), drillFlowTypes)
	for _, d := range drillFlows {
		if d.Suite == suiteName {
			if err := fs.DeleteFlow(r.Context(), d.Name); err == nil {
				deleted = append(deleted, d.Name)
			}
		}
	}

	// Delete the suite itself.
	if err := fs.DeleteFlow(r.Context(), suiteName); err != nil {
		respondError(w, http.StatusNotFound, "suite not found")
		return
	}
	deleted = append(deleted, suiteName)

	// Also clean up drill reports.
	if rptStore := drillReportStoreFromRequest(r); rptStore != nil {
		_ = rptStore.DeleteReportsForSuite(r.Context(), suiteName)
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	if err := fs.DeleteFlow(r.Context(), drillName); err != nil {
		respondError(w, http.StatusNotFound, "drill not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status":  "deleted",
		"deleted": []string{drillName},
		"suite":   suiteName,
	})
}

// ------------------------------------------------------------------
// Reports
// ------------------------------------------------------------------

// ListDrillReportsHandler handles GET /api/drill-reports
func ListDrillReportsHandler(w http.ResponseWriter, r *http.Request) {
	rptStore := drillReportStoreFromRequest(r)
	if rptStore == nil {
		respondError(w, http.StatusServiceUnavailable, "drill reports require platform mode")
		return
	}

	reports, err := rptStore.ListReports(r.Context())
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
}

// GetDrillReportHandler handles GET /api/drill-reports/{suite}
func GetDrillReportHandler(w http.ResponseWriter, r *http.Request) {
	suiteName := mux.Vars(r)["suite"]

	rptStore := drillReportStoreFromRequest(r)
	if rptStore == nil {
		respondError(w, http.StatusServiceUnavailable, "drill reports require platform mode")
		return
	}

	rpt, err := rptStore.GetLatestReport(r.Context(), suiteName)
	if err != nil || rpt == nil {
		respondError(w, http.StatusNotFound, "no report found for suite")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(rpt.ReportData)
}

// ------------------------------------------------------------------
// YAML Editors
// ------------------------------------------------------------------

// GetDrillYAMLHandler handles GET /api/drills/{suite}/drills/{name}/yaml
func GetDrillYAMLHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	drillName := vars["name"]

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	yamlContent, err := fs.GetFlow(r.Context(), drillName)
	if err != nil {
		respondError(w, http.StatusNotFound, "drill not found")
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write([]byte(yamlContent))
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	if err := fs.SaveFlow(r.Context(), drillName, string(body)); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save drill: "+err.Error())
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	yamlContent, err := fs.GetFlow(r.Context(), suiteName)
	if err != nil {
		respondError(w, http.StatusNotFound, "suite not found")
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write([]byte(yamlContent))
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

	fs := flowStoreFromRequest(r)
	if fs == nil {
		respondError(w, http.StatusServiceUnavailable, "drill management requires platform mode")
		return
	}

	if err := fs.SaveFlow(r.Context(), suiteName, string(body)); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save suite: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"status": "saved",
		"suite":  suiteName,
	})
}
