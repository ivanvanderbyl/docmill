# The path to a learned pipeline that beats every existing option

**Date:** 2026-08-08
**Follows:** `2026-08-08-region-markdown-dpbench.md`, `docs/LEARNINGS.md`
**Standing:** after the cleanup commit there are exactly three options. On DPBench
(200 docs, four metrics — extraction / NID / TEDS / MHS):

| option | ext | NID | TEDS | MHS | ms/page | role |
|---|---|---|---|---|---|---|
| default | 0.92 | 0.90 | 0.76 | 0.77 | 49 | the old bar |
| `-learned-layout` | **0.93** | **0.90** | **0.76** | **0.77** | 67 | **the new bar** |
| `-region-markdown` | 0.92 | 0.86 | 0.71 | 0.77 | 260 | the candidate |

Measured after the cleanup commit, and the heading revert did more than predicted:
`-learned-layout` now matches or beats the default on EVERY headline metric
(per-case: NID +7/−2, MHS +1/−0, TEDS 0/0, extraction +5/−6 with the higher
headline) at +18 ms/page. **The first learned configuration with no losses
anywhere.** The earlier TEDS deficit (0.73) was the line-model heading pass
stealing cells from tables; heuristic headings returned it to parity. The region
path is byte-for-byte unmoved by the cleanup, as intended.

This changes the target: `-region-markdown` must now beat `-learned-layout`, and
the interim recommendation for users is `-learned-layout` — strictly ≥ the default
at modest cost.

**Definition of "beats all existing options":** `-region-markdown` ≥ every other option
on ALL four metrics, with per-case wins ≥ losses on each, under 100 ms/page — measured
on DPBench AND spot-verified on entropy.pdf and real user documents. Only then does it
become the default, and only then do superseded heuristics get deleted (Task 6
discipline: the flag flips first, deletion follows evidence).

The gaps to close: **NID −0.04 (21 cases), TEDS −0.05 (8 cases), speed 5×.**
Extraction and MHS are already at or above the bar.

---

## Phase 1 — close the gaps. No new data, no new model classes.

### 1.1 Reading order (−0.04): diagnose the 21 cases before touching anything

The record is unambiguous: every "obvious" cause in this project was wrong until a
failing document was read (LEARNINGS §1). The named hypotheses, each checkable in an
afternoon against the per-case JSON:

- **Geometric re-sort vs inherited order.** Region blocks are sorted by
  `sortMarkdownBlocks` over big boxes; the default orders fine-grained blocks whose
  cells already carry the proven `orderCells` sequence. Candidate fix: order region
  blocks by their minimum line `ReadingOrder` (inherit the sequence, don't re-derive).
- **Cross-column regions.** A coarse region spanning both columns forces column
  interleave. Candidate fix: split render-side at the same persistent gutters the
  proposer already computes.
- **Dropped picture innards shifting alignment** — verify against ground truth
  tokenisation before assuming.

Gate: the 21 named cases improve, zero new losses >0.05, TOC and table cases stay fixed.

### 1.2 Tables (−0.05): absence of a region must not veto a detection

Acceptance currently requires a detected table to overlap a model Table region. The
model's end-to-end Table region recall is 0.548 — so roughly half of true tables depend
on the detector AND the region agreeing, and a missed region turns a perfect detection
into TEDS 0. That is "one stage's absence of opinion becomes a veto", the exact failure
LEARNINGS §6 names.

Fix: veto requires **positive contradiction** — the detected area claimed by a
higher-rank non-Table region (Formula, Text with strong score). No region at all means
the detector stands, as it does on the default path.

Regression gate: the equation-fake-table set (the defect this project was born from,
`c759c5f` era, `formulacheck`) must stay fixed — that is what the veto is FOR. Both
directions measured per-case before merging.

### 1.3 Speed (250 → <100 ms/page): profile, then prune

Known shape: ~390 candidates/page × (16 ms features total + 4,550 trees each).
Levers in order of expected yield, each measured alone:

1. **Early Background exit.** Score a prefix of trees; drop candidates whose Background
   margin is already decisive. Most of 390 candidates are obvious nothing.
2. **Cheap pre-filter before features.** Single-line fine proposals duplicated by their
   coarse parent contribute candidates without contributing winners (check the kept-set
   provenance first — measure, don't assume).
3. Batch tree traversal (shared feature vector layout) only if 1–2 fall short.

### 1.4 One retrain, carrying everything queued

The standing skew ledger: decisive-gap 4.0 shipped while models trained on 6.0-emitted
features (LEARNINGS §3 — measured end-to-end as a win anyway, but the skew is real).
Emission is now 18× faster, so the retrain also upgrades from a 22k-page sample to the
full 74k training pages, and re-sweeps lambda_l2 around 10. Expected: low single-digit
region-F1 points, flowing into all four metrics.

## Phase 2 — beat, don't tie

### 2.1 Word-primitive proposals for the line-capped classes

The proposer's remaining reach losses are not tunable away (LEARNINGS §7: span widening
was +0.1% for +25% cost). They are the assembled line itself: Text reachability 69.6%
against a 75% line-oracle ceiling, Page-header 44.7% — because the assembler builds
lines that straddle annotation boundaries. The durable fix is proposing from finer
primitives: raster word-box clustering (the same 2pt-grid trick that carried ink
clustering and the gutter index) generating candidates BELOW line granularity for
header/caption/title shapes. This is the full inversion of the pipeline — regions
first, lines within regions — which the original plan sketched and every measurement
since has pointed back toward.

Expected: Page-header reach 45→70+%, Caption 70→80+%, with retrain. These flow
directly to MHS/extraction per-case wins.

### 2.2 Table structure, done in the order the evidence demands

The deleted column model's epitaph (grid.go, LEARNINGS §7) states the resurrection
conditions: **PubTabNet-class breadth AND the acceptance gate in front** — both now
exist or are cheap. Order: rows and columns as one model over accepted regions only,
trained on PubTabNet + FinTabNet, judged per-case TEDS with the fake-table regression
set as a hard gate. The grid heuristics are deleted only per-class, only on wins.

### 2.3 The flip

When `-region-markdown` clears the definition above: it becomes the default;
the old pipeline moves behind a `-classic` flag for one release; the routed path
(`-learned-layout`) is folded in (its remaining unique value — line-model lists,
figures, formula veto — already lives inside the region path's machinery); then the
superseded heuristic detectors are deleted class by class, each deletion carrying its
DPBench evidence in the commit message.

## What is explicitly NOT on the path

- **Learned reading order.** The heuristic wins it today; nothing in the evidence says
  a model is needed. Fix the renderer, keep the deterministic order.
- **OCR for drop caps / glyph images.** Real, rare, and a product decision — the
  geometry to crop them ships already (`render -json`).
- **Another proposer widening.** Measured dead end. The next reach comes from finer
  primitives, not longer merges.
- **Tuning any threshold the offline simulator can sweep.** The simulator matches Go to
  four decimals; sweeps happen there, Go re-measures once.
