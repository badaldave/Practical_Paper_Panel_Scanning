"""Hybrid evaluation: Paddle for structure (subcode/batch/college + row anchors),
Surya for handwritten name/mobile. Compares against DBF ground truth.
Name scored by character similarity (>=0.85 counts as correct)."""
import os, sys, json, re, copy, argparse
from collections import defaultdict
from difflib import SequenceMatcher

HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
from app.pipeline.engine import ProcessingEngine
from groundtruth import load_by_page

PADDLE = os.path.join(HERE, "ocr_cache_150.json")
SURYA = os.path.join(HERE, "ocr_cache_surya.json")
NAME_SIM_OK = float(os.environ.get("NAME_SIM_OK", "0.85"))
MOBILE_TOL = int(os.environ.get("MOBILE_TOL", "0"))   # allow up to N wrong digits

def _lev(a, b):
    if a == b: return 0
    if not a or not b: return max(len(a), len(b))
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[-1] + 1, prev[j-1] + (ca != cb)))
        prev = cur
    return prev[-1]

def norm_text(s):
    s = (s or "").upper().strip()
    return re.sub(r"[^A-Z0-9 ]", "", re.sub(r"\s+", " ", s)).strip()
def norm_mobile(s):
    d = re.sub(r"\D", "", s or ""); return d[-10:] if len(d) >= 10 else d
def norm_code(s):
    return re.sub(r"[^A-Z0-9]", "", (s or "").upper())

def parse_surya_row(eng, lines, ay, tol):
    """Return (name, mobile) from a row strip's Surya text lines, keeping only
    lines whose y is near this row's anchor (avoids neighbour-row contamination)."""
    lines = [l for l in lines if abs(l["y"] - ay) <= tol]
    name_parts, mobile = [], ""
    for l in sorted(lines, key=lambda l: (round(l["y"] / 15), l["x"])):
        t = re.sub(r"[|]+", " ", l["t"])
        t = re.sub(r"[_]{2,}", " ", t)
        t = re.sub(r"[-]{3,}", " ", t).strip()
        if not t:
            continue
        digits = re.sub(r"\D", "", t)
        letters = re.sub(r"[^A-Za-z]", "", t)
        if len(digits) >= 7 and len(digits) >= len(letters):
            nm, mob = eng._split_trailing_mobile(t)
            if not mobile:
                mobile = re.sub(r"\D", "", mob or t)
            if nm and len(re.sub(r"[^A-Za-z]", "", nm)) >= 2:
                name_parts.append(nm)
        elif len(letters) >= 2:
            nm, mob = eng._split_trailing_mobile(t)
            name_parts.append(nm or t)
            if mob and not mobile:
                mobile = mob
    return eng._clean_name(" ".join(name_parts)), eng._clean_mobile(mobile)

