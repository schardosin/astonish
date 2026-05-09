-- Fleet monitor state: per-plan state for GitHub (or other) monitors.
-- Stores seen-issue cursors, retry counts, and poll timestamps.
CREATE TABLE IF NOT EXISTS {{schema}}.fleet_monitor_state (
    plan_key    TEXT PRIMARY KEY,
    state       JSONB NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
