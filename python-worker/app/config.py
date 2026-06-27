import os
from dotenv import load_dotenv

load_dotenv()

class Config:
    DATABASE_URL = os.getenv(
        "DATABASE_URL", 
        "postgresql://postgres:postgres@localhost:5432/university_ocr"
    )
    UPLOAD_DIR = os.getenv("UPLOAD_DIR", "../go-backend/uploads")
    POLL_INTERVAL = int(os.getenv("POLL_INTERVAL", "5"))
    WORKER_ID = os.getenv("WORKER_ID", "python-ocr-worker-01")

    # Self-healing: jobs left in 'processing' whose lock has gone stale (a worker
    # that died mid-job) are reclaimed and re-queued so they never stay stuck.
    # Safe to keep short because an in-flight job is heart-beated (locked_at is
    # refreshed every HEARTBEAT_INTERVAL_SECONDS): a healthy slow job keeps its
    # lock fresh, so only a genuinely dead worker crosses this threshold. A worker
    # that crashes and restarts recovers its OWN job immediately (see
    # reclaim_own_jobs), independent of this timeout.
    JOB_STALE_TIMEOUT_MINUTES = int(os.getenv("JOB_STALE_TIMEOUT_MINUTES", "10"))
    # How often (seconds) the worker scans for stale jobs to reclaim.
    REAP_INTERVAL_SECONDS = int(os.getenv("REAP_INTERVAL_SECONDS", "60"))
    # How often (seconds) an in-flight job refreshes its lock so the reaper can
    # tell "slow but alive" from "dead". Must be well under JOB_STALE_TIMEOUT.
    HEARTBEAT_INTERVAL_SECONDS = int(os.getenv("HEARTBEAT_INTERVAL_SECONDS", "30"))
    
    # OCR Provider execution priority (default order: surya, paddle, doctr, tesseract)
    OCR_PRIORITY = os.getenv("OCR_PRIORITY", "surya,paddle,doctr,tesseract").split(",")
