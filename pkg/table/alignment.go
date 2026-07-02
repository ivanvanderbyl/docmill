package table

import (
	"math"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// alignment captures the horizontal anchor family a column of cells snaps to.
// A geometry pass classifies columns as left, centre, or right aligned before
// rebuilding their cell boxes.
type alignment string

const (
	alignLeft   alignment = "left"
	alignMiddle alignment = "middle"
	alignRight  alignment = "right"
)

// medianFloat64 returns the median of values. An empty slice returns 0, an odd
// length returns the middle element, and an even length averages the two middle
// elements. The input slice is not mutated.
func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) * 0.5
}

// spread returns the range (max-min) of values, or 0 when empty.
func spread(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	return maximum - minimum
}

// findAlignment classifies a column of cells by the anchor family with the
// tightest spread. Empty input is treated as left aligned, and ties resolve in
// the stable order left, middle, right.
func findAlignment(cells []Cell) alignment {
	if len(cells) == 0 {
		return alignLeft
	}

	lefts := make([]float64, 0, len(cells))
	middles := make([]float64, 0, len(cells))
	rights := make([]float64, 0, len(cells))
	for _, cell := range cells {
		if cell.Box == nil {
			continue
		}
		lefts = append(lefts, cell.Box.L)
		middles = append(middles, (cell.Box.L+cell.Box.R)*0.5)
		rights = append(rights, cell.Box.R)
	}
	if len(lefts) == 0 {
		return alignLeft
	}

	scores := []struct {
		alignment alignment
		spread    float64
	}{
		{alignLeft, spread(lefts)},
		{alignMiddle, spread(middles)},
		{alignRight, spread(rights)},
	}
	best := scores[0]
	for _, score := range scores[1:] {
		if score.spread < best.spread {
			best = score
		}
	}
	return best.alignment
}

// intersectionOverWordMatch reports whether word overlaps cellBox above the
// threshold, measured over the word's own area rather than the cell's. This is
// the direct-assignment rule, matching the existing intersection-over-text-cell
// behaviour.
func intersectionOverWordMatch(cellBox geom.Box, word page.TextCell, threshold float64) bool {
	return word.Box.IntersectionOverSelf(cellBox) > threshold
}

// gridBand is a robust row or column interval recovered from the median edges
// of the cells that span that line.
type gridBand struct {
	Index int
	Start float64
	End   float64
}

// tableGrid holds the estimated row and column bands used to place orphan words
// by containment.
type tableGrid struct {
	Rows []gridBand
	Cols []gridBand
}

// estimateTableGrid derives one band per row and per column using the median
// top/bottom and median left/right of the cells covering that line. Rows or
// columns without any boxed cells emit a zero band that later passes ignore.
func estimateTableGrid(data Data) tableGrid {
	grid := tableGrid{
		Rows: make([]gridBand, 0, data.NumRows),
		Cols: make([]gridBand, 0, data.NumCols),
	}

	for row := 0; row < data.NumRows; row++ {
		cells := cellsForRow(data.Cells, row)
		tops := make([]float64, 0, len(cells))
		bottoms := make([]float64, 0, len(cells))
		for _, cell := range cells {
			if cell.Box == nil {
				continue
			}
			tops = append(tops, math.Min(cell.Box.T, cell.Box.B))
			bottoms = append(bottoms, math.Max(cell.Box.T, cell.Box.B))
		}
		grid.Rows = append(grid.Rows, gridBand{Index: row, Start: medianFloat64(tops), End: medianFloat64(bottoms)})
	}

	for col := 0; col < data.NumCols; col++ {
		cells := cellsForColumn(data.Cells, col)
		lefts := make([]float64, 0, len(cells))
		rights := make([]float64, 0, len(cells))
		for _, cell := range cells {
			if cell.Box == nil {
				continue
			}
			lefts = append(lefts, cell.Box.L)
			rights = append(rights, cell.Box.R)
		}
		grid.Cols = append(grid.Cols, gridBand{Index: col, Start: medianFloat64(lefts), End: medianFloat64(rights)})
	}

	return grid
}

// cellsForRow returns every cell whose row span covers row.
func cellsForRow(cells []Cell, row int) []Cell {
	matches := make([]Cell, 0)
	for _, cell := range cells {
		if cell.StartRow <= row && row < cell.EndRow {
			matches = append(matches, cell)
		}
	}
	return matches
}

// cellsForColumn returns every cell whose column span covers col.
func cellsForColumn(cells []Cell, col int) []Cell {
	matches := make([]Cell, 0)
	for _, cell := range cells {
		if cell.StartCol <= col && col < cell.EndCol {
			matches = append(matches, cell)
		}
	}
	return matches
}

