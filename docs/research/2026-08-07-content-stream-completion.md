# Completing the content-stream interpreter: images, shadings, fills, clipping

**Date:** 2026-08-07
**Follows:** `2026-08-07-proposal-ceiling.md`
**Verdict:** The interpreter now reports every kind of ink the page lays down, each
clipped to what is actually visible. **All 200 DPBench documents produce byte-identical
Markdown**, so this is purely additive.

---

## Why

The ceiling measurement found that 39.8% of DocLayNet's annotated Picture regions contain
no assembled text line at all. `pkg/page` offered `TextCell`, `FormField` and
`RulingSegment`, so there was no signal in the page model that could ever find them.

The cause turned out to be one deliberate line in the interpreter:

```go
// Image (and other) subtypes are out of scope for text extraction.
```

The interpreter is a port of PDFium's `core/fpdfapi/page` layer, and it already walks
every drawing operation on the page and computes a bounding box for each. It just threw
most of them away, because when it was written only text was wanted.

## What was added

Everything below was checked against PDFium at the pinned revision `0db284a42` rather
than written from the spec, because the port's whole value is that it agrees with the
renderer.

**Images** — `Do` on an `/Image` XObject, and inline `BI`/`ID`/`EI`. A PDF image always
occupies the unit square mapped by a matrix, so the rendered box needs no image data at
all. `AddImageObject` uses `CTM * mt_content_to_user_`; so does this.

**Shadings** — `sh`. A shading has no geometry of its own; it floods the current clip, or
the whole page box when nothing clips it. That is exactly `Handle_ShadeFill`'s
`clip_path().HasRef() ? GetClipBox() : bbox_`. PDFium additionally intersects a *mesh*
shading with its coordinate data, which is skipped here since the stream is never loaded —
recorded as an over-estimate, in the safe direction.

**Filled paths** — `f`, `F`, `f*` produced nothing before. `AddPathObject` emits when the
path is stroked **or** filled; only `n` paints nothing. This lost every rule drawn as a
thin filled rectangle and every solid shape in a chart.

**Rectangles as path starts** — the port only entered its path run on `m`, so a path
written as `re` alone was invisible. This mattered little while only strokes were tracked,
because such paths are rarely stroked. It matters entirely for clipping: `x y w h re W n`
is the commonest clip in real PDFs and contains no `m`.

**Clipping** — `W`/`W*` arm the clip, and it merges when the paint operator arrives.
Two orderings from `AddPathObject` are load-bearing and easy to get backwards:

- the object is emitted **before** its clip merges, so a path is never clipped by the clip
  it establishes;
- a degenerate path clips to an EMPTY rect rather than leaving the previous clip alone —
  hiding everything after it, which is very different from doing nothing.

Form XObjects get their `/BBox` as an initial clip, which the C++ does in the content
parser constructor. And the enclosing clip is deliberately **not** pushed into a form's
own states: `AddForm` copies the general, graph, color and text states one at a time and
pointedly omits the clip. The enclosing clip rides on the `FormObject` instead, and
`DrawnObjects` intersects it down into the children on the way in.

### The one deliberate approximation

`ClipPath` keeps the intersection of bounding boxes, not the paths. This is not a
shortcut invented here — it is what PDFium's own `CPDF_ClipPath::GetClipBox()` computes.

It has a direction, and it is the safe one: a clip region is always a subset of its
bounding box, so the visible area we report is always a superset of the real one. We may
keep a little ink that a non-rectangular clip hides; we can never hide ink that is drawn.
For layout analysis, wrongly keeping content is recoverable and wrongly dropping it is not.
Text-object clips (`CPDF_ClipPath::AppendTexts`) remain unported.

## Seeing it

```
docmill render -out /tmp/boxes -pages 3 -scale 2 paper.pdf
docmill render -json paper.pdf          # one JSON record per page
```

One PNG per page outlining every object: text blue, paths grey, **images red**, shadings
orange, form XObjects dashed green. Deliberately naive — outlines only, no fills, no image
data. Anything more would be a renderer, and a renderer that disagreed with PDFium would
raise the question of which one was wrong.

On `1508.06576` page 3 it draws the figure at `(77.7, 108.9)-(531.2, 444.7)` in red, with
the caption's text boxes starting immediately below it. That figure was previously
invisible to every stage of the pipeline.

## Neutrality

Text extraction is untouched by design: the clip is *recorded* on each object and applied
only by `DrawnObjects`, never folded into the geometry existing callers read.
`RulingSegments` filters to stroked paths, so the newly-emitted filled paths cannot invent
a table grid around every solid block.

- **200/200 DPBench documents byte-identical**, against a binary built from a clean
  worktree of `HEAD` (the first attempt stashed nothing and produced a byte-identical
  baseline — worth checking the binaries actually differ before trusting a neutrality run).
- `go vet ./...` and the full suite clean, with new tests covering fills, `re`-started
  paths, clip narrowing, clip restore at `Q`, and inline images.

## The ceiling, re-measured

`spike drawn` dumped 2,806,188 objects from all 6,489 DocLayNet val pages in 24 seconds,
zero failures. `ceiling_ink.py` then asked the same question as before with ink added.

| class | regions | single object | form | ink cluster | **ink** | text run | **COMBINED** |
|---|---|---|---|---|---|---|---|
| **Picture** | 2,775 | **58.3%** | 5.9% | 47.0% | **63.1%** | 22.1% | **69.6%** |
| **Formula** | 1,894 | 1.0% | 0.0% | 18.5% | 19.4% | 62.0% | **69.0%** |
| **Table** | 2,269 | 9.0% | 0.2% | 27.7% | 29.4% | 85.8% | **87.7%** |
| Text | 49,186 | 0.5% | 0.0% | 0.8% | 1.0% | 74.8% | 75.0% |
| Section-header | 15,744 | 0.4% | 0.1% | 1.3% | 1.7% | 76.0% | 76.3% |
| List-item | 13,320 | 0.0% | 0.0% | 0.2% | 0.2% | 75.6% | 75.6% |
| Page-header | 6,683 | 0.2% | 1.5% | 0.1% | 1.8% | 26.5% | 26.8% |
| Caption | 1,763 | 0.3% | 0.0% | 1.0% | 1.2% | 58.1% | 58.7% |

**Picture goes from 22.1% to 69.6%.** The blind spot is closed.

Three things worth keeping from this table:

**More than half of all pictures are a single object.** 58.3% of picture regions are
matched by one image XObject at IoU ≥ 0.5. No clustering, no grouping, no model — the box
is read straight off the content stream. Clustering adds the rest.

**The two sources are complementary, not competing.** Ink contributes essentially nothing
to the text classes (≤ 1.8% everywhere except Picture, Table, Formula) and text
contributes 6.5 points on top of ink for Picture. Neither number alone describes what the
cascade can reach, which is why the proposer needs both and not a choice between them.

**Formula gained 7 points** from ink, which was not the goal. Radicals, fraction bars and
matrix delimiters are drawn, not typed, so the ink cluster catches formulas that the text
run splits.

Page-header stays at 26.8%, unmoved. That is the third finding of the previous note — one
assembled line spanning what the annotator marked as two — and no amount of ink fixes it.

## Next

1. Cluster nearby ink into picture candidates in Go. 47.0% of pictures need it, and the
   measurement shows the rasterise-then-connected-components approach works; a pairwise
   union-find does NOT finish, because a chart is thousands of path operations in a small
   area, which is both the quadratic worst case and the input that matters.
2. The geometric grouper, replacing `GroupLineRegions`, taking both sources.
3. Splitting assembled lines at persistent vertical whitespace, for Page-header and
   Caption.
