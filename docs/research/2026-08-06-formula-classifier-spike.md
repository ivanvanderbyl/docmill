# Task 0 spike: can a tree over docmill's geometry find display equations?

**Date:** 2026-08-06
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Task 0
**Verdict:** **Success, qualified.** The signal is in docmill's features. Proceed with
the plan — but Task 1's teacher-verification step is now the critical path, not an
optional nicety.

The question Task 0 was built to answer was whether formulas can be separated from
headings using docmill's own geometry, because if they cannot, "the signal is not in
our features and the full plan should be abandoned or rescoped." They can.

---

## Headline

On `entropy.pdf`, which never entered training or model selection:

| | current heuristics | spike model |
|---|---|---|
| Display equations emitted as Markdown headings | 12 of 12 (the defect) | 5 of 12 |
| Equation-headings correctly identified as Formula | 0 | **7** |
| Genuine headings wrongly called Formula | — | **0** |

The current pipeline emits 71 `#` headings for `entropy.pdf`. Twelve of them are display
equations. The model labels seven of those twelve `Formula` and does not wrongly claim a
single genuine heading — three lines that first appeared to be false positives were
checked against the rendered pages and are display equations the teacher missed
(page 16 `T = (H/C + λ)N`, page 17 `G_N ≤ H' < G_N + 1/N`, page 28
`X₁ " " " " γ = X₁+X₃+X₅+X₇`).

That is a clean win on the defect the spike was chosen to attack, and it is why the
recommendation is to proceed.

---

## Which code these numbers were measured against

