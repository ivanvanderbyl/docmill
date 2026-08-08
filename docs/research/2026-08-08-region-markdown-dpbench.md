# The region path on DPBench: from four losses to one win, two ties, two small gaps

**Date:** 2026-08-08
**Follows:** `2026-08-07-combined-retrain.md`
**Verdict:** Five benchmark rounds took `-region-markdown` from losing every structural
metric to **beating the tuned pipeline on extraction, tying it on headings, and sitting
0.04–0.05 behind on reading order and tables**. Every point recovered came from reading
per-case failures, not from tuning anything.

| metric | default | v1 | v5 | per-case (v5) |
|---|---|---|---|---|
| Extraction accuracy | 0.92 | 0.93 | **0.92** | 6 wins / 2 losses |
| Heading hierarchy (MHS) | 0.77 | 0.58 | **0.77** | 13 wins / 21 losses |
| Table structure (TEDS) | 0.76 | 0.65 | **0.71** | 7 wins / 8 losses |
| Reading order (NID) | 0.90 | 0.84 | **0.86** | 4 wins / 21 losses |
| speed | 48 ms/page | 329 | 247 | |

## What each round found

Every headline regression decomposed, on inspection, into something that was not what
the metric's name says:

**"Reading order" was a table of contents.** The worst NID case (1.00 → 0.02) had no
lines out of order: the default renders TOC pages as two-column tables via a dedicated
TOC pass, and the region path emitted dot-leader prose. Routing region prose through the
same `assembleWithToc` fixed it. NID 0.84 → 0.86.

**"Table structure" was a starved detector.** The region stage FOUND the unruled tables
(Table 0.59) and rendered zero rows, because detection ran inside the region box and the
anchored-text detector's anchor — the "Table 1:" line — lives in a NEIGHBOURING region.
Detection now runs page-wide with full context, and the model's Table regions decide
which detections are ACCEPTED. This is the division the plan named "the region model
owns table acceptance", arrived at by benchmark rather than by argument. TEDS 0.65 →
0.71, per-case now balanced.

**"Heading hierarchy" was three separate defects:**
1. One false heading on a heading-free document scores MHS 1.00 → 0.00 — the harshest
   single penalty on the board. The false headings were figure captions ("Figure 1.2.
   Per capita GDP growth in 2020") promoted by the region classifier.
2. The first caption guard reused the model's marker-SHAPE feature ("any word then a
   number") as a hard rule, and deleted "Activity 1:" headings wholesale. The shape is
   right as a feature, where the model weighs it; wrong as a rule. The rule now names
   actual caption words.
3. The region classifier finds ~70% of headings, and each miss on a one-heading document
   is another 1.00 → 0.00. Headings now come from the UNION of the heuristic detector
   (whose set is exactly the default's 0.77) and the region classifier, with the caption
   and math guards applied to the OUTPUT — attached to any single detector, the guards
   had a path around them within one round of measurement.

MHS 0.58 → 0.77, parity with the default.

## The proposer experiments (step 2), both honestly dead-ish

- **Wider merge spans (8/14 → 12/20):** +25% proposals for +0.1% reach. Text is already
  within a few points of its assembled-line ceiling; span length was never the binding
  constraint. Reverted.
- **Decisive-gap ratio 6 → 4:** overall reach 72.6% → 73.3%, Title +16.7pp, Caption
  +1.2pp, at +4% proposals. KEPT — but note the models currently serving were trained on
  6.0-emitted features, a mild train/serve skew that the measured end-to-end results
  already absorb. Fold into the next retrain.

The larger conclusion stands: proposer reach is no longer the dominant loss on DPBench
documents; renderer decisions were, and those are now largely paid down.

## What remains, named

1. **NID −0.04 (21 cases).** The one metric where losses still outnumber wins 5:1.
   Multi-column ordering of large region blocks is the suspected mass; nobody has
   proven it yet — and this file is the reminder that every previous "obvious" cause
   here turned out to be something else.
2. **TEDS −0.05.** Now balanced per-case (7 wins, 8 losses); the remaining losses are
   genuine structure differences worth reading individually before any further change.
3. **Speed: 247 ms/page vs 48.** The region stage still classifies ~390 candidates per
   page through 4,550 trees. Nothing has been done about inference cost yet; batching
   and early Background pruning are the obvious levers.
4. **Extraction wins.** Worth saying twice: the model path recovers MORE content than
   the tuned pipeline (6 wins / 2 losses per-case), while running every guard that
   keeps it from inventing structure.

## Protocol note

All five rounds ran the same corpus, same binary pair, same command. The per-case JSON
is what found every fix; the headline table alone would have suggested "tune the
classifier", which would have been wrong five times out of five.
