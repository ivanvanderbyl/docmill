# Learnings: the learned layout classifier

A living record of everything this project paid to find out, so nobody pays twice.
Each entry is a rule, the incident that produced it, and where the full story lives.
The dated notes in `docs/research/` are the evidence; this is the index you read
before making the same class of decision.

Convention: **the rule in bold**, then what happened, with the numbers.

---

## 1. Measurement discipline

**A "no change" result is meaningless until you prove the code executed.**
Twice, a benchmark reported "nothing changed" and the truthful reading was "the new
code never ran": the Formula veto (added `SetFormulaVetoSink` to prove execution) and
the learned columns (67/67 derivations took the heuristic branch — the "anchor" branch
being replaced ran first). Instrument the decision point, count both branches, then
believe the delta. (`2026-08-06-formula-migration.md`, `2026-08-07-learned-columns.md`)

**Headline metrics absorb catastrophes; per-case distributions do not.**
DPBench's headline TEDS was unchanged while one document went from a perfect table to
nothing (1.00 → 0.00) — the headline averages over documents-with-tables and the broken
one fell outside the set. Every fix in the five-round DPBench campaign was found in
`case_results`; the headline alone suggested "tune the classifier", which would have
been wrong five times out of five. (`2026-08-07-learned-columns.md`,
`2026-08-08-region-markdown-dpbench.md`)

**Never sample the head of a sorted file.**
All early spot measurements used the FIRST 800 pages of DocLayNet val, which is not
shuffled. On those pages Text is reachable for 33.7% of regions against 69.6%
corpus-wide; the same pipeline scored 0.29 there and 0.54 on a seeded random sample.
Days of pessimism from one lazy `head -800`. Use `random.seed(...)`+shuffle, record the
seed, keep the sample file. (`2026-08-07-region-decision-rule.md`)

**Metrics wear costumes: decompose before fixing.**
The worst "reading order" regression (NID 1.00 → 0.02) had zero lines out of order — it
was a table of contents rendered as dot-leader prose. "Characters dropped a lot" was
91% char retention but 98.6% WORD retention: table markup, not content. The worst
"table structure" case had the table FOUND and unrendered. Name the failure by reading
the document, not by trusting the metric's name. (`2026-08-08-region-markdown-dpbench.md`)

**A/B on identical inputs, same denominator.**
The old and new proposers scored "69.6%" and "33.7%" until compared on the same
documents — where they were IDENTICAL to the decimal. The difference was the sample,
not the code. When two numbers disagree, first make them measure the same thing.

**Compare binaries before trusting a neutrality run.**
A `git stash` that silently didn't take produced a "baseline" byte-identical to the
candidate — because they were the same binary. Build the baseline from a clean worktree
of the pinned commit and check the binaries differ before believing 200/200 identical.

**Word-level and char-level retention answer different questions.**
Char counts punish structure changes (a table rendered as prose loses its pipes and
padding); word counts measure content loss. Report both, diagnose with words.

## 2. Training models

**Class weights and ranking-based selection are incompatible.**
`sqrt(background/count)` weighting (Table 4.5×, Title 45×) is standard for balanced
classification and destroyed non-max suppression, which ranks by probability: the
classifier called 44 candidates per page "Table" against 0.73 real tables. Removing the
weights roughly doubled precision on every structural class. Cost asymmetry belongs in
training only when nothing downstream compares scores across classes.
(`2026-08-07-region-classifier-nms.md`)

**With rare classes, set lambda_l2 explicitly.**
LightGBM's default L2 of zero let rare-class leaves explode until raw scores reached
±1.6 million and softmax outputs collapsed to exact 0s and 1s — no ranking granularity
left for NMS, and the saturation was also what detonated a latent softmax bug into
Inf/NaN. `lambda_l2=10` restored sane scores. (`2026-08-07-combined-retrain.md`)

**Class probability cannot rank extents; train an IoU head.**
A table one line short and the correct table have nearly identical content features, so
a classifier scores them nearly the same and suppression picks between them at random —
measured as recall halving on every structural class. A second regressor predicting
"how well does this box fit" over the SAME features, with rank = P(class) × predicted
IoU, is the standard fix and needed no new data: the IoU was already in the join.
Its top features came out `gap_above`/`gap_below` — whitespace is how humans find block
edges too. (`2026-08-07-region-classifier-nms.md`)

