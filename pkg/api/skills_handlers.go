package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/skills"
)

// SkillListItem represents a skill in the listing response.
type SkillListItem struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Source       string   `json:"source"`
	Eligible     bool     `json:"eligible"`
	Missing      []string `json:"missing,omitempty"`
	RequireBins  []string `json:"require_bins,omitempty"`
	RequireEnv   []string `json:"require_env,omitempty"`
	OS           []string `json:"os,omitempty"`
	FilePath     string   `json:"file_path,omitempty"`
	HasDirectory bool     `json:"has_directory"`
}

// SkillContentResponse is the response for GET /api/skills/{name}/content.
type SkillContentResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Content     string `json:"content"`
	RawFile     string `json:"raw_file"`
	FilePath    string `json:"file_path,omitempty"`
	Editable    bool   `json:"editable"`
}

// SkillContentUpdateRequest is the request for PUT /api/skills/{name}/content.
type SkillContentUpdateRequest struct {
	RawFile string `json:"raw_file"`
}

// ListSkillsHandler handles GET /api/skills
func ListSkillsHandler(w http.ResponseWriter, r *http.Request) {
	allSkills, err := loadAPISkills()
	if err != nil {
		http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]SkillListItem, 0, len(allSkills))
	for _, s := range allSkills {
		item := SkillListItem{
			Name:         s.Name,
			Description:  s.Description,
			Source:       s.Source,
			Eligible:     s.IsEligible(),
			RequireBins:  s.RequireBins,
			RequireEnv:   s.RequireEnv,
			OS:           s.OS,
			FilePath:     s.FilePath,
			HasDirectory: s.Directory != "",
		}
		if !item.Eligible {
			item.Missing = s.MissingRequirements()
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"skills": items,
	})
}

// GetSkillContentHandler handles GET /api/skills/{name}/content
func GetSkillContentHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	allSkills, err := loadAPISkills()
	if err != nil {
		http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			// Reconstruct the raw file (frontmatter + content)
			rawFile := reconstructRawFile(&s)
			resp := SkillContentResponse{
				Name:        s.Name,
				Description: s.Description,
				Source:      s.Source,
				Content:     s.Content,
				RawFile:     rawFile,
				FilePath:    s.FilePath,
				Editable:    s.Source != "bundled",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
}

// UpdateSkillContentHandler handles PUT /api/skills/{name}/content
func UpdateSkillContentHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	var req SkillContentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.RawFile) == "" {
		http.Error(w, "raw_file cannot be empty", http.StatusBadRequest)
		return
	}

	// Validate the content parses correctly
	_, err := skills.ParseSkillFile("validation", []byte(req.RawFile))
	if err != nil {
		http.Error(w, "Invalid skill file: "+err.Error(), http.StatusBadRequest)
		return
	}

	allSkills, err := loadAPISkills()
	if err != nil {
		http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			if s.Source == "bundled" {
				http.Error(w, "Cannot edit bundled skills", http.StatusForbidden)
				return
			}
			if s.FilePath == "" {
				http.Error(w, "Skill has no file path", http.StatusInternalServerError)
				return
			}

			// Write updated content to the file
			if err := os.WriteFile(s.FilePath, []byte(req.RawFile), 0644); err != nil {
				http.Error(w, "Failed to write skill file: "+err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status": "ok",
			})
			return
		}
	}

	http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
}

// CreateSkillHandler handles POST /api/skills (create new skill from template)
type CreateSkillRequest struct {
	Name string `json:"name"`
}

func CreateSkillHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "Skill name is required", http.StatusBadRequest)
		return
	}

	// Validate name (alphanumeric, hyphens, underscores)
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			http.Error(w, "Skill name must contain only letters, numbers, hyphens, and underscores", http.StatusBadRequest)
			return
		}
	}

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	userDir := skillsCfg.GetUserSkillsDir()
	if userDir == "" {
		http.Error(w, "Cannot determine user skills directory", http.StatusInternalServerError)
		return
	}

	skillDir := filepath.Join(userDir, name)
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err == nil {
		http.Error(w, fmt.Sprintf("Skill %q already exists at %s", name, skillFile), http.StatusConflict)
		return
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		http.Error(w, "Failed to create skill directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	template := fmt.Sprintf(`---
name: %s
description: "TODO: One-line description of what this skill does"
require_bins: []
---

# %s

## When to Use
- TODO: Describe when this skill should be used

## When NOT to Use
- TODO: Describe when other tools are more appropriate

## Common Commands
`+"```"+`
# TODO: Add common commands and patterns
`+"```"+`

## Tips
- TODO: Add tips and best practices
`, name, name)

	if err := os.WriteFile(skillFile, []byte(template), 0644); err != nil {
		http.Error(w, "Failed to write skill file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"file_path": skillFile,
	})
}

// DeleteSkillHandler handles DELETE /api/skills/{name}
func DeleteSkillHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	allSkills, err := loadAPISkills()
	if err != nil {
		http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			if s.Source == "bundled" {
				http.Error(w, "Cannot delete bundled skills", http.StatusForbidden)
				return
			}
			if s.Directory == "" {
				http.Error(w, "Skill has no directory to delete", http.StatusInternalServerError)
				return
			}

			if err := os.RemoveAll(s.Directory); err != nil {
				http.Error(w, "Failed to delete skill: "+err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status": "ok",
			})
			return
		}
	}

	http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
}

// --- Helpers ---

func loadAPISkills() ([]skills.Skill, error) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	workDir, _ := os.Getwd()
	return skills.LoadSkills(
		skillsCfg.GetUserSkillsDir(),
		skillsCfg.ExtraDirs,
		workDir,
		skillsCfg.Allowlist,
	)
}

func reconstructRawFile(s *skills.Skill) string {
	// If we have the file path, read the raw file from disk
	if s.FilePath != "" {
		data, err := os.ReadFile(s.FilePath)
		if err == nil {
			return string(data)
		}
	}
	// Fallback: reconstruct from parsed data
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", s.Name))
	sb.WriteString(fmt.Sprintf("description: %q\n", s.Description))
	if len(s.OS) > 0 {
		sb.WriteString(fmt.Sprintf("os: [%s]\n", strings.Join(quoteStrings(s.OS), ", ")))
	}
	if len(s.RequireBins) > 0 {
		sb.WriteString(fmt.Sprintf("require_bins: [%s]\n", strings.Join(quoteStrings(s.RequireBins), ", ")))
	}
	if len(s.RequireEnv) > 0 {
		sb.WriteString(fmt.Sprintf("require_env: [%s]\n", strings.Join(quoteStrings(s.RequireEnv), ", ")))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(s.Content)
	return sb.String()
}

func quoteStrings(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = fmt.Sprintf("%q", s)
	}
	return result
}
