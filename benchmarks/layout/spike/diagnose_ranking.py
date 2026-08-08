#!/usr/bin/env python3
"""Why does a merged block outrank the paragraphs inside it?

The suppression logic is sound on its own terms: if each paragraph outranked
the merge that contains them, the paragraphs would be kept first and the merge
would then be suppressed by reverse containment. The observed under-segmentation
(Text keeps 3,430 of 5,100; List-item recall 0.068) is therefore a RANKING
failure by construction — the merge must be outranking its own parts.

Rank is class probability x predicted IoU, so there are exactly two ways to be
wrong, and they need different fixes:

  - the classifier is more confident about merges than parts, or
  - the IoU head fails to score a merge low, even though it overlaps each of
    the regions inside it at roughly 1/n.

For every annotated region, this finds its best-matching candidate (the one
suppression SHOULD keep) and the highest-ranked candidate that would suppress
it (IoU >= 0.3 against it, or containment >= 0.8 either way). When the
suppressor ranks higher, the region is lost, and the per-factor breakdown says
which factor did it.
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


def rank(row):
    if row.get("iou_pred", 0) > 0:
        return row["score"] * row["iou_pred"]
    return row["score"]


def would_suppress(a, b):
    """Mirror of pkg/pdf nms.go suppresses()."""
    if iou(a, b) >= 0.3:
        return True
    return contained(b, a) >= 0.8 or contained(a, b) >= 0.8


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidates", required=True,
                        help="JSONL from `spike propose -select -no-suppress`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--focus", default="Text,List-item,Table")
    args = parser.parse_args()

    splits = set(args.split.split(","))
    focus = set(args.focus.split(","))

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
    with open(args.candidates) as handle:
        for raw in handle:
            try:
                row = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if row.get("doc") in pages:
                by_page[row["doc"]].append(row)

    outcome = collections.defaultdict(collections.Counter)
    # Why did the suppressor win? Sum of per-factor comparisons on lost regions.
    factor = collections.defaultdict(lambda: collections.Counter())
    span_of_suppressor = collections.defaultdict(collections.Counter)

    for doc, truth in pages.items():
        candidates = by_page.get(doc)
        if not candidates:
            continue
        for *region, name in truth:
            if name not in focus:
                continue
            o = outcome[name]
            o["regions"] += 1

            best, best_iou = None, 0.0
            for row in candidates:
                score = iou((row["l"], row["t"], row["r"], row["b"]), region)
                if score > best_iou:
                    best, best_iou = row, score
            if best is None or best_iou < 0.5:
                o["not proposed"] += 1
                continue
            if best["class"] == "Background":
                # The candidate exists and the argmax rejected it. Recoverable
                # by a decision rule, unlike "not proposed" — so it is counted
                # separately, along with whether the runner-up class was even
                # right, which bounds what any threshold rule can recover.
                o["called Background"] += 1
                if best.get("nb_class") == name:
                    o["  ...runner-up class correct"] += 1
                continue
            if best["class"] != name:
                o["misclassified"] += 1
                continue

            best_box = (best["l"], best["t"], best["r"], best["b"])
            suppressor = None
            for row in candidates:
                # Background rows cannot suppress: SelectRegions drops them
                # before anything competes.
                if row is best or row["class"] == "Background":
                    continue
                box = (row["l"], row["t"], row["r"], row["b"])
                if rank(row) > rank(best) and would_suppress(box, best_box):
                    if suppressor is None or rank(row) > rank(suppressor):
                        suppressor = row
            if suppressor is None:
                o["kept"] += 1
                continue

            o["outranked"] += 1
            f = factor[name]
            if suppressor["score"] > best["score"]:
                f["classifier prefers suppressor"] += 1
            if suppressor.get("iou_pred", 0) > best.get("iou_pred", 0):
                f["IoU head prefers suppressor"] += 1
            if suppressor["class"] == name:
                f["suppressor is SAME class"] += 1
            else:
                f[f"suppressor is {suppressor['class']}"] += 1
            span_of_suppressor[name][min(suppressor.get("span", 0), 10)] += 1

    for name in sorted(outcome, key=lambda k: -outcome[k]["regions"]):
        o = outcome[name]
        n = o["regions"]
        print(f"\n{name}: {n} annotated regions")
        for key in ("kept", "outranked", "misclassified", "called Background",
                    "  ...runner-up class correct", "not proposed"):
            print(f"  {key:16s} {o[key]:6d}  {o[key] / n:6.1%}")
        if factor[name]:
            print("  when outranked, the suppressor:")
            for key, count in factor[name].most_common(8):
                print(f"    {key:34s} {count:6d}  {count / max(o['outranked'], 1):6.1%}")
            spans = span_of_suppressor[name]
            print("  suppressor span (atomic groups, 10 = 10+):",
                  " ".join(f"{s}:{c}" for s, c in sorted(spans.items())))


if __name__ == "__main__":
    main()
