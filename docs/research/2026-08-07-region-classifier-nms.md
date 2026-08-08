# The region classifier and non-max suppression: half fixed

**Date:** 2026-08-07
**Follows:** `2026-08-07-region-proposer.md`
**Verdict:** Two real bugs found and fixed, and **Table nearly doubled (F1 0.260 → 0.461)**.
But the flowing-text classes did not follow, and overall weighted F1 is 0.251 — far below
what the proposer's 72.2% recall allows. The stage is not ready to own any routing.

---

## What was built

The proposer is class-agnostic, so the region stage had to become a CLASSIFIER rather
than the gate it was. A gate can only reject; it can veto a wrong extent but never
replace it with the right one. A classifier assigns a class and a confidence, and
confidence comparable ACROSS candidates is what lets ~375 proposals per page compete.

- `proposalfeatures.go` — 55 features: shape, isolation, structure, the line model's
  label distribution, ink counts, provenance.
- `nms.go` — `SelectRegions`, containment checked both ways as well as IoU.
- `proposalmodel.bin` — 4,200 trees, 12 classes, 5.41 MB.
- `proposaliou.bin` — 350 trees, one output, 0.58 MB.

## The measurement that mattered

Scoring the candidate set is the wrong question for a pipeline: 375 proposals per page
reaches high recall trivially and hides what it costs. So the same 783 val pages were
scored end to end, matching greedily one-to-one so ten copies of one table cannot each
count as correct.

Running it **with and without suppression** is what located the fault:

| class | recall, no suppression | recall, with suppression |
|---|---|---|
| Table | 0.741 | 0.281 |
| Page-header | 0.696 | 0.350 |
| Page-footer | 0.672 | 0.391 |
| Picture | 0.582 | 0.264 |
| Section-header | 0.562 | 0.257 |

The right candidate was being found and classified correctly. Suppression then threw it
away, halving every class. That is not a tuning problem, it is a ranking problem.

## Two causes, both self-inflicted

**Class weights destroyed the calibration suppression depends on.** Weighting by
`sqrt(background/count)` upweights Table 4.5x and Title 45x. That is right for balanced
classification and wrong here, because suppression ranks by probability and the weights
deliberately distort probabilities. The classifier called **44 candidates per page Table**
against 0.73 real tables per page.

Removing the weights roughly doubled per-candidate precision everywhere:

| class | precision with weights | without |
|---|---|---|
| Table | 0.296 | **0.514** |
| Section-header | 0.354 | **0.557** |
| Page-footer | 0.446 | **0.690** |
| Formula | 0.264 | **0.473** |

**Class probability cannot rank extents.** A table one line short and the correct table
have nearly identical CONTENT features, so the classifier scores them nearly the same and
suppression picks between them close to randomly.

The fix is an IoU head: a second model over the same 55 features predicting how well a
candidate's extent matches what it overlaps, with suppression ranking by
`class probability x predicted overlap`. It needed no new data — the IoU was already in
the joined dataset — so it is a second pass over the same rows, not another emission.

It reaches 0.119 mean absolute error, and **its top three features are `gap_above`,
`gap_below` and `height_frac`**. Whitespace above and below is how a person decides where
a block ends; the model found that unaided, and it is exactly the signal the classifier
could not use.

## A crash the fix exposed

Removing the weights left the rare classes with too little signal to split on, so
LightGBM emitted trees with **no splits at all** — a single constant. The packer recorded
"this tree starts at node N" while the tree contributed no nodes, so N pointed one past
the end of the array and inference read off it.

The bug was always present; the first model simply contained no such tree. It crashes
rather than answering wrongly, which is the lucky failure mode. Fixed on both sides, with
two tests that build tiny models by hand so reproducing it needs no 5 MB artefact.

## Result, same 783 pages

| class | truth | old NMS F1 | **fixed F1** |
|---|---|---|---|
| **Table** | 572 | 0.260 | **0.461** |
| **Section-header** | 1,921 | 0.247 | **0.324** |
| **Picture** | 591 | 0.254 | **0.314** |
| **Page-footer** | 686 | 0.487 | **0.549** |
| Text | 5,100 | 0.145 | 0.195 |
| **Page-header** | 452 | 0.346 | **0.230** |
| **List-item** | 809 | 0.126 | **0.063** |
| weighted | | 0.2032 | **0.2508** |

Table nearly doubled. Picture, Section-header and Page-footer all improved. **List-item
halved and Page-header fell by a third.**

## What is still wrong

Text keeps 3,430 regions against 5,100 annotated, and List-item 927 against 809 with
recall 0.068. The stage is UNDER-SEGMENTING the flowing text: one big merged candidate
wins and then suppresses, by containment, every paragraph inside it.

That is a plausible decomposition of a page — it is simply not the one DocLayNet
annotates, which is per paragraph and per list item. The IoU head should rank a
three-paragraph merge below each single paragraph, since it overlaps each at about 0.33,
and for Table it evidently does. For Text it does not, and the containment rule then
removes the paragraphs outright.

So the next question is narrow and answerable: is the IoU head weak on Text, or is
containment suppression wrong WITHIN a class? Those need different fixes, and the same
with/without experiment that located the first fault will separate them.

## Status

Nothing routes through this. `LearnedProposals` is off, `go vet ./...` and the full suite
are clean.

Two things remain deliberately unaddressed until the quality question closes, because
both would force another retrain:

1. Feature extraction costs **291 ms/page** against a 12-14 ms/page budget, dominated by
   `ColumnGapCandidates` at 722 us called once per candidate.
2. The gutter features would likely be better defined at page scope anyway — find the
   gutters once, then ask only whether each stays clear inside a candidate's rows.
