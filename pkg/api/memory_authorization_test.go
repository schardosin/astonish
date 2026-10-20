package api

import (
	"context"
	"encoding/json"
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
