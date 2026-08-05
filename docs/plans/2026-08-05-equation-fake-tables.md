# Equation Fake-Table Suppression Implementation Plan

**Goal:** Stop display mathematics and prose being gridded into Markdown tables,
and stop real table rows being emitted two or three times.

**Why this matters:** `entropy.pdf` currently emits 19 table blocks / 121 table
rows. The paper contains roughly three real tables. The visual audit found that
34 of those rows are either equations turned into tables or duplicated Table I
rows. A fake table is worse than plain text: it asserts a structure that does not
exist and scrambles the reading order of the content it swallows.

**Two distinct defects, both in scope:**

1. **Equations gridded into tables.** Example from page 3 of the current output —
   a display equation plus three paragraphs of prose became a 5-column table:
   ```
   | N(t )= N(t | t1)+ N(t | t2)+ | + N(t | tn): |
   | The total number is equal to the sum of | the numbers | of | sequences | ... |
   ```
   The page-3 footer digit `3` was also swallowed into a cell mid-equation.

2. **Duplicated rows in a real table.** Table I (page 40) has each data row
   emitted two or three times, and its header split into two false `#` headings.
   The probability tables on page 6 duplicate every row similarly.

**Tech Stack:** Go, `pkg/table`, `pkg/pdf/backend.go`, Testify.

---

### Task 1: Reproduce and localise

1. Convert `entropy.pdf`, list every table block, and classify each as real or
   fake against the page images (render with
   `pdftoppm -png -r 110 entropy.pdf /tmp/pages/p`).
2. For one clear fake (page 3) and one duplicated real table (page 40),
   determine WHICH detector produced it: `DetectTables` (ruled),
   `DetectAnchoredTextTables` (borderless/word-grid), or the merge in
   `mergePreferredTables`. Instrument if necessary.
3. Write the finding down before changing code.

### Task 2: Suppress equation grids — failing test first

**Files:**
- Test: `pkg/table/detect_test.go` or a new internal test

1. Build a synthetic region from realistic geometry: several lines of display
   mathematics whose inter-token gaps happen to align vertically across rows.
2. Assert `DetectTables` does NOT return a table for it.
3. Assert a genuine borderless table with the same row count IS still detected
   (the negative case is what keeps this change safe).
4. Run and confirm the first assertion fails.

### Task 3: Implement the suppression

**Files:**
- Modify: `pkg/table/detect.go` (and/or `validity.go`)

Design constraint from `AGENTS.md`: geometric and document-general signals only.
Do not test for `=` or `∑` in cell text — character content may only be
supporting evidence inside a broader layout algorithm. Note `pkg/table` already
contains `no_text_cues_internal_test.go` and `no_custom_fixes_test.go`, which
exist to enforce exactly this; your change must keep them passing.

Candidate signals, all geometric:
- **Column-gutter consistency.** A real table's column boundaries are the same x
  positions on EVERY row. Math spacing produces ragged gaps whose positions drift
  row to row. Measure gutter alignment variance and require it to be tight.
- **Gutter persistence.** A real table has whitespace corridors spanning the full
  height of the block. Equations do not.
- **Baseline count per row.** A math row occupies several baselines
  (sub/superscripts, fraction bars); a table row occupies one. This overlaps with
  the display-equation reading-order plan — coordinate if both land together.
- **Row-content shape.** Prose sentences split across cells (long runs broken
  mid-clause) indicate the grid is imaginary.

`ValidityScore` already drops zero-validity tables; consider whether tightening
it is the smallest correct change before adding a new mechanism.

### Task 4: Fix duplicated rows

**Files:**
- Modify: `pkg/table/detect.go` or `pkg/pdf/backend.go`

1. Determine why Table I's rows repeat. Likely candidates:
   `reassignDetectedTableTextFromWords` assigning the same word cells to several
   grid rows, or the base and anchored detectors both contributing overlapping
   rows through `mergePreferredTables`.
2. Write a failing test that reproduces the duplication from synthetic geometry.
3. Fix it so each source cell contributes to exactly one output row.

### Task 5: Keep page furniture out of tables

**Files:**
- Modify: `pkg/pdf/backend.go`

The page-3 footer digit was absorbed into a table cell, and page 27's and page
50's page numbers were absorbed into equations/tables. Consider extracting
marginal page-number cells BEFORE table detection rather than after
(`splitMarginalPageNumberBlocks` currently runs in postprocess). Add a test.

### Task 6: Verify on the real document

1. Rebuild and reconvert `entropy.pdf`.
2. Table blocks should fall from 19 towards ~3–5. Any surviving table must
   correspond to a real table in the page image.
3. Table I (page 40) must have each row exactly once.
4. Confirm the equations released from fake tables now appear as ordinary text
   and are not lost.

### Task 7: Validate against the corpus

1. Follow the DPBench protocol in `AGENTS.md`.
2. `table_structure_teds` is the primary metric (currently 0.763126) and MUST NOT
   regress — this corpus has 78 documents with real tables, so an over-aggressive
   suppression will show up immediately.
3. Required: `errors` stays 0 and no other metric regresses.
4. Run with nothing else on the machine; variance under load is ±3 ms/page.
