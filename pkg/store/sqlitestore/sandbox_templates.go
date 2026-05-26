package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/store"
)

// SQLiteSandboxTemplateStore implements store.SandboxTemplateStore backed by SQLite.
type SQLiteSandboxTemplateStore struct {
	db *sql.DB

	// buildMu is a process-level mutex for the build lock (SQLite does not
	// have advisory locks like PG; we use an in-process mutex instead).
	buildMu *sync.Mutex
}

// NewSQLiteSandboxTemplateStore creates a new SQLite-backed sandbox template store.
// The buildMu must be shared across all instances for the same database.
func NewSQLiteSandboxTemplateStore(db *sql.DB, buildMu *sync.Mutex) *SQLiteSandboxTemplateStore {
	return &SQLiteSandboxTemplateStore{db: db, buildMu: buildMu}
}

// Create inserts a new template row.
func (s *SQLiteSandboxTemplateStore) Create(ctx context.Context, tpl *store.SandboxTemplate) error {
	if tpl.ID == "" {
		tpl.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if tpl.CreatedAt.IsZero() {
		tpl.CreatedAt = time.Now().UTC()
	}
	if tpl.UpdatedAt.IsZero() {
		tpl.UpdatedAt = time.Now().UTC()
	}

	// Cycle detection: walk the parent chain.
	if tpl.ParentTemplateID != nil {
		if err := s.detectCycle(ctx, *tpl.ParentTemplateID, tpl.ID); err != nil {
			return err
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sandbox_templates (id, slug, scope, owner_id, purpose, name, description,
		    parent_template_id, top_layer_id, version, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tpl.ID, tpl.Slug, string(tpl.Scope), tpl.OwnerID, string(tpl.Purpose),
		tpl.Name, tpl.Description, tpl.ParentTemplateID, tpl.TopLayerID,
		tpl.Version, tpl.CreatedBy, now, now,
	)
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	return nil
}

// GetByID returns a template by primary key.
func (s *SQLiteSandboxTemplateStore) GetByID(ctx context.Context, id string) (*store.SandboxTemplate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        parent_template_id, top_layer_id, version, created_by, created_at, updated_at
		   FROM sandbox_templates WHERE id = ?`, id)
	return s.scanTemplate(row)
}

// GetBySlug returns the template matching (scope, ownerID, slug).
func (s *SQLiteSandboxTemplateStore) GetBySlug(ctx context.Context, scope store.SandboxTemplateScope, ownerID, slug string) (*store.SandboxTemplate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        parent_template_id, top_layer_id, version, created_by, created_at, updated_at
		   FROM sandbox_templates WHERE scope = ? AND owner_id = ? AND slug = ?`,
		string(scope), ownerID, slug)
	return s.scanTemplate(row)
}

// List returns templates matching the filter.
func (s *SQLiteSandboxTemplateStore) List(ctx context.Context, filter store.SandboxTemplateFilter) ([]*store.SandboxTemplate, error) {
	query := `SELECT id, slug, scope, owner_id, purpose, name, description,
	                  parent_template_id, top_layer_id, version, created_by, created_at, updated_at
	             FROM sandbox_templates WHERE 1=1`
	var args []any

	if filter.Scope != "" {
		query += " AND scope = ?"
		args = append(args, string(filter.Scope))
	}
	if filter.OwnerID != "" {
		query += " AND owner_id = ?"
		args = append(args, filter.OwnerID)
	}
	if filter.Purpose != "" {
		query += " AND purpose = ?"
		args = append(args, string(filter.Purpose))
	}
	query += " ORDER BY scope, slug"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var results []*store.SandboxTemplate
	for rows.Next() {
		tpl, err := s.scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, tpl)
	}
	return results, rows.Err()
}

// Update mutates mutable fields.
func (s *SQLiteSandboxTemplateStore) Update(ctx context.Context, tpl *store.SandboxTemplate) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE sandbox_templates
		    SET name = ?, description = ?, purpose = ?, top_layer_id = ?,
		        version = version + 1, updated_at = ?
		  WHERE id = ?`,
		tpl.Name, tpl.Description, string(tpl.Purpose), tpl.TopLayerID, now, tpl.ID,
	)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("template %s not found", tpl.ID)
	}
	return nil
}

// Delete removes the template row.
func (s *SQLiteSandboxTemplateStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sandbox_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

// Resolve walks the parent chain and returns the ordered list of layer IDs.
func (s *SQLiteSandboxTemplateStore) Resolve(ctx context.Context, id string) (*store.ResolvedTemplateChain, error) {
	var layerIDs []string
	currentID := id

	// Walk up the parent chain (max depth 20 to prevent infinite loops).
	for i := 0; i < 20; i++ {
		row := s.db.QueryRowContext(ctx,
			`SELECT top_layer_id, parent_template_id FROM sandbox_templates WHERE id = ?`, currentID)

		var topLayer *string
		var parentID *string
		if err := row.Scan(&topLayer, &parentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			return nil, fmt.Errorf("resolve template chain: %w", err)
		}

		if topLayer != nil && *topLayer != "" {
			layerIDs = append([]string{*topLayer}, layerIDs...)
		}

		if parentID == nil {
			break
		}
		currentID = *parentID
	}

	return &store.ResolvedTemplateChain{
		TemplateID: id,
		LayerIDs:   layerIDs,
	}, nil
}

// ListRoots returns templates with no parent.
func (s *SQLiteSandboxTemplateStore) ListRoots(ctx context.Context) ([]*store.SandboxTemplate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        parent_template_id, top_layer_id, version, created_by, created_at, updated_at
		   FROM sandbox_templates WHERE parent_template_id IS NULL
		  ORDER BY scope, slug`)
	if err != nil {
		return nil, fmt.Errorf("list roots: %w", err)
	}
	defer rows.Close()

	var results []*store.SandboxTemplate
	for rows.Next() {
		tpl, err := s.scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, tpl)
	}
	return results, rows.Err()
}

