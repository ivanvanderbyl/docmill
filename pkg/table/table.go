// Package table detects and reconstructs tabular structure from positioned text
// cells and ruling segments, producing a Data grid that render serialises to a
// Markdown table. It also decodes OTSL token sequences into the same grid.
package table

import (
	"math"
	"slices"
	"sort"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
)

type Cell struct {
	Text         string
	Box          *geom.Box
	RowSpan      int
	ColSpan      int
	StartRow     int
	EndRow       int
	StartCol     int
	EndCol       int
	ColumnHeader bool
	RowHeader    bool
	RowSection   bool
}

type Data struct {
	Cells   []Cell
	NumRows int
	NumCols int
}

type RegionSemantics struct {
	ColumnHeaders []geom.Box
	RowHeaders    []geom.Box
	RowSections   []geom.Box
}

// FromGrid builds a Data from a dense row-major grid of cell text. Each
// entry becomes a single-span Cell whose StartRow/StartCol position lets
// Grid() reconstruct the same layout, so render.Table reproduces the rows
// verbatim. Rows shorter than the widest row are padded with empty cells. It is
// used by cross-page table stitching to assemble a merged table from the
// concatenated rows of two adjacent-page tables.
func FromGrid(rows [][]string) Data {
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if len(rows) == 0 || numCols == 0 {
		return Data{}
	}

	cells := make([]Cell, 0, len(rows)*numCols)
	for rowIdx, row := range rows {
		for colIdx := 0; colIdx < numCols; colIdx++ {
			text := ""
			if colIdx < len(row) {
				text = row[colIdx]
			}
			cells = append(cells, Cell{
				Text:     text,
				RowSpan:  1,
				ColSpan:  1,
				StartRow: rowIdx,
				EndRow:   rowIdx + 1,
				StartCol: colIdx,
				EndCol:   colIdx + 1,
			})
		}
	}

	return Data{
		Cells:   cells,
		NumRows: len(rows),
		NumCols: numCols,
	}
}

func FromRegions(tableBox geom.Box, rows, cols, merges []geom.Box, semantics RegionSemantics) Data {
	const containmentThreshold = 0.5

	allRows := append([]geom.Box(nil), rows...)
	allRows = append(allRows, semantics.RowSections...)

	filteredRows := dedupeBoxes(filterContainedBoxes(allRows, tableBox, containmentThreshold), 0.9)
	filteredCols := dedupeBoxes(filterContainedBoxes(cols, tableBox, containmentThreshold), 0.9)
	filteredMerges := dedupeBoxes(filterContainedBoxes(merges, tableBox, containmentThreshold), 0.9)
	columnHeaders := dedupeBoxes(filterContainedBoxes(semantics.ColumnHeaders, tableBox, containmentThreshold), 0.9)
	rowHeaders := dedupeBoxes(filterContainedBoxes(semantics.RowHeaders, tableBox, containmentThreshold), 0.9)
	rowSections := dedupeBoxes(filterContainedBoxes(semantics.RowSections, tableBox, containmentThreshold), 0.9)

	if len(filteredRows) == 0 || len(filteredCols) == 0 {
		return Data{
			NumRows: 1,
			NumCols: 1,
			Cells: []Cell{{
				Box:      boxPtr(tableBox),
				RowSpan:  1,
				ColSpan:  1,
				StartRow: 0,
				EndRow:   1,
				StartCol: 0,
				EndCol:   1,
			}},
		}
	}

	return Data{
		Cells:   computeCells(filteredRows, filteredCols, filteredMerges, rowHeaders, columnHeaders, rowSections),
		NumRows: len(filteredRows),
		NumCols: len(filteredCols),
	}
}

