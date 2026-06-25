import time
import os
import fitz
from app.pipeline.engine import ProcessingEngine

print("Initializing ProcessingEngine (this loads models)...")
start_init = time.time()
engine = ProcessingEngine()
# Force load paddle provider
provider = engine._get_provider("paddle")
print(f"Initialization took {time.time() - start_init:.2f} seconds.")

pdf_path = "/var/data/uploads/001.pdf"
doc = fitz.open(pdf_path)
print(f"PDF loaded. Total pages: {len(doc)}")

# Run OCR on first 5 pages
for i in range(5):
    print(f"\nProcessing page {i+1}...")
    start_page = time.time()
    
    # Render page to image
    page = doc.load_page(i)
    pix = page.get_pixmap(dpi=150)
    temp_png = f"/var/data/uploads/temp_page_{i}.png"
    pix.save(temp_png)
    
    # Run OCR
    res = provider.extract_table(temp_png)
    print(f"Page {i+1} OCR took {time.time() - start_page:.2f} seconds.")
    print(f"Found {len(res['cells'])} cells.")
    
    # Cleanup
    if os.path.exists(temp_png):
        os.remove(temp_png)
