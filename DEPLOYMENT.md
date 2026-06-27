# DEPLOYMENT — Marksheet OCR & Verification Platform

Production deployment guide for the **single-host Ubuntu + Docker + NVIDIA RTX 5070** target.
Hand this to the system administrator. Every command is copy-paste ready; version numbers
are pinned where they matter.

---

## 1. What gets deployed

Four components on one machine, all via Docker, behind the existing **Apache** front door:

| Component | Image / base | Role | Port |
|---|---|---|---|
| PostgreSQL | `postgres:16-alpine` | Database **and** job queue (services talk only through it) | 5432 → host **5439** |
| go-backend | `golang:1.25-alpine` build → `alpine:3.19` | REST API (auth, upload, export) | **8081** |
| python-worker | `python:3.12` (+ CUDA for GPU) | OCR daemon. Polls DB, **no HTTP** | none (DB + GPU only) |
| react-frontend | built with `node:20-alpine` | SPA static files (served by Apache) | n/a (Apache serves) |

There is **no RPC** between backend and worker — they coordinate through the `processing_jobs`
table. The worker can therefore be restarted independently.

The OCR worker uses the **GPU when one is available and falls back to CPU automatically** (§4) —
no configuration needed.

---

## 2. Host hardware (this deployment)

- **OS:** Ubuntu 22.04 / 24.04 LTS
- **CPU:** Intel i7 · **RAM:** 16 GB (~13 GB usable — shared with the existing Python program + Apache)
- **GPU:** NVIDIA GeForce RTX 5070 12 GB (**Blackwell, compute capability sm_120**)
- **Disk:** keep **≥ 100 GB free** on the partition holding `go-backend/uploads` (originals +
  rendered page PNGs). See §9 for why.

> ⚠️ **RAM is the tight resource, not VRAM.** Cap container memory (§7) and watch `docker stats`
> on the first big job. If you can grow the box to 32 GB, do it — it removes the main fragility.

---

## 3. Host prerequisites & getting the code (one-time)

```bash
# Docker Engine 24+ and the Compose v2 plugin
sudo apt-get update
sudo apt-get install -y ca-certificates curl git
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# verify
docker --version          # expect Docker version 24.x or newer
docker compose version    # expect Docker Compose version v2.x

# (optional) run docker without sudo — log out/in afterwards for it to take effect
sudo usermod -aG docker "$USER"
```

**Get the code.** The repository is public — clone it directly:

```bash
cd /opt                                  # or wherever deployments live
git clone https://github.com/badaldave/Practical_Paper_Panel_Scanning.git
cd Practical_Paper_Panel_Scanning        # <-- ALL later commands run from this repo root
git checkout main
```

You do **not** install Go, Node, Python, or CUDA on the host — the images carry them. Everything
below assumes your shell is in the cloned repo root (`Practical_Paper_Panel_Scanning/`).

---

## 4. GPU enablement for the RTX 5070 (Blackwell)

The 5070 needs **CUDA 12.8+** and a recent driver. The GPU image (`Dockerfile.gpu`) is already
configured for this; the host just needs the driver and the container toolkit below.

```bash
# 4.1 NVIDIA driver (Blackwell needs >= 570). Reboot after install.
sudo apt-get install -y nvidia-driver-570
sudo reboot
# after reboot:
nvidia-smi                # must list the "RTX 5070" and a CUDA >= 12.8 runtime

# 4.2 NVIDIA Container Toolkit (lets Docker pass the GPU into containers)
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -fsSL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
  | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
  | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker

# 4.3 Confirm Docker can see the GPU with a CUDA 12.8 image
docker run --rm --gpus all nvidia/cuda:12.8.0-base-ubuntu22.04 nvidia-smi
```

**`python-worker/Dockerfile.gpu` is already configured for the 5070** — its defaults are
**CUDA 12.8 base + cu128 torch**. No edit is needed; just build it (§7). To move to CUDA 12.9
instead, override both build args together:
`--build-arg CUDA_TAG=12.9.0-cudnn-runtime-ubuntu22.04 --build-arg TORCH_INDEX=.../cu129`.

**Single image, automatic GPU/CPU fallback — no code branching.** The worker auto-detects the
device at runtime: it uses the GPU when a usable CUDA device is present and **falls back to CPU
automatically** otherwise (so the *same* image runs accelerated on the 5070 and CPU-only on a
host with no GPU or if the driver breaks — the worker keeps running instead of dying). The chosen
device is logged at startup (see §7).

> The existing GPU program on this box shares the 12 GB VRAM with the worker — fine here (the OCR
> models use < ~2 GB), but confirm headroom with `nvidia-smi` while both run.

> ⚠️ **Verify on first build.** This GPU image hasn't been built on a physical Blackwell host from
> here. If `pip` reports a package-version conflict during the build, switch to the cu129 args
> above. After the build, confirm the startup log shows the GPU (§7).

---

## 5. Secrets / config to change before go-live

Edit `docker-compose.yml` — **do not ship the dev defaults**:

