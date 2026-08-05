# Task 1 findings: reproduce and localise (entropy.pdf)

Baseline (commit 513c968): 19 table blocks, 121 pipe rows in entropy.pdf output.

## Census of the 19 table blocks

| # | Line | Page | Verdict | Notes |
|---|------|------|---------|-------|
| 1 | 55 | 3 | FAKE | Display equation N(t)=... + 3 paragraphs of prose gridded 5×5 |
| 2 | 120 | 6 | REAL (dup) | Probability tables; every fraction row duplicated |
| 3 | 404 | ~14 | FAKE (dup) | One equation repeated 4× |
| 4 | 414 | ~14 | FAKE | log2 inequality + prose |
| 5 | 442 | ~15 | FAKE (dup) | Entropy H equation repeated 4× |
| 6 | 450 | ~15 | REAL-ish | A/B/C/D code table; header swallowed prose, one column duplicated |
| 7 | 461 | ~16 | REAL-ish | 00→A' mapping table; header swallowed prose |
| 8 | 518 | ~17 | FAKE | R = H(x) − Hy(x) equation group |
| 9 | 626 | ~21 | FAKE (dup) | C = log4... equations |
| 10 | 634 | ~21 | FAKE | C = log3... equations |
| 11 | 648 | ~22 | FAKE | C = Max... equation + prose |
| 12 | 677 | ~23 | FAKE (dup) | ∑ equations repeated 3× |
| 13 | 707 | ~24 | FAKE (dup) | A(t)/A(s) limit equations |
| 14 | 721 | ~24 | FAKE | H = ∑ pi equation + prose |
| 15 | 762 | ~25 | FAKE (dup) | ∑Pi p(s) equations |
| 16 | 1042 | 40 | REAL (dup) | Table I; header split into 2 false headings; rows ×2–3 |
| 17 | 1289 | ~50 | FAKE | Prose slab (Theorem 22/23 region) |
| 18 | 1359 | ~54 | FAKE (dup) | logr(xi) equation repeated 3× |
| 19 | 1367 | ~54 | FAKE (dup) | H3−H1 equation repeated 3× |

Roughly 4 real tables (2, 6, 7, 16); 15 fake. 121 rows, of which ~34 are
equation-grid rows or duplicates.

## Defect 1: equations gridded into tables (page 3 exemplar)

Producer: `detectWideMultilineTextTables` on the anchored path
(`DetectAnchoredTextTables`, pkg/table/detect.go). Base `DetectTables` finds
nothing on page 3 (0 rulings). The equation's inter-token gaps line up loosely
across the display line and the neighbouring prose lines, forming a 5×5 grid.
`ValidityScore` = 0.527, so the zero-validity floor does not fire (the prose
column floor requires EVERY content column to be prose; equation fragments are
short so their columns don't count as prose).

## Defect 2: duplicated rows in a real table (page 40, Table I)

Producer: base path main loop `buildDetectedTablePrefix` →
`buildDetectedTable` → `ReconstructGridWithRows` → `buildTableFromGrid`.
The ruled detector finds nothing despite 63 rulings (the in-cell plot axes
break `spansGrid`). The text-line grid has 15 rows for 6 visual rows; tall
cells (fractions, plot labels) get RowSpan 2–3 via `gridMergeBoxes`. The 15×5
result has 48 cells, 17 with RowSpan>1.

Duplication mechanism: `render.Table` renders `grid[row][col].Text` for every
row×col slot. `Data.Grid()` places a spanned cell into every slot it covers,
so a RowSpan-3 cell's text is emitted three times (and a ColSpan-N cell's text
N times in one row). Markdown has no rowspan, so the correct rendering is to
emit the text once at the cell's anchor slot (StartRow, StartCol) and leave
continuation slots blank.

The header of Table I ("GAIN | ENTROPY POWER FACTOR | ...") sits above the
detected table box and is consumed by heading detection instead, producing the
two false `#` headings.

---

# Results (post-fix, commit on ivan/equation-fake-tables)

## entropy.pdf (Task 6)

| Metric | Before | After |
|---|---|---|
| Table blocks | 19 | 8 |
| Pipe rows | 121 | 44 |
| Duplicated consecutive rows | many | 0 |

Surviving blocks: 4 real (page-6 probability tables, both page-18 code
tables, page-40 Table I — each row exactly once) and 4 residual fakes
(page-27 "C = log3", page-29 "m/A(t)", page-53 pair). The residual fakes are
small stacked-fraction equation grids whose numerals align in genuine columns
with real whitespace gutters — at line-cell granularity they are geometrically
indistinguishable from small aligned tables, so the gutter-persistence gate
correctly declines to fire. Killing them would need a sub-line/baseline-stack
signal that also fires on the REAL page-6 table (its cells are stacked
fractions too); measured and rejected as unsafe.

