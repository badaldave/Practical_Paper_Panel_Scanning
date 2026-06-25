import time
from paddleocr import PaddleOCR

print("Initializing PaddleOCR with use_textline_orientation=False...")
start = time.time()
ocr = PaddleOCR(use_textline_orientation=False, lang='en', enable_mkldnn=False)
print(f"Init took {time.time() - start:.2f} seconds.")

print("Running OCR on page 0...")
start_ocr = time.time()
res = ocr.ocr("/var/data/uploads/preprocessed/prep_001.png")
print(f"OCR took {time.time() - start_ocr:.2f} seconds.")
print(f"Found {len(res[0]) if res else 0} boxes.")
