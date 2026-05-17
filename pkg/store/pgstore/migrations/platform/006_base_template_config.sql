-- 006_base_template_config.sql
-- Adds a JSONB column to sandbox_templates for storing the rendered
-- BaseConfig that produced the @base layer. This allows the platform admin
-- UI to pre-fill the configuration form from the current state and provides
-- an audit trail of what was installed.
--
-- Only the @base row (slug='base', scope='global') typically has a non-NULL
-- value here. All other templates leave this column NULL.

ALTER TABLE sandbox_templates
  ADD COLUMN IF NOT EXISTS base_config JSONB;

-- Also add a built_by column so we can track which admin triggered the build.
-- Nullable for backward compat with existing rows.
ALTER TABLE sandbox_templates
  ADD COLUMN IF NOT EXISTS configured_by UUID;

ALTER TABLE sandbox_templates
  ADD COLUMN IF NOT EXISTS configured_at TIMESTAMPTZ;
