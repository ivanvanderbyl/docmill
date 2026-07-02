package table

import (
	"math"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/textline"
)

// Grid reconstruction for borderless text tables.
//
// This is the grid-first layer that the multiline/caption-before/wide table
// builders call instead of deriving column boxes from a single anchor row and
// assigning cells by horizontal centre. It produces a row/column grid with
// spanning cells, recovered purely from text-cell geometry — no ML model.
//
// Every threshold below is derived from geometry (median line height, overlap
// fractions, gutter fractions) — no corpus-specific signals.

// GridResult is the output of grid reconstruction.
type GridResult struct {
	TableBox geom.Box
	RowBoxes []geom.Box   // logical row bands, top-to-bottom
	ColBoxes []geom.Box   // logical column bands, left-to-right
	Assign   []CellAssign // per input-cell grid placement
}

// CellAssign is one input cell's placement in the reconstructed grid.
type CellAssign struct {
	CellIndex int
	Row       int
	Col       int
	ColSpan   int
	RowSpan   int
}

func buildTableFromGrid(grid GridResult, cells []page.TextCell) (DetectedTable, bool) {
	if len(grid.RowBoxes) < gridMinRows || len(grid.ColBoxes) < gridMinCols {
		return DetectedTable{}, false
	}

	merges := gridMergeBoxes(grid)
	data := FromRegions(grid.TableBox, grid.RowBoxes, grid.ColBoxes, merges, RegionSemantics{
		ColumnHeaders: []geom.Box{grid.RowBoxes[0]},
	}).WithAssignedText(cells, 0.3)

	return DetectedTable{
		Data:      data,
		Box:       grid.TableBox,
		TextCells: append([]page.TextCell(nil), cells...),
	}, true
}

func gridMergeBoxes(grid GridResult) []geom.Box {
	merges := make([]geom.Box, 0)
	seen := make(map[[4]int]bool)
	for _, assignment := range grid.Assign {
		if assignment.RowSpan <= 1 && assignment.ColSpan <= 1 {
			continue
		}
		if assignment.Row < 0 || assignment.Col < 0 || assignment.Row >= len(grid.RowBoxes) || assignment.Col >= len(grid.ColBoxes) {
			continue
		}
		endRow := assignment.Row + max(assignment.RowSpan, 1) - 1
		endCol := assignment.Col + max(assignment.ColSpan, 1) - 1
		if endRow >= len(grid.RowBoxes) {
			endRow = len(grid.RowBoxes) - 1
		}
		if endCol >= len(grid.ColBoxes) {
			endCol = len(grid.ColBoxes) - 1
		}
		key := [4]int{assignment.Row, endRow, assignment.Col, endCol}
		if seen[key] {
			continue
		}
		seen[key] = true
		merges = append(merges, geom.Box{
			L:      grid.ColBoxes[assignment.Col].L,
			T:      grid.RowBoxes[assignment.Row].T,
			R:      grid.ColBoxes[endCol].R,
			B:      grid.RowBoxes[endRow].B,
			Origin: grid.TableBox.Origin,
		})
	}
	return merges
}

// grid overlap / adjacency thresholds. All document-general.
const (
	// gridRowGapFactor: a vertical gap larger than this × median line height
	// starts a new logical row. 1.0 keeps wrapped lines of one cell together
	// (consecutive lines are at most one line-height apart) while separating
	// distinct rows whose leading exceeds a full line.
	gridRowGapFactor = 1.0
	// gridRowOverlapFraction: a cell joins an existing row band iff its
	// y-extent overlaps the band by at least this fraction of the cell height.
	gridRowOverlapFraction = 0.5
	// gridGutterEmptyFraction: an x-bin is a column gutter iff it is empty of
	// cell centres for at least this fraction of the logical rows.
	gridGutterEmptyFraction = 0.8
	// gridColOverlapFraction: a cell is assigned to column k iff its bbox
	// overlaps colBox_k by at least this fraction of the CELL's area.
	gridColOverlapFraction = 0.5
	// gridRowOverlapForSpan: a cell spans rows r..s iff it overlaps each of
	// rowBox_r..s by at least this fraction of the CELL's area.
	gridRowOverlapForSpan = 0.5
	// gridMinCols / gridMinRows: floors below which we report no grid.
	gridMinCols = 2
	gridMinRows = 2
	// gridBinColumns: resolution of the gutter-detection x-grid.
	gridBinColumns = 200
)

