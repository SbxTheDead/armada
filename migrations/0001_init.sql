-- 0001_init.sql — initial schema for the Armada control plane.
--
-- Design notes:
--   * Every tenant-scoped table carries tenant_id and is indexed on it for
--     multi-tenant isolation and cheap per-tenant queries.
--   * Secrets (enrollment tokens, agent API keys) are stored only as SHA-256
--     hashes; plaintext is shown to the operator/agent exactly once.
--   * Telemetry (heartbeats) is append-only and time-partitioned in production;
--     this initial migration keeps a simple table plus a "latest" materialized
--     view pattern via a covering index.
--
-- Apply with any migration runner (golang-migrate, goose, dbmate) or psql.

BEGIN;

CREATE TABLE tenants (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE systems (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    fqdn          TEXT NOT NULL,
    project       TEXT NOT NULL DEFAULT '',
    region        TEXT NOT NULL DEFAULT '',
    environment   TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    labels        JSONB  NOT NULL DEFAULT '{}',
    arch          TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    lifecycle     TEXT NOT NULL DEFAULT 'pending',
    health        TEXT NOT NULL DEFAULT 'unknown',
    last_check_in TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT systems_fqdn_unique_per_tenant UNIQUE (tenant_id, fqdn)
);

CREATE INDEX idx_systems_tenant        ON systems (tenant_id);
CREATE INDEX idx_systems_tenant_region ON systems (tenant_id, region);
CREATE INDEX idx_systems_tenant_health ON systems (tenant_id, health);
CREATE INDEX idx_systems_tags          ON systems USING GIN (tags);
CREATE INDEX idx_systems_labels        ON systems USING GIN (labels);

CREATE TABLE enrollment_tokens (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    system_id   TEXT REFERENCES systems(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,      -- sha256 hex of the one-time secret
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_enrollment_tokens_tenant ON enrollment_tokens (tenant_id);

CREATE TABLE agent_identities (
    system_id    TEXT PRIMARY KEY REFERENCES systems(id) ON DELETE CASCADE,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    api_key_hash TEXT NOT NULL UNIQUE,     -- sha256 hex of the bearer key
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_identities_key ON agent_identities (api_key_hash);

CREATE TABLE heartbeats (
    id             BIGSERIAL PRIMARY KEY,
    system_id      TEXT NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    agent_version  TEXT NOT NULL DEFAULT '',
    uptime_seconds BIGINT NOT NULL DEFAULT 0,
    metrics        JSONB  NOT NULL DEFAULT '{}',
    problem        BOOLEAN NOT NULL DEFAULT false,
    message        TEXT NOT NULL DEFAULT '',
    sent_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Fast "latest heartbeat per system" lookups.
CREATE INDEX idx_heartbeats_system_time ON heartbeats (system_id, sent_at DESC);

CREATE TABLE inventory_snapshots (
    system_id    TEXT PRIMARY KEY REFERENCES systems(id) ON DELETE CASCADE,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    snapshot     JSONB NOT NULL
);

COMMIT;
