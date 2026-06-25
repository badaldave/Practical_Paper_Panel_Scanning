"""Hybrid eval + cross-row CONSENSUS pass (the 'same mobile => same examiner' idea).

Builds the same ex_rows as harness_hybrid, then runs a global consensus step that
clusters rows by examiner and votes a single name/mobile per cluster. Inferred
changes are flagged (not silently trusted). Re-scores against the DBF.

Modes (env CONSENSUS=off|name|both):
  off  - baseline (no consensus)
  name - cluster by exact 10-digit mobile, vote the NAME  (the user's literal idea)
  both - entity-resolution: bridge mobile-misreads via name, vote NAME and MOBILE
"""
import os, sys, json, re, copy
from collections import defaultdict, Counter
from difflib import SequenceMatcher

HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
from app.pipeline.engine import ProcessingEngine
from groundtruth import load_by_page
import harness_hybrid as H

MODE = os.environ.get("CONSENSUS", "both")
NAME_SIM_OK = float(os.environ.get("NAME_SIM_OK", "0.85"))
MOBILE_TOL = int(os.environ.get("MOBILE_TOL", "0"))


def nname(s):
    return re.sub(r"[^A-Z ]", "", re.sub(r"\s+", " ", (s or "").upper())).strip()
def nmob(s):
    d = re.sub(r"\D", "", s or ""); return d[-10:] if len(d) >= 10 else d
def name_letters(s):
    return len(re.sub(r"[^A-Za-z]", "", s or ""))
def sim(a, b):
    return SequenceMatcher(None, a, b).ratio()
def lev(a, b):
    if a == b: return 0
    if not a or not b: return max(len(a), len(b))
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[-1] + 1, prev[j-1] + (ca != cb)))
        prev = cur
    return prev[-1]


def medoid(values, weights=None):
    """Return the value with max total weighted similarity to all others (robust majority)."""
    uniq = list(dict.fromkeys(values))
    if len(uniq) == 1:
        return uniq[0]
    cnt = Counter(values)
    best, bestscore = uniq[0], -1.0
    for cand in uniq:
        score = sum(cnt[o] * sim(cand, o) for o in uniq)
        if score > bestscore:
            best, bestscore = cand, score
    return best


def cluster_by_mobile(rows):
    """Exact 10-digit mobile clusters."""
    groups = defaultdict(list)
    singles = []
    for r in rows:
        m = nmob(r["mobile"])
        if len(m) == 10:
            groups[m].append(r)
        else:
            singles.append(r)
    return groups, singles


def union_find_clusters(rows):
    """Entity resolution: connect rows whose mobiles are near-equal (lev<=1, both 10-digit)
    OR whose mobiles are close (lev<=3) AND names are similar (>=0.7). Returns list of clusters."""
    n = len(rows)
    parent = list(range(n))
    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]; x = parent[x]
        return x
    def union(a, b):
        ra, rb = find(a), find(b)
        if ra != rb: parent[ra] = rb
    mobs = [nmob(r["mobile"]) for r in rows]
    nms = [nname(r["name"]) for r in rows]
    # bucket by mobile prefix to avoid O(n^2) blowup; n is small (~520) so full is fine
    for i in range(n):
        if len(mobs[i]) != 10: continue
        for j in range(i + 1, n):
            if len(mobs[j]) != 10: continue
            d = lev(mobs[i], mobs[j])
            if d == 0:
                union(i, j)
            elif d <= 1 and nms[i] and nms[j] and sim(nms[i], nms[j]) >= 0.55:
                union(i, j)
            elif d <= 3 and nms[i] and nms[j] and sim(nms[i], nms[j]) >= 0.80:
                union(i, j)
    comp = defaultdict(list)
    for i in range(n):
        comp[find(i)].append(rows[i])
    return list(comp.values())


def apply_consensus(rows, mode):
    """Mutate rows in place. Returns (name_changes, mobile_changes)."""
    nch = mch = 0
    if mode == "off":
        return 0, 0
    if mode == "name":
        groups, _ = cluster_by_mobile(rows)
        clusters = [g for g in groups.values() if len(g) >= 2]
    else:  # both
        clusters = [c for c in union_find_clusters(rows) if len(c) >= 2]

    for cl in clusters:
        # --- NAME consensus: medoid over valid name reads, weighted by occurrence ---
        cand_names = [r["name"] for r in cl if name_letters(r["name"]) >= 2]
        if cand_names:
            cons = medoid(cand_names)
            for r in cl:
                if nname(r["name"]) != nname(cons):
                    r["name"] = cons; r["inferred_name"] = True; nch += 1
        # --- MOBILE consensus (mode=both only): vote the 10-digit mobile ---
        if mode == "both":
            cand_mobs = [nmob(r["mobile"]) for r in cl if len(nmob(r["mobile"])) == 10]
            if cand_mobs:
                consm = Counter(cand_mobs).most_common(1)[0][0]
                for r in cl:
                    if nmob(r["mobile"]) != consm:
                        r["mobile"] = consm; r["inferred_mobile"] = True; mch += 1
    return nch, mch


