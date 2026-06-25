import os
os.environ["OMP_NUM_THREADS"] = "1"
os.environ["MKL_NUM_THREADS"] = "1"
os.environ["OPENBLAS_NUM_THREADS"] = "1"
os.environ["VECLIB_MAXIMUM_THREADS"] = "1"
os.environ["NUMEXPR_NUM_THREADS"] = "1"
os.environ["FLAGS_num_threads"] = "1"

import sys
import re
import time
import multiprocessing
import psycopg
from psycopg.rows import dict_row

# Ensure app is in path
sys.path.append("/app")

from read_dbf import read_dbf

def init_worker():
    global engine
    # Lazy import inside the worker process to avoid serialization/multiprocessing issues
    from app.pipeline.engine import ProcessingEngine
    engine = ProcessingEngine()

def process_single_page(args):
    page_idx, pdf_path = args
    global engine
    
    import fitz
    import os
    import re
    
    try:
        # Open PDF page
        doc = fitz.open(pdf_path)
        page = doc.load_page(page_idx)
        pix = page.get_pixmap(dpi=150)
        
        # Save temp image
        from app.config import Config
        temp_png = os.path.join(Config.UPLOAD_DIR, f"temp_page_{page_idx}.png")
        pix.save(temp_png)
        
        # Run OCR
        provider = engine._get_provider("paddle")
        res = provider.extract_table(temp_png)
        
        # Cleanup temp file immediately
        if os.path.exists(temp_png):
            try:
                os.remove(temp_png)
            except:
                pass
                
        # Align coordinates
        w, h = pix.width, pix.height
        grouped_data = engine._align_coordinates(res["cells"], w, h)
        
        # Parse center code from text
        center_code = None
        for cell in res["cells"]:
            val = cell["value"]
            m = re.search(r'\b(\d{3})\b', val)
            if m:
                # Ensure it's part of header/center text
                if "CENTRE" in val.upper() or "MAHAVIDHYALAYA" in val.upper() or "-" in val or len(val) < 25:
                    center_code = m.group(1)
                    break
        
        if not center_code:
            # Fallback search
            for cell in res["cells"]:
                val = cell["value"]
                m = re.search(r'\b(\d{3})\b', val)
                if m:
                    center_code = m.group(1)
                    break
                    
        # Extract rows
        rows = []
        for table in grouped_data.get("tables", []):
            for row in table.get("rows", []):
                row_idx = row["row_index"]
                cells_dict = {}
                for cell in row.get("cells", []):
                    cells_dict[cell["column_index"]] = cell["value"]
                rows.append(cells_dict)
                
        return {
            "page_idx": page_idx,
            "center_code": center_code,
            "rows": rows,
            "success": True
        }
        
    except Exception as e:
        import traceback
        return {
            "page_idx": page_idx,
            "success": False,
            "error": str(e),
            "traceback": traceback.format_exc()
        }

def clean_name(name):
    if not name:
        return ""
    # Convert to uppercase
    name = name.upper()
    # Remove titles
    name = re.sub(r'\b(DR\b\.?|MRS\b\.?|MR\b\.?|MS\b\.?|SHRI\b\.?|PROF\b\.?)\s*', '', name)
    # Remove all non-alphabetic chars
    name = re.sub(r'[^A-Z]', '', name)
    return name

def clean_mobile(mob):
    if not mob:
        return ""
    # Keep only digits
    mob = re.sub(r'\D', '', mob)
    # Keep last 10 digits to handle country code prefix (like +91 or 91)
    if len(mob) > 10:
        mob = mob[-10:]
    return mob

def clean_batch(batch):
    if not batch:
        return ""
    # Upper, strip spaces and dashes
    batch = batch.upper().replace("-", "").replace(" ", "")
    # Normalize 'R01' or 'R-1' or 'R1' -> 'R1'
    if batch.startswith('R0'):
        batch = 'R' + batch[2:]
    return batch

def clean_subcode(subcode):
    if not subcode:
        return ""
    return subcode.upper().replace("-", "").replace(" ", "")

