package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/pkg/store"
)

type consolidationSpyStore struct {
	deletingMemoryStore
	writes int
}

func (s *consolidationSpyStore) Add(context.Context, store.MemoryEntry) error { s.writes++; return nil }
func (s *consolidationSpyStore) Update(context.Context, string, string, string) error {
	s.writes++
	return nil
}

type consolidationFailureStore struct {
	deletingMemoryStore
	failDeleteID      string
	deleteCalls       []string
	listErr, writeErr error
	listCalls         int
	failFirstList     bool
	writes            int
}

const consolidationStorageCanary = "private-storage-canary postgres://secret@internal/database"

func (s *consolidationFailureStore) Delete(ctx context.Context, id string) error {
	s.deleteCalls = append(s.deleteCalls, id)
	if id == s.failDeleteID {
		return errors.New(consolidationStorageCanary)
	}
	return s.deletingMemoryStore.Delete(ctx, id)
}

func (s *consolidationFailureStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	s.listCalls++
	if s.listErr != nil && (!s.failFirstList || s.listCalls == 1) {
		return nil, s.listErr
	}
	return s.deletingMemoryStore.List(ctx, category, limit, offset)
}

func (s *consolidationFailureStore) Add(_ context.Context, entry store.MemoryEntry) error {
	s.writes++
	s.entries["saved-destination"] = store.MemorySearchResult{ID: "saved-destination", Category: entry.Category, Snippet: entry.Content, CreatedBy: "actor"}
	return s.writeErr
}

func (s *consolidationFailureStore) Update(_ context.Context, id, content, category string) error {
	s.writes++
	entry := s.entries[id]
	entry.Snippet, entry.Category = content, category
	s.entries[id] = entry
	return s.writeErr
}

type consolidationPreviewRouter struct {
	store.TenantRouter
	org store.OrgDataStore
}

func (r consolidationPreviewRouter) ForOrg(string) (store.OrgDataStore, error) { return r.org, nil }

type consolidationPreviewOrg struct{ store.OrgDataStore }

func (consolidationPreviewOrg) ForUser(string) store.PersonalDataStore {
	return consolidationPreviewPersonal{}
}
func (consolidationPreviewOrg) OrgMemories() store.MemoryStore { return nil }

type consolidationPreviewPersonal struct{ store.PersonalDataStore }

func (consolidationPreviewPersonal) Memories() store.MemoryStore { return nil }

func consolidationFailureRequest(t *testing.T, s store.MemoryStore, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(data)))
	svc := &store.Services{Mode: store.ModePlatform, Memory: s}
	if strings.HasSuffix(path, "/preview") {
		svc.TenantRouter = consolidationPreviewRouter{org: consolidationPreviewOrg{}}
	}
	req = req.WithContext(store.WithServices(WithPlatformUser(req.Context(), &PlatformUser{ID: "actor", Role: "member"}), svc))
	w := httptest.NewRecorder()
	if strings.HasSuffix(path, "/preview") {
		MemoryConsolidationPreviewHandler(w, req)
	} else {
		MemoryConsolidationApplyHandler(w, req)
	}
	return w
}

