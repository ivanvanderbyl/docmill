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

## Next

1. Re-run the ceiling measurement with images included — does the 22.1% Picture ceiling
   move, and by how much? Nothing should be built on this until that number exists.
2. Cluster nearby ink into picture candidates. A chart is thousands of tiny path
   operations, not one object.
3. The geometric grouper, replacing `GroupLineRegions`.
