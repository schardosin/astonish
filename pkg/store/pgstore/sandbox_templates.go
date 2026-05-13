package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgSandboxTemplateStore is the platform-scoped template DAG implementation.
// Rows live in the platform database's public.sandbox_templates table (see
// migration platform/003). Layer rows live in public.sandbox_layers.
type pgSandboxTemplateStore struct {
	pool *pgxpool.Pool
}

// NewPGSandboxTemplateStore constructs a template store bound to the platform
// connection pool. The caller must pass a platform pool (see PGStore.poolMgr
// PlatformPool()) since sandbox_templates lives in the platform database.
func NewPGSandboxTemplateStore(pool *pgxpool.Pool) store.SandboxTemplateStore {
	return &pgSandboxTemplateStore{pool: pool}
}

func (s *pgSandboxTemplateStore) Create(ctx context.Context, tpl *store.SandboxTemplate) error {
	if tpl == nil {
		return errors.New("sandbox template is nil")
	}
	if tpl.Slug == "" {
		return errors.New("sandbox template slug is required")
	}
	if !validScope(tpl.Scope) {
		return fmt.Errorf("invalid sandbox template scope %q", tpl.Scope)
	}
	if tpl.ID == "" {
		// Let PG generate the UUID; return via RETURNING.
		row := s.pool.QueryRow(ctx,
			`INSERT INTO sandbox_templates
			   (slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version, created_by)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE(NULLIF($9,0),1),NULLIF($10,'')::uuid)
			 RETURNING id, created_at, updated_at`,
			tpl.Slug, string(tpl.Scope), tpl.OwnerID, string(tpl.Purpose), tpl.Name, tpl.Description,
			parentArg(tpl.ParentTemplateID), topLayerArg(tpl.TopLayerID), tpl.Version, tpl.CreatedBy,
		)
		return row.Scan(&tpl.ID, &tpl.CreatedAt, &tpl.UpdatedAt)
	}

	row := s.pool.QueryRow(ctx,
		`INSERT INTO sandbox_templates
		   (id, slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE(NULLIF($10,0),1),NULLIF($11,'')::uuid)
		 RETURNING created_at, updated_at`,
		tpl.ID, tpl.Slug, string(tpl.Scope), tpl.OwnerID, string(tpl.Purpose), tpl.Name, tpl.Description,
		parentArg(tpl.ParentTemplateID), topLayerArg(tpl.TopLayerID), tpl.Version, tpl.CreatedBy,
	)
	return row.Scan(&tpl.CreatedAt, &tpl.UpdatedAt)
}

func (s *pgSandboxTemplateStore) GetByID(ctx context.Context, id string) (*store.SandboxTemplate, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        COALESCE(parent_template_id::text, ''), top_layer_id, version,
		        COALESCE(created_by::text, ''), created_at, updated_at
		   FROM sandbox_templates
		  WHERE id = $1`,
		id,
	)
	return scanSandboxTemplate(row)
}

func (s *pgSandboxTemplateStore) GetBySlug(ctx context.Context, scope store.SandboxTemplateScope, ownerID, slug string) (*store.SandboxTemplate, error) {
	if !validScope(scope) {
		return nil, fmt.Errorf("invalid sandbox template scope %q", scope)
	}
	row := s.pool.QueryRow(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        COALESCE(parent_template_id::text, ''), top_layer_id, version,
		        COALESCE(created_by::text, ''), created_at, updated_at
		   FROM sandbox_templates
		  WHERE scope = $1 AND owner_id = $2 AND slug = $3`,
		string(scope), ownerID, slug,
	)
	return scanSandboxTemplate(row)
}

