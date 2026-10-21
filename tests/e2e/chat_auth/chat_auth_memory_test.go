//go:build e2e

// Package chat_auth contains E2E tests for chat authorization boundaries.
// This file covers memory authorization across users, teams, and orgs.
package chat_auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/memory"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/SAP/astonish/tests/e2eboot"
	"github.com/gorilla/mux"
)

// localMemoryDeleteFailure delegates every successful operation to SQLite. The
// injected error occurs before the selected delete; it does not model rollback.
const localMemoryDeleteCanary = "sqlite-delete-storage-canary /private/platform.db secret=do-not-disclose"

type localMemoryDeleteFailure struct {
	store.MemoryStore
	failID      string
	deleteCalls []string
}

func (s *localMemoryDeleteFailure) Delete(ctx context.Context, id string) error {
	s.deleteCalls = append(s.deleteCalls, id)
	if id == s.failID {
		return errors.New(localMemoryDeleteCanary)
	}
	return s.MemoryStore.Delete(ctx, id)
}

// TestE2E_Memory_LocalDeletionFailures deliberately does not call Bootstrap:
// only temporary SQLite, normal registered routes, JWT auth and tenant routing
// are needed. No provider, sandbox, infrastructure or preflight bypass is used.
func TestE2E_Memory_LocalDeletionFailures(t *testing.T) {
	for _, mode := range []string{"source", "duplicate", "discard"} {
		for _, failureIndex := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/failure_%d", mode, failureIndex), func(t *testing.T) {
				ctx := context.Background()
				var fault *localMemoryDeleteFailure
				seed, backend, server := localMemoryHTTPFixture(t, func(r *http.Request) *http.Request {
					tc := store.TenantContextFrom(r.Context())
					if fault != nil && tc != nil && tc.OrgSlug == e2eboot.OrgAcmeSlug && tc.TeamSlug == e2eboot.TeamAcmeRed {
						requestSvc := *store.FromRequest(r)
						requestSvc.Memory = fault
						return r.WithContext(store.WithServices(r.Context(), &requestSvc))
					}
					return r
				})
				alice := seed.Users[e2eboot.UserAliceEmail]
				org, err := backend.ForOrg(alice.OrgSlug)
				if err != nil {
					t.Fatal(err)
				}
				otherOrg, err := backend.ForOrg(seed.Users[e2eboot.UserEveEmail].OrgSlug)
				if err != nil {
					t.Fatal(err)
				}
				team := org.ForTeam(alice.TeamSlug).Memories()
				fault = &localMemoryDeleteFailure{MemoryStore: team}
				snapshot := func(ms store.MemoryStore) map[string]store.MemorySearchResult {
					t.Helper()
					rows, err := ms.List(ctx, "", 1000, 0)
					if err != nil {
						t.Fatal(err)
					}
					result := make(map[string]store.MemorySearchResult, len(rows))
					for _, row := range rows {
						full, err := ms.Get(ctx, row.ID)
						if err != nil || full == nil {
							t.Fatalf("get persisted memory %s: %v", row.ID, err)
						}
						result[row.ID] = *full
					}
					return result
				}
				add := func(content, category string) string {
					t.Helper()
					if err := team.Add(ctx, store.MemoryEntry{Content: content, Category: category, CreatedBy: alice.ID}); err != nil {
						t.Fatal(err)
					}
					for id, row := range snapshot(team) {
						if row.Snippet == content {
							return id
						}
					}
					t.Fatal("seeded memory not found")
					return ""
				}
				card := memory.ScenarioCard{CanonicalKey: "local-console-recovery", Title: "Local Console Recovery", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}
				var duplicateIDs []string
				for i := 0; i < 3; i++ {
					card.SourceMemoryIDs = append(card.SourceMemoryIDs, add(fmt.Sprintf("local deletion source %d", i), "note"))
					duplicateIDs = append(duplicateIDs, add(fmt.Sprintf("unparsed duplicate card %d", i), memory.ScenarioCardCategory))
				}
				failedGroup := card.SourceMemoryIDs
				var expectedCalls []string
				if mode == "duplicate" {
					failedGroup = duplicateIDs
					expectedCalls = append(expectedCalls, card.SourceMemoryIDs...)
				}
				fault.failID = failedGroup[failureIndex]
				expectedCalls = append(expectedCalls, failedGroup[:failureIndex+1]...)
				if mode == "discard" {
					card.RecommendedRecipe = []string{memory.ScenarioCardPlaceholderRecipe}
				}
				otherStores := map[string]store.MemoryStore{
					"personal":          org.ForUser(alice.ID).Memories(),
					"teammate-personal": org.ForUser(seed.Users[e2eboot.UserBobEmail].ID).Memories(),
					"org":               org.OrgMemories(),
					"other-team":        org.ForTeam(e2eboot.TeamAcmeBlue).Memories(),
					"other-org":         otherOrg.OrgMemories(),
					"other-org-team":    otherOrg.ForTeam(e2eboot.TeamGlobexEng).Memories(),
				}
				otherBefore := make(map[string]map[string]store.MemorySearchResult)
				for scope, ms := range otherStores {
					otherBefore[scope] = snapshot(ms)
					if len(otherBefore[scope]) == 0 {
						t.Fatalf("cross-scope assertion would be vacuous: %s", scope)
					}
				}
				before := snapshot(team)
				// An unauthenticated request must still traverse normal auth denial.
				unauth, err := server.Client().Get(server.URL + "/api/memories/team")
				if err != nil {
					t.Fatal(err)
				}
				unauth.Body.Close()
				if unauth.StatusCode != http.StatusUnauthorized {
					t.Fatalf("unauthenticated status=%d", unauth.StatusCode)
				}
				resp := seed.Client(e2eboot.UserAliceEmail).PostWithTimeout(t, "/api/memories/consolidate/apply", api.MemoryConsolidationApplyRequest{
					Card: card, TargetScope: "team", DuplicateCardIDs: duplicateIDs,
				}, 5*time.Second)
				body, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusInternalServerError || len(body) > 1024 {
					t.Fatalf("expected bounded 500, status=%d bytes=%d", resp.StatusCode, len(body))
				}
				var response struct {
					Applied bool   `json:"applied"`
					Error   string `json:"error"`
				}
				if err := json.Unmarshal(body, &response); err != nil || response.Applied || response.Error == "" {
					t.Fatalf("failure did not return an honest JSON error: %v", err)
				}
				for _, private := range []string{localMemoryDeleteCanary, "sqlite-delete-storage-canary", "/private/platform.db", "do-not-disclose"} {
					if strings.Contains(string(body), private) {
						t.Fatal("storage error leaked through HTTP")
					}
				}
				if !reflect.DeepEqual(fault.deleteCalls, expectedCalls) {
					t.Fatalf("cleanup calls=%v, want=%v", fault.deleteCalls, expectedCalls)
				}
				after := snapshot(team)
				for _, id := range expectedCalls[:len(expectedCalls)-1] {
					if _, exists := after[id]; exists {
						t.Fatalf("successful deletion rolled back: %s", id)
					}
					delete(before, id)
				}
				for id, row := range before {
					if !reflect.DeepEqual(after[id], row) {
						t.Fatalf("failed/later/unrelated memory changed: %s", id)
					}
				}
				newRows := 0
				for id, row := range after {
					if _, exists := before[id]; exists {
						continue
					}
					newRows++
					if mode == "discard" || row.Category != memory.ScenarioCardCategory || row.Snippet != memory.RenderScenarioCard(card) {
						t.Fatal("unexpected destination content after failed cleanup")
					}
				}
				wantNew := 1
				if mode == "discard" {
					wantNew = 0
				}
				if newRows != wantNew || len(after) != len(before)+wantNew {
					t.Fatalf("durable destinations=%d, want=%d", newRows, wantNew)
				}
				for scope, ms := range otherStores {
					if !reflect.DeepEqual(snapshot(ms), otherBefore[scope]) {
						t.Fatalf("failure changed cross-scope memory: %s", scope)
					}
				}
				t.Logf("JWT HTTP 500 bounded; %s failure %d stopped cleanup; %d earlier deletes and %d destination persisted; six other scopes unchanged", mode, failureIndex, len(expectedCalls)-1, wantNew)
			})
		}
	}
}

