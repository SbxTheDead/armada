-- 0002_join_tokens.sql — reusable join tokens + stable machine identity.
--
-- Adds zero-touch onboarding: one reusable, tenant-scoped key binds many
-- devices, each deduplicated by a stable machine_id so re-installs/reboots/IP
-- changes never create duplicates.

BEGIN;

-- Stable per-host identifier for dedupe (empty for operator-registered systems).
ALTER TABLE systems ADD COLUMN machine_id TEXT NOT NULL DEFAULT '';

-- A device is uniquely one machine within a tenant. Partial unique index so the
-- many rows with an empty machine_id (manual registrations) don't collide.
CREATE UNIQUE INDEX idx_systems_machine_id
    ON systems (tenant_id, machine_id)
    WHERE machine_id <> '';

CREATE TABLE join_tokens (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL UNIQUE,      -- sha256 hex of the reusable key

    -- Grouping presets applied to every device that joins with this key.
    project     TEXT NOT NULL DEFAULT '',
    region      TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    provider    TEXT NOT NULL DEFAULT '',
    tags        TEXT[] NOT NULL DEFAULT '{}',

    approval    TEXT NOT NULL DEFAULT 'auto',  -- 'auto' | 'manual'
    max_uses    INTEGER NOT NULL DEFAULT 0,    -- 0 = unlimited
    uses        INTEGER NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ,                   -- NULL = never
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_join_tokens_tenant ON join_tokens (tenant_id);

COMMIT;
