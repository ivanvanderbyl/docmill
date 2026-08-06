# Learned Layout Classifier Implementation Plan

**Goal:** Replace docmill's hand-tuned layout heuristics with a single gradient-boosted
tree classifier, compiled to Go, that decides what each assembled line is.

**Target architecture (decided):** one learned cascade — a LINE model and a REGION
model — with no hand-tuned decision thresholds and no parallel heuristic fallback.
Generated Go cannot be unavailable, so the usual reason for keeping a fallback path
does not apply here. The two models are not rival classifiers needing a tie-break:
they decide different things at different granularities, in sequence, and the region
model consumes the line model's output. During migration a fixed transitional
precedence rule arbitrates between model-owned and heuristic-owned classes (defined
in Task 6); it is deleted with the last heuristic.

**Sequencing constraint:** the heuristics cannot be deleted before the model exists and
is proven per class. Removal is Task 6, gated on measured evidence, not Task 1.

**Why:** Every threshold in `pkg/pdf/headings.go`, `pkg/pdf/figures.go` and
`pkg/table/detect.go` was chosen by a person looking at one document. `AGENTS.md`
warns against exactly that. A model picks all of them at once, from data, and weighs
them jointly rather than in an arbitrary firing order. AGENTS.md explicitly permits
learned components provided "the inputs and invariants [are] document-general".

**Evidence this is feasible:** HURIDOCS `pdf-document-layout-analysis` in `fast=true`
mode reaches high accuracy on Shannon's 1948 paper using LightGBM over PDF-derived
features alone, with no vision model. On page 3 — the page docmill handles worst,
currently emitting a 5-column fake table and swallowing the page number into an
equation — it correctly separates three `Formula` regions, three `Text` regions, and
the `Page footer`. That answers the only question that could kill this project: the
signal is present in PDF features, not only in pixels.

**Tech Stack:** Go with `github.com/dmitryikh/leaves` + `go:embed` (inference),
Python + LightGBM (offline training), Docker (HURIDOCS labeller), DPBench (validation).

---

## Design decisions already made

**Where the classifier runs: classify, then route.** The current pipeline decides
figures and tables BEFORE lines exist: `figureRegions` runs on raw cells and rulings
and its cells are dropped pre-assembly (`backend.go:411`), and `DetectTables`
consumes cells directly, producing table blocks that never become lines. A line
classifier bolted on after today's assembly cannot take over those decisions — they
are already baked into what got assembled. So the pipeline is restructured:

1. Assemble ALL cells into lines, class-agnostically — no figure drops, no table
   carve-outs.
2. Classify every line with the LINE model.
3. Group runs of same-label lines into candidate regions and gate the structural
   ones with the REGION model (see the cascade decision below): accepted `Table`
   candidates hand their underlying cells to the `pkg/table` structure builder;
   accepted `Picture` candidates are excluded from prose flow as figure innards
   (captions survive as `Caption`); everything else flows as today.
4. Reading order runs after routing — as it effectively does today, since ordering
   currently runs after the figure drops.

The candidate feature list already anticipates this: baseline dispersion, cell count
and gutter alignment are exactly the features that light up when a "line" is really a
table row.

**Two granularities, one cascade: a line pass, then a region pass.** The table
accept/reject decision is region-scoped, and no line feature can express it:
gutter persistence, column-count stability and row regularity are properties of the
whole run of lines. `tableViolatesGutterPersistence` is an invariant over a candidate
table, not over any line in it, and the fake-table defect has exactly this shape —
each equation line looks tableish alone; the run lacks persistent gutters. So
classification happens twice:

1. The LINE model labels every line (all 11 classes).
2. Runs of same-label lines become candidate regions. For the structural classes —
   `Table` and `Picture` — a REGION model takes region-scoped features (gutter
   persistence score, column-count stability, row-height regularity, ruling and
   stroke coverage, math-symbol density, and the distribution of line labels inside
   the candidate) and accepts or rejects the candidate. Rejected candidates fall
   back to their line labels.

The equation-versus-table arbitration thereby becomes learned rather than ordered:
"80% of these lines scored Formula" is a region feature. Two bonuses fall out. The
teacher's labels are region-shaped — HURIDOCS emits region boxes — so the region
model's training join (candidate box to teacher box) is a cleaner, near one-to-one
match than the line-level one-to-many join. And the cascade stays threshold-free:
each stage is argmax over learned trees; nothing compares against a hand-picked
constant.

Candidate extent is deliberately simple: a maximal run of same-label lines with a
small gap tolerance. Boundary trimming (growing or shrinking a candidate by a line)
is a later refinement, only if TEDS shows boundary errors matter.