// --- @base configuration helpers ---

// GetBaseConfig returns the current @base template's configuration and metadata.
func (s *SQLiteSandboxTemplateStore) GetBaseConfig(ctx context.Context) (*store.BaseConfigInfo, error) {
	var info store.BaseConfigInfo
	var topLayer *string
	var configJSON []byte
	var configuredBy *string
	var configuredAtStr *string
	var updatedAtStr string
	var sizeBytes sql.NullInt64

	err := s.db.QueryRowContext(ctx,
		`SELECT t.top_layer_id, t.base_config, t.configured_by, t.configured_at, t.updated_at,
		        l.size_bytes
		   FROM sandbox_templates t
		   LEFT JOIN sandbox_layers l ON l.layer_id = t.top_layer_id
		  WHERE t.scope = 'global' AND t.slug = 'base' AND t.parent_template_id IS NULL`,
	).Scan(&topLayer, &configJSON, &configuredBy, &configuredAtStr, &updatedAtStr, &sizeBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetBaseConfig: %w", err)
	}

	if topLayer != nil {
		info.LayerID = *topLayer
	}
	info.ConfigJSON = configJSON
	if configuredBy != nil {
		info.ConfiguredBy = *configuredBy
	}
	if configuredAtStr != nil {
		t, _ := time.Parse(time.RFC3339, *configuredAtStr)
		info.ConfiguredAt = &t
	}
	info.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	if sizeBytes.Valid {
		info.SizeBytes = sizeBytes.Int64
	}
	return &info, nil
}

