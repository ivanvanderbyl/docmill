#!/usr/bin/env python3
"""Why do some teacher regions contain no assembled line?

`ceiling_regions.py` reports 71.4% of Page-header regions and 51.3% of Picture
regions with no line inside them. Those two numbers have completely different
causes and only one is a bug, so this separates them:

  OVERLAPPED — a line touches the region but less than half of it is inside.
  The content exists and the containment threshold is what rejected it; the
  region box is tight and the line runs wider, or the line straddles a boundary.

  EMPTY — no assembled line overlaps the region at all. The content never
  reached the classifier. For Picture that is expected (graphics are not text).
  For Page-header it would mean docmill dropped the cells before assembly,
  which contradicts the class-agnostic assembly the whole cascade depends on.
"""

import argparse
import collections
import json

CATEGORIES = {
    1: "Caption", 2: "Footnote", 3: "Formula", 4: "List-item", 5: "Page-footer",
    6: "Page-header", 7: "Picture", 8: "Section-header", 9: "Table", 10: "Text", 11: "Title",
}


def overlap_area(a, b):
    width = min(a[2], b[2]) - max(a[0], b[0])
    height = min(a[3], b[3]) - max(a[1], b[1])
    return width * height if width > 0 and height > 0 else 0.0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--lines", required=True)
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
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

    stats = collections.defaultdict(collections.Counter)
    samples = collections.defaultdict(list)

    for doc, boxes in pages.items():
        lines = lines_by_page.get(doc, [])
        geometry = [(r["l"], r["t"], r["r"], r["b"]) for r in lines]

        for *box, name in boxes:
            if name == "Background":
                continue
            s = stats[name]
            best_containment = 0.0
            touching = 0
            for g in geometry:
                area = (g[2] - g[0]) * (g[3] - g[1])
                if area <= 0:
                    continue
                fraction = overlap_area(g, box) / area
                if fraction > 0:
                    touching += 1
                best_containment = max(best_containment, fraction)

            if best_containment >= 0.5:
                s["has a contained line"] += 1
            elif touching > 0:
                s["overlapped only"] += 1
                if len(samples[name]) < 3:
                    samples[name].append((doc, box, round(best_containment, 3)))
            else:
                s["EMPTY — no line touches"] += 1
                if len(samples[name]) < 3:
                    samples[name].append((doc, box, "empty"))
            s["regions"] += 1

    header = f"{'class':16s} {'regions':>8s} {'contained':>10s} {'overlapped':>11s} {'EMPTY':>9s}"
    print(header)
    print("-" * len(header))
    for name in sorted(stats, key=lambda k: -stats[k]["regions"]):
        s = stats[name]
        n = s["regions"]
        print(f"{name:16s} {n:8d} {s['has a contained line'] / n:9.1%} "
              f"{s['overlapped only'] / n:10.1%} {s['EMPTY — no line touches'] / n:8.1%}")

    for name in ("Page-header", "Picture", "Caption"):
        if samples.get(name):
            print(f"\n{name} samples (doc, box, best containment):")
            for doc, box, detail in samples[name]:
                print(f"  {doc[:16]} [{box[0]:.0f},{box[1]:.0f},{box[2]:.0f},{box[3]:.0f}] {detail}")


if __name__ == "__main__":
    main()
