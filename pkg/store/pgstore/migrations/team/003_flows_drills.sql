-- Add yaml_content column to flows table for raw YAML storage.
-- The existing definition JSONB column holds parsed structure;
-- yaml_content preserves the original YAML with formatting/comments.

ALTER TABLE {{schema}}.flows ADD COLUMN IF NOT EXISTS yaml_content TEXT;

-- Add type column to flows table for efficient type-based filtering.
-- Values: '' (regular flow), 'drill_suite', 'drill', 'test_suite', 'test'.
ALTER TABLE {{schema}}.flows ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_flows_type
    ON {{schema}}.flows(type) WHERE type != '';

-- Add yaml_content column to fleet_plans table for raw YAML storage.
ALTER TABLE {{schema}}.fleet_plans ADD COLUMN IF NOT EXISTS yaml_content TEXT;

-- Add drill_reports table for team-scoped drill test results.
CREATE TABLE IF NOT EXISTS {{schema}}.drill_reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suite           TEXT NOT NULL,
    status          TEXT NOT NULL,
    summary         TEXT DEFAULT '',
    duration_ms     BIGINT DEFAULT 0,
    report_data     JSONB NOT NULL,      -- full SuiteReport JSON
    started_at      TIMESTAMPTZ NOT NULL,
    finished_at     TIMESTAMPTZ NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_drill_reports_suite
    ON {{schema}}.drill_reports(suite);
CREATE INDEX IF NOT EXISTS idx_drill_reports_created
    ON {{schema}}.drill_reports(created_at DESC);
