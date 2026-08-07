#!/usr/bin/env python3
"""Label docmill's candidate column boundaries using FinTabNet's grids.

The layout classifier stops at "this region is a table". Finding the columns is
a different job, and today it is done by hand-tuned gutter thresholds. This
turns it into a learned one, the same way the line classes were: keep the
measurement, drop the cutoff, let annotated data choose.

How the truth is derived
------------------------
FinTabNet gives, per table, an HTML token stream and a list of cells with
bounding boxes in PDF points. Walking the tokens reconstructs the grid — which
row and column each cell occupies, and how far it spans. Cells occupying
exactly ONE column then bound that column horizontally, and the true boundary
between column c and c+1 is the clear strip between them.

Only single-column cells contribute. A cell with colspan=3 sits across two
internal boundaries and says nothing about where either lies, so including it
would smear the columns together — which is precisely the error the hand-tuned
gutter logic makes on tables with merged headers.

A candidate gap emitted by docmill is labelled positive when its interval
overlaps one of those true boundary strips.

Coordinates
-----------
FinTabNet is bottom-left origin (y up); docmill is top-left. Only the x axis
matters for column boundaries, and x needs no conversion — which is why this
join is simpler than the DocLayNet one. The y flip is applied by the Go emitter
when it selects the table region.
"""

import argparse
import collections
import json
import re

TD = re.compile(r"<td")
SPAN = re.compile(r'(colspan|rowspan)="(\d+)"')


def parse_grid(tokens):
    """Return a list of (row, col, colspan) in the order cells appear.

    The token stream interleaves structure and attributes: "<td", ' colspan="6"',
    ">", "</td>". Attributes follow their opening tag, so they are accumulated
    until the tag closes.
    """
    placements = []
    occupied = set()  # (row, col) taken by an earlier rowspan
    row = -1
    col = 0
    pending = None  # {"colspan": n, "rowspan": n} for the tag being read

    def place(colspan, rowspan):
        nonlocal col
        while (row, col) in occupied:
            col += 1
        placements.append((row, col, colspan))
        for r in range(row, row + rowspan):
            for c in range(col, col + colspan):
                if r != row or c != col:
                    occupied.add((r, c))
        col += colspan

    for token in tokens:
        if token == "<tr>":
            row += 1
            col = 0
            continue
        if token.startswith("<td"):
            pending = {"colspan": 1, "rowspan": 1}
            # "<td>" is self-contained; "<td" waits for attributes then ">".
            if token == "<td>":
                place(1, 1)
                pending = None
            continue
        if pending is not None:
            match = SPAN.search(token)
            if match:
                pending[match.group(1)] = int(match.group(2))
                continue
            if token == ">":
                place(pending["colspan"], pending["rowspan"])
                pending = None
    return placements


def true_boundaries(record):
    """Return the clear x-strips between adjacent grid columns."""
    tokens = record["html"]["structure"]["tokens"]
    cells = record["html"]["cells"]
    placements = parse_grid(tokens)
    if len(placements) != len(cells):
        # A stream we did not reconstruct exactly is not worth guessing at.
        return None

    extents = collections.defaultdict(lambda: [float("inf"), float("-inf")])
    for (_, column, colspan), cell in zip(placements, cells):
        box = cell.get("bbox")
        if not box or colspan != 1:
            continue
        extent = extents[column]
        extent[0] = min(extent[0], box[0])
        extent[1] = max(extent[1], box[2])

    columns = sorted(c for c, e in extents.items() if e[0] <= e[1])
    strips = []
    for left, right in zip(columns, columns[1:]):
        edge = extents[left][1]
        start = extents[right][0]
        if start > edge:
            strips.append((edge, start))
    return strips


def overlaps(gap, strip):
    return min(gap[1], strip[1]) - max(gap[0], strip[0]) > 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--gaps", required=True, help="JSONL from tablegaps")
    parser.add_argument("--annotations", required=True, nargs="+")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    truth = {}
    unparsed = 0
    for path in args.annotations:
        with open(path) as handle:
            for raw in handle:
                record = json.loads(raw)
                strips = true_boundaries(record)
                if strips is None:
                    unparsed += 1
                    continue
                truth[record["table_id"]] = strips
    print(f"tables with a reconstructed grid: {len(truth)} ({unparsed} skipped)")

    counts = collections.Counter()
    per_split = collections.Counter()
    tables = set()
    with open(args.gaps) as source, open(args.out, "w") as sink:
        for raw in source:
            row = json.loads(raw)
            strips = truth.get(row["table_id"])
            if strips is None:
                counts["no truth"] += 1
                continue
            gap = (row["l"], row["r"])
            row["label"] = 1 if any(overlaps(gap, s) for s in strips) else 0
            counts["positive" if row["label"] else "negative"] += 1
            per_split[row["split"]] += 1
            tables.add(row["table_id"])
            sink.write(json.dumps(row) + "\n")

    total = counts["positive"] + counts["negative"]
    print(f"candidate gaps: {total} over {len(tables)} tables")
    print(f"  real column boundaries: {counts['positive']} ({counts['positive'] / max(total, 1):.1%})")
    print(f"  not boundaries:         {counts['negative']}")
    print(f"  dropped (no truth):     {counts['no truth']}")
    print(f"by split: {dict(per_split)}")


if __name__ == "__main__":
    main()