**What the model does NOT replace.** Three things survive on purpose:

- The table STRUCTURE builder in `pkg/table`. TEDS scores cell structure; a class
  label cannot build a grid. The gutter logic survives as structure building — the
  accept/reject and membership decisions that `tableViolatesGutterPersistence` and
  friends own today move to the REGION model.
- Heading LEVEL assignment (`assignDocumentHeadingLevels`). The teacher is flat on
  headings anyway (see Risks).
- Cross-page table stitching (`connectCrossPageTables`).

Line-assembly geometry, column detection and reading order also keep their
thresholds. This project removes classification thresholds, not all thresholds.

**Unit of classification: the class-agnostically assembled line.** Not the token, not
the block. Lines are what docmill already produces with full geometry and font
metrics; grouping runs of same-class lines into blocks afterwards is routing, not new
detection; and a wrong prediction costs one line rather than a whole region. The
region pass classifies GROUPS of lines, but every group is derived from line labels —
the line stays the atomic unit.

**Teacher: HURIDOCS labels, not its model.** Do NOT port their LightGBM model
directly. It expects features derived from Poppler's XML output, so porting it means
reproducing a foreign extraction pipeline bit-for-bit in Go, and a subtle mismatch
yields a model that runs fine and predicts rubbish. Take their labels; train on OUR
features.

**Corpora: DPBench is the exam, not the textbook.** The model is never trained on
DPBench documents or on `entropy.pdf`. Training uses a separate corpus labelled by
the same teacher — labels are nearly free once the Docker service is up. Target
several hundred to ~1000 PDFs sampled for diversity: arXiv papers, reports, manuals,
forms. Model selection uses held-out documents WITHIN the training corpus; DPBench
and `entropy.pdf` measure only the end-to-end effect, untouched. Training on the
benchmark would turn Task 7's "beats the baseline" into a memory test.

**Label set: the teacher's 11 DocLayNet classes, mapped to docmill routing.**

| Teacher label | docmill routing after classification |
|---|---|
| Text | paragraph line, as today |
| Section header, Title | heading line; levels still assigned by `assignDocumentHeadingLevels` |
| List item | list/structure path (`DetectStructure`) |
| Table | candidate region gated by the REGION model; accepted candidates' cells go to the `pkg/table` structure builder |
| Picture | candidate region gated by the REGION model; accepted candidates are figure innards, excluded from prose flow (replaces the `dropCellsInFigureRegions` decision) |
| Caption | prose, kept adjacent to its figure/table |
| Formula | display-equation handling; never table cells |
| Page header, Page footer | keep current behaviour initially (docmill has no page-furniture concept; the page-number footer splitting at `backend.go:886` stays). Flip to exclusion only if DPBench extraction improves. |
| Footnote | prose at end of page (current behaviour) |

The undecided rows are decisions with defaults, not blockers; each flip is measured
in Task 6 like any other migration.

**Existing heuristics become feature extractors.** `lineMostlyItalic`,
`tableViolatesGutterPersistence`, baseline dispersion, gutter scores, stroke
clustering: keep the measurement, discard the cutoff. These functions stop returning
decisions and start returning numbers. The constants (`0.4`, `1.6`,
`figureRegionMinExtent`, `headingScaleFactor`, …) disappear because nothing compares
against them any more.

**Do NOT use LightGBM `init_score` seeded from the heuristics.** It is elegant, but it
requires running the heuristics at inference time to compute the starting score, which
makes them mandatory rather than removable — the opposite of the goal.

---

### Task 0: Spike — prove the idea end to end on ONE class

Do this before anything else. It is throwaway code whose only job is to answer "does
this work at all", and it should take days rather than weeks. If it fails, the rest of
the plan is not worth building.

**Pick `Formula`.** It is the highest-value class (root cause of both the
equation-as-heading and equation-as-table defects), HURIDOCS labels it well, and
docmill currently has no dedicated detector for it, so there is a clean before/after.

1. Run HURIDOCS over ~20 documents including `entropy.pdf`. Keep only `Formula` boxes.
2. Emit docmill's assembled lines with a SMALL feature set — a dozen features, not the
   full list in Task 2. Baseline count, font size ratio, alignment, gap above/below,
   width fraction, cell count, italic fraction will do. Emit with `DetectTables`
   disabled: the worst formula cases are precisely the ones currently swallowed into
   fake tables, and those lines never exist in a dump taken from the default path.
3. Join by IoU, train a binary LightGBM (formula vs not), hold out whole documents.
   `entropy.pdf` MUST be in the held-out set — step 5 evaluates on it, and a spike
   that grades itself on its own training document proves nothing.
