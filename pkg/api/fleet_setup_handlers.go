package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
)

func setupProfileSource(key string) string {
	if fleet.IsBundledSetupProfileKey(key) {
		return "bundled"
	}
	return "custom"
}

// ListFleetSetupProfilesHandler handles GET /api/fleet-setup-profiles.
func ListFleetSetupProfilesHandler(w http.ResponseWriter, r *http.Request) {
	profileStore := getSetupProfileStore(store.FromRequest(r))
	summaries := profileStore.ListProfiles(r.Context())
	out := make([]fleet.SetupProfileSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, fleet.SetupProfileSummary{
			Key: s.Key, Name: s.Name, Description: s.Description,
			Domain: s.Domain, StepCount: s.StepCount, Source: s.Source,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"profiles": out})
}

// GetFleetSetupProfileHandler handles GET /api/fleet-setup-profiles/{key}.
func GetFleetSetupProfileHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	profile, err := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(store.FromRequest(r)))
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"key": key, "profile": profile, "source": setupProfileSource(key)})
}

// SaveFleetSetupProfileHandler handles PUT /api/fleet-setup-profiles/{key}.
func SaveFleetSetupProfileHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if fleet.IsBundledSetupProfileKey(key) {
		respondError(w, http.StatusConflict, "Bundled setup profiles are immutable; clone to a new key to customize")
		return
	}

	var profile fleet.SetupProfile
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	profile.Key = key
	if err := profile.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, "Validation error: "+err.Error())
		return
	}

	profileStore := getSetupProfileStore(store.FromRequest(r))
	if err := profileStore.Save(r.Context(), key, &profile); err != nil {
		if errors.Is(err, store.ErrBundledSetupProfileImmutable) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to save setup profile: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "key": key, "source": "custom"})
}

// DeleteFleetSetupProfileHandler handles DELETE /api/fleet-setup-profiles/{key}.
func DeleteFleetSetupProfileHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if fleet.IsBundledSetupProfileKey(key) {
		respondError(w, http.StatusConflict, "Bundled setup profiles cannot be deleted")
		return
	}
	profileStore := getSetupProfileStore(store.FromRequest(r))
	if err := profileStore.Delete(r.Context(), key); err != nil {
		if errors.Is(err, store.ErrBundledSetupProfileImmutable) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to delete setup profile: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

type cloneSetupProfileRequest struct {
	NewKey string `json:"new_key"`
	Name   string `json:"name,omitempty"`
}

// CloneFleetSetupProfileHandler handles POST /api/fleet-setup-profiles/{key}/clone.
func CloneFleetSetupProfileHandler(w http.ResponseWriter, r *http.Request) {
	fromKey := mux.Vars(r)["key"]
	var req cloneSetupProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	newKey := strings.TrimSpace(req.NewKey)
	if newKey == "" {
		respondError(w, http.StatusBadRequest, "new_key is required")
		return
	}
	if fleet.IsBundledSetupProfileKey(newKey) {
		respondError(w, http.StatusConflict, "Cannot clone onto a bundled setup profile key; choose a different new_key")
		return
	}

	profileStore := getSetupProfileStore(store.FromRequest(r))
	srcAny, ok := profileStore.GetProfile(r.Context(), fromKey)
	if !ok {
		respondError(w, http.StatusNotFound, "Source setup profile not found")
		return
	}
	src, err := normalizeSetupProfile(srcAny)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read source profile: "+err.Error())
		return
	}
	if _, ok := profileStore.GetProfile(r.Context(), newKey); ok {
		respondError(w, http.StatusConflict, "A setup profile with key "+newKey+" already exists")
		return
	}

	clone, err := fleet.CloneSetupProfile(src, newKey, req.Name)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := profileStore.Save(r.Context(), newKey, clone); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save cloned profile: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "key": newKey, "source": "custom"})
}

