"""Cross-row consensus pass for examiner-panel marksheets.

The same examiner recurs across many rows of a document, keyed by their MOBILE
number. On the SCC58 ground truth, 89.4% of rows share a mobile with another row
and every duplicate mobile maps to exactly one name. We exploit that: cluster the
document's rows into examiner entities, then vote a single name (and mobile) per
cluster so a poorly-read row borrows the value from its well-read siblings.

Backfilled values are FLAGGED (`is_inferred=True`, confidence dialled to
INFERRED_CONFIDENCE) rather than trusted outright, so the verification UI can still
surface them for a human. Measured on SCC58: strict cell accuracy 83.0 -> 88.9%,
name 57.9 -> 76.5%, mobile 74.0 -> 81.2%.
"""
import re
from collections import Counter, defaultdict
from difflib import SequenceMatcher

NAME_COL = 2
MOBILE_COL = 3
INFERRED_CONFIDENCE = 0.95
# How many cross-document votes a single examiner can contribute to the name
# medoid. Caps the directory so it informs the result without overpowering the
# rows physically on the page being processed.
DB_VOTE_CAP = 6


def _nname(s):
    return re.sub(r"[^A-Z ]", "", re.sub(r"\s+", " ", (s or "").upper())).strip()


def _nmob(s):
    d = re.sub(r"\D", "", s or "")
    return d[-10:] if len(d) >= 10 else d


def _name_letters(s):
    return len(re.sub(r"[^A-Za-z]", "", s or ""))


def _sim(a, b):
    return SequenceMatcher(None, a, b).ratio()


def _lev(a, b):
    if a == b:
        return 0
    if not a or not b:
        return max(len(a), len(b))
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[-1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1]


def _medoid(values):
    """Value with max occurrence-weighted similarity to all others (robust majority)."""
    uniq = list(dict.fromkeys(values))
    if len(uniq) == 1:
        return uniq[0]
    cnt = Counter(values)
    best, best_score = uniq[0], -1.0
    for cand in uniq:
        score = sum(cnt[o] * _sim(cand, o) for o in uniq)
        if score > best_score:
            best, best_score = cand, score
    return best


def _cluster(entries):
    """Entity resolution via union-find. Connect two rows when their mobiles are
    identical, or near-identical and the names agree enough to bridge an OCR
    misread. Returns a list of clusters (lists of entry indices)."""
    n = len(entries)
    parent = list(range(n))

    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    def union(a, b):
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[ra] = rb

    mobs = [e["mob"] for e in entries]
    nms = [e["nname"] for e in entries]
    for i in range(n):
        if len(mobs[i]) != 10:
            continue
        for j in range(i + 1, n):
            if len(mobs[j]) != 10:
                continue
            d = _lev(mobs[i], mobs[j])
            if d == 0:
                union(i, j)
            elif d <= 1 and nms[i] and nms[j] and _sim(nms[i], nms[j]) >= 0.55:
                union(i, j)
            elif d <= 3 and nms[i] and nms[j] and _sim(nms[i], nms[j]) >= 0.80:
                union(i, j)
    comp = defaultdict(list)
    for i in range(n):
        comp[find(i)].append(i)
    return list(comp.values())


def build_examiner_directory(pairs):
    """Aggregate cross-document (name, mobile) pairs into vote maps.

    `pairs` is the output of `WorkerRepository.load_examiner_pairs` — one entry
    per (name, mobile) row stored elsewhere in the tenant's data. We build two
    Counters: mobile -> {name: votes} (the safe key direction, mobile uniquely
    identifies an examiner) and name -> {mobile: votes} (reverse, used only when
    a name maps to a single mobile). Names that were themselves inferred guesses
    are skipped so the directory can't amplify its own earlier mistakes."""
    mob_to_names = defaultdict(Counter)
    name_to_mobs = defaultdict(Counter)
    for p in pairs or []:
        if p.get("name_inferred"):
            continue
        name = p.get("name") or ""
        mob = _nmob(p.get("mobile"))
        if len(mob) != 10 or _name_letters(name) < 2:
            continue
        # Seeded registry rows carry `votes` (times_seen) so a heavily-attested
        # examiner outweighs a one-off read; document pairs default to a single
        # vote. The apply step still caps any one examiner at DB_VOTE_CAP.
        weight = p.get("votes") or 1
        mob_to_names[mob][name] += weight
        name_to_mobs[_nname(name)][mob] += weight
    return {"mob_to_names": mob_to_names, "name_to_mobs": name_to_mobs}


