"""Render every page of 001.pdf at 150 DPI (matching engine.py) and cache raw PaddleOCR cells to JSON.
Run once; the eval harness then iterates on post-processing without re-running OCR.
"""
import os, sys, json, time
import fitz

PDF = r"C:\Users\badal.dave\Downloads\001.pdf"
DPI = int(os.environ.get("CACHE_DPI", "150"))
OUT = os.path.join(os.path.dirname(__file__), f"ocr_cache_{DPI}.json")
PNG_DIR = os.path.join(os.path.dirname(__file__), f"pages_{DPI}")
os.makedirs(PNG_DIR, exist_ok=True)

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "python-worker"))
from app.ocr.paddle import PaddleOCRProvider

def main():
    t0 = time.time()
    ocr = PaddleOCRProvider()
    assert ocr.is_available(), "PaddleOCR not available"
    print(f"[cache] engine loaded in {time.time()-t0:.1f}s", flush=True)

    doc = fitz.open(PDF)
    n = len(doc)
    result = {"dpi": DPI, "pages": {}}
    # resume support
    if os.path.exists(OUT):
        try:
            result = json.load(open(OUT))
            print(f"[cache] resuming, {len(result['pages'])} pages already cached", flush=True)
        except Exception:
            pass

    for i in range(n):
        pno = i + 1
        if str(pno) in result["pages"]:
            continue
        page = doc.load_page(i)
        pix = page.get_pixmap(dpi=DPI)
        png = os.path.join(PNG_DIR, f"page_{pno}.png")
        pix.save(png)
        t = time.time()
        res = ocr.extract_table(png)
        result["pages"][str(pno)] = {
            "width": pix.width, "height": pix.height,
            "cells": res.get("cells", []),
        }
        if pno % 5 == 0 or pno == n:
            json.dump(result, open(OUT, "w"))
            print(f"[cache] page {pno}/{n}  ({time.time()-t:.1f}s, {len(res.get('cells',[]))} cells)", flush=True)

    json.dump(result, open(OUT, "w"))
    print(f"[cache] DONE {n} pages in {time.time()-t0:.1f}s -> {OUT}", flush=True)

if __name__ == "__main__":
    main()
