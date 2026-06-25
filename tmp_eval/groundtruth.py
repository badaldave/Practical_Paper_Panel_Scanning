"""Parse the SCC58.XLS dBASE/DBF ground-truth file into per-page records."""
import struct
from collections import defaultdict

DBF = r"C:\Users\badal.dave\Downloads\SCC58.XLS"

def load_records(path=DBF):
    d = open(path, "rb").read()
    numrec = struct.unpack("<I", d[4:8])[0]
    hdrlen = struct.unpack("<H", d[8:10])[0]
    reclen = struct.unpack("<H", d[10:12])[0]
    fields = []
    off = 32
    while d[off] != 0x0D:
        name = d[off:off+11].split(b"\x00")[0].decode()
        ftype = chr(d[off+11]); flen = d[off+16]
        fields.append((name, ftype, flen)); off += 32
    recs = []
    for i in range(numrec):
        base = hdrlen + i*reclen
        r = d[base:base+reclen]
        if not r or r[0:1] == b"*":  # deleted
            continue
        o = 1; row = {}
        for n, t, l in fields:
            row[n] = r[o:o+l].decode("latin-1").strip(); o += l
        recs.append(row)
    return recs

def load_by_page(path=DBF):
    """Returns {page_int: [ {subcode,batch,name,mobile,ccode}, ... ]} preserving record order."""
    by_page = defaultdict(list)
    for r in load_records(path):
        try:
            pno = int(r["PAGENO"])
        except ValueError:
            continue
        by_page[pno].append({
            "ccode": r["CCODE"].strip(),
            "subcode": r["SUBCODE"].strip(),
            "batch": r["BATCH"].strip(),
            "name": r["EXMNAME"].strip(),
            "mobile": r["MOBILENO"].strip(),
        })
    return dict(by_page)

if __name__ == "__main__":
    bp = load_by_page()
    print("pages:", len(bp), "total rows:", sum(len(v) for v in bp.values()))
    for p in list(bp)[:3]:
        print(p, bp[p])