Page numbers previously swallowed into tables/equations (pages 3, 27, 50) now
emit as standalone marginal blocks, matching the document's existing
convention (53 baseline / 54 after).

## DPBench 200-PDF corpus (Task 7)

| Metric | Baseline (513c968) | Current | Delta |
|---|---|---|---|
| table_structure_teds | 0.763126 | 0.765178 | +0.002052 |
| extraction_accuracy | 0.921572 | 0.921965 | +0.000392 |
| reading_order_nid | 0.893762 | 0.893486 | -0.000275 |
| heading_level_mhs | 0.770814 | 0.770814 | 0 |
| errors | 0 | 0 | 0 |
| cases | 200 | 200 | 0 |

Only 7 of 200 cases changed. The aggregate reading-order dip comes from 3
cases where the SAME change produced a larger table/extraction win — e.g.
doc_a5b975 (TEDS 0 → 1: a display-equation fake grid released to prose;
NID -0.0153) and doc_c4416f (TEDS +0.1165, extraction +0.0095, NID -0.0384).
No case regressed without a larger compensating improvement on the same
document. milliseconds_per_page measured under concurrent load (3 other
agents); not meaningful per the task brief.

---

# Post-review revision

The first submission was reviewed and not merged: the gate suppressed a real
four-column numeric table whose right-aligned entries have mixed magnitude
(wide entries reach left of their column's MEDIAN content extent and were
scored as gutter crossings). Changes in this revision:

1. **Crossing co-signal (the blocker fix).** A run covering a corridor
   midpoint only counts as a crossing when it also overlaps ANOTHER column's
   content core by more than the tolerance. A wide ragged entry sits alone in
   its own column and is now exempt; genuine spanning prose still covers the
   neighbouring columns' content. Regression tests
   (`TestDetect*KeepsRaggedRightAlignedNumericTable`) verified to fail without
   the fix.
2. **Crossing floor raised 2 → 3** so up to two spanning lines (merged group
   header plus a full-width title/section/footnote line) never fire the gate;
   near-miss negative tests added for both shapes, plus direct tests for
   `dropGutterCrossingTables` claim release and the token fallback.
3. **Degenerate corridor width** raised from the crossing tolerance (~2.5pt)
   to half the modal character height capped at 6pt: a corridor narrower than
   half a character cannot separate columns visually. Real-table margins:
   smallest real median corridor measured is 9.6pt (page-6 table) apart from
   one isolated 1.6pt corridor, which stays under the ≥2-corridor floor.
4. **connect.go** `appendTableRows`/`gridRowText` now takes a spanned cell's
   text only at its anchor slot (same rule as render.Table), with a test —
   cross-page-stitched tables no longer duplicate spanned text.
5. **wordCells** are now filtered through the marginal page-number split
   before anchored detection and word-based text reassignment, closing the
   word-path route for swallowing page numbers.
6. **Page-number extraction co-signal:** the cell must also be vertically
   isolated (nearest neighbour further than 2.5x its height, min 18pt), so a
   bare year/count in a margin-band table row is never pulled out of table
   detection; test added.

## Revised entropy.pdf result

19 blocks / 121 rows -> **14 blocks / 66 rows** (first submission was 8/44).
Zero duplicated rows; all four real tables intact; page numbers standalone.
The safety fixes give back six equation-drift fakes that only the (unsafe)
midpoint-crossing rule had caught (pages 16, 18, 21, 27, 28, 30): their runs
cover corridor midpoints without touching neighbouring content — the same
geometry as a wide ragged numeric entry, which is exactly the false-positive
class the review found. Distinguishing them needs a signal this gate does not
have; deferred rather than risked.

## Revised DPBench (vs 513c968)

| Metric | Baseline | Revised | Delta |
|---|---|---|---|
| table_structure_teds | 0.763126 | 0.763683 | +0.000557 |
| extraction_accuracy | 0.921572 | 0.921965 | +0.000392 |
| reading_order_nid | 0.893762 | 0.893678 | -0.000084 |
| heading_level_mhs | 0.770814 | 0.770814 | 0 |
| errors / cases | 0 / 200 | 0 / 200 | 0 |

7 cases changed; both reading-order dips co-occur with larger same-case wins
(doc_a5b975: TEDS 0 -> 1, NID -0.0153; doc_8bfb767: extraction +0.002,
NID -0.0016). No case regressed without a compensating improvement.