// GetFleetSetupProfileYAMLHandler handles GET /api/fleet-setup-profiles/{key}/yaml.
func GetFleetSetupProfileYAMLHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	profile, err := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(store.FromRequest(r)))
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	data, err := fleet.SetupProfileToYAML(profile)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// SaveFleetSetupProfileYAMLHandler handles PUT /api/fleet-setup-profiles/{key}/yaml.
func SaveFleetSetupProfileYAMLHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if fleet.IsBundledSetupProfileKey(key) {
		respondError(w, http.StatusConflict, "Bundled setup profiles are immutable; clone to a new key to customize")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Failed to read body: "+err.Error())
		return
	}
	profile, err := fleet.ParseSetupProfileYAML(body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid YAML: "+err.Error())
		return
	}
	profile.Key = key
	if err := profile.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, "Validation error: "+err.Error())
		return
	}
	profileStore := getSetupProfileStore(store.FromRequest(r))
	if err := profileStore.Save(r.Context(), key, profile); err != nil {
		if errors.Is(err, store.ErrBundledSetupProfileImmutable) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to save setup profile: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "key": key, "source": "custom"})
}

type createSetupDraftRequest struct {
	TemplateKey string `json:"template_key"`
}

// CreateFleetSetupDraftHandler handles POST /api/fleet-setup/drafts.
func CreateFleetSetupDraftHandler(w http.ResponseWriter, r *http.Request) {
	var req createSetupDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	if req.TemplateKey == "" {
		respondError(w, http.StatusBadRequest, "template_key is required")
		return
	}

	svc := store.FromRequest(r)
	templateStore := svc.FleetTemplates
	if templateStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet templates not available")
		return
	}
	cfgAny, ok := templateStore.GetFleet(r.Context(), req.TemplateKey)
	if !ok {
		respondError(w, http.StatusNotFound, "Template not found")
		return
	}
	cfg, ok := cfgAny.(*fleet.FleetConfig)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Invalid template type")
		return
	}

	profileKey := fleet.ProfileForTemplate(cfg)
	draft := &store.FleetSetupDraft{
		ID:              uuid.NewString(),
		TemplateKey:     req.TemplateKey,
		SetupProfileKey: profileKey,
		Collected:       map[string]any{},
	}
	if err := getSetupDraftStore(svc).Create(r.Context(), draft); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create draft: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"draft": draft})
}

