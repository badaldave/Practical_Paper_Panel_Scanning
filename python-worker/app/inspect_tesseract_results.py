import time
import os
import fitz
from app.pipeline.engine import ProcessingEngine
import re

engine = ProcessingEngine()
# Force load tesseract provider
provider = engine._get_provider("tesseract")

pdf_path = "/var/data/uploads/001.pdf"
doc = fitz.open(pdf_path)

# Process page 1 (0-indexed)
page = doc.load_page(0)
pix = page.get_pixmap(dpi=150)
temp_png = "/var/data/uploads/temp_page_0.png"
pix.save(temp_png)

res = provider.extract_table(temp_png)

# Perform coordinate alignment using our updated engine methods (using tesseract results)
img_w, img_h = 1000, 1500 # rough numbers or extract from page
img = fitz.open(pdf_path)
page = img[0]
img_w = int(page.rect.width * 150 / 72)
img_h = int(page.rect.height * 150 / 72)

grouped_data = engine._align_coordinates(res["cells"], img_w, img_h)

print("Tesseract Extracted Table for Page 1:")
print("="*80)
for table in grouped_data.get("tables", []):
    for row in table.get("rows", []):
        cell_strs = []
        for cell in row.get("cells", []):
            cell_strs.append(f"Col {cell['column_index']}: '{cell['value']}' (conf: {float(cell['confidence']):.2f})")
        print(f"Row {row['row_index']}: " + " | ".join(cell_strs))
print("="*80)

if os.path.exists(temp_png):
    os.remove(temp_png)