**Choose which negatives to keep; never subsample uniformly.**
Background was 87.6% of candidates and didn't fit in memory. Near-misses (IoU 0.25–0.5
against a real region) are the negatives ranking must get right; candidates overlapping
nothing are free wins. Keep near-misses in full, thin only the easy negatives — and note
that "near miss ≥ 0.1" selected almost everything, because on a dense page nearly every
box overlaps something. 0.25 is where "near" starts meaning something.

**A model trained only on positives answers the wrong question confidently.**
The FinTabNet column model, trained only on regions that ARE tables, turned a display
equation into a four-column table — it had never been asked "is this a table at all".
Train the gate before the specialist, or the specialist hallucinates structure.
(`2026-08-07-learned-columns.md`)

**Transfer is a property you measure, not assume.**
That same column model: 0.876 F1 on FinTabNet val, net regression on DPBench. Financial
tables alone don't cover mixed corpora. Excellent-and-useless is a real state.

**A teacher's ceiling becomes the student's ceiling.**
The Task 0 spike distilled the HURIDOCS model and inherited its errors; switching to
human labels (DocLayNet) was the single largest quality unlock of the early project.
(`2026-08-06-formula-classifier-spike.md`, `2026-08-06-doclaynet-line-model.md`)

**Argmax over softmax beats tuned thresholds — until something downstream compares
scores.** Per-line routing never needed thresholds (monotonic softmax, argmax = plurality
vote). The moment NMS compared candidates, calibration became the product. Know which
regime each decision lives in.

**Features and rules are different tools.**
`hasCaptionMarkerShape` ("any word then a number") is a good FEATURE — the model weighs
it. Reused as a hard renderer RULE it deleted "Activity 1:" headings wholesale. Rules
need named, specific triggers (an explicit caption-word list); features can stay fuzzy.
(`2026-08-08-region-markdown-dpbench.md`)

## 3. The training/serving boundary

**Define every feature exactly once, in the shipping code, and read the contract out of
the binary.** The Python trainers assert against `spike features` /
`spike proposal-features` output. Restating a feature in Python is how skew is born.

**A count-based contract check cannot catch a semantic shift.**
The gutter features changed meaning (per-candidate doctable rows → page-scope index
rows) while the count stayed 55. The old blobs read the new features without any error
and scored garbage — the "drop dropped a lot" report traced partly to exactly this.
Blobs and extractor move in the SAME commit, always; if semantics shift, retrain before
anything user-visible runs.

**Emit training data with the shipping binary over the shipping assembly.**
Class-agnostic assembly exists so the classifier sees the same lines at training and
inference. When an upstream merge changed the assembler (2,551 vs 2,335 lines on the
fixture), the model was invalid and had to be retrained — detected only because line
counts were compared. Re-emit after ANY assembler change.

**Measure the shipping proposer, not a Python re-implementation.**
`spike propose` exists because every ceiling number before it came from Python stand-ins.
The offline NMS simulator earned trust by matching the Go path to four decimal places on
identical input — that equivalence is what made offline threshold sweeps valid.

## 4. Infrastructure on this host

**`/tmp` is a 7.9 GB tmpfs. It is RAM. It fills, and it purges.**
Paid twice: multi-GB training JSONL filled it mid-download (day 1), and later ~7 GB of
intermediate files starved the trainer while "free" memory looked fine. Big files live
in `/home/orca/doclaynet-work/`. Go caches too — a tmpfs module cache half-vanished and
made the project unbuildable (the sandbox blocks proxy.golang.org, so a missing zip is
fatal, not slow). `goenv.sh` now pins disk caches and a file:// GOPROXY fallback to the
host's intact download cache.

**Never pipe `go build` into anything.**
`go build | head` masks the exit status; a stale binary then emitted an entire diagnosis
dataset WITHOUT the field under diagnosis, and the diagnosis "IoU head is never wrong"
was an artefact of comparing zero with zero. Build bare, or `&&`-chain off the build.

