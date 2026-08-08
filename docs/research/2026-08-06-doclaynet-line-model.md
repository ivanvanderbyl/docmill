# Scaling the layout classifier to DocLayNet: the 12-class LINE model

**Date:** 2026-08-06
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Tasks 1–3
**Follows:** `2026-08-06-formula-classifier-spike.md` (Task 0)
**Verdict:** The spike generalises. On 256,338 held-out lines the model reaches
**0.776 accuracy / 0.744 macro-F1** across 12 classes, and `Formula` — the class Task 0
was built to test — improves from the spike's 0.762 F1 to **0.803**.

More importantly, the model stopped leaning on content. In the spike `math_frac` was the
single largest feature by gain; here it does not appear in the top twelve at all.
Geometry carries the whole thing, which is what `AGENTS.md` asks for.

---

## What changed, and why it is not just "more data"

**The teacher is gone.** Task 0's binding constraint was HURIDOCS' own recall: it emitted
zero `Formula` regions for a paper full of display equations, and hand-adjudication found
~42% of the model's apparent false positives on `entropy.pdf` were equations the teacher
had missed. DocLayNet's labels are drawn by trained human annotators, so that ceiling is
simply removed. The plan's "teacher errors become student errors" risk no longer applies
to the training corpus.

**Licence checked, as the plan requires:** DocLayNet is CDLA-Permissive-1.0 — permissive,
and explicit that outputs and derived models may be used freely.

**We take the labels, not the features.** DocLayNet-v1.1 ships a `pdf_cells` column and
it is tempting to train straight off it. That would have been a trap:

- `font.size` is `1.0` for essentially every cell — 29,135 of 29,180 sampled. It records
  a text-matrix scale, not a point size. `font_size_ratio` would have been noise.
- There are no bold/italic flags, only a font *name*.
- Cells are far more fragmented than docmill's (median 2 words per cell against docmill's
  whole-line rects), so `cell_count`, `baseline_count` and `baseline_dispersion` would
  have meant something different at training time than at inference.

That is exactly the training/serving skew the plan warns about, arriving through the
front door. So the pipeline instead takes the single-page PDFs from
`DocLayNet_extra.zip` — 81,471 of them, keyed by the same `page_hash` as the annotations
— and lets **docmill do its own extraction**. Every feature is computed by the code that
will compute it at inference.

## Corpus

| | |
|---|---|
| Pages annotated | 80,863 |
| Single-page PDFs extracted | 81,471 |
| Pages docmill failed to parse | **0** |
| Assembled lines emitted | 3,099,657 |
| Lines joined to an annotation | 3,077,348 |
| Source documents | 2,570 |

Splits are DocLayNet's own — built to keep a document's pages together and to balance the
class mix — so `train`/`val`/`test` are used as published rather than re-derived. `test`
(184,073 lines) has not been touched.

**Coordinate reconciliation.** DocLayNet boxes are `[x, y, w, h]` in a 1025×1025 COCO
image space, y down. Pages are *not* letterboxed into that square: a 612×792 page and a
566×708 page both fill it, so the aspect ratio is not preserved and the two axes need
**separate** scale factors. Using one factor for both — the obvious mistake — displaces
boxes by several points vertically, a large fraction of a line height, which would have
quietly poisoned the labels.

## Result: DocLayNet val split, 256,338 lines

| class | support | precision | recall | F1 |
|---|---|---|---|---|
| Page-footer | 4,992 | 0.947 | 0.992 | **0.969** |
| Text | 133,321 | 0.904 | 0.764 | 0.828 |
| Formula | 3,402 | 0.747 | 0.867 | **0.803** |
| Section-header | 13,075 | 0.744 | 0.848 | 0.793 |
| Page-header | 2,709 | 0.657 | 0.889 | 0.756 |
| Table | 34,776 | 0.759 | 0.762 | 0.760 |
| Picture | 9,681 | 0.734 | 0.761 | 0.747 |
| Background | 30,950 | 0.632 | 0.833 | 0.719 |
| Footnote | 403 | 0.556 | 0.866 | 0.677 |
| Caption | 2,050 | 0.568 | 0.741 | 0.643 |
| Title | 328 | 0.559 | 0.747 | 0.640 |
| List-item | 20,651 | 0.523 | 0.672 | 0.588 |
| **overall** | | | | **acc 0.7758 / macro-F1 0.7436** |

