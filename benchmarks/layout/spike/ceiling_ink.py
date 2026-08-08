#!/usr/bin/env python3
"""Recompute the region ceiling over INK rather than over text lines.

`ceiling_regions.py` measured what a proposer built from assembled text lines
could reach, and found Picture capped at 22.1% because 39.8% of picture regions
contain no text at all. The interpreter now reports images, shadings and filled
paths, so the question can be asked again with the primitive that was missing.

Three candidate sources, each strictly more generous than the last, so the gap
between them says what the proposer would actually have to do:

  SINGLE OBJECT — one drawn object already matches the region. A photograph
  placed as one image XObject is this case, and it needs no grouping at all.

  FORM XOBJECT — one form XObject matches. Figures are very often emitted as a
  single form, in which case the document has already done the grouping for us
  and the box can be read straight off.

  INK CLUSTER — objects merged by proximity. A chart is thousands of tiny path
  operations, so this is the case that decides whether clustering is worth
  building. Text is included as cluster INPUT (a chart's axis labels are part of
  the chart) but text alone can never form a candidate, since that is precisely
  what the old text-only proposer already measured.
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
    """Fraction of inner that lies inside outer."""
    width = min(inner[2], outer[2]) - max(inner[0], outer[0])
    height = min(inner[3], outer[3]) - max(inner[1], outer[1])
    if width <= 0 or height <= 0:
        return 0.0
    area = (inner[2] - inner[0]) * (inner[3] - inner[1])
    return (width * height) / area if area > 0 else 0.0


def union_box(boxes):
    return (min(b[0] for b in boxes), min(b[1] for b in boxes),
            max(b[2] for b in boxes), max(b[3] for b in boxes))


# Clustering is done by rasterising rather than by comparing boxes pairwise.
# Pairwise union-find is the obvious implementation and it does not finish: a
# single chart is thousands of path operations in a small area, which is the
# quadratic worst case AND exactly the input this measurement exists to handle.
# Painting boxes into a coarse grid and taking connected components is linear in
# page area, and models "merge what is within gap of each other" directly.
CELL = 2.0  # points per grid cell


def cluster_boxes(boxes, ink_flags, gap, page_w, page_h, min_ink):
    """Return (union box, non-text count) for each connected blob of ink."""
    import numpy as np
    from scipy import ndimage

    if not boxes:
        return []
    cols = max(1, int(page_w / CELL) + 2)
    rows = max(1, int(page_h / CELL) + 2)
    grid = np.zeros((rows, cols), dtype=bool)

    def clamp(value, high):
        return max(0, min(high, value))

    pad = gap / 2.0
    spans = []
    for left, top, right, bottom in boxes:
        # Both ends need clamping, not just the near one. Content is regularly
        # drawn off the page — bleed marks, oversized backgrounds, objects the
        # clip trimmed to the page edge — and a one-sided clamp turns those into
        # an out-of-range index rather than a cell on the border.
        x0 = clamp(int((left - pad) / CELL), cols - 1)
        x1 = clamp(int((right + pad) / CELL), cols - 1)
        y0 = clamp(int((top - pad) / CELL), rows - 1)
        y1 = clamp(int((bottom + pad) / CELL), rows - 1)
        grid[y0:y1 + 1, x0:x1 + 1] = True
        spans.append((y0, y1, x0, x1))

    labels, count = ndimage.label(grid, structure=np.ones((3, 3), dtype=int))
    if count == 0:
        return []

    # Attribute each box to the blob its top-left cell landed in, so the union
    # box is the ORIGINAL geometry rather than the padded raster (which would
    # inflate every candidate by the gap and depress its IoU).
    extents = {}
    ink_counts = collections.Counter()
    for (left, top, right, bottom), (y0, y1, x0, x1), is_ink in zip(boxes, spans, ink_flags):
        label = labels[y0, x0]
        if label == 0:
            continue
        if label in extents:
            a = extents[label]
            extents[label] = (min(a[0], left), min(a[1], top), max(a[2], right), max(a[3], bottom))
        else:
            extents[label] = (left, top, right, bottom)
        if is_ink:
            ink_counts[label] += 1

    return [box for label, box in extents.items() if ink_counts[label] >= min_ink]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--drawn", required=True, help="JSONL from `spike drawn`")
    parser.add_argument("--annotations", required=True)
    parser.add_argument("--split", default="val")
    parser.add_argument("--iou", type=float, default=0.5)
    parser.add_argument("--gap", type=float, default=6.0,
                        help="points of slack when merging ink into a cluster")
    parser.add_argument("--min-ink", type=int, default=1,
                        help="a cluster needs this many NON-TEXT objects to be a candidate")
    parser.add_argument("--lines", default="",
                        help="line JSONL from `spike emit`; adds TEXT RUN and COMBINED columns")
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

    # Text-run reachability, so the two proposal sources can be scored together.
    # They are complementary by construction — one sees glyphs, the other sees
    # everything else — so neither number alone says what the cascade can reach.
    lines_by_page = collections.defaultdict(list)
    if args.lines:
        with open(args.lines) as handle:
            for raw in handle:
                line = json.loads(raw)
                if line["doc"] in pages:
                    lines_by_page[line["doc"]].append(
                        (line["l"], line["t"], line["r"], line["b"]))

    def text_run_reaches(doc, region):
        """Best IoU from the union of the lines that fall inside the region —
        the oracle set of ceiling_regions.py, its most generous column."""
        geometry = lines_by_page.get(doc)
        if not geometry:
            return 0.0
        members = [g for g in geometry if contained(g, region) >= 0.5]
        if not members:
            return 0.0
        return iou(union_box(members), region)

    stats = collections.defaultdict(collections.Counter)

    with open(args.drawn) as handle:
        for raw in handle:
            row = json.loads(raw)
            boxes = pages.get(row["doc"])
            if boxes is None:
                continue

            singles, forms, cluster_input, is_ink = [], [], [], []
            # A page that draws nothing encodes as null, not []. Those pages are
            # real and must be counted as zero rather than skipped, or the
            # ceiling is measured only over pages that already have ink.
            for obj in row["objects"] or ():
                box = obj["Box"]
                rect = (box["L"], box["T"], box["R"], box["B"])
                if rect[2] <= rect[0] or rect[3] <= rect[1]:
                    continue  # a rule has no area; useless as a region candidate
                kind = obj["Kind"]
                if kind == "form":
                    forms.append(rect)
                    continue  # a form's box is the union of its children
                if kind != "text":
                    singles.append(rect)
                cluster_input.append(rect)
                is_ink.append(kind != "text")

            clusters = cluster_boxes(cluster_input, is_ink, args.gap,
                                     row["page_w"], row["page_h"], args.min_ink)

            for *region, name in boxes:
                if name == "Background":
                    continue
                s = stats[name]
                s["regions"] += 1
                best_single = max((iou(r, region) for r in singles), default=0.0)
                best_form = max((iou(r, region) for r in forms), default=0.0)
                best_cluster = max((iou(r, region) for r in clusters), default=0.0)
                if best_single >= args.iou:
                    s["single"] += 1
                if best_form >= args.iou:
                    s["form"] += 1
                if best_cluster >= args.iou:
                    s["cluster"] += 1
                best_ink = max(best_single, best_form, best_cluster)
                if best_ink >= args.iou:
                    s["any"] += 1
                if args.lines:
                    best_text = text_run_reaches(row["doc"], region)
                    if best_text >= args.iou:
                        s["text"] += 1
                    if max(best_ink, best_text) >= args.iou:
                        s["combined"] += 1

    print(f"ink ceiling on {len(pages)} pages, IoU>={args.iou}, cluster gap {args.gap}pt, "
          f"min {args.min_ink} non-text object(s) per candidate\n")
    tail = f" {'TEXT RUN':>9s} {'COMBINED':>9s}" if args.lines else ""
    header = (f"{'class':16s} {'regions':>8s} {'SINGLE':>8s} {'FORM':>8s} "
              f"{'CLUSTER':>9s} {'INK ANY':>8s}{tail}")
    print(header)
    print("-" * len(header))
    for name in sorted(stats, key=lambda k: -stats[k]["regions"]):
        s = stats[name]
        n = s["regions"]
        row = (f"{name:16s} {n:8d} {s['single'] / n:7.1%} {s['form'] / n:7.1%} "
               f"{s['cluster'] / n:8.1%} {s['any'] / n:7.1%}")
        if args.lines:
            row += f" {s['text'] / n:8.1%} {s['combined'] / n:8.1%}"
        print(row)


if __name__ == "__main__":
    main()
