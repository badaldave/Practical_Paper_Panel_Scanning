-- ============================================================================
-- Migration 000005 — Columns the OCR worker writes that earlier migrations never
-- created.
--
-- These columns existed on dev/older databases only because they were hand-added
-- (this repo has no migration tooling — see CLAUDE.md). They were never captured
-- in 000001–000004, so a database built purely from the migrations is missing
-- them and the worker crashes on its first document with errors like
--   column "college_code" of relation "document_pages" does not exist
--   column "page_number" of relation "extracted_cells_history" does not exist
-- This migration brings a migrations-only database up to what the code expects.
--
-- Two writers depend on these:
--   * document_pages metadata  — python-worker save_page_record / extract_headers
--     and go-backend document_repository (CreatePage/GetPages/UpdatePageMetadata).
--     `_extract_page_metadata` regex-scrapes college/subject/faculty/candidate
--     count from the top of each page; the verification UI surfaces them.
--   * extracted_cells_history.page_number — written by both the worker
--     (save_extractions) and go-backend (SaveCell), mirroring
--     extracted_cells.page_number (INTEGER NOT NULL DEFAULT 1).
--
-- Types/lengths match the columns as they exist on the dev database. Idempotent
-- (IF NOT EXISTS) so it is safe to re-run and safe to apply on top of databases
-- that already have these columns from a manual fix.
-- ============================================================================

-- Per-page marksheet header metadata.
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS college_code     VARCHAR(50);
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS college_name     VARCHAR(255);
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS subject_code     VARCHAR(50);
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS subject_name     VARCHAR(255);
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS faculty          VARCHAR(100);
ALTER TABLE document_pages ADD COLUMN IF NOT EXISTS total_candidates INTEGER;

-- Cell-history rows carry the page number, like extracted_cells. Existing rows
-- (if any) default to page 1; the column is NOT NULL because every writer sets it.
ALTER TABLE extracted_cells_history ADD COLUMN IF NOT EXISTS page_number INTEGER NOT NULL DEFAULT 1;

-- Fix the extracted_cells uniqueness key. 000001 created uq_cell_coordinate as
-- UNIQUE (document_id, row_index, column_index, version); 000002 then added the
-- page_number column but never folded it into the constraint. Without page_number
-- in the key, row 0/col 0 of page 2 collides with row 0/col 0 of page 1, so the
-- worker fails on EVERY multi-page document with:
--   duplicate key value violates unique constraint "uq_cell_coordinate"
-- Recreate the constraint to include page_number (matches the dev database).
-- Widening the key (more columns) can never violate rows that satisfied the old,
-- narrower key, so this is safe on existing data. Guarded so re-runs are no-ops.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'uq_cell_coordinate'
          AND conrelid = 'extracted_cells'::regclass
          AND pg_get_constraintdef(oid) =
              'UNIQUE (document_id, page_number, row_index, column_index, version)'
    ) THEN
        ALTER TABLE extracted_cells DROP CONSTRAINT IF EXISTS uq_cell_coordinate;
        ALTER TABLE extracted_cells ADD CONSTRAINT uq_cell_coordinate
            UNIQUE (document_id, page_number, row_index, column_index, version);
    END IF;
END$$;