type localMemoryBlockedAudit struct {
	store.AuditStore
	started chan struct{}
	release chan struct{}
	done    chan error
}

func (a *localMemoryBlockedAudit) Log(ctx context.Context, entry *store.AuditEntry) error {
	close(a.started)
	select {
	case <-a.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	err := a.AuditStore.Log(ctx, entry)
	a.done <- err
	return err
}

func TestE2E_Memory_LocalFixtureDrainsAuditBeforeDatabaseCleanup(t *testing.T) {
	audit := &localMemoryBlockedAudit{started: make(chan struct{}), release: make(chan struct{}), done: make(chan error, 1)}
	listenerClosed := make(chan error, 1)
	if !t.Run("request", func(t *testing.T) {
		seed, _, server := localMemoryHTTPFixture(t, func(r *http.Request) *http.Request {
			svc := *store.FromRequest(r)
			if svc.Audit != nil {
				audit.AuditStore = svc.Audit
				svc.Audit = audit
			}
			return r.WithContext(store.WithServices(r.Context(), &svc))
		})
		resp := seed.Client(e2eboot.UserAliceEmail).Get(t, "/api/memories/team")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		select {
		case <-audit.started:
		case <-time.After(5 * time.Second):
			t.Fatal("audit never started")
		}
		// Hold the real SQLite audit write until fixture shutdown closes the
		// listener. It must still have an open tenant DB at that point.
		go func() {
			defer close(audit.release)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), 100*time.Millisecond)
				if err != nil {
					listenerClosed <- nil
					return
				}
				conn.Close()
				time.Sleep(time.Millisecond)
			}
			listenerClosed <- errors.New("fixture listener did not close")
		}()
	}) {
		return
	}
	if err := <-listenerClosed; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-audit.done:
		if err != nil {
			t.Fatalf("audit outlived its tenant database: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not drain audit")
	}
}

