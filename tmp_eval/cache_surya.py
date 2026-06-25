"""Cache Surya OCR of each row's handwritten (name + mobile) strip for all pages.
Uses Paddle-derived row anchors for cropping. One-time slow run (~1h CPU);
the harness then iterates name/mobile parsing without re-running Surya.
"""
import os, sys, json, time, copy
HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
from PIL import Image
from app.pipeline.engine import ProcessingEngine

from surya.detection import DetectionPredictor
from surya.foundation import FoundationPredictor
from surya.recognition import RecognitionPredictor
from surya.common.surya.schema import TaskNames

PADDLE = os.path.join(HERE, "ocr_cache_150.json")
OUT = os.path.join(HERE, "ocr_cache_surya.json")
PNG_DIR = os.path.join(HERE, "pages_150")
X_LEFT = 0.40   # left edge of handwritten area (after subcode+batch)

import numpy as np

def anchors_y(eng, page):
    # Keep ALL batch-anchor slots (drop_empty=False) so Surya can read rows that
    # Paddle missed entirely; blank slots are filtered later by ink presence.
    rows = eng._align_coordinates(copy.deepcopy(page["cells"]), page["width"], page["height"], drop_empty=False)["tables"][0]["rows"]
    return [r["cells"][1]["bbox"]["y"] + r["cells"][1]["bbox"]["height"]/2.0 for r in rows]

def has_ink(img, ay, sp, W, H):
    """True if the row's name band contains handwriting. Measured on a tight band
    (x 0.45-0.96, y +-0.32*spacing) to avoid the printed row-rule lines."""
    band = img.crop((int(0.45*W), int(max(0, ay-0.32*sp)), int(0.96*W), int(min(H, ay+0.32*sp))))
    arr = np.asarray(band.convert("L"))
    return float((arr < 140).mean()) >= 0.02

def main():
    t0 = time.time()
    fp = FoundationPredictor(); det = DetectionPredictor(); rec = RecognitionPredictor(fp)
    print("surya load %.1fs" % (time.time()-t0), flush=True)
    eng = ProcessingEngine()
    cache = json.load(open(PADDLE))
    out = {"pages": {}}
    if os.path.exists(OUT):
        try:
            out = json.load(open(OUT)); print("resume", len(out["pages"]), "pages", flush=True)
        except Exception:
            pass

    pnos = sorted(cache["pages"], key=lambda x: int(x))
    for pno in pnos:
        if pno in out["pages"]:
            continue
        page = cache["pages"][pno]
        W, H = page["width"], page["height"]
        ys = anchors_y(eng, page)
        if not ys:
            out["pages"][pno] = []
            continue
        diffs = sorted(b-a for a, b in zip(ys, ys[1:]) if b-a > 5)
        sp = diffs[len(diffs)//2] if diffs else 90.0
        half = sp * 0.62
        img = Image.open(os.path.join(PNG_DIR, f"page_{pno}.png"))
        rows_out = [{"y": ay, "lines": []} for ay in ys]
        crops, idxs, boxes = [], [], []
        for i, ay in enumerate(ys):
            if not has_ink(img, ay, sp, W, H):
                continue
            box = (int(X_LEFT*W), int(max(0, ay-half)), int(0.99*W), int(min(H, ay+half)))
            crops.append(img.crop(box)); idxs.append(i); boxes.append(box)
        t = time.time()
        if crops:
            res = rec(crops, task_names=[TaskNames.ocr_with_boxes]*len(crops), det_predictor=det)
            for i, box, r in zip(idxs, boxes, res):
                rows_out[i]["lines"] = [{"t": l.text, "x": float(l.bbox[0]) + box[0], "y": float(l.bbox[1]) + box[1]}
                                        for l in r.text_lines]
        out["pages"][pno] = rows_out
        if int(pno) % 5 == 0 or pno == pnos[-1]:
            json.dump(out, open(OUT, "w"))
        print(f"page {pno}: {len(crops)} rows {time.time()-t:.1f}s  (total {time.time()-t0:.0f}s, {len(out['pages'])}/{len(pnos)})", flush=True)

    json.dump(out, open(OUT, "w"))
    print(f"DONE {len(out['pages'])} pages in {time.time()-t0:.0f}s", flush=True)

if __name__ == "__main__":
    main()
