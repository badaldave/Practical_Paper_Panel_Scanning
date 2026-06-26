"""Import a historical examiner spreadsheet into `examiner_registry`.

The spreadsheet (e.g. EXMNAME.xlsx) is a two-column MOBILENO / EXMNAME dump of
past years' examiners. We distil it into one row per mobile so the OCR consensus
pass can use it as a PRIOR: when a new sheet's row carries a recognised mobile
but a missing or poorly-read name, the name is filled from history.

Per mobile, the many spellings/partials of ONE person are clustered and collapsed
to a single canonical name (most name-tokens, then longest, then most frequent).
When a mobile carried genuinely DIFFERENT people across years (number reassigned),
that mobile is flagged is_ambiguous and excluded from auto-inference — it falls
back to normal in-document consensus / human review.

Usage (from python-worker/, venv active):
    python -m app.import_examiner_registry <file.xlsx>            # dry-run report
    python -m app.import_examiner_registry <file.xlsx> --commit   # write to DB
    python -m app.import_examiner_registry <file.xlsx> --commit --merge   # accumulate
    python -m app.import_examiner_registry <file.xlsx> --tenant <uuid> --source 2023

By default a commit REPLACES each mobile present in the file (re-running the same
file is idempotent). --merge unions the file with whatever is already stored, so
several years' files can be loaded one at a time and accumulate.
"""
import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from difflib import SequenceMatcher
from uuid import uuid4

import openpyxl

from app.db.connection import DBConnection

DEMO_TENANT = "e93fca1e-1f7c-47bc-87c2-127e7740e53a"


# --- normalisation ---------------------------------------------------------

def norm_mobile(raw) -> str:
    """Digits only; keep the last 10 (drops leading tab / +91 / 0). '' if not 10."""
    d = re.sub(r"\D", "", str(raw or ""))
    d = d[-10:] if len(d) > 10 else d
    return d if len(d) == 10 else ""


def norm_name(raw) -> str:
    return re.sub(r"\s+", " ", str(raw or "").upper()).strip()


# --- entity resolution within one mobile -----------------------------------

def _sim(a: str, b: str) -> float:
    return SequenceMatcher(None, a, b).ratio()


def _related(a: str, b: str) -> bool:
    """Two names plausibly the SAME person: identical, one a token-subset of the
    other (partial name), close spelling (drift), or shared surname + similar
    first token (initials expanded, e.g. 'S K TRIVEDI' ~ 'SHASHIKANT TRIVEDI')."""
    if a == b:
        return True
    ta, tb = set(a.split()), set(b.split())
    if ta and tb and (ta <= tb or tb <= ta):
        return True
    if _sim(a, b) >= 0.78:
        return True
    if a.split()[-1] == b.split()[-1] and _sim(a.split()[0], b.split()[0]) >= 0.6:
        return True
    return False


def _cluster(names) -> list:
    """Greedy single-link clustering with transitive merge. Each cluster is a
    list of names believed to be one person."""
    uniq = list(dict.fromkeys(names))
    clusters = []
    for n in uniq:
        for c in clusters:
            if any(_related(n, x) for x in c):
                c.append(n)
                break
        else:
            clusters.append([n])
    merged = True
    while merged:
        merged = False
        for i in range(len(clusters)):
            for j in range(i + 1, len(clusters)):
                if any(_related(a, b) for a in clusters[i] for b in clusters[j]):
                    clusters[i] += clusters[j]
                    del clusters[j]
                    merged = True
                    break
            if merged:
                break
    return clusters


def _canonical(cluster, counts: Counter) -> str:
    """Most complete spelling: most name-tokens, then longest, then most frequent."""
    return max(cluster, key=lambda n: (len(n.split()), len(n), counts.get(n, 0)))


def resolve_mobile(counts: Counter) -> dict:
    """Collapse one mobile's observed names (name -> occurrence count) into a
    registry record: canonical name, ambiguity flag, variants, vote weight."""
    names = list(counts.keys())
    clusters = _cluster(names)
    ambiguous = len(clusters) > 1
    canonical = None if ambiguous else _canonical(clusters[0], counts)
    return {
        "canonical_name": canonical,
        "is_ambiguous": ambiguous,
        "variants": dict(counts),
        "times_seen": sum(counts.values()),
    }


# --- spreadsheet ------------------------------------------------------------

def read_spreadsheet(path: str):
    """Yield (mobile_counts, skipped) from the xlsx. Auto-detects the mobile and
    name columns from the header row (MOBILENO / EXMNAME), else falls back to the
    first two columns."""
    wb = openpyxl.load_workbook(path, read_only=True, data_only=True)
    ws = wb.worksheets[0]
    rows = ws.iter_rows(values_only=True)
    header = next(rows, None) or ()
    mob_col, name_col = 0, 1
    for i, h in enumerate(header):
        hl = str(h or "").strip().upper()
        if "MOB" in hl:
            mob_col = i
        elif "NAME" in hl or "EXMNAME" in hl:
            name_col = i

    per_mobile = defaultdict(Counter)
    skipped = []
    for raw in rows:
        if raw is None:
            continue
        rawmob = raw[mob_col] if len(raw) > mob_col else None
        rawname = raw[name_col] if len(raw) > name_col else None
        mob = norm_mobile(rawmob)
        name = norm_name(rawname)
        if not mob or len(re.sub(r"[^A-Za-z]", "", name)) < 2:
            skipped.append((rawmob, rawname))
            continue
        per_mobile[mob][name] += 1
    return per_mobile, skipped