// localMemoryTrackedAudit preserves real asynchronous audit persistence while
// letting the fixture join it before Seed decommissions tenant databases.
type localMemoryTrackedAudit struct {
	store.AuditStore
	pending *sync.WaitGroup
}

func (a localMemoryTrackedAudit) Log(ctx context.Context, entry *store.AuditEntry) error {
	defer a.pending.Done()
	return a.AuditStore.Log(ctx, entry)
}

// localMemoryHTTPFixture reuses the local deletion fixture, not external Bootstrap.
// Request overrides run only after normal JWT and tenant middleware resolution.
func localMemoryHTTPFixture(t *testing.T, override func(*http.Request) *http.Request) (*e2eboot.SeedResult, *entstore.Store, *httptest.Server) {
	t.Helper()
	dataDir := t.TempDir()
	svc, backend, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN: "file:" + filepath.Join(dataDir, "platform.db"), DataDir: dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	authCfg := config.PlatformAuthConfig{Mode: config.AuthModeBuiltin, JWTSecret: "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"}
	auth := api.NewPlatformAuth(authCfg, backend, config.StorageConfig{Backend: "sqlite", Auth: authCfg})
	var pendingAudit sync.WaitGroup
	tenantMW := func(next http.Handler) http.Handler {
		return entstore.TenantMiddleware(backend)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = override(r)
			requestSvc := *store.FromRequest(r)
			if requestSvc.Audit != nil && requestSvc.Mode == store.ModePlatform {
				// Register before the handler can return, not inside the audit
				// goroutine: Close then Wait must not race a late Add.
				pendingAudit.Add(1)
				requestSvc.Audit = localMemoryTrackedAudit{AuditStore: requestSvc.Audit, pending: &pendingAudit}
				r = r.WithContext(store.WithServices(r.Context(), &requestSvc))
			}
			next.ServeHTTP(w, r)
		}))
	}
	router := mux.NewRouter()
	api.RegisterPlatformAuthRoutes(router, auth)
	api.RegisterRoutes(router, svc, backend, tenantMW)
	server := httptest.NewServer(api.PlatformAuthMiddleware(auth, router))
	// Register after Seed's cleanup hooks (also on Fatal/Goexit). Cleanup is
	// LIFO: stop accepting requests, join handlers/audits, then close databases.
	defer func() {
		t.Cleanup(func() {
			server.Close()
			pendingAudit.Wait()
		})
	}()
	h := &e2eboot.Harness{BaseURL: server.URL, Store: backend, DataDir: dataDir}
	return e2eboot.Seed(t, h), backend, server
}