| Variable | Default (change it) | Notes |
|---|---|---|
| `POSTGRES_PASSWORD` | `postgres_secure_db_pass_2026` | Change, then update both `DATABASE_URL`s below |
| `JWT_SECRET` | `platform_jwt_secure_signing_secret_key_2026` | Change — signs all auth tokens |
| `MOCK_WORKER` | `false` | Keep `false` so the real worker processes jobs |
| `UPLOAD_DIR` | `/var/data/uploads` | Backend **and** worker must share this volume |

`DATABASE_URL` has **two different formats** (this trips people up):
- **Go backend** (libpq): `host=db port=5432 user=postgres password=... dbname=university_ocr sslmode=disable`
- **Python worker** (URL): `postgresql://postgres:...@db:5432/university_ocr`

---

## 6. Database setup — step by step (run once, in order)

The schema is applied by the Go `migrate` entrypoint, which runs **all** `migrations/*.up.sql`
(000001 init → 000002 RBAC → 000003 examiner registry → 000004 page count) in order. It is
**idempotent** — safe to re-run.

```bash
# 0. From the repo root. Set the DSN once for the helper commands (host port 5439).
cd "/path/to/Practical Paper Panel Scanning"
export PGPASS='postgres_secure_db_pass_2026'   # <-- your real password from §5

# 1. Start ONLY the database first and wait for it to be healthy
docker compose up -d db
docker compose ps db                            # STATUS should show "healthy"
#   (the image is postgres:16-alpine; data lives in the named volume "pgdata")

# 2. Apply the schema (all four migrations) using a throwaway Go 1.25 container.
#    It reaches Postgres through the PUBLISHED host port 5439 via host-gateway,
#    so it does not depend on the compose network/project name.
docker run --rm --add-host=host.docker.internal:host-gateway \
  -v "$PWD/go-backend:/src" -w /src golang:1.25-alpine \
  sh -c "DATABASE_URL='host=host.docker.internal port=5439 user=postgres password=${PGPASS} dbname=university_ocr sslmode=disable' go run ./cmd/migrate"
#   Expect: "Executing migration: 000001_init_schema.up.sql" ... up to 000004,
#           then "Database schema migrations completed successfully!"

# 3. Seed the demo tenant + admin user + roles (Micronic tenant e93fca1e-...)
docker run --rm --add-host=host.docker.internal:host-gateway \
  -v "$PWD/go-backend:/src" -w /src golang:1.25-alpine \
  sh -c "DATABASE_URL='postgres://postgres:${PGPASS}@host.docker.internal:5439/university_ocr?sslmode=disable' go run ./cmd/seed"
#   Expect: "Starting database seeding process..." then a success line.

# 4. (Optional) Seed the historical examiner registry from EXMNAME.xlsx, if you have it.
#    Copy the file into the shared uploads dir first so the worker can read it, then
#    run the importer in the worker container (after the worker is built, §7):
#      cp EXMNAME.xlsx go-backend/uploads/
#      docker compose exec python-worker python -m app.import_examiner_registry /var/data/uploads/EXMNAME.xlsx --commit
```

> If you prefer not to use throwaway Go containers, you can instead run `cmd/migrate` and
> `cmd/seed` from any machine that has **Go 1.25** installed and network access to host port
> **5439**, passing the same `DATABASE_URL` (using `127.0.0.1:5439` instead of `db:5432`).

---

## 7. Build and run the stack

```bash
# DB + backend (backend already runs with MOCK_WORKER=false)
docker compose up -d --build db go-backend

# GPU OCR worker (base compose + GPU override; Dockerfile.gpu defaults to CUDA 12.8 / cu128)
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build python-worker

docker compose ps                 # all "running"/"healthy"
docker compose logs -f python-worker   # watch provider init + per-job timings
```

**Add memory caps** so OCR can't starve the OS / the existing Python program. In
`docker-compose.yml` (single-host, ~13 GB usable):

```yaml
  db:
    mem_limit: 2g
  go-backend:
    mem_limit: 512m
  python-worker:
    mem_limit: 6g      # one OCR pipeline ~2–4 GB; 6g leaves headroom
```

Verify the worker is actually on GPU — two ways:
```bash
# (a) torch sees the card
docker compose exec python-worker python -c \
  "import torch; print('cuda:', torch.cuda.is_available(), torch.cuda.get_device_name(0) if torch.cuda.is_available() else '')"
# expect: cuda: True NVIDIA GeForce RTX 5070

# (b) the worker's own startup log (printed the first time it OCRs a document)
docker compose logs python-worker | grep "Inference device"
# expect: ... Inference device: GPU (NVIDIA GeForce RTX 5070)
# CPU fallback would instead read: Inference device: CPU (no usable CUDA device — auto fallback)
```

---

## 8. Apache front door (serve the SPA + proxy the API + enforce upload limits)

Apache already owns 80/443, so we **do not** run the frontend container on port 80. Apache serves
the built static files and reverse-proxies `/api` to the backend. This is also where the
**upload size limit and timeouts** are enforced — no code change needed.