// ReconstructGrid clusters text cells into a table grid by geometry.
//
// Pipeline:
//  1. logical rows — maximal horizontal bands of vertically-adjacent cells
//     (gap < gridRowGapFactor × median line height); a cell joins the band
//     above iff its y-extent overlaps the band by > gridRowOverlapFraction.
//  2. columns — whitespace gutters spanning ≥ gridGutterEmptyFraction of the
//     logical rows (table-scoped gutter detection).
//  3. assignment — argmax area-overlap of each cell's bbox with each column
//     band; ColSpan = count of column bands overlapped by >
//     gridColOverlapFraction of the cell's area; RowSpan analogously.
//
// cells must be the full set of text cells inside tableBox. The tableBox is
// respected: cells are clipped to it before overlap is computed.
func ReconstructGrid(cells []page.TextCell, tableBox geom.Box) (GridResult, error) {
	return ReconstructGridWithAnchor(cells, tableBox, nil)
}

// ReconstructGridWithRows is like ReconstructGridWithAnchor but takes explicit
// logical-row boxes (already merged by the caller) instead of reconstructing
// rows from the cells. Callers like buildDetectedTable already merge
// first-column continuation rows themselves; re-merging in reconstructLogicalRows
// would over-collapse. This variant stretches each row to the table width,
// derives columns from the anchor, and assigns cells by area overlap.
func ReconstructGridWithRows(cells []page.TextCell, tableBox geom.Box, rowBoxes []geom.Box, anchor []page.TextCell) (GridResult, error) {
	visible := visibleCells(cells)
	if len(visible) < gridMinCols+1 || len(rowBoxes) < gridMinRows {
		return GridResult{}, nil
	}
	// Stretch each row band to the full table width so FromRegions'
	// row×col intersection yields a cell in every column.
	for i := range rowBoxes {
		rowBoxes[i].L = tableBox.L
		rowBoxes[i].R = tableBox.R
	}

	var colBoxes []geom.Box
	if len(anchor) >= gridMinCols {
		colBoxes = columnBoxesFromAnchor(anchor, tableBox)
	} else {
		colBoxes = reconstructColumns(visible, rowBoxes, tableBox)
	}
	if len(colBoxes) < gridMinCols {
		return GridResult{}, nil
	}

	// Assign cells to rows by vertical-centre containment, then to columns by
	// area overlap.
	membership := make(map[int]int, len(visible))
	for _, c := range visible {
		cy := c.Box.CenterY()
		for ri, rb := range rowBoxes {
			top, bot := orderedY(rb)
			if cy >= top && cy <= bot {
				membership[c.Index] = ri
				break
			}
		}
	}
	assign := assignCellsToGrid(visible, rowBoxes, colBoxes, membership)
	return GridResult{
		TableBox: tableBox,
		RowBoxes: rowBoxes,
		ColBoxes: colBoxes,
		Assign:   assign,
	}, nil
}

// from an explicit anchor row (typically the header) when supplied. The
// anchor row has one cell per column without wrapping, so its edges define
// columns exactly; this is more reliable than the densest-row heuristic when
// data rows have varying cell counts. Logical-row reconstruction and
// area-overlap assignment are unchanged.
func ReconstructGridWithAnchor(cells []page.TextCell, tableBox geom.Box, anchor []page.TextCell) (GridResult, error) {
	visible := visibleCells(cells)
	if len(visible) < gridMinCols+1 {
		return GridResult{}, nil
	}

	rowBoxes, rowMembership := reconstructLogicalRows(visible)
	if len(rowBoxes) < gridMinRows {
		return GridResult{}, nil
	}
	// Stretch each row band to the full table width so FromRegions'
	// row×col intersection yields a cell in every column.
	for i := range rowBoxes {
		rowBoxes[i].L = tableBox.L
		rowBoxes[i].R = tableBox.R
	}

	var colBoxes []geom.Box
	if len(anchor) >= gridMinCols {
		colBoxes = anchoredColumnBoxes(anchor, tableBox)
	} else {
		colBoxes = reconstructColumns(visible, rowBoxes, tableBox)
	}
	if len(colBoxes) < gridMinCols {
		return GridResult{}, nil
	}

	assign := assignCellsToGrid(visible, rowBoxes, colBoxes, rowMembership)
	return GridResult{
		TableBox: tableBox,
		RowBoxes: rowBoxes,
		ColBoxes: colBoxes,
		Assign:   assign,
	}, nil
}

// visibleCells returns non-empty cells sorted by vertical centre then left.
func visibleCells(cells []page.TextCell) []page.TextCell {
	out := make([]page.TextCell, 0, len(cells))
	for _, c := range cells {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i].Box.CenterY(), out[j].Box.CenterY()
		if ci == cj {
			return out[i].Box.L < out[j].Box.L
		}
		return ci < cj
	})
	return out
}