// Add generates IDs in the real stores. This wrapper deliberately aliases one
// persisted row's ID for Get/Delete; it does NOT establish a SQL-level collision.
// All successful reads/deletes still reach that row in the real scoped SQLite DB.
type localMemoryIDAlias struct {
	store.MemoryStore
	alias, backingID string
}

func (s *localMemoryIDAlias) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	if id != s.alias {
		return s.MemoryStore.Get(ctx, id)
	}
	row, err := s.MemoryStore.Get(ctx, s.backingID)
	if row != nil {
		copy := *row
		copy.ID = s.alias
		row = &copy
	}
	return row, err
}

func (s *localMemoryIDAlias) Delete(ctx context.Context, id string) error {
	if id == s.alias {
		id = s.backingID
	}
	return s.MemoryStore.Delete(ctx, id)
}

// These scoped delegating wrappers mirror the existing consolidation unit fixture
// pattern while preserving every unrelated organization/user and real DB store.
type localMemoryScopedRouter struct {
	store.TenantRouter
	orgSlug, userID string
	scope           store.MemoryScope
	memory          store.MemoryStore
}

func (s localMemoryScopedRouter) ForOrg(slug string) (store.OrgDataStore, error) {
	org, err := s.TenantRouter.ForOrg(slug)
	if err != nil || slug != s.orgSlug {
		return org, err
	}
	return localMemoryScopedOrg{OrgDataStore: org, userID: s.userID, scope: s.scope, memory: s.memory}, nil
}

type localMemoryScopedOrg struct {
	store.OrgDataStore
	userID string
	scope  store.MemoryScope
	memory store.MemoryStore
}

func (s localMemoryScopedOrg) ForUser(id string) store.PersonalDataStore {
	personal := s.OrgDataStore.ForUser(id)
	if s.scope == store.MemoryScopePersonal && id == s.userID {
		return localMemoryScopedPersonal{PersonalDataStore: personal, memory: s.memory}
	}
	return personal
}

func (s localMemoryScopedOrg) OrgMemories() store.MemoryStore {
	if s.scope == store.MemoryScopeOrg {
		return s.memory
	}
	return s.OrgDataStore.OrgMemories()
}

type localMemoryScopedPersonal struct {
	store.PersonalDataStore
	memory store.MemoryStore
}

func (s localMemoryScopedPersonal) Memories() store.MemoryStore { return s.memory }

