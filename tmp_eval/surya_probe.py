"""Validate per-row Surya cropping: speed + quality on a few pages."""
import os, sys, json, time, copy
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "python-worker"))
from PIL import Image
from app.pipeline.engine import ProcessingEngine
from groundtruth import load_by_page

from surya.detection import DetectionPredictor
from surya.foundation import FoundationPredictor
from surya.recognition import RecognitionPredictor
from surya.common.surya.schema import TaskNames

t = time.time()
fp = FoundationPredictor(); det = DetectionPredictor(); rec = RecognitionPredictor(fp)
print("surya load %.1fs" % (time.time() - t), flush=True)

eng = ProcessingEngine()
gt = load_by_page()
cache = json.load(open(os.path.join(os.path.dirname(__file__), "ocr_cache_150.json")))

def anchors_y(page):
    rows = eng._align_coordinates(copy.deepcopy(page["cells"]), page["width"], page["height"])["tables"][0]["rows"]
    return [ (r["cells"][1]["bbox"]["y"] + r["cells"][1]["bbox"]["height"]/2.0) for r in rows ]

for pno in [1, 2, 5]:
    page = cache["pages"][str(pno)]
    W, H = page["width"], page["height"]
    img = Image.open(os.path.join(os.path.dirname(__file__), f"pages_150/page_{pno}.png"))
    ys = anchors_y(page)
    # build per-row strips of the handwritten area (x: 0.42W..0.99W)
    spacing = 90
    if len(ys) > 1:
        diffs = sorted(b-a for a, b in zip(ys, ys[1:]) if b-a > 5)
        if diffs: spacing = diffs[len(diffs)//2]
    half = spacing * 0.5
    crops = []
    for ay in ys:
        box = (int(0.42*W), int(max(0, ay-half)), int(0.99*W), int(min(H, ay+half)))
        crops.append(img.crop(box))
    t = time.time()
    res = rec(crops, task_names=[TaskNames.ocr_with_boxes]*len(crops), det_predictor=det)
    dt = time.time() - t
    print(f"--- page {pno}: {len(crops)} row-strips in {dt:.1f}s ({dt/max(len(crops),1):.1f}s/row) ---", flush=True)
    for i, r in enumerate(res):
        lines = sorted(r.text_lines, key=lambda l: l.bbox[0])
        txt = " | ".join(l.text for l in lines)
        g = gt[pno][i] if i < len(gt[pno]) else {}
        try:
            print(f"   EX {txt!r}", flush=True)
            print(f"   GT name={g.get('name')!r} mob={g.get('mobile')!r}", flush=True)
        except Exception:
            print("   (encode err)", flush=True)
