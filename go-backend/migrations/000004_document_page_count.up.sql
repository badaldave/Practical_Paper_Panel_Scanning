-- Store the document's page count so the UI can show it even before extraction
-- finishes (and even if processing fails part-way). The worker writes this the
-- moment it opens the file — for PDFs the true page count from PyMuPDF, for
-- single images 1 — so a document that crashes mid-OCR still reports how many
-- pages it has, instead of only the number of pages that happened to render.
--
-- No migration tooling in this repo (see CLAUDE.md): apply by hand against the
-- live DB. Idempotent.

ALTER TABLE documents ADD COLUMN IF NOT EXISTS page_count INTEGER;

-- Backfill existing rows from however many pages were already rendered, so the
-- column is populated for documents uploaded before this change.
UPDATE documents d
SET page_count = (SELECT COUNT(*) FROM document_pages dp WHERE dp.document_id = d.id)
WHERE d.page_count IS NULL;