**The sandbox hides other processes: `ps` says nothing.**
"No process found" looked like "trainer died", so it was relaunched — three trainers
ended up fighting over 15 GB and OOM-killing each other. Check `/proc/*/cmdline`
directly (with sandbox disabled) or track PIDs/task IDs; never conclude death from an
empty `ps`.

**`pkill -f <pattern>` can match your own shell.** It did, on day 1
(`pkill -f "DocLayNet_extra.zip"` killed the script driving it). Kill explicit PIDs.

**LightGBM emits trees with no splits for starved classes, and the packed format must
encode "root IS a leaf".** The packer pointed such a tree's root one past the node
array; inference read off the end. Latent from day one, triggered only when
deregularised training produced constant trees. Both packer and runtime now handle it,
with hand-built tiny-model tests. (`fix(gbm)` commits)

**The softmax shifted-max must be copied out before the loop overwrites the slice.**
`scores[i] = exp(scores[i] - scores[best])` read `scores[best]` AFTER writing slot
`best` — every class after the winner subtracted 1 instead of the max. Quietly wrong
for hours (all rankings suspect), loudly Inf/NaN only with huge raw scores. The loud
failure was the lucky one.

**Long-running work: one chain, sequential, on-disk logs, `-u` for Python.**
Parallel emit workers OOM'd at 8 jobs (use 3–4); two trainers must never coexist;
buffered Python output made progress invisible until `-u`.

## 5. PDF and geometry facts

**Every dataset has its own coordinate convention; reconcile before joining.**
HURIDOCS: top-left, integer page dims. DocLayNet: COCO 1025×1025 with SEPARATE x/y
scale factors (stretched, not letterboxed). FinTabNet: PDF points, bottom-left origin,
y-flip required. A wrong assumption here looks like a bad model everywhere else.

**Alignment checks must match the GRANULARITY of the annotation.**
FinTabNet "53% aligned" was a bug in the check (line-level cells vs per-cell boxes);
word-level cells read 86.4%. Geometry right, granularity wrong.

**The text layer is a property of the FILE, not the document.**
The same Shannon page has its drop-cap "T" as an 18.6pt text glyph in the original PDF
and as an 18×18 IMAGE in a re-exported 3-page excerpt — no extractor can read the
excerpt's T without OCR. When two tools disagree impossibly, diff the files' object
layers (`docmill render -json`) before blaming either tool.

**PDFium truths that bit us:** the clip is merged AFTER a path object is emitted (a
path is never clipped by the clip it establishes); a degenerate clip is an EMPTY clip,
not no clip; form XObjects do NOT inherit the enclosing clip (it rides on the
FormObject; intersect on the walk down) but DO take their /BBox as an initial clip;
`x y w h re W n` opens a path run with no `m`, and a port keyed only on `m` misses the
commonest clip idiom in real PDFs; filled paths are ink (`f`), and only `n` paints
nothing — but ruling extraction must filter to STROKED paths or every solid block grows
a fake table frame.

**A chart is thousands of tiny paths: cluster ink by rasterising, never pairwise.**
Union-find over box proximity did not finish (10-minute kill) — dense path clusters are
simultaneously the quadratic worst case and the exact input that matters. Paint into a
2pt grid and flood-fill: linear, and it expresses "within gap of each other" directly.
Same trick again for the page-scope gutter index (291 → 16 ms/page).

**Per-candidate work is multiplied by ~400.** 722 µs of gutter detection per call looked
harmless and was 271 ms/page. Benchmark the PAGE, not the call.

**Text and ink are complementary proposal sources, not competitors.**
Ink contributes ≤1.8% to text classes and 63.1% to Picture; text adds 6.5 points on top
of ink for Picture. 58.3% of pictures are a SINGLE image XObject needing no clustering
at all. Formulas gained 7 points from ink nobody aimed at them (radicals and fraction
bars are drawn, not typed). (`2026-08-07-content-stream-completion.md`)