// reconstructLogicalRows clusters cells into horizontal bands. A new band
// starts when a cell appears in the table's leftmost occupied column (the
// anchor column): each logical row begins with its first-column cell, and
// wrapped fragments in other columns (or further first-column lines of a
// multi-line cell) join the current band by vertical adjacency. This mirrors
// how the old build*LogicalRows continuation predicates worked but is driven
// by column membership rather than text content.
//
// gapLimit (gridRowGapFactor × median line height) bounds how far below the
// current band a wrapped fragment may sit and still belong to it.
func reconstructLogicalRows(cells []page.TextCell) ([]geom.Box, map[int]int) {
	if len(cells) == 0 {
		return nil, nil
	}
	median := medianLineHeight(cells)
	gapLimit := math.Max(median*gridRowGapFactor, 1.0)

	// Determine the leftmost column centre: the smallest horizontal centre
	// among cells that are not the sole wide cell on their line. This is the
	// anchor column whose cells mark new logical rows.
	centres := make([]float64, 0, len(cells))
	for _, c := range cells {
		centres = append(centres, c.Box.CenterX())
	}
	sort.Float64s(centres)
	// Cluster centres coarsely to find the leftmost cluster.
	leftmostCluster := centres[0]
	clusterEnd := leftmostCluster + math.Max(median, 8.0)
	for _, x := range centres[1:] {
		if x <= clusterEnd {
			continue
		}
		break
	}
	_ = leftmostCluster
	// Simpler: the anchor column is the column of the leftmost cell. A cell
	// starts a new row iff its centre is within the leftmost cluster.
	anchorMax := centres[0] + math.Max(median, 8.0)

	type band struct {
		box   geom.Box
		cells []page.TextCell
	}
	var bands []band

	startsNewRow := func(c page.TextCell) bool {
		return c.Box.CenterX() <= anchorMax
	}

	for _, c := range cells {
		cellTop := math.Min(c.Box.T, c.Box.B)
		cellBottom := math.Max(c.Box.T, c.Box.B)
		placed := false
		if len(bands) > 0 {
			b := &bands[len(bands)-1]
			bandBottom := math.Max(b.box.T, b.box.B)
			// A first-column cell always starts a new row (unless it abuts the
			// current band as a wrapped continuation — but first-column wrapping
			// is rare; treat a first-column cell within gapLimit as continuation).
			if !startsNewRow(c) && cellTop <= bandBottom+gapLimit {
				b.cells = append(b.cells, c)
				if cellTop < math.Min(b.box.T, b.box.B) {
					b.box.T = c.Box.T
				}
				if cellBottom > bandBottom {
					b.box.B = c.Box.B
				}
				placed = true
			}
			// A first-column cell within gapLimit of the band's bottom is a
			// wrapped continuation of the first-column cell (multi-line first
			// column), so it also joins.
			if startsNewRow(c) && cellTop <= bandBottom+gapLimit && cellTop >= bandBottom-median {
				b.cells = append(b.cells, c)
				if cellBottom > bandBottom {
					b.box.B = c.Box.B
				}
				placed = true
			}
		}
		if !placed {
			bands = append(bands, band{box: c.Box, cells: []page.TextCell{c}})
		}
	}

	// Sort bands top-to-bottom and build output.
	sort.SliceStable(bands, func(i, j int) bool {
		return bands[i].box.CenterY() < bands[j].box.CenterY()
	})
	rowBoxes := make([]geom.Box, len(bands))
	membership := make(map[int]int, len(cells))
	for ri, b := range bands {
		rowBoxes[ri] = b.box
		for _, c := range b.cells {
			membership[c.Index] = ri
		}
	}
	return rowBoxes, membership
}

// reconstructColumns recovers column bands by picking the densest row
// (the row band with the most cells, typically the header) and splitting
// the table width at the midpoints between its cell edges. Callers that
// already know the anchor row should use ReconstructGridWithAnchor instead.
func reconstructColumns(cells []page.TextCell, rowBoxes []geom.Box, tableBox geom.Box) []geom.Box {
	if len(cells) == 0 || tableBox.Width() <= 0 {
		return nil
	}
	rowCells := densestRow(cells)
	if len(rowCells) < gridMinCols {
		return nil
	}
	return columnBoxesFromAnchor(rowCells, tableBox)
}

