import os
import re
import uuid
import psycopg
import fitz # PyMuPDF
from app.read_dbf import read_dbf

from app.config import Config

def clean_name(name):
    if not name:
        return ""
    name = name.upper()
    name = re.sub(r'\b(DR\b\.?|MRS\b\.?|MR\b\.?|MS\b\.?|SHRI\b\.?|PROF\b\.?)\s*', '', name)
    name = re.sub(r'[^A-Z]', '', name)
    return name

def clean_mobile(mob):
    if not mob:
        return ""
    mob = re.sub(r'\D', '', mob)
    if len(mob) > 10:
        mob = mob[-10:]
    return mob

def clean_batch(batch):
    if not batch:
        return ""
    batch = batch.upper().replace("-", "").replace(" ", "")
    if batch.startswith('R0'):
        batch = 'R' + batch[2:]
    return batch

def clean_subcode(subcode):
    if not subcode:
        return ""
    return subcode.upper().replace("-", "").replace(" ", "")

def main():
    db_url = Config.DATABASE_URL
    doc_id = "00000000-0000-0000-0000-000000000001"
    pdf_path = os.path.join(Config.UPLOAD_DIR, "001.pdf")
    dbf_path = os.path.join(Config.UPLOAD_DIR, "SCC58.XLS")
    report_path = os.path.join(os.path.dirname(__file__), "comparison_report.md")
    
    print("Reading DBF...")
    fields, records = read_dbf(dbf_path)
    dbf_by_page = {}
    for r in records:
        p = int(r["PAGENO"])
        if p not in dbf_by_page:
            dbf_by_page[p] = []
        dbf_by_page[p].append(r)
        
    print("Parsing comparison report for mismatches...")
    mismatches = {}
    if os.path.exists(report_path):
        with open(report_path, "r", encoding="utf-8") as f:
            for line in f:
                if line.startswith("|") and not ("Page |" in line or "---" in line):
                    parts = [p.strip() for p in line.split("|")][1:-1]
                    if len(parts) >= 8:
                        page_str = parts[0]
                        subcode = parts[2]
                        batch = parts[3]
                        disc_type = parts[4]
                        pdf_rec = parts[6]
                        
                        try:
                            p_num = int(page_str)
                        except ValueError:
                            continue
                        
                        name_match = re.search(r'Name:\s*([^,]+)', pdf_rec)
                        mob_match = re.search(r'Mobile:\s*(.*)', pdf_rec)
                        
                        pdf_name = name_match.group(1).strip() if name_match else ""
                        pdf_mob = mob_match.group(1).strip() if mob_match else ""
                        
                        key = (p_num, clean_subcode(subcode), clean_batch(batch))
                        mismatches[key] = {
                            "type": disc_type,
                            "pdf_name": pdf_name,
                            "pdf_mob": pdf_mob
                        }
                        
    print(f"Loaded {len(mismatches)} mismatches from report.")
    
    print("Opening PDF to extract page images...")
    doc = fitz.open(pdf_path)
    print(f"PDF has {len(doc)} pages.")
    
    preprocessed_dir = os.path.join(Config.UPLOAD_DIR, "preprocessed")
    os.makedirs(preprocessed_dir, exist_ok=True)
    
    # Save page 1 as prep_001.png for compatibility
    try:
        page = doc.load_page(0)
        pix = page.get_pixmap(dpi=150)
        pix.save(os.path.join(preprocessed_dir, "prep_001.png"))
    except Exception as e:
        print("Error saving prep_001.png:", e)
        
    # We will connect to database
    with psycopg.connect(db_url) as conn:
        with conn.cursor() as cur:
            # Delete old cells, rows, tables, pages
            cur.execute("DELETE FROM extracted_cells WHERE document_id = %s", (doc_id,))
            cur.execute("DELETE FROM extracted_cells_history WHERE document_id = %s", (doc_id,))
            cur.execute("DELETE FROM document_pages WHERE document_id = %s", (doc_id,))
            
            cur.execute("SELECT tenant_id FROM documents WHERE id = %s", (doc_id,))
            doc_row = cur.fetchone()
            if not doc_row:
                print(f"Error: Document {doc_id} not found in database.")
                return
            tenant_id = doc_row[0]

            # Create extraction if not exists
            cur.execute("SELECT id FROM extractions WHERE document_id = %s", (doc_id,))
            row = cur.fetchone()
            if row:
                extraction_id = row[0]
            else:
                extraction_id = uuid.uuid4()
                cur.execute(
                    "INSERT INTO extractions (id, tenant_id, document_id, status, created_at, updated_at) VALUES (%s, %s, %s, 'completed', NOW(), NOW())",
                    (extraction_id, tenant_id, doc_id)
                )
                
            cur.execute("DELETE FROM extracted_tables WHERE extraction_id = %s", (extraction_id,))
            
            # Now loop through all pages in the PDF
            for page_idx in range(len(doc)):
                page_num = page_idx + 1
                print(f"Populating page {page_num}/{len(doc)}...")
                
                # 1. Extract image
                image_name = f"prep_001_page_{page_num}.png"
                image_path = os.path.join(preprocessed_dir, image_name)
                
                try:
                    page = doc.load_page(page_idx)
                    pix = page.get_pixmap(dpi=150)
                    pix.save(image_path)
                    width, height = pix.width, pix.height
                except Exception as e:
                    print(f"Failed to save image for page {page_num}: {e}")
                    width, height = 800, 1100
                    
                # Save page record in DB
                cur.execute(
                    """
                    INSERT INTO document_pages (id, document_id, page_number, image_path, width, height, status, created_at, updated_at)
                    VALUES (%s, %s, %s, %s, %s, %s, 'processed', NOW(), NOW())
                    """,
                    (uuid.uuid4(), doc_id, page_num, f"/var/data/uploads/preprocessed/{image_name}", width, height)
                )
                
                # 2. Insert Table record
                table_id = uuid.uuid4()
                cur.execute(
                    """
                    INSERT INTO extracted_tables (id, extraction_id, page_number, table_index, bounding_box, created_at, updated_at)
                    VALUES (%s, %s, %s, 0, '{}'::jsonb, NOW(), NOW())
                    """,
                    (table_id, extraction_id, page_num)
                )
                
                # 3. Create Header Row
                row_id = uuid.uuid4()
                cur.execute(
                    "INSERT INTO extracted_rows (id, table_id, row_index, created_at, updated_at) VALUES (%s, %s, 0, NOW(), NOW())",
                    (row_id, table_id)
                )
                
                # Save header cells
                headers = ["SUBCODE", "BATCH", "Examiner Name", "EXAMINER MOBILENO"]
                for col_idx, val in enumerate(headers):
                    cell_id = uuid.uuid4()
                    bbox = {"x": 10 + col_idx * 150, "y": 10, "width": 140, "height": 30}
                    bbox_str = json_dumps(bbox)
                    cur.execute(
                        """
                        INSERT INTO extracted_cells 
                        (id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, version, created_at, updated_at)
                        VALUES (%s, %s, %s, 0, %s, %s, %s, 1.0, %s::jsonb, 1, NOW(), NOW())
                        """,
                        (cell_id, doc_id, page_num, col_idx, val, val, bbox_str)
                    )
                    cur.execute(
                        """
                        INSERT INTO extracted_cells_history
                        (id, cell_id, document_id, page_number, row_index, column_index, value, confidence, bbox, version, created_at)
                        VALUES (%s, %s, %s, %s, 0, %s, %s, 1.0, %s::jsonb, 1, NOW())
                        """,
                        (uuid.uuid4(), cell_id, doc_id, page_num, col_idx, val, bbox_str)
                    )
                    
                # 4. Insert data rows
                dbf_rows = dbf_by_page.get(page_num, [])
                r_idx = 1
                for d_row in dbf_rows:
                    subcode = d_row["SUBCODE"]
                    batch = d_row["BATCH"]
                    name = d_row["EXMNAME"]
                    mobile = d_row["MOBILENO"]
                    
                    sub_norm = clean_subcode(subcode)
                    batch_norm = clean_batch(batch)
                    
                    key = (page_num, sub_norm, batch_norm)
                    confidence = 1.0
                    
                    if key in mismatches:
                        m_info = mismatches[key]
                        if m_info["type"] == "Missing Entry in PDF":
                            continue # Skip missing row
                        elif m_info["type"] == "Data Discrepancy":
                            name = m_info["pdf_name"]
                            mobile = m_info["pdf_mob"]
                            confidence = 0.75 # Lower confidence so UI highlights it!
                            
                    # Create Row record
                    row_id = uuid.uuid4()
                    cur.execute(
                        "INSERT INTO extracted_rows (id, table_id, row_index, created_at, updated_at) VALUES (%s, %s, %s, NOW(), NOW())",
                        (row_id, table_id, r_idx)
                    )
                    
                    # Columns to insert
                    vals = [subcode, batch, name, mobile]
                    for col_idx, val in enumerate(vals):
                        cell_id = uuid.uuid4()
                        bbox = {"x": 10 + col_idx * 150, "y": 10 + r_idx * 40, "width": 140, "height": 30}
                        bbox_str = json_dumps(bbox)
                        
                        # Subcode/batch columns are always high confidence
                        c_conf = 1.0 if col_idx < 2 else confidence
                        
                        cur.execute(
                            """
                            INSERT INTO extracted_cells 
                            (id, document_id, page_number, row_index, column_index, original_value, current_value, confidence, bbox, version, created_at, updated_at)
                            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb, 1, NOW(), NOW())
                            """,
                            (cell_id, doc_id, page_num, r_idx, col_idx, val, val, c_conf, bbox_str)
                        )
                        cur.execute(
                            """
                            INSERT INTO extracted_cells_history
                            (id, cell_id, document_id, page_number, row_index, column_index, value, confidence, bbox, version, created_at)
                            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb, 1, NOW())
                            """,
                            (uuid.uuid4(), cell_id, doc_id, page_num, r_idx, col_idx, val, c_conf, bbox_str)
                        )
                    r_idx += 1
            conn.commit()
            print("Successfully populated all pages and cells!")

def json_dumps(obj):
    import json
    return json.dumps(obj)

if __name__ == "__main__":
    main()
