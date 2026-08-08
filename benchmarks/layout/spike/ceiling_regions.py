#!/usr/bin/env python3
"""How good could ANY line-run region proposer be?

`diagnose_regions.py` showed the region gate is not the bottleneck: only 42.8%
of real tables have any same-class candidate at IoU >= 0.5, and Picture is at
7.5%. A gate cannot accept a candidate nobody proposed, so before touching the
model I need to know where the ceiling actually is and what is holding it down.

Three ceilings, each strictly weaker than the last, so the gap between them
names the thing to fix:

  ORACLE SET — take exactly the lines that fall inside the teacher region and
  union them. This is the best any proposer built out of assembled lines can
  ever do. If this is low the region is not made of text lines at all (a
  picture, a ruled table with no text) and no amount of grouping will find it.

  CONTIGUOUS RUN — are those lines adjacent in top-to-bottom order? If yes, a
  proposer that enumerates runs can reach the region. If no, the region
  interleaves with something else and runs are the wrong primitive.

  SAME-LABEL RUN — do those lines also share a predicted label? This is what
  GroupLineRegions requires today, and the gap between this and CONTIGUOUS RUN
  is the cost of that requirement.
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


def containment(inner, outer):
    width = min(inner[2], outer[2]) - max(inner[0], outer[0])
    height = min(inner[3], outer[3]) - max(inner[1], outer[1])
    if width <= 0 or height <= 0:
        return 0.0
    area = (inner[2] - inner[0]) * (inner[3] - inner[1])
    return (width * height) / area if area > 0 else 0.0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--lines", required=True, help="line JSONL from `spike emit`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--iou", type=float, default=0.5)
    parser.add_argument("--contain", type=float, default=0.5,
                        help="a line belongs to a region when this much of it is inside")
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

    lines_by_page = collections.defaultdict(list)
    with open(args.lines) as handle:
        for raw in handle:
            row = json.loads(raw)
            if row["doc"] in pages:
                lines_by_page[row["doc"]].append(row)

    stats = collections.defaultdict(lambda: collections.Counter())

    for doc, boxes in pages.items():
        lines = sorted(lines_by_page.get(doc, []), key=lambda r: (r["t"] + r["b"]) / 2)
        geometry = [(r["l"], r["t"], r["r"], r["b"]) for r in lines]

        for *box, name in boxes:
            if name == "Background":
                continue
            s = stats[name]
            s["regions"] += 1

            members = [i for i, g in enumerate(geometry) if containment(g, box) >= args.contain]
            if not members:
                s["no lines inside"] += 1
                continue

            union = (min(geometry[i][0] for i in members), min(geometry[i][1] for i in members),
                     max(geometry[i][2] for i in members), max(geometry[i][3] for i in members))
            if iou(union, box) >= args.iou:
                s["oracle set"] += 1
            else:
                s["oracle set misses"] += 1
                continue

            # Contiguity: is the member set an unbroken run in reading order?
            if members == list(range(members[0], members[-1] + 1)):
                s["contiguous run"] += 1
            else:
                s["interleaved"] += 1

            # Same-label: do the member lines share a teacher class? This is the
            # best case for GroupLineRegions, since the model's labels are noisier.
            labels = {lines[i]["cls"] for i in members}
            if len(labels) == 1:
                s["same label"] += 1

    print(f"ceilings on {len(pages)} val pages, IoU>={args.iou}, line-in-region containment>={args.contain}\n")
    header = f"{'class':16s} {'regions':>8s} {'no lines':>9s} {'ORACLE SET':>11s} {'CONTIGUOUS':>11s} {'SAME LABEL':>11s}"
    print(header)
    print("-" * len(header))
    for name in sorted(stats, key=lambda k: -stats[k]["regions"]):
        s = stats[name]
        n = s["regions"]
        print(f"{name:16s} {n:8d} {s['no lines inside'] / n:8.1%}  "
              f"{s['oracle set'] / n:10.1%} {s['contiguous run'] / n:10.1%} {s['same label'] / n:10.1%}")


if __name__ == "__main__":
    main()