`Background` is a real class, not a discard bucket: it is what an assembled line gets
when no annotated region covers it, and at inference docmill sees those lines and must
route them somewhere.

**Feature gain, in order:** `height_ratio`, `gap_above_ratio`, `gap_below_ratio`,
`center_offset_frac`, `y_center_frac`, `left_frac`, `right_gap_frac`, `char_count`,
`mean_char_width`, `width_frac`, `font_size_ratio`, `digit_frac`.

Pure layout geometry takes the top seven places. In the spike, `math_frac` had roughly
three times the gain of the next feature; at this scale it has fallen out of the top
twelve. The English-bias caveat the plan raised against lexical features is much reduced
— though it should be re-tested by ablation before any of this ships.

## What this says about the plan

**`List-item` at 0.588 is the weakest class and is a known modelling gap, not noise.**
List items are distinguished from body text largely by a leading marker and a hanging
indent. The spike feature set has neither a numbering-depth feature nor a
content-left-offset feature — both are in the Task 2 candidate list and neither was
implemented here. This is the clearest evidence for building the full Task 2 vector
rather than the spike's twenty.

**`Table` at 0.760 and `Picture` at 0.747 are line-level scores and should not be read as
region-level ones.** These are exactly the two classes the plan hands to the REGION model,
for exactly this reason: whether a run of lines is a table is not a property of any line
in it. The line model's job is to feed that second stage, and 0.76 line labels are a
usable input to it.

**Model size lands where the scaling note predicted.** 3,600 trees, 108,000 nodes — about
10.7 MB as generated Go, against the 9.0 MB projected from the 90,823-node estimate. That
is over the threshold where the earlier measurement says to switch from Go literals to a
`go:embed`ed blob: 6.9 s versus 0.35 s to rebuild after each retrain.

## Honest gaps

- **No `entropy.pdf` cross-check.** This branch is rebased onto `main`, whose line
  assembler fragments display equations differently (2,551 lines against 2,335 on
  `ivan/layout-classifier-plan`). Equation lines there carry embedded newlines and do not
  match the earlier heading list, so the Task 0 comparison cannot be reproduced from this
  base. The partial match that did resolve was 3 of 6 equation-headings labelled
  `Formula` with **zero** false positives among the other 59 headings — indicative only.
  Re-run this on a branch carrying the line-assembly work before drawing conclusions.
- **`test` is untouched and unreported** — deliberately, so it stays a genuine final exam.
- **No REGION model yet.** That is Task 3 step 3 and needs this model embedded first.
- **No ablation at this scale.** The spike's lexical ablation cost 2.4 F1 points; it
  should be repeated here, where the lexical features matter much less.
- **No DPBench run.** Nothing in this note measures end-to-end extraction quality.
- **`num_threads` is pinned to 4, not 1.** LightGBM is deterministic for a *fixed* thread
  count, and 1 thread over 2.6M rows is hours for no benefit. Changing the value changes
  the model, so it is recorded with it.

## Reproducing

```bash
# 1. Annotations (~6 min): column-selective parquet reads over HF, which skips the
#    image column and so costs a few MB per shard instead of 1.1 GB.
python fetch_annotations.py annotations.jsonl

# 2. PDFs (8 GB). Use several connections: a single one is throttled to ~2 MB/s,
#    six reach ~63 MB/s. NOT into /tmp, which is a 7.9 GB RAM-backed tmpfs here.
curl -r <range> .../DocLayNet_extra.zip   # x6, then concatenate

# 3. docmill extracts its own features over all 81k pages (~9 min, 4 workers)
spike emit -list pdflist.txt -jobs 4 > lines.jsonl

# 4. Join labels, train
python join_doclaynet.py --lines lines.jsonl --annotations annotations.jsonl --out dataset.jsonl
python train_doclaynet.py --dataset dataset.jsonl --model-out doclaynet_line.txt
```

The corpus and the 10.8 MB model artefact are not committed. Working data lives outside
the repo at `/home/orca/doclaynet-work/`.
