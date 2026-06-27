import json
from datetime import datetime, timedelta
from uuid import uuid4
import psycopg
from app.config import Config
from app.db.connection import DBConnection

class WorkerRepository:
    @staticmethod
    def dequeue_job() -> dict:
        """Atomically lock and pull a pending job using FOR UPDATE SKIP LOCKED."""
        query = """
            UPDATE processing_jobs
            SET status = 'processing',
                locked_at = %s,
                locked_by = %s,
                attempts = attempts + 1,
                updated_at = %s
            WHERE id = (
                SELECT id
                FROM processing_jobs
                WHERE (status = 'pending' OR status = 'retrying') AND run_at <= %s
                ORDER BY created_at ASC
                FOR UPDATE SKIP LOCKED
                LIMIT 1
            )
            RETURNING id, tenant_id, document_id, status, error_message, attempts, max_attempts
        """
        now = datetime.utcnow()
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, (now, Config.WORKER_ID, now, now))
                row = cur.fetchone()
                if row:
                    conn.commit()
                    return row
        return None

    @staticmethod
    def reap_stale_jobs(timeout_minutes: int = None) -> int:
        """Self-healing: reclaim jobs stuck in 'processing' whose lock has gone stale
        (i.e. the worker that claimed them died mid-job). They are re-queued so they
        get picked up again instead of staying stuck forever. Jobs that have already
        exhausted their attempts are failed permanently. Returns count reclaimed."""
        if timeout_minutes is None:
            timeout_minutes = Config.JOB_STALE_TIMEOUT_MINUTES

        now = datetime.utcnow()
        cutoff = now - timedelta(minutes=timeout_minutes)
        query = """
            UPDATE processing_jobs
            SET status = CASE WHEN attempts < max_attempts THEN 'retrying' ELSE 'failed' END,
                error_message = 'Reclaimed: worker lock expired (stale processing job)',
                locked_at = NULL,
                locked_by = NULL,
                run_at = %s,
                updated_at = %s
            WHERE status = 'processing'
              AND locked_at IS NOT NULL
              AND locked_at < %s
            RETURNING id, document_id, status
        """
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, (now, now, cutoff))
                reaped = cur.fetchall()
                for r in reaped:
                    if r["status"] == "failed":
                        cur.execute(
                            "UPDATE documents SET status = 'failed', updated_at = %s WHERE id = %s",
                            (now, r["document_id"])
                        )
                    else:
                        # Hand it back to the queue UI as queued for reprocessing
                        cur.execute(
                            "UPDATE documents SET status = 'queued', updated_at = %s WHERE id = %s",
                            (now, r["document_id"])
                        )
                conn.commit()
        return len(reaped)

    @staticmethod
    def reclaim_own_jobs() -> int:
        """Crash recovery on startup: re-queue any job still marked 'processing'
        under THIS worker's id. A worker that is (re)starting cannot actually be
        running anything, so such a row is necessarily an orphan left by a crash —
        reclaim it immediately regardless of how long ago it was locked. (The
        age-based reaper would otherwise ignore a freshly-crashed job for the whole
        JOB_STALE_TIMEOUT window, which is exactly how a crashed worker used to
        leave its own upload stuck.) Jobs that have exhausted their attempts are
        failed permanently. Returns count reclaimed.

        Safe because WORKER_ID is a single-instance identity: each concurrently
        running worker must have a distinct WORKER_ID (e.g. docker-ocr-worker-01
        vs docker-gpu-worker-01), so we never yank a job from a live sibling."""
        now = datetime.utcnow()
        query = """
            UPDATE processing_jobs
            SET status = CASE WHEN attempts < max_attempts THEN 'retrying' ELSE 'failed' END,
                error_message = CASE WHEN attempts < max_attempts
                    THEN 'Worker restarted mid-job; automatically re-queued (crash recovery).'
                    ELSE 'Processing failed: the worker crashed repeatedly while reading this document and has reached the maximum number of attempts. The file may be too large or complex for the available memory, or a page may be unreadable. Please try again or contact support.'
                END,
                locked_at = NULL,
                locked_by = NULL,
                run_at = %s,
                updated_at = %s
            WHERE status = 'processing'
              AND locked_by = %s
            RETURNING id, document_id, status
        """
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, (now, now, Config.WORKER_ID))
                reaped = cur.fetchall()
                for r in reaped:
                    new_doc_status = "failed" if r["status"] == "failed" else "queued"
                    cur.execute(
                        "UPDATE documents SET status = %s, updated_at = %s WHERE id = %s",
                        (new_doc_status, now, r["document_id"])
                    )
                conn.commit()
        return len(reaped)

    @staticmethod
    def heartbeat_job(job_id: str) -> None:
        """Refresh a running job's lock so the reaper treats locked_at as a liveness
        signal, not just an age. A healthy job that runs longer than
        JOB_STALE_TIMEOUT keeps beating and is never mistaken for a dead one; a
        worker that actually dies stops beating and is reclaimed promptly. Scoped to
        our own WORKER_ID + 'processing' so a heartbeat can never revive a job the
        reaper already handed to someone else."""
        now = datetime.utcnow()
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE processing_jobs SET locked_at = %s, updated_at = %s "
                    "WHERE id = %s AND locked_by = %s AND status = 'processing'",
                    (now, now, job_id, Config.WORKER_ID)
                )
                conn.commit()

    @staticmethod
    def record_job_attempt(job_id: str, attempt_num: int):
        query = """
            INSERT INTO job_attempts (id, job_id, attempt_number, started_at, status, created_at)
            VALUES (%s, %s, %s, %s, 'processing', %s)
            RETURNING id
        """
        attempt_id = str(uuid4())
        now = datetime.utcnow()
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, (attempt_id, job_id, attempt_num, now, now))
                conn.commit()
                return attempt_id

    @staticmethod
    def update_job_attempt(attempt_id: str, status: str, error_msg: str = None):
        query = """
            UPDATE job_attempts
            SET status = %s, error_message = %s, ended_at = %s
            WHERE id = %s
        """
        now = datetime.utcnow()
        DBConnection.execute_query(query, (status, error_msg, now, attempt_id))

    @staticmethod
    def complete_job(job_id: str, document_id: str):
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute("UPDATE processing_jobs SET status = 'completed', locked_at = NULL, locked_by = NULL, updated_at = %s WHERE id = %s", (datetime.utcnow(), job_id))
                cur.execute("UPDATE documents SET status = 'extracted', progress_percentage = 100, updated_at = %s WHERE id = %s", (datetime.utcnow(), document_id))
                conn.commit()

    @staticmethod
    def fail_job(job_id: str, document_id: str, reason: str):
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute("UPDATE processing_jobs SET status = 'failed', error_message = %s, locked_at = NULL, locked_by = NULL, updated_at = %s WHERE id = %s", (reason, datetime.utcnow(), job_id))
                cur.execute("UPDATE documents SET status = 'failed', progress_percentage = 0, updated_at = %s WHERE id = %s", (datetime.utcnow(), document_id))
                conn.commit()

    @staticmethod
    def set_page_count(document_id: str, page_count: int):
        """Record how many pages the source file has, as soon as it is known (when
        the worker opens the file), so the UI can show it even if extraction later
        fails part-way through."""
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE documents SET page_count = %s, updated_at = %s WHERE id = %s",
                    (page_count, datetime.utcnow(), document_id)
                )
                conn.commit()

    @staticmethod
    def mark_page_failed(document_id: str, page_number: int):
        """Flag a single page whose OCR crashed/failed so partial results stay
        usable and the failed page is visible, without failing the whole document."""
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE document_pages SET status = 'failed', updated_at = %s "
                    "WHERE document_id = %s AND page_number = %s",
                    (datetime.utcnow(), document_id, page_number)
                )
                conn.commit()

    @staticmethod
    def update_document_progress(document_id: str, progress: int):
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE documents SET progress_percentage = %s, updated_at = %s WHERE id = %s",
                    (progress, datetime.utcnow(), document_id)
                )
                conn.commit()

    @staticmethod
    def get_document(document_id: str) -> dict:
        query = "SELECT id, name, file_path, tenant_id FROM documents WHERE id = %s"
        rows = DBConnection.execute_query(query, (document_id,), fetch=True)
        return rows[0] if rows else None

    @staticmethod
    def save_page_record(document_id: str, page_number: int, image_path: str, width: int, height: int,
                         college_code: str = None, college_name: str = None,
                         subject_code: str = None, subject_name: str = None,
                         faculty: str = None, total_candidates: int = None):
        query = """
            INSERT INTO document_pages (id, document_id, page_number, image_path, width, height, status,
                                        college_code, college_name, subject_code, subject_name, faculty, total_candidates,
                                        created_at, updated_at)
            VALUES (%s, %s, %s, %s, %s, %s, 'processed', %s, %s, %s, %s, %s, %s, %s, %s)
            ON CONFLICT (document_id, page_number) DO UPDATE
            SET image_path = EXCLUDED.image_path, width = EXCLUDED.width, height = EXCLUDED.height, status = 'processed',
                college_code = COALESCE(EXCLUDED.college_code, document_pages.college_code),
                college_name = COALESCE(EXCLUDED.college_name, document_pages.college_name),
                subject_code = COALESCE(EXCLUDED.subject_code, document_pages.subject_code),
                subject_name = COALESCE(EXCLUDED.subject_name, document_pages.subject_name),
                faculty = COALESCE(EXCLUDED.faculty, document_pages.faculty),
                total_candidates = COALESCE(EXCLUDED.total_candidates, document_pages.total_candidates),
                updated_at = EXCLUDED.updated_at
        """
        now = datetime.utcnow()
        DBConnection.execute_query(query, (
            str(uuid4()), document_id, page_number, image_path, width, height,
            college_code, college_name, subject_code, subject_name, faculty, total_candidates,
            now, now
        ))

    @staticmethod
    def save_extractions(tenant_id: str, document_id: str, extracted_data: dict):
        """
        Saves structural layout records and versioned cells into Postgres.
        extracted_data structure:
        {
            "tables": [
                {
                    "page_number": 1,
                    "table_index": 0,
                    "bbox": {"x": 0.1, "y": 0.1, "width": 0.8, "height": 0.8},
                    "rows": [
                        {
                            "row_index": 0,
                            "cells": [
                                {
                                    "column_index": 0,
                                    "value": "Roll No",
                                    "confidence": 0.99,
                                    "bbox": {"x": 0.1, "y": 0.1, "width": 0.2, "height": 0.05}
                                }
                            ]
                        }
                    ]
                }
            ]
        }
        """
        extraction_id = str(uuid4())
        now = datetime.utcnow()

        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                # Clear previous extractions and cells to avoid unique constraint violations
                cur.execute("DELETE FROM extractions WHERE document_id = %s", (document_id,))
                cur.execute("DELETE FROM extracted_cells WHERE document_id = %s", (document_id,))
                
                # 1. Create main Extraction record
                cur.execute(
                    "INSERT INTO extractions (id, tenant_id, document_id, status, created_at, updated_at) VALUES (%s, %s, %s, 'completed', %s, %s)",
                    (extraction_id, tenant_id, document_id, now, now)
                )

                for table in extracted_data.get("tables", []):
                    table_id = str(uuid4())
                    bbox_json = json.dumps(table["bbox"])
                    
                    # 2. Create Table record
                    cur.execute(
                        "INSERT INTO extracted_tables (id, extraction_id, page_number, table_index, bounding_box, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s)",
                        (table_id, extraction_id, table["page_number"], table["table_index"], bbox_json, now, now)
                    )

                    for row in table.get("rows", []):
                        row_id = str(uuid4())
                        row_index = row["row_index"]
                        
                        # 3. Create Row record
                        cur.execute(
                            "INSERT INTO extracted_rows (id, table_id, row_index, created_at, updated_at) VALUES (%s, %s, %s, %s, %s)",
                            (row_id, table_id, row_index, now, now)
                        )

                        for cell in row.get("cells", []):
                            cell_id = str(uuid4())
                            col_index = cell["column_index"]
                            val = cell["value"]
                            conf = cell["confidence"]
                            is_inferred = bool(cell.get("is_inferred", False))
                            cell_bbox = json.dumps(cell["bbox"])

                            # 4. Insert Cell version 1 (AI extracted initial value)
                            cur.execute(
                                """
                                INSERT INTO extracted_cells
                                (id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, is_inferred, version, created_at, updated_at)
                                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, 1, %s, %s)
                                """,
                                (cell_id, document_id, table["page_number"], row_index, col_index, val, val, conf, cell_bbox, is_inferred, now, now)
                            )

                            # 5. Record version 1 in Cell History
                            cur.execute(
                                """
                                INSERT INTO extracted_cells_history
                                (id, cell_id, document_id, page_number, row_index, column_index, value, confidence, bbox, version, created_at)
                                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, 1, %s)
                                """,
                                (str(uuid4()), cell_id, document_id, table["page_number"], row_index, col_index, val, conf, cell_bbox, now)
                            )
                conn.commit()

    @staticmethod
    def load_examiner_pairs(tenant_id: str, exclude_document_id: str = None) -> list:
        """Cross-document examiner directory source.

        Returns every (name, mobile) pair from this TENANT's HUMAN-VERIFIED
        documents (verification_status = 'submitted'), so the consensus pass can
        infer a poorly-read name from how the same examiner was read and corrected
        on other sheets — not just from sibling rows in the current document.

        Only submitted documents vote: an extracted-but-unverified duplicate still
        holds raw OCR misreads, and counting those equally to a human correction
        lets a stale copy outvote (or tie) the verified value. Gating on
        verification_status keeps the directory trustworthy.

        Only the latest version of each cell is used (so the final corrected value
        wins), and the current document is excluded to avoid voting against stale
        copies of its own cells on reprocessing. Name column = 2, mobile col = 3."""
        params = [tenant_id]
        exclude_clause = ""
        if exclude_document_id:
            exclude_clause = "AND c.document_id <> %s"
            params.append(exclude_document_id)

        query = f"""
            WITH latest AS (
                SELECT DISTINCT ON (c.document_id, c.page_number, c.row_index, c.column_index)
                    c.document_id, c.page_number, c.row_index, c.column_index,
                    c.current_value, c.is_inferred
                FROM extracted_cells c
                JOIN documents d ON d.id = c.document_id
                WHERE d.tenant_id = %s
                  AND d.verification_status = 'submitted'
                  AND c.column_index IN (2, 3)
                  {exclude_clause}
                ORDER BY c.document_id, c.page_number, c.row_index, c.column_index, c.version DESC
            )
            SELECT n.current_value AS name,
                   n.is_inferred AS name_inferred,
                   m.current_value AS mobile
            FROM latest n
            JOIN latest m
              ON n.document_id = m.document_id
             AND n.page_number = m.page_number
             AND n.row_index = m.row_index
            WHERE n.column_index = 2 AND m.column_index = 3
        """
        return DBConnection.execute_query(query, tuple(params), fetch=True) or []

    @staticmethod
    def load_registry_pairs(tenant_id: str) -> list:
        """Historical examiner prior, shaped like `load_examiner_pairs` output.

        Returns the seeded (mobile -> canonical name) directory imported from
        spreadsheets into `examiner_registry`, so the consensus pass treats it as
        just another set of cross-document votes — a poorly-read or missing name
        on a new sheet borrows from history when its mobile is recognised.

        Ambiguous mobiles (a number reused by different people across years) are
        excluded: we never auto-fill a name we aren't confident about. `votes`
        carries `times_seen` so a heavily-attested examiner weighs more (the apply
        step still caps any single examiner's influence at DB_VOTE_CAP)."""
        query = """
            SELECT mobile, canonical_name AS name, times_seen AS votes
            FROM examiner_registry
            WHERE tenant_id = %s
              AND is_ambiguous = FALSE
              AND canonical_name IS NOT NULL
        """
        return DBConnection.execute_query(query, (tenant_id,), fetch=True) or []

    @staticmethod
    def load_correction_memory(tenant_id: str) -> dict:
        """Loads non-applied corrections to build a local correction dictionary."""
        query = """
            SELECT original_value, corrected_value, document_type
            FROM correction_feedback
            WHERE tenant_id = %s AND is_applied_in_training = FALSE
        """
        rows = DBConnection.execute_query(query, (tenant_id,), fetch=True)
        
        # Build mapping: {document_type: {original: corrected}}
        mapping = {}
        for r in rows:
            doc_type = r["document_type"]
            orig = r["original_value"]
            corr = r["corrected_value"]
            
            if doc_type not in mapping:
                mapping[doc_type] = {}
            mapping[doc_type][orig] = corr
        return mapping