4. Embed via `leaves` and predict inside docmill.
5. Compare against the current heuristics on `entropy.pdf`: how many of the nine
   equation-headings and the residual fake-table regions does it catch, and how many
   genuine headings or tables does it wrongly call formulas?

**Success looks like:** the model beats the hand-tuned guards on formula regions with
few enough false positives to be worth pursuing. **Failure looks like:** it cannot
separate formulas from headings using docmill's geometry, which means the signal is not
in our features and the full plan should be abandoned or rescoped.

Report the number either way. A negative result here is a cheap and valuable outcome.

### Task 1: Measurement phase — an oracle for what we get wrong

This phase ships no Go and is worth doing even if the rest is never built. Every
quality judgement in this project so far has been docmill's own output judging itself.

**Files:**
- Create: `benchmarks/layout/` (harness, gitignored outputs)

1. Stand up HURIDOCS `pdf-document-layout-analysis` locally via Docker.
2. Run it over the DPBench corpus (200 PDFs) and over `entropy.pdf`, in both
   `fast=true` and `fast=false` modes. Keep both: the gap between them is the
   accuracy a feature-only model gives up versus a vision model, measured on our
   own corpus rather than taken from a paper. These documents are for measurement
   and final evaluation ONLY — they never enter training (see Corpora decision).
3. Assemble the separate TRAINING corpus (several hundred to ~1000 diverse PDFs)
   and run the labeller over it. Record the source and licence of every document.
4. Run docmill over the same PDFs, emitting each assembled line's box and its current
   predicted class.
5. Match docmill lines to HURIDOCS regions by IoU. Produce a per-class confusion
   matrix and error rate for the CURRENT heuristics on DPBench + `entropy.pdf`.
6. **Verify the labeller before trusting it.** Hand-check a stratified sample of at
   least 100 regions against the page images, drawn from BOTH corpora. Teacher
   errors become student errors. Report the labeller's own error rate; if it is high
   on a class, that class is not a candidate for distillation.

**Deliverable:** a table of per-class error rates for the existing heuristics. This
decides which classes are worth replacing and which are already good. Record it in
`docs/research/`.

### Task 2: Feature extraction

**Files:**
- Create: `pkg/pdf/layoutfeatures.go`
- Test: `pkg/pdf/layoutfeatures_test.go`

1. Define a fixed, ordered feature vector per assembled line. Candidates, all already
   computable: font size relative to the page's dominant size; boldness fraction;
   italic fraction; box position (x, y normalised to page); width fraction; left and
   right inset; gap above and below relative to line height; distinct baseline count;
   baseline dispersion; cell count; median vs max cell size ratio; alignment
   (left/centre/right); column index; nearby stroke density; whether an equivalent box
   repeats at the same position on other pages; page index normalised; numbering depth
   if a leading marker is present.
2. Add a small set of LEXICAL features — caption and formula detection lean on
   content, and the teacher's own fast model uses content features: leading caption
   keyword (`Figure`, `Table`, `Fig.`, `Eq.`); digit fraction; math-symbol fraction;
   punctuation density; uppercase fraction; trailing full stop. Caveat: keyword
   features are English-biased. Keep them a small minority of the vector and check
   during Task 3 that the model degrades gracefully with them ablated — AGENTS.md
   requires document-general inputs.
3. Define the REGION feature vector for the second pass (see the cascade decision):
   gutter persistence score, column-count stability, row-height regularity, ruling
   and stroke coverage, math-symbol density, extent fractions, cell-count statistics,
   and the distribution of line labels inside the candidate. Region training rows are
   emitted later, in the Task 3 loop, because they need the embedded line model.
4. Refactor the existing heuristics into feature extractors returning numbers, not
   verdicts. Do not delete the decision paths yet.
5. **Each feature vector's order and meaning is a contract** between the Python
   trainer and the Go predictor. Define each in ONE place and generate both sides
   from it, or a silent index shift will produce a model that is confidently wrong.
6. Add a debug emitter: docmill dumps `(box, features, current class)` as JSONL for a
   corpus. **The emitter MUST run the class-agnostic assembly path** — the same
   assembly the classifier will see at inference (see Task 5). Dumping features from
   today's post-carve-out assembly would train the model on lines that never occur at
   prediction time: training/serving skew, the silent cousin of the index-shift bug.

### Task 3: Train offline

**Files:**
- Create: `benchmarks/layout/train.py`

