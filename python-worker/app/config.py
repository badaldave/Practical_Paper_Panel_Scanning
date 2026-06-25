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

    # Self-healing: jobs left in 'processing' longer than this (a worker that died
    # mid-job) are reclaimed and re-queued so they never stay stuck.
    JOB_STALE_TIMEOUT_MINUTES = int(os.getenv("JOB_STALE_TIMEOUT_MINUTES", "30"))
    # How often (seconds) the worker scans for stale jobs to reclaim.
    REAP_INTERVAL_SECONDS = int(os.getenv("REAP_INTERVAL_SECONDS", "60"))
    
    # OCR Provider execution priority (default order: surya, paddle, doctr, tesseract)
    OCR_PRIORITY = os.getenv("OCR_PRIORITY", "surya,paddle,doctr,tesseract").split(",")