# --- existing state (for --merge) ------------------------------------------

def load_existing(tenant_id: str) -> dict:
    rows = DBConnection.execute_query(
        "SELECT mobile, name_variants FROM examiner_registry WHERE tenant_id = %s",
        (tenant_id,), fetch=True) or []
    out = {}
    for r in rows:
        variants = r["name_variants"]
        if isinstance(variants, str):
            variants = json.loads(variants)
        # Tolerate the array form ["A","B"] as well as the object form {"A":2}.
        if isinstance(variants, list):
            out[r["mobile"]] = Counter({norm_name(v): 1 for v in variants})
        elif isinstance(variants, dict):
            out[r["mobile"]] = Counter({norm_name(k): int(v) for k, v in variants.items()})
    return out


# --- main -------------------------------------------------------------------

def main(argv=None):
    ap = argparse.ArgumentParser(description="Seed examiner_registry from a spreadsheet.")
    ap.add_argument("file", help="Path to the .xlsx file (e.g. EXMNAME.xlsx)")
    ap.add_argument("--tenant", default=DEMO_TENANT, help="Tenant UUID (default: demo)")
    ap.add_argument("--source", default=None, help="Source label (default: filename)")
    ap.add_argument("--commit", action="store_true", help="Write to DB (else dry-run)")
    ap.add_argument("--merge", action="store_true",
                    help="Union with existing rows instead of replacing per-mobile")
    args = ap.parse_args(argv)

    source = args.source or args.file.replace("\\", "/").rsplit("/", 1)[-1]
    per_mobile, skipped = read_spreadsheet(args.file)

    existing = load_existing(args.tenant) if args.merge else {}

    records = {}
    for mob, counts in per_mobile.items():
        merged = Counter(counts)
        if args.merge and mob in existing:
            merged += existing[mob]
        records[mob] = resolve_mobile(merged)

    n_total = len(records)
    n_amb = sum(1 for r in records.values() if r["is_ambiguous"])
    n_collapsed = sum(1 for r in records.values()
                      if not r["is_ambiguous"] and len(r["variants"]) > 1)
    n_single = n_total - n_amb - n_collapsed

    print(f"Source file        : {args.file}")
    print(f"Tenant             : {args.tenant}")
    print(f"Mode               : {'MERGE' if args.merge else 'REPLACE'} | "
          f"{'COMMIT' if args.commit else 'DRY-RUN'}")
    print(f"Skipped rows       : {len(skipped)} (bad mobile / empty name)")
    print(f"Mobiles resolved   : {n_total}")
    print(f"  single name       : {n_single}")
    print(f"  collapsed variants: {n_collapsed}  -> auto-infer canonical")
    print(f"  AMBIGUOUS (flagged): {n_amb}  -> excluded from auto-infer")
    inferable = n_single + n_collapsed
    print(f"Usable for inference: {inferable} mobiles "
          f"({inferable * 100 // n_total if n_total else 0}%)")

    print("\nSample collapsed (variants -> canonical):")
    shown = 0
    for mob, r in records.items():
        if not r["is_ambiguous"] and len(r["variants"]) > 1:
            print(f"  {mob}  {list(r['variants'])} -> {r['canonical_name']}")
            shown += 1
            if shown >= 8:
                break
    print("\nSample ambiguous (flagged, not inferred):")
    shown = 0
    for mob, r in records.items():
        if r["is_ambiguous"]:
            print(f"  {mob}  {list(r['variants'])}")
            shown += 1
            if shown >= 8:
                break
    if skipped:
        print(f"\nSample skipped: {skipped[:5]}")

    if not args.commit:
        print("\nDry-run only. Re-run with --commit to write.")
        return 0

    rows = [(
        str(uuid4()), args.tenant, mob, r["canonical_name"],
        json.dumps(r["variants"]), r["is_ambiguous"], r["times_seen"],
        json.dumps([source]),
    ) for mob, r in records.items()]

    upsert = """
        INSERT INTO examiner_registry
            (id, tenant_id, mobile, canonical_name, name_variants,
             is_ambiguous, times_seen, source_files, created_at, updated_at)
        VALUES (%s, %s, %s, %s, %s::jsonb, %s, %s, %s::jsonb, now(), now())
        ON CONFLICT (tenant_id, mobile) DO UPDATE SET
            canonical_name = EXCLUDED.canonical_name,
            name_variants  = EXCLUDED.name_variants,
            is_ambiguous   = EXCLUDED.is_ambiguous,
            times_seen     = EXCLUDED.times_seen,
            source_files   = (
                SELECT to_jsonb(array(
                    SELECT DISTINCT e FROM jsonb_array_elements_text(
                        examiner_registry.source_files || EXCLUDED.source_files) e))
            ),
            updated_at     = now()
    """
    with DBConnection.get_connection() as conn:
        with conn.cursor() as cur:
            cur.executemany(upsert, rows)
        conn.commit()
    print(f"\nCommitted {len(rows)} registry rows for tenant {args.tenant}.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
