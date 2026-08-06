#!/usr/bin/env python3
"""Grade the spike model on entropy.pdf against the current heuristics.

Task 0 step 5 of docs/plans/2026-08-06-learned-layout-classifier.md: "how many
of the nine equation-headings and the residual fake-table regions does it catch,
and how many genuine headings or tables does it wrongly call formulas?"

entropy.pdf never entered training or model selection, so every number here is
held out.

Method
------
The heuristics' verdicts are read back out of docmill's actual Markdown rather
than by calling the detectors directly: the heading detector is unexported, and
more importantly the production path applies figure drops and column
partitioning before detection, so anything invoked in isolation would be
grading a pipeline the user never runs.

Markdown carries no boxes, so each emitted heading and each table cell is
matched back to the assembled line it came from by normalised text (exact
match first, then a difflib best match above --similarity). Lines that cannot
be matched are reported, not silently dropped.
"""

import argparse
import collections
import difflib
import json
import re


def normalise(text):
    return re.sub(r"[^0-9a-z]+", " ", text.lower()).strip()


def load_jsonl(path, doc=None):
    rows = []
    for line in open(path):
        row = json.loads(line)
        if doc is None or row["doc"] == doc:
            rows.append(row)
    return rows


def parse_markdown(path):
    """Return (headings, table_cells) as lists of raw strings."""
    headings, table_cells = [], []
    for line in open(path):
        line = line.rstrip("\n")
        if line.startswith("#"):
            headings.append(line.lstrip("#").strip())
        elif line.startswith("|"):
            for cell in line.strip("|").split("|"):
                cell = cell.strip()
                if cell and not set(cell) <= set("-: "):
                    table_cells.append(cell)
    return headings, table_cells


class Matcher:
    """Match a Markdown fragment back to the assembled line it came from."""

    def __init__(self, lines, similarity):
        self.lines = lines
        self.similarity = similarity
        self.exact = collections.defaultdict(list)
        for line in lines:
            key = normalise(line["text"])
            if key:
                self.exact[key].append(line)
        self.keys = list(self.exact)

    def match(self, fragment):
        key = normalise(fragment)
        if not key:
            return None
        if key in self.exact:
            return self.exact[key][0]
        # A Markdown heading can be one line, or several lines joined. Prefer a
        # line whose text contains the fragment (or vice versa) before falling
        # back to fuzzy similarity.
        for candidate in self.keys:
            if key in candidate or candidate in key:
                if min(len(key), len(candidate)) >= 8:
                    return self.exact[candidate][0]
        close = difflib.get_close_matches(key, self.keys, n=1, cutoff=self.similarity)
        return self.exact[close[0]][0] if close else None


def rates(tp, fp, fn):
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
    return precision, recall, f1


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", default="out/dataset.jsonl")
    parser.add_argument("--predictions", default="out/entropy.pred.jsonl")
    parser.add_argument("--markdown", default="out/entropy.current.md")
    parser.add_argument("--doc", default="entropy")
    parser.add_argument("--similarity", type=float, default=0.85)
    args = parser.parse_args()

    truth = load_jsonl(args.dataset, args.doc)
    predicted = load_jsonl(args.predictions, args.doc)
    by_key = {(r["page"], r["line"]): r for r in predicted}
    for row in truth:
        row["pred"] = by_key[(row["page"], row["line"])]["formula"]
        row["score"] = by_key[(row["page"], row["line"])]["score"]

    # ---- Headline: the model against the teacher over the whole held-out doc.
    tp = sum(1 for r in truth if r["pred"] and r["label"])
    fp = sum(1 for r in truth if r["pred"] and not r["label"])
    fn = sum(1 for r in truth if not r["pred"] and r["label"])
    p, r, f1 = rates(tp, fp, fn)
    print(f"=== {args.doc}.pdf: model vs teacher, all {len(truth)} lines ===")
    print(f"  Formula lines (teacher): {sum(x['label'] for x in truth)}")
    print(f"  Formula lines (model):   {sum(x['pred'] for x in truth)}")
    print(f"  precision={p:.3f} recall={r:.3f} F1={f1:.3f}  (tp={tp} fp={fp} fn={fn})\n")

    headings, table_cells = parse_markdown(args.markdown)
    matcher = Matcher(truth, args.similarity)

    # ---- Headings the current heuristics emit.
    print(f"=== headings emitted by the current heuristics: {len(headings)} ===")
    unmatched, buckets = [], collections.defaultdict(list)
    for text in headings:
        line = matcher.match(text)
        if line is None:
            unmatched.append(text)
            continue
        bucket = ("formula" if line["label"] else "not-formula") + (
            "/caught" if line["pred"] else "/missed"
        )
        buckets[bucket].append((text, line))

    equation_headings = buckets["formula/caught"] + buckets["formula/missed"]
    print(f"  teacher calls {len(equation_headings)} of them Formula (the equation-headings defect)")
    print(f"    model catches {len(buckets['formula/caught'])}, misses {len(buckets['formula/missed'])}")
    genuine = buckets["not-formula/caught"] + buckets["not-formula/missed"]
    print(f"  teacher calls {len(genuine)} of them non-Formula (genuine headings and other defects)")
    print(f"    model wrongly calls {len(buckets['not-formula/caught'])} of those Formula")
    if unmatched:
        print(f"  unmatched headings (not graded): {len(unmatched)}")
        for text in unmatched[:5]:
            print(f"      {text[:70]!r}")

    print("\n  --- equation-headings, caught ---")
    for text, line in buckets["formula/caught"]:
        print(f"    p{line['page']:>3} score={line['score']:.3f}  {text[:64]!r}")
    print("  --- equation-headings, missed ---")
    for text, line in buckets["formula/missed"]:
        print(f"    p{line['page']:>3} score={line['score']:.3f}  {text[:64]!r}")
    print("  --- non-Formula headings the model calls Formula ---")
    for text, line in buckets["not-formula/caught"]:
        print(f"    p{line['page']:>3} score={line['score']:.3f}  {text[:64]!r}")

    # ---- Table cells the current heuristics emit.
    print(f"\n=== table cells emitted by the current heuristics: {len(table_cells)} ===")
    seen, stats, examples = set(), collections.Counter(), collections.defaultdict(list)
    for text in table_cells:
        line = matcher.match(text)
        if line is None:
            stats["unmatched"] += 1
            continue
        key = (line["page"], line["line"])
        if key in seen:
            continue
        seen.add(key)
        stats["lines"] += 1
        bucket = ("formula" if line["label"] else "not-formula") + (
            "/caught" if line["pred"] else "/missed"
        )
        stats[bucket] += 1
        examples[bucket].append((text, line))

    print(f"  distinct assembled lines behind them: {stats['lines']} (unmatched cells: {stats['unmatched']})")
    print(f"  teacher calls {stats['formula/caught'] + stats['formula/missed']} of those lines Formula (fake-table regions)")
    print(f"    model catches {stats['formula/caught']}, misses {stats['formula/missed']}")
    print(f"  teacher calls {stats['not-formula/caught'] + stats['not-formula/missed']} of those lines non-Formula")
    print(f"    model wrongly calls {stats['not-formula/caught']} of those Formula")
    print("\n  --- fake-table lines the model recovers as Formula ---")
    for text, line in examples["formula/caught"][:20]:
        print(f"    p{line['page']:>3} score={line['score']:.3f}  {text[:64]!r}")
    print("  --- genuine table lines the model wrongly calls Formula ---")
    for text, line in examples["not-formula/caught"][:20]:
        print(f"    p{line['page']:>3} score={line['score']:.3f}  {text[:64]!r}")


if __name__ == "__main__":
    main()
