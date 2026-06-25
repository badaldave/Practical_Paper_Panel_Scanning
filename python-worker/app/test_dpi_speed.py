import time
import os
import fitz
from paddleocr import PaddleOCR

print("Loading PaddleOCR...")
ocr = PaddleOCR(use_angle_cls=False, lang='en', enable_mkldnn=False)

pdf_path = "/var/data/uploads/001.pdf"
doc = fitz.open(pdf_path)
page = doc.load_page(0)

for dpi in [75, 100, 120, 150]:
    print(f"\nTesting DPI {dpi}...")
    start = time.time()
    pix = page.get_pixmap(dpi=dpi)
    temp_png = f"/var/data/uploads/temp_dpi_{dpi}.png"
    pix.save(temp_png)
    
    start_ocr = time.time()
    res = ocr.ocr(temp_png)
    print(f"  DPI {dpi} OCR took {time.time() - start_ocr:.2f} seconds.")
    print(f"  Found {len(res[0]) if res else 0} boxes.")
    
    if os.path.exists(temp_png):
        os.remove(temp_png)
