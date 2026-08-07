#!/usr/bin/env python3
"""Label candidate regions against DocLayNet, for the REGION model.

The line model says what each line is. This stage asks a different question:
given a run of lines the model called `Table`, is that run actually a table?
Whether a region is a table depends on gutter persistence, column-count
stability and row regularity across the whole run — properties no single line
has, which is why a second stage exists at all.

The join is by IoU rather than by the containment used for lines, and the plan
says why: candidate boxes and teacher boxes are both region-shaped, so the match
is near one-to-one and IoU is meaningful. For lines it was not — one teacher
region covers many lines, and IoU would have scored every one of them near zero.

A candidate is ACCEPTED (label 1) when a teacher region of the same class
overlaps it well. Everything else is rejected, which deliberately lumps together
three different mistakes: a run that is not a region at all, a run of the wrong
class, and a run whose extent is badly wrong. The region model only has to
answer "should this candidate stand", so it does not need them separated.
"""

import argparse
import collections
import json


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


CATEGORIES = {
    1: "Caption", 2: "Footnote", 3: "Formula", 4: "List-item", 5: "Page-footer",
    6: "Page-header", 7: "Picture", 8: "Section-header", 9: "Table", 10: "Text", 11: "Title",
}


def load_annotations(path):
    pages = {}
    with open(path) as handle:
        for raw in handle:
            page = json.loads(raw)
            sx = page["w"] / page["cw"] if page["cw"] else 1.0
            sy = page["h"] / page["ch"] if page["ch"] else 1.0
            boxes = []
            for (x, y, w, h), category in zip(page["boxes"], page["cats"]):
                boxes.append((x * sx, y * sy, (x + w) * sx, (y + h) * sy, CATEGORIES.get(category, "Background")))
            pages[page["hash"]] = {"split": page["split"], "boxes": boxes}
    return pages


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--regions", required=True, help="JSONL from `spike regions`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--iou", type=float, default=0.5)
    args = parser.parse_args()

    pages = load_annotations(args.annotations)
    print(f"annotations: {len(pages)} pages")

    per_class = collections.defaultdict(lambda: [0, 0])
    per_split = collections.Counter()
    unknown = 0
    total = 0

    with open(args.regions) as source, open(args.out, "w") as sink:
        for raw in source:
            row = json.loads(raw)
            page = pages.get(row["doc"])
            if page is None:
                unknown += 1
                continue
            total += 1

            candidate = (row["l"], row["t"], row["r"], row["b"])
            best_same, best_any = 0.0, 0.0
            for *box, name in page["boxes"]:
                score = iou(candidate, box)
                best_any = max(best_any, score)
                if name == row["class"]:
                    best_same = max(best_same, score)

            row["label"] = 1 if best_same >= args.iou else 0
            row["iou"] = round(best_same, 4)
            row["best_any_iou"] = round(best_any, 4)
            row["split"] = page["split"]
            per_class[row["class"]][0] += 1
            per_class[row["class"]][1] += row["label"]
            per_split[page["split"]] += 1
            sink.write(json.dumps(row) + "\n")

    accepted = sum(v[1] for v in per_class.values())
    print(f"candidates: {total} ({unknown} with no annotated page)")
    print(f"accepted at IoU >= {args.iou}: {accepted} ({accepted / max(total, 1):.1%})")
    print(f"by split: {dict(per_split)}\n")
    print(f"{'proposed class':16s} {'candidates':>11s} {'accepted':>9s} {'rate':>7s}")
    for name, (n, ok) in sorted(per_class.items(), key=lambda kv: -kv[1][0]):
        print(f"{name:16s} {n:11d} {ok:9d} {ok / max(n, 1):6.1%}")


if __name__ == "__main__":
    main()