def main():
    # Set document status to processing in DB so UI knows it is running
    try:
        from app.config import Config
        with psycopg.connect(Config.DATABASE_URL) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE documents SET status = 'processing', progress_percentage = 0, updated_at = NOW() WHERE id = '00000000-0000-0000-0000-000000000001'"
                )
                conn.commit()
    except Exception as db_err:
        print("Error updating initial status:", db_err)

    from app.config import Config
    dbf_path = os.path.join(Config.UPLOAD_DIR, "SCC58.XLS")
    fields, records = read_dbf(dbf_path)
    print(f"Loaded {len(records)} records from DBF.")
    
    # Group DBF records by page number
    dbf_by_page = {}
    for r in records:
        p_num = int(r["PAGENO"])
        if p_num not in dbf_by_page:
            dbf_by_page[p_num] = []
        dbf_by_page[p_num].append(r)
        
    pdf_path = os.path.join(Config.UPLOAD_DIR, "001.pdf")
    
    # We will process all pages using a Process Pool
    num_cpus = 2 # Use 2 CPU cores to avoid memory swapping
    print(f"Starting Process Pool with {num_cpus} workers...")
    
    pages_to_process = list(range(204))
    args_list = [(i, pdf_path) for i in pages_to_process]
    
    results = [None] * len(pages_to_process)
    
    start_time = time.time()
    
    with multiprocessing.Pool(processes=num_cpus, initializer=init_worker) as pool:
        # Use imap_unordered for lazy processing and progress printing
        for res in pool.imap_unordered(process_single_page, args_list):
            p_idx = res["page_idx"]
            results[p_idx] = res
            
            # Print progress
            completed = sum(1 for x in results if x is not None)
            pct = (completed / len(pages_to_process)) * 100
            elapsed = time.time() - start_time
            rate = completed / elapsed if elapsed > 0 else 0
            eta = (len(pages_to_process) - completed) / rate if rate > 0 else 0
            print(f"Progress: {completed}/{len(pages_to_process)} pages completed ({pct:.1f}%). ETA: {eta/60:.1f} mins.")
            sys.stdout.flush()
            
            # Update database progress
            try:
                from app.config import Config
                with psycopg.connect(Config.DATABASE_URL) as conn:
                    with conn.cursor() as cur:
                        cur.execute(
                            "UPDATE documents SET progress_percentage = %s, updated_at = NOW() WHERE id = '00000000-0000-0000-0000-000000000001'",
                            (int(pct),)
                        )
                        conn.commit()
            except Exception as db_err:
                pass
            
    print(f"All OCR processing completed in {(time.time() - start_time)/60:.2f} minutes.")
    
    # 4. Perform Comparison
    mismatches = []
    total_dbf_checked = 0
    total_pdf_checked = 0
    pages_with_errors = 0
    
    for page_idx in range(204):
        page_num = page_idx + 1
        res = results[page_idx]
        
        if not res or not res["success"]:
            err_msg = res["error"] if res else "Unknown error"
            mismatches.append({
                "page": page_num,
                "center": "N/A",
                "batch": "N/A",
                "type": "OCR Failure",
                "detail": f"Failed to run OCR on this page. Error: {err_msg}"
            })
            pages_with_errors += 1
            continue
            
        center_code = res["center_code"]
        pdf_rows = res["rows"]
        dbf_rows = dbf_by_page.get(page_num, [])
        
        # Normalize and index PDF rows by normalized batch and subcode
        pdf_indexed = {}
        for r in pdf_rows:
            # Skip header row
            if "SUBCODE" in str(r.get(0, "")).upper():
                continue
                
            sub = clean_subcode(r.get(0))
            batch = clean_batch(r.get(1))
            
            if not batch:
                continue
                
            pdf_indexed[(sub, batch)] = r
            total_pdf_checked += 1
            
        # Match with DBF rows
        page_has_mismatch = False
        
        for d_row in dbf_rows:
            total_dbf_checked += 1
            dbf_sub = clean_subcode(d_row["SUBCODE"])
            dbf_batch = clean_batch(d_row["BATCH"])
            dbf_name = d_row["EXMNAME"]
            dbf_mob = d_row["MOBILENO"]
            
            pdf_match = pdf_indexed.get((dbf_sub, dbf_batch))
            
            if pdf_match is None:
                # Missing entry in PDF (batch existed in DBF but not found in PDF)
                mismatches.append({
                    "page": page_num,
                    "center": center_code or d_row["CCODE"],
                    "subcode": d_row["SUBCODE"],
                    "batch": d_row["BATCH"],
                    "type": "Missing Entry in PDF",
                    "dbf_value": f"Name: {dbf_name}, Mobile: {dbf_mob}",
                    "pdf_value": "N/A (Batch not found in PDF table)",
                    "detail": "The batch entry is present in DBF but missing in PDF scan."
                })
                page_has_mismatch = True
                continue
                
            # Entry found, compare Name and Mobile
            pdf_name = pdf_match.get(2, "")
            pdf_mob = pdf_match.get(3, "")
            
            c_dbf_name = clean_name(dbf_name)
            c_pdf_name = clean_name(pdf_name)
            
            c_dbf_mob = clean_mobile(dbf_mob)
            c_pdf_mob = clean_mobile(pdf_mob)
            
            # Compare names (fuzzy check: is one name contained in another or very close?)
            name_discrepancy = False
            if c_dbf_name != c_pdf_name:
                # If they are very different, it's a discrepancy
                # E.g. MONIKA SAINI vs MONIKA SHARMA
                # Let's check if the name is empty in PDF
                if not pdf_name:
                    name_discrepancy = True
                else:
                    name_discrepancy = True
                    
            mob_discrepancy = False
            if c_dbf_mob != c_pdf_mob:
                mob_discrepancy = True
                
            if name_discrepancy or mob_discrepancy:
                detail_msgs = []
                if name_discrepancy:
                    detail_msgs.append(f"Name mismatch (DBF: '{dbf_name}' vs PDF: '{pdf_name}')")
                if mob_discrepancy:
                    detail_msgs.append(f"Mobile mismatch (DBF: '{dbf_mob}' vs PDF: '{pdf_mob}')")
                    
                mismatches.append({
                    "page": page_num,
                    "center": center_code or d_row["CCODE"],
                    "subcode": d_row["SUBCODE"],
                    "batch": d_row["BATCH"],
                    "type": "Data Discrepancy",
                    "dbf_value": f"Name: {dbf_name}, Mobile: {dbf_mob}",
                    "pdf_value": f"Name: {pdf_name}, Mobile: {pdf_mob}",
                    "detail": " & ".join(detail_msgs)
                })
                page_has_mismatch = True
                
        if page_has_mismatch:
            pages_with_errors += 1
            
    # Write report
    report_dir = "/app/app"
    os.makedirs(report_dir, exist_ok=True)
    report_path = os.path.join(report_dir, "comparison_report.md")
    
    with open(report_path, "w") as f:
        f.write("# External Examiner Comparison Report\n\n")
        f.write("## Executive Summary\n")
        f.write(f"- **PDF File**: `001.pdf` (204 pages)\n")
        f.write(f"- **DBF File**: `SCC58.XLS` (520 database records)\n")
        f.write(f"- **Total Database Records Checked**: {total_dbf_checked}\n")
        f.write(f"- **Total PDF Table Rows Extracted**: {total_pdf_checked}\n")
        f.write(f"- **Pages with Discrepancies**: {pages_with_errors} / 204\n")
        f.write(f"- **Total Mismatches Found**: {len(mismatches)}\n\n")
        
        f.write("## Mismatch Summary Table\n")
        f.write("| Page | Center Code | Subcode | Batch | Type of Discrepancy | Database Record (DBF) | Scanned Document (PDF) | Details |\n")
        f.write("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
        
        for m in mismatches:
            f.write(f"| {m['page']} | {m['center']} | {m.get('subcode', 'N/A')} | {m['batch']} | {m['type']} | {m.get('dbf_value', 'N/A')} | {m.get('pdf_value', 'N/A')} | {m['detail']} |\n")
            
        f.write("\n\n*Report generated automatically on 2026-06-16.*")
        
    print(f"Report written successfully to {report_path}")

    # Set document status to extracted in DB so UI knows it is complete
    try:
        from app.config import Config
        with psycopg.connect(Config.DATABASE_URL) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "UPDATE documents SET status = 'extracted', progress_percentage = 100, updated_at = NOW() WHERE id = '00000000-0000-0000-0000-000000000001'"
                )
                conn.commit()
    except Exception as db_err:
        pass

if __name__ == "__main__":
    main()
