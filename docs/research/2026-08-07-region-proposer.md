# The new region proposer: 42.8% → 83.3% on tables, 7.5% → 71.1% on pictures

**Date:** 2026-08-07
**Follows:** `2026-08-07-proposal-ceiling.md`, `2026-08-07-content-stream-completion.md`
**Verdict:** Every class improved, most of them by a lot. Overall region recall
**58.7% → 72.2%** against a proposer that previously reached 34.7% overall. The default
pipeline path is byte-identical over all 200 DPBench documents.

---

## What replaced what

`GroupLineRegions` emitted one candidate per MAXIMAL run of lines sharing a PREDICTED
label. Three things replace it, and each was measured separately before being kept.

**1. Ink clusters** (`pkg/pdf/inkclusters.go`) — candidates from what the page draws.

**2. A label-free grouper** (`pkg/pdf/proposals.go`) — atomic groups split on geometry
alone, then every contiguous merge of them.

**3. Column-gap line splitting** (`pkg/pdf/linesplit.go`) — cutting assembled lines that
straddle two columns.

## Result

| class | old proposer | **new** | change |
|---|---|---|---|
| **Section-header** | 54.5% | **86.1%** | +31.6 |
| **Table** | 42.8% | **83.3%** | +40.5 |
| **Page-footer** | 73.5% | **82.9%** | +9.4 |
| **Formula** | 42.5% | **78.1%** | +35.6 |
| **List-item** | 8.8% | **72.8%** | +64.0 |
| **Picture** | 7.5% | **71.1%** | +63.6 |
| **Caption** | 42.1% | **70.4%** | +28.3 |
| **Text** | 19.7% | **69.6%** | +49.9 |
| **Footnote** | 31.1% | **67.6%** | +36.5 |
| **Title** | 37.8% | **64.9%** | +27.1 |
| **Page-header** | 19.9% | **44.7%** | +24.8 |

375 proposals per page, 33.8 per region found. Several classes now exceed the ceilings in
the earlier note, which were computed over UNSPLIT lines — splitting raised the ceiling
itself.

## Three findings, each from a measurement that contradicted the design

### Neither split granularity is right

A coarse split (0.6 line heights) gives Table 81.4% and List-item 50.3%. A fine split
(0.25) gives List-item 66.8% and Table 76.9%.

The cause is structural, not a tuning accident. DocLayNet annotates each list item and
each heading as its own region, and a coarse split swallows a whole list into one group
with no sub-runs available. But a table cut into thirty fine groups needs thirty merges to
rebuild, and bounding the merge span is what keeps the enumeration affordable at all.

Picking a compromise gives up on both ends. **Running both levels and letting the model
choose recovers each**: Table 83.0%, List-item 66.8%, and it is *cheaper* than fine-only
(308 vs 326 proposals per page) because deduplication removes the overlap.

### Persistence is not enough to find a column

The splitter was built on the principle that a gap is a column when it PERSISTS — when the
lines above and below are clear at the same x. That is the right test for side-by-side
captions, and it is worth +1.4 Caption and +2.0 Title.

It cannot work for a running header. `Chapter 3 … Page 45` has body text directly beneath
it spanning the very corridor being tested, so corroboration always fails. Page-header
moved 28.0% → 28.1%, which is nothing, on the class that motivated the work.

Adding one rule — a gap of six ems or more stands without corroboration, because
justification stretches spaces but never that far — moved:

| | persistence only | + decisive gap |
|---|---|---|
| Page-header | 28.1% | **44.7%** |
| Caption | 57.0% | **70.4%** |
| Formula | 64.9% | **78.2%** |
| Section-header | 74.6% | **86.1%** |
| overall | 66.2% | **72.0%** |

### Splitting lines costs tables 5.6 points

A table row IS a set of column-separated cells. Splitting it is correct for captions and
headers and destroys exactly the full-width runs a table is built from: Table fell 81.8% →
76.2%.

So the proposer runs on BOTH line sets — split and unsplit — and offers candidates from
each. Table returns to 83.3%, the highest measured anywhere, with every splitting gain
kept. This is the same principle as the two granularities: **when two representations each
win somewhere, propose both rather than choosing.**

## Implementation notes worth keeping

**Ink clustering must rasterise.** Pairwise union-find over box proximity did not finish
on real input — killed at 10 minutes. A chart is thousands of path operations packed into
a small area, simultaneously the quadratic worst case and the exact input this exists to
handle. Painting into a 2pt grid and flood-filling is linear in page area.

**Single objects are candidates in their own right.** 58.3% of picture regions are one
image XObject. A photograph inside a framed figure clusters with its frame and caption,
and that cluster is a good candidate for the figure — but the photograph alone is the
better candidate for the picture. Proposing both costs nothing.

**Form boxes are excluded from clustering.** A form XObject's box is the union of its
children, which are reported alongside it, so clustering both merges everything the form
contains into one blob regardless of how far apart it sits.

**All proposals index one line space.** Split-derived candidates resolve their line
indices back against the primary (unsplit) set, or a consumer holding both could not use
them together.

## Status

`InkProposals` and `SplitColumnLines` are both off by default and nothing routes through
the proposer yet. `go vet ./...` and the full suite are clean, and **all 200 DPBench
documents produce byte-identical Markdown**.

## Next

1. Region features and a model over these candidates, with non-max suppression so
   overlapping proposals compete instead of each getting an isolated yes/no. The current
   region model scores candidates in isolation, which is why it could never fix extent.
2. Then table routing, and deleting the heuristic detector.
3. Page-header at 44.7% is now the weakest class by a wide margin, and worth a look on its
   own once the above lands.