// alignColumns rebuilds each column's cell boxes around the column's median
// anchor and median width, choosing the anchor family with findAlignment. The
// receiver is not mutated; horizontal coverage is only ever expanded so noisy
// boxes never lose their original extent.
func alignColumns(data Data) Data {
	result := Data{
		Cells:   cloneCells(data.Cells),
		NumRows: data.NumRows,
		NumCols: data.NumCols,
	}

	for col := 0; col < result.NumCols; col++ {
		indexes := cellIndexesForColumn(result.Cells, col)
		cells := cellsAtIndexes(result.Cells, indexes)
		mode := findAlignment(cells)
		anchor := medianFloat64(columnAnchors(cells, mode))
		width := medianFloat64(cellWidths(cells))

		for _, index := range indexes {
			if result.Cells[index].Box == nil {
				continue
			}
			box := *result.Cells[index].Box
			candidateL, candidateR := alignedHorizontalSpan(anchor, width, mode)
			adjusted := box
			adjusted.L = math.Min(box.L, candidateL)
			adjusted.R = math.Max(box.R, candidateR)
			result.Cells[index].Box = boxPtr(adjusted)
		}
	}

	return result
}

// cellIndexesForColumn returns the indexes of cells whose column span covers col.
func cellIndexesForColumn(cells []Cell, col int) []int {
	indexes := make([]int, 0)
	for index, cell := range cells {
		if cell.StartCol <= col && col < cell.EndCol {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

// cellsAtIndexes gathers the cells at the given indexes.
func cellsAtIndexes(cells []Cell, indexes []int) []Cell {
	gathered := make([]Cell, 0, len(indexes))
	for _, index := range indexes {
		gathered = append(gathered, cells[index])
	}
	return gathered
}

// cellWordAssignment links a recovered or directly matched word to its target
// cell index.
type cellWordAssignment struct {
	CellIndex int
	Word      page.TextCell
}

// recoverOrphanWords places words that direct intersection missed by the
// containing row and column bands of the estimated grid. A word is only
// assigned when its centre falls inside both a row band and a column band and a
// cell exists at the resulting (row, col). Words already matched (tracked by
// page.TextCell.Index) are skipped.
func recoverOrphanWords(data Data, grid tableGrid, words []page.TextCell, alreadyAssigned map[int]bool) []cellWordAssignment {
	assignments := make([]cellWordAssignment, 0)
	for _, word := range words {
		if alreadyAssigned[word.Index] {
			continue
		}
		x := (word.Box.L + word.Box.R) * 0.5
		y := (math.Min(word.Box.T, word.Box.B) + math.Max(word.Box.T, word.Box.B)) * 0.5

		row, rowOK := containingBand(grid.Rows, y)
		col, colOK := containingBand(grid.Cols, x)
		if !rowOK || !colOK {
			continue
		}
		cellIndex, ok := cellIndexAt(data.Cells, row.Index, col.Index)
		if !ok {
			continue
		}
		assignments = append(assignments, cellWordAssignment{CellIndex: cellIndex, Word: word})
	}
	return assignments
}

// alignCellsToWords expands each cell that received words so its box encloses
// the original cell box plus every assigned word box. Cells without words, or
// assignments pointing outside the cell slice, are left unchanged. The receiver
// is not mutated.
func alignCellsToWords(data Data, assignments []cellWordAssignment) Data {
	result := Data{
		Cells:   cloneCells(data.Cells),
		NumRows: data.NumRows,
		NumCols: data.NumCols,
	}

	byCell := assignmentsByCell(assignments)
	for cellIndex, words := range byCell {
		if cellIndex < 0 || cellIndex >= len(result.Cells) || result.Cells[cellIndex].Box == nil {
			continue
		}
		boxes := []geom.Box{*result.Cells[cellIndex].Box}
		for _, word := range words {
			boxes = append(boxes, word.Box)
		}
		expanded := geom.EnclosingBox(boxes...)
		result.Cells[cellIndex].Box = boxPtr(expanded)
	}

	return result
}

// mergeWordsIntoCells writes each cell's final text by ordering its assigned
// words in reading order (vertical band, then left edge, then document index)
// and joining the non-empty trimmed text with single spaces. Cell geometry and
// table shape are preserved; the receiver is not mutated.
func mergeWordsIntoCells(data Data, assignments []cellWordAssignment) Data {
	result := Data{
		Cells:   cloneCells(data.Cells),
		NumRows: data.NumRows,
		NumCols: data.NumCols,
	}

	byCell := assignmentsByCell(assignments)
	for cellIndex, words := range byCell {
		if cellIndex < 0 || cellIndex >= len(result.Cells) {
			continue
		}
		sort.SliceStable(words, func(i, j int) bool {
			return wordReadingOrderLess(words[i], words[j])
		})

		parts := make([]string, 0, len(words))
		for _, word := range words {
			text := strings.TrimSpace(word.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		result.Cells[cellIndex].Text = strings.Join(parts, " ")
	}

	return result
}

// wordReadingOrderLess orders words top-to-bottom by row band, then
// left-to-right, then by stable document index.
func wordReadingOrderLess(a, b page.TextCell) bool {
	if !sameVisualWordLine(a.Box, b.Box) {
		ta, tb := wordTop(a.Box), wordTop(b.Box)
		if ta != tb {
			return ta < tb
		}
	}
	if a.Box.L != b.Box.L {
		return a.Box.L < b.Box.L
	}
	return a.Index < b.Index
}

// sameVisualWordLine reports whether two word boxes belong to the same visual
// line using only vertical geometry. Sub-point top/baseline jitter is common in
// PDF text extraction, so overlap and centre distance are more stable than a
// rounded top coordinate.
func sameVisualWordLine(a, b geom.Box) bool {
	aTop, aBottom := wordVerticalBounds(a)
	bTop, bBottom := wordVerticalBounds(b)
	aHeight := aBottom - aTop
	bHeight := bBottom - bTop
	minHeight := math.Min(aHeight, bHeight)
	if minHeight <= 0 {
		return false
	}

	overlap := math.Min(aBottom, bBottom) - math.Max(aTop, bTop)
	if overlap > 0 && overlap/minHeight >= 0.5 {
		return true
	}

	tolerance := minHeight * 0.5
	if tolerance < 2 {
		tolerance = 2
	}
	return math.Abs(a.CenterY()-b.CenterY()) <= tolerance
}

func wordTop(box geom.Box) float64 {
	top, _ := wordVerticalBounds(box)
	return top
}

func wordVerticalBounds(box geom.Box) (float64, float64) {
	if box.T <= box.B {
		return box.T, box.B
	}
	return box.B, box.T
}

// assignmentsByCell groups word assignments by their target cell index.
func assignmentsByCell(assignments []cellWordAssignment) map[int][]page.TextCell {
	byCell := make(map[int][]page.TextCell)
	for _, assignment := range assignments {
		byCell[assignment.CellIndex] = append(byCell[assignment.CellIndex], assignment.Word)
	}
	return byCell
}

// containingBand returns the first band whose interval contains value. Bands
// with non-positive extent (rows/columns that had no boxed cells) are skipped.
func containingBand(bands []gridBand, value float64) (gridBand, bool) {
	for _, band := range bands {
		if band.End <= band.Start {
			continue
		}
		if value >= band.Start && value <= band.End {
			return band, true
		}
	}
	return gridBand{}, false
}

// cellIndexAt returns the index of the cell whose span covers (row, col).
func cellIndexAt(cells []Cell, row, col int) (int, bool) {
	for index, cell := range cells {
		if cell.StartRow <= row && row < cell.EndRow && cell.StartCol <= col && col < cell.EndCol {
			return index, true
		}
	}
	return 0, false
}

// columnAnchors projects each cell box onto the anchor coordinate selected by
// mode, skipping cells without a box.
func columnAnchors(cells []Cell, mode alignment) []float64 {
	anchors := make([]float64, 0, len(cells))
	for _, cell := range cells {
		if cell.Box == nil {
			continue
		}
		switch mode {
		case alignRight:
			anchors = append(anchors, cell.Box.R)
		case alignMiddle:
			anchors = append(anchors, (cell.Box.L+cell.Box.R)*0.5)
		default:
			anchors = append(anchors, cell.Box.L)
		}
	}
	return anchors
}

// cellWidths returns the horizontal width of each cell box, skipping cells
// without a box.
func cellWidths(cells []Cell) []float64 {
	widths := make([]float64, 0, len(cells))
	for _, cell := range cells {
		if cell.Box == nil {
			continue
		}
		widths = append(widths, cell.Box.Width())
	}
	return widths
}

// alignedHorizontalSpan returns the left and right edges that place a box of the
// given width against anchor under the given alignment.
func alignedHorizontalSpan(anchor, width float64, mode alignment) (float64, float64) {
	switch mode {
	case alignRight:
		return anchor - width, anchor
	case alignMiddle:
		half := width * 0.5
		return anchor - half, anchor + half
	default:
		return anchor, anchor + width
	}
}