// WithAssignedText assigns text cells to table cells and returns a new
// Data; the receiver is never mutated. Words are first matched directly by
// intersection-over-text-cell, then a robust geometry pass aligns column boxes
// by median anchors, estimates row/column grid bands, and recovers orphan words
// that fall inside the estimated grid but outside any raw cell box. Cells expand
// to enclose their assigned words before the words are merged into reading-order
// text.
func (d Data) WithAssignedText(textCells []page.TextCell, intersectionOverTextCell float64) Data {
	base := Data{
		Cells:   cloneCells(d.Cells),
		NumRows: d.NumRows,
		NumCols: d.NumCols,
	}

	assignments := make([]cellWordAssignment, 0, len(textCells))
	alreadyAssigned := make(map[int]bool, len(textCells))
	for index, cell := range base.Cells {
		if cell.Box == nil {
			continue
		}
		for _, textCell := range textCells {
			if intersectionOverWordMatch(*cell.Box, textCell, intersectionOverTextCell) {
				assignments = append(assignments, cellWordAssignment{CellIndex: index, Word: textCell})
				alreadyAssigned[textCell.Index] = true
			}
		}
	}

	aligned := alignColumns(base)
	grid := estimateTableGrid(aligned)
	recovered := recoverOrphanWords(aligned, grid, textCells, alreadyAssigned)
	assignments = append(assignments, recovered...)
	assignments = dedupeWordAssignments(assignments, base.Cells)

	expanded := alignCellsToWords(aligned, assignments)
	return mergeWordsIntoCells(expanded, assignments)
}

func dedupeWordAssignments(assignments []cellWordAssignment, cells []Cell) []cellWordAssignment {
	if len(assignments) < 2 {
		return assignments
	}

	bestByWord := make(map[wordAssignmentKey]cellWordAssignment, len(assignments))
	for _, assignment := range assignments {
		key := wordAssignmentIdentity(assignment.Word)
		best, ok := bestByWord[key]
		if !ok || wordAssignmentBetter(assignment, best, cells) {
			bestByWord[key] = assignment
		}
	}

	out := make([]cellWordAssignment, 0, len(bestByWord))
	seen := make(map[wordAssignmentKey]bool, len(bestByWord))
	for _, assignment := range assignments {
		key := wordAssignmentIdentity(assignment.Word)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, bestByWord[key])
	}
	return out
}

type wordAssignmentKey struct {
	index         int
	text          string
	left, top     int64
	right, bottom int64
}

func wordAssignmentIdentity(word page.TextCell) wordAssignmentKey {
	const scale = 1000
	return wordAssignmentKey{
		index:  word.Index,
		text:   word.Text,
		left:   int64(math.Round(word.Box.L * scale)),
		top:    int64(math.Round(word.Box.T * scale)),
		right:  int64(math.Round(word.Box.R * scale)),
		bottom: int64(math.Round(word.Box.B * scale)),
	}
}

func wordAssignmentBetter(candidate, current cellWordAssignment, cells []Cell) bool {
	candidateScore, candidateDistance := wordAssignmentScore(candidate, cells)
	currentScore, currentDistance := wordAssignmentScore(current, cells)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	return candidateDistance < currentDistance
}

func wordAssignmentScore(assignment cellWordAssignment, cells []Cell) (float64, float64) {
	if assignment.CellIndex < 0 || assignment.CellIndex >= len(cells) || cells[assignment.CellIndex].Box == nil {
		return 0, math.Inf(1)
	}
	cellBox := *cells[assignment.CellIndex].Box
	score := assignment.Word.Box.IntersectionOverSelf(cellBox)
	wordCenterX := assignment.Word.Box.CenterX()
	wordCenterY := assignment.Word.Box.CenterY()
	distance := math.Abs(wordCenterX-cellBox.CenterX()) + math.Abs(wordCenterY-cellBox.CenterY())
	return score, distance
}

