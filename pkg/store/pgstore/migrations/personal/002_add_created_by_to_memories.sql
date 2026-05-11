-- Add created_by column to personal memories for ownership tracking.
-- In personal schema this is technically always the user themselves,
-- but having the column ensures consistent query patterns across all tiers.

ALTER TABLE {{schema}}.memories ADD COLUMN IF NOT EXISTS created_by UUID;