// TestE2E_SecurityUrgentScopedMemoryCleanup exercises production HTTP/JWT with
// real SQLite persistence, injected cross-scope ID aliasing and delete failures.
// It does not call Bootstrap or establish an actual SQL-level ID collision.
// COVERS: CHAT-113
func TestE2E_SecurityUrgentScopedMemoryCleanup(t *testing.T) {
	for _, participant := range []string{"source", "duplicate"} {
		for _, scope := range []store.MemoryScope{store.MemoryScopePersonal, store.MemoryScopeOrg} {
			for _, outcome := range []string{"success", "failure"} {
				t.Run(fmt.Sprintf("%s/%s/%s", participant, scope, outcome), func(t *testing.T) {
					ctx := context.Background()
					var scoped *localMemoryScopedRouter
					var targetCalls *localMemoryDeleteFailure
					seed, backend, server := localMemoryHTTPFixture(t, func(r *http.Request) *http.Request {
						tc := store.TenantContextFrom(r.Context())
						if scoped != nil && tc != nil && tc.OrgSlug == scoped.orgSlug && tc.TeamSlug == e2eboot.TeamAcmeRed {
							svc := *store.FromRequest(r)
							svc.TenantRouter, svc.Memory = scoped, targetCalls
							return r.WithContext(store.WithServices(r.Context(), &svc))
						}
						return r
					})
					alice := seed.Users[e2eboot.UserAliceEmail]
					org, err := backend.ForOrg(alice.OrgSlug)
					if err != nil {
						t.Fatal(err)
					}
					foreign, err := backend.ForOrg(seed.Users[e2eboot.UserEveEmail].OrgSlug)
					if err != nil {
						t.Fatal(err)
					}
					stores := map[string]store.MemoryStore{
						"team":              org.ForTeam(alice.TeamSlug).Memories(),
						"personal":          org.ForUser(alice.ID).Memories(),
						"org":               org.OrgMemories(),
						"teammate-personal": org.ForUser(seed.Users[e2eboot.UserBobEmail].ID).Memories(),
						"other-team":        org.ForTeam(e2eboot.TeamAcmeBlue).Memories(),
						"other-org":         foreign.OrgMemories(),
						"other-org-team":    foreign.ForTeam(e2eboot.TeamGlobexEng).Memories(),
					}
					snapshot := func(ms store.MemoryStore) map[string]store.MemorySearchResult {
						t.Helper()
						rows, err := ms.List(ctx, "", 1000, 0)
						if err != nil {
							t.Fatal(err)
						}
						result := make(map[string]store.MemorySearchResult, len(rows))
						for _, row := range rows {
							full, err := ms.Get(ctx, row.ID)
							if err != nil || full == nil {
								t.Fatalf("get persisted memory %s: %v", row.ID, err)
							}
							result[row.ID] = *full
						}
						return result
					}
					add := func(ms store.MemoryStore, content, category string) string {
						t.Helper()
						if err := ms.Add(ctx, store.MemoryEntry{Content: content, Category: category, CreatedBy: alice.ID}); err != nil {
							t.Fatal(err)
						}
						for id, row := range snapshot(ms) {
							if row.Snippet == content {
								return id
							}
						}
						t.Fatal("seeded memory missing")
						return ""
					}
					card := memory.ScenarioCard{CanonicalKey: "scoped-console-recovery", Title: "Scoped Console Recovery", Scope: "team", RecommendedRecipe: []string{"Use the noVNC ticket endpoint."}}
					team, other := stores["team"], stores[string(scope)]
					destinationID := add(team, memory.RenderScenarioCard(card), memory.ScenarioCardCategory)
					content, category := "authorized raw scoped participant", "note"
					if participant == "duplicate" {
						content, category = memory.RenderScenarioCard(card), memory.ScenarioCardCategory
					}
					backingID := add(other, content, category)
					if backingID == destinationID {
						t.Fatal("expected distinct generated backing IDs for alias fixture")
					}
					fault := &localMemoryDeleteFailure{MemoryStore: &localMemoryIDAlias{MemoryStore: other, alias: destinationID, backingID: backingID}}
					if outcome == "failure" {
						fault.failID = destinationID
					}
					targetCalls = &localMemoryDeleteFailure{MemoryStore: team}
					scoped = &localMemoryScopedRouter{TenantRouter: backend, orgSlug: alice.OrgSlug, userID: alice.ID, scope: scope, memory: fault}
					before := make(map[string]map[string]store.MemorySearchResult)
					for name, ms := range stores {
						before[name] = snapshot(ms)
						if len(before[name]) == 0 {
							t.Fatalf("vacuous snapshot: %s", name)
						}
					}
					card.RecommendedRecipe = []string{"Use the noVNC ticket endpoint, then verify the console."}
					request := api.MemoryConsolidationApplyRequest{Card: card, TargetScope: "team"}
					if participant == "source" {
						request.Card.SourceMemoryIDs = []string{destinationID}
					} else {
						request.DuplicateCardIDs = []string{destinationID}
					}
					unauth, err := server.Client().Get(server.URL + "/api/memories/team")
					if err != nil {
						t.Fatal(err)
					}
					unauth.Body.Close()
					if unauth.StatusCode != http.StatusUnauthorized {
						t.Fatalf("unauthenticated status=%d", unauth.StatusCode)
					}
					resp := seed.Client(e2eboot.UserAliceEmail).PostWithTimeout(t, "/api/memories/consolidate/apply", request, 5*time.Second)
					body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
					resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if outcome == "failure" {
						var response struct {
							Applied bool   `json:"applied"`
							Error   string `json:"error"`
						}
						if resp.StatusCode != http.StatusInternalServerError || len(body) > 1024 {
							t.Fatalf("expected bounded 500, status=%d bytes=%d", resp.StatusCode, len(body))
						}
						if err := json.Unmarshal(body, &response); err != nil || response.Applied || !strings.Contains(response.Error, "scenario card saved") {
							t.Fatalf("dishonest cleanup error: %s (%v)", body, err)
						}
						for _, private := range []string{localMemoryDeleteCanary, "sqlite-delete-storage-canary", "/private/platform.db", "do-not-disclose"} {
							if strings.Contains(string(body), private) {
								t.Fatal("storage error leaked through HTTP")
							}
						}
					} else {
						var response api.MemoryConsolidationApplyResponse
						if err := json.Unmarshal(body, &response); err != nil {
							t.Fatal(err)
						}
						wantSources, wantDuplicates := 0, 1
						if participant == "source" {
							wantSources, wantDuplicates = 1, 0
						}
						if resp.StatusCode != http.StatusOK || !response.Applied || response.ExistingID != destinationID || response.Action != "merged" || response.Scope != "team" || response.DeletedSources != wantSources || response.DeletedDuplicateCards != wantDuplicates {
							t.Fatalf("unexpected scoped cleanup response: status=%d body=%s", resp.StatusCode, body)
						}
					}
					if !reflect.DeepEqual(fault.deleteCalls, []string{destinationID}) || len(targetCalls.deleteCalls) != 0 {
						t.Fatalf("wrong deletes: participant=%v destination=%v", fault.deleteCalls, targetCalls.deleteCalls)
					}
					for name, ms := range stores {
						after := snapshot(ms)
						if name == "team" {
							destination, exists := after[destinationID]
							if !exists || destination.Category != memory.ScenarioCardCategory || !strings.Contains(destination.Snippet, card.RecommendedRecipe[0]) || destination.Snippet == before[name][destinationID].Snippet {
								t.Fatal("updated durable team destination missing")
							}
							delete(after, destinationID)
							delete(before[name], destinationID)
						}
						if name == string(scope) && outcome == "success" {
							delete(before[name], backingID)
						}
						if !reflect.DeepEqual(after, before[name]) {
							t.Fatalf("unexpected persisted rows in %s", name)
						}
					}
					t.Logf("JWT HTTP %s: aliased %s %s delete attempted; team destination persisted; backing SQLite rows checked (not a SQL ID collision)", outcome, scope, participant)
				})
			}
		}
	}
}

