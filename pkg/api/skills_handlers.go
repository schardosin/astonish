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
	"github.com/schardosin/astonish/pkg/store"
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
	var allItems []SkillListItem

	if svc := store.FromRequest(r); svc != nil && svc.Skills != nil {
		// Platform mode: custom skills from PG + bundled overlay
		items, err := listSkillsPlatform(svc.Skills)
		if err != nil {
			http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
			return
		}
		allItems = items
	} else {
		// Personal mode: load from filesystem (bundled + user dir + extra dirs)
		loaded, err := loadAPISkills()
		if err != nil {
			http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
			return
		}
		allItems = skillsToListItems(loaded)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"skills": allItems,
	})
}

// GetSkillContentHandler handles GET /api/skills/{name}/content
func GetSkillContentHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	if svc := store.FromRequest(r); svc != nil && svc.Skills != nil {
		// Platform mode: try PG first, then bundled
		getSkillContentPlatform(w, svc.Skills, name)
		return
	}

	// Personal mode: load from filesystem
	allSkills, err := loadAPISkills()
	if err != nil {
		http.Error(w, "Failed to load skills: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
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
	parsed, err := skills.ParseSkillFile("validation", []byte(req.RawFile))
	if err != nil {
		http.Error(w, "Invalid skill file: "+err.Error(), http.StatusBadRequest)
		return
	}

	if svc := store.FromRequest(r); svc != nil && svc.Skills != nil {
		// Platform mode: update in PG
		updateSkillContentPlatform(w, r, svc.Skills, name, parsed, req.RawFile)
		return
	}

	// Personal mode: update on filesystem
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

			if err := os.WriteFile(s.FilePath, []byte(req.RawFile), 0644); err != nil {
				http.Error(w, "Failed to write skill file: "+err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

	if svc := store.FromRequest(r); svc != nil && svc.Skills != nil {
		// Platform mode: create in PG
		createSkillPlatform(w, r, svc.Skills, name)
		return
	}

	// Personal mode: create on filesystem
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

	if err := os.WriteFile(skillFile, []byte(skills.NewSkillTemplate(name)), 0644); err != nil {
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

	if svc := store.FromRequest(r); svc != nil && svc.Skills != nil {
		// Platform mode: delete from PG
		deleteSkillPlatform(w, svc.Skills, name)
		return
	}

	// Personal mode: delete from filesystem
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
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
	}

	http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
}

// --- Platform mode helpers ---

// listSkillsPlatform loads custom skills from PG and overlays bundled skills.
// Custom skills take precedence over bundled ones with the same name.
func listSkillsPlatform(skillStore store.SkillStore) ([]SkillListItem, error) {
	// Load bundled skills first
	bundled, err := skills.LoadBundledSkills()
	if err != nil {
		return nil, fmt.Errorf("load bundled skills: %w", err)
	}

	byName := make(map[string]SkillListItem, len(bundled))
	for _, s := range bundled {
		byName[s.Name] = skillToListItem(s.Name, s.Description, "bundled",
			s.IsEligible(), s.MissingRequirements(), s.RequireBins, s.RequireEnv, s.OS, "", false)
	}

	// Load custom skills from PG — these override bundled ones by name
	pgSkills, err := skillStore.List()
	if err != nil {
		return nil, fmt.Errorf("load skills from store: %w", err)
	}
	for _, s := range pgSkills {
		byName[s.Name] = storeSkillToListItem(&s)
	}

	items := make([]SkillListItem, 0, len(byName))
	for _, item := range byName {
		items = append(items, item)
	}

	// Sort by name for deterministic output
	sortListItems(items)
	return items, nil
}

// getSkillContentPlatform retrieves a skill's content from PG or bundled.
func getSkillContentPlatform(w http.ResponseWriter, skillStore store.SkillStore, name string) {
	// Try PG first
	pgSkill, err := skillStore.Get(name)
	if err == nil && pgSkill != nil {
		resp := SkillContentResponse{
			Name:        pgSkill.Name,
			Description: pgSkill.Description,
			Source:      "custom",
			Content:     extractBody(pgSkill.Content),
			RawFile:     pgSkill.Content,
			Editable:    true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Fall back to bundled
	bundled, loadErr := skills.LoadBundledSkills()
	if loadErr != nil {
		http.Error(w, "Failed to load bundled skills: "+loadErr.Error(), http.StatusInternalServerError)
		return
	}
	for _, s := range bundled {
		if strings.EqualFold(s.Name, name) {
			rawFile := reconstructRawFileFromSkillPkg(&s)
			resp := SkillContentResponse{
				Name:        s.Name,
				Description: s.Description,
				Source:      "bundled",
				Content:     s.Content,
				RawFile:     rawFile,
				Editable:    false,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
}

// updateSkillContentPlatform updates a skill in PG. Cannot edit bundled skills.
func updateSkillContentPlatform(w http.ResponseWriter, r *http.Request, skillStore store.SkillStore, name string, parsed *skills.Skill, rawFile string) {
	// Check if this is a bundled skill
	bundled, _ := skills.LoadBundledSkills()
	for _, s := range bundled {
		if strings.EqualFold(s.Name, name) {
			// Only block if there's no custom override in PG
			_, err := skillStore.Get(name)
			if err != nil {
				http.Error(w, "Cannot edit bundled skills. Create a custom skill with the same name to override it.", http.StatusForbidden)
				return
			}
			break
		}
	}

	userID := ""
	if pu := GetPlatformUser(r); pu != nil {
		userID = pu.ID
	}

	skill := &store.Skill{
		Name:      parsed.Name,
		Content:   rawFile,
		CreatedBy: userID,
	}
	if err := skillStore.Save(skill); err != nil {
		http.Error(w, "Failed to save skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// createSkillPlatform creates a new skill in PG from a template.
func createSkillPlatform(w http.ResponseWriter, r *http.Request, skillStore store.SkillStore, name string) {
	// Check if already exists in PG
	existing, _ := skillStore.Get(name)
	if existing != nil {
		http.Error(w, fmt.Sprintf("Skill %q already exists", name), http.StatusConflict)
		return
	}

	userID := ""
	if pu := GetPlatformUser(r); pu != nil {
		userID = pu.ID
	}

	template := skills.NewSkillTemplate(name)
	skill := &store.Skill{
		Name:      name,
		Content:   template,
		CreatedBy: userID,
	}

	if err := skillStore.Save(skill); err != nil {
		http.Error(w, "Failed to create skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// deleteSkillPlatform deletes a custom skill from PG. Cannot delete bundled skills.
func deleteSkillPlatform(w http.ResponseWriter, skillStore store.SkillStore, name string) {
	// Verify it exists in PG (can't delete bundled)
	_, err := skillStore.Get(name)
	if err != nil {
		// Check if it's a bundled skill
		bundled, _ := skills.LoadBundledSkills()
		for _, s := range bundled {
			if strings.EqualFold(s.Name, name) {
				http.Error(w, "Cannot delete bundled skills", http.StatusForbidden)
				return
			}
		}
		http.Error(w, fmt.Sprintf("Skill %q not found", name), http.StatusNotFound)
		return
	}

	if err := skillStore.Delete(name); err != nil {
		http.Error(w, "Failed to delete skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- Personal mode helper ---

func loadAPISkills() ([]skills.Skill, error) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	var skillsCfg config.SkillsConfig
	if appCfg != nil {
		skillsCfg = appCfg.Skills
	}

	workDir, wdErr := os.Getwd()
	if wdErr != nil {
		slog.Warn("failed to get working directory", "error", wdErr)
	}
	return skills.LoadSkills(
		skillsCfg.GetUserSkillsDir(),
		skillsCfg.ExtraDirs,
		workDir,
		skillsCfg.Allowlist,
	)
}

// --- Shared helpers ---

func skillsToListItems(loaded []skills.Skill) []SkillListItem {
	items := make([]SkillListItem, 0, len(loaded))
	for _, s := range loaded {
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
	return items
}

func skillToListItem(name, description, source string, eligible bool, missing, requireBins, requireEnv, os []string, filePath string, hasDirectory bool) SkillListItem {
	item := SkillListItem{
		Name:         name,
		Description:  description,
		Source:       source,
		Eligible:     eligible,
		RequireBins:  requireBins,
		RequireEnv:   requireEnv,
		OS:           os,
		FilePath:     filePath,
		HasDirectory: hasDirectory,
	}
	if !eligible {
		item.Missing = missing
	}
	return item
}

func storeSkillToListItem(s *store.Skill) SkillListItem {
	// Re-parse the raw content to check eligibility
	parsed, err := skills.ParseSkillFile("store:"+s.Name, []byte(s.Content))
	if err != nil {
		return SkillListItem{
			Name:        s.Name,
			Description: s.Description,
			Source:      "custom",
			Eligible:    true,
		}
	}
	return SkillListItem{
		Name:        parsed.Name,
		Description: parsed.Description,
		Source:      "custom",
		Eligible:    parsed.IsEligible(),
		Missing:     parsed.MissingRequirements(),
		RequireBins: parsed.RequireBins,
		RequireEnv:  parsed.RequireEnv,
		OS:          parsed.OS,
	}
}

func reconstructRawFile(s *skills.Skill) string {
	// If we have the file path, read the raw file from disk
	if s.FilePath != "" {
		data, err := os.ReadFile(s.FilePath)
		if err == nil {
			return string(data)
		}
	}
	return reconstructRawFileFromSkillPkg(s)
}

func reconstructRawFileFromSkillPkg(s *skills.Skill) string {
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

// extractBody returns the markdown body after frontmatter from a raw SKILL.md.
// Used to populate the Content field for the API response (which expects body only).
func extractBody(rawFile string) string {
	parsed, err := skills.ParseSkillFile("extract", []byte(rawFile))
	if err != nil {
		return rawFile
	}
	return parsed.Content
}

func quoteStrings(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = fmt.Sprintf("%q", s)
	}
	return result
}

func sortListItems(items []SkillListItem) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Name > items[j].Name {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}
