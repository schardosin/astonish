package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// SQLiteSandboxLayerStore implements store.LayerStore backed by SQLite.
// Manages the content-addressed layer registry for sandbox containers.
type SQLiteSandboxLayerStore struct {
	db *sql.DB
}

// NewSQLiteSandboxLayerStore creates a new SQLite-backed layer store.
func NewSQLiteSandboxLayerStore(db *sql.DB) *SQLiteSandboxLayerStore {
	return &SQLiteSandboxLayerStore{db: db}
}

func (s *SQLiteSandboxLayerStore) PutLayer(ctx context.Context, layer *store.SandboxLayer) error {
	var parent *string
	if layer.ParentLayer != nil {
		parent = layer.ParentLayer
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sandbox_layers (layer_id, parent_layer, cephfs_path, size_bytes, ref_count, created_by, added_at, last_referenced)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(layer_id) DO NOTHING`,
		layer.LayerID, parent, layer.CephFSPath, layer.SizeBytes,
		layer.RefCount, layer.CreatedBy,
		layer.AddedAt.UTC().Format(time.RFC3339),
		layer.LastReferenced.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("put layer: %w", err)
	}
	return nil
}

func (s *SQLiteSandboxLayerStore) GetLayer(ctx context.Context, layerID string) (*store.SandboxLayer, error) {
	var layer store.SandboxLayer
	var parent sql.NullString
	var addedStr, lastRefStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT layer_id, parent_layer, cephfs_path, size_bytes, ref_count, created_by, added_at, last_referenced
		 FROM sandbox_layers WHERE layer_id = ?`, layerID,
	).Scan(&layer.LayerID, &parent, &layer.CephFSPath, &layer.SizeBytes,
		&layer.RefCount, &layer.CreatedBy, &addedStr, &lastRefStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get layer: %w", err)
	}
	if parent.Valid {
		layer.ParentLayer = &parent.String
	}
	layer.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
	layer.LastReferenced, _ = time.Parse(time.RFC3339, lastRefStr)
	return &layer, nil
}

func (s *SQLiteSandboxLayerStore) IncrementRefCount(ctx context.Context, layerID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sandbox_layers SET ref_count = ref_count + 1, last_referenced = datetime('now')
		 WHERE layer_id = ?`, layerID)
	if err != nil {
		return fmt.Errorf("increment ref count: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("layer %s not found", layerID)
	}
	return nil
}

func (s *SQLiteSandboxLayerStore) DecrementRefCount(ctx context.Context, layerID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sandbox_layers SET ref_count = MAX(0, ref_count - 1) WHERE layer_id = ?`, layerID)
	if err != nil {
		return fmt.Errorf("decrement ref count: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("layer %s not found", layerID)
	}
	return nil
}

func (s *SQLiteSandboxLayerStore) ListUnreferenced(ctx context.Context, grace time.Duration) ([]*store.SandboxLayer, error) {
	cutoff := time.Now().Add(-grace).UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx,
		`SELECT layer_id, parent_layer, cephfs_path, size_bytes, ref_count, created_by, added_at, last_referenced
		 FROM sandbox_layers
		 WHERE ref_count = 0 AND last_referenced < ?
		 ORDER BY added_at ASC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced: %w", err)
	}
	defer rows.Close()

	return scanSandboxLayers(rows)
}

func (s *SQLiteSandboxLayerStore) ListAll(ctx context.Context) ([]*store.SandboxLayer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT layer_id, parent_layer, cephfs_path, size_bytes, ref_count, created_by, added_at, last_referenced
		 FROM sandbox_layers ORDER BY added_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all layers: %w", err)
	}
	defer rows.Close()

	return scanSandboxLayers(rows)
}

func (s *SQLiteSandboxLayerStore) DeleteLayer(ctx context.Context, layerID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sandbox_layers WHERE layer_id = ?`, layerID)
	if err != nil {
		return fmt.Errorf("delete layer: %w", err)
	}
	return nil
}

func scanSandboxLayers(rows *sql.Rows) ([]*store.SandboxLayer, error) {
	var layers []*store.SandboxLayer
	for rows.Next() {
		var layer store.SandboxLayer
		var parent sql.NullString
		var addedStr, lastRefStr string
		if err := rows.Scan(&layer.LayerID, &parent, &layer.CephFSPath, &layer.SizeBytes,
			&layer.RefCount, &layer.CreatedBy, &addedStr, &lastRefStr); err != nil {
			return nil, fmt.Errorf("scan layer: %w", err)
		}
		if parent.Valid {
			layer.ParentLayer = &parent.String
		}
		layer.AddedAt, _ = time.Parse(time.RFC3339, addedStr)
		layer.LastReferenced, _ = time.Parse(time.RFC3339, lastRefStr)
		layers = append(layers, &layer)
	}
	return layers, rows.Err()
}

// Compile-time interface check
var _ store.LayerStore = (*SQLiteSandboxLayerStore)(nil)
