"""Measure the effect of the SEEDED examiner_registry (EXMNAME) on SCC58 accuracy.

Compares three configurations against the DBF ground truth, all using the REAL
production functions (`build_examiner_directory`, `apply_document_consensus`):

  BASELINE            - raw OCR rows, no consensus at all
  CONSENSUS           - in-document cross-row consensus only (today's behaviour)
  CONSENSUS+REGISTRY  - same, plus the historical EXMNAME prior folded in

The registry directory is built straight from EXMNAME.xlsx using the importer's
own resolve logic (canonical name per mobile, ambiguous mobiles dropped), i.e.
exactly what `load_registry_pairs` would return from the seeded DB.

Also prints the mobile overlap between the registry and SCC58 so the gain can be
read honestly (the prior can only help rows whose mobile it already knows).
"""
import os, sys, json, re, copy
from collections import Counter

HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
from app.pipeline.consensus import build_examiner_directory, apply_document_consensus
from app.import_examiner_registry import read_spreadsheet, resolve_mobile, norm_mobile
from groundtruth import load_by_page
import harness_hybrid as H
import harness_consensus as HC

EXMNAME = os.environ.get("EXMNAME", r"C:\Users\badal.dave\Downloads\EXMNAME.xlsx")


def build_registry_pairs(path):
    """Mirror load_registry_pairs: one {mobile, name, votes} per non-ambiguous mobile."""
    per_mobile, _ = read_spreadsheet(path)
    pairs = []
    for mob, counts in per_mobile.items():
        r = resolve_mobile(counts)
        if r["is_ambiguous"] or not r["canonical_name"]:
            continue
        pairs.append({"mobile": mob, "name": r["canonical_name"], "votes": r["times_seen"]})
    return pairs


def rows_to_tables(ex_rows):
    """Wrap flat ex_rows as the all_tables structure apply_document_consensus mutates.
    Keeps a back-reference so we can read voted values back into ex_rows for scoring."""
    cells_for = []
    table_rows = []
    for r in ex_rows:
        name_cell = {"column_index": HC.__dict__.get("NAME_COL", 2), "value": r["name"],
                     "confidence": 0.5, "is_inferred": False}
        # column indices must match consensus.NAME_COL (2) / MOBILE_COL (3)
        name_cell["column_index"] = 2
        mob_cell = {"column_index": 3, "value": r["mobile"], "confidence": 0.5, "is_inferred": False}
        table_rows.append({"cells": [
            {"column_index": 0, "value": r["subcode"], "confidence": 0.5, "is_inferred": False},
            {"column_index": 1, "value": r["batch"], "confidence": 0.5, "is_inferred": False},
            name_cell, mob_cell,
        ]})
        cells_for.append((r, name_cell, mob_cell))
    return [{"rows": table_rows}], cells_for


def tag_singletons(rows_by_page, pnos):
    """Mark rows whose mobile forms a size-1 cluster (no in-document sibling to vote
    with) — the ONLY rows the registry prior can act on. Tags survive deepcopy.
    Returns the singleton count."""
    flat = [r for pno in pnos for r in rows_by_page[pno]]
    for r in flat:
        r["_singleton"] = False
    n = 0
    for cl in HC.union_find_clusters(flat):
        if len(cl) == 1:
            cl[0]["_singleton"] = True
            n += 1
    return n


def score_singletons(rows_by_page, metas, pnos, gt):
    """Like HC.score but tallies ONLY singleton-tagged ex_rows (no page-level ccode)."""
    from collections import defaultdict
    from difflib import SequenceMatcher
    ftot = defaultdict(int); fok = defaultdict(int)
    exact_rows = total = 0; name_sim_sum = 0.0
    for pno in pnos:
        ex_rows = rows_by_page[pno]; gt_rows = gt[pno]
        left = list(range(len(ex_rows))); pairs = []
        for gi, g in enumerate(gt_rows):
            f = next((j for j in left if H.norm_code(ex_rows[j]["batch"]) == H.norm_code(g["batch"]) and H.norm_code(g["batch"])), None)
            if f is not None: left.remove(f); pairs.append((gi, f))
            else: pairs.append((gi, None))
        for k, (gi, ej) in enumerate(pairs):
            if ej is None and left: pairs[k] = (gi, left.pop(0))
        for gi, ej in pairs:
            if ej is None or not ex_rows[ej].get("_singleton"):
                continue
            g = gt_rows[gi]; e = ex_rows[ej]
            total += 1; allok = True
            for f in ("subcode", "batch", "mobile"):
                ftot[f] += 1
                if f == "mobile":
                    gm, em = H.norm_mobile(g[f]), H.norm_mobile(e[f])
                    ok = bool(gm) and len(em) == len(gm) and H._lev(gm, em) <= HC.MOBILE_TOL
                else:
                    ok = H.norm_code(g[f]) == H.norm_code(e[f])
                if ok: fok[f] += 1
                else: allok = False
            s = SequenceMatcher(None, H.norm_text(g["name"]), H.norm_text(e["name"])).ratio()
            name_sim_sum += s; ftot["name"] += 1
            if s >= HC.NAME_SIM_OK: fok["name"] += 1
            else: allok = False
            if allok: exact_rows += 1
    return ftot, fok, total, name_sim_sum, exact_rows


