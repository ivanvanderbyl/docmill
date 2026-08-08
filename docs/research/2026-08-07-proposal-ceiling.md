# The gate was never the problem: measuring the proposal ceiling

**Date:** 2026-08-07
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, the cascade decision
**Follows:** `2026-08-07-region-model.md`
**Verdict:** The region model is not underperforming. **It is being handed candidates
that cannot be right.** Only 42.8% of real tables have any same-class candidate at
IoU ≥ 0.5, against a reachable ceiling of 78.4%. Picture is worse and for a different
reason: 39.8% of picture regions contain no text at all, and docmill extracts no
non-text geometry to find them with.

Three findings, three different fixes. None of them is "train a better gate".

---

## How I was reading the numbers wrong

The previous note said Table candidates are "correct only 11.8% of the time" and
treated that as the gate's opportunity. It is not an opportunity, it is a symptom. The
accept/reject framing scores a candidate that overlaps a real table at IoU 0.49 exactly
as wrong as one that overlaps nothing, and those two demand opposite fixes.

The first labelled row in the training set is the whole story:

```json
{"class": "Table", "lines": 3, "label": 0, "iou": 0.4903}
```

A real table, off by 0.0097.

So I measured the ceiling instead — what any proposer built out of assembled lines
could reach if the gate were perfect. `ceiling_regions.py`, DocLayNet val, 6,489 pages:

| class | regions | ORACLE SET | CONTIGUOUS RUN | current proposer |
|---|---|---|---|---|
| **Table** | 2,269 | **85.8%** | **78.4%** | **42.8%** |
| Section-header | 15,744 | 76.0% | 75.6% | 54.5% |
| Text | 49,186 | 74.8% | 67.9% | 19.7% |
| List-item | 13,320 | 75.6% | 72.1% | 8.8% |
| Page-footer | 5,571 | 75.5% | 75.5% | 73.5% |
| Formula | 1,894 | 62.0% | 56.8% | 42.5% |
| Caption | 1,763 | 58.1% | 55.5% | 42.1% |
| **Picture** | 2,775 | **22.1%** | **13.7%** | **7.5%** |
| Page-header | 6,683 | 26.5% | 26.4% | 19.9% |

ORACLE SET takes exactly the lines inside the teacher region and unions them: the best
any line-based proposer can ever do. CONTIGUOUS RUN asks whether those lines are also
adjacent top-to-bottom, which is what a run-based proposer needs.

**For tables there are 35.6 points sitting on the floor.** That is not a data problem
or a geometry problem. It is the proposer.

## Finding 1 — one bad line label splits a table in two

`GroupLineRegions` emits *maximal* runs of *same-label* lines. Both words are the bug.

I also measured the ceiling using the TEACHER's labels instead of the model's. The
SAME-LABEL column came out identical to ORACLE SET for every class — 85.8% for Table,
to the decimal. When the labels are right, the lines in a region do share a label.

So every point of the 35.6-point gap is line-model label noise. A single line the model
calls `Text` in the middle of a table cuts the candidate into two, and because only
maximal runs are emitted, neither piece — nor their union — is ever offered to the gate.
The failure shape confirms it:

| what a Table candidate actually is | share |
|---|---|
| grazing a real table at IoU 0–0.25 (fragments) | 38.6% |
| no teacher region at all | 36.6% |
| **correct** | **11.8%** |
| sitting on another class at IoU ≥ 0.5 | 6.7% |
| near miss, IoU 0.25–0.50 | 6.3% |

Fragments are the largest bucket. And **merging adjacent candidates would recover 24.3%
of the tables currently missed** — a third of the gap, from grouping alone.

The response is not to chase perfect line labels. It is to stop requiring them: propose
generously and let the region model choose. Sub-runs, merges across a small number of
foreign lines, and the maximal run — then score all of them and keep the best
non-overlapping set. The correct extent only has to be *among* the proposals.

## Finding 2 — half of all pictures are invisible to us

Picture is not a proposer problem. 39.8% of picture regions have **no assembled line
touching them at all** — not a containment threshold artefact, genuinely no text. Add
the ones with only grazing text and the oracle ceiling is 22.1%.

A cascade whose only primitive is the text line cannot see a photograph. And
`pkg/page` offers `TextCell`, `FormField` and `RulingSegment` — **no image placements,
no vector path geometry.** The signal does not exist in our page model, so this is a
parser change before it is a model change.

This also re-reads an earlier result honestly. The line model scores 0.714 F1 on
Picture, and I reported that as replacing the figure heuristic. It does not: it labels
*figure captions and figure text*, which is a different and much easier job than finding
figures.

## Finding 3 — we assemble lines across column boundaries

Look at where the ceiling is lowest after Picture: Page-header 26.5%, Caption 58.1%,
Title 60.9%. For these the content is present but only *grazes* the region — 67.3% of
page-headers, 30.5% of captions, 31.8% of titles.

Two sampled captions on one page say why:

```
[50,282,158,303]  and  [345,282,452,303]
```

Two figures side by side, two captions, one assembled line spanning both. A running
header of `Chapter 3 … Page 45` is the same defect: DocLayNet annotates the fragments,
docmill assembles the strip.

Line assembly is officially out of scope for this project. It is now measurably capping
three classes, so that scope needs revisiting — lines should split at persistent
vertical whitespace before anything classifies them.

## What this changes

The cascade is right. The proposer inside it is a placeholder, and I gated it instead of
fixing it. Ordered by measured gain:

1. **Replace `GroupLineRegions` with an over-generating proposer**, and give the region
   model competition between overlapping proposals rather than an isolated yes/no.
   Table ceiling 42.8% → 78.4%. No new data, no new model, retrain the existing gate on
   the richer candidate set.
2. **Then hand table regions to the cascade and delete the heuristic detector.** Step 1
   is what makes this safe; today's proposer would lose more tables than it found.
3. **Extend `pkg/page` with image placements and path bounding boxes**, then propose
   picture regions from graphics rather than from text. This is the only route to
   Picture and it starts in the parser.
4. **Split assembled lines at persistent vertical whitespace.** Unblocks the Caption,
   Page-header and Title ceilings, and it is a correctness fix independent of any model.

## Reproducing

```
uv run python diagnose_regions.py --regions regions.jsonl --annotations annotations.jsonl --split val --focus Table
uv run python ceiling_regions.py  --lines dataset3.jsonl  --annotations annotations.jsonl --split val
uv run python missing_lines.py    --lines dataset3.jsonl  --annotations annotations.jsonl --split val
```
