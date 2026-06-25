"""Evaluate the engine's post-processing against DBF ground truth using cached raw OCR.
Fast loop: edit engine.py, re-run this, read accuracy. No OCR, no DB."""
import os, sys, json, re, copy, argparse
from collections import defaultdict
from difflib import SequenceMatcher

HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
from app.pipeline.engine import ProcessingEngine
from groundtruth import load_by_page

DPI = int(os.environ.get("CACHE_DPI", "150"))
CACHE = os.path.join(HERE, f"ocr_cache_{DPI}.json")

# ---- normalization ----
def norm_text(s):
    s = (s or "").upper().strip()
    s = re.sub(r"\s+", " ", s)
    s = re.sub(r"[^A-Z0-9 ]", "", s)
    return s.strip()

def norm_mobile(s):
    d = re.sub(r"\D", "", s or "")
    return d[-10:] if len(d) >= 10 else d

def norm_code(s):
    return re.sub(r"[^A-Z0-9]", "", (s or "").upper())

COLS = {0: "subcode", 1: "batch", 2: "name", 3: "mobile"}

def extracted_rows(engine, page):
    cells = copy.deepcopy(page["cells"])
    res = engine._align_coordinates(cells, page["width"], page["height"])
    out = []
    for tbl in res.get("tables", []):
        for row in tbl.get("rows", []):
            cmap = {}
            for c in row["cells"]:
                cmap[c["column_index"]] = c["value"]
            out.append({
                "subcode": cmap.get(0, ""), "batch": cmap.get(1, ""),
                "name": cmap.get(2, ""), "mobile": cmap.get(3, ""),
            })
    meta = engine._extract_page_metadata(copy.deepcopy(page["cells"]), page["width"], page["height"])
    return out, meta

def cmp_field(field, gt, ex):
    if field == "mobile":
        return norm_mobile(gt) == norm_mobile(ex) and norm_mobile(gt) != ""
    if field in ("subcode", "batch"):
        return norm_code(gt) == norm_code(ex)
    return norm_text(gt) == norm_text(ex)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dump", default=os.path.join(HERE, "mismatches.txt"))
    ap.add_argument("--max-pages", type=int, default=0)
    args = ap.parse_args()

    gt = load_by_page()
    cache = json.load(open(CACHE))
    pages = cache["pages"]
    engine = ProcessingEngine()

    field_tot = defaultdict(int); field_ok = defaultdict(int)
    ccode_tot = ccode_ok = 0
    rowcount_ok = rowcount_tot = 0
    exact_rows = total_rows = 0
    name_sim_sum = 0.0; name_sim_n = 0; name_close = 0
    dump = []

    page_nums = sorted(int(p) for p in pages if int(p) in gt)
    if args.max_pages:
        page_nums = page_nums[:args.max_pages]

    def match_rows(gt_rows, ex_rows):
        """Pair each GT row with an EX row by batch label, falling back to order
        for anything left unmatched (so one dropped row doesn't cascade)."""
        ex_left = list(range(len(ex_rows)))
        pairs = []
        for gi, g in enumerate(gt_rows):
            found = None
            for j in ex_left:
                if norm_code(ex_rows[j]["batch"]) == norm_code(g["batch"]) and norm_code(g["batch"]):
                    found = j; break
            if found is not None:
                ex_left.remove(found); pairs.append((gi, found))
            else:
                pairs.append((gi, None))
        # positional fill for unmatched GT rows
        for k, (gi, ej) in enumerate(pairs):
            if ej is None and ex_left:
                pairs[k] = (gi, ex_left.pop(0))
        return pairs

    EMPTY = {"subcode":"","batch":"","name":"","mobile":""}
    for pno in page_nums:
        gt_rows = gt[pno]
        ex_rows, meta = extracted_rows(engine, pages[str(pno)])

        gt_cc = gt_rows[0]["ccode"]
        ccode_tot += 1
        if norm_code(meta.get("college_code") or "") == norm_code(gt_cc):
            ccode_ok += 1

        rowcount_tot += 1
        if len(ex_rows) == len(gt_rows):
            rowcount_ok += 1

        pairs = match_rows(gt_rows, ex_rows)
        page_bad = []
        for gi, ej in pairs:
            g = gt_rows[gi]
            e = ex_rows[ej] if ej is not None else EMPTY
            total_rows += 1
            allok = True
            for f in ("subcode", "batch", "name", "mobile"):
                field_tot[f] += 1
                if cmp_field(f, g[f], e[f]):
                    field_ok[f] += 1
                else:
                    allok = False
                    page_bad.append(f"      {f}: GT={g[f]!r}  EX={e[f]!r}")
            sim = SequenceMatcher(None, norm_text(g["name"]), norm_text(e["name"])).ratio()
            name_sim_sum += sim; name_sim_n += 1
            if sim >= 0.8:
                name_close += 1
            if allok:
                exact_rows += 1
        if page_bad or len(ex_rows) != len(gt_rows):
            dump.append(f"PAGE {pno} (gt {len(gt_rows)} rows, ex {len(ex_rows)} rows, cc GT={gt_cc} EX={meta.get('college_code')})")
            dump.extend(page_bad)

    # ---- report ----
    tot_cells = sum(field_tot.values()) + ccode_tot
    ok_cells = sum(field_ok.values()) + ccode_ok
    print(f"=== EVAL ({len(page_nums)} pages, {total_rows} rows) DPI={DPI} ===")
    for f in ("subcode", "batch", "name", "mobile"):
        t = field_tot[f]; k = field_ok[f]
        print(f"  {f:8s}: {k}/{t}  {100*k/max(t,1):5.1f}%")
    print(f"  ccode   : {ccode_ok}/{ccode_tot}  {100*ccode_ok/max(ccode_tot,1):5.1f}%")
    print(f"  -------------------------------------")
    print(f"  CELL ACCURACY: {ok_cells}/{tot_cells}  {100*ok_cells/max(tot_cells,1):5.2f}%")
    print(f"  exact rows   : {exact_rows}/{total_rows}  {100*exact_rows/max(total_rows,1):5.1f}%")
    print(f"  rowcount ok  : {rowcount_ok}/{rowcount_tot} pages  {100*rowcount_ok/max(rowcount_tot,1):5.1f}%")
    print(f"  -- name (char-level) --")
    print(f"  name avg char-sim : {100*name_sim_sum/max(name_sim_n,1):5.1f}%")
    print(f"  name >=80% sim    : {name_close}/{name_sim_n}  {100*name_close/max(name_sim_n,1):5.1f}%")
    # overall if names scored by char similarity instead of exact match
    fuzzy_ok = (sum(field_ok[f] for f in ('subcode','batch','mobile')) + ccode_ok + name_sim_sum)
    print(f"  CELL ACC (fuzzy name): {100*fuzzy_ok/max(tot_cells,1):5.2f}%")

    open(args.dump, "w", encoding="utf-8").write("\n".join(dump))
    print(f"  mismatches -> {args.dump} ({len(dump)} lines)")

if __name__ == "__main__":
    main()
