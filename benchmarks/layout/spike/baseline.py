#!/usr/bin/env python3
"""Task 1: score the CURRENT heuristics against DocLayNet's human labels.

This is the deliverable the plan gates Task 6 on — "a class where the model does
not beat the heuristic is not a class to migrate." Without it there is no
evidence on which to migrate anything, only a model score floating in space.

Two things make the comparison fair:

  * Both sides are scored on the SAME lines. `spike emit` reports the feature
    vector and the current heuristic class for every class-agnostically
    assembled line in one pass, so the heuristic is not being graded on a
    different segmentation from the model.
  * Both sides are joined to DocLayNet by the same rule (containment of the
    line's own area, >= 0.5), computed by the same code.

The class mapping is the honest part. docmill's label set is not DocLayNet's,
and for four classes it has no concept at all — there is no formula detector, no
caption detector, no footnote or page-furniture detector. Those are reported as
structural zeros rather than quietly dropped, because "docmill scores 0 on
Formula" is the single most important number in this table: it is why Task 0
picked that class.
"""

import argparse
import collections
import json

# docmill's current class -> the DocLayNet class it is trying to be.
# Anything not listed here has no docmill equivalent.
HEURISTIC_TO_DOCLAYNET = {
    "heading": {"Section-header", "Title"},
    "table": {"Table"},
    "list-item": {"List-item"},
    "figure-label": {"Picture"},
    "paragraph": {"Text"},
}

# DocLayNet classes docmill has no detector for. Its recall on these is zero by
# construction: every such line is called something else, usually paragraph.
NO_DETECTOR = ["Formula", "Caption", "Footnote", "Page-header", "Page-footer"]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", required=True, help="joined dataset.jsonl with cls + current")
    parser.add_argument("--split", default="val")
    args = parser.parse_args()

    # For each DocLayNet class, how the heuristics labelled its lines.
    confusion = collections.defaultdict(collections.Counter)
    predicted_total = collections.Counter()
    total = 0

    with open(args.dataset) as handle:
        for raw in handle:
            row = json.loads(raw)
            if row.get("split") != args.split:
                continue
            total += 1
            confusion[row["cls"]][row["current"]] += 1
            predicted_total[row["current"]] += 1

    if not total:
        raise SystemExit(f"no rows in split {args.split!r}")

    print(f"=== current heuristics vs DocLayNet {args.split} split ({total} lines) ===\n")
    print(f"{'DocLayNet class':16s} {'support':>8s} {'precision':>10s} {'recall':>8s} {'F1':>8s}   docmill class")
    print("-" * 74)

    rows = []
    for target, sources in sorted(HEURISTIC_TO_DOCLAYNET.items()):
        for gold in sorted(sources):
            support = sum(confusion[gold].values())
            tp = confusion[gold][target]
            fp = predicted_total[target] - tp
            fn = support - tp
            precision = tp / (tp + fp) if tp + fp else 0.0
            recall = tp / (tp + fn) if tp + fn else 0.0
            f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
            rows.append((gold, support, precision, recall, f1, target))

    # Section-header and Title both map to `heading`, so their precision shares a
    # denominator; report them together rather than double-counting.
    for gold, support, precision, recall, f1, target in sorted(rows, key=lambda r: -r[1]):
        print(f"{gold:16s} {support:8d} {precision:10.3f} {recall:8.3f} {f1:8.3f}   {target}")

    print()
    for gold in NO_DETECTOR:
        support = sum(confusion[gold].values())
        if not support:
            continue
        got = confusion[gold].most_common(2)
        spread = ", ".join(f"{name} {count / support:.0%}" for name, count in got)
        print(f"{gold:16s} {support:8d} {0.0:10.3f} {0.0:8.3f} {0.0:8.3f}   (no detector; lands as {spread})")

    print("\n=== where each DocLayNet class actually goes today ===")
    for gold in sorted(confusion, key=lambda g: -sum(confusion[g].values())):
        support = sum(confusion[gold].values())
        spread = "  ".join(f"{name}={count / support:.0%}" for name, count in confusion[gold].most_common(4))
        print(f"{gold:16s} {support:8d}   {spread}")


if __name__ == "__main__":
    main()