```bash
# Build the frontend once (Node 20) and publish it where Apache serves it
docker run --rm -v "$PWD/react-frontend:/app" -w /app node:20-alpine \
  sh -c "npm install && npm run build"
sudo mkdir -p /var/www/marksheet
sudo cp -r react-frontend/dist/* /var/www/marksheet/

sudo a2enmod proxy proxy_http rewrite headers
```

Vhost (`/etc/apache2/sites-available/marksheet.conf`):

```apache
<VirtualHost *:443>
    ServerName marksheet.yourdomain
    DocumentRoot /var/www/marksheet

    # ---- Upload limit + timeouts (covers a ~1000-page batch comfortably) ----
    LimitRequestBody 2147483648      # 2 GiB max upload (a 1000-page scan PDF is ~0.1–0.5 GB)
    Timeout 600                      # 10 min, so large uploads don't time out mid-transfer
    ProxyTimeout 600

    # ---- API -> Go backend ----
    ProxyPreserveHost On
    ProxyPass        /api  http://127.0.0.1:8081/api
    ProxyPassReverse /api  http://127.0.0.1:8081/api

    # ---- SPA: serve files, fall back to index.html for client routes ----
    <Directory /var/www/marksheet>
        Require all granted
        RewriteEngine On
        RewriteCond %{REQUEST_FILENAME} !-f
        RewriteCond %{REQUEST_FILENAME} !-d
        RewriteRule ^ /index.html [L]
    </Directory>

    # ---- your TLS cert directives here ----
    # SSLEngine on
    # SSLCertificateFile      /etc/ssl/...
    # SSLCertificateKeyFile   /etc/ssl/...
</VirtualHost>
```

```bash
sudo a2ensite marksheet
sudo apache2ctl configtest && sudo systemctl reload apache2
```

When the frontend changes, rebuild and re-copy `dist/` — the API and worker keep running.

---

## 9. Capacity, throughput, and the three big-file safeguards

**Throughput (per-page rates measured on this hardware; GPU figures projected):**

| Config | Per page | 1000-page file |
|---|---|---|
| Without GPU (CPU only) | ~29 s | ~8 hours |
| **With GPU** (this deployment) | ~7–8 s | **~2 hours** |

To roughly halve the wall-clock you can run 2 documents/pages in parallel (the i7 has the cores) —
but watch RAM (each OCR pipeline uses ~2–4 GB).

**Plan capacity by page count, not megabytes.** The three safeguards for large files:

1. **Upload size + timeout — handled in Apache (§8):** `LimitRequestBody 2147483648` (2 GiB) and
   `Timeout/ProxyTimeout 600`. The code itself has no hard cap, so Apache is where we set it.
   (OCR is asynchronous via the job queue, so the HTTP request only spans the *upload*, not the
   hours of OCR — 10 min is plenty for the transfer.)
2. **Disk headroom:** keep **≥ 100 GB free** on the `uploads` volume. A 1000-page job stores the
   original + ~1000 rendered PNGs. (DB growth is negligible — ~30k cells for 1000 pages.) Monitor
   with `df -h` and alert at 80%.
3. **Image resolution only matters for non-PDF uploads.** PDFs are normalized to **150 DPI per
   page** (`engine.py` `get_pixmap(dpi=150)`), so a 50 MB and a 500 MB PDF of equal page count
   cost the same. A single very high-res standalone scan image (e.g. 8000 px) is the exception —
   it skips normalization and uses more RAM/time. **Prefer multi-page PDFs over giant single images.**

Memory stays **bounded per page**: pages are rendered and OCR'd one at a time in an isolated
subprocess (with OOM handling), so a 1000-page file never loads all pages into RAM at once, and
one bad page won't kill the whole job.

---

## 10. Operations

- **Backups (only two stateful things):** the `pgdata` Docker volume (database) and the
  `uploads` directory (source scans). Back up both.
  ```bash
  docker compose exec -T db pg_dump -U postgres university_ocr | gzip > backup_$(date +%F).sql.gz
  ```
- **One worker per database.** Don't also run the host venv worker (`run_local_worker.ps1/.sh`)
  against the same DB — pick one.
- **Restart policy** is `unless-stopped` on all services, and the worker self-heals stuck jobs,
  so the queue survives reboots/crashes unattended.
- **Logs:** `docker compose logs -f <service>`.

---

## 11. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `could not select device driver "nvidia"` | NVIDIA Container Toolkit not configured — redo §4.2 |
| `torch.cuda.is_available()` is `False` | Driver too old for Blackwell (need ≥ 570), or the GPU image wasn't used — rebuild with `Dockerfile.gpu` (§4/§7); it keeps running on CPU meanwhile |
| Worker runs but processes nothing | Backend must have `MOCK_WORKER=false` (it does); check `docker compose logs python-worker` for dequeue lines |
| Upload fails on a large file | Raise `LimitRequestBody`/`Timeout` in the Apache vhost (§8) |
| Worker OOM-killed on a big job | Lower `python-worker` `mem_limit` pressure / add host RAM; per-page subprocess isolation logs the page and continues |
| `Migration ... failed` | Ensure `db` is healthy first (`docker compose ps db`) and the `DATABASE_URL` password matches §5 |