func (d Data) Grid() [][]Cell {
	grid := make([][]Cell, d.NumRows)
	for row := 0; row < d.NumRows; row++ {
		grid[row] = make([]Cell, d.NumCols)
		for col := 0; col < d.NumCols; col++ {
			grid[row][col] = Cell{
				RowSpan:  1,
				ColSpan:  1,
				StartRow: row,
				EndRow:   row + 1,
				StartCol: col,
				EndCol:   col + 1,
			}
		}
	}

	for _, cell := range d.Cells {
		cell = normaliseCellSpan(cell)
		for row := cell.StartRow; row < cell.EndRow && row < d.NumRows; row++ {
			if row < 0 {
				continue
			}
			for col := cell.StartCol; col < cell.EndCol && col < d.NumCols; col++ {
				if col < 0 {
					continue
				}
				grid[row][col] = cell
			}
		}
	}

	return grid
}

func normaliseCellSpan(cell Cell) Cell {
	if cell.RowSpan <= 0 {
		cell.RowSpan = 1
	}
	if cell.ColSpan <= 0 {
		cell.ColSpan = 1
	}
	if cell.EndRow <= cell.StartRow {
		cell.EndRow = cell.StartRow + cell.RowSpan
	}
	if cell.EndCol <= cell.StartCol {
		cell.EndCol = cell.StartCol + cell.ColSpan
	}
	return cell
}

func cloneCells(cells []Cell) []Cell {
	cloned := make([]Cell, len(cells))
	for index, cell := range cells {
		cloned[index] = cell
		if cell.Box != nil {
			cloned[index].Box = boxPtr(*cell.Box)
		}
	}
	return cloned
}

func filterContainedBoxes(boxes []geom.Box, containing geom.Box, threshold float64) []geom.Box {
	filtered := make([]geom.Box, 0, len(boxes))
	for _, box := range boxes {
		if box.IntersectionOverSelf(containing) >= threshold {
			filtered = append(filtered, box)
		}
	}
	return filtered
}

func dedupeBoxes(boxes []geom.Box, iouThreshold float64) []geom.Box {
	deduped := make([]geom.Box, 0, len(boxes))
	for _, candidate := range boxes {
		unique := true
		for _, kept := range deduped {
			if candidate.IoU(kept) >= iouThreshold {
				unique = false
				break
			}
		}
		if unique {
			deduped = append(deduped, candidate)
		}
	}
	return deduped
}

func computeCells(rows, cols, merges, rowHeaders, columnHeaders, rowSections []geom.Box) []Cell {
	rows = append([]geom.Box(nil), rows...)
	cols = append([]geom.Box(nil), cols...)

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CenterY() < rows[j].CenterY()
	})
	sort.SliceStable(cols, func(i, j int) bool {
		return cols[i].CenterX() < cols[j].CenterX()
	})

	cells := make([]Cell, 0, len(rows)*len(cols))
	covered := make(map[[2]int]bool)
	seenMerges := make(map[[4]int]bool)

	for _, merge := range merges {
		rowSpan, rowOK := spanFromMerge(merge, rows, "row", 0.5)
		colSpan, colOK := spanFromMerge(merge, cols, "col", 0.5)
		if !rowOK || !colOK {
			continue
		}

		startRow, endRowInclusive := rowSpan[0], rowSpan[1]
		startCol, endColInclusive := colSpan[0], colSpan[1]
		key := [4]int{startRow, endRowInclusive, startCol, endColInclusive}
		if seenMerges[key] {
			continue
		}
		seenMerges[key] = true

		cellBox := geom.Box{
			L:      cols[startCol].L,
			T:      rows[startRow].T,
			R:      cols[endColInclusive].R,
			B:      rows[endRowInclusive].B,
			Origin: rows[startRow].Origin,
		}
		columnHeader, rowHeader, rowSection := regionFlags(cellBox, rowHeaders, columnHeaders, rowSections)
		cells = append(cells, Cell{
			Box:          boxPtr(cellBox),
			RowSpan:      endRowInclusive - startRow + 1,
			ColSpan:      endColInclusive - startCol + 1,
			StartRow:     startRow,
			EndRow:       endRowInclusive + 1,
			StartCol:     startCol,
			EndCol:       endColInclusive + 1,
			ColumnHeader: columnHeader,
			RowHeader:    rowHeader,
			RowSection:   rowSection,
		})

		for row := startRow; row <= endRowInclusive; row++ {
			for col := startCol; col <= endColInclusive; col++ {
				covered[[2]int{row, col}] = true
			}
		}
	}

	for rowIdx, row := range rows {
		for colIdx, col := range cols {
			if covered[[2]int{rowIdx, colIdx}] {
				continue
			}

			intersection, ok := intersectionBox(row, col)
			if !ok {
				continue
			}
			columnHeader, rowHeader, rowSection := regionFlags(intersection, rowHeaders, columnHeaders, rowSections)
			cells = append(cells, Cell{
				Box:          boxPtr(intersection),
				RowSpan:      1,
				ColSpan:      1,
				StartRow:     rowIdx,
				EndRow:       rowIdx + 1,
				StartCol:     colIdx,
				EndCol:       colIdx + 1,
				ColumnHeader: columnHeader,
				RowHeader:    rowHeader,
				RowSection:   rowSection,
			})
		}
	}

	return cells
}

