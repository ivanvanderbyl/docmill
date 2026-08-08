#!/usr/bin/env python3
"""Score the region stage end to end, as a detection problem.

`recall_proposals.py` answers "could the right answer be found" — it scores the
candidate set, ignoring how many wrong candidates sit beside each right one. That
is the correct question for a proposer and the wrong one for a pipeline: a
proposer offering 375 candidates per page reaches high recall trivially, and the
precision it costs is invisible.

This scores what actually survives classification and suppression, which is what
downstream stages consume. Each kept region is matched greedily, highest
confidence first, against unmatched teacher regions of the same class at
IoU >= 0.5. Unmatched kept regions are false positives; unmatched teacher
regions are false negatives.

Greedy one-to-one matching matters. Counting every kept region that overlaps
some teacher region as correct would let ten copies of one table each score a
true positive, which is precisely the failure non-max suppression exists to
prevent and therefore the one the metric must be able to see.
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
    parser.add_argument("--regions", required=True, help="JSONL from `spike propose -select`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--iou", type=float, default=0.5)
    parser.add_argument("--min-score", type=float, default=0.0,
                        help="drop kept regions below this confidence")
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

    kept_by_page = collections.defaultdict(list)
    corrupt = 0
    with open(args.regions) as handle:
        for raw in handle:
            # A progress line written to the same stream can land inside a JSON
            # line. Skipping and COUNTING beats failing, but the count has to be
            # reported: silently dropped regions read as recall loss.
            try:
                row = json.loads(raw)
            except json.JSONDecodeError:
                corrupt += 1
                continue
            if row.get("doc") in pages and row.get("score", 0) >= args.min_score:
                kept_by_page[row["doc"]].append(row)
    if corrupt:
        print(f"WARNING: skipped {corrupt} unparseable lines\n")

    tp = collections.Counter()
    fp = collections.Counter()
    fn = collections.Counter()
    kept_total = 0
    scored_pages = 0

    for doc, regions in pages.items():
        if doc not in kept_by_page:
            continue  # page not emitted; scoring it would credit absence as recall loss
        scored_pages += 1
        kept = sorted(kept_by_page[doc], key=lambda r: -r.get("score", 0))
        kept_total += len(kept)

        truth = [(box, name) for *box, name in regions if name != "Background"]
        matched = [False] * len(truth)

        for region in kept:
            candidate = (region["l"], region["t"], region["r"], region["b"])
            name = region.get("class", "")
            best, best_index = 0.0, -1
            for index, (box, truth_name) in enumerate(truth):
                if matched[index] or truth_name != name:
                    continue
                score = iou(candidate, box)
                if score > best:
                    best, best_index = score, index
            if best >= args.iou:
                matched[best_index] = True
                tp[name] += 1
            else:
                fp[name] += 1

        for index, (_, truth_name) in enumerate(truth):
            if not matched[index]:
                fn[truth_name] += 1

    classes = sorted(set(tp) | set(fp) | set(fn), key=lambda c: -(tp[c] + fn[c]))
    print(f"{scored_pages} pages scored, {kept_total} regions kept "
          f"({kept_total / max(scored_pages, 1):.1f} per page), IoU>={args.iou}"
          + (f", score>={args.min_score}" if args.min_score else ""))
    header = f"{'class':16s} {'truth':>7s} {'kept':>7s} {'prec':>7s} {'recall':>7s} {'F1':>7s}"
    print(header)
    print("-" * len(header))

    weighted, support = 0.0, 0
    for name in classes:
        precision = tp[name] / (tp[name] + fp[name]) if tp[name] + fp[name] else 0.0
        recall = tp[name] / (tp[name] + fn[name]) if tp[name] + fn[name] else 0.0
        f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
        truth_count = tp[name] + fn[name]
        print(f"{name:16s} {truth_count:7d} {tp[name] + fp[name]:7d} "
              f"{precision:7.3f} {recall:7.3f} {f1:7.3f}")
        weighted += f1 * truth_count
        support += truth_count

    print(f"\nweighted F1 {weighted / max(support, 1):.4f} over {support} annotated regions")


if __name__ == "__main__":
    main()