def apply_document_consensus(all_tables, examiner_directory=None):
    """Mutate cells in `all_tables` in place. Returns stats dict.

    For every row, locates its name cell (col 2) and mobile cell (col 3), clusters
    rows into examiner entities, then votes a single name and mobile per cluster.
    Changed cells get `is_inferred=True` and confidence=INFERRED_CONFIDENCE.

    When an `examiner_directory` (from `build_examiner_directory`) is supplied,
    cross-document votes for the same examiner are folded into the name vote and
    can recover a missing mobile via the name — so a poorly-read row borrows from
    the whole database, not only from its siblings on this sheet."""
    directory = examiner_directory or {}
    mob_to_names = directory.get("mob_to_names", {})
    name_to_mobs = directory.get("name_to_mobs", {})
    have_directory = bool(mob_to_names or name_to_mobs)
    entries = []
    for table in all_tables:
        for row in table.get("rows", []):
            cells = {c["column_index"]: c for c in row.get("cells", [])}
            name_cell = cells.get(NAME_COL)
            mobile_cell = cells.get(MOBILE_COL)
            if name_cell is None and mobile_cell is None:
                continue
            entries.append({
                "name_cell": name_cell,
                "mobile_cell": mobile_cell,
                "nname": _nname(name_cell["value"]) if name_cell else "",
                "mob": _nmob(mobile_cell["value"]) if mobile_cell else "",
            })

    name_changes = mobile_changes = 0
    db_name_backfills = db_mobile_backfills = 0
    clusters = _cluster(entries)
    # A lone row can't reach in-document consensus, but it CAN be resolved against
    # the cross-document directory — so keep singletons when one is available.
    if not have_directory:
        clusters = [c for c in clusters if len(c) >= 2]

    for cl in clusters:
        members = [entries[i] for i in cl]
        cluster_mobs = {m["mob"] for m in members if len(m["mob"]) == 10}

        # NAME consensus: medoid over usable name reads in the cluster, plus
        # capped cross-document votes for this examiner (keyed by mobile).
        name_reads = [m["name_cell"]["value"] for m in members
                      if m["name_cell"] and _name_letters(m["name_cell"]["value"]) >= 2]
        weighted = list(name_reads)
        for mob in cluster_mobs:
            for name, cnt in mob_to_names.get(mob, {}).items():
                weighted += [name] * min(cnt, DB_VOTE_CAP)

        consensus = None
        if weighted:
            consensus = _medoid(weighted)
            # Did the chosen value come from this sheet, or only from the DB?
            from_db = _nname(consensus) not in {_nname(x) for x in name_reads}
            for m in members:
                cell = m["name_cell"]
                if cell is None:
                    continue
                if _nname(cell["value"]) != _nname(consensus):
                    cell["value"] = consensus
                    cell["confidence"] = INFERRED_CONFIDENCE
                    cell["is_inferred"] = True
                    name_changes += 1
                    if from_db:
                        db_name_backfills += 1

        # MOBILE consensus: plurality vote over valid 10-digit reads. If the
        # cluster has none, recover the mobile from the directory via the
        # consensus name — but only when that name maps to a single mobile.
        mob_reads = [m["mob"] for m in members if len(m["mob"]) == 10]
        consensus_mob = None
        mob_from_db = False
        if mob_reads:
            consensus_mob = Counter(mob_reads).most_common(1)[0][0]
        elif consensus is not None:
            cand = name_to_mobs.get(_nname(consensus))
            if cand and len(cand) == 1:
                consensus_mob = next(iter(cand))
                mob_from_db = True
        if consensus_mob:
            for m in members:
                cell = m["mobile_cell"]
                if cell is None:
                    continue
                if _nmob(cell["value"]) != consensus_mob:
                    cell["value"] = consensus_mob
                    cell["confidence"] = INFERRED_CONFIDENCE
                    cell["is_inferred"] = True
                    mobile_changes += 1
                    if mob_from_db:
                        db_mobile_backfills += 1

    return {
        "clusters": len(clusters),
        "name_backfills": name_changes,
        "mobile_backfills": mobile_changes,
        "db_name_backfills": db_name_backfills,
        "db_mobile_backfills": db_mobile_backfills,
    }