def report_singletons(tag, st):
    ftot, fok, total, name_sim_sum, exact_rows = st
    tot_cells = sum(ftot.values()); ok_cells = sum(fok.values())
    print(f"=== {tag} ({total} singleton rows) ===")
    for f in ("subcode", "batch", "name", "mobile"):
        print(f"  {f:8s}: {fok[f]}/{ftot[f]}  {100*fok[f]/max(ftot[f],1):5.1f}%")
    print(f"  name avg char-sim: {100*name_sim_sum/max(ftot['name'],1):5.1f}%")
    print(f"  CELL ACCURACY: {ok_cells}/{tot_cells}  {100*ok_cells/max(tot_cells,1):5.2f}%")
    print(f"  full-correct rows: {exact_rows}/{total}  {100*exact_rows/max(total,1):5.1f}%\n")


def run_config(rows_by_page, pnos, directory):
    """Deep-copy rows, run real consensus with the given directory (or None), return new rows_by_page."""
    rbp = copy.deepcopy(rows_by_page)
    flat = [r for pno in pnos for r in rbp[pno]]
    tables, cells_for = rows_to_tables(flat)
    stats = apply_document_consensus(tables, examiner_directory=directory)
    for r, name_cell, mob_cell in cells_for:
        r["name"] = name_cell["value"]
        r["mobile"] = mob_cell["value"]
    return rbp, stats


def main():
    gt = load_by_page()
    paddle = json.load(open(H.PADDLE))["pages"]
    surya = json.load(open(H.SURYA))["pages"]
    eng = HC.ProcessingEngine()
    rows_by_page, metas, pnos = HC.build_all_rows(gt, paddle, surya, eng)

    # --- registry directory + overlap report ---
    reg_pairs = build_registry_pairs(EXMNAME)
    reg_mobiles = {p["mobile"] for p in reg_pairs}
    gt_mobiles = {norm_mobile(g["mobile"]) for rows in gt.values() for g in rows}
    gt_mobiles.discard("")
    overlap = reg_mobiles & gt_mobiles
    print(f"Registry (EXMNAME): {len(reg_pairs)} non-ambiguous mobiles")
    print(f"SCC58 ground-truth distinct mobiles: {len(gt_mobiles)}")
    print(f"  of which present in registry      : {len(overlap)}  "
          f"({100*len(overlap)//max(len(gt_mobiles),1)}%)")
    print()

    directory = build_examiner_directory(reg_pairs)

    HC.report("BASELINE (raw OCR)", HC.score(copy.deepcopy(rows_by_page), metas, pnos, gt))

    rbp_noreg, _ = run_config(rows_by_page, pnos, None)
    HC.report("CONSENSUS (in-document only)", HC.score(rbp_noreg, metas, pnos, gt))

    rbp_reg, stats = run_config(rows_by_page, pnos, directory)
    print(f">>> registry backfills: {stats['db_name_backfills']} names, "
          f"{stats['db_mobile_backfills']} mobiles (from {stats['clusters']} clusters)")
    HC.report("CONSENSUS + REGISTRY (seeded)", HC.score(rbp_reg, metas, pnos, gt))

    # --- singleton-only subset: the rows the registry is the ONLY lever for ---
    n_single = tag_singletons(rows_by_page, pnos)
    n_single_known = sum(
        1 for pno in pnos for r in rows_by_page[pno]
        if r.get("_singleton") and norm_mobile(r["mobile"]) in reg_mobiles)
    flat_total = sum(len(rows_by_page[pno]) for pno in pnos)
    print("=" * 60)
    print(f"SINGLETON SUBSET: {n_single}/{flat_total} rows have no in-document "
          f"mobile sibling")
    print(f"  of which the registry knows the mobile: {n_single_known}\n")

    rbp_s_noreg, _ = run_config(rows_by_page, pnos, None)
    report_singletons("SINGLETONS — consensus only (no registry)",
                      score_singletons(rbp_s_noreg, metas, pnos, gt))
    rbp_s_reg, _ = run_config(rows_by_page, pnos, directory)
    report_singletons("SINGLETONS — consensus + REGISTRY",
                      score_singletons(rbp_s_reg, metas, pnos, gt))


if __name__ == "__main__":
    main()
