-- Migration 004: Remove static org/team routing columns from user_channels.
-- Routing is now dynamic per-message (routing hint > persistent pref > first-org/first-team).
ALTER TABLE user_channels DROP COLUMN IF EXISTS default_org_slug;
ALTER TABLE user_channels DROP COLUMN IF EXISTS default_team_slug;
