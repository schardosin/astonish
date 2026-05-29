package api

import (
	"context"
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
	Scope        string   `json:"scope,omitempty"` // "bundled", "org", or "team"
	Eligible     bool     `json:"eligible"`
	Editable     bool     `json:"editable"`
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
	Scope       string `json:"scope,omitempty"`
	Content     string `json:"content"`
	RawFile     string `json:"raw_file"`
	FilePath    string `json:"file_path,omitempty"`
	Editable    bool   `json:"editable"`
}

// SkillContentUpdateRequest is the request for PUT /api/skills/{name}/content.
type SkillContentUpdateRequest struct {
	RawFile string `json:"raw_file"`
}

// SkillFileInfo represents metadata about one auxiliary file belonging to a skill.
type SkillFileInfo struct {
	Path         string `json:"path"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
	IsExecutable bool   `json:"is_executable"`
}

// SkillFilesResponse is the response for GET /api/skills/{name}/files
type SkillFilesResponse struct {
	Name  string          `json:"name"`
	Files []SkillFileInfo `json:"files"`
}

// SaveSkillFileRequest is the request body for saving an auxiliary skill file.
type SaveSkillFileRequest struct {
	Content       string `json:"content"`
	IsExecutable  bool   `json:"is_executable"`
}

// SkillsListResponse is the response for GET /api/skills.
type SkillsListResponse struct {
	Skills      []SkillListItem `json:"skills"`
	IsTeamAdmin bool            `json:"is_team_admin"`
	IsOrgAdmin  bool            `json:"is_org_admin"`
}

// ListSkillsHandler handles GET /api/skills
//
// Query params:
//   - scope=team: return only team skills
//   - scope=org: return only org skills
//   - (empty): return merged view (bundled + org + team, team overrides org overrides bundled)
func ListSkillsHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode: scope-aware listing
		items, err := listSkillsPlatform(svc, scope)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to load skills: "+err.Error())
			return
		}
		resp := SkillsListResponse{
			Skills:      items,
			IsTeamAdmin: IsTeamAdmin(r),
			IsOrgAdmin:  !isPlatformMode(r) || CanManageOrg(GetPlatformUser(r)),
		}
		respondJSON(w, http.StatusOK, resp)
		return
	}

	// Personal mode: load from filesystem (bundled + user dir + extra dirs)
	loaded, err := loadAPISkills()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load skills: "+err.Error())
		return
	}
	items := skillsToListItems(loaded)
	resp := SkillsListResponse{
		Skills:      items,
		IsTeamAdmin: true, // personal mode: user is always admin
		IsOrgAdmin:  true,
	}
	respondJSON(w, http.StatusOK, resp)
}

// GetSkillContentHandler handles GET /api/skills/{name}/content
//
// Query params:
//   - scope=team: look in team store only
//   - scope=org: look in org store only
//   - (empty): try team → org → bundled
func GetSkillContentHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode: scope-aware get
		getSkillContentPlatform(w, r, svc, name, scope)
		return
	}

	// Personal mode: load from filesystem
	allSkills, err := loadAPISkills()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load skills: "+err.Error())
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
			respondJSON(w, http.StatusOK, resp)
			return
		}
	}

	respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found", name))
}

// ListSkillFilesHandler handles GET /api/skills/{name}/files
// Returns the list of auxiliary files for a skill (from skill_files table in platform mode,
// or from the skill directory in personal mode).
func ListSkillFilesHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode
		var skillStore store.SkillStore
		if scope == "team" && svc.TeamSkills != nil {
			skillStore = svc.TeamSkills
		} else if scope == "org" && svc.Skills != nil {
			skillStore = svc.Skills
		} else if svc.TeamSkills != nil {
			skillStore = svc.TeamSkills // default to team
		} else {
			skillStore = svc.Skills
		}

		if skillStore == nil {
			respondError(w, http.StatusNotFound, "No skill store available for scope")
			return
		}

		files, err := skillStore.ListFiles(r.Context(), name)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to list skill files: "+err.Error())
			return
		}

		resp := SkillFilesResponse{Name: name}
		for _, f := range files {
			resp.Files = append(resp.Files, SkillFileInfo{
				Path:         f.Path,
				Filename:     f.Filename,
				Size:         f.SizeBytes,
				IsExecutable: f.IsExecutable,
			})
		}
		respondJSON(w, http.StatusOK, resp)
		return
	}

	// Personal mode fallback: try to read from filesystem (best effort)
	// For now we return empty — full personal multi-file support can be added later.
	respondJSON(w, http.StatusOK, SkillFilesResponse{Name: name, Files: []SkillFileInfo{}})
}

// GetSkillFileHandler handles GET /api/skills/{name}/file?path=...&filename=...
// Returns the content of one auxiliary file.
func GetSkillFileHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	path := r.URL.Query().Get("path")
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		respondError(w, http.StatusBadRequest, "filename query parameter is required")
		return
	}

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		var skillStore store.SkillStore
		scope := r.URL.Query().Get("scope")
		if scope == "team" && svc.TeamSkills != nil {
			skillStore = svc.TeamSkills
		} else if scope == "org" && svc.Skills != nil {
			skillStore = svc.Skills
		} else if svc.TeamSkills != nil {
			skillStore = svc.TeamSkills
		} else {
			skillStore = svc.Skills
		}

		if skillStore == nil {
			respondError(w, http.StatusNotFound, "No skill store available")
			return
		}

		f, err := skillStore.GetFile(r.Context(), name, path, filename)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to get skill file: "+err.Error())
			return
		}
		if f == nil {
			respondError(w, http.StatusNotFound, "File not found")
			return
		}

		// For now return as JSON (later we can support raw content type)
		resp := map[string]any{
			"name":          name,
			"path":          f.Path,
			"filename":      f.Filename,
			"content":       f.Content,
			"is_executable": f.IsExecutable,
			"size":          f.SizeBytes,
		}
		respondJSON(w, http.StatusOK, resp)
		return
	}

	respondError(w, http.StatusNotImplemented, "File retrieval not yet supported in personal mode")
}

// SaveSkillFileHandler handles PUT /api/skills/{name}/file?path=...&filename=...
// Creates or updates an auxiliary file for a skill.
func SaveSkillFileHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	path := r.URL.Query().Get("path")
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		respondError(w, http.StatusBadRequest, "filename query parameter is required")
		return
	}

	var req SaveSkillFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		scope := r.URL.Query().Get("scope")
		targetStore := resolveSkillStoreForWrite(w, r, svc, scope)
		if targetStore == nil {
			return
		}

		file := &store.SkillFile{
			Path:         path,
			Filename:     filename,
			Content:      req.Content,
			IsExecutable: req.IsExecutable,
			SizeBytes:    int64(len(req.Content)),
		}

		if err := targetStore.SaveFile(r.Context(), name, file); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save skill file: "+err.Error())
			return
		}

		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	respondError(w, http.StatusNotImplemented, "Saving skill files not yet supported in personal mode")
}

// DeleteSkillFileHandler handles DELETE /api/skills/{name}/file?path=...&filename=...
func DeleteSkillFileHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	path := r.URL.Query().Get("path")
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		respondError(w, http.StatusBadRequest, "filename query parameter is required")
		return
	}

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		scope := r.URL.Query().Get("scope")
		targetStore := resolveSkillStoreForWrite(w, r, svc, scope)
		if targetStore == nil {
			return
		}

		if err := targetStore.DeleteFile(r.Context(), name, path, filename); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to delete skill file: "+err.Error())
			return
		}

		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	respondError(w, http.StatusNotImplemented, "Deleting skill files not yet supported in personal mode")
}

// UpdateSkillContentHandler handles PUT /api/skills/{name}/content
//
// Query params:
//   - scope=team: save to team store (requires team admin)
//   - scope=org: save to org store (requires org admin)
func UpdateSkillContentHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	var req SkillContentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.RawFile) == "" {
		respondError(w, http.StatusBadRequest, "raw_file cannot be empty")
		return
	}

	// Validate the content parses correctly
	parsed, err := skills.ParseSkillFile("validation", []byte(req.RawFile))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid skill file: "+err.Error())
		return
	}

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode: scope-aware update with auth check
		targetStore := resolveSkillStoreForWrite(w, r, svc, scope)
		if targetStore == nil {
			return // auth error already written
		}
		updateSkillContentPlatform(w, r, targetStore, name, parsed, req.RawFile, scope)
		return
	}

	// Personal mode: update on filesystem
	allSkills, err := loadAPISkills()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load skills: "+err.Error())
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			if s.Source == "bundled" {
				respondError(w, http.StatusForbidden, "Cannot edit bundled skills")
				return
			}
			if s.FilePath == "" {
				respondError(w, http.StatusInternalServerError, "Skill has no file path")
				return
			}

			if err := os.WriteFile(s.FilePath, []byte(req.RawFile), 0644); err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to write skill file: "+err.Error())
				return
			}

			respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
	}

	respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found", name))
}

// CreateSkillHandler handles POST /api/skills (create new skill from template)
type CreateSkillRequest struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"` // "team" or "org" (platform mode)
}

func CreateSkillHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(w, http.StatusBadRequest, "Skill name is required")
		return
	}

	// Validate name (alphanumeric, hyphens, underscores)
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			respondError(w, http.StatusBadRequest, "Skill name must contain only letters, numbers, hyphens, and underscores")
			return
		}
	}

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode: scope-aware create with auth check
		scope := req.Scope
		if scope == "" {
			scope = "team" // default to team scope for new skills
		}
		targetStore := resolveSkillStoreForWrite(w, r, svc, scope)
		if targetStore == nil {
			return // auth error already written
		}
		createSkillPlatform(w, r, targetStore, name)
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
		respondError(w, http.StatusInternalServerError, "Cannot determine user skills directory")
		return
	}

	skillDir := filepath.Join(userDir, name)
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err == nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("Skill %q already exists at %s", name, skillFile))
		return
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create skill directory: "+err.Error())
		return
	}

	if err := os.WriteFile(skillFile, []byte(skills.NewSkillTemplate(name)), 0644); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to write skill file: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"file_path": skillFile,
	})
}

// DeleteSkillHandler handles DELETE /api/skills/{name}
//
// Query params:
//   - scope=team: delete from team store (requires team admin)
//   - scope=org: delete from org store (requires org admin)
func DeleteSkillHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc != nil && (svc.Skills != nil || svc.TeamSkills != nil) {
		// Platform mode: scope-aware delete with auth check
		targetStore := resolveSkillStoreForWrite(w, r, svc, scope)
		if targetStore == nil {
			return // auth error already written
		}
		deleteSkillPlatform(w, targetStore, name, scope)
		return
	}

	// Personal mode: delete from filesystem
	allSkills, err := loadAPISkills()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load skills: "+err.Error())
		return
	}

	for _, s := range allSkills {
		if strings.EqualFold(s.Name, name) {
			if s.Source == "bundled" {
				respondError(w, http.StatusForbidden, "Cannot delete bundled skills")
				return
			}
			if s.Directory == "" {
				respondError(w, http.StatusInternalServerError, "Skill has no directory to delete")
				return
			}

			if err := os.RemoveAll(s.Directory); err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to delete skill: "+err.Error())
				return
			}

			respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
	}

	respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found", name))
}