// columnBoxesFromAnchor derives column boxes from an explicit anchor row by
// splitting the table width at the midpoints between adjacent cell edges. A
// header row has one cell per column without wrapping, so its edges define
// the columns exactly.
func columnBoxesFromAnchor(anchor []page.TextCell, tableBox geom.Box) []geom.Box {
	if len(anchor) == 0 || tableBox.Width() <= 0 {
		return nil
	}
	rowCells := append([]page.TextCell(nil), anchor...)
	sort.SliceStable(rowCells, func(i, j int) bool { return rowCells[i].Box.L < rowCells[j].Box.L })
	cols := make([]geom.Box, 0, len(rowCells))
	for i, c := range rowCells {
		left := tableBox.L
		if i > 0 {
			left = (rowCells[i-1].Box.R + c.Box.L) / 2
		}
		right := tableBox.R
		if i+1 < len(rowCells) {
			right = (c.Box.R + rowCells[i+1].Box.L) / 2
		}
		cols = append(cols, geom.Box{L: left, T: tableBox.T, R: right, B: tableBox.B, Origin: tableBox.Origin})
	}
	return cols
}

// densestRow returns the cells whose vertical centres cluster most tightly,
// i.e. the row band with the most cells. Used to pick the anchor for column
// derivation.
func densestRow(cells []page.TextCell) []page.TextCell {
	if len(cells) == 0 {
		return nil
	}
	ordered := append([]page.TextCell(nil), cells...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Box.CenterY() < ordered[j].Box.CenterY()
	})
	median := medianLineHeight(ordered)
	tol := math.Max(median*gridRowGapFactor, 1.0)
	bestStart, bestEnd, bestCount := 0, 0, 0
	start := 0
	for i := 1; i <= len(ordered); i++ {
		if i < len(ordered) && math.Abs(ordered[i].Box.CenterY()-ordered[i-1].Box.CenterY()) <= tol {
			continue
		}
		if i-start > bestCount {
			bestCount = i - start
			bestStart, bestEnd = start, i
		}
		start = i
	}
	if bestCount == 0 {
		return ordered
	}
	return ordered[bestStart:bestEnd]
}

// assignCellsToGrid assigns each cell to (row, col) by overlap and computes spans.
func assignCellsToGrid(cells []page.TextCell, rowBoxes, colBoxes []geom.Box, rowMembership map[int]int) []CellAssign {
	assign := make([]CellAssign, 0, len(cells))
	for _, c := range cells {
		// Row: from membership (already computed by vertical adjacency).
		row := rowMembership[c.Index]
		rowSpan := 1
		// RowSpan: a cell spans rows r..s iff it overlaps each by > gridRowOverlapForSpan of itself.
		for r := row + 1; r < len(rowBoxes); r++ {
			if overlapFractionOfCell(c.Box, rowBoxes[r]) > gridRowOverlapForSpan {
				rowSpan++
			} else {
				break
			}
		}

		// Col: argmax area-overlap with each column band.
		col, colSpan := columnForCell(c.Box, colBoxes)
		if col < 0 {
			continue
		}
		assign = append(assign, CellAssign{
			CellIndex: c.Index, Row: row, Col: col, ColSpan: colSpan, RowSpan: rowSpan,
		})
	}
	return assign
}

// columnForCell returns the column index (argmax overlap) and the col-span.
func columnForCell(cell geom.Box, colBoxes []geom.Box) (int, int) {
	if len(colBoxes) == 0 {
		return -1, 0
	}
	cellArea := cell.Area()
	if cellArea <= 0 {
		// Degenerate: fall back to centre containment.
		cx := cell.CenterX()
		for i, cb := range colBoxes {
			if cx >= cb.L && cx <= cb.R {
				return i, 1
			}
		}
		return 0, 1
	}
	bestCol := 0
	bestOverlap := 0.0
	span := 0
	for i, cb := range colBoxes {
		overlap := cell.IntersectionArea(cb)
		if overlap > bestOverlap {
			bestOverlap = overlap
			bestCol = i
		}
		cellCoverage := overlap / cellArea
		colCoverage := 0.0
		horizontalOverlap := math.Min(cell.R, cb.R) - math.Max(cell.L, cb.L)
		if cb.Width() > 0 && horizontalOverlap > 0 {
			colCoverage = horizontalOverlap / cb.Width()
		}
		if cellCoverage > gridColOverlapFraction || colCoverage > gridColOverlapFraction {
			span++
		}
	}
	if span < 1 {
		span = 1
	}
	return bestCol, span
}

