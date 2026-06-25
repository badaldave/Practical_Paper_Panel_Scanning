import os
import re
import psycopg
import fitz  # PyMuPDF
from paddleocr import PaddleOCR

def parse_header_texts(lines):
    college_code = None
    college_name = None
    subject_code = None
    subject_name = None
    faculty = None
    total_candidates = None
    
    # 1. College Code & Name
    for line in lines:
        match = re.match(r'^(\d+)\s*-\s*(.*)$', line.strip())
        if match:
            college_code = match.group(1).strip()
            college_name = match.group(2).strip()
            break
            
    # 2. Faculty
    for line in lines:
        if "FACULTY OF" in line.upper():
            faculty = line.upper().replace("FACULTY OF", "").strip()
            break
            
    # 3. Total Candidates
    for idx, line in enumerate(lines):
        if "TOTAL CANDIDATE" in line.upper():
            match = re.search(r'TOTAL CANDIDATE\s*:?\s*(\d+)', line.upper())
            if match:
                total_candidates = int(match.group(1))
            elif idx + 1 < len(lines):
                next_line = lines[idx+1].strip()
                try:
                    total_candidates = int(next_line)
                except ValueError:
                    pass
            break
            
    # 4. Subject Code & Subject Name
    subject_code_candidates = []
    for line in lines:
        match = re.search(r'\b([A-Z]{3}-\w+-\d+)\b', line.strip())
        if match:
            subject_code_candidates.append(match.group(1))
            
    if subject_code_candidates:
        subject_code = min(subject_code_candidates, key=len)
        for line in lines:
            line_strip = line.strip()
            if line_strip.startswith(subject_code) and len(line_strip) > len(subject_code):
                subject_name = line_strip
                break
        if not subject_name:
            subject_name = subject_code
            
    return {
        "college_code": college_code,
        "college_name": college_name,
        "subject_code": subject_code,
        "subject_name": subject_name,
        "faculty": faculty,
        "total_candidates": total_candidates
    }

def main():
    db_url = "postgresql://postgres:postgres_secure_db_pass_2026@db:5432/university_ocr"
    pdf_path = "/var/data/uploads/001.pdf"
    
    print("Initializing PaddleOCR...")
    ocr = PaddleOCR(use_angle_cls=True, lang='en', enable_mkldnn=False)
    
    print(f"Opening PDF: {pdf_path}")
    doc = fitz.open(pdf_path)
    num_pages = len(doc)
    print(f"Total pages: {num_pages}")
    
    conn = psycopg.connect(db_url)
    cur = conn.cursor()
    
    temp_crop_path = "temp_crop_header.png"
    
    for page_num in range(1, num_pages + 1):
        print(f"\n--- Processing Page {page_num} / {num_pages} ---")
        page = doc.load_page(page_num - 1)
        
        # Crop top 350 points of the page
        rect = fitz.Rect(0, 0, page.rect.width, 350)
        pix = page.get_pixmap(clip=rect, dpi=150, alpha=False)
        pix.save(temp_crop_path)
        
        # Run OCR
        try:
            res = ocr.ocr(temp_crop_path)
            lines = []
            if res and res[0]:
                for line in res[0]:
                    text = line[1][0]
                    lines.append(text)
            
            print(f"Page {page_num} OCR text lines: {lines}")
            metadata = parse_header_texts(lines)
            print(f"Page {page_num} parsed metadata: {metadata}")
            
            # Update database
            cur.execute("""
                UPDATE document_pages
                SET college_code = %s,
                    college_name = %s,
                    subject_code = %s,
                    subject_name = %s,
                    faculty = %s,
                    total_candidates = %s,
                    updated_at = NOW()
                WHERE document_id = '00000000-0000-0000-0000-000000000001' AND page_number = %s
            """, (
                metadata["college_code"],
                metadata["college_name"],
                metadata["subject_code"],
                metadata["subject_name"],
                metadata["faculty"],
                metadata["total_candidates"],
                page_num
            ))
            conn.commit()
            print(f"Page {page_num} updated in DB successfully.")
            
        except Exception as e:
            print(f"Error processing page {page_num}: {e}")
            conn.rollback()
            
    cur.close()
    conn.close()
    if os.path.exists(temp_crop_path):
        os.remove(temp_crop_path)
    print("\nHeader extraction and database update complete!")

if __name__ == "__main__":
    main()
