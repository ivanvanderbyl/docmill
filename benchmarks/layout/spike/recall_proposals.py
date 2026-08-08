#!/usr/bin/env python3
"""What does the SHIPPING Go proposer actually reach?

Every ceiling number so far came from a Python reimplementation of what a
proposer could reach in principle. This scores the candidates Go really emits,
against the same annotations, so the two can be compared directly. A gap between
"reachable" and "reached" is a bug in the Go proposer, and finding it after a
model has been trained on these candidates would mean retraining.

Reported per class:

  RECALL — teacher regions with some proposal at IoU >= 0.5. This is the hard
  ceiling on everything downstream; the region model can only ever choose among
  what is here.

  by source — which proposal kind found it. Text runs and ink clusters are
  complementary, and the split says whether both are pulling their weight.

  cost — proposals per page, and how many proposals exist per region found. The
  region model has to sort through all of them, so recall bought with a hundred
  times more candidates is not free.
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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--proposals", required=True, help="JSONL from `spike propose`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--iou", type=float, default=0.5)
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
                (x * sx, y * sy, (x + w) * sx, (y + h) * sy, CATEGORIES.get(c, "Background"))
                for (x, y, w, h), c in zip(page["boxes"], page["cats"])
            ]

    by_page = collections.defaultdict(list)
    total_proposals = 0
    source_totals = collections.Counter()
    with open(args.proposals) as handle:
        for raw in handle:
            row = json.loads(raw)
            if row["doc"] in pages:
                by_page[row["doc"]].append(row)
                total_proposals += 1
                source_totals[row["source"]] += 1

    found = collections.defaultdict(collections.Counter)
    for doc, regions in pages.items():
        proposals = by_page.get(doc, [])
        boxes = [(p["l"], p["t"], p["r"], p["b"], p["source"]) for p in proposals]
        for *region, name in regions:
            if name == "Background":
                continue
            s = found[name]
            s["regions"] += 1
            best, best_source = 0.0, None
            by_source = {}
            for *box, source in boxes:
                score = iou(box, region)
                if score > by_source.get(source, 0.0):
                    by_source[source] = score
                if score > best:
                    best, best_source = score, source
            if best >= args.iou:
                s["found"] += 1
                s[f"src:{best_source}"] += 1
            for source, score in by_source.items():
                if score >= args.iou:
                    s[f"any:{source}"] += 1

    page_count = len(pages)
    print(f"{total_proposals} proposals over {page_count} pages "
          f"({total_proposals / max(page_count, 1):.0f} per page), IoU>={args.iou}")
    print("by source: " + ", ".join(f"{k}={v}" for k, v in source_totals.most_common()) + "\n")

    header = (f"{'class':16s} {'regions':>8s} {'RECALL':>8s} {'text':>8s} {'ink':>8s} "
              f"{'ink+text':>9s}")
    print(header)
    print("-" * len(header))
    for name in sorted(found, key=lambda k: -found[k]["regions"]):
        s = found[name]
        n = s["regions"]
        print(f"{name:16s} {n:8d} {s['found'] / n:7.1%} {s['any:text'] / n:7.1%} "
              f"{s['any:ink'] / n:7.1%} {s['any:ink+text'] / n:8.1%}")

    total_regions = sum(s["regions"] for s in found.values())
    total_found = sum(s["found"] for s in found.values())
    print(f"\noverall {total_found}/{total_regions} = {total_found / max(total_regions, 1):.1%}")
    print(f"cost: {total_proposals / max(total_found, 1):.1f} proposals per region found")


if __name__ == "__main__":
    main()