func spanFromMerge(merge geom.Box, lines []geom.Box, axis string, threshold float64) ([2]int, bool) {
	indexes := make([]int, 0, len(lines))
	bestIndex := -1
	bestOverlap := 0.0

	for index, line := range lines {
		intersection, ok := intersectionBox(merge, line)
		if !ok {
			continue
		}

		var overlapLength, baseLength float64
		switch axis {
		case "row":
			overlapLength = intersection.Height()
			baseLength = math.Max(1e-9, line.Height())
		default:
			overlapLength = intersection.Width()
			baseLength = math.Max(1e-9, line.Width())
		}

		if overlapLength/baseLength >= threshold {
			indexes = append(indexes, index)
		}
		if overlapLength > bestOverlap {
			bestOverlap = overlapLength
			bestIndex = index
		}
	}

	if len(indexes) > 0 {
		return [2]int{slices.Min(indexes), slices.Max(indexes)}, true
	}
	if bestIndex >= 0 && bestOverlap > 0 {
		return [2]int{bestIndex, bestIndex}, true
	}
	return [2]int{}, false
}

func regionFlags(box geom.Box, rowHeaders, columnHeaders, rowSections []geom.Box) (bool, bool, bool) {
	columnHeader := false
	rowHeader := false
	rowSection := false

	for _, header := range columnHeaders {
		if box.IntersectionOverSelf(header) >= 0.5 {
			columnHeader = true
			break
		}
	}
	for _, header := range rowHeaders {
		if box.IntersectionOverSelf(header) >= 0.5 {
			rowHeader = true
			break
		}
	}
	for _, section := range rowSections {
		if box.IntersectionOverSelf(section) >= 0.5 {
			rowSection = true
			break
		}
	}

	return columnHeader, rowHeader, rowSection
}

func intersectionBox(a, b geom.Box) (geom.Box, bool) {
	left := math.Max(a.L, b.L)
	right := math.Min(a.R, b.R)
	if right <= left {
		return geom.Box{}, false
	}

	top := math.Max(math.Min(a.T, a.B), math.Min(b.T, b.B))
	bottom := math.Min(math.Max(a.T, a.B), math.Max(b.T, b.B))
	if bottom <= top {
		return geom.Box{}, false
	}

	if a.Origin == geom.BottomLeft {
		return geom.Box{L: left, T: bottom, R: right, B: top, Origin: a.Origin}, true
	}
	return geom.Box{L: left, T: top, R: right, B: bottom, Origin: a.Origin}, true
}

func boxPtr(box geom.Box) *geom.Box {
	boxCopy := box
	return &boxCopy
}