// GetFleetSetupDraftHandler handles GET /api/fleet-setup/drafts/{id}.
func GetFleetSetupDraftHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	draft, ok := getSetupDraftStore(store.FromRequest(r)).Get(r.Context(), id)
	if !ok {
		respondError(w, http.StatusNotFound, "Draft not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"draft": draft})
}

type patchSetupDraftRequest struct {
	Collected   map[string]any `json:"collected"`
	CurrentStep string         `json:"current_step"`
}

// PatchFleetSetupDraftHandler handles PATCH /api/fleet-setup/drafts/{id}.
func PatchFleetSetupDraftHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	svc := store.FromRequest(r)
	draftStore := getSetupDraftStore(svc)
	draft, ok := draftStore.Get(r.Context(), id)
	if !ok {
		respondError(w, http.StatusNotFound, "Draft not found")
		return
	}
	var req patchSetupDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	if req.Collected != nil {
		draft.Collected = req.Collected
	}
	if req.CurrentStep != "" {
		draft.CurrentStep = req.CurrentStep
	}
	if err := draftStore.Update(r.Context(), draft); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update draft: "+err.Error())
		return
	}

	profile, pErr := fleet.ResolveSetupProfile(r.Context(), draft.SetupProfileKey, getSetupProfileStore(svc))
	engine := fleet.NewSetupEngine(func(key string) (*fleet.SetupProfile, bool) {
		p, e := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(svc))
		return p, e == nil
	})
	collected := fleet.ParseSetupCollected(draft.Collected)
	resp := map[string]any{"draft": draft, "valid": true}
	if pErr == nil && profile != nil && req.CurrentStep != "" {
		if valErr := engine.ValidateStep(profile, req.CurrentStep, collected); valErr != nil {
			resp["valid"] = false
			resp["errors"] = []string{valErr.Error()}
		} else {
			resp["next_step"] = engine.NextIncompleteStep(profile, collected)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type validateSetupStepRequest struct {
	StepID string `json:"step_id"`
}

// ValidateFleetSetupStepHandler handles POST /api/fleet-setup/drafts/{id}/validate-step.
func ValidateFleetSetupStepHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	svc := store.FromRequest(r)
	draftStore := getSetupDraftStore(svc)
	draft, ok := draftStore.Get(r.Context(), id)
	if !ok {
		respondError(w, http.StatusNotFound, "Draft not found")
		return
	}
	var req validateSetupStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	profile, err := fleet.ResolveSetupProfile(r.Context(), draft.SetupProfileKey, getSetupProfileStore(svc))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	engine := fleet.NewSetupEngine(func(key string) (*fleet.SetupProfile, bool) {
		p, e := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(svc))
		return p, e == nil
	})
	collected := fleet.ParseSetupCollected(draft.Collected)
	if err := engine.ValidateStep(profile, req.StepID, collected); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

type finalizeSetupDraftRequest struct {
	ValidationPassed bool `json:"validation_passed"`
}

// FinalizeFleetSetupDraftHandler handles POST /api/fleet-setup/drafts/{id}/finalize.
func FinalizeFleetSetupDraftHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	svc := store.FromRequest(r)
	draftStore := getSetupDraftStore(svc)
	draft, ok := draftStore.Get(r.Context(), id)
	if !ok {
		respondError(w, http.StatusNotFound, "Draft not found")
		return
	}
	var req finalizeSetupDraftRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	profile, err := fleet.ResolveSetupProfile(r.Context(), draft.SetupProfileKey, getSetupProfileStore(svc))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	engine := fleet.NewSetupEngine(func(key string) (*fleet.SetupProfile, bool) {
		p, e := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(svc))
		return p, e == nil
	})
	collected := fleet.ParseSetupCollected(draft.Collected)
	if next := engine.NextIncompleteStep(profile, collected); next != "" {
		respondError(w, http.StatusBadRequest, "Incomplete step: "+next)
		return
	}
	build, err := engine.BuildPlanArgs(profile, draft.TemplateKey, collected)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	templateStore := svc.FleetTemplates
	planStore := svc.FleetPlans
	if templateStore == nil || planStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet stores not available")
		return
	}
	cfgAny, ok := templateStore.GetFleet(r.Context(), draft.TemplateKey)
	if !ok {
		respondError(w, http.StatusNotFound, "Template not found")
		return
	}
	baseCfg, ok := cfgAny.(*fleet.FleetConfig)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Invalid template type")
		return
	}

	createdBy := ""
	if user := GetPlatformUser(r); user != nil {
		createdBy = user.ID
	}
	plan, err := fleet.BuildFleetPlanFromSetup(baseCfg, build, fleet.PlanFromSetupBuildOptions{
		ValidationPassed: req.ValidationPassed,
		CreatedBy:        createdBy,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := planStore.Save(r.Context(), plan); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save plan: "+err.Error())
		return
	}
	_ = draftStore.Delete(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "saved", "key": plan.Key})
}

// ensureSetupDraft returns an existing draft ID or creates a new draft for the template.
func ensureSetupDraft(r *http.Request, templateKey, draftID string) (string, error) {
	draftID = strings.TrimSpace(draftID)
	draftStore := getSetupDraftStore(store.FromRequest(r))
	if draftStore == nil {
		return "", fmt.Errorf("setup draft store not available")
	}
	if draftID != "" {
		if _, ok := draftStore.Get(r.Context(), draftID); ok {
			return draftID, nil
		}
	}
	templateKey = strings.TrimSpace(templateKey)
	if templateKey == "" {
		return "", fmt.Errorf("template key is required")
	}
	svc := store.FromRequest(r)
	templateStore := svc.FleetTemplates
	if templateStore == nil {
		if reg := GetFleetRegistry(); reg != nil {
			if cfg, ok := reg.GetFleet(templateKey); ok {
				profileKey := fleet.ProfileForTemplate(cfg)
				draft := &store.FleetSetupDraft{
					ID:              uuid.NewString(),
					TemplateKey:     templateKey,
					SetupProfileKey: profileKey,
					Collected:       map[string]any{},
				}
				if err := draftStore.Create(r.Context(), draft); err != nil {
					return "", err
				}
				return draft.ID, nil
			}
		}
		return "", fmt.Errorf("template not found")
	}
	cfgAny, ok := templateStore.GetFleet(r.Context(), templateKey)
	if !ok {
		return "", fmt.Errorf("template not found")
	}
	cfg, ok := cfgAny.(*fleet.FleetConfig)
	if !ok {
		return "", fmt.Errorf("invalid template type")
	}
	draft := &store.FleetSetupDraft{
		ID:              uuid.NewString(),
		TemplateKey:     templateKey,
		SetupProfileKey: fleet.ProfileForTemplate(cfg),
		Collected:       map[string]any{},
	}
	if err := draftStore.Create(r.Context(), draft); err != nil {
		return "", err
	}
	return draft.ID, nil
}

