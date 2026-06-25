"""Read the extracted cells back from the DB and score against the DBF ground truth."""
import os, sys, re
from collections import defaultdict
from difflib import SequenceMatcher
HERE = os.path.dirname(__file__)
sys.path.insert(0, os.path.join(HERE, "..", "python-worker"))
import psycopg
from groundtruth import load_by_page

DSN = "postgresql://postgres:postgres_secure_db_pass_2026@localhost:5439/university_ocr"
DOC = sys.argv[1]
NAME_SIM_OK = float(os.environ.get("NAME_SIM_OK", "0.80"))
MOBILE_TOL = int(os.environ.get("MOBILE_TOL", "1"))   # allow up to N wrong digits

def nt(s): return re.sub(r"[^A-Z0-9 ]", "", re.sub(r"\s+", " ", (s or "").upper())).strip()
def nm(s):
    d = re.sub(r"\D", "", s or ""); return d[-10:] if len(d) >= 10 else d
def ncode(s): return re.sub(r"[^A-Z0-9]", "", (s or "").upper())
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

gt = load_by_page()
rows_by_page = defaultdict(lambda: defaultdict(dict))
ccode_by_page = {}
with psycopg.connect(DSN) as conn, conn.cursor() as cur:
    cur.execute("SELECT page_number, row_index, column_index, current_value FROM extracted_cells WHERE document_id=%s", (DOC,))
    for pno, ri, ci, val in cur.fetchall():
        rows_by_page[pno][ri][ci] = val
    cur.execute("SELECT page_number, college_code FROM document_pages WHERE document_id=%s", (DOC,))
    for pno, cc in cur.fetchall():
        ccode_by_page[pno] = cc

ftot = defaultdict(int); fok = defaultdict(int); ccode_o = ccode_t = 0; sim_sum = 0.0
for pno in sorted(gt):
    ex = []
    for ri in sorted(rows_by_page.get(pno, {})):
        c = rows_by_page[pno][ri]
        ex.append({"subcode": c.get(0, ""), "batch": c.get(1, ""), "name": c.get(2, ""), "mobile": c.get(3, "")})
    ccode_t += 1
    if ncode(str(ccode_by_page.get(pno))) == ncode(gt[pno][0]["ccode"]): ccode_o += 1
    # batch-keyed match
    left = list(range(len(ex))); pairs = []
    for g in gt[pno]:
        f = next((j for j in left if ncode(ex[j]["batch"]) == ncode(g["batch"]) and ncode(g["batch"])), None)
        if f is not None: left.remove(f); pairs.append((g, ex[f]))
        else: pairs.append((g, None))
    for k,(g,e) in enumerate(pairs):
        if e is None and left: pairs[k] = (g, ex[left.pop(0)])
    for g, e in pairs:
        e = e or {"subcode":"","batch":"","name":"","mobile":""}
        for f in ("subcode","batch","mobile"):
            ftot[f]+=1
            if f == "mobile":
                gm, em = nm(g[f]), nm(e[f])
                ok = bool(gm) and len(em) == len(gm) and lev(gm, em) <= MOBILE_TOL
            else:
                ok = ncode(g[f]) == ncode(e[f])
            if ok: fok[f]+=1
        s = SequenceMatcher(None, nt(g["name"]), nt(e["name"])).ratio(); sim_sum += s; ftot["name"]+=1
        if s>=NAME_SIM_OK: fok["name"]+=1

tot = sum(ftot.values())+ccode_t; ok = sum(fok.values())+ccode_o
print(f"=== DB VERIFICATION (doc {DOC[:8]}, {ftot['subcode']} rows) ===")
for f in ("subcode","batch","name","mobile"):
    extra = f"   (sim>={NAME_SIM_OK})" if f == "name" else (f"   (+-{MOBILE_TOL} digit)" if f == "mobile" else "")
    print(f"  {f:8s}: {fok[f]}/{ftot[f]}  {100*fok[f]/max(ftot[f],1):5.1f}%{extra}")
print(f"  ccode   : {ccode_o}/{ccode_t}  {100*ccode_o/max(ccode_t,1):5.1f}%")
print(f"  name avg char-sim: {100*sim_sum/max(ftot['name'],1):5.1f}%")
print(f"  CELL ACCURACY (name>={NAME_SIM_OK}, mobile +-{MOBILE_TOL}): {ok}/{tot}  {100*ok/max(tot,1):5.2f}%")