def build_all_rows(gt, paddle, surya, eng):
    """Returns {pno: [ex_row,...]} using the same logic as harness_hybrid."""
    out = {}
    pnos = sorted((int(p) for p in paddle if p in surya and int(p) in gt))
    metas = {}
    for pno in pnos:
        page = paddle[str(pno)]
        aligned = eng._align_coordinates(copy.deepcopy(page["cells"]), page["width"], page["height"], drop_empty=False)["tables"][0]["rows"]
        metas[pno] = eng._extract_page_metadata(copy.deepcopy(page["cells"]), page["width"], page["height"])
        srows = surya[str(pno)]
        sys_ys = sorted(sr["y"] for sr in srows)
        sdiffs = sorted(b - a for a, b in zip(sys_ys, sys_ys[1:]) if b - a > 5)
        sp = sdiffs[len(sdiffs)//2] if sdiffs else 90.0
        tol = sp * 0.42
        ex_rows = []
        for r in aligned:
            cm = {c["column_index"]: c for c in r["cells"]}
            ay = cm[1]["bbox"]["y"] + cm[1]["bbox"]["height"]/2.0
            sr = H.match_surya(srows, ay)
            s_name, s_mob = H.parse_surya_row(eng, sr["lines"], sr["y"], tol) if sr else ("", "")
            name = s_name or cm[2]["value"]
            mobile = s_mob or cm[3]["value"]
            if name_letters(name) < 2 and len(re.sub(r"\D", "", mobile)) < 6:
                continue
            ex_rows.append({"subcode": cm[0]["value"], "batch": cm[1]["value"],
                            "name": name, "mobile": mobile, "pno": pno})
        out[pno] = ex_rows
    return out, metas, pnos


def score(rows_by_page, metas, pnos, gt):
    ftot = defaultdict(int); fok = defaultdict(int)
    ccode_t = ccode_o = 0; exact_rows = total = 0; name_sim_sum = 0.0
    for pno in pnos:
        ex_rows = rows_by_page[pno]; gt_rows = gt[pno]
        ccode_t += 1
        if H.norm_code(metas[pno].get("college_code") or "") == H.norm_code(gt_rows[0]["ccode"]):
            ccode_o += 1
        left = list(range(len(ex_rows))); pairs = []
        for gi, g in enumerate(gt_rows):
            f = next((j for j in left if H.norm_code(ex_rows[j]["batch"]) == H.norm_code(g["batch"]) and H.norm_code(g["batch"])), None)
            if f is not None: left.remove(f); pairs.append((gi, f))
            else: pairs.append((gi, None))
        for k, (gi, ej) in enumerate(pairs):
            if ej is None and left: pairs[k] = (gi, left.pop(0))
        for gi, ej in pairs:
            g = gt_rows[gi]; e = ex_rows[ej] if ej is not None else {"subcode":"","batch":"","name":"","mobile":""}
            total += 1; allok = True
            for f in ("subcode", "batch", "mobile"):
                ftot[f] += 1
                if f == "mobile":
                    gm, em = H.norm_mobile(g[f]), H.norm_mobile(e[f])
                    ok = bool(gm) and len(em) == len(gm) and H._lev(gm, em) <= MOBILE_TOL
                else:
                    ok = H.norm_code(g[f]) == H.norm_code(e[f])
                if ok: fok[f] += 1
                else: allok = False
            s = SequenceMatcher(None, H.norm_text(g["name"]), H.norm_text(e["name"])).ratio()
            name_sim_sum += s; ftot["name"] += 1
            if s >= NAME_SIM_OK: fok["name"] += 1
            else: allok = False
            if allok: exact_rows += 1
    tot_cells = sum(ftot.values()) + ccode_t
    ok_cells = sum(fok.values()) + ccode_o
    return ftot, fok, ccode_o, ccode_t, ok_cells, tot_cells, exact_rows, total, name_sim_sum


def report(tag, st):
    ftot, fok, ccode_o, ccode_t, ok_cells, tot_cells, exact_rows, total, name_sim_sum = st
    print(f"=== {tag} ({total} rows) ===")
    for f in ("subcode", "batch", "name", "mobile"):
        print(f"  {f:8s}: {fok[f]}/{ftot[f]}  {100*fok[f]/max(ftot[f],1):5.1f}%")
    print(f"  ccode   : {ccode_o}/{ccode_t}  {100*ccode_o/max(ccode_t,1):5.1f}%")
    print(f"  name avg char-sim: {100*name_sim_sum/max(ftot['name'],1):5.1f}%")
    print(f"  CELL ACCURACY: {ok_cells}/{tot_cells}  {100*ok_cells/max(tot_cells,1):5.2f}%")
    print(f"  full-correct rows: {exact_rows}/{total}  {100*exact_rows/max(total,1):5.1f}%")
    print()


def main():
    gt = load_by_page()
    paddle = json.load(open(H.PADDLE))["pages"]
    surya = json.load(open(H.SURYA))["pages"]
    eng = ProcessingEngine()
    rows_by_page, metas, pnos = build_all_rows(gt, paddle, surya, eng)

    report("BASELINE", score(copy.deepcopy(rows_by_page), metas, pnos, gt))

    for mode in (["name", "both"] if MODE == "all" else [MODE]):
        rbp = copy.deepcopy(rows_by_page)
        flat = [r for pno in pnos for r in rbp[pno]]
        nch, mch = apply_consensus(flat, mode)
        print(f">>> consensus mode={mode}: {nch} name backfills, {mch} mobile backfills")
        report(f"CONSENSUS[{mode}]", score(rbp, metas, pnos, gt))


if __name__ == "__main__":
    main()
