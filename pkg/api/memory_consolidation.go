package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/pkg/store"
)

type MemoryConsolidationRequest struct {
	Key         string   `json:"key"`
	TargetScope string   `json:"target_scope"`
	MemoryIDs   []string `json:"memory_ids,omitempty"`
}

type MemoryConsolidationPreviewResponse struct {
	Card    memory.ScenarioCard `json:"card"`
	Content string              `json:"content"`
	Sources []string            `json:"sources"`
}

type MemoryConsolidationApplyRequest struct {
	Card             memory.ScenarioCard `json:"card"`
	TargetScope      string              `json:"target_scope"`
	DuplicateCardIDs []string            `json:"duplicate_card_ids,omitempty"`
}

type MemoryConsolidationApplyResponse struct {
	Applied               bool                        `json:"applied"`
	Scope                 string                      `json:"scope"`
	Action                string                      `json:"action"`
	ExistingID            string                      `json:"existing_id,omitempty"`
	Card                  memory.ScenarioCard         `json:"card"`
	Result                memory.ScenarioUpsertResult `json:"result"`
	DeletedSources        int                         `json:"deleted_sources"`
	DeletedDuplicateCards int                         `json:"deleted_duplicate_cards"`
}

// MemoryConsolidationPreviewHandler drafts a structured scenario card from a
// Memory Map group without saving anything.
//
//	POST /api/memories/consolidate/preview
func MemoryConsolidationPreviewHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}
	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	var req MemoryConsolidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetScope == "" {
		req.TargetScope = "personal"
	}
	if err := validateConsolidationScope(r, pu, req.TargetScope); err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	memories, err := collectVisibleMemories(r, svc, pu, 2000)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load memories for consolidation")
		return
	}
	selected := selectConsolidationMemories(memories, req.Key, req.MemoryIDs)
	if len(selected) == 0 {
		respondError(w, http.StatusNotFound, "no memories matched the requested group")
		return
	}

	card := memory.DraftScenarioCardFromMemories(req.Key, req.TargetScope, selected)
	respondJSON(w, http.StatusOK, MemoryConsolidationPreviewResponse{
		Card:    card,
		Content: memory.RenderScenarioCard(card),
		Sources: card.SourceMemoryIDs,
	})
}

// MemoryConsolidationApplyHandler saves or merges a scenario card into the
// requested target scope. Raw source memories referenced by the card are deleted
// after a successful upsert; the card is the durable memory.
//
//	POST /api/memories/consolidate/apply
func MemoryConsolidationApplyHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}
	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	var req MemoryConsolidationApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetScope == "" {
		req.TargetScope = req.Card.Scope
	}
	if req.TargetScope == "" {
		req.TargetScope = "personal"
	}
	if err := validateConsolidationScope(r, pu, req.TargetScope); err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	if req.Card.CanonicalKey == "" || len(req.Card.RecommendedRecipe) == 0 {
		respondError(w, http.StatusBadRequest, "card canonical_key and recommended_recipe are required")
		return
	}
	sources, duplicates, err := authorizeConsolidationParticipants(r, svc, pu, req.Card.SourceMemoryIDs, req.DuplicateCardIDs)
	if err != nil {
		respondError(w, http.StatusForbidden, "not authorized to manage consolidation participants")
		return
	}
	if !memory.HasUsableScenarioRecipe(req.Card) {
		deletedSources, err := deleteAuthorizedConsolidationMemories(r.Context(), sources, consolidationMemory{})
		invalidateMemoryHealthCache()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to discard source memories; some deletions may have persisted")
			return
		}
		respondJSON(w, http.StatusOK, MemoryConsolidationApplyResponse{
			Applied:        true,
			Scope:          req.TargetScope,
			Action:         "discarded",
			Card:           req.Card,
			DeletedSources: deletedSources,
		})
		return
	}

	memStore, err := resolveMemoryStoreForScope(r, svc, pu, req.TargetScope)
	if err != nil || memStore == nil {
		respondError(w, http.StatusBadRequest, "target memory store unavailable")
		return
	}
	req.Card.Scope = req.TargetScope
	// Record the identity resolver's selected write without embedding or persisting.
	planned := &consolidationWritePlan{MemoryStore: memStore}
	preservedID := preferredPreservedScenarioCardID(r.Context(), planned, req.DuplicateCardIDs, req.Card)
	if planned.readErr != nil {
		respondError(w, http.StatusInternalServerError, "failed to load scenario cards for consolidation")
		return
	}
	var result memory.ScenarioUpsertResult
	if preservedID != "" {
		if err := planned.Update(r.Context(), preservedID, memory.RenderScenarioCard(req.Card), memory.ScenarioCardCategory); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update scenario card")
			return
		}
		result = memory.ScenarioUpsertResult{Action: "merged", ExistingID: preservedID}
	} else {
		var err error
		result, err = memory.UpsertScenarioCard(r.Context(), planned, req.Card)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save scenario card")
			return
		}
	}
	if planned.id != "" {
		entry, lookupErr := memStore.Get(r.Context(), planned.id)
		if lookupErr != nil || entry == nil {
			respondError(w, http.StatusInternalServerError, "failed to resolve destination memory")
			return
		}
		if !canManageMemory(r, pu, entry, req.TargetScope) {
			respondError(w, http.StatusForbidden, "not authorized to manage destination memory")
			return
		}
		err = memStore.Update(r.Context(), planned.id, planned.content, planned.category)
	} else {
		err = memStore.Add(r.Context(), planned.entry)
	}
	// Even a failed write may have mutated the store; MemoryStore offers no rollback.
	defer invalidateMemoryHealthCache()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save scenario card")
		return
	}
	preserved := consolidationMemory{scope: store.MemoryScope(req.TargetScope), id: result.ExistingID}
	deletedSources, err := deleteAuthorizedConsolidationMemories(r.Context(), sources, preserved)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "scenario card saved but source cleanup failed; some deletions may have persisted")
		return
	}
	deletedDuplicateCards, err := deleteAuthorizedConsolidationMemories(r.Context(), duplicates, preserved)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "scenario card saved but duplicate cleanup failed; some deletions may have persisted")
		return
	}

	respondJSON(w, http.StatusOK, MemoryConsolidationApplyResponse{
		Applied:               true,
		Scope:                 req.TargetScope,
		Action:                result.Action,
		ExistingID:            result.ExistingID,
		Card:                  req.Card,
		Result:                result,
		DeletedSources:        deletedSources,
		DeletedDuplicateCards: deletedDuplicateCards,
	})
}