1. Join the Task 2 JSONL (training corpus only) to the Task 1 labels by IoU. Hold
   out documents (not lines) for validation, so a document's lines cannot appear in
   both splits. DPBench and `entropy.pdf` never enter either split.
2. Train the LINE model: LightGBM multiclass over the teacher label set. Use class
   weights: `Text` dominates the row count, and docmill's costs are asymmetric — a
   fake table is worse than a missed one. Encode that asymmetry in training weights,
   NOT in per-class decision thresholds at inference; inference stays pure argmax.
3. Train the REGION model second. It needs the line model's labels, so the loop is:
   train line model → embed (Task 4) → run the Go emitter to group candidates and
   dump region features over the training corpus → train region model → embed.
   Candidate boxes join to teacher region boxes by IoU — the cleaner, near
   one-to-one join. Grouping and region features are computed ONLY by the shared Go
   path; a Python reimplementation would reintroduce skew.
4. **Pin for reproducibility:** LightGBM version, random seed, `deterministic=true`,
   fixed thread count, for both models. Commit the training config next to
   `train.py`. Two runs from the same data must produce the same model, or the
   repeatability story dies at the first retrain.
5. Report per-class precision/recall for the line model, and accept/reject
   precision/recall for the region model, against the held-out documents, and compare
   directly to Task 1's heuristic error rates. **A class where the model does not beat
   the heuristic is not a class to migrate.**
6. Keep the model small and shallow enough to stay explainable: prefer a few hundred
   trees of limited depth. `AGENTS.md` requires detection to be "deterministic,
   repeatable, and explainable"; a decision path you can print satisfies that.

### Task 4: Run the model in Go

**Decision: use `github.com/dmitryikh/leaves` with `go:embed`, not codegen.** MIT
licensed, pure Go, no cgo, multiclass supported. `LGEnsembleFromReader(*bufio.Reader,
bool)` loads from memory, so embedding the artefact gives BOTH properties we want: no
runtime disk dependency, and no exporter or tree-walker to write and maintain.
(`LGEnsembleFromJSON` takes a plain `io.Reader` if the JSON dump proves easier.)

**Files:**
- Create: `pkg/pdf/layoutmodel.go`
- Create: `pkg/pdf/layoutmodel.txt` (line-model artefact, embedded;
  `pkg/pdf/layoutregion.txt` joins it once the region model is trained)
- Test: `pkg/pdf/layoutmodel_test.go`

```go
//go:embed layoutmodel.txt
var modelBytes []byte

var model = sync.OnceValue(func() *leaves.Ensemble { ... })
```

1. Embed the artefact and load it once via `sync.OnceValue`. Nothing is read from disk
   at runtime and nothing ships alongside the binary.
2. Predict per line with the line ensemble and per candidate region with the region
   ensemble; take the argmax.
3. **Pin the port with a fixture test**: feature vectors plus the scores the Python
   models produced for them, for both stages, asserted against the Go path. This
   catches index shifts and
   float drift, the realistic failure modes, and is the test proving the Python and Go
   sides agree.
4. Check what introspection `leaves` exposes for the explainability requirement. If it
   cannot report a decision path, record that as a known gap against `AGENTS.md` rather
   than quietly dropping the requirement.

**Codegen is deferred, not rejected.** Generating flat tree arrays as Go source
(feature index, threshold, children, leaf value) would remove the dependency, move
model parsing from startup to compile time, and make a decision path trivial to print.
Do it only if profiling shows `leaves` is too slow, or if the explainability gap in
step 4 turns out to matter. The artefact is identical either way, so switching later is
contained to this one file.

### Task 5: Reroute the pipeline behind a flag

The classify-then-route pipeline (see Design decisions) is built as an alternate path
in `pkg/pdf/backend.go` behind an extraction option. The default path is untouched.
This task isolates REFACTOR risk from MODEL risk: the reroute must be proven neutral
before any model decision goes live.

**Files:**
- Modify: `pkg/pdf/backend.go`

1. Add class-agnostic assembly: assemble all cells into lines with no figure drops
   and no table carve-outs. Reuse the existing line assembler; only the pre-assembly
   exclusions change.
2. Add the routing stage. In this task every routing decision is still taken by the
   EXISTING heuristics (figures, tables, headings exactly as today), restated as
   post-assembly routing instead of pre-assembly carve-outs.
3. **Gate: DPBench through the rerouted path must match the current pipeline** within
   noise. Any real delta is an assembly or routing bug — for example the
   tall-delimiter corridor logic guarded by `mathline_internal_test.go` now sees
   lines inside regions it never saw before. Fix before proceeding.