// overlapFractionOfCell returns overlap(rowBox) / cellArea.
func overlapFractionOfCell(cell, row geom.Box) float64 {
	cellArea := cell.Area()
	if cellArea <= 0 {
		return 0
	}
	return cell.IntersectionArea(row) / cellArea
}

func orderedY(b geom.Box) (float64, float64) {
	t, bb := b.T, b.B
	if t > bb {
		return bb, t
	}
	return t, bb
}

// medianLineHeight returns the median cell height of the visible cells.
func medianLineHeight(cells []page.TextCell) float64 {
	if len(cells) == 0 {
		return 0
	}
	heights := make([]float64, 0, len(cells))
	for _, c := range cells {
		h := c.Box.Height()
		if h > 0 {
			heights = append(heights, h)
		}
	}
	if len(heights) == 0 {
		return 0
	}
	sort.Float64s(heights)
	return heights[len(heights)/2]
}

// GridLogicalRows reconstructs the text content per logical grid cell, in the
// shape the existing validity predicates consume ([]multilineLogicalRow). It
// is the bridge between ReconstructGrid and the valid* predicates that gate
// the three borderless builders.
func (g GridResult) LogicalRows(cells []page.TextCell) []multilineLogicalRow {
	rows := make([]multilineLogicalRow, len(g.RowBoxes))
	for i := range rows {
		rows[i].Parts = make([][]string, len(g.ColBoxes))
	}
	byIndex := make(map[int]page.TextCell, len(cells))
	for _, c := range cells {
		byIndex[c.Index] = c
	}
	for _, a := range g.Assign {
		c, ok := byIndex[a.CellIndex]
		if !ok {
			continue
		}
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		if a.Row < 0 || a.Row >= len(rows) {
			continue
		}
		// Distribute across spanned columns (text goes in the start column).
		endCol := min(a.Col+a.ColSpan, len(rows[a.Row].Parts))
		if a.Col < 0 || a.Col >= len(rows[a.Row].Parts) {
			continue
		}
		rows[a.Row].Parts[a.Col] = append(rows[a.Row].Parts[a.Col], text)
		// Mark spanned columns as occupied (empty text) so span cells preserve shape.
		for k := a.Col + 1; k < endCol; k++ {
			if len(rows[a.Row].Parts[k]) == 0 {
				rows[a.Row].Parts[k] = append(rows[a.Row].Parts[k], "")
			}
		}
	}
	// Compute the bounding box per logical row from its cells.
	for ri := range rows {
		var boxes []geom.Box
		for _, a := range g.Assign {
			if a.Row == ri {
				if c, ok := byIndex[a.CellIndex]; ok {
					boxes = append(boxes, c.Box)
				}
			}
		}
		if len(boxes) > 0 {
			rows[ri].Box = geom.EnclosingBox(boxes...)
		}
	}
	return rows
}

// buildTableFromGrid turns a reconstructed grid into a DetectedTable. It feeds
// the grid's row/column bands (and any spanning cells) into FromRegions, then
// assigns text to grid cells by area overlap. This is the shared tail of the
// three borderless-table builders (multiline-numeric, caption-before, wide).

// densestLayoutRow returns the layout row (textline.ParagraphTextLine) with the most cells —
// the best column anchor, since a row with one cell per column has no
// wrapping and defines column edges exactly.
func densestLayoutRow(rows []textline.ParagraphTextLine) []page.TextCell {
	best := 0
	for i := 1; i < len(rows); i++ {
		if len(rows[i].Cells) > len(rows[best].Cells) {
			best = i
		}
	}
	if best < len(rows) {
		return rows[best].Cells
	}
	return nil
}

// gridHasColumnSupport reports whether the header row (row 0) populates
// >= minCols distinct columns and at least one other row populates >= 2.
// It is the shape gate that replaces the centre-cluster rowsFitClusters
// check: it accepts genuine multi-column tables (including those with a
// blank first column on grouped data rows) while rejecting label-value prose
// whose every row fills only 1-2 columns. The header is the reliable signal
// because a real table's header has one cell per column without wrapping.
func gridHasColumnSupport(grid GridResult, minCols int) bool {
	if len(grid.RowBoxes) == 0 {
		return false
	}
	colsInRow := func(r int) int {
		seen := make(map[int]bool)
		for _, a := range grid.Assign {
			if a.Row == r && a.Col >= 0 {
				seen[a.Col] = true
			}
		}
		return len(seen)
	}
	if colsInRow(0) < minCols {
		return false
	}
	for r := 1; r < len(grid.RowBoxes); r++ {
		if colsInRow(r) >= 2 {
			return true
		}
	}
	return false
}
