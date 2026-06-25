#!/usr/bin/env bash
# Linux launcher for the Python OCR worker — with auto-restart supervision.
# Mirror of run_local_worker.ps1 for Linux production hosts.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# DATABASE_URL: inside the worker host we reach Postgres on the published port.
# Override via env or python-worker/.env as needed for your prod DB.
export DATABASE_URL="${DATABASE_URL:-postgresql://postgres:postgres_secure_db_pass_2026@localhost:5439/university_ocr}"
export PYTHONPATH="$SCRIPT_DIR/python-worker"

# Make CUDA discoverable if present (NVIDIA driver + toolkit on the host).
if [ -d /usr/local/cuda/bin ]; then
    export PATH="/usr/local/cuda/bin:${PATH}"
    export LD_LIBRARY_PATH="/usr/local/cuda/lib64:${LD_LIBRARY_PATH:-}"
    echo "Injected /usr/local/cuda into PATH/LD_LIBRARY_PATH."
fi

cd "$SCRIPT_DIR/python-worker"

# Prefer the venv python; fall back to system python3.
if [ -x "./venv/bin/python" ]; then
    PY="./venv/bin/python"
else
    PY="python3"
fi

# Supervision loop: keep the daemon alive across crashes.
while true; do
    echo "Starting Python OCR Worker Daemon..."
    "$PY" -u app/main.py
    code=$?
    echo "Worker exited (code $code). Restarting in 5s..."
    sleep 5
done
