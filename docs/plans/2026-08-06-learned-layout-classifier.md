# Learned Layout Classifier Implementation Plan

**Goal:** Replace docmill's hand-tuned layout heuristics with a single gradient-boosted
tree classifier, compiled to Go, that decides what each assembled line is.

**Target architecture (decided):** ONE classifier. No hand-tuned decision thresholds,
no parallel heuristic fallback. Generated Go cannot be unavailable, so the usual
reason for keeping a fallback path does not apply here, and maintaining two
classifiers would require a tie-break rule that is itself a hand-tuned threshold.

**Sequencing constraint:** the heuristics cannot be deleted before the model exists and
is proven per class. Removal is Phase 5, gated on measured evidence, not Phase 1.

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

**Unit of classification: the assembled line.** Not the token, not the block.
Lines are what docmill already produces with full geometry and font metrics; grouping
runs of same-class lines into blocks afterwards is straightforward; and a wrong
prediction costs one line rather than a whole region.

**Teacher: HURIDOCS labels, not its model.** Do NOT port their LightGBM model
directly. It expects features derived from Poppler's XML output, so porting it means
reproducing a foreign extraction pipeline bit-for-bit in Go, and a subtle mismatch
yields a model that runs fine and predicts rubbish. Take their labels; train on OUR
features.

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
   width fraction, cell count, italic fraction will do.
3. Join by IoU, train a binary LightGBM (formula vs not), hold out whole documents.
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
   own corpus rather than taken from a paper.
3. Run docmill over the same PDFs, emitting each assembled line's box and its current
   predicted class.
4. Match docmill lines to HURIDOCS regions by IoU. Produce a per-class confusion
   matrix and error rate for the CURRENT heuristics.
5. **Verify the labeller before trusting it.** Hand-check a stratified sample of at
   least 100 regions against the page images. Teacher errors become student errors.
   Report the labeller's own error rate; if it is high on a class, that class is not
   a candidate for distillation.

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
2. Refactor the existing heuristics into feature extractors returning numbers, not
   verdicts. Do not delete the decision paths yet.
3. **The feature vector's order and meaning is a contract** between the Python trainer
   and the Go predictor. Define it in ONE place and generate both sides from it, or a
   silent index shift will produce a model that is confidently wrong.
4. Add a debug emitter: docmill dumps `(box, features, current class)` as JSONL for a
   corpus. Task 1's harness consumes this.

### Task 3: Train offline

**Files:**
- Create: `benchmarks/layout/train.py`

1. Join the Task 2 JSONL to the Task 1 labels by IoU. Hold out documents (not lines)
   for validation, so a document's lines cannot appear in both splits.
2. Train LightGBM multiclass over the HURIDOCS label set.
3. Report per-class precision/recall against the held-out documents, and compare
   directly to Task 1's heuristic error rates. **A class where the model does not beat
   the heuristic is not a class to migrate.**
4. Keep the model small and shallow enough to stay explainable: prefer a few hundred
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
- Create: `pkg/pdf/layoutmodel.txt` (trained artefact, embedded)
- Test: `pkg/pdf/layoutmodel_test.go`

```go
//go:embed layoutmodel.txt
var modelBytes []byte

var model = sync.OnceValue(func() *leaves.Ensemble { ... })
```

1. Embed the artefact and load it once via `sync.OnceValue`. Nothing is read from disk
   at runtime and nothing ships alongside the binary.
2. Predict per line; take the argmax class.
3. **Pin the port with a fixture test**: feature vectors plus the scores the Python
   model produced for them, asserted against the Go path. This catches index shifts and
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

### Task 5: Migrate per class, then delete

This is where the hand-coded classifier is removed, one class at a time, on evidence.

**Files:**
- Modify: `pkg/pdf/headings.go`, `pkg/pdf/figures.go`, `pkg/table/detect.go`,
  `pkg/pdf/backend.go`

For each class, in ascending order of current error rate (worst first, so Formula and
Table lead):

1. Switch that class's decision to the model.
2. Run DPBench. The relevant metric must improve and no other metric may regress.
3. Run `entropy.pdf` and check the class visually against the page images.
4. Only then delete the superseded heuristic and its constants.
5. If the model loses on that class, KEEP the heuristic and record why in this plan.
   A heuristic that survives is telling you about a signal the model lacks — most
   likely stroke clustering from vector paths, which the line feature vector may not
   capture well.

**End state:** no hand-tuned decision thresholds remain. The rule code that survives
does so as feature extractors only.

### Task 6: Validate

1. Full DPBench per the `AGENTS.md` protocol. **Rebuild `bin/docmill-bench` before
   benchmarking** — a stale binary silently reports the previous build's scores and has
   already caused a false conclusion in this project.
2. Baseline to beat: extraction 0.922288, reading_order 0.895882, teds 0.763683,
   mhs 0.770467, errors 0.
3. Latency: measure IN-PROCESS over the corpus, not via the subprocess harness, whose
   variance is ±3 ms/page. Budget: inference should be a rounding error against the
   current ~12–14 ms/page.
4. Record every delta in the handoff.

---

## Risks

**Label alignment is where this fails, not training.** Matching docmill lines to
HURIDOCS boxes by overlap is fiddly, and mislabelled rows produce a confidently wrong
model. Budget real time for it and inspect samples by hand.

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
whether its outputs may be used as training labels. DocLayNet itself is permissively
licensed, but confirm rather than assume.

**Granularity mismatch.** HURIDOCS' fast model classifies tokens; we classify lines.
The IoU join must handle one-to-many cleanly.
