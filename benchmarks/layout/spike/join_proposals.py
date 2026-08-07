#!/usr/bin/env python3
"""Label region proposals against DocLayNet, for the region CLASSIFIER.

The previous region stage was a gate: candidates arrived already carrying a
class from the line labels they were grouped by, and the model only decided
whether to keep them. The new proposer splits on geometry and clusters ink, so a
candidate arrives with no opinion about what it is. The model therefore has to
assign the class as well, which is what makes its confidence comparable across
candidates — and comparable confidence is the whole basis of non-max
suppression.

So every proposal gets one of twelve labels: the class of the teacher region it
matches at IoU >= 0.5, or Background.

Background is the overwhelming majority — with ~375 proposals per page and ~15
annotated regions, well over 90% of candidates are nothing. That is not a flaw
in the proposer; it is what over-generating means. The trainer handles the
imbalance with class weights rather than by throwing candidates away, because
the near-misses are exactly the negatives worth learning from: a table one line
too short is the hardest and most valuable negative in the set.
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
    parser.add_argument("--out", required=True)
    parser.add_argument("--iou", type=float, default=0.5)
    args = parser.parse_args()

    pages = {}
    with open(args.annotations) as handle:
        for raw in handle:
            page = json.loads(raw)
            sx = page["w"] / page["cw"] if page["cw"] else 1.0
            sy = page["h"] / page["ch"] if page["ch"] else 1.0
            pages[page["hash"]] = {
                "split": page["split"],
                "boxes": [
                    (x * sx, y * sy, (x + w) * sx, (y + h) * sy, CATEGORIES.get(c, "Background"))
                    for (x, y, w, h), c in zip(page["boxes"], page["cats"])
                ],
            }

    per_class = collections.Counter()
    per_split = collections.Counter()
    unknown = missing_features = total = 0

    with open(args.proposals) as source, open(args.out, "w") as sink:
        for raw in source:
            row = json.loads(raw)
            page = pages.get(row["doc"])
            if page is None:
                unknown += 1
                continue
            if not row.get("f"):
                # No feature vector means the line model did not load for that
                # page, so the frac_* features would be absent rather than zero.
                # Training on a silently different vector is exactly the skew
                # this project keeps guarding against.
                missing_features += 1
                continue
            total += 1

            candidate = (row["l"], row["t"], row["r"], row["b"])
            best_score, best_class = 0.0, "Background"
            for *box, name in page["boxes"]:
                score = iou(candidate, box)
                if score > best_score:
                    best_score, best_class = score, name
            label = best_class if best_score >= args.iou else "Background"

            row["label"] = label
            row["iou"] = round(best_score, 4)
            row["split"] = page["split"]
            per_class[label] += 1
            per_split[page["split"]] += 1
            sink.write(json.dumps(row) + "\n")

    print(f"proposals: {total} ({unknown} unknown page, {missing_features} without features)")
    print(f"by split: {dict(per_split)}\n")
    print(f"{'label':16s} {'count':>10s} {'share':>8s}")
    for name, count in per_class.most_common():
        print(f"{name:16s} {count:10d} {count / max(total, 1):7.2%}")


if __name__ == "__main__":
    main()
