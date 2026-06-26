-- ============================================================================
-- Migration 000003 — Examiner registry (historical prior for name inference).
--
-- Seeds a per-tenant directory of known examiners (mobile -> canonical name)
-- imported from historical spreadsheets (e.g. EXMNAME.xlsx). The Python worker
-- folds these rows into the consensus `examiner_directory` so a poorly-read or
-- missing name on a NEW sheet can be filled from history when its mobile is
-- recognised — before the in-document cross-row vote even runs.
--
-- Mobile is the trusted join key (10 digits, OCRs far better than handwriting).
-- When one mobile carried genuinely different people across years (number
-- reassigned), the row is marked is_ambiguous and EXCLUDED from auto-inference;
-- it falls back to normal consensus / human review. Spelling and partial-name
-- variants of one person are collapsed to a single canonical_name at import.
--
-- Idempotent (IF NOT EXISTS) so it can be applied on top of a running DB.
-- ============================================================================

CREATE TABLE IF NOT EXISTS examiner_registry (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- 10-digit mobile, the entity key. Stored digits-only, no leading tab/+91.
    mobile         VARCHAR(10) NOT NULL,

    -- Voted full name for this examiner. NULL when is_ambiguous (we refuse to
    -- pick one of several genuinely different people).
    canonical_name TEXT,

    -- Every distinct spelling/partial observed for this mobile, for human
    -- reference and audit. e.g. ["RAVI KANT", "RAVI KANT SHARMA"].
    name_variants  JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- TRUE => >=2 different people seen on this number; do not auto-infer.
    is_ambiguous   BOOLEAN NOT NULL DEFAULT FALSE,

    -- How many source rows fed this mobile (vote weight / data volume signal).
    times_seen     INTEGER NOT NULL DEFAULT 1,

    -- Provenance: which import file(s) contributed, so re-runs are traceable.
    source_files   JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One registry row per (tenant, mobile) — the importer upserts on this.
    CONSTRAINT uq_examiner_registry_tenant_mobile UNIQUE (tenant_id, mobile)
);

-- The worker loads the whole non-ambiguous directory for a tenant on each job.
CREATE INDEX IF NOT EXISTS idx_examiner_registry_tenant
    ON examiner_registry (tenant_id)
    WHERE is_ambiguous = FALSE;
