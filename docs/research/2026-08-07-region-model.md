# The REGION model: built, strong offline, barely wired in yet

**Date:** 2026-08-07
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, the cascade decision
**Follows:** `2026-08-07-learned-columns.md`
**Verdict:** The second stage exists and is good — **Picture 0.744 F1 on candidates that
are only 5.7% correct, Table 0.680 on candidates only 15.6% correct.** But wired as a
Picture gate it changes almost nothing end to end (−0.002), because Picture is not where
it is needed. `Table` is, and Table routing is not model-owned yet.

---

## Why this stage exists

Two separate measurements demanded it:

- Handing table routing to the line model moved `Table` F1 by **0.008**. Whether a run of
  lines is a table is not a property of any line in it.
- The learned column model turned a display equation into a four-column table, because
  nothing had asked "is this a table at all". It was trained only on regions that *are*
  tables, so it had never been asked the question.

Both are region questions, and the plan anticipated exactly this.

## What was built

**Candidates** — `GroupLineRegions` collects maximal runs of same-label lines with a
2.5-line-height gap tolerance. Deliberately generous: a candidate that is too large can be
rejected, but a table split into three candidates can never be reassembled.

**Features** — 32 region-scoped numbers in `pkg/pdf/regions.go`. Every one is a property
of the RUN: if it could be computed from a single line, the line model already had it.
Gutter persistence (computed with the same `ColumnGapCandidates` code the column model
uses, so both stages agree what a gutter is), column-count stability, row-height and
row-gap regularity, ruling coverage, vertical rule count, extent fractions — and the
distribution of line labels inside the candidate, twelve `frac_*` features.

That last group is the point. "80% of these lines scored Formula" is now a *feature*, so
the equation-versus-table arbitration is learned rather than ordered.

**Training** — 1,088,757 candidates from all 81,471 pages, zero failures, joined to
DocLayNet by IoU ≥ 0.5. IoU is right here where it was wrong for lines: candidate and
teacher boxes are both region-shaped, so the match is near one-to-one.

## Offline result (DocLayNet val, 94,230 candidates)

Accept/reject overall: precision 0.787, recall 0.739, **F1 0.762**.

| proposed class | candidates | actually correct | gate precision | recall | F1 |
|---|---|---|---|---|---|
| Page-footer | 5,358 | 76.4% | 0.856 | 0.994 | **0.920** |
| Section-header | 12,217 | 70.3% | 0.807 | 0.938 | **0.868** |
| Caption | 2,055 | 36.2% | 0.839 | 0.883 | **0.860** |
| Page-header | 2,992 | 44.4% | 0.692 | 0.841 | 0.759 |
| **Picture** | 3,709 | **5.6%** | 0.782 | 0.710 | **0.744** |
| Formula | 2,160 | 37.3% | 0.752 | 0.676 | 0.712 |
| **Table** | 8,258 | **11.8%** | 0.671 | 0.688 | **0.680** |
| Text | 32,891 | 29.4% | 0.752 | 0.526 | 0.619 |
| List-item | 10,450 | 11.2% | 0.652 | 0.088 | 0.155 |

The two rows that matter are Picture and Table. Those candidates are wrong roughly nine
times in ten, and the gate sorts them at 0.78 and 0.67 precision. That is the stage
earning its place.

`frac_Background` is the single strongest feature by gain, followed by `height_frac` and
`frac_Section-header` — so the model leans hardest on the label distribution and the
extent, which is what a region-scoped model should do.

## End to end it does almost nothing — yet

Wired as a gate on `Picture` (both stages must agree before figure innards are dropped),
over 2,334 pages spanning all six document types:

| class | hand-tuned | lines only | + region gate |
|---|---|---|---|
| Picture | 0.110 | 0.714 | **0.712** |
| everything else | — | — | unchanged |
| weighted F1 | 0.557 | 0.679 | 0.679 |

The gate rejected 24 of 3,543 figure-label lines — 0.7%.

**The reason is a mismatch between what the offline number measures and what the gate
sees.** The 5.6% acceptance rate is over ALL candidate regions, most of which are one or
two lines and never become a block. By the time a block reaches `dropPictureBlocks` the
line path has already discarded the weak cases, so the gate is asked mostly about
candidates that were going to be right anyway.

So: the model is good, and it is gating the wrong class.

## Where it should go instead

`Table`. The region model scores 0.680 on Table candidates that are correct only 11.8% of
the time — the largest available gain anywhere in this project. But it cannot be applied
yet, because table routing is still the heuristic's: the line model proposes Table runs
and nothing consumes them.

That is the next piece of work, and it is also what unblocks the column model. The
sequence is now clear:

1. Let the region model own **table region acceptance** — the candidates exist, the gate
   is trained, only the routing is missing.
2. Re-enable `-learned-columns` behind that gate. The column model's one bad failure was
   a region with no columns at all, which is precisely what the region gate rejects.
3. Re-measure TEDS on the per-case distribution, not the headline, per the previous note.

## Status

`-learned-regions` exists and is off by default. The default path is byte-identical over
all 200 DPBench documents; `go vet ./...` and the full suite are clean.
