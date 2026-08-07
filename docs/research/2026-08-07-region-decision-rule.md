# The region stage measured honestly: 0.55, not 0.25

**Date:** 2026-08-07
**Follows:** `2026-08-07-region-classifier-nms.md`
**Verdict:** After a softmax bug fix, a decision-rule change, and — most importantly — a
fair sample, the region stage scores **weighted F1 0.5525** end to end on a seeded random
800-page DocLayNet val sample. The 0.25 in the previous note was measured through a
broken softmax on a biased sample. Both problems are fixed and both are worth
remembering.

---

## The three findings, in the order they were dug up

### 1. A softmax that overwrote its own input

`PredictProbabilities` read `scores[best]` inside the loop that overwrites the slice.
Once the loop passes `best`, that slot holds `exp(0)=1`, so every later class subtracted
**1 instead of the maximum**.

With ordinary raw scores this is the bad kind of wrong: finite, plausible-looking, and
silently distorting every probability after the winning class — every ranking measured
through it is suspect. With this model's huge raw scores (see below) it became loud:
`exp(raw−1)` overflowed to `+Inf`, the sum went `Inf`, probabilities came back `0` for
the winner and `NaN` for the rest, and the JSON emitter refused the NaN twelve documents
into a run. The loud version is the lucky one.

Fixing it alone moved the (biased-sample) score 0.251 → 0.290 and doubled Page-header.

Two infrastructure traps surfaced during the hunt, both now closed in `goenv.sh`:
the Go module cache lived in tmpfs and half-vanished (the sandbox blocks
proxy.golang.org, so missing zips mean *unbuildable*, not slow), and one
`go build | head` pipeline masked exactly that failure — the "diagnosis" then ran a
stale binary and produced a dataset without the field under diagnosis. Its conclusion
("the IoU head is never wrong") was an artefact: it compared zero against zero.

### 2. The argmax let Background outvote correct answers

With trustworthy numbers, the biggest recoverable loss was candidates whose argmax was
Background while the **runner-up class was correct**: 22.6% of all tables, 16.4% of list
items, 11.0% of text blocks. The model knew what they were; the decision rule didn't
listen.

The fix: every candidate is scored as its best REAL class, and survives only when
`real-class probability × predicted IoU ≥ 0.2`. Background still vetoes — a candidate
that is probably nothing has a low real-class score, a low predicted overlap, or both —
but it vetoes through the product instead of winning the argmax outright.

The 0.2 was swept offline: 0.5399 (argmax) / 0.5448 (0.1) / **0.5525 (0.2)** / 0.5469
(0.3). The sweep ran in a Python simulator of `SelectRegions` that matches the Go
pipeline to four decimal places on identical input, so the choice transferred exactly:
the Go re-run reproduced 0.5525 in every per-class row.

### 3. The sample was biased, and it cost a factor of two in morale

Every "800-page" measurement before this note used the FIRST 800 pages of the val file.
DocLayNet's val split is not shuffled. On those pages only 33.7% of Text regions have any
matching candidate, against 69.6% corpus-wide; Page-header is the mirror image (86.9%
reachable there, 44.7% corpus-wide).

Proved cleanly: the old and current proposers reach **identical** regions on those pages,
to the decimal — nothing regressed, the sample was just skewed. The pipeline scored 0.29
there and 0.54 on a seeded random sample. All future spot measurements use
`rand800.txt` (seed 20260806).

## Where it stands

| class | truth | prec | recall | F1 |
|---|---|---|---|---|
| Page-footer | 692 | 0.744 | 0.720 | **0.732** |
| Section-header | 1,919 | 0.666 | 0.655 | **0.660** |
| Text | 5,688 | 0.704 | 0.457 | 0.554 |
| Table | 314 | 0.595 | 0.487 | 0.536 |
| List-item | 1,679 | 0.579 | 0.478 | 0.523 |
| Picture | 354 | 0.413 | 0.492 | 0.449 |
| Caption | 254 | 0.473 | 0.417 | 0.444 |
| Formula | 225 | 0.506 | 0.396 | 0.444 |
| Page-header | 782 | 0.588 | 0.244 | 0.345 |
| **weighted** | 11,994 | | | **0.5525** |

11.7 regions kept per page. Precision is respectable nearly everywhere; recall is the
gap, and the diagnosis splits it into named pieces per class (proposer reach, Background
veto, extent outranking).

## What goes into the one retrain

Three things are queued so the models retrain once, not three times:

1. **lambda_l2.** The classifier's raw scores reach ±1.6 million; healthy boosted scores
   are single digits. LightGBM's default L2 is zero and the unweighted rare classes
   saturate. This is also why so many probabilities are exactly 1.0, which wastes the
   ranking granularity NMS depends on.
2. **Page-scope gutter features.** 291 ms/page against a 12–14 ms budget, all in
   `ColumnGapCandidates` × 375 candidates. Computing gutters once per page and asking
   per-candidate "does it stay clear here" is cheaper and arguably the better feature.
3. **The IoU head's Table errors.** Where tables are outranked by same-class wrong
   extents, the IoU head prefers the wrong one 80% of the time — its errors concentrate
   exactly where they matter. Candidate-set rebalancing (more table near-misses) goes in
   with the retrain.

## Status

`LearnedProposals` remains off; nothing routes through the region stage. `go vet ./...`
and the full suite are clean.