**Table detection needs page context.** The anchored-text detector requires its
"Table 1:" anchor line, which usually lives in a NEIGHBOURING region. Detection inside
a region box starves it: the region stage FOUND tables and rendered zero rows. Detect
page-wide; let the model own ACCEPTANCE. (`2026-08-08-region-markdown-dpbench.md`)

**The line assembler merges across columns, and that caps entire classes.**
Two side-by-side captions become one line; `Chapter 3 … Page 45` becomes one line.
This capped Page-header at 26.5%, Caption at 58.1% reachability BEFORE any model ran.
Splitting needs PERSISTENCE (clear corridor across neighbouring rows) plus a
DECISIVE-width escape for isolated lines like running headers, where persistence has
nothing to corroborate against — the decisive rule alone was worth +16.6pp Page-header.

**Aligned word gaps ARE structure.** A test fixture with words at identical x every row
"failed" by reporting five gutters — correctly: that layout is a table grid. Prose
staggers. Fixtures must too.

## 6. Architecture principles that survived contact

**Over-generate candidates and rank; never emit one candidate per decision.**
The old proposer emitted one maximal same-label run — one misread line destroyed the
only candidate there was (Table reach 42.8% vs 78.4% possible; with teacher labels the
gap closed to 0.0, proving ALL of it was label noise). Offering every contiguous merge
plus ink, then ranking, is what made label noise survivable.

**When two representations each win somewhere, propose both.**
Fine + coarse split granularities (lists vs tables); split + unsplit line sets
(captions/headers vs table rows); text + ink sources. Every "pick the better one"
decision measured worse than "offer both, let the model choose" — and dedupe made
both-levels CHEAPER than fine-only.

**One stage's absence of opinion must not become a veto.**
Argmax letting Background outvote a correct runner-up discarded 22.6% of tables the
model actually recognised. Background now vetoes only through the rank product.
Same principle at the renderer: missing models degrade to yesterday's pipeline, never
to an empty page.

**Union of detectors, guards on the OUTPUT.**
Heading recall: heuristic ∪ region classifier, with caption/math guards applied to the
final block stream. Guards attached to one detector had a path around them within a
single benchmark round. And impostor guards are RENDERER decisions ("don't restructure
the document on weak evidence"), not classifications — text is kept, only its promotion
is refused: no-ink Pictures keep their text, mathy "headings" render as prose, Caption-
labelled lines are rescued from Picture boxes.

**Model owns WHERE; existing machinery owns STRUCTURE (until a learned replacement
WINS).** Page-wide table detection + model acceptance recovered TEDS 0.65 → 0.71 and is
the shape the plan predicted. The reverse (model owns structure before acceptance —
learned columns) reintroduced the exact defect the project exists to fix.

**Delete on evidence, and actually delete.**
The plan's Task 6 discipline. The branch accumulated three parallel pipelines before
the first deletion happened; the cleanup commit deleting the superseded region gate and
the losing column model is what keeps "learned" from meaning "additional".

**Reuse the tuned text machinery; never re-join text naively.**
Naive line-joining produced "Vol . 27 , pp ." and lost hyphens. One paragraph builder
(TOC-aware) serves every path. Ad-hoc text assembly is where quality quietly leaks.

## 7. Dead ends, named, so nobody re-walks them

- **Wider merge spans (8/14 → 12/20):** +25% proposals, +0.1% reach. Text is already
  near its assembled-line ceiling; span length is not the constraint.
- **Line-model-only heading decisions:** DPBench MHS 0.63 vs heuristic 0.77 (+17/−39
  per-case). The line model loses headings the heuristic finds; union or heuristic.
- **FinTabNet-only column model:** wins its own val, loses the mixed corpus, and a
  structure model without an acceptance gate invents structure. Resurrect only with
  PubTabNet-class breadth AND the region gate in front.
- **Picture-gating with the old region model:** rejected 24 of 3,543 figure lines
  (−0.002 F1) — the line path had already discarded the weak cases; the gate answered a
  question nobody was asking. The gain was always in Table acceptance.
- **`ps`-based liveness, `/tmp` for anything big, piped `go build`, head-of-file
  samples, uniform negative subsampling, class weights under NMS** — see sections above.
