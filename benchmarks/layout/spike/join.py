#!/usr/bin/env python3
"""Join the HURIDOCS teacher's Formula boxes onto docmill's assembled lines.

Task 0 step 3 of docs/plans/2026-08-06-learned-layout-classifier.md. The plan
warns that "label alignment is where this fails, not training", so this script
does one job carefully and reports what it could not decide.

Coordinate reconciliation
-------------------------
Both sides are top-left origin with y increasing downwards, in PDF points:

  * docmill  -- pkg/pdf.TextRectsToCells flips PDFium's bottom-left rects with
                ``pageHeight - y`` and stamps geom.TopLeft. The spike emitter
                re-normalises through topEdge/bottomEdge so l/t/r/b are always
                TopLeft regardless of the box's recorded origin.
  * HURIDOCS -- reports left/top/width/height plus the page_width/page_height it
                measured them against.

The one real mismatch is scale: HURIDOCS rounds page dimensions to integers
(595 for an A4 page docmill calls 595.276), so every teacher box is rescaled by
docmill's page size over the teacher's before any overlap is computed. Skipping
that would shift a box by up to a point at the page foot -- small, but the
whole join turns on fractions of a line height.

Join rule
---------
A line is labelled Formula when the fraction of the LINE's own box area lying
inside some Formula region clears --containment (default 0.5). Containment of
the line, not IoU: teacher regions are region-shaped and a single Formula box
routinely covers several assembled lines, so IoU would score every line in a
multi-line equation near zero. Lines landing in the ambiguous band around the
cutoff are counted and reported rather than silently rounded.
"""

import argparse
import collections
import json
import os
import sys


def load_labels(path, keep_type):
    """Return {page_number: [(l, t, r, b, page_w, page_h)]} for one document."""
    regions = collections.defaultdict(list)
    with open(path) as handle:
        for region in json.load(handle):
            if region["type"] != keep_type:
                continue
            left, top = float(region["left"]), float(region["top"])
            width, height = float(region["width"]), float(region["height"])
            regions[int(region["page_number"])].append(
                (
                    left,
                    top,
                    left + width,
                    top + height,
                    float(region["page_width"]),
                    float(region["page_height"]),
                )
            )
    return regions


def containment(line, region):
    """Fraction of the line box's area that lies inside the region box."""
    lx0, ly0, lx1, ly1 = line
    rx0, ry0, rx1, ry1 = region
    overlap_w = min(lx1, rx1) - max(lx0, rx0)
    overlap_h = min(ly1, ry1) - max(ly0, ry0)
    if overlap_w <= 0 or overlap_h <= 0:
        return 0.0
    line_area = (lx1 - lx0) * (ly1 - ly0)
    if line_area <= 0:
        # A zero-height line (a lone rule or a stray glyph) has no area to
        # contain; treat it as unmatched rather than dividing by zero.
        return 0.0
    return (overlap_w * overlap_h) / line_area


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--lines", default="out/lines.jsonl")
    parser.add_argument("--labels", default="labels")
    parser.add_argument("--out", default="out/dataset.jsonl")
    parser.add_argument("--type", default="Formula")
    parser.add_argument("--containment", type=float, default=0.5)
    parser.add_argument(
        "--ambiguous-band",
        type=float,
        default=0.15,
        help="report lines whose best containment falls within this much of the cutoff",
    )
    args = parser.parse_args()

    labels_by_doc = {}
    for name in os.listdir(args.labels):
        if name.endswith(".json"):
            labels_by_doc[name[: -len(".json")]] = load_labels(
                os.path.join(args.labels, name), args.type
            )

    per_doc = collections.Counter()
    per_doc_pos = collections.Counter()
    ambiguous = []
    missing_labels = set()
    total = 0

    with open(args.lines) as source, open(args.out, "w") as sink:
        for raw in source:
            row = json.loads(raw)
            doc = row["doc"]
            total += 1
            per_doc[doc] += 1

            regions = labels_by_doc.get(doc)
            if regions is None:
                missing_labels.add(doc)
                continue

            best = 0.0
            for region in regions.get(row["page"], []):
                rx0, ry0, rx1, ry1, teacher_w, teacher_h = region
                # Rescale the teacher box into docmill's page units.
                sx = row["page_w"] / teacher_w if teacher_w else 1.0
                sy = row["page_h"] / teacher_h if teacher_h else 1.0
                scaled = (rx0 * sx, ry0 * sy, rx1 * sx, ry1 * sy)
                best = max(best, containment((row["l"], row["t"], row["r"], row["b"]), scaled))

            label = 1 if best >= args.containment else 0
            per_doc_pos[doc] += label
            if abs(best - args.containment) < args.ambiguous_band and best > 0:
                ambiguous.append((doc, row["page"], round(best, 3), row["text"][:60]))

            row["label"] = label
            row["containment"] = best
            sink.write(json.dumps(row) + "\n")

    positives = sum(per_doc_pos.values())
    print(f"lines: {total}  {args.type}: {positives} ({positives / max(total, 1):.2%})")
    if missing_labels:
        print(f"WARNING no teacher labels for: {sorted(missing_labels)}", file=sys.stderr)
    print(f"ambiguous (best containment within {args.ambiguous_band} of cutoff): {len(ambiguous)}")
    for entry in ambiguous[:15]:
        print("   ", entry)
    print()
    print(f"{'document':22s} {'lines':>7s} {args.type:>9s} {'rate':>7s}")
    for doc in sorted(per_doc):
        n, p = per_doc[doc], per_doc_pos[doc]
        print(f"{doc:22s} {n:7d} {p:9d} {p / max(n, 1):6.1%}")


if __name__ == "__main__":
    main()
