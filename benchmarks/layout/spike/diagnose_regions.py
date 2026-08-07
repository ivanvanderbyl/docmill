#!/usr/bin/env python3
"""Ask what the region stage is actually getting wrong.

The headline said "Table candidates are correct only 11.8% of the time" and I
read that as a classification problem. It may not be one. A candidate that
overlaps a real table at IoU 0.49 is labelled exactly as wrong as one that
overlaps nothing, and the two demand completely different fixes: the first is a
boundary that is one line off, the second is a run that is not a table at all.

So this reports two things the accept/reject framing cannot:

  RECALL CEILING — for every teacher region, the best IoU any candidate of the
  same class achieves. No gate can accept a candidate that was never proposed,
  so this is the hard upper bound on the whole cascade, and it is a property of
  GroupLineRegions rather than of any model.

  FAILURE SHAPE — for candidates the join rejected, how close they came and what
  they actually landed on. Near-misses are an extent problem; zero-overlap
  candidates are a classification problem; candidates sitting on a DIFFERENT
  class at high IoU are the line model's problem, one stage earlier.
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
    """Fraction of `inner` that lies inside `outer`."""
    width = min(inner[2], outer[2]) - max(inner[0], outer[0])
    height = min(inner[3], outer[3]) - max(inner[1], outer[1])
    if width <= 0 or height <= 0:
        return 0.0
    area = (inner[2] - inner[0]) * (inner[3] - inner[1])
    return (width * height) / area if area > 0 else 0.0


def load_annotations(path, splits):
    pages = {}
    with open(path) as handle:
        for raw in handle:
            page = json.loads(raw)
            if page["split"] not in splits:
                continue
            sx = page["w"] / page["cw"] if page["cw"] else 1.0
            sy = page["h"] / page["ch"] if page["ch"] else 1.0
            boxes = []
            for (x, y, w, h), category in zip(page["boxes"], page["cats"]):
                boxes.append((x * sx, y * sy, (x + w) * sx, (y + h) * sy,
                              CATEGORIES.get(category, "Background")))
            pages[page["hash"]] = boxes
    return pages


def bucket(score):
    if score >= 0.75:
        return ">=0.75"
    if score >= 0.5:
        return "0.50-0.75"
    if score >= 0.25:
        return "0.25-0.50"
    if score > 0.0:
        return "0.00-0.25"
    return "none"


ORDER = [">=0.75", "0.50-0.75", "0.25-0.50", "0.00-0.25", "none"]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--regions", required=True, help="raw JSONL from `spike regions`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--focus", default="Table", help="class to break down in detail")
    args = parser.parse_args()

    splits = set(args.split.split(","))
    pages = load_annotations(args.annotations, splits)
    print(f"annotations: {len(pages)} pages in split(s) {sorted(splits)}\n")

    # Candidates, grouped per page, only for pages in this split.
    by_page = collections.defaultdict(list)
    with open(args.regions) as handle:
        for raw in handle:
            row = json.loads(raw)
            if row["doc"] in pages:
                by_page[row["doc"]].append(row)

    # ---- 1. Recall ceiling, per teacher class.
    ceiling = collections.defaultdict(collections.Counter)
    # ---- 2. Failure shape, for the focus class.
    shape = collections.Counter()
    landed_on = collections.Counter()
    near_miss_lines = collections.Counter()
    proposed = collections.Counter()
    union_rescue = 0
    union_total = 0

    for doc, boxes in pages.items():
        candidates = by_page.get(doc, [])
        same_class = collections.defaultdict(list)
        for row in candidates:
            same_class[row["class"]].append(row)
            proposed[row["class"]] += 1

        for *box, name in boxes:
            if name == "Background":
                continue
            best = 0.0
            for row in same_class.get(name, []):
                best = max(best, iou((row["l"], row["t"], row["r"], row["b"]), box))
            ceiling[name][bucket(best)] += 1

            # Could a MERGE of adjacent same-class candidates have hit it? This
            # separates "the grouper split one table into three" from "the line
            # model never called these lines Table at all".
            if name == args.focus:
                union_total += 1
                if best < 0.5:
                    parts = [r for r in same_class.get(name, [])
                             if containment((r["l"], r["t"], r["r"], r["b"]), box) >= 0.8]
                    if parts:
                        merged = (min(p["l"] for p in parts), min(p["t"] for p in parts),
                                  max(p["r"] for p in parts), max(p["b"] for p in parts))
                        if iou(merged, box) >= 0.5:
                            union_rescue += 1

        # Failure shape for the focus class.
        for row in same_class.get(args.focus, []):
            candidate = (row["l"], row["t"], row["r"], row["b"])
            best_same, best_other, other_name = 0.0, 0.0, "-"
            for *box, name in boxes:
                score = iou(candidate, box)
                if name == args.focus:
                    best_same = max(best_same, score)
                elif score > best_other:
                    best_other, other_name = score, name
            if best_same >= 0.5:
                shape["accepted by join"] += 1
                continue
            if best_same >= 0.25:
                shape["near miss (0.25-0.50 on a real table)"] += 1
                near_miss_lines[min(row["lines"], 10)] += 1
            elif best_same > 0:
                shape["grazing (0-0.25 on a real table)"] += 1
            elif best_other >= 0.5:
                shape[f"sits on another class (IoU>=0.5)"] += 1
                landed_on[other_name] += 1
            else:
                shape["no teacher region at all"] += 1

    print("RECALL CEILING — best IoU any same-class candidate reached, per teacher region")
    print(f"{'teacher class':16s} {'regions':>8s} " + " ".join(f"{b:>10s}" for b in ORDER))
    for name in sorted(ceiling, key=lambda k: -sum(ceiling[k].values())):
        counts = ceiling[name]
        total = sum(counts.values())
        cells = " ".join(f"{counts[b] / total:9.1%} " for b in ORDER)
        print(f"{name:16s} {total:8d} {cells}")

    print(f"\nreachable at IoU>=0.5 — the hard ceiling on the cascade:")
    for name in sorted(ceiling, key=lambda k: -sum(ceiling[k].values())):
        counts = ceiling[name]
        total = sum(counts.values())
        hit = counts[">=0.75"] + counts["0.50-0.75"]
        print(f"  {name:16s} {hit / total:6.1%}  ({hit}/{total}, proposed {proposed[name]} candidates)")

    print(f"\nFAILURE SHAPE for {args.focus} candidates ({sum(shape.values())} total)")
    for reason, count in shape.most_common():
        print(f"  {reason:44s} {count:7d}  {count / max(sum(shape.values()), 1):6.1%}")
    if landed_on:
        print(f"\n  what the mislabelled ones actually sit on:")
        for name, count in landed_on.most_common():
            print(f"    {name:16s} {count:7d}")
    if near_miss_lines:
        print(f"\n  near-miss candidates by line count (10 = 10 or more):")
        for lines in sorted(near_miss_lines):
            print(f"    {lines:2d} lines {near_miss_lines[lines]:7d}")

    print(f"\n  {args.focus} regions missed at IoU>=0.5 that MERGING adjacent "
          f"candidates would recover: {union_rescue}/{union_total} ({union_rescue / max(union_total, 1):.1%})")


if __name__ == "__main__":
    main()
