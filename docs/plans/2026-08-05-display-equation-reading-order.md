# Display-Equation Reading Order Implementation Plan

**Goal:** Stop display-equation glyphs being interleaved into the prose lines
above and below them, which currently destroys the surrounding English as well
as the mathematics.

**Why this matters:** The visual audit of `entropy.pdf` found ~150 reading-order
defects, of which ~30 corrupt the prose itself. Examples, all verbatim from
current output:

- `possiblnformation surces` — should be `possible information sources`; the
  letters `e`, `i` and `o` were pulled into an inlined `C = Max(...)` display.
- `logarit` / `hm` split across a formula — should be `logarithm`.
- `chang- ing the bles to x1vay  ria  xn` — the word `variables` shredded into
  `va` / `ria` / `bles`.
- `provinhe theorem` — should be `proving the theorem`.
- `When asures the randomness when equal we change` — `measures` split into
  `me` / `asures` and reordered.

Unlike a mangled formula, which a reader can see is broken, this damage is
unrecoverable: the reader cannot reconstruct the missing letters.

**Scope boundary:** This plan is about ORDERING and SEGMENTATION only. Do not
attempt to reconstruct fractions or emit LaTeX — that is a separate plan.

**Tech Stack:** Go, `pkg/parser/internal/text`, `pkg/pdf`, Testify.

---

## Background: the likely mechanism

A display equation occupies a tall band containing several distinct baselines
(numerator, denominator, sub/superscripts, big-operator limits). Body text above
and below sits on single baselines. The line-assembly step groups cells into
visual lines by vertical proximity; when an equation's tall band vertically
overlaps a neighbouring prose line's band, cells from both get merged into one
"line" and then ordered left-to-right. That interleaves the equation's glyphs
with the prose's, producing exactly the observed damage.

Relevant code:
- `pkg/pdf/assemble.go` — `AssembleLineElements`, `mergeLines`.
- `pkg/pdf/cells.go` — `groupCellRows` (vertical grouping, `VerticalThresholdFactor`).
- `pkg/parser/internal/text/process.go` — `textFromSelectedIndices`, which emits
  characters in stored order for a selected region.

**Investigate before changing anything.** Confirm the mechanism with a dump of
the offending page's cells (page 22, 24, 34, 38 and 48 are the worst) before
writing code. Do not assume the description above is correct.

---

### Task 1: Reproduce and characterise

**Files:**
- Generate: a scratch dump under `/tmp`

1. Convert `entropy.pdf` and locate `possiblnformation surces` (page 22) and the
   `variables` shred (page 37).
2. Dump the `TextCell`s for those pages with their boxes, and identify precisely
   which cells were merged into one line and why.
3. Write down the actual mechanism. If it differs from the hypothesis above,
   follow the evidence, not the plan.

### Task 2: Write failing tests

**Files:**
- Test: `pkg/pdf/assemble_test.go` or a new `pkg/pdf/mathline_internal_test.go`

1. Construct a synthetic page: one prose line, a display equation below it with
   cells on three baselines (base, superscript, subscript), then another prose
   line. Use realistic geometry taken from the Task 1 dump.
2. Assert the prose lines come out intact and contiguous, and the equation's
   glyphs do not appear inside them.
3. Run and confirm failure.

### Task 3: Separate multi-baseline bands from prose lines

**Files:**
- Modify: `pkg/pdf/cells.go` and/or `pkg/pdf/assemble.go`

Design constraint from `AGENTS.md`: the signal must be geometric and
document-general. Do NOT classify by character content (looking for `=` or `∑`
is forbidden as a primary signal).

Suggested approach — **baseline clustering**:
1. Within a candidate row, cluster cells by their bottom edge with a tolerance
   proportional to the median cell height.
2. A row whose cells form ≥2 well-separated baseline clusters, where the
   clusters differ in font size or vertical offset consistently with
   sub/superscripting, is a MATH BAND, not a prose line.
3. A math band must not absorb cells from a neighbouring prose line, and a prose
   line must not absorb cells from a math band.

Guard against false positives: a prose line with a single footnote-marker
superscript must still be treated as prose. Require the evidence to be
substantial (multiple cells off-baseline, or a large vertical spread relative to
line height) before splitting.

### Task 4: Preserve within-equation order

**Files:**
- Modify: `pkg/pdf/assemble.go`

1. Once a math band is isolated, order its cells so the reading is sensible:
   left-to-right by column, and within a column, top-to-bottom (numerator before
   denominator, base before subscript).
2. Do NOT hoist limits or numerators to the start of the line — the audit found
   this happening for `Lim`, `Max` and every fraction.
3. Add a test for a fraction and for a summation with limits.

### Task 5: Verify on the real document

1. Rebuild and reconvert `entropy.pdf`.
2. Confirm ALL of these strings disappear from the output:
   `possiblnformation`, `provinhe`, `logarit`, `psitive`, `sequencef`,
   `vay  ria`, `ecessarily`.
3. Confirm the corresponding correct words are present.
4. Confirm no NEW word-splitting was introduced: compare token count and average
   token length against the previous output (currently 26,236 tokens / 4.28
   chars); a large shift in either direction indicates a regression.

### Task 6: Validate against the corpus

1. Follow the DPBench protocol in `AGENTS.md`.
2. `reading_order_nid` is the primary metric for this change and should improve.
3. Required: `errors` stays 0 and no other metric regresses.
4. Run the benchmark with nothing else running; variance under load is ±3 ms/page.