// TestE2E_Memory_PersonalIsolation verifies that personal memories are
// visible only to their owner and invisible to other users, even teammates.
//
// COVERS: CHAT-017
func TestE2E_Memory_PersonalIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice can see her own personal memory
	t.Run("alice_sees_own_personal_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/personal")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
			Count   int              `json:"count"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				found = true
				break
			}
		}
		if !found {
			t.Error("alice cannot see her own personal memory")
		}
	})

	// Bob searches for Alice's personal memory — should get nothing
	t.Run("bob_cannot_search_alice_personal_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAlicePersonal,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				t.Error("bob (same team) should NOT see alice's personal memory via search")
			}
		}
	})

	// Eve (different org) searches for Alice's personal memory — should get nothing
	t.Run("eve_cannot_search_alice_personal_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAlicePersonal,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAlicePersonal) {
				t.Error("eve (different org) should NOT see alice's personal memory")
			}
		}
	})
}

// TestE2E_Memory_TeamVisibility verifies that team memories are searchable
// by all members of that team.
//
// COVERS: CHAT-018
func TestE2E_Memory_TeamVisibility(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team admin) sees red team memory
	t.Run("alice_sees_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeRedTeam) {
				found = true
				break
			}
		}
		if !found {
			t.Error("alice (red team) cannot see red team memory")
		}
	})

	// Bob (red team member) also sees red team memory
	t.Run("bob_sees_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserBobEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		found := false
		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeRedTeam) {
				found = true
				break
			}
		}
		if !found {
			t.Error("bob (red team member) cannot see red team memory")
		}
	})
}

// TestE2E_Memory_TeamIsolation verifies that team memories are invisible
// to members of other teams in the same org.
//
// COVERS: CHAT-019
func TestE2E_Memory_TeamIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (red team) cannot see blue team memory
	t.Run("alice_cannot_see_blue_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeBlueTeam) {
				t.Error("alice (red team) should NOT see blue team memory")
			}
		}
	})

	// Carol (blue team) cannot see red team memory
	t.Run("carol_cannot_see_red_team_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Get(t, "/api/memories/team")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var response struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &response)

		for _, m := range response.Results {
			snippet, _ := m["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeRedTeam) {
				t.Error("carol (blue team) should NOT see red team memory")
			}
		}
	})
}

// TestE2E_Memory_OrgIsolation verifies that org-level memories are visible
// across teams within the org but invisible to other orgs.
//
// COVERS: CHAT-020
func TestE2E_Memory_OrgIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	seed := e2eboot.Seed(t, h)

	// Alice (acme/red) can search and find acme org memory
	t.Run("alice_can_find_acme_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserAliceEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAcmeOrg,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("alice (acme) cannot find acme org memory via search")
		}
	})

	// Carol (acme/blue) can also find acme org memory (cross-team within org)
	t.Run("carol_can_find_acme_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserCarolEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAcmeOrg,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("carol (acme/blue) cannot find acme org memory — cross-team org visibility broken")
		}
	})

	// Eve (globex) cannot find acme org memory
	t.Run("eve_cannot_find_acme_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemAcmeOrg,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemAcmeOrg) {
				t.Error("eve (globex) should NOT see acme org memory — cross-org isolation broken")
			}
		}
	})

	// Eve (globex) CAN find globex org memory
	t.Run("eve_can_find_globex_org_memory", func(t *testing.T) {
		client := seed.Client(e2eboot.UserEveEmail)
		resp := client.Post(t, "/api/memories/search", map[string]string{
			"query": e2eboot.MemGlobexOrg,
		})
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := e2eboot.ReadBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Results []map[string]any `json:"results"`
		}
		e2eboot.DecodeJSON(t, resp, &result)

		found := false
		for _, r := range result.Results {
			snippet, _ := r["snippet"].(string)
			if strings.Contains(snippet, e2eboot.MemGlobexOrg) {
				found = true
				break
			}
		}
		if !found {
			t.Error("eve (globex) cannot find her own org memory")
		}
	})
}
