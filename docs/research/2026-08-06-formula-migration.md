# Task 6, first class: migrating `Formula` off the heuristics

**Date:** 2026-08-06
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Tasks 4 and 6
**Follows:** `2026-08-06-reroute-neutrality.md`
**Verdict:** **Migrate.** Display-equation lines wrongly emitted as table cells fall from
**34.0% to 7.6%** — a 78% reduction — while genuine table lines lose **one line in 197**
and DPBench stays byte-identical.

---

## The model is now embedded

`pkg/pdf/layoutmodel.bin` (2.93 MB) carries the 12-class DocLayNet LINE model: 3,600
trees, 108,000 nodes, 32 features. Storage is a packed binary blob rather than generated
Go source, per the measurement in the codegen note — identical binary size and identical
prediction speed, but 0.35 s to rebuild after a retrain instead of 6.9 s, for a one-off
4.4 ms decode. Nothing parses LightGBM text at run time; `packmodel` does that at build
time.

**Go and Python agree exactly.** The fixture test replays LightGBM's own raw scores for 48
vectors stratified across all twelve classes: worst delta **0**. Raw scores rather than
probabilities, so a mismatch points at the tree walk instead of being blurred by softmax.
Four corrupt-blob cases confirm a stale or truncated artefact fails loudly.

The loader also refuses a model whose feature count differs from `LayoutFeatureNames`.
That is the guard against the project's quietest failure mode: a model trained on a
different vector that runs fine and is confidently wrong.

## The migration rule

A candidate table is rejected when the most common label among the lines inside it is
`Formula`. Its cells return to the prose path, exactly as the existing zero-validity drop
already does.

**This is a plurality vote, not a threshold.** It is argmax over the region's line-label
distribution — the region feature the plan describes ("80% of these lines scored Formula
is a region feature") — so no hand-picked constant enters. That matters: the entire
premise of the project is removing tuned cutoffs, and a migration that introduced one
would be self-defeating.

Nothing is deleted yet. The heuristic table detector still proposes every candidate; the
model only vetoes. Deleting the superseded guards is a later step, gated on this.

Features are computed with a nil document context, so `repeat_frac` is 0 — which is
exactly what the model was trained on, because DocLayNet is 81k single-page PDFs. That
consistency is not luck; it is why `DocumentLayoutContext.fraction` reports 0 below two
pages.

## Result

DocLayNet `val`, `scientific_articles` subset — 944 pages, 34,650 assembled lines, gold
human labels. "→ table" is the share of each gold class currently emitted as table cells.

| gold class | support | → table before | → table after | change |
|---|---|---|---|---|
| **Formula** | 2,860 | **973 (34.0%)** | **217 (7.6%)** | **−756** |
| Text | 16,633 | 1,937 (11.6%) | 1,777 (10.7%) | −160 |
| Background | 5,811 | 589 (10.1%) | 513 (8.8%) | −76 |
| List-item | 2,443 | 194 (7.9%) | 188 (7.7%) | −6 |
| Caption | 1,001 | 131 (13.1%) | 126 (12.6%) | −5 |
| **Table** | 574 | 197 (34.3%) | **196 (34.1%)** | **−1** |
| Picture | 2,475 | 724 (29.3%) | 724 (29.3%) | 0 |
| Section-header | 391 | 19 (4.9%) | 19 (4.9%) | 0 |

Total lines routed to a table: 4,797 → 3,790 (−21.0%).

The shape is what a correct fix looks like. The targeted class collapses; `Text`,
`Background` and `Caption` improve as a side effect because they were in the same fake
tables; `Picture` and `Section-header` are untouched; and the class that must not
regress, `Table`, loses a single line out of 197.

On whole documents the effect is visible directly — table rows before → after:
`entropy.pdf` 838 → 587, `math_0211159` 52 → 26, `1706.03762` 43 → 38.

## DPBench cannot measure this class, and that is the finding

The plan's gate is "run DPBench; the relevant metric must improve and no other metric may
regress." Half of that is satisfied trivially:

```
documents: 200   identical: 200   differing: 0   errors: 0
```

Byte-identical with the migration ENABLED. No metric regresses because no output changes.

But the relevant metric does not improve either, because **the veto never fires on
DPBench**. Not one of its 200 documents contains a candidate table whose lines are
predominantly `Formula`. DPBench is financial reports, patents, tenders and manuals; the
equation-as-fake-table defect is a scientific-paper problem, and DPBench has essentially
no scientific papers with display maths.

So DPBench is the wrong instrument for this class. It is doing its job — proving no
collateral damage across 200 diverse documents — but the evidence that the migration
*helps* has to come from a corpus where the defect exists, which is why the table above
is scored on DocLayNet's `scientific_articles` split against human labels.

**This is worth carrying into the rest of Task 6:** each class needs its metric chosen
before it is migrated. A class DPBench cannot see is not a class DPBench can approve.

## What is not done

- **The superseded heuristics are still in place.** The model vetoes; it does not yet
  decide. Deleting `tableViolatesGutterPersistence` and friends is the next step for this
  class, and it should follow a run with the heuristic guards disabled to check the model
  alone holds the line.
- **No region model.** The plurality vote over line labels is a stand-in for it. It is
  principled and it works for this class, but `Table` and `Picture` acceptance still need
  the real second stage.
- **The remaining 7.6%.** A fifth of the original defect survives — candidate tables where
  `Formula` is present but not the plurality. Those are the mixed regions the region model
  is designed for.
- **Latency unmeasured.** Prediction runs per line on the routed path; the plan budgets
  feature extraction plus inference against ~12–14 ms/page, and that has not been checked.