func assertConsolidationFailure(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), consolidationStorageCanary) {
		t.Error("response leaked storage error")
	}
	var body struct {
		Applied bool   `json:"applied"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Applied || body.Error == "" {
		t.Errorf("expected failure, got %s", w.Body.String())
	}
}

func seedConsolidationHealthCache(t *testing.T) {
	t.Helper()
	invalidateMemoryHealthCache()
	memoryHealthCache.Lock()
	memoryHealthCache.entries["failure-test"] = memoryHealthCacheEntry{}
	memoryHealthCache.Unlock()
	t.Cleanup(invalidateMemoryHealthCache)
}

func TestConsolidationDeletionFailureStopsCleanup(t *testing.T) {
	for _, mode := range []string{"source", "duplicate", "discard"} {
		for _, failureIndex := range []int{0, 1} {
			for _, destination := range []string{"new", "existing"} {
				t.Run(fmt.Sprintf("%s/%d/%s", mode, failureIndex, destination), func(t *testing.T) {
					card := memory.ScenarioCard{CanonicalKey: "proxmox-console-access", Title: "Proxmox Console Access", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}, SourceMemoryIDs: []string{"source-0", "source-1", "source-2"}}
					s := &consolidationFailureStore{deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{}}}
					duplicates := []string{"duplicate-0", "duplicate-1", "duplicate-2"}
					for _, id := range card.SourceMemoryIDs {
						s.entries[id] = store.MemorySearchResult{ID: id, CreatedBy: "actor", Snippet: "raw memory"}
					}
					for _, id := range duplicates {
						s.entries[id] = store.MemorySearchResult{ID: id, CreatedBy: "actor", Category: memory.ScenarioCardCategory, Snippet: "unparsed duplicate card"}
					}
					destinationID := "saved-destination"
					if destination == "existing" {
						destinationID = "existing-destination"
						s.entries[destinationID] = store.MemorySearchResult{ID: destinationID, CreatedBy: "actor", Category: memory.ScenarioCardCategory, Snippet: memory.RenderScenarioCard(card)}
					}
					failedGroup := card.SourceMemoryIDs
					var expectedCalls []string
					if mode == "duplicate" {
						failedGroup = duplicates
						expectedCalls = append(expectedCalls, card.SourceMemoryIDs...)
					}
					s.failDeleteID = failedGroup[failureIndex]
					expectedCalls = append(expectedCalls, failedGroup[:failureIndex+1]...)
					if mode == "discard" {
						card.RecommendedRecipe = []string{memory.ScenarioCardPlaceholderRecipe}
					}
					seedConsolidationHealthCache(t)
					w := consolidationFailureRequest(t, s, "/api/memories/consolidate/apply", MemoryConsolidationApplyRequest{Card: card, TargetScope: "team", DuplicateCardIDs: duplicates})
					assertConsolidationFailure(t, w)
					if fmt.Sprint(s.deleteCalls) != fmt.Sprint(expectedCalls) {
						t.Errorf("deletes=%v, want %v", s.deleteCalls, expectedCalls)
					}
					if _, ok := s.entries[s.failDeleteID]; !ok {
						t.Error("failed deletion removed row")
					}
					if _, ok := s.entries[failedGroup[failureIndex+1]]; !ok {
						t.Error("cleanup continued after failure")
					}
					for _, id := range expectedCalls[:len(expectedCalls)-1] {
						if _, ok := s.entries[id]; ok {
							t.Errorf("successful deletion %s unexpectedly rolled back", id)
						}
					}
					if mode != "discard" {
						if s.writes != 1 {
							t.Errorf("writes=%d", s.writes)
						}
						if entry, ok := s.entries[destinationID]; !ok || !strings.Contains(entry.Snippet, "source-0") {
							t.Error("durable destination not preserved")
						}
					} else if s.writes != 0 {
						t.Error("discard wrote a destination")
					}
					if memoryHealthCacheSize() != 0 {
						t.Error("cache not invalidated")
					}
				})
			}
		}
	}
}

func TestConsolidationTransientPlanningFailureIsClosed(t *testing.T) {
	s := &consolidationFailureStore{
		deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{
			"source": {ID: "source", CreatedBy: "actor", Snippet: "raw memory"},
		}},
		listErr: errors.New(consolidationStorageCanary), failFirstList: true,
	}
	card := memory.ScenarioCard{CanonicalKey: "console-access", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}, SourceMemoryIDs: []string{"source"}}
	w := consolidationFailureRequest(t, s, "/api/memories/consolidate/apply", MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"})
	assertConsolidationFailure(t, w)
	if s.listCalls != 1 {
		t.Errorf("planning retried a failed lookup: calls=%d", s.listCalls)
	}
	if s.writes != 0 || len(s.deleteCalls) != 0 {
		t.Fatalf("planning error caused persistence: writes=%d deletes=%v", s.writes, s.deleteCalls)
	}
}

func TestConsolidationStorageErrorsAreBounded(t *testing.T) {
	for _, operation := range []string{"preview", "planning", "add", "update"} {
		t.Run(operation, func(t *testing.T) {
			s := &consolidationFailureStore{deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{"source": {ID: "source", CreatedBy: "actor", Snippet: "raw memory"}}}}
			card := memory.ScenarioCard{CanonicalKey: "proxmox-console-access", Title: "Proxmox Console Access", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}, SourceMemoryIDs: []string{"source"}}
			path := "/api/memories/consolidate/apply"
			var body any = MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"}
			if operation == "preview" || operation == "planning" {
				s.listErr = errors.New(consolidationStorageCanary)
			} else {
				s.writeErr = errors.New(consolidationStorageCanary)
			}
			if operation == "preview" {
				path = "/api/memories/consolidate/preview"
				body = MemoryConsolidationRequest{TargetScope: "team"}
			}
			if operation == "update" {
				s.entries["destination"] = store.MemorySearchResult{ID: "destination", CreatedBy: "actor", Category: memory.ScenarioCardCategory, Snippet: memory.RenderScenarioCard(card)}
			}
			seedConsolidationHealthCache(t)
			w := consolidationFailureRequest(t, s, path, body)
			assertConsolidationFailure(t, w)
			if len(s.deleteCalls) != 0 {
				t.Errorf("deleted after planning/persistence failure: %v", s.deleteCalls)
			}
			if operation == "add" || operation == "update" {
				if memoryHealthCacheSize() != 0 {
					t.Error("potentially mutated store left stale cache")
				}
			} else if s.writes != 0 {
				t.Error("planning failure wrote destination")
			}
		})
	}
}

type consolidationScopedOrg struct {
	store.OrgDataStore
	personal, org store.MemoryStore
}

func (s consolidationScopedOrg) ForUser(string) store.PersonalDataStore {
	return consolidationScopedPersonal{memories: s.personal}
}
func (s consolidationScopedOrg) OrgMemories() store.MemoryStore { return s.org }

type consolidationScopedPersonal struct {
	store.PersonalDataStore
	memories store.MemoryStore
}

func (s consolidationScopedPersonal) Memories() store.MemoryStore { return s.memories }

func TestConsolidationPreservesOnlyDestinationScope(t *testing.T) {
	for _, participant := range []string{"source", "duplicate"} {
		for _, scope := range []store.MemoryScope{store.MemoryScopePersonal, store.MemoryScopeOrg} {
			for _, failDelete := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/failure=%t", participant, scope, failDelete), func(t *testing.T) {
					card := memory.ScenarioCard{CanonicalKey: "proxmox-console-access", Title: "Proxmox Console Access", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}
					entry := store.MemorySearchResult{ID: "shared-id", CreatedBy: "actor", Category: memory.ScenarioCardCategory, Snippet: memory.RenderScenarioCard(card)}
					target := &consolidationFailureStore{deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{entry.ID: entry}}}
					body := MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"}
					if participant == "source" {
						body.Card.SourceMemoryIDs = []string{entry.ID}
						entry.Category, entry.Snippet = "", "raw memory"
					} else {
						body.DuplicateCardIDs = []string{entry.ID}
					}
					other := &consolidationFailureStore{deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{entry.ID: entry}}}
					if failDelete {
						other.failDeleteID = entry.ID
					}
					org := consolidationScopedOrg{}
					if scope == store.MemoryScopePersonal {
						org.personal = other
					} else {
						org.org = other
					}
					data, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					req := httptest.NewRequest(http.MethodPost, "/api/memories/consolidate/apply", strings.NewReader(string(data)))
					svc := &store.Services{Mode: store.ModePlatform, Memory: target, TenantRouter: consolidationPreviewRouter{org: org}}
					req = req.WithContext(store.WithServices(WithPlatformUser(req.Context(), &PlatformUser{ID: "actor", Role: "member"}), svc))
					seedConsolidationHealthCache(t)
					w := httptest.NewRecorder()
					MemoryConsolidationApplyHandler(w, req)
					if failDelete {
						assertConsolidationFailure(t, w)
						if !strings.Contains(w.Body.String(), "scenario card saved") {
							t.Errorf("missing partial-persistence warning: %s", w.Body.String())
						}
					} else {
						var result MemoryConsolidationApplyResponse
						if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						if w.Code != http.StatusOK || !result.Applied || result.ExistingID != entry.ID || result.DeletedSources+result.DeletedDuplicateCards != 1 {
							t.Errorf("expected one scoped participant deleted: status=%d body=%s", w.Code, w.Body.String())
						}
					}
					if len(other.deleteCalls) != 1 || other.deleteCalls[0] != entry.ID {
						t.Errorf("other-scope delete calls=%v, want [%s]", other.deleteCalls, entry.ID)
					}
					if _, remains := other.entries[entry.ID]; remains != failDelete {
						t.Errorf("other-scope row remains=%t, failure=%t", remains, failDelete)
					}
					if target.writes != 1 || len(target.deleteCalls) != 0 || target.entries[entry.ID].ID != entry.ID {
						t.Errorf("saved destination not preserved: writes=%d deletes=%v", target.writes, target.deleteCalls)
					}
					if memoryHealthCacheSize() != 0 {
						t.Error("cache not invalidated")
					}
				})
			}
		}
	}
}

func TestConsolidationPreauthorizesAllParticipants(t *testing.T) {
	for _, participant := range []string{"source", "duplicate", "discard", "destination", "upsert-destination"} {
		for _, authorized := range []bool{false, true} {
			t.Run(participant+map[bool]string{true: "/owner", false: "/other"}[authorized], func(t *testing.T) {
				owner := "other"
				if authorized {
					owner = "actor"
				}
				card := memory.ScenarioCard{CanonicalKey: "proxmox-console-access", Title: "Proxmox Console Access", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}
				entry := store.MemorySearchResult{ID: "participant", CreatedBy: owner, Scope: "team", Snippet: "raw memory"}
				switch participant {
				case "source":
					card.SourceMemoryIDs = []string{entry.ID}
				case "discard":
					card.SourceMemoryIDs = []string{entry.ID}
					card.RecommendedRecipe = []string{memory.ScenarioCardPlaceholderRecipe}
				default:
					entry.Category = memory.ScenarioCardCategory
					entry.Snippet = memory.RenderScenarioCard(card)
				}
				if participant == "duplicate" {
					otherCard := memory.ScenarioCard{CanonicalKey: "postgres-backup", Title: "Postgres Backup", RecommendedRecipe: []string{"Run pg_dump for the production database."}}
					entry.Snippet = memory.RenderScenarioCard(otherCard)
				}
				s := &consolidationSpyStore{deletingMemoryStore: deletingMemoryStore{entries: map[string]store.MemorySearchResult{entry.ID: entry}}}
				body := MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"}
				if participant == "duplicate" || participant == "upsert-destination" {
					body.DuplicateCardIDs = []string{entry.ID}
				}
				data, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/api/memories/consolidate/apply", strings.NewReader(string(data)))
				req = req.WithContext(store.WithServices(WithPlatformUser(req.Context(), &PlatformUser{ID: "actor", Role: "member"}), &store.Services{Mode: store.ModePlatform, Memory: s}))
				w := httptest.NewRecorder()
				MemoryConsolidationApplyHandler(w, req)
				if !authorized {
					if w.Code != http.StatusForbidden {
						t.Errorf("status=%d body=%s", w.Code, w.Body.String())
					}
					if s.writes != 0 || len(s.deleted) != 0 {
						t.Fatalf("unauthorized side effects: writes=%d deletes=%v", s.writes, s.deleted)
					}
				} else if w.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				} else if participant == "discard" {
					if !s.deleted[entry.ID] || s.writes != 0 {
						t.Fatal("discard did not delete only source")
					}
				} else if s.writes != 1 {
					t.Fatalf("writes=%d", s.writes)
				} else if (participant == "source" || participant == "duplicate") && !s.deleted[entry.ID] {
					t.Fatal("authorized participant not deleted")
				}
			})
		}
	}
}
