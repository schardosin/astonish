package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgLayerStore backs store.LayerStore with the platform.sandbox_layers table.
// Ref-count discipline (see docs/architecture/sandbox-backends.md §5.12) is
// enforced by transactional UPDATEs; the GC reconciler queries for
// ref_count=0 + grace-period-elapsed candidates.
type pgLayerStore struct {
	pool *pgxpool.Pool
}

// NewPGLayerStore constructs a layer store bound to the platform connection
// pool. Callers must pass a platform pool (layers live in the platform DB).
func NewPGLayerStore(pool *pgxpool.Pool) store.LayerStore {
	return &pgLayerStore{pool: pool}
}

// PutLayer is idempotent: a duplicate LayerID is a content-address collision
// and silently succeeds (layers are content-addressed by construction).
// Callers MUST call IncrementRefCount separately to record a new reference.
func (s *pgLayerStore) PutLayer(ctx context.Context, layer *store.SandboxLayer) error {
	if layer == nil {
		return errors.New("layer is nil")
	}
	if layer.LayerID == "" {
		return errors.New("layer ID is required")
	}
	if layer.CephFSPath == "" {
		return errors.New("layer CephFSPath is required")
	}
	var parent any
	if layer.ParentLayer != nil && *layer.ParentLayer != "" {
		parent = *layer.ParentLayer
	}
	var createdBy any
	if layer.CreatedBy != "" {
		createdBy = layer.CreatedBy
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sandbox_layers
		   (layer_id, parent_layer, cephfs_path, size_bytes, ref_count, created_by)
		 VALUES ($1, $2, $3, $4, 0, $5)
		 ON CONFLICT (layer_id) DO NOTHING`,
		layer.LayerID, parent, layer.CephFSPath, layer.SizeBytes, createdBy,
	)
	return err
}

func (s *pgLayerStore) GetLayer(ctx context.Context, layerID string) (*store.SandboxLayer, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT layer_id, parent_layer, cephfs_path, size_bytes, ref_count,
		        COALESCE(created_by::text, ''), added_at, last_referenced
		   FROM sandbox_layers
		  WHERE layer_id = $1`,
		layerID,
	)
	layer := &store.SandboxLayer{}
	var parent *string
	err := row.Scan(
		&layer.LayerID, &parent, &layer.CephFSPath, &layer.SizeBytes, &layer.RefCount,
		&layer.CreatedBy, &layer.AddedAt, &layer.LastReferenced,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	layer.ParentLayer = parent
	return layer, nil
}

func (s *pgLayerStore) IncrementRefCount(ctx context.Context, layerID string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE sandbox_layers
		    SET ref_count       = ref_count + 1,
		        last_referenced = now()
		  WHERE layer_id = $1`,
		layerID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("layer %s not found", layerID)
	}
	return nil
}

// DecrementRefCount atomically decreases ref_count by 1 and errors if it
// would go negative (which indicates a bookkeeping bug — a caller called
// Decrement without a prior Increment).
func (s *pgLayerStore) DecrementRefCount(ctx context.Context, layerID string) error {
	// The CHECK (ref_count >= 0) constraint on the column catches negatives
	// in the DB, but we want a targeted error message rather than a generic
	// constraint violation. Do a guarded UPDATE.
	ct, err := s.pool.Exec(ctx,
		`UPDATE sandbox_layers
		    SET ref_count = ref_count - 1
		  WHERE layer_id = $1 AND ref_count > 0`,
		layerID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		// Either the layer doesn't exist or ref_count is already 0.
		// Distinguish for a useful error.
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM sandbox_layers WHERE layer_id = $1)`,
			layerID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("layer %s not found", layerID)
		}
		return fmt.Errorf("layer %s ref_count already at zero", layerID)
	}
	return nil
}

func (s *pgLayerStore) ListUnreferenced(ctx context.Context, grace time.Duration) ([]*store.SandboxLayer, error) {
	if grace < 0 {
		grace = 0
	}
	rows, err := s.pool.Query(ctx,
		`SELECT layer_id, parent_layer, cephfs_path, size_bytes, ref_count,
		        COALESCE(created_by::text, ''), added_at, last_referenced
		   FROM sandbox_layers
		  WHERE ref_count = 0
		    AND added_at < now() - $1::interval
		  ORDER BY size_bytes ASC`,
		fmt.Sprintf("%d seconds", int64(grace.Seconds())),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.SandboxLayer
	for rows.Next() {
		layer := &store.SandboxLayer{}
		var parent *string
		if err := rows.Scan(
			&layer.LayerID, &parent, &layer.CephFSPath, &layer.SizeBytes, &layer.RefCount,
			&layer.CreatedBy, &layer.AddedAt, &layer.LastReferenced,
		); err != nil {
			return nil, err
		}
		layer.ParentLayer = parent
		out = append(out, layer)
	}
	return out, rows.Err()
}

func (s *pgLayerStore) DeleteLayer(ctx context.Context, layerID string) error {
	// Reject deletion of referenced layers to surface bookkeeping bugs.
	var refCount int
	err := s.pool.QueryRow(ctx,
		`SELECT ref_count FROM sandbox_layers WHERE layer_id = $1`, layerID,
	).Scan(&refCount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("layer %s not found", layerID)
		}
		return err
	}
	if refCount > 0 {
		return fmt.Errorf("layer %s has ref_count=%d, refusing delete", layerID, refCount)
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM sandbox_layers WHERE layer_id = $1`, layerID)
	return err
}