// ResolveSetupWizardContext builds chat wizard context from template and optional draft.
func ResolveSetupWizardContext(r *http.Request, templateKey, draftID string) (profileKey, description, systemPrompt string, pinned []string, currentStepID, currentStepTitle string, err error) {
	svc := store.FromRequest(r)
	templateStore := svc.FleetTemplates
	if templateStore == nil {
		if reg := GetFleetRegistry(); reg != nil {
			if cfg, ok := reg.GetFleet(templateKey); ok {
				return resolveWizardFromConfig(r, templateKey, cfg, draftID)
			}
		}
		return "", "", "", nil, "", "", nil
	}
	cfgAny, ok := templateStore.GetFleet(r.Context(), templateKey)
	if !ok {
		return "", "", "", nil, "", "", nil
	}
	cfg, ok := cfgAny.(*fleet.FleetConfig)
	if !ok {
		return "", "", "", nil, "", "", nil
	}
	return resolveWizardFromConfig(r, templateKey, cfg, draftID)
}

func resolveWizardFromConfig(r *http.Request, templateKey string, cfg *fleet.FleetConfig, draftID string) (profileKey, description, systemPrompt string, pinned []string, currentStepID, currentStepTitle string, err error) {
	if cfg.PlanWizard != nil && cfg.SetupProfileKey == "" {
		return "", cfg.PlanWizard.Description, cfg.PlanWizard.SystemPrompt, cfg.PlanWizard.PinnedToolGroups, "", "", nil
	}
	profileKey = fleet.ProfileForTemplate(cfg)
	profile, err := fleet.ResolveSetupProfile(r.Context(), profileKey, getSetupProfileStore(store.FromRequest(r)))
	if err != nil {
		return profileKey, "", "", nil, "", "", err
	}

	var collected fleet.SetupCollected
	draftCtx := fleet.SetupDraftContext{BaseFleetKey: templateKey}
	if cfg != nil && cfg.Agents != nil {
		for k := range cfg.Agents {
			draftCtx.TemplateAgentNames = append(draftCtx.TemplateAgentNames, k)
		}
	}
	if draftID != "" {
		if draft, ok := getSetupDraftStore(store.FromRequest(r)).Get(r.Context(), draftID); ok {
			collected = fleet.ParseSetupCollected(draft.Collected)
		}
	}
	wctx := fleet.BuildSetupWizardContext(profile, collected, draftCtx)
	return profileKey, wctx.Description, wctx.SystemPrompt, wctx.PinnedToolGroups, wctx.CurrentStepID, wctx.CurrentStepTitle, nil
}

// GetFleetSetupToolCatalogHandler handles GET /api/fleet-setup/tool-catalog.
func GetFleetSetupToolCatalogHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"tools": fleet.SetupToolCatalog()})
}

// GetFleetSetupProfileStepHandler handles GET /api/fleet-setup-profiles/{key}/steps/{stepId}.
func GetFleetSetupProfileStepHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	stepID := mux.Vars(r)["stepId"]
	profile, err := fleet.ResolveSetupProfile(r.Context(), key, getSetupProfileStore(store.FromRequest(r)))
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	step, ok := profile.StepByID(stepID)
	if !ok {
		respondError(w, http.StatusNotFound, "Step not found")
		return
	}
	collected := fleet.SetupCollected{}
	tools := fleet.StepTools(step, collected)
	groups := fleet.StepToolGroups(profile, step, collected)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"step":                step,
		"tools":               tools,
		"pinned_tool_groups":  groups,
		"resolved_tool_refs":  fleet.SetupToolCatalog(),
	})
}
