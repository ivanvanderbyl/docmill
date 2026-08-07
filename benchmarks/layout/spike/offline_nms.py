#!/usr/bin/env python3
"""Sweep region decision rules offline, against candidates Go already emitted.

The diagnosis found the largest recoverable loss in one place: candidates whose
argmax is Background but whose runner-up class is CORRECT — 22.6% of all tables,
16.4% of list items, 11.0% of text blocks. The model knows what they are; the
decision rule lets Background outvote it.

Each Go emit of 800 pages costs ~10 minutes, so the rule is tuned here instead:
this simulates SelectRegions in Python over the emitted candidates and scores
each policy end to end with the same greedy one-to-one matcher eval_regions
uses. The winning rule is then ported to Go ONCE and re-measured there — the
Python is for choosing, the Go number is the one that counts.

Policies:
  argmax    — what ships today: keep argmax classes, rank score*iou_pred.
  thresh:X  — ignore Background in classification. Every candidate is its best
              REAL class; keep it when nb_score*iou_pred >= X. Background only
              enters as a veto through nb_score being low.
"""

import argparse
import collections
import json

CATEGORIES = {
    1: "Caption", 2: "Footnote", 3: "Formula", 4: "List-item", 5: "Page-footer",
    6: "Page-header", 7: "Picture", 8: "Section-header", 9: "Table", 10: "Text", 11: "Title",
}


def iou(a, b):
    width = min(a[2], b[2]) - max(a[0], b[0])
    height = min(a[3], b[3]) - max(a[1], b[1])
    if width <= 0 or height <= 0:
        return 0.0
    overlap = width * height
    area_a = (a[2] - a[0]) * (a[3] - a[1])
    area_b = (b[2] - b[0]) * (b[3] - b[1])
    union = area_a + area_b - overlap
    return overlap / union if union > 0 else 0.0


def contained(inner, outer):
    width = min(inner[2], outer[2]) - max(inner[0], outer[0])
    height = min(inner[3], outer[3]) - max(inner[1], outer[1])
    if width <= 0 or height <= 0:
        return 0.0
    area = (inner[2] - inner[0]) * (inner[3] - inner[1])
    return (width * height) / area if area > 0 else 0.0


def box_of(row):
    return (row["l"], row["t"], row["r"], row["b"])


def suppresses(a, b):
    """Mirror of pkg/pdf nms.go."""
    if iou(a, b) >= 0.3:
        return True
    return contained(b, a) >= 0.8 or contained(a, b) >= 0.8


def select(rows):
    """Mirror of SelectRegions: rank-sorted greedy keep."""
    rows = sorted(rows, key=lambda r: (-r["_rank"], -(r["r"] - r["l"]) * (r["b"] - r["t"])))
    kept = []
    for row in rows:
        box = box_of(row)
        if not any(suppresses(box_of(winner), box) for winner in kept):
            kept.append(row)
    return kept


def apply_policy(rows, policy):
    if policy == "argmax":
        out = []
        for row in rows:
            if row["class"] == "Background":
                continue
            r = dict(row)
            r["_class"] = row["class"]
            r["_rank"] = row["score"] * row.get("iou_pred", 1)
            out.append(r)
        return out
    if policy.startswith("thresh:"):
        cutoff = float(policy.split(":")[1])
        out = []
        for row in rows:
            rank = row.get("nb_score", 0) * row.get("iou_pred", 1)
            if rank < cutoff:
                continue
            r = dict(row)
            r["_class"] = row.get("nb_class") or row["class"]
            r["_rank"] = rank
            out.append(r)
        return out
    raise ValueError(policy)


def evaluate(pages, kept_by_page):
    tp = collections.Counter()
    fp = collections.Counter()
    fn = collections.Counter()
    for doc, truth in pages.items():
        kept = kept_by_page.get(doc, [])
        matched = [False] * len(truth)
        for row in sorted(kept, key=lambda r: -r["_rank"]):
            box = box_of(row)
            best, index = 0.0, -1
            for i, (t_box, t_name) in enumerate(truth):
                if matched[i] or t_name != row["_class"]:
                    continue
                score = iou(box, t_box)
                if score > best:
                    best, index = score, i
            if best >= 0.5:
                matched[index] = True
                tp[row["_class"]] += 1
            else:
                fp[row["_class"]] += 1
        for i, (_, t_name) in enumerate(truth):
            if not matched[i]:
                fn[t_name] += 1
    return tp, fp, fn


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidates", required=True)
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--policies", default="argmax,thresh:0.05,thresh:0.1,thresh:0.15,thresh:0.2,thresh:0.3")
    parser.add_argument("--detail", default="", help="print per-class rows for this policy")
    args = parser.parse_args()

    splits = set(args.split.split(","))
    pages = {}
    with open(args.annotations) as handle:
        for raw in handle:
            page = json.loads(raw)
            if page["split"] not in splits:
                continue
            sx = page["w"] / page["cw"] if page["cw"] else 1.0
            sy = page["h"] / page["ch"] if page["ch"] else 1.0
            pages[page["hash"]] = [
                ((x * sx, y * sy, (x + w) * sx, (y + h) * sy), CATEGORIES.get(c, "Background"))
                for (x, y, w, h), c in zip(page["boxes"], page["cats"])
            ]

    by_page = collections.defaultdict(list)
    with open(args.candidates) as handle:
        for raw in handle:
            try:
                row = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if row.get("doc") in pages:
                by_page[row["doc"]].append(row)
    pages = {doc: truth for doc, truth in pages.items() if doc in by_page}
    print(f"{len(pages)} pages with candidates\n")

    for policy in args.policies.split(","):
        kept_by_page = {}
        kept_total = 0
        for doc, rows in by_page.items():
            kept = select(apply_policy(rows, policy))
            kept_by_page[doc] = kept
            kept_total += len(kept)
        tp, fp, fn = evaluate(pages, kept_by_page)

        weighted, support = 0.0, 0
        lines = []
        for name in sorted(set(tp) | set(fn), key=lambda c: -(tp[c] + fn[c])):
            truth_count = tp[name] + fn[name]
            precision = tp[name] / (tp[name] + fp[name]) if tp[name] + fp[name] else 0.0
            recall = tp[name] / truth_count if truth_count else 0.0
            f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
            weighted += f1 * truth_count
            support += truth_count
            lines.append(f"  {name:16s} {truth_count:6d} {precision:6.3f} {recall:6.3f} {f1:6.3f}")
        print(f"{policy:12s} weighted F1 {weighted / max(support, 1):.4f}  "
              f"({kept_total / max(len(pages), 1):.1f} kept/page)")
        if policy == args.detail:
            print("\n".join(lines))


if __name__ == "__main__":
    main()
