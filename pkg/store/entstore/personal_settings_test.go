package entstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	personalent "github.com/schardosin/astonish/ent/personal"
	"github.com/schardosin/astonish/pkg/store"

	_ "modernc.org/sqlite"
)

func newPersonalClient(t *testing.T) *personalent.Client {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "personal.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("pragma: %v", err)
		}
	}

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := personalent.NewClient(personalent.Driver(drv))
	t.Cleanup(func() { client.Close() })

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema create: %v", err)
	}
	return client
}

func TestPersonalSettings_GetEmpty_ReturnsZeroValue(t *testing.T) {
	// Given a fresh store with no rows yet
	client := newPersonalClient(t)
	ps := &personalSettingsStore{client: client, userID: ""}
	ctx := context.Background()

	// When Get is called
	got, err := ps.Get(ctx)

	// Then a zero-value struct (not nil, not error) is returned
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil, want zero-value struct")
	}
	if got.DefaultProvider != "" || got.DefaultModel != "" {
		t.Errorf("Get on empty store = %+v, want zero-value", got)
	}
}

func TestPersonalSettings_SaveGet_RoundTrip(t *testing.T) {
	// Given a fresh store
	client := newPersonalClient(t)
	ps := &personalSettingsStore{client: client, userID: ""}
	ctx := context.Background()

	want := &store.PersonalSettings{
		DefaultProvider: "anthropic",
		DefaultModel:    "claude-sonnet-4.5",
	}

	// When Save is called then Get
	if err := ps.Save(ctx, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := ps.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Then the values round-trip
	if got.DefaultProvider != want.DefaultProvider {
		t.Errorf("DefaultProvider = %q, want %q", got.DefaultProvider, want.DefaultProvider)
	}
	if got.DefaultModel != want.DefaultModel {
		t.Errorf("DefaultModel = %q, want %q", got.DefaultModel, want.DefaultModel)
	}
}

func TestPersonalSettings_Save_Upsert(t *testing.T) {
	// Given a store with a saved row
	client := newPersonalClient(t)
	ps := &personalSettingsStore{client: client, userID: ""}
	ctx := context.Background()

	if err := ps.Save(ctx, &store.PersonalSettings{DefaultProvider: "openai", DefaultModel: "gpt-5"}); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// When Save is called again with different values
	if err := ps.Save(ctx, &store.PersonalSettings{DefaultProvider: "anthropic", DefaultModel: "claude-opus-4"}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	// Then Get returns the latest values (single row upsert, not duplicate insert)
	got, err := ps.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefaultProvider != "anthropic" || got.DefaultModel != "claude-opus-4" {
		t.Errorf("after upsert = %+v, want {anthropic, claude-opus-4}", got)
	}
}

func TestPersonalSettings_Save_ClearRestoresEmpty(t *testing.T) {
	// Given a store with a saved row
	client := newPersonalClient(t)
	ps := &personalSettingsStore{client: client, userID: ""}
	ctx := context.Background()

	if err := ps.Save(ctx, &store.PersonalSettings{DefaultProvider: "openai", DefaultModel: "gpt-5"}); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// When Save is called with empty strings (clear semantics)
	if err := ps.Save(ctx, &store.PersonalSettings{}); err != nil {
		t.Fatalf("clear save: %v", err)
	}

	// Then Get returns empty values
	got, err := ps.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefaultProvider != "" || got.DefaultModel != "" {
		t.Errorf("after clear = %+v, want zero-value", got)
	}
}

func TestPersonalSettings_RouterAccessor(t *testing.T) {
	// Given a personalDataStore
	client := newPersonalClient(t)
	pds := &personalDataStore{client: client, userID: ""}

	// When PersonalSettings() is called
	got := pds.PersonalSettings()

	// Then it returns a non-nil store that satisfies the interface
	if got == nil {
		t.Fatal("PersonalSettings() returned nil")
	}
	ctx := context.Background()
	if _, err := got.Get(ctx); err != nil {
		t.Fatalf("Get via router: %v", err)
	}
}
