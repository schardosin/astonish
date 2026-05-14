-- 004_sandbox_ref_count_triggers.sql
-- Phase A: ref-count backstop triggers on sandbox_templates.
--
-- See docs/architecture/sandbox-backends.md §5.12 (GC lifecycle) and §7.5
-- (ref-count maintenance). Application code already updates
-- sandbox_layers.ref_count explicitly in the same transaction as template
-- mutations (via pkg/store/pgstore/sandbox_templates.go). This trigger is a
-- defense-in-depth mechanism that compensates for application bugs where a
-- template row changes top_layer_id without a matching application-level
-- ref-count adjustment.
--
-- Why templates only, not sessions:
-- ---------------------------------
-- platform.sandbox_layers lives in the platform DB. sandbox_templates also
-- lives in the platform DB, so a BEFORE/AFTER trigger can mutate
-- sandbox_layers.ref_count in the same transaction — perfect for a
-- transactional backstop.
--
-- {team_schema}.sandbox_sessions lives in a team DB (astonish_org_<slug>),
-- which is a different database. A PG trigger cannot cross database
-- boundaries, so we CANNOT install an analogous trigger for sessions.
-- Session-side ref-count discipline remains the application's
-- responsibility; the pgstore SandboxSessionStore.SetUpperLayer and
-- Delete call sites must adjust platform.sandbox_layers.ref_count via the
-- platform pool in a surrounding coordinating transaction. The
-- architecture reference (§7.5) notes this trade-off; we do not emulate
-- it with postgres_fdw today to keep the operational surface small.
--
-- Personal mode never applies this migration (it uses filestore, which has
-- no layer store to count refs on).

-- ----------------------------------------------------------------------------
-- Helper: bump or drop a single layer's ref_count. Always guarded by
-- layer_id IS NOT NULL; ref_count is subject to the CHECK (ref_count >= 0)
-- constraint on sandbox_layers, so a buggy drop that would go negative
-- raises a constraint violation instead of silently corrupting state.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sandbox_layers_bump_ref(layer TEXT, delta INTEGER) RETURNS void AS $$
BEGIN
    IF layer IS NULL THEN
        RETURN;
    END IF;
    UPDATE sandbox_layers
       SET ref_count       = ref_count + delta,
           last_referenced = CASE WHEN delta > 0 THEN now() ELSE last_referenced END
     WHERE layer_id = layer;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'sandbox_layers_bump_ref: layer % not found', layer;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- ----------------------------------------------------------------------------
-- Trigger body: detect INSERT / UPDATE OF top_layer_id / DELETE and adjust
-- ref_count. The trigger is idempotent with respect to the application's
-- explicit UPDATE on sandbox_layers: both the app and this trigger bump
-- ref_count by 1 per referencing row, so if both run, the count would be
-- 2x per ref. To avoid double-counting we keep the trigger DISABLED BY
-- DEFAULT and provide it as a diagnostic that deployments MAY enable in
-- staging to surface application bugs.
--
-- Rationale (decision recorded in §7.5): the application path is the
-- primary source of truth; the trigger exists so that operators can
-- temporarily enable it in a test environment, run the suite, and confirm
-- that application code and trigger agree. Enabling in production would
-- double-count and corrupt ref_counts.
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sandbox_templates_ref_count_backstop() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM sandbox_layers_bump_ref(NEW.top_layer_id, 1);
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        IF NEW.top_layer_id IS DISTINCT FROM OLD.top_layer_id THEN
            PERFORM sandbox_layers_bump_ref(OLD.top_layer_id, -1);
            PERFORM sandbox_layers_bump_ref(NEW.top_layer_id, 1);
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        PERFORM sandbox_layers_bump_ref(OLD.top_layer_id, -1);
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Install the trigger DISABLED. Operators enable it for diagnostic runs via:
--   ALTER TABLE sandbox_templates ENABLE TRIGGER trg_sandbox_templates_ref_backstop;
-- and disable again afterward. Production runs with the trigger OFF.
DROP TRIGGER IF EXISTS trg_sandbox_templates_ref_backstop ON sandbox_templates;
CREATE TRIGGER trg_sandbox_templates_ref_backstop
    AFTER INSERT OR UPDATE OF top_layer_id OR DELETE ON sandbox_templates
    FOR EACH ROW EXECUTE FUNCTION sandbox_templates_ref_count_backstop();

ALTER TABLE sandbox_templates
    DISABLE TRIGGER trg_sandbox_templates_ref_backstop;
