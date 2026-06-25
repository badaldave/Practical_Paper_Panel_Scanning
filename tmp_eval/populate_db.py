"""Write the improved pipeline's extraction of 001.pdf into the (wiped) DB.
Uses the cached Paddle + Surya OCR (identical to a live run) and the real
engine post-processing + WorkerRepository, so the DB ends up exactly as the
worker would have produced it -- without re-running the ~80 min of OCR.
"""
import os, sys, json, copy, uuid, shutil
from datetime import datetime
HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
os.environ.setdefault("DATABASE_URL", "postgresql://postgres:postgres_secure_db_pass_2026@localhost:5439/university_ocr")

import psycopg
from app.config import Config
from app.db.repository import WorkerRepository
from app.pipeline.engine import ProcessingEngine

TENANT = "e93fca1e-1f7c-47bc-87c2-127e7740e53a"
USER = "c869fb1e-cfa1-4560-9bb3-5bb28e2195f2"
PDF = r"C:\Users\badal.dave\Downloads\001.pdf"
UPLOAD = os.path.abspath(os.path.join(HERE, "..", "go-backend", "uploads"))

paddle = json.load(open(os.path.join(HERE, "ocr_cache_150.json")))["pages"]
surya = json.load(open(os.path.join(HERE, "ocr_cache_surya.json")))["pages"]
eng = ProcessingEngine()

def surya_rows_for(pno):
    return surya.get(str(pno), [])

def build_tables(pno):
    page = paddle[str(pno)]; W, H = page["width"], page["height"]
    grouped = eng._align_coordinates(copy.deepcopy(page["cells"]), W, H, drop_empty=False)
    rows = grouped["tables"][0]["rows"]
    srows = surya_rows_for(pno)
    sys_ys = sorted(sr["y"] for sr in srows)
    sd = sorted(b - a for a, b in zip(sys_ys, sys_ys[1:]) if b - a > 5)
    sp = sd[len(sd)//2] if sd else 90.0
    half, tol = sp * 0.62, sp * 0.42
    for r in rows:
        cm = {c["column_index"]: c for c in r["cells"]}
        ay = cm[1]["bbox"]["y"] + cm[1]["bbox"]["height"]/2.0
        sr = min(srows, key=lambda s: abs(s["y"] - ay), default=None)
        if sr and abs(sr["y"] - ay) <= 40:
            name, mob = eng._parse_surya_lines(sr["lines"], sr["y"], tol)
            if name:
                cm[2]["value"] = name; cm[2]["confidence"] = 0.85
                cm[2]["bbox"] = {"x": 0.40*W, "y": ay-half, "width": 0.33*W, "height": 2*half}
            if mob:
                cm[3]["value"] = mob; cm[3]["confidence"] = 0.85
                cm[3]["bbox"] = {"x": 0.73*W, "y": ay-half, "width": 0.26*W, "height": 2*half}
    # drop empty slots
    import re
    kept = []
    for r in rows:
        cm = {c["column_index"]: c for c in r["cells"]}
        if len(re.sub(r"[^A-Za-z]", "", cm[2]["value"])) >= 2 or len(re.sub(r"\D", "", cm[3]["value"])) >= 6:
            kept.append(r)
    for i, r in enumerate(kept):
        r["row_index"] = i
    grouped["tables"][0]["rows"] = kept
    grouped["tables"][0]["page_number"] = pno
    return grouped["tables"], W, H

def main():
    doc_id = str(uuid.uuid4())
    # copy PDF into uploads and create the document row
    dest_dir = os.path.join(UPLOAD, TENANT); os.makedirs(dest_dir, exist_ok=True)
    dest_pdf = os.path.join(dest_dir, doc_id + ".pdf")
    try: shutil.copyfile(PDF, dest_pdf)
    except Exception as e: print("copy warn:", e)
    prep_dir = os.path.join(dest_dir, doc_id, "preprocessed"); os.makedirs(prep_dir, exist_ok=True)

    with psycopg.connect(Config.DATABASE_URL) as conn:
        with conn.cursor() as cur:
            cur.execute("""INSERT INTO documents (id, tenant_id, name, file_path, file_size, mime_type, status, uploaded_by, progress_percentage)
                           VALUES (%s,%s,'001.pdf',%s,%s,'application/pdf','extracted',%s,100)""",
                        (doc_id, TENANT, f"uploads/{TENANT}/{doc_id}.pdf", os.path.getsize(PDF), USER))
        conn.commit()
    print("document:", doc_id)

    all_tables = []
    npages = len(paddle)
    for pno in range(1, npages + 1):
        tables, W, H = build_tables(pno)
        # copy page png + page record
        src_png = os.path.join(HERE, "pages_150", f"page_{pno}.png")
        img_db = f"/var/data/uploads/{TENANT}/{doc_id}/preprocessed/page_{pno}.png"
        try: shutil.copyfile(src_png, os.path.join(prep_dir, f"page_{pno}.png"))
        except Exception: pass
        meta = eng._extract_page_metadata(copy.deepcopy(paddle[str(pno)]["cells"]), W, H)
        WorkerRepository.save_page_record(doc_id, pno, img_db, W, H,
            college_code=meta.get("college_code"), college_name=meta.get("college_name"),
            subject_code=meta.get("subject_code"), subject_name=meta.get("subject_name"),
            faculty=meta.get("faculty"), total_candidates=meta.get("total_candidates"))
        all_tables.extend(tables)

    WorkerRepository.save_extractions(TENANT, doc_id, {"tables": all_tables})
    total_cells = sum(len(r["cells"]) for t in all_tables for r in t["rows"])
    print(f"saved {len(all_tables)} tables, {sum(len(t['rows']) for t in all_tables)} rows, {total_cells} cells")
    print("DOC_ID", doc_id)

if __name__ == "__main__":
    main()