// SetBaseConfig updates the @base template's top_layer_id and configuration.
func (s *SQLiteSandboxTemplateStore) SetBaseConfig(ctx context.Context, newLayerID string, configJSON []byte, configuredBy string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var cfgBy *string
	if configuredBy != "" {
		cfgBy = &configuredBy
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE sandbox_templates
		    SET top_layer_id  = ?,
		        base_config   = ?,
		        configured_by = ?,
		        configured_at = ?,
		        updated_at    = ?,
		        version       = version + 1
		  WHERE scope = 'global' AND slug = 'base' AND parent_template_id IS NULL`,
		newLayerID, configJSON, cfgBy, now, now,
	)
	if err != nil {
		return fmt.Errorf("SetBaseConfig: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("SetBaseConfig: @base template row not found")
	}
	return nil
}

// GetBaseTopLayerID returns the current top_layer_id of @base.
func (s *SQLiteSandboxTemplateStore) GetBaseTopLayerID(ctx context.Context) (string, error) {
	var topLayer *string
	err := s.db.QueryRowContext(ctx,
		`SELECT top_layer_id FROM sandbox_templates
		  WHERE scope = 'global' AND slug = 'base' AND parent_template_id IS NULL`,
	).Scan(&topLayer)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if topLayer == nil {
		return "", nil
	}
	return *topLayer, nil
}

// AcquireBuildLock uses an in-process mutex (SQLite doesn't have advisory locks).
func (s *SQLiteSandboxTemplateStore) AcquireBuildLock(_ context.Context) (bool, func(), error) {
	acquired := s.buildMu.TryLock()
	if !acquired {
		return false, nil, nil
	}
	return true, func() { s.buildMu.Unlock() }, nil
}

// IsBuildInProgress checks if the build lock is held.
func (s *SQLiteSandboxTemplateStore) IsBuildInProgress(_ context.Context) (bool, error) {
	acquired := s.buildMu.TryLock()
	if acquired {
		s.buildMu.Unlock()
		return false, nil
	}
	return true, nil
}

// --- internal helpers ---

func (s *SQLiteSandboxTemplateStore) detectCycle(ctx context.Context, parentID, newID string) error {
	currentID := parentID
	for i := 0; i < 20; i++ {
		if currentID == newID {
			return fmt.Errorf("cycle detected: template %s would create a cycle", newID)
		}
		var nextParent *string
		err := s.db.QueryRowContext(ctx,
			`SELECT parent_template_id FROM sandbox_templates WHERE id = ?`, currentID,
		).Scan(&nextParent)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // reached root
			}
			return err
		}
		if nextParent == nil {
			return nil // reached root
		}
		currentID = *nextParent
	}
	return nil
}

type templateScannable interface {
	Scan(dest ...any) error
}

func (s *SQLiteSandboxTemplateStore) scanTemplate(row templateScannable) (*store.SandboxTemplate, error) {
	var tpl store.SandboxTemplate
	var scope, purpose string
	var parentID, topLayer, createdBy *string
	var createdAtStr, updatedAtStr string

	err := row.Scan(
		&tpl.ID, &tpl.Slug, &scope, &tpl.OwnerID, &purpose,
		&tpl.Name, &tpl.Description, &parentID, &topLayer,
		&tpl.Version, &createdBy, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan template: %w", err)
	}

	tpl.Scope = store.SandboxTemplateScope(scope)
	tpl.Purpose = store.SandboxTemplatePurpose(purpose)
	tpl.ParentTemplateID = parentID
	tpl.TopLayerID = topLayer
	if createdBy != nil {
		tpl.CreatedBy = *createdBy
	}
	tpl.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	tpl.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &tpl, nil
}

func (s *SQLiteSandboxTemplateStore) scanTemplateRow(rows *sql.Rows) (*store.SandboxTemplate, error) {
	var tpl store.SandboxTemplate
	var scope, purpose string
	var parentID, topLayer, createdBy *string
	var createdAtStr, updatedAtStr string

	err := rows.Scan(
		&tpl.ID, &tpl.Slug, &scope, &tpl.OwnerID, &purpose,
		&tpl.Name, &tpl.Description, &parentID, &topLayer,
		&tpl.Version, &createdBy, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, fmt.Errorf("scan template row: %w", err)
	}

	tpl.Scope = store.SandboxTemplateScope(scope)
	tpl.Purpose = store.SandboxTemplatePurpose(purpose)
	tpl.ParentTemplateID = parentID
	tpl.TopLayerID = topLayer
	if createdBy != nil {
		tpl.CreatedBy = *createdBy
	}
	tpl.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	tpl.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &tpl, nil
}

// Compile-time interface check
var _ store.SandboxTemplateStore = (*SQLiteSandboxTemplateStore)(nil)