func (s *pgSandboxTemplateStore) List(ctx context.Context, filter store.SandboxTemplateFilter) ([]*store.SandboxTemplate, error) {
	// Build dynamic WHERE with up to three filter dimensions.
	conds := []string{}
	args := []any{}
	if filter.Scope != "" {
		if !validScope(filter.Scope) {
			return nil, fmt.Errorf("invalid sandbox template scope %q", filter.Scope)
		}
		args = append(args, string(filter.Scope))
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)))
	}
	if filter.OwnerID != "" {
		args = append(args, filter.OwnerID)
		conds = append(conds, fmt.Sprintf("owner_id = $%d", len(args)))
	}
	if filter.Purpose != "" {
		args = append(args, string(filter.Purpose))
		conds = append(conds, fmt.Sprintf("purpose = $%d", len(args)))
	}

	query := `SELECT id, slug, scope, owner_id, purpose, name, description,
	                 COALESCE(parent_template_id::text, ''), top_layer_id, version,
	                 COALESCE(created_by::text, ''), created_at, updated_at
	            FROM sandbox_templates`
	if len(conds) > 0 {
		query += " WHERE "
		for i, c := range conds {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}
	query += " ORDER BY scope, slug"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.SandboxTemplate
	for rows.Next() {
		tpl, err := scanSandboxTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, rows.Err()
}

func (s *pgSandboxTemplateStore) Update(ctx context.Context, tpl *store.SandboxTemplate) error {
	if tpl == nil || tpl.ID == "" {
		return errors.New("sandbox template ID is required for update")
	}
	// Scope, OwnerID, ParentTemplateID are immutable. We do not include them
	// in the SET clause; if a caller mutated them in memory they are ignored.
	ct, err := s.pool.Exec(ctx,
		`UPDATE sandbox_templates
		    SET name         = $2,
		        description  = $3,
		        purpose      = $4,
		        top_layer_id = $5,
		        version      = $6,
		        updated_at   = now()
		  WHERE id = $1`,
		tpl.ID, tpl.Name, tpl.Description, string(tpl.Purpose),
		topLayerArg(tpl.TopLayerID), tpl.Version,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("sandbox template %s not found", tpl.ID)
	}
	return nil
}

func (s *pgSandboxTemplateStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sandbox_templates WHERE id = $1`, id)
	return err
}

// Resolve walks parent_template_id from the given template toward the root,
// collecting non-NULL top_layer_id values. The returned chain is ordered
// oldest-first (root ancestor's layer is chain[0]; the template's own
// top_layer_id, if any, is chain[len-1]).
func (s *pgSandboxTemplateStore) Resolve(ctx context.Context, id string) (*store.ResolvedTemplateChain, error) {
	// Recursive CTE walks the parent chain. We ORDER BY depth DESC so the
	// root ancestor is first in the resulting array, then filter out NULL
	// top_layer_ids.
	rows, err := s.pool.Query(ctx,
		`WITH RECURSIVE chain AS (
		   SELECT id, parent_template_id, top_layer_id, 0 AS depth
		     FROM sandbox_templates
		    WHERE id = $1
		   UNION ALL
		   SELECT t.id, t.parent_template_id, t.top_layer_id, c.depth + 1
		     FROM sandbox_templates t
		     JOIN chain c ON t.id = c.parent_template_id
		 )
		 SELECT top_layer_id
		   FROM chain
		  WHERE top_layer_id IS NOT NULL
		  ORDER BY depth DESC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var layerIDs []string
	for rows.Next() {
		var layerID string
		if err := rows.Scan(&layerID); err != nil {
			return nil, err
		}
		layerIDs = append(layerIDs, layerID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Verify the template actually exists — the CTE returns zero rows both
	// for an absent template and for a template with no layers in its chain.
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sandbox_templates WHERE id = $1)`, id,
	).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return &store.ResolvedTemplateChain{TemplateID: id, LayerIDs: layerIDs}, nil
}

func (s *pgSandboxTemplateStore) ListRoots(ctx context.Context) ([]*store.SandboxTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, scope, owner_id, purpose, name, description,
		        COALESCE(parent_template_id::text, ''), top_layer_id, version,
		        COALESCE(created_by::text, ''), created_at, updated_at
		   FROM sandbox_templates
		  WHERE parent_template_id IS NULL
		  ORDER BY slug`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.SandboxTemplate
	for rows.Next() {
		tpl, err := scanSandboxTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, rows.Err()
}

// --- helpers ---------------------------------------------------------------

// scanSandboxTemplate works with both pgx.Row and pgx.Rows via the package-
// local scannable interface (defined in platform_stores.go).
func scanSandboxTemplate(r scannable) (*store.SandboxTemplate, error) {
	var (
		tpl       store.SandboxTemplate
		parentStr string
		topLayer  *string
	)
	err := r.Scan(
		&tpl.ID, &tpl.Slug, &tpl.Scope, &tpl.OwnerID, &tpl.Purpose, &tpl.Name, &tpl.Description,
		&parentStr, &topLayer, &tpl.Version, &tpl.CreatedBy, &tpl.CreatedAt, &tpl.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if parentStr != "" {
		parent := parentStr
		tpl.ParentTemplateID = &parent
	}
	tpl.TopLayerID = topLayer
	return &tpl, nil
}

func validScope(scope store.SandboxTemplateScope) bool {
	switch scope {
	case store.SandboxTemplateScopeGlobal,
		store.SandboxTemplateScopeOrg,
		store.SandboxTemplateScopeTeam,
		store.SandboxTemplateScopePersonal:
		return true
	}
	return false
}

// parentArg converts a *string template ID to a value suitable for PG's UUID
// column. pgx accepts a string or nil.
func parentArg(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

// topLayerArg converts *string layer ID to a value suitable for PG's TEXT
// nullable column.
func topLayerArg(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
