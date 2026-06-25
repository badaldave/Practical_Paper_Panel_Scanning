# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A multi-tenant platform for digitizing scanned university result sheets / marksheets (PDF or image) via OCR, then letting humans verify and correct the extracted tabular data cell-by-cell against the source image. Three independently-deployed services share one PostgreSQL database:

- **`go-backend/`** — Gin REST API (auth, document upload, extraction CRUD, export). Default port 8080, runs on **8081** under Docker.
- **`python-worker/`** — Standalone OCR daemon. Polls the DB job queue, runs the OCR/layout pipeline, writes extracted cells back. **Not** called over HTTP by the backend.
- **`react-frontend/`** — Vite + React 19 SPA (TypeScript). Dev server on port **3001**, proxies `/api` to the backend.

## Service communication = the DB job queue

There is no direct RPC between the Go API and the Python worker. They coordinate entirely through the `processing_jobs` table:

1. On document upload the Go `DocumentService` enqueues a row in `processing_jobs` (status `pending`).
2. The Python worker (`app/main.py`) polls via `WorkerRepository.dequeue_job()` — a `SELECT ... FOR UPDATE SKIP LOCKED`-style claim using `locked_by`/`locked_at`.
3. The worker runs `ProcessingEngine.process_document()`, writes `document_pages` + `extractions`/`extracted_tables`/`extracted_rows`/`extracted_cells`, updates document status/progress, and marks the job complete.
4. Failures re-queue with backoff (status `retrying`, `run_at` pushed out by `30s * attempts`) until `max_attempts` (default 3), then fail permanently.

**Mock worker:** The Go server contains `startMockJobProcessor` (`cmd/api/main.go`) that does the same dequeue→fake-cells→complete loop entirely in-process, so the backend is testable without the Python daemon. It runs **unless** `MOCK_WORKER=false`. `docker-compose.yml` sets `MOCK_WORKER=false` because the real Python worker is expected to run separately (on the host, for GPU access).

## Go backend architecture (clean / hexagonal)

`internal/` is layered — keep dependencies pointing inward (interfaces → application → domain; infrastructure implements domain interfaces):

- `domain/` — entities + repository **interfaces** (no framework imports). `BoundingBox` and the extraction model live here.
- `application/{auth,document,extraction}/` — services holding business logic, depend only on domain interfaces.
- `infrastructure/db/` — pgx-backed repository implementations; `storage/` — local filesystem storage provider.
- `interfaces/http/` — `router.go` wires everything; `handlers/`, `middlewares/` (JWT auth, CORS, token-bucket rate limit).
- `pkg/` — `crypto` (JWT + bcrypt), `contextutil` (tenant/user extraction from request context).

`cmd/` has three entrypoints: `api` (server), `migrate` (applies `migrations/000001_init_schema.up.sql`), `seed` (inserts the demo tenant `e93fca1e-...` + user/roles).

Multi-tenancy is enforced in queries by `tenant_id`, derived from the JWT and carried in request context — when adding queries that touch tenant-scoped tables, always filter by tenant.

## Python OCR pipeline

`ProcessingEngine` (`app/pipeline/engine.py`) is the core and the most intricate code in the repo:

- **Provider selection:** tries providers in `OCR_PRIORITY` order (`paddle,surya,doctr,tesseract`), picking the first whose `is_available()` is true; falls back to a mock if none load. Each provider in `app/ocr/` implements the `OCRProvider` ABC in `base.py` returning a common `{cells: [{value, confidence, bbox}]}` shape.
- **PDF vs image:** PDFs are rendered per-page at 150 DPI via PyMuPDF (`fitz`) with no OpenCV preprocessing; single images go through `ImagePreprocessor` (deskew, CLAHE, denoise, binarize).
- **`_align_coordinates`** turns flat OCR cells into a 4-column marksheet grid (subcode, batch, examiner name, mobile) using x-position percentages, with heavy domain-specific heuristics: splitting merged name+mobile cells, content-based column overrides, header-row detection, vertical merge of split handwritten cells, and noise-row filtering. There are hardcoded value fixups (e.g. `khondelwal`→`Khandelwal`) tuned to the target documents.
- **`_extract_page_metadata`** regex-scrapes college/subject/faculty/candidate-count from the top of the page.
- **Cross-row consensus** (`app/pipeline/consensus.py`, `apply_document_consensus`, called from `process_document`): the same examiner recurs across many rows keyed by their mobile number, so rows are clustered into examiner entities and a single voted name/mobile is backfilled into poorly-read siblings. Backfilled cells are flagged `is_inferred=True` (confidence forced to `INFERRED_CONFIDENCE`) — that flag is persisted (`extracted_cells.is_inferred` column) and meant to be surfaced for human review rather than trusted.
- **Feedback loop:** `FeedbackMemory` applies tenant-specific `original→corrected` mappings from the `correction_feedback` table (populated when users fix cells in the UI).

