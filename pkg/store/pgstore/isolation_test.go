//go:build integration

package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// TestIsolation_OrgDataSeparation verifies that data written to one org schema
// is invisible from a different org schema (simulated via separate team schemas
// within the same database).
func TestIsolation_OrgDataSeparation(t *testing.T) {
	pool := testPool(t)
	schemaA := setupTestSchema(t, pool)
	schemaB := setupTestSchema(t, pool)
	ctx := context.Background()

	// --- Write a credential to org_a ---
	credStoreA := &pgCredentialStore{pool: pool, schema: schemaA, encKey: nil}
	err := credStoreA.Set(ctx, "my-api-key", &store.Credential{
		Type:  store.CredBearer,
		Value: "secret-token-for-org-a",
	})
	if err != nil {
		t.Fatalf("Set credential in org_a: %v", err)
	}

	// --- Write a flow to org_a ---
	flowStoreA := &pgFlowStore{pool: pool, schema: schemaA}
	err = flowStoreA.SaveFlowDefinition(ctx, "deploy-pipeline", map[string]any{"description": "org_a pipeline"}, "name: deploy-pipeline")
	if err != nil {
		t.Fatalf("SaveFlowDefinition in org_a: %v", err)
	}

	// --- Write a scheduled job to org_a ---
	schedStoreA := &pgSchedulerStore{pool: pool, schema: schemaA}
	jobA := &store.ScheduledJob{
		ID:        uuid.New().String(),
		Name:      "nightly-backup",
		Mode:      "routine",
		Schedule:  store.JobSchedule{Cron: "0 2 * * *"},
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	err = schedStoreA.Add(ctx, jobA)
	if err != nil {
		t.Fatalf("Add scheduled job in org_a: %v", err)
	}

	// --- Read from org_b: should find nothing ---
	credStoreB := &pgCredentialStore{pool: pool, schema: schemaB, encKey: nil}
	if got := credStoreB.Get(ctx, "my-api-key"); got != nil {
		t.Errorf("org_b should NOT see org_a credential, got %+v", got)
	}

	flowStoreB := &pgFlowStore{pool: pool, schema: schemaB}
	flows := flowStoreB.ListAllFlows(ctx)
	if len(flows) != 0 {
		t.Errorf("org_b should have 0 flows, got %d", len(flows))
	}

	schedStoreB := &pgSchedulerStore{pool: pool, schema: schemaB}
	jobs := schedStoreB.List(ctx)
	if len(jobs) != 0 {
		t.Errorf("org_b should have 0 scheduled jobs, got %d", len(jobs))
	}

	// --- Verify org_a still sees its own data ---
	if got := credStoreA.Get(ctx, "my-api-key"); got == nil {
		t.Error("org_a should see its own credential")
	}
	if flowsA := flowStoreA.ListAllFlows(ctx); len(flowsA) != 1 {
		t.Errorf("org_a should have 1 flow, got %d", len(flowsA))
	}
	if jobsA := schedStoreA.List(ctx); len(jobsA) != 1 {
		t.Errorf("org_a should have 1 scheduled job, got %d", len(jobsA))
	}
}

// TestIsolation_TeamDataSeparation verifies that two team schemas within the
// same database cannot see each other's data.
func TestIsolation_TeamDataSeparation(t *testing.T) {
	pool := testPool(t)
	schemaAlpha := setupTestSchema(t, pool)
	schemaBeta := setupTestSchema(t, pool)
	ctx := context.Background()

	// Write a session to team_alpha
	sessStoreAlpha := &pgSessionStore{pool: pool, schema: schemaAlpha, sessions: make(map[string]*pgSession)}
	meta := store.SessionMeta{
		ID:        uuid.New().String(),
		Title:     "Alpha session",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := sessStoreAlpha.AddSessionMeta(ctx, meta); err != nil {
		t.Fatalf("AddSessionMeta in team_alpha: %v", err)
	}

	// Write a credential to team_alpha
	credAlpha := &pgCredentialStore{pool: pool, schema: schemaAlpha, encKey: nil}
	if err := credAlpha.Set(ctx, "alpha-cred", &store.Credential{Type: store.CredAPIKey, Value: "key123"}); err != nil {
		t.Fatalf("Set credential in team_alpha: %v", err)
	}

	// Read from team_beta: should be empty
	sessStoreBeta := &pgSessionStore{pool: pool, schema: schemaBeta, sessions: make(map[string]*pgSession)}
	metas, err := sessStoreBeta.ListSessionMetas(ctx, "app", "user")
	if err != nil {
		t.Fatalf("ListSessionMetas in team_beta: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("team_beta should have 0 sessions, got %d", len(metas))
	}

	credBeta := &pgCredentialStore{pool: pool, schema: schemaBeta, encKey: nil}
	if got := credBeta.Get(ctx, "alpha-cred"); got != nil {
		t.Errorf("team_beta should NOT see team_alpha credential, got %+v", got)
	}

	// Verify team_alpha still sees its data
	metasAlpha, err := sessStoreAlpha.ListSessionMetas(ctx, "app", "user")
	if err != nil {
		t.Fatalf("ListSessionMetas in team_alpha: %v", err)
	}
	if len(metasAlpha) != 1 {
		t.Errorf("team_alpha should have 1 session, got %d", len(metasAlpha))
	}
}

// TestIsolation_ForOrgInvalidSlug verifies that attempting to look up data in
// a non-existent schema gracefully returns no data (credential Get returns nil,
// scheduler List returns nil, etc.).
func TestIsolation_ForOrgInvalidSlug(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	nonExistentSchema := "nonexistent_schema_xyz_99999"

	// Credential store with non-existent schema — Get should return nil (query error)
	credStore := &pgCredentialStore{pool: pool, schema: nonExistentSchema, encKey: nil}
	if got := credStore.Get(ctx, "anything"); got != nil {
		t.Errorf("Get on non-existent schema should return nil, got %+v", got)
	}

	// Scheduler store with non-existent schema — List should return nil
	schedStore := &pgSchedulerStore{pool: pool, schema: nonExistentSchema}
	if jobs := schedStore.List(ctx); jobs != nil {
		t.Errorf("List on non-existent schema should return nil, got %+v", jobs)
	}
}
