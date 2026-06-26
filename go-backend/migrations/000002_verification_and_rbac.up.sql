-- ============================================================================
-- Migration 000002 — Verification workflow, file distribution/locking,
-- per-tenant RBAC, and analytics event sourcing.
--
-- Written to be idempotent (IF NOT EXISTS / IF EXISTS) so it can be applied on
-- top of an already-running database as well as a fresh one created from 000001.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 0. Repair known schema drift between 000001 and the live database.
--    The Go repository and Python worker both read/write these columns, but
--    they were hand-added to the running DB and never made it back into the
--    init migration — so a DB built purely from 000001 is missing them.
-- ----------------------------------------------------------------------------
ALTER TABLE extracted_cells ADD COLUMN IF NOT EXISTS page_number INTEGER NOT NULL DEFAULT 1;
ALTER TABLE documents       ADD COLUMN IF NOT EXISTS progress_percentage INTEGER NOT NULL DEFAULT 0;

-- ----------------------------------------------------------------------------
-- 1. Make roles tenant-scoped so every tenant can build and edit its own roles
--    independently, while a set of shared "system" roles remains available to
--    all tenants (tenant_id IS NULL, is_system = TRUE).
-- ----------------------------------------------------------------------------
ALTER TABLE roles ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
ALTER TABLE roles ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT FALSE;

-- Replace the global-unique role name with per-tenant uniqueness. NULL tenant_id
-- (system roles) collapses to the all-zero UUID so names stay globally unique
-- among system roles, while each tenant gets its own namespace.
ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_roles_tenant_name
    ON roles (COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid), name);

-- ----------------------------------------------------------------------------
-- 2. Verification assignment / lock / live-presence columns on documents.
--    verification_status:  pending -> in_progress -> submitted
--    locked_by/locked_at:  the verifier currently holding the file (manual lock)
--    assigned_to:          admin pin — only this user may claim it from the pool
--    current_page / last_activity_at: live presence (which page, last seen)
-- ----------------------------------------------------------------------------
ALTER TABLE documents ADD COLUMN IF NOT EXISTS verification_status   VARCHAR(50) NOT NULL DEFAULT 'pending';
ALTER TABLE documents ADD COLUMN IF NOT EXISTS locked_by             UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS locked_at             TIMESTAMP WITH TIME ZONE;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS assigned_to           UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS current_page          INTEGER;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS last_activity_at      TIMESTAMP WITH TIME ZONE;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS verification_started_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS submitted_by          UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS submitted_at          TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_documents_verification ON documents(tenant_id, verification_status);
CREATE INDEX IF NOT EXISTS idx_documents_locked_by    ON documents(locked_by);
CREATE INDEX IF NOT EXISTS idx_documents_assigned_to  ON documents(assigned_to);

-- ----------------------------------------------------------------------------
-- 3. Per-page verification progress. "Submit" is gated on every page of a
--    document having a row here with is_verified = TRUE.
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS page_verifications (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    document_id  UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    page_number  INTEGER NOT NULL,
    is_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    verified_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    verified_at  TIMESTAMP WITH TIME ZONE,
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_page_verification UNIQUE (document_id, page_number)
);
CREATE INDEX IF NOT EXISTS idx_page_verif_tenant_time ON page_verifications(tenant_id, verified_at);
CREATE INDEX IF NOT EXISTS idx_page_verif_user_time   ON page_verifications(verified_by, verified_at);

-- ----------------------------------------------------------------------------
-- 4. Append-only verification activity log. Drives the live activity feed and
--    every time-bucketed statistic (today / month / date-range, per user/all).
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS verification_events (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
    page_number INTEGER,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    -- claim | release | force_release | assign | unassign | open_page |
    -- page_verified | page_unverified | submit | reopen | cell_edit
    event_type  VARCHAR(50) NOT NULL,
    metadata    JSONB DEFAULT '{}'::jsonb,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_verif_events_tenant_time ON verification_events(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verif_events_user_time   ON verification_events(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verif_events_doc         ON verification_events(document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verif_events_type_time   ON verification_events(event_type, created_at);
