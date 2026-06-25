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
    
    # OCR Provider execution priority (default order: surya, paddle, doctr, tesseract)
    OCR_PRIORITY = os.getenv("OCR_PRIORITY", "surya,paddle,doctr,tesseract").split(",")