`app/*.py` at the top level (`process_all_pages.py`, `test_*_speed.py`, `analyze_dbf.py`, etc.) are ad-hoc scripts/spikes, not part of the daemon path.

## Pipeline tuning loop (`tmp_eval/`)

`tmp_eval/` is the offline iteration harness for tuning the marksheet post-processing against the **SCC58** target document — it is **not** part of the deployed daemon, but it is how `engine.py`/`consensus.py` heuristics get measured.

- `groundtruth.py` parses `SCC58.XLS` (actually a dBASE/DBF binary, path hardcoded to a `Downloads` location) into per-page records — the accuracy ground truth.
- OCR is cached to JSON (`ocr_cache_150.json` = Paddle @150 DPI, `ocr_cache_surya.json` = Surya) by `cache_ocr.py` / `cache_surya.py` so the tuning loop runs **without re-OCRing or touching the DB**.
- `harness.py` / `harness_hybrid.py` / `harness_consensus.py` call `ProcessingEngine._align_coordinates` (and the consensus pass) on the cached cells and print strict cell / name / mobile accuracy. Workflow: edit `engine.py` or `consensus.py`, re-run a harness, read the accuracy delta. `harness_hybrid.py` scores names by char similarity (`NAME_SIM_OK`, default 0.85) and mobiles with optional digit tolerance (`MOBILE_TOL`).
- Accuracy is handwriting-bound (~82–89% ceiling on names/mobiles); see the `scc-marksheet-pipeline` memory note.

## Frontend architecture

Feature-Sliced Design layout under `src/`: `pages/` (login, dashboard, verification), `entities/` (e.g. `user/model/store.ts` — Zustand), `shared/` (`api/client.ts` fetch wrapper, `ui/canvas-viewer.tsx`). Path alias `@` → `src`.

- Auth token is stored in `localStorage` (`auth_token`); `apiClient` attaches it as a Bearer header and clears it on 401.
- The verification page uses **ag-grid** for the editable cell grid alongside `canvas-viewer` which overlays cell bounding boxes on the page image — bbox coordinates flow from the worker's extraction through the API into the canvas.

## Commands

### Database (required first)
```bash
docker compose up -d db          # Postgres 16 on host port 5439
cd go-backend && DATABASE_URL=... go run ./cmd/migrate   # create schema
DATABASE_URL=... go run ./cmd/seed                       # demo tenant/user/roles
```

### Go backend
```bash
cd go-backend
go run ./cmd/api          # start API (PORT, DATABASE_URL, JWT_SECRET, UPLOAD_DIR, MOCK_WORKER)
go build -o api-server ./cmd/api/main.go
go test ./...
go vet ./...
```

### Python worker (run on host for GPU)
```bash
# From repo root — sets DATABASE_URL/PYTHONPATH, injects CUDA paths, activates venv:
./run_local_worker.ps1
# Or manually:
cd python-worker && ./venv/Scripts/python -u app/main.py
```
Worker config comes from `python-worker/.env` (`DATABASE_URL`, `UPLOAD_DIR`, `OCR_PRIORITY`, `POLL_INTERVAL`, `WORKER_ID`).

### Frontend
```bash
cd react-frontend
npm install
npm run dev      # Vite dev server :3001, proxies /api → http://127.0.0.1:8081
npm run build    # tsc typecheck + vite build
npm run lint     # eslint, --max-warnings 0
```

### Full stack via Docker
`docker compose up` starts `db` + `go-backend` only (port 8081, `MOCK_WORKER=false`). The `python-worker` service is intentionally commented out in `docker-compose.yml` — run it on the host so it can reach the GPU.

## Gotchas

- **DB port mismatch:** Postgres is published on host **5439** (mapped to container 5432). The default DSNs in the three `cmd/` entrypoints disagree (`5432`, `5433`, `5439`) — always pass an explicit `DATABASE_URL` rather than trusting the fallback. The Go DSN uses `key=value` libpq format; Python uses a `postgresql://` URL.
- **Upload path translation:** the worker stores DB image paths as `/var/data/uploads/...` (the container mount) but resolves them on the host against `Config.UPLOAD_DIR` (`../go-backend/uploads`). `process_document` strips the mount prefix to find the real file — preserve this when changing storage paths.
- **Column layout is hardcoded to 4 columns** for the specific marksheet format, with literal text corrections baked into `_align_coordinates`. Treat it as document-specific, not general table extraction.
- Cell edits are versioned: `extracted_cells` keeps the latest version (unique on `document_id,row,col,version`) and `extracted_cells_history` retains prior values; `audit_logs` is append-only.
- There is no migration tool/versioning — schema is one hand-applied SQL file run by `cmd/migrate`.
