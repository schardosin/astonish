-- 003_sandbox_templates_and_layers.sql
-- Phase A (Round 2): sandbox template DAG + content-addressed layer store.
-- These tables are deployment-wide (platform schema) because the "@base"
-- template is a global singleton shared across orgs, and layer dedup works
-- best when the layer registry spans every template and evicted session.
--
-- See docs/architecture/sandbox-backends.md §7 for placement rationale and
-- §3.8/§3.9 for the scope/DAG semantics.
--
-- Personal mode never applies this migration (it uses filestore, which
-- returns ErrUnsupported for layer operations).

-- ----------------------------------------------------------------------------
-- sandbox_layers: content-addressed layer registry.
-- LayerID is the SHA-256 (hex) of the canonical uncompressed tar.
-- The actual bytes live on CephFS at cephfs_path; this table is the index.
-- ref_count is maintained by application code in the same transaction as
-- template/session mutations that add or remove a reference.
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sandbox_layers (
    layer_id        TEXT PRIMARY KEY,                       -- sha256 hex
    parent_layer    TEXT REFERENCES sandbox_layers(layer_id),
    cephfs_path     TEXT NOT NULL,                          -- e.g., /mnt/astonish-layers/<layer_id>
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    ref_count       INTEGER NOT NULL DEFAULT 0
                    CHECK (ref_count >= 0),
    created_by      UUID,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_referenced TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GC candidate query: ref_count=0 AND added_at < now() - grace.
CREATE INDEX IF NOT EXISTS idx_sandbox_layers_unreferenced
    ON sandbox_layers(added_at)
    WHERE ref_count = 0;

CREATE INDEX IF NOT EXISTS idx_sandbox_layers_parent
    ON sandbox_layers(parent_layer)
    WHERE parent_layer IS NOT NULL;

-- ----------------------------------------------------------------------------
-- sandbox_templates: template DAG.
-- scope: 'global' | 'org' | 'team' | 'personal'
-- owner_id: semantics depend on scope (see interface doc):
--   global   → empty string ''
--   org      → organizations.id (UUID, cast to text for simplicity)
--   team     → teams.id
--   personal → users.id
-- purpose: optional discriminator within a scope; '' (normal) or 'fleet'.
-- parent_template_id: NULL only for the global "@base" root; every other
-- template inherits from exactly one parent, forming a DAG.
-- top_layer_id: the most-recent layer in this template's chain; nullable
-- because a template MAY be a pure alias of its parent (top_layer_id=NULL).
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sandbox_templates (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                 TEXT NOT NULL,
    scope                TEXT NOT NULL
                         CHECK (scope IN ('global', 'org', 'team', 'personal')),
    owner_id             TEXT NOT NULL DEFAULT '',
    purpose              TEXT NOT NULL DEFAULT ''
                         CHECK (purpose IN ('', 'fleet')),
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    parent_template_id   UUID REFERENCES sandbox_templates(id) ON DELETE RESTRICT,
    top_layer_id         TEXT REFERENCES sandbox_layers(layer_id) ON DELETE RESTRICT,
    version              INTEGER NOT NULL DEFAULT 1,
    created_by           UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Uniqueness: slug unique within (scope, owner_id).
    UNIQUE (scope, owner_id, slug),

    -- Only "@base" may have no parent, and it MUST be global.
    CONSTRAINT sandbox_templates_root_is_base
        CHECK (
            parent_template_id IS NOT NULL
            OR (scope = 'global' AND slug = 'base')
        )
);

CREATE INDEX IF NOT EXISTS idx_sandbox_templates_scope_owner
    ON sandbox_templates(scope, owner_id);

CREATE INDEX IF NOT EXISTS idx_sandbox_templates_parent
    ON sandbox_templates(parent_template_id)
    WHERE parent_template_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sandbox_templates_top_layer
    ON sandbox_templates(top_layer_id)
    WHERE top_layer_id IS NOT NULL;

-- ----------------------------------------------------------------------------
-- Cycle-detection trigger: a template MUST NOT reach itself via
-- parent_template_id. Checked on INSERT and UPDATE of parent_template_id.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sandbox_templates_check_no_cycle() RETURNS trigger AS $$
DECLARE
    current_id UUID;
    depth      INTEGER := 0;
BEGIN
    IF NEW.parent_template_id IS NULL THEN
        RETURN NEW;
    END IF;

    current_id := NEW.parent_template_id;
    WHILE current_id IS NOT NULL LOOP
        IF current_id = NEW.id THEN
            RAISE EXCEPTION
                'cycle detected in sandbox_templates parent chain at template %',
                NEW.id;
        END IF;
        depth := depth + 1;
        IF depth > 100 THEN
            RAISE EXCEPTION
                'sandbox_templates parent chain exceeds depth 100 (possible cycle)';
        END IF;
        SELECT parent_template_id INTO current_id
          FROM sandbox_templates
         WHERE id = current_id;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sandbox_templates_no_cycle ON sandbox_templates;
CREATE TRIGGER trg_sandbox_templates_no_cycle
    BEFORE INSERT OR UPDATE OF parent_template_id ON sandbox_templates
    FOR EACH ROW EXECUTE FUNCTION sandbox_templates_check_no_cycle();
