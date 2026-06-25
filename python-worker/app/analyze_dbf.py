from read_dbf import read_dbf

fields, records = read_dbf("/var/data/uploads/SCC58.XLS")
page_counts = {}
for r in records:
    page_num = int(r["PAGENO"])
    page_counts[page_num] = page_counts.get(page_num, 0) + 1

print("Total unique pages in DBF:", len(page_counts))
print("Sorted unique pages in DBF:", sorted(page_counts.keys()))
print("Page counts:")
for p in sorted(page_counts.keys()):
    print(f"  Page {p}: {page_counts[p]} records")
