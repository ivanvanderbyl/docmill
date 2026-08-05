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
