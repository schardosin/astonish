-- 005_seed_base_template.sql
-- Seed the global "@base" layer and template rows.
--
-- The K8s seed Job creates /mnt/astonish-layers/@base/rootfs on the PVC.
-- This migration creates the corresponding platform-DB rows so that:
--   1. sandbox_templates has a global root row (slug='base', scope='global')
--   2. sandbox_layers has the matching layer_id='@base' row
--   3. The template's top_layer_id references the layer, satisfying the FK.
--
-- Both INSERTs use ON CONFLICT DO NOTHING so the migration is idempotent
-- (safe to re-run on existing deployments that already have these rows).
--
-- The @base layer has ref_count=1 from birth (the template references it).
-- Its size_bytes=0 because the actual size is unknown at migration time
-- (it depends on the sandbox base image version); this is acceptable since
-- size_bytes is informational only for GC heuristics.

-- Layer row first (FK target for the template).
INSERT INTO sandbox_layers (layer_id, parent_layer, cephfs_path, size_bytes, ref_count)
VALUES ('@base', NULL, '/mnt/astonish-layers/@base', 0, 1)
ON CONFLICT (layer_id) DO NOTHING;

-- Template row: global scope, slug='base', no parent (the DAG root).
-- Uses a well-known UUID so that other templates can reference it as
-- parent_template_id without a lookup. The UUID is deterministic:
-- UUID v5 of "astonish:base-template" in the DNS namespace.
INSERT INTO sandbox_templates (id, slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version)
VALUES (
    'a0000000-0000-4000-8000-000000000001',
    'base',
    'global',
    '',
    '',
    'Base',
    'Deployment-wide base sandbox image',
    NULL,
    '@base',
    1
)
ON CONFLICT (scope, owner_id, slug) DO NOTHING;
