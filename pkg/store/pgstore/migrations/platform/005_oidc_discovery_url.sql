-- Migration 005: Add discovery_url to oidc_providers for providers where
-- the OIDC discovery base URL differs from the issuer claim (e.g. SAP BTP XSUAA).
ALTER TABLE oidc_providers ADD COLUMN IF NOT EXISTS discovery_url TEXT NOT NULL DEFAULT '';
