//go:build integration

package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Helpers for Phase A sandbox stores.
//
// The SandboxTemplateStore and LayerStore use UNQUALIFIED table names
// (sandbox_templates / sandbox_layers) and rely on search_path to resolve
// them. For isolation each test gets its own schema and a dedicated pool
// whose AfterConnect hook pins search_path to that schema.
//
// ChatEventJournal is schema-qualified by construction so the shared
// testPool + setupTestSchema helpers from memories_integration_test.go work.
// ---------------------------------------------------------------------------

// setupPlatformTestSchema creates a fresh schema, applies the platform
// migration set against it (via a conn whose search_path is pinned), and
// returns (pinnedPool, schemaName). The pool's connections all have
// search_path = <schema>, so unqualified DDL/DML in the Phase A migration
// and in pgSandboxTemplateStore / pgLayerStore resolve correctly.
//
// Cleanup drops the schema and closes the pool.
func setupPlatformTestSchema(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	// Acquire the shared pool just to create the schema.
	bootstrap := testPool(t)
	ctx := context.Background()
	schema := testSchema(t)
	if _, err := bootstrap.Exec(ctx,
		fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		t.Fatalf("create platform test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrap.Exec(context.Background(),
			fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schema}.Sanitize()))
	})

	// Build a dedicated pool pinned to the test schema.
	dsn := mustTestDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.MaxConns = 4
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		// Include public so the pgvector type (installed in public) and any
		// platform tables created there by earlier tests remain resolvable.
		_, err := c.Exec(ctx,
			fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
		)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pinned pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Apply platform migrations via a dedicated conn (must also have
	// search_path pinned).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for migrations: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		t.Fatalf("pin migration search_path: %v", err)
	}
	if err := Migrate(ctx, conn, MigrationPlatform, schema); err != nil {
		t.Fatalf("apply platform migrations: %v", err)
	}

	return pool, schema
}

// mustTestDSN returns the test DSN or skips the test. Mirrors the skip
// semantics of testPool().
func mustTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set; skipping integration test")
	}
	return dsn
}

// seedBaseTemplate inserts the global "@base" root so child templates can
// reference it via parent_template_id. Returns the base template ID.
func seedBaseTemplate(t *testing.T, ctx context.Context, ts store.SandboxTemplateStore) string {
	t.Helper()
	base := &store.SandboxTemplate{
		Slug:    "base",
		Scope:   store.SandboxTemplateScopeGlobal,
		OwnerID: "",
		Name:    "@base",
		Version: 1,
	}
	if err := ts.Create(ctx, base); err != nil {
		t.Fatalf("seed @base: %v", err)
	}
	if base.ID == "" {
		t.Fatal("seed @base: ID not populated")
	}
	return base.ID
}

// ---------------------------------------------------------------------------
// SandboxTemplateStore integration tests
// ---------------------------------------------------------------------------