**Every number below was measured on `ivan/layout-classifier-plan` (`1206f2b`), not on
this branch's base.** That branch carries the parser and line-assembly work from
`ivan/improve-scientific-paper-parsing` plus `83cb3b7` ("measure a line by its dominant
glyph size"). This branch is rebased onto `main`, which has none of it.

That matters, because the difference is in the unit of classification itself:

| `entropy.pdf` | on `main` | on `ivan/layout-classifier-plan` |
|---|---|---|
| Assembled lines the emitter produces | 2,551 | **2,335** |
| Headings docmill emits | 67 | **71** |

So the "12 of 71 headings are display equations" baseline does not exist on `main`, and
re-running the harness here will not reproduce the figures in this note. To reproduce
them, check out `ivan/layout-classifier-plan` and apply this directory on top. Re-measure
against whatever lands on `main` before treating any of this as a baseline for Task 1.

## Setup

- **Corpus:** 20 PDFs — 19 arXiv papers (spanning ML, physics, and mathematics
  typesetting) plus `entropy.pdf`. `entropy.pdf` was held out of training and of model
  selection entirely.
- **Teacher:** HURIDOCS `pdf-document-layout-analysis:v0.0.35`, `fast=true`, ~40 s/doc.
  `Formula` boxes only.
- **Lines:** 18,253, assembled class-agnostically — raw text cells straight into
  `AssembleLineElements`, no figure-region drops and no table carve-outs. This is what
  "`DetectTables` disabled" means in practice, and it matters: the equations this
  targets are precisely the lines today's pipeline swallows into fake tables, so a dump
  from the default path would not contain them.
- **Join:** containment fraction of the *line* box inside a teacher region (≥ 0.5), not
  IoU — one teacher `Formula` box routinely covers several assembled lines, so IoU
  would score every line of a multi-line equation near zero. Teacher page dimensions are
  integers (595 for a page docmill calls 595.276), so every teacher box is rescaled into
  docmill's units before any overlap is computed. 1,507 lines (8.3%) labelled `Formula`;
  64 lines fell within 0.15 of the cutoff and are reported by `join.py` rather than
  silently rounded.
- **Model:** 20 features, LightGBM binary, 300 trees, depth 6, `num_leaves` 31, positive
  class weight 3, seed 20260806, `deterministic=true`, single-threaded, `force_row_wise`.
  LightGBM 4.7.0; `github.com/dmitryikh/leaves` v0.0.0-20230708180554 for inference.

## Results

**Held-out documents** (5-fold `GroupKFold` over the 19 training documents, split by
document so no paper's lines appear on both sides; `entropy.pdf` excluded from all folds):

```
precision 0.720   recall 0.810   F1 0.762      (tp=965 fp=375 fn=227)
```

**Lexical ablation** — dropping `math_frac`, `digit_frac` and `letter_frac`:

```
precision 0.699   recall 0.781   F1 0.738
```

A 2.4-point F1 cost. Geometry carries the signal and the content features are a
refinement, which is what `AGENTS.md`'s document-general requirement asks for. Keep
checking this as the feature set grows in Task 2.

**`entropy.pdf`, scored against the teacher:**

```
2,335 lines   teacher Formula 315   model Formula 392
precision 0.495   recall 0.616   F1 0.549      (tp=194 fp=198 fn=121)
```

That looks much worse than the held-out number. Most of the gap is not model error.

## The teacher is the binding constraint, not the model

A random sample of 24 of the 198 `entropy.pdf` false positives, adjudicated against
rendered page images:

| adjudication | n |
|---|---|
| Genuine display equations the **teacher** missed (model correct) | 10 |
| Genuine model errors | 13 |
| Ambiguous (a prose continuation line that is entirely mathematical) | 1 |

So roughly 42% of the "false positives" are teacher errors (10/24; the 95% interval on
that proportion is wide, about 22–63%, and it is a 24-line sample). Correcting for it
puts real precision near **0.71** rather than 0.495 — in line with the held-out figure,
and consistent with `entropy.pdf` being a 1948 hot-metal journal page rather than modern
LaTeX.

The same under-labelling shows up corpus-wide. The teacher emitted **zero** `Formula`
regions for `1207.7214` and one each for `1602.03837`, `1706.03762` and `2010.11929` —
all papers with display equations. The worst cross-validation fold (precision 0.316) is
the one containing `1207.7214`, where every formula the model finds is a false positive
by construction.

**Consequence for the plan:** Task 1 step 6 — hand-verifying the labeller before trusting
it — is not optional and is not cheap. On this evidence the teacher's `Formula` recall is
well under 100%, which caps any student trained on it and, worse, makes offline
precision numbers read low for the wrong reason. Consider `fast=false` for the training
labels and measure the gap, which Task 1 already plans to do.

## The remaining errors are region-shaped, not line-shaped

The genuine errors cluster into two kinds, and both are already addressed by the
two-stage cascade the plan specifies. Neither should be attacked with more line features.

**False positives are mostly figure innards.** 86 of the 198 (43%) fall inside a teacher
`Picture` or `Table` region: state-diagram node labels on pages 8–9 (`A`, `B D E`,
`C B .4 .5`), graph axis ticks on page 11, and the inline plots and impulse-response
formulas inside Table I on page 40. A run of "Formula" lines inside a `Picture`
candidate is exactly what the REGION model exists to reject.

**False negatives are mostly equation fragments.** 52 of the 121 (43%) are lines under
eight characters — `i; j`, `n`, `i=1`, `B_i B_i`, `ij` — the sub- and superscript pieces
that line assembly splits off from a multi-line equation. In isolation they carry no
signal; inside a run of `Formula` lines they are unambiguous. Restricted to formula lines
of eight characters or more, recall is **0.675** rather than 0.616.

One further false-negative cause is docmill's own text extraction rather than the model:
`H = −∫···∫p(x₁,…,xₙ) log p(x₁,…,xₙ) dx₁···dxₙ` on page 35 scores 0.01 because the
integral signs arrive as the glyph `Z`, so `math_frac` reads 0.04 on a line that is
nothing but mathematics. Worth a look independently of this project.

## Go/Python agreement, and a `leaves` gotcha

`spike verify` replays 20 feature vectors and LightGBM's own scores for them through the
embedded `leaves` ensemble: **worst delta 8.5e-22** across all 20. The `go:embed` +
`leaves` path from Task 4 works as specified, with nothing read from disk at run time.

`leaves` accepts only `version=v2` or `v3` in the text-model header
(`lgensemble_io.go`: `params.Compare("version", "v3")`); LightGBM has written `v4` since
4.0. Everything `leaves` reads after that line — `num_class`,
`num_tree_per_iteration`, `max_feature_idx`, `tree_sizes`, the `Tree=` blocks — is
byte-identical between the two, so `train.py` rewrites the single header line, and the
fixture check above is what makes that an assertion rather than a hope.

**Task 4 decision to record:** keep the one-line rewrite, pin LightGBM 3.x (EOL, no
wheels past CPython 3.11), or take the codegen route the plan already holds in reserve.
The rewrite is fine for the spike; the choice should be made deliberately before the
model ships. The codegen option is now measured — see below.

## Codegen versus `leaves`, measured

The plan defers codegen: do it "only if profiling shows `leaves` is too slow, or if the
explainability gap in step 4 turns out to matter." Both were cheap to settle, so `spike
gen` now emits `layoutmodel_gen.go` — flat arrays of nodes, thresholds, child links and
leaf values — alongside the `leaves` path, and the two are benchmarked against each
other.

First, a correction to the framing: **generated Go cannot be loaded into `leaves`.**
`leaves.Ensemble` embeds an unexported `lgEnsemble` and exposes no constructor beyond
its file/reader/JSON parsers, so nothing outside the package can build one. Codegen
*replaces* `leaves`; it does not feed it.

The model makes this easy. Every split in the artefact has `decision_type=2` and every
tree `num_cat=0`, so there are no categorical splits and the decision is uniformly
`if IsNaN(v) { v = 0 }; v <= threshold`. `spike gen` asserts both and refuses to
generate otherwise, rather than emitting Go that scores differently from the trainer.
`shrinkage` is ignored on purpose — LightGBM bakes the learning rate into `leaf_value`
in the text format, and `leaves` ignores it too.

**Equivalence.** The generated path reproduces LightGBM's fixture scores, and against
`leaves` over 200,000 random vectors — including NaN in every feature position — the
worst difference is **exactly 0.0**. Not "within tolerance": bit-identical.

**Performance** (Xeon @ 2.20 GHz, 300 trees, 20 features, `-benchtime 2s -count 3`):

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| `leaves.PredictSingle` | 25,870 | 16 | 2 |
| generated, flat arrays | **17,410** | **0** | **0** |
| generated, interleaved nodes | 17,800 | 0 | 0 |
| `leaves` model load (once) | 8,000,000 | 3.28 MB | 17,745 |

Codegen is ~33% faster per prediction, allocation-free, and removes an 8 ms / 3.3 MB /
17.7k-allocation start-up parse entirely.

Interleaving the four parallel arrays into one 24-byte node record — two per cache line
— **did not pay**: 17.8 µs against 17.4 µs, a wash. So the walk is not bound by cache
lines per node but by branch misprediction on a data-dependent traversal, which no
layout change fixes. Recorded so Task 4 does not repeat the experiment.

Keep the absolute numbers in proportion: at ~45 lines per page, 17.4 µs/line is about
0.8 ms/page against the current ~12–14 ms/page, and `leaves` would be ~1.2 ms. Both are
affordable; neither is the reason to choose.

**The explainability gap closes.** Task 4 step 4 asks what introspection `leaves`
exposes and says to record a gap against `AGENTS.md` if a decision path cannot be
reported. `leaves` exposes scores only. Generated code walks the same trees and can say
why — `spike explain <pdf>` prints, per line, the trees that moved the score most with
their full split path, plus which features the vector was tested against most:

```
=== page 25 line 41: "log P = log Q +"
score=0.984062 (raw 4.122959, 300 trees)
  tree   0  leaf -1.17694  math_frac=0.2 > 0.09938 | left_frac=0.4382 > 0.1834 | ...
  tree   1  leaf +0.19832  math_frac=0.2 > 0.09938 | left_frac=0.4382 > 0.1834 | ...
most-tested features on this vector:
  left_frac              tested 186 times, value 0.4382
  y_center_frac          tested 151 times, value 0.8677
```

**Recommendation for Task 4: generate.** It is faster, allocation-free, has no start-up
cost, drops a third-party dependency, removes the `version=v4` header problem along with
the parser that caused it, and satisfies the explainability requirement instead of
booking a gap against it. The costs are a generator to maintain (~250 lines), a
bootstrap ordering wrinkle (the generated file must exist before the generator that
writes it will compile — a placeholder breaks the cycle), and 843 KB of generated Go in
place of a 979 KB embedded artefact. The equivalence tests are what make this safe to
switch; keep them.

## Top features by gain

```
math_frac 72064   left_frac 24549   char_count 18089   gap_below_ratio 5638
mean_char_width 5567   height_ratio 5285   font_size_ratio 4986
gap_above_ratio 4732   letter_frac 4313   y_center_frac 3854
```

`left_frac` ranking second is the centring signal in disguise — display equations are
indented to a consistent left offset. Both gap features earning their place confirms
that the extra leading around a display equation is real, learnable signal.

## What this does not show

- One document graded end to end. `entropy.pdf` is a hard, unrepresentative case
  (1948 typesetting, heavy mathematics); it is not a stand-in for DPBench.
- 19 training documents, all academic papers. The plan's several-hundred-document
  diverse corpus will move these numbers in both directions.
- No region model, so nothing here measures the fake-table defect end to end. What it
  shows is that the line labels the region model would consume are informative: seven of
  the ten fake-table lines the teacher calls `Formula` are recovered.
- Latency was not measured.

## Reproducing

Start from `ivan/layout-classifier-plan` — see the provenance section above; on `main`
the line assembler produces a different line set and the figures will not match.

```bash
docker run -d --name pdla -p 5060:5060 --entrypoint ./start.sh \
    huridocs/pdf-document-layout-analysis:v0.0.35
cd benchmarks/layout/spike
./label_all.sh                                    # teacher labels (committed already)
source goenv.sh && go build -o ../../../bin/spike ./cmd/spike
../../../bin/spike emit pdfs/*.pdf > out/lines.jsonl
python join.py && python train.py                 # writes cmd/spike/layoutmodel.txt
go build -o ../../../bin/spike ./cmd/spike && ../../../bin/spike verify
../../../bin/spike predict pdfs/entropy.pdf > out/entropy.pred.jsonl
python eval.py
```

The corpus itself is not committed (37 MB, mixed publisher licences); Task 1's manifest
will record source and licence per document. The teacher labels under
`benchmarks/layout/spike/labels/` are committed, so the training and evaluation steps
reproduce without re-running the 17 GB Docker image.