// consolidationWritePlan captures a write without invoking a store's embedder.
type consolidationWritePlan struct {
	store.MemoryStore
	id, content, category string
	entry                 store.MemoryEntry
	readErr               error
}

// Remember lookup failures even when a legacy selector treats them as no match.
func (p *consolidationWritePlan) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	rows, err := p.MemoryStore.List(ctx, category, limit, offset)
	if err != nil && p.readErr == nil {
		p.readErr = err
	}
	return rows, err
}

func (p *consolidationWritePlan) Add(_ context.Context, entry store.MemoryEntry) error {
	p.entry = entry
	return nil
}
func (p *consolidationWritePlan) Update(_ context.Context, id, content, category string) error {
	p.id, p.content, p.category = id, content, category
	return nil
}

type consolidationMemory struct {
	store store.MemoryStore
	scope store.MemoryScope
	id    string
}

func authorizeConsolidationParticipants(r *http.Request, svc *store.Services, pu *PlatformUser, sourceIDs, duplicateIDs []string) ([]consolidationMemory, []consolidationMemory, error) {
	var sources, duplicates []consolidationMemory
	for _, scope := range []store.MemoryScope{store.MemoryScopePersonal, store.MemoryScopeTeam, store.MemoryScopeOrg} {
		// Missing optional scopes are normal; routing failures are not.
		if scope != store.MemoryScopeTeam && svc.TenantRouter == nil {
			continue
		}
		if scope == store.MemoryScopeTeam && svc.Memory == nil {
			continue
		}
		ms, err := resolveMemoryStoreForScope(r, svc, pu, string(scope))
		if err != nil {
			return nil, nil, err
		}
		if ms == nil {
			continue
		}
		for _, group := range []struct {
			ids   []string
			cards bool
			out   *[]consolidationMemory
		}{{sourceIDs, false, &sources}, {duplicateIDs, true, &duplicates}} {
			seen := make(map[string]bool)
			for _, id := range group.ids {
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				entry, err := ms.Get(r.Context(), id)
				if err != nil {
					return nil, nil, err
				}
				if entry == nil {
					continue
				}
				if !canManageMemory(r, pu, entry, string(scope)) {
					return nil, nil, fmt.Errorf("memory management denied")
				}
				if memory.IsScenarioCard(*entry) == group.cards {
					*group.out = append(*group.out, consolidationMemory{store: ms, scope: scope, id: id})
				}
			}
		}
	}
	return sources, duplicates, nil
}

// deleteAuthorizedConsolidationMemories stops at the first failure. Earlier
// deletions and the saved destination remain persisted; this is not a transaction.
// Scope comes from request routing, not row metadata or store-wrapper identity.
func deleteAuthorizedConsolidationMemories(ctx context.Context, entries []consolidationMemory, preserved consolidationMemory) (int, error) {
	deleted := 0
	for _, entry := range entries {
		if entry.scope != preserved.scope || entry.id != preserved.id {
			if err := entry.store.Delete(ctx, entry.id); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}

func collectVisibleMemories(r *http.Request, svc *store.Services, pu *PlatformUser, limit int) ([]store.MemorySearchResult, error) {
	if svc.TenantRouter == nil {
		return nil, fmt.Errorf("tenant router not available")
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve org store")
	}

	var memories []store.MemorySearchResult
	if personalMem := orgStore.ForUser(pu.ID).Memories(); personalMem != nil {
		results, err := personalMem.List(r.Context(), "", limit, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to list personal memories: %w", err)
		}
		memories = append(memories, ensureMemoryScope(results, string(store.MemoryScopePersonal))...)
	}
	if svc.Memory != nil {
		results, err := svc.Memory.List(r.Context(), "", limit, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to list team memories: %w", err)
		}
		memories = append(memories, ensureMemoryScope(results, string(store.MemoryScopeTeam))...)
	}
	if orgMem := orgStore.OrgMemories(); orgMem != nil {
		results, err := orgMem.List(r.Context(), "", limit, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to list org memories: %w", err)
		}
		memories = append(memories, ensureMemoryScope(results, string(store.MemoryScopeOrg))...)
	}
	return memories, nil
}

func selectConsolidationMemories(memories []store.MemorySearchResult, key string, ids []string) []store.MemorySearchResult {
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	var selected []store.MemorySearchResult
	for _, candidate := range memories {
		if len(idSet) > 0 {
			if idSet[candidate.ID] {
				selected = append(selected, candidate)
			}
			continue
		}
		if key != "" && canonicalMemoryTopic(candidate) == key {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func validateConsolidationScope(r *http.Request, pu *PlatformUser, scope string) error {
	switch scope {
	case "personal":
		return nil
	case "team":
		return nil
	case "org":
		if !CanManageOrg(pu) {
			return fmt.Errorf("admin role required to save org scenario cards")
		}
		return nil
	default:
		return fmt.Errorf("invalid target_scope: %s", scope)
	}
}