4. Wire the Task 4 predictors in shadow mode: line labels and region verdicts
   computed and logged, decisions unchanged. This produces a live confusion matrix
   on the real pipeline to cross-check Task 1's offline one.

### Task 6: Migrate per class, then delete

This is where the hand-coded classifier is removed, one class at a time, on evidence.

**Files:**
- Modify: `pkg/pdf/headings.go`, `pkg/pdf/figures.go`, `pkg/table/detect.go`,
  `pkg/pdf/backend.go`

**Transitional precedence rule (decided now, deleted with the last heuristic):**
while model-owned and heuristic-owned classes coexist, conflicting claims over the
same cells are resolved by the pipeline's existing structural order — figures claim
first, tables second, line classes last. Migration flips WHO decides a class, never
the order in which classes claim. Without this rule the "no other metric may regress"
gate below would be noise. In the end state the rule vanishes on its own: one model,
one label per line, regions derive from labels.

For each class, worst current error rate first (so Formula and Table lead):

1. Switch that class's routing decision to the model, in the rerouted path — the
   line-plus-region cascade for `Table` and `Picture`, the line model alone for
   line classes.
2. Run DPBench. The relevant metric must improve and no other metric may regress.
3. Run `entropy.pdf` and check the class visually against the page images.
4. Only then delete the superseded heuristic and its constants.
5. If the model loses on that class, KEEP the heuristic and record why in this plan.
   A heuristic that survives is telling you about a signal the model lacks — most
   likely stroke clustering from vector paths, which the line feature vector may not
   capture well.

Once every class is migrated, make the rerouted path the only path and delete the
flag, the transitional precedence rule, and the superseded heuristic decision code.

**End state:** no hand-tuned CLASSIFICATION thresholds remain in `headings.go`,
`figures.go` or `detect.go` decision paths. The rule code that survives does so as
feature extractors and structure builders only. Line assembly, column detection and
reading order keep their thresholds — out of scope for this project.

### Task 7: Validate

1. Full DPBench per the `AGENTS.md` protocol. **Rebuild `bin/docmill-bench` before
   benchmarking** — a stale binary silently reports the previous build's scores and has
   already caused a false conclusion in this project.
2. Baseline to beat: extraction 0.922288, reading_order 0.895882, teds 0.763683,
   mhs 0.770467, errors 0. Because DPBench never entered training, this comparison
   is a genuine held-out result, not a memory test.
3. Latency: measure IN-PROCESS over the corpus, not via the subprocess harness, whose
   variance is ±3 ms/page. **The budget covers feature extraction PLUS inference**,
   not tree walks alone: cross-page box repetition and nearby stroke density are the
   costly features, not the model. Combined, they should be a rounding error against
   the current ~12–14 ms/page.
4. Record every delta in the handoff.

---

## Risks

**Label alignment is where this fails, not training.** Matching docmill lines to
HURIDOCS boxes by overlap is fiddly, and mislabelled rows produce a confidently wrong
model. Budget real time for it and inspect samples by hand.

**Training/serving skew.** Features must be computed by the same code AND the same
assembly mode in the training dumps and at inference. The Task 2 emitter runs the
class-agnostic path for exactly this reason. The Task 4 fixture test pins the
predictor, but only the shared emitter pins the features. Candidate grouping and
region features carry the same rule: generated for training by the embedded line
model through the Go emitter, never reimplemented in Python.

**Class-agnostic assembly creates lines that never existed before.** Cells inside
tables and figures currently never reach the line assembler. Assembly behaviour on
them (tall-delimiter corridors, dense gutter rows) is unproven; the Task 5 neutrality
gate exists to flush this out before the model is in the loop.

**Teacher errors become student errors.** Hence the Task 1 verification step. Both
HURIDOCS paths are trained on DocLayNet, so its blind spots are inherited: `Picture`
rather than a distinct chart class, and a finance/science/patents/law/manuals domain
mix.

**Heading levels are not solved by this teacher.** DocLayNet has one flat
`Section header` class: on `entropy.pdf` page 1 the paper title, the byline and
`INTRODUCTION` are all labelled the same. `ivan/heading-levels` gets no help here.
PP-DocLayout-L does separate document title from paragraph title and would be the
better teacher for that one problem.

**Licensing must be confirmed before use.** Check the HURIDOCS model licence and
whether its outputs may be used as training labels — and the licence of every
training-corpus document, recorded at collection time (Task 1). DocLayNet itself is
permissively licensed, but confirm rather than assume.

**Granularity mismatch.** HURIDOCS' fast model classifies tokens; we classify lines.
The IoU join must handle one-to-many cleanly.