func TestPGSandboxTemplateStore_CRUD(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)

	baseID := seedBaseTemplate(t, ctx, ts)

	orgID := uuid.New().String()
	tpl := &store.SandboxTemplate{
		Slug:             "node-dev",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Node Dev",
		Description:      "Node 22 + pnpm",
		ParentTemplateID: &baseID,
		Version:          1,
	}
	if err := ts.Create(ctx, tpl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tpl.ID == "" {
		t.Fatal("Create did not populate ID")
	}

	got, err := ts.GetByID(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Slug != "node-dev" || got.OwnerID != orgID {
		t.Errorf("GetByID mismatch: slug=%q owner=%q", got.Slug, got.OwnerID)
	}
	if got.ParentTemplateID == nil || *got.ParentTemplateID != baseID {
		t.Errorf("ParentTemplateID = %v, want %v", got.ParentTemplateID, baseID)
	}

	gotBySlug, err := ts.GetBySlug(ctx, store.SandboxTemplateScopeOrg, orgID, "node-dev")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if gotBySlug == nil || gotBySlug.ID != tpl.ID {
		t.Errorf("GetBySlug mismatch")
	}

	// Update mutable fields.
	tpl.Name = "Node Dev v2"
	tpl.Description = "Node 22 + pnpm + corepack"
	tpl.Version = 2
	if err := ts.Update(ctx, tpl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = ts.GetByID(ctx, tpl.ID)
	if got.Name != "Node Dev v2" || got.Version != 2 {
		t.Errorf("Update did not persist: name=%q version=%d", got.Name, got.Version)
	}

	// List with filter.
	tpls, err := ts.List(ctx, store.SandboxTemplateFilter{
		Scope:   store.SandboxTemplateScopeOrg,
		OwnerID: orgID,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tpls) != 1 {
		t.Errorf("List returned %d templates, want 1", len(tpls))
	}

	// Delete.
	if err := ts.Delete(ctx, tpl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = ts.GetByID(ctx, tpl.ID)
	if got != nil {
		t.Error("GetByID after Delete returned non-nil")
	}
}

func TestPGSandboxTemplateStore_UniqueSlug(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	baseID := seedBaseTemplate(t, ctx, ts)

	orgID := uuid.New().String()
	first := &store.SandboxTemplate{
		Slug:             "dupe",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "First",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := &store.SandboxTemplate{
		Slug:             "dupe",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Second",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, second); err == nil {
		t.Fatal("expected unique-violation error on duplicate (scope, owner, slug)")
	}
}

func TestPGSandboxTemplateStore_Resolve(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	ls := NewPGLayerStore(pool)

	// Seed 3 layers.
	layerA := seedLayer(t, ctx, ls, "aaaaaaaaaaaaaaaa")
	layerB := seedLayer(t, ctx, ls, "bbbbbbbbbbbbbbbb")
	layerC := seedLayer(t, ctx, ls, "cccccccccccccccc")

	// Base has layerA as top.
	base := &store.SandboxTemplate{
		Slug:       "base",
		Scope:      store.SandboxTemplateScopeGlobal,
		Name:       "@base",
		TopLayerID: &layerA,
	}
	if err := ts.Create(ctx, base); err != nil {
		t.Fatalf("create base: %v", err)
	}
	orgID := uuid.New().String()
	mid := &store.SandboxTemplate{
		Slug:             "org-customized",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Org customized",
		ParentTemplateID: &base.ID,
		TopLayerID:       &layerB,
	}
	if err := ts.Create(ctx, mid); err != nil {
		t.Fatalf("create mid: %v", err)
	}
	userID := uuid.New().String()
	leaf := &store.SandboxTemplate{
		Slug:             "my-box",
		Scope:            store.SandboxTemplateScopePersonal,
		OwnerID:          userID,
		Name:             "My box",
		ParentTemplateID: &mid.ID,
		TopLayerID:       &layerC,
	}
	if err := ts.Create(ctx, leaf); err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	chain, err := ts.Resolve(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if chain == nil {
		t.Fatal("Resolve returned nil for existing template")
	}
	want := []string{layerA, layerB, layerC} // oldest first
	if len(chain.LayerIDs) != 3 {
		t.Fatalf("LayerIDs len = %d, want 3: %v", len(chain.LayerIDs), chain.LayerIDs)
	}
	for i, got := range chain.LayerIDs {
		if got != want[i] {
			t.Errorf("LayerIDs[%d] = %q, want %q", i, got, want[i])
		}
	}

	// Missing template returns nil, nil.
	chain, err = ts.Resolve(ctx, uuid.New().String())
	if err != nil {
		t.Fatalf("Resolve missing: %v", err)
	}
	if chain != nil {
		t.Errorf("Resolve missing returned %+v, want nil", chain)
	}
}

func TestPGSandboxTemplateStore_CycleRejected(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	baseID := seedBaseTemplate(t, ctx, ts)

	// Create A → base.
	a := &store.SandboxTemplate{
		Slug:             "a",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          uuid.New().String(),
		Name:             "A",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, a); err != nil {
		t.Fatalf("create A: %v", err)
	}

	// Try to repoint A's parent to itself — must be rejected either by the
	// Update's immutability contract or by the cycle trigger if attempted
	// at raw SQL level. We exercise both paths.
	// 1) Update() is documented to ignore parent changes; the row stays
	//    pointed at baseID. Not a cycle test per se, but confirms immutability.
	selfID := a.ID
	a.ParentTemplateID = &selfID
	if err := ts.Update(ctx, a); err != nil {
		t.Fatalf("Update (should ignore parent change): %v", err)
	}
	got, _ := ts.GetByID(ctx, a.ID)
	if got.ParentTemplateID == nil || *got.ParentTemplateID != baseID {
		t.Errorf("Parent mutated through Update: got=%v, want=%v",
			got.ParentTemplateID, baseID)
	}

	// 2) Direct SQL cycle attempt should fire the trigger.
	_, err := pool.Exec(ctx,
		`UPDATE sandbox_templates SET parent_template_id = id WHERE id = $1`,
		a.ID,
	)
	if err == nil {
		t.Error("expected cycle trigger to reject self-parent UPDATE")
	}
}

func TestPGSandboxTemplateStore_ListRoots(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)

	baseID := seedBaseTemplate(t, ctx, ts)
	child := &store.SandboxTemplate{
		Slug:             "child",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          uuid.New().String(),
		Name:             "Child",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	roots, err := ts.ListRoots(ctx)
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("ListRoots returned %d roots, want 1", len(roots))
	}
	if roots[0].ID != baseID {
		t.Errorf("ListRoots[0].ID = %q, want %q", roots[0].ID, baseID)
	}
}

// ---------------------------------------------------------------------------
// LayerStore integration tests
// ---------------------------------------------------------------------------

// seedLayer inserts a layer with a unique synthetic hex ID (64 chars to pass
// as a plausible sha256). Returns the ID.
func seedLayer(t *testing.T, ctx context.Context, ls store.LayerStore, seed string) string {
	t.Helper()
	id := fmt.Sprintf("%064s", seed)[:64]
	layer := &store.SandboxLayer{
		LayerID:    id,
		CephFSPath: "/mnt/astonish-layers/" + id,
		SizeBytes:  1024,
	}
	if err := ls.PutLayer(ctx, layer); err != nil {
		t.Fatalf("PutLayer(%s): %v", id, err)
	}
	return id
}

func TestPGLayerStore_PutAndGet(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	id := seedLayer(t, ctx, ls, "abcdef1234567890")
	got, err := ls.GetLayer(ctx, id)
	if err != nil {
		t.Fatalf("GetLayer: %v", err)
	}
	if got == nil {
		t.Fatal("GetLayer returned nil")
	}
	if got.LayerID != id {
		t.Errorf("LayerID = %q, want %q", got.LayerID, id)
	}
	if got.RefCount != 0 {
		t.Errorf("RefCount = %d, want 0 on fresh layer", got.RefCount)
	}
	if got.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", got.SizeBytes)
	}

	// Idempotent re-put: must succeed and must not change ref_count.
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("IncrementRefCount: %v", err)
	}
	dup := &store.SandboxLayer{
		LayerID:    id,
		CephFSPath: "/different/path",
		SizeBytes:  99999,
	}
	if err := ls.PutLayer(ctx, dup); err != nil {
		t.Fatalf("PutLayer idempotent: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got.RefCount != 1 {
		t.Errorf("RefCount after idempotent put = %d, want 1", got.RefCount)
	}
	if got.SizeBytes != 1024 {
		t.Errorf("SizeBytes after idempotent put = %d, want 1024 (unchanged)", got.SizeBytes)
	}

	// Missing layer.
	got, err = ls.GetLayer(ctx, fmt.Sprintf("%064s", "deadbeef"))
	if err != nil {
		t.Fatalf("GetLayer missing: %v", err)
	}
	if got != nil {
		t.Errorf("GetLayer missing returned %+v", got)
	}
}

func TestPGLayerStore_RefCountLifecycle(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	id := seedLayer(t, ctx, ls, "refcount")

	// Inc twice → 2.
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("Inc 1: %v", err)
	}
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("Inc 2: %v", err)
	}
	got, _ := ls.GetLayer(ctx, id)
	if got.RefCount != 2 {
		t.Errorf("RefCount after 2xInc = %d, want 2", got.RefCount)
	}

	// Dec once → 1.
	if err := ls.DecrementRefCount(ctx, id); err != nil {
		t.Fatalf("Dec 1: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got.RefCount != 1 {
		t.Errorf("RefCount after Dec = %d, want 1", got.RefCount)
	}

	// DeleteLayer must refuse while referenced.
	if err := ls.DeleteLayer(ctx, id); err == nil {
		t.Error("DeleteLayer on referenced layer should fail")
	}

	// Dec to 0.
	if err := ls.DecrementRefCount(ctx, id); err != nil {
		t.Fatalf("Dec 2: %v", err)
	}

	// Further Dec at 0 must error.
	if err := ls.DecrementRefCount(ctx, id); err == nil {
		t.Error("Decrement at 0 should error")
	}

	// Inc on missing layer must error.
	if err := ls.IncrementRefCount(ctx, fmt.Sprintf("%064s", "missing")); err == nil {
		t.Error("Increment missing layer should error")
	}

	// DeleteLayer at 0 now succeeds.
	if err := ls.DeleteLayer(ctx, id); err != nil {
		t.Fatalf("DeleteLayer at 0: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got != nil {
		t.Errorf("layer still present after Delete: %+v", got)
	}
}

func TestPGLayerStore_ListUnreferenced(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	unref := seedLayer(t, ctx, ls, "unref")
	refd := seedLayer(t, ctx, ls, "refd")
	if err := ls.IncrementRefCount(ctx, refd); err != nil {
		t.Fatalf("Inc refd: %v", err)
	}

	// With grace=0 both-layers-old enough, only unref shows.
	got, err := ls.ListUnreferenced(ctx, 0)
	if err != nil {
		t.Fatalf("ListUnreferenced: %v", err)
	}
	if len(got) != 1 || got[0].LayerID != unref {
		ids := make([]string, len(got))
		for i, l := range got {
			ids[i] = l.LayerID
		}
		t.Errorf("ListUnreferenced = %v, want [%s]", ids, unref)
	}

	// Large grace: nothing qualifies (layer just added).
	got, err = ls.ListUnreferenced(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ListUnreferenced grace=1h: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListUnreferenced grace=1h returned %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// ChatEventJournal integration tests (team-schema)
// ---------------------------------------------------------------------------

// seedSession inserts a minimal sessions row so the FK + last_seq UPDATE land.
func seedSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, id string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %s.sessions (id) VALUES ($1)`,
			pgx.Identifier{schema}.Sanitize()),
		id,
	)
	if err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
}

func TestPGChatEventJournal_AppendAndRead(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	chatID := uuid.New().String()
	seedSession(t, ctx, pool, schema, chatID)

	// LastSeq on fresh session = 0.
	seq, err := j.LastSeq(ctx, chatID)
	if err != nil {
		t.Fatalf("LastSeq fresh: %v", err)
	}
	if seq != 0 {
		t.Errorf("LastSeq fresh = %d, want 0", seq)
	}

	// Append 3 events in one batch.
	payload := func(s string) []byte {
		b, _ := json.Marshal(map[string]string{"msg": s})
		return b
	}
	events := []*store.ChatEvent{
		{ChatSessionID: chatID, EventType: "user.message", Payload: payload("hi"), ProducerPod: "pod-a"},
		{ChatSessionID: chatID, EventType: "assistant.partial", Payload: payload("think"), ProducerPod: "pod-a"},
		{ChatSessionID: chatID, EventType: "assistant.final", Payload: payload("done"), ProducerPod: "pod-a"},
	}
	if err := j.Append(ctx, events); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Seq values assigned in order.
	for i, e := range events {
		wantSeq := int64(i + 1)
		if e.Seq != wantSeq {
			t.Errorf("events[%d].Seq = %d, want %d", i, e.Seq, wantSeq)
		}
	}

	seq, _ = j.LastSeq(ctx, chatID)
	if seq != 3 {
		t.Errorf("LastSeq after batch = %d, want 3", seq)
	}

	// Append another batch — seq continues.
	more := []*store.ChatEvent{
		{ChatSessionID: chatID, EventType: "user.message", Payload: payload("again"), ProducerPod: "pod-b"},
	}
	if err := j.Append(ctx, more); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if more[0].Seq != 4 {
		t.Errorf("second batch Seq = %d, want 4", more[0].Seq)
	}

	// ReadSince(0) → all 4 events.
	got, err := j.ReadSince(ctx, chatID, 0, 0)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("ReadSince returned %d events, want 4", len(got))
	}
	for i, e := range got {
		if e.Seq != int64(i+1) {
			t.Errorf("got[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}

	// ReadSince(2) → events 3 and 4.
	got, err = j.ReadSince(ctx, chatID, 2, 0)
	if err != nil {
		t.Fatalf("ReadSince(2): %v", err)
	}
	if len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
		seqs := make([]int64, len(got))
		for i, e := range got {
			seqs[i] = e.Seq
		}
		t.Errorf("ReadSince(2) seqs = %v, want [3 4]", seqs)
	}

	// ReadSince with limit.
	got, err = j.ReadSince(ctx, chatID, 0, 2)
	if err != nil {
		t.Fatalf("ReadSince limit: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ReadSince limit=2 returned %d", len(got))
	}
}

func TestPGChatEventJournal_AppendRejectsMixedChat(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	chatA := uuid.New().String()
	chatB := uuid.New().String()
	seedSession(t, ctx, pool, schema, chatA)
	seedSession(t, ctx, pool, schema, chatB)

	events := []*store.ChatEvent{
		{ChatSessionID: chatA, EventType: "x", Payload: []byte("{}")},
		{ChatSessionID: chatB, EventType: "x", Payload: []byte("{}")},
	}
	if err := j.Append(ctx, events); err == nil {
		t.Error("expected error on mixed-chat batch")
	}

	// Neither session should have advanced.
	for _, id := range []string{chatA, chatB} {
		seq, err := j.LastSeq(ctx, id)
		if err != nil {
			t.Fatalf("LastSeq %s: %v", id, err)
		}
		if seq != 0 {
			t.Errorf("LastSeq %s = %d after failed mixed batch, want 0", id, seq)
		}
	}
}

func TestPGChatEventJournal_AppendUnknownChat(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	events := []*store.ChatEvent{
		{ChatSessionID: uuid.New().String(), EventType: "x", Payload: []byte("{}")},
	}
	if err := j.Append(ctx, events); err == nil {
		t.Error("expected error when chat session does not exist")
	}
}

func TestPGChatEventJournal_EmptyBatchNoop(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	if err := j.Append(ctx, nil); err != nil {
		t.Errorf("Append(nil) = %v, want nil", err)
	}
	if err := j.Append(ctx, []*store.ChatEvent{}); err != nil {
		t.Errorf("Append([]) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// SandboxSessionStore integration tests
//
// sandbox_sessions is team-scoped (team migration 002). We use the shared
// setupTestSchema helper which runs team migrations. Because the
// chat_session_events FK references {{schema}}.sessions(id), and because
// sandbox_sessions.chat_session_id has no FK (it's cross-table-shaped but
// unenforced), the session store tests do not require a seeded chat session.
// ---------------------------------------------------------------------------

// seedSandboxSession returns a SandboxSession with defaulted required fields
// suitable for the team schema shape. The caller can override fields before
// calling Put.
func seedSandboxSession(t *testing.T, sessionID string) *store.SandboxSession {
	t.Helper()
	return &store.SandboxSession{
		SessionID:     sessionID,
		ChatSessionID: sessionID,
		Backend:       "incus",
		ContainerName: "astn-" + sessionID,
		TemplateID:    uuid.New().String(),
		State:         store.SandboxSessionStateRunning,
		ExposedPorts:  []int{8080},
		BaseDomain:    "sandbox.test",
	}
}

func TestPGSandboxSessionStore_PutGetDelete(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := NewPGSandboxSessionStore(pool, schema)
	sess := seedSandboxSession(t, uuid.New().String())
	if err := ss.Put(ctx, sess); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := ss.Get(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get: nil")
	}
	if got.ContainerName != sess.ContainerName || got.TemplateID != sess.TemplateID {
		t.Fatalf("mismatch: %+v", got)
	}
	if got.State != store.SandboxSessionStateRunning {
		t.Fatalf("State: %q", got.State)
	}
	if len(got.ExposedPorts) != 1 || got.ExposedPorts[0] != 8080 {
		t.Fatalf("ExposedPorts: %+v", got.ExposedPorts)
	}
	if got.BaseDomain != "sandbox.test" {
		t.Fatalf("BaseDomain: %q", got.BaseDomain)
	}
	if got.Backend != "incus" {
		t.Fatalf("Backend: %q", got.Backend)
	}
	if got.ChatSessionID != sess.SessionID {
		t.Fatalf("ChatSessionID: %q", got.ChatSessionID)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("timestamps not set")
	}

	// Replace preserves CreatedAt.
	created := got.CreatedAt
	time.Sleep(10 * time.Millisecond)
	sess.State = store.SandboxSessionStateEvicted
	if err := ss.Put(ctx, sess); err != nil {
		t.Fatalf("Put replace: %v", err)
	}
	got, _ = ss.Get(ctx, sess.SessionID)
	if !got.CreatedAt.Equal(created) {
		// Allow sub-microsecond rounding from PG's microsecond precision.
		diff := got.CreatedAt.Sub(created)
		if diff < -time.Microsecond || diff > time.Microsecond {
			t.Fatalf("CreatedAt drifted on replace: was %v now %v", created, got.CreatedAt)
		}
	}
	if got.State != store.SandboxSessionStateEvicted {
		t.Fatalf("State after replace: %q", got.State)
	}

	// Delete + idempotent second Delete.
	if err := ss.Delete(ctx, sess.SessionID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := ss.Delete(ctx, sess.SessionID); err != nil {
		t.Fatalf("Delete (second): %v", err)
	}
	if g, _ := ss.Get(ctx, sess.SessionID); g != nil {
		t.Fatalf("Get after Delete: %+v", g)
	}
}

func TestPGSandboxSessionStore_GetByContainerName(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := NewPGSandboxSessionStore(pool, schema)
	sess := seedSandboxSession(t, uuid.New().String())
	if err := ss.Put(ctx, sess); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := ss.GetByContainerName(ctx, sess.ContainerName)
	if err != nil {
		t.Fatalf("GetByContainerName: %v", err)
	}
	if got == nil || got.SessionID != sess.SessionID {
		t.Fatalf("GetByContainerName mismatch: %+v", got)
	}
	if g, _ := ss.GetByContainerName(ctx, "missing"); g != nil {
		t.Fatalf("GetByContainerName missing: %+v", g)
	}
}

func TestPGSandboxSessionStore_ListFilters(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := NewPGSandboxSessionStore(pool, schema)

	userA := uuid.New().String()
	userB := uuid.New().String()

	s1 := seedSandboxSession(t, uuid.New().String())
	s1.CreatedBy = userA
	s1.Pinned = true
	s1.State = store.SandboxSessionStateRunning

	s2 := seedSandboxSession(t, uuid.New().String())
	s2.CreatedBy = userB
	s2.State = store.SandboxSessionStateRunning

	s3 := seedSandboxSession(t, uuid.New().String())
	s3.CreatedBy = userA
	s3.State = store.SandboxSessionStateEvicted

	for _, s := range []*store.SandboxSession{s1, s2, s3} {
		if err := ss.Put(ctx, s); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	all, err := ss.List(ctx, store.SandboxSessionFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List all: %d", len(all))
	}

	running, _ := ss.List(ctx, store.SandboxSessionFilter{State: store.SandboxSessionStateRunning})
	if len(running) != 2 {
		t.Fatalf("List running: %d", len(running))
	}

	byUser, _ := ss.List(ctx, store.SandboxSessionFilter{CreatedBy: userA})
	if len(byUser) != 2 {
		t.Fatalf("List userA: %d", len(byUser))
	}

	pinned := true
	pinnedList, _ := ss.List(ctx, store.SandboxSessionFilter{Pinned: &pinned})
	if len(pinnedList) != 1 || pinnedList[0].SessionID != s1.SessionID {
		t.Fatalf("List pinned: %+v", pinnedList)
	}
}

func TestPGSandboxSessionStore_Mutators(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := NewPGSandboxSessionStore(pool, schema)
	sess := seedSandboxSession(t, uuid.New().String())
	if err := ss.Put(ctx, sess); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := ss.UpdateState(ctx, sess.SessionID, store.SandboxSessionStateEvicting); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := ss.UpdatePorts(ctx, sess.SessionID, []int{80, 443, 3000}); err != nil {
		t.Fatalf("UpdatePorts: %v", err)
	}
	if err := ss.SetBaseDomain(ctx, sess.SessionID, "alt.example"); err != nil {
		t.Fatalf("SetBaseDomain: %v", err)
	}
	if err := ss.SetPinned(ctx, sess.SessionID, true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if err := ss.SetUpperLayer(ctx, sess.SessionID, "layer-abc"); err != nil {
		t.Fatalf("SetUpperLayer: %v", err)
	}

	got, _ := ss.Get(ctx, sess.SessionID)
	if got.State != store.SandboxSessionStateEvicting {
		t.Fatalf("State: %q", got.State)
	}
	if len(got.ExposedPorts) != 3 {
		t.Fatalf("ExposedPorts: %+v", got.ExposedPorts)
	}
	if got.BaseDomain != "alt.example" {
		t.Fatalf("BaseDomain: %q", got.BaseDomain)
	}
	if !got.Pinned {
		t.Fatal("Pinned should be true")
	}
	if got.UpperLayerID != "layer-abc" {
		t.Fatalf("UpperLayerID: %q", got.UpperLayerID)
	}

	// Clear upper layer.
	if err := ss.SetUpperLayer(ctx, sess.SessionID, ""); err != nil {
		t.Fatalf("SetUpperLayer clear: %v", err)
	}
	got, _ = ss.Get(ctx, sess.SessionID)
	if got.UpperLayerID != "" {
		t.Fatalf("UpperLayerID clear: %q", got.UpperLayerID)
	}

	// Missing session errors on mutators.
	if err := ss.UpdateState(ctx, uuid.New().String(), store.SandboxSessionStateRunning); err == nil {
		t.Error("UpdateState on missing should error")
	}
	if err := ss.UpdatePorts(ctx, uuid.New().String(), nil); err == nil {
		t.Error("UpdatePorts on missing should error")
	}
	if err := ss.SetBaseDomain(ctx, uuid.New().String(), "x"); err == nil {
		t.Error("SetBaseDomain on missing should error")
	}
	if err := ss.SetPinned(ctx, uuid.New().String(), true); err == nil {
		t.Error("SetPinned on missing should error")
	}
	if err := ss.SetUpperLayer(ctx, uuid.New().String(), "l"); err == nil {
		t.Error("SetUpperLayer on missing should error")
	}
}

func TestPGSandboxSessionStore_Validation(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ss := NewPGSandboxSessionStore(pool, schema)
	if err := ss.Put(ctx, nil); err == nil {
		t.Error("Put(nil) should error")
	}
	if err := ss.Put(ctx, &store.SandboxSession{}); err == nil {
		t.Error("Put empty should error (missing SessionID)")
	}
	if err := ss.Put(ctx, &store.SandboxSession{SessionID: "x"}); err == nil {
		t.Error("Put without TemplateID should error")
	}
}

// ---------------------------------------------------------------------------
// Ref-count backstop trigger (platform/004) integration test
//
// The trigger is installed DISABLED. This test enables it, verifies it
// correctly bumps and drops ref_count on INSERT / UPDATE OF top_layer_id /
// DELETE, then disables it again. In production the trigger remains off
// and the application is the sole source of ref_count mutations (§7.5).
// ---------------------------------------------------------------------------

func TestPGSandboxRefCountBackstopTrigger(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()

	// Enable the trigger for this test.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE sandbox_templates ENABLE TRIGGER trg_sandbox_templates_ref_backstop`,
	); err != nil {
		t.Fatalf("enable trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`ALTER TABLE sandbox_templates DISABLE TRIGGER trg_sandbox_templates_ref_backstop`)
	})

	ls := NewPGLayerStore(pool)
	ts := NewPGSandboxTemplateStore(pool)

	// Seed two layers for the churn test.
	layer1 := &store.SandboxLayer{LayerID: "l1-" + uuid.NewString(), CephFSPath: "/t/l1", SizeBytes: 100}
	layer2 := &store.SandboxLayer{LayerID: "l2-" + uuid.NewString(), CephFSPath: "/t/l2", SizeBytes: 200}
	if err := ls.PutLayer(ctx, layer1); err != nil {
		t.Fatalf("PutLayer l1: %v", err)
	}
	if err := ls.PutLayer(ctx, layer2); err != nil {
		t.Fatalf("PutLayer l2: %v", err)
	}

	// Seed @base for parent linkage.
	baseID := seedBaseTemplate(t, ctx, ts)

	// INSERT with top_layer_id=layer1 -> layer1.ref_count should become 1.
	tpl := &store.SandboxTemplate{
		Slug:             "t-backstop-" + uuid.NewString(),
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          uuid.NewString(),
		Name:             "Backstop test",
		ParentTemplateID: &baseID,
		TopLayerID:       &layer1.LayerID,
	}
	// pgSandboxTemplateStore.Create does not adjust ref_count itself today
	// (explicit ref-count updates live in higher-level orchestration). With
	// the trigger enabled the INSERT alone should bump layer1.ref_count.
	if err := ts.Create(ctx, tpl); err != nil {
		t.Fatalf("Create template: %v", err)
	}

	got, err := ls.GetLayer(ctx, layer1.LayerID)
	if err != nil {
		t.Fatalf("GetLayer l1: %v", err)
	}
	if got.RefCount != 1 {
		t.Fatalf("after INSERT: layer1 ref_count = %d, want 1", got.RefCount)
	}

	// UPDATE top_layer_id to layer2 -> layer1 -1, layer2 +1.
	// Use raw SQL to bypass the store (which does not support top_layer_id
	// updates today; this is a pure trigger check).
	if _, err := pool.Exec(ctx,
		`UPDATE sandbox_templates SET top_layer_id = $2, updated_at = now() WHERE id = $1::uuid`,
		tpl.ID, layer2.LayerID,
	); err != nil {
		t.Fatalf("UPDATE top_layer_id: %v", err)
	}
	l1After, _ := ls.GetLayer(ctx, layer1.LayerID)
	l2After, _ := ls.GetLayer(ctx, layer2.LayerID)
	if l1After.RefCount != 0 {
		t.Fatalf("after UPDATE: layer1 ref_count = %d, want 0", l1After.RefCount)
	}
	if l2After.RefCount != 1 {
		t.Fatalf("after UPDATE: layer2 ref_count = %d, want 1", l2After.RefCount)
	}

	// DELETE -> layer2 -1.
	if _, err := pool.Exec(ctx,
		`DELETE FROM sandbox_templates WHERE id = $1::uuid`, tpl.ID,
	); err != nil {
		t.Fatalf("DELETE template: %v", err)
	}
	l2Final, _ := ls.GetLayer(ctx, layer2.LayerID)
	if l2Final.RefCount != 0 {
		t.Fatalf("after DELETE: layer2 ref_count = %d, want 0", l2Final.RefCount)
	}
}

// jsonEqual is a small helper that unmarshals both arguments and compares
// the results, ignoring object-key ordering. Used in tests that need to
// assert JSONB round-trips through PG without caring about whitespace.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var ax, bx any
	if err := json.Unmarshal(a, &ax); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bx); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	aJSON, _ := json.Marshal(ax)
	bJSON, _ := json.Marshal(bx)
	return string(aJSON) == string(bJSON)
}

// Silence unused-warning in builds that don't reference jsonEqual; it's
// available to future tests that round-trip JSONB columns.
var _ = jsonEqual