def match_surya(surya_rows, ay, tol=40):
    best, bd = None, tol
    for sr in surya_rows:
        d = abs(sr["y"] - ay)
        if d <= bd:
            bd = d; best = sr
    return best

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dump", default=os.path.join(HERE, "mismatches_hybrid.txt"))
    args = ap.parse_args()

    gt = load_by_page()
    paddle = json.load(open(PADDLE))["pages"]
    surya = json.load(open(SURYA))["pages"] if os.path.exists(SURYA) else {}
    eng = ProcessingEngine()

    ftot = defaultdict(int); fok = defaultdict(int)
    ccode_t = ccode_o = 0; exact_rows = total = 0
    name_sim_sum = 0.0
    dump = []

    pnos = sorted((int(p) for p in paddle if p in surya and int(p) in gt))
    for pno in pnos:
        page = paddle[str(pno)]
        aligned = eng._align_coordinates(copy.deepcopy(page["cells"]), page["width"], page["height"], drop_empty=False)["tables"][0]["rows"]
        meta = eng._extract_page_metadata(copy.deepcopy(page["cells"]), page["width"], page["height"])
        srows = surya[str(pno)]
        sys_ys = sorted(sr["y"] for sr in srows)
        sdiffs = sorted(b - a for a, b in zip(sys_ys, sys_ys[1:]) if b - a > 5)
        sp = sdiffs[len(sdiffs)//2] if sdiffs else 90.0
        tol = sp * 0.42

        ex_rows = []
        for r in aligned:
            cm = {c["column_index"]: c for c in r["cells"]}
            ay = cm[1]["bbox"]["y"] + cm[1]["bbox"]["height"]/2.0
            sr = match_surya(srows, ay)
            s_name, s_mob = parse_surya_row(eng, sr["lines"], sr["y"], tol) if sr else ("", "")
            name = s_name or cm[2]["value"]
            mobile = s_mob or cm[3]["value"]
            # Drop the slot only if neither engine found an examiner.
            if len(re.sub(r"[^A-Za-z]", "", name)) < 2 and len(re.sub(r"\D", "", mobile)) < 6:
                continue
            ex_rows.append({
                "subcode": cm[0]["value"], "batch": cm[1]["value"],
                "name": name, "mobile": mobile,
            })

        gt_rows = gt[pno]
        ccode_t += 1
        if norm_code(meta.get("college_code") or "") == norm_code(gt_rows[0]["ccode"]):
            ccode_o += 1

        # match GT<->EX by batch, positional fallback
        left = list(range(len(ex_rows))); pairs = []
        for gi, g in enumerate(gt_rows):
            f = next((j for j in left if norm_code(ex_rows[j]["batch"]) == norm_code(g["batch"]) and norm_code(g["batch"])), None)
            if f is not None: left.remove(f); pairs.append((gi, f))
            else: pairs.append((gi, None))
        for k, (gi, ej) in enumerate(pairs):
            if ej is None and left: pairs[k] = (gi, left.pop(0))

        page_bad = []
        for gi, ej in pairs:
            g = gt_rows[gi]; e = ex_rows[ej] if ej is not None else {"subcode":"","batch":"","name":"","mobile":""}
            total += 1; allok = True
            for f in ("subcode", "batch", "mobile"):
                ftot[f] += 1
                if f == "mobile":
                    gm, em = norm_mobile(g[f]), norm_mobile(e[f])
                    ok = bool(gm) and len(em) == len(gm) and _lev(gm, em) <= MOBILE_TOL
                else:
                    ok = norm_code(g[f]) == norm_code(e[f])
                if ok: fok[f] += 1
                else: allok = False; page_bad.append(f"      {f}: GT={g[f]!r} EX={e[f]!r}")
            sim = SequenceMatcher(None, norm_text(g["name"]), norm_text(e["name"])).ratio()
            name_sim_sum += sim; ftot["name"] += 1
            if sim >= NAME_SIM_OK: fok["name"] += 1
            else: allok = False; page_bad.append(f"      name(sim={sim:.2f}): GT={g['name']!r} EX={e['name']!r}")
            if allok: exact_rows += 1
        if page_bad:
            dump.append(f"PAGE {pno} (gt {len(gt_rows)} ex {len(ex_rows)} cc GT={gt_rows[0]['ccode']} EX={meta.get('college_code')})")
            dump.extend(page_bad)

    tot_cells = sum(ftot.values()) + ccode_t
    ok_cells = sum(fok.values()) + ccode_o
    print(f"=== HYBRID EVAL ({len(pnos)} pages, {total} rows) ===")
    for f in ("subcode", "batch", "name", "mobile"):
        print(f"  {f:8s}: {fok[f]}/{ftot[f]}  {100*fok[f]/max(ftot[f],1):5.1f}%" + ("   (name: sim>=%.2f)" % NAME_SIM_OK if f == "name" else ""))
    print(f"  ccode   : {ccode_o}/{ccode_t}  {100*ccode_o/max(ccode_t,1):5.1f}%")
    print(f"  name avg char-sim: {100*name_sim_sum/max(ftot['name'],1):5.1f}%")
    print(f"  -------------------------------------")
    print(f"  CELL ACCURACY: {ok_cells}/{tot_cells}  {100*ok_cells/max(tot_cells,1):5.2f}%")
    print(f"  full-correct rows: {exact_rows}/{total}  {100*exact_rows/max(total,1):5.1f}%")
    open(args.dump, "w", encoding="utf-8").write("\n".join(dump))
    print(f"  mismatches -> {args.dump} ({len(dump)} lines)")

if __name__ == "__main__":
    main()
