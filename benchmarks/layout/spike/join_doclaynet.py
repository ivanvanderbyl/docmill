#!/usr/bin/env python3
"""Join DocLayNet's human layout annotations onto docmill's assembled lines.

This replaces join.py's HURIDOCS teacher for the large corpus, and the change is
not merely one of scale. The spike measured the teacher's Formula recall as the
binding constraint — it emitted zero Formula regions for a paper full of display
equations, and ~42% of the model's apparent false positives on entropy.pdf were
equations the teacher had missed. DocLayNet's labels are hand-drawn by trained
annotators, so that ceiling disappears: the student is no longer learning one
model's blind spots.

The other half of the reason is training/serving skew. DocLayNet-v1.1 ships
`pdf_cells` and it is tempting to train on those directly, but they come from
IBM's extractor: font size is recorded as 1.0 for essentially every cell, there
are no bold/italic flags, and cells are more fragmented than docmill's
(median 2 words against docmill's whole-line rects). Features computed from them
would not be the features docmill computes at inference. So we take only the
LABELS from DocLayNet and let docmill extract its own geometry from the
single-page PDFs in DocLayNet_extra.zip, which share the `page_hash` key.

Coordinate reconciliation
-------------------------
DocLayNet boxes are `[x, y, w, h]` in the 1025x1025 COCO image space, y
increasing downwards. Pages are NOT letterboxed into that square — each axis is
scaled independently, so a 612x792 page and a 566x708 page both fill 1025x1025
and the aspect ratio is not preserved. The x and y scale factors therefore
differ and must be applied separately:

    x_pdf = x_coco * original_width  / coco_width
    y_pdf = y_coco * original_height / coco_height

Using one factor for both — the obvious mistake — skews every box by several
points vertically on a typical page, which is a large fraction of a line height.
docmill's own boxes are already top-left origin in PDF points (see the spike
emitter's topEdge/bottomEdge), so after this transform the two agree directly.
"""

import argparse
import collections
import json
import os

# DocLayNet category ids, from the dataset card.
CATEGORIES = {
    1: "Caption",
    2: "Footnote",
    3: "Formula",
    4: "List-item",
    5: "Page-footer",
    6: "Page-header",
    7: "Picture",
    8: "Section-header",
    9: "Table",
    10: "Text",
    11: "Title",
}
BACKGROUND = "Background"


def containment(line, box):
    """Fraction of the LINE's area inside box; both (x0, y0, x1, y1), y down."""
    lx0, ly0, lx1, ly1 = line
    bx0, by0, bx1, by1 = box
    width = min(lx1, bx1) - max(lx0, bx0)
    height = min(ly1, by1) - max(ly0, by0)
    if width <= 0 or height <= 0:
        return 0.0
    area = (lx1 - lx0) * (ly1 - ly0)
    return (width * height) / area if area > 0 else 0.0


def load_annotations(path):
    pages = {}
    with open(path) as handle:
        for raw in handle:
            page = json.loads(raw)
            sx = page["w"] / page["cw"] if page["cw"] else 1.0
            sy = page["h"] / page["ch"] if page["ch"] else 1.0
            boxes = []
            for (x, y, w, h), category in zip(page["boxes"], page["cats"]):
                boxes.append(
                    (
                        x * sx,
                        y * sy,
                        (x + w) * sx,
                        (y + h) * sy,
                        CATEGORIES.get(category, BACKGROUND),
                    )
                )
            pages[page["hash"]] = {
                "split": page["split"],
                "cat": page["cat"],
                "doc": page["doc"],
                "boxes": boxes,
            }
    return pages


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--lines", required=True, help="JSONL from `spike emit`")
    parser.add_argument("--annotations", required=True, help="JSONL from fetch_annotations.py")
    parser.add_argument("--out", required=True)
    parser.add_argument("--containment", type=float, default=0.5)
    args = parser.parse_args()

    pages = load_annotations(args.annotations)
    print(f"annotations: {len(pages)} pages")

    per_class = collections.Counter()
    per_split = collections.Counter()
    unknown = 0
    total = 0

    with open(args.lines) as source, open(args.out, "w") as sink:
        for raw in source:
            row = json.loads(raw)
            total += 1
            # The emitter names each document by its file stem, and every
            # DocLayNet PDF is <page_hash>.pdf, so doc IS the join key.
            page = pages.get(row["doc"])
            if page is None:
                unknown += 1
                continue

            best, label = 0.0, BACKGROUND
            line = (row["l"], row["t"], row["r"], row["b"])
            for bx0, by0, bx1, by1, name in page["boxes"]:
                overlap = containment(line, (bx0, by0, bx1, by1))
                if overlap > best:
                    best, label = overlap, name
            if best < args.containment:
                label = BACKGROUND

            row["cls"] = label
            row["containment"] = round(best, 4)
            row["split"] = page["split"]
            row["doc_category"] = page["cat"]
            # Keep the source document, not the page, as the grouping key: two
            # pages of one report share a template and must not straddle a
            # split. DocLayNet's own splits already respect this; carrying it
            # lets us verify rather than assume.
            row["source_doc"] = page["doc"]
            per_class[label] += 1
            per_split[page["split"]] += 1
            sink.write(json.dumps(row) + "\n")

    kept = total - unknown
    print(f"lines: {total}, joined {kept}, no annotation for {unknown}")
    print(f"\nby split: {dict(per_split)}")
    print(f"\n{'class':16s} {'lines':>9s}   share")
    for name, count in per_class.most_common():
        print(f"{name:16s} {count:9d}   {count / max(kept, 1):6.2%}")


if __name__ == "__main__":
    main()
