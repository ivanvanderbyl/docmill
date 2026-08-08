# Learned table columns: wired, measured, and NOT enabled

**Date:** 2026-08-07
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Task 6
**Follows:** `2026-08-06-formula-migration.md`
**Verdict:** **Keep the heuristic.** The column model is excellent on FinTabNet and a net
regression on DPBench. `-learned-columns` exists and is off by default.

This is the outcome Task 6 step 5 anticipated: "If the model loses on that class, KEEP
the heuristic and record why."

---

## What was built

- `pkg/gbm` — one model runtime shared by `pkg/pdf` (line classes) and `pkg/table`
  (columns). `pkg/table` cannot import `pkg/pdf`, so without a shared package the two
  would each carry a copy of the decoder and drift.
- `pkg/table/columnmodel.bin` — 0.24 MB, 300 trees, 16 features.
- `deriveColumnBoxes` — the single place column boundaries are chosen, behind
  `DetectionOptions.LearnedColumns` and the `-learned-columns` CLI flag.

## The model itself is good

Held out on FinTabNet's own val split, 15,245 candidates over 2,976 tables:

| | |
|---|---|
| per boundary | precision 0.865, recall 0.887, F1 **0.876** |
| per table | **65.4%** recover every boundary correctly |

## On DPBench it loses

| metric | baseline | learned columns | change |
|---|---|---|---|
| extraction accuracy | 0.922190 | 0.922190 | 0 |
| reading order (NID) | 0.895746 | 0.895799 | +0.000053 |
| heading level (MHS) | 0.770467 | 0.770467 | 0 |
| **table structure (TEDS), headline** | **0.763683** | **0.763683** | **0** |
| mean TEDS over all cases | 0.857837 | 0.852837 | **−0.005000** |
| documents scoring TEDS 0 | 18 | **19** | **+1** |

**The headline TEDS is unchanged and that is misleading.** One document goes from a
perfect table to nothing — TEDS 1.0000 → 0.0000 — and the headline absorbs it without
moving, because it averages over a fixed set of documents-with-tables that this one falls
outside. Only the per-case scores show it. Anyone reading the summary row would conclude
the change was neutral.

That is worth carrying forward independently of this model: **on this harness, an
unchanged headline TEDS does not mean unchanged table quality.** Check `case_results`.

## Why it loses

The regressed document is a display equation sitting beside body text. The heuristic
produces no table. The learned columns produce a four-column one:

```
| PLL       | PLL þ PHH |      | NLL=ð NLH þ NHL Þ! 0 when W ! ∞, there is not really … |
| lim       | and lim   | (13) |                                                        |
| PLH þ PHL | PLH þ PHL |      | prediction about the more precise asymptotic behavior … |
```

This is the fake-table defect — the exact thing this project set out to remove —
reintroduced by the column model. The cause is a category error in how I wired it: the
model was trained only on regions that *are* tables, so it has never been asked "is this
one?", and it answers the question it was given (where are the boundaries) with confident
nonsense on a region that has none.

I tried the obvious fix — let the heuristic decide *whether* a region has columns and the
model only decide *where* they go. It did not help: the regression survives, so the
mechanism is further downstream, in a validity gate that the model's different column
boxes let a bad region pass. Chasing it further was not worth doing before the more basic
problem below is addressed.

## The more basic problem

The model fires on 30 of DPBench's 67 column derivations and changes only 5 documents of
200. On the other 25 it agrees with the heuristic. So on this corpus it is mostly
redundant, occasionally harmful, and never helpful.

That is a *transfer* result, not a quality result. FinTabNet is financial tables — ruled,
dense, numeric, with clean headers. DPBench is a mix where the densest-row anchor already
works. A model trained on one and measured on the other can be excellent and useless at
once, which is what happened.

## What would change the verdict

1. **A region model.** The column model needs a gate in front of it that answers "is this
   a table at all". The plan already specifies one, and this is now the second class
   asking for it — the first was `Table` line labels barely moving without it.
2. **Training data that matches the target.** DocLayNet has table *regions* but no cell
   grids; FinTabNet has grids but only financial tables. Neither alone covers DPBench.
   PubTabNet (568k tables, scientific) is the obvious complement and is already
   identified.
3. **A per-case gate in the benchmark protocol.** Given the headline hid a 1.0 → 0.0
   regression, any future table change should be judged on the per-case distribution, not
   the summary row.

## Status

`-learned-columns` is off by default. `-learned-layout` (line classes) is unaffected and
keeps its measured gains. The default path is byte-identical over all 200 DPBench
documents, and `go vet ./...` and the full test suite are clean.