// --- Platform mode helpers ---

// resolveSkillStoreForWrite returns the appropriate SkillStore for a write operation
// based on the requested scope. Returns nil and writes an error if auth fails.
func resolveSkillStoreForWrite(w http.ResponseWriter, r *http.Request, svc *store.Services, scope string) store.SkillStore {
	switch scope {
	case "team":
		if svc.TeamSkills == nil {
			respondError(w, http.StatusServiceUnavailable, "Team skills store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamSkills
	case "org":
		if svc.Skills == nil {
			respondError(w, http.StatusServiceUnavailable, "Org skills store not available")
			return nil
		}
		// Org skills require org admin
		user := GetPlatformUser(r)
		if user == nil {
			respondError(w, http.StatusUnauthorized, "Authentication required")
			return nil
		}
		if !CanManageOrg(user) {
			respondError(w, http.StatusForbidden, "Organization admin access required to manage org skills")
			return nil
		}
		return svc.Skills
	default:
		// No scope specified — default to team
		if svc.TeamSkills == nil {
			respondError(w, http.StatusServiceUnavailable, "Team skills store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamSkills
	}
}

// listSkillsPlatform loads skills with three-tier merge: bundled → org → team.
// Team skills override org skills of the same name; org skills override bundled.
func listSkillsPlatform(svc *store.Services, scope string) ([]SkillListItem, error) {
	switch scope {
	case "team":
		return listSkillsFromStore(svc.TeamSkills, "team")
	case "org":
		return listSkillsOrgWithBundled(svc.Skills)
	default:
		return listSkillsMerged(svc)
	}
}

// listSkillsFromStore lists skills from a single store with given scope label.
func listSkillsFromStore(skillStore store.SkillStore, scope string) ([]SkillListItem, error) {
	if skillStore == nil {
		return []SkillListItem{}, nil
	}
	pgSkills, err := skillStore.List(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("load %s skills: %w", scope, err)
	}
	items := make([]SkillListItem, 0, len(pgSkills))
	for _, s := range pgSkills {
		item := storeSkillToListItem(&s)
		item.Scope = scope
		item.Editable = true
		items = append(items, item)
	}
	sortListItems(items)
	return items, nil
}

// listSkillsOrgWithBundled lists org skills + bundled (org overrides bundled).
func listSkillsOrgWithBundled(orgStore store.SkillStore) ([]SkillListItem, error) {
	// Load bundled
	bundled, err := skills.LoadBundledSkills()
	if err != nil {
		return nil, fmt.Errorf("load bundled skills: %w", err)
	}

	byName := make(map[string]SkillListItem, len(bundled))
	for _, s := range bundled {
		byName[s.Name] = skillToListItem(s.Name, s.Description, "bundled", "bundled",
			s.IsEligible(), s.MissingRequirements(), s.RequireBins, s.RequireEnv, s.OS, "", false, false)
	}

	// Org customs override bundled
	if orgStore != nil {
		pgSkills, err := orgStore.List(context.TODO())
		if err != nil {
			return nil, fmt.Errorf("load org skills: %w", err)
		}
		for _, s := range pgSkills {
			item := storeSkillToListItem(&s)
			item.Scope = "org"
			item.Editable = true
			byName[s.Name] = item
		}
	}

	items := make([]SkillListItem, 0, len(byName))
	for _, item := range byName {
		items = append(items, item)
	}
	sortListItems(items)
	return items, nil
}

// listSkillsMerged returns all skills merged: bundled → org → team (team wins).
func listSkillsMerged(svc *store.Services) ([]SkillListItem, error) {
	// Load bundled
	bundled, err := skills.LoadBundledSkills()
	if err != nil {
		return nil, fmt.Errorf("load bundled skills: %w", err)
	}

	byName := make(map[string]SkillListItem, len(bundled))
	for _, s := range bundled {
		byName[s.Name] = skillToListItem(s.Name, s.Description, "bundled", "bundled",
			s.IsEligible(), s.MissingRequirements(), s.RequireBins, s.RequireEnv, s.OS, "", false, false)
	}

	// Org customs override bundled
	if svc.Skills != nil {
		pgSkills, err := svc.Skills.List(context.TODO())
		if err != nil {
			slog.Warn("failed to load org skills", "error", err)
		} else {
			for _, s := range pgSkills {
				item := storeSkillToListItem(&s)
				item.Scope = "org"
				item.Editable = true
				byName[s.Name] = item
			}
		}
	}

	// Team customs override org and bundled
	if svc.TeamSkills != nil {
		teamSkills, err := svc.TeamSkills.List(context.TODO())
		if err != nil {
			slog.Warn("failed to load team skills", "error", err)
		} else {
			for _, s := range teamSkills {
				item := storeSkillToListItem(&s)
				item.Scope = "team"
				item.Editable = true
				byName[s.Name] = item
			}
		}
	}

	items := make([]SkillListItem, 0, len(byName))
	for _, item := range byName {
		items = append(items, item)
	}
	sortListItems(items)
	return items, nil
}

// getSkillContentPlatform retrieves a skill's content with scope-aware resolution.
func getSkillContentPlatform(w http.ResponseWriter, r *http.Request, svc *store.Services, name, scope string) {
	switch scope {
	case "team":
		if svc.TeamSkills == nil {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found in team", name))
			return
		}
		skill, err := svc.TeamSkills.Get(r.Context(), name)
		if err != nil || skill == nil {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found in team", name))
			return
		}
		respondJSON(w, http.StatusOK, SkillContentResponse{
			Name:        skill.Name,
			Description: skill.Description,
			Source:      "custom",
			Scope:       "team",
			Content:     extractBody(skill.Content),
			RawFile:     skill.Content,
			Editable:    true,
		})

	case "org":
		if svc.Skills == nil {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found in org", name))
			return
		}
		skill, err := svc.Skills.Get(r.Context(), name)
		if err != nil || skill == nil {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found in org", name))
			return
		}
		respondJSON(w, http.StatusOK, SkillContentResponse{
			Name:        skill.Name,
			Description: skill.Description,
			Source:      "custom",
			Scope:       "org",
			Content:     extractBody(skill.Content),
			RawFile:     skill.Content,
			Editable:    !isPlatformMode(r) || CanManageOrg(GetPlatformUser(r)),
		})

	default:
		// No scope: try team → org → bundled
		if svc.TeamSkills != nil {
			if skill, err := svc.TeamSkills.Get(r.Context(), name); err == nil && skill != nil {
				respondJSON(w, http.StatusOK, SkillContentResponse{
					Name:        skill.Name,
					Description: skill.Description,
					Source:      "custom",
					Scope:       "team",
					Content:     extractBody(skill.Content),
					RawFile:     skill.Content,
					Editable:    IsTeamAdmin(r),
				})
				return
			}
		}

		if svc.Skills != nil {
			if skill, err := svc.Skills.Get(r.Context(), name); err == nil && skill != nil {
				respondJSON(w, http.StatusOK, SkillContentResponse{
					Name:        skill.Name,
					Description: skill.Description,
					Source:      "custom",
					Scope:       "org",
					Content:     extractBody(skill.Content),
					RawFile:     skill.Content,
					Editable:    !isPlatformMode(r) || CanManageOrg(GetPlatformUser(r)),
				})
				return
			}
		}

		// Fall back to bundled
		bundled, loadErr := skills.LoadBundledSkills()
		if loadErr != nil {
			respondError(w, http.StatusInternalServerError, "Failed to load bundled skills: "+loadErr.Error())
			return
		}
		for _, s := range bundled {
			if strings.EqualFold(s.Name, name) {
				rawFile := reconstructRawFileFromSkillPkg(&s)
				respondJSON(w, http.StatusOK, SkillContentResponse{
					Name:        s.Name,
					Description: s.Description,
					Source:      "bundled",
					Scope:       "bundled",
					Content:     s.Content,
					RawFile:     rawFile,
					Editable:    false,
				})
				return
			}
		}

		respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found", name))
	}
}

// updateSkillContentPlatform updates a skill in the given store.
func updateSkillContentPlatform(w http.ResponseWriter, r *http.Request, targetStore store.SkillStore, name string, parsed *skills.Skill, rawFile, scope string) {
	// If editing a bundled skill name (and scope is org), this creates an org override
	// If editing a skill that exists in org (and scope is team), this creates a team override
	// Both cases are valid — the store upserts by name.

	userID := ""
	if pu := GetPlatformUser(r); pu != nil {
		userID = pu.ID
	}

	skill := &store.Skill{
		Name:      parsed.Name,
		Content:   rawFile,
		CreatedBy: userID,
	}
	if err := targetStore.Save(r.Context(), skill); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save skill: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// createSkillPlatform creates a new skill in the given store.
func createSkillPlatform(w http.ResponseWriter, r *http.Request, targetStore store.SkillStore, name string) {
	// Check if already exists in this store
	existing, _ := targetStore.Get(r.Context(), name)
	if existing != nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("Skill %q already exists", name))
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

	if err := targetStore.Save(r.Context(), skill); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create skill: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteSkillPlatform deletes a skill from the given store.
func deleteSkillPlatform(w http.ResponseWriter, targetStore store.SkillStore, name, scope string) {
	// Verify it exists in this store
	_, err := targetStore.Get(context.TODO(), name)
	if err != nil {
		// Check if it's a bundled skill
		bundled, _ := skills.LoadBundledSkills()
		for _, s := range bundled {
			if strings.EqualFold(s.Name, name) {
				respondError(w, http.StatusForbidden, "Cannot delete bundled skills")
				return
			}
		}
		respondError(w, http.StatusNotFound, fmt.Sprintf("Skill %q not found in %s store", name, scope))
		return
	}

	if err := targetStore.Delete(context.TODO(), name); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete skill: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
			Editable:     s.Source != "bundled",
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

func skillToListItem(name, description, source, scope string, eligible bool, missing, requireBins, requireEnv, osNames []string, filePath string, hasDirectory, editable bool) SkillListItem {
	item := SkillListItem{
		Name:         name,
		Description:  description,
		Source:       source,
		Scope:        scope,
		Eligible:     eligible,
		Editable:     editable,
		RequireBins:  requireBins,
		RequireEnv:   requireEnv,
		OS:           osNames,
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
			Editable:    true,
		}
	}
	return SkillListItem{
		Name:        parsed.Name,
		Description: parsed.Description,
		Source:      "custom",
		Eligible:    parsed.IsEligible(),
		Editable:    true,
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
