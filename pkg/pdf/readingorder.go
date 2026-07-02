package pdf

import (
	"math"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
)

// Tuning constants for the confidence-gated column detector. They are
// deliberately conservative: the detector only reorders when it finds a clear,
// page-tall gutter, otherwise it falls back to identity order so single-column
// documents are never disturbed.
const (
	// minCellsForDetection is the floor below which we never attempt detection.
	minCellsForDetection = 8
	// gridColumns is the number of x-bins spanning [0, pageWidth].
	gridColumns = 100
	// gridRows is the number of y-bins spanning the cells' vertical extent.
	gridRows = 50
	// minGutterFraction is the minimum gutter run width as a fraction of the page
	// width before it is treated as a real column separator.
	minGutterFraction = 0.015
	// bannerWidthFraction is the cell-width fraction above which a cell is treated
	// as a full-width banner (e.g. a title spanning all columns).
	bannerWidthFraction = 0.66
	// minColumnRowFraction is the minimum fraction of occupied rows that EVERY
	// resulting column must cover for the detection to be accepted. Real text
	// columns run most of the page height; sparse figure-label bands or thin
	// table strips do not, so this rejects them and falls back to identity.
	minColumnRowFraction = 0.5
	// minCellsForColumns is the minimum non-banner cell count required before a
	// multi-column split is accepted. Sparse figure pages (few scattered labels)
	// fall below this and stay in identity order. Relaxed from 60 once the
	// relative-dip gutter test (relGutterFraction) made detection reliable on
	// dense two-column reference/body pages: at ~55+ cells a split averages ~27+
	// cells per column (a substantial text column), which admits genuine
	// two-column pages while the sparser figure-caption layouts that read as one
	// flow stay below it (measured no-regression on DPBench).
	minCellsForColumns = 55
	// minCellsForRaggedColumn is the minimum cell count required for a column
	// whose vertical coverage is below minColumnRowFraction. Bibliographies and
	// references often have a final column that ends early but is still a real
	// reading-order column.
	minCellsForRaggedColumn = 24
	// minRaggedColumnRowFraction is the lower vertical-coverage bound accepted
	// for dense ragged columns. Below this, the band is more likely to be a
	// sidebar, figure label cluster, or table fragment than a reading column.
	minRaggedColumnRowFraction = 0.25
	// relGutterFraction: an x-bin counts as a gutter when its occupied-row
	// coverage is at most this fraction of the page's peak column coverage. A
	// real gutter is a deep DIP relative to the column density, not necessarily
	// fully empty — a few citations whose box clips the gutter (or a running
	// head) must not hide it. Relative to peak coverage, so document-general
	// (this replaces the absolute gutterEmptyFraction emptiness test, which
	// rejected clear two-column reference pages where ~20% of rows cross the
	// gutter). Mirrors the "deep valley with text shoulders" separator test
	// (Breuel whitespace cover; docling/pymupdf4llm both separate columns by a
	// relative, not absolute, criterion).
	relGutterFraction = 0.30
)

// orderCells (the reading-order pass) now lives in readingorder_graph.go as a
// directional-neighbour spatial-graph reading-order algorithm.
//
// partitionColumns below retains the geometric column-gutter detector for the
// assembleByColumn line-assembly seam: it is the column-BOUNDARY source the
// directional graph cannot replace at cell granularity. The graph's connected
// components fragment dense multi-cell lines into one component per x-position
// and separate sparse columns, so the detector's confidence gates (which keep
// dense single-column rows in one group and decline sparse/ambiguous splits)
// remain the correct, false-positive-guarded partition. orderCells reassigns
// Index in graph reading order;
// partitionColumns only controls which cells may merge into a shared visual
// line, and the final block order follows orderCells' Index via each block's
// MinIndex.

// partitionColumns groups cells into reading-order column groups (optional
// leading banner group, then one per text column) WITHOUT reassigning Index, so
// callers can assemble each column independently while preserving the global
// reading-order indices set earlier by orderCells. When the page is not
// confidently multi-column it returns a single visual top-to-bottom group.
func partitionColumns(cells []page.TextCell, size geom.Size) [][]page.TextCell {
	return columnGroups(cells, size, false)
}

// columnGroups partitions the cells into ordered reading-order groups: an
// optional leading banner group (full-width cells, top-to-bottom) followed by
// one group per detected text column (left-to-right). Within each group cells
// are sorted top-to-bottom then left-to-right, and Index is reassigned globally
// across all groups in emission order so downstream block sorting reads
// column-by-column.
//
// When the detector is not confident the page is multi-column it returns a
// single visual top-to-bottom group. The input slice is never mutated. When
// reindex is true each cell's Index is reassigned globally across groups in
// emission order; otherwise indices are preserved.
func columnGroups(cells []page.TextCell, size geom.Size, reindex bool) [][]page.TextCell {
	singleColumn := func() [][]page.TextCell {
		group := sortReadingGroup(cells)
		if reindex {
			for i := range group {
				group[i].Index = i
			}
		}
		return [][]page.TextCell{group}
	}

	if len(cells) < minCellsForDetection || size.Width <= 0 {
		return singleColumn()
	}

	boundaries := detectColumnBoundaries(cells, size.Width)
	// 0 interior boundaries means single-column or low confidence.
	if len(boundaries) < 1 {
		return singleColumn()
	}

	// Column bands are [edge, b0], [b0, b1], ..., [bk, edge].
	bands := make([]float64, 0, len(boundaries)+2)
	bands = append(bands, 0)
	bands = append(bands, boundaries...)
	bands = append(bands, size.Width)
	columnCount := len(bands) - 1

	if splitsTightSameLineFragments(cells, bands) {
		return singleColumn()
	}

	bannerWidth := bannerWidthFraction * size.Width

	var banner []page.TextCell
	columns := make([][]page.TextCell, columnCount)

	for _, cell := range cells {
		// Full-width banner cells (titles spanning multiple columns) form a
		// leading group ordered purely by their top position.
		if cell.Box.Width() > bannerWidth {
			banner = append(banner, cell)
			continue
		}
		center := (cell.Box.L + cell.Box.R) / 2
		rank := columnRankFor(center, bands)
		columns[rank] = append(columns[rank], cell)
	}

	// Require enough non-banner cells distributed across the columns before
	// committing to a multi-column split, and require at least two non-empty
	// columns. Sparse figure pages fall below this and stay in identity order.
	//
	// NOTE: a multi-credit confidence score (cell support + column balance) was
	// implemented and swept here in place of this cell-count cliff; it produced no
	// clean win. Balance cannot separate a RAGGED real two-column bibliography
	// (low balance, must split) from a LOPSIDED false split (low balance, must
	// not), whereas the cell count already does (the real bibliography clears 55;
	// the lopsided figure-caption band does not). This is a heuristic that is
	// better as a boolean predicate than a score.
	columnCells := 0
	nonEmptyColumns := 0
	for _, column := range columns {
		columnCells += len(column)
		if len(column) > 0 {
			nonEmptyColumns++
		}
	}
	if columnCells < minCellsForColumns || nonEmptyColumns < 2 {
		return singleColumn()
	}

	groups := make([][]page.TextCell, 0, columnCount+1)
	if len(banner) > 0 {
		groups = append(groups, sortReadingGroup(banner))
	}
	for _, column := range columns {
		if len(column) == 0 {
			continue
		}
		groups = append(groups, sortReadingGroup(column))
	}

	if len(groups) == 0 {
		return singleColumn()
	}

	// Reassign Index globally in emission order so block sorting respects the
	// column reading order.
	if reindex {
		next := 0
		for _, group := range groups {
			for i := range group {
				group[i].Index = next
				next++
			}
		}
	}

	return groups
}

func splitsTightSameLineFragments(cells []page.TextCell, bands []float64) bool {
	lines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)
	for _, line := range lines {
		if len(line.Cells) < 2 {
			continue
		}
		visible := make([]page.TextCell, 0, len(line.Cells))
		for _, cell := range line.Cells {
			text := strings.TrimSpace(cell.Text)
			if text == "" || isListSpacerText(text) {
				continue
			}
			visible = append(visible, cell)
		}
		for i := 0; i+1 < len(visible); i++ {
			left := visible[i]
			right := visible[i+1]
			leftRank := columnRankFor((left.Box.L+left.Box.R)*0.5, bands)
			rightRank := columnRankFor((right.Box.L+right.Box.R)*0.5, bands)
			if leftRank == rightRank {
				continue
			}
			if ok, _ := geometricListMarker([]page.TextCell{left, right}); ok {
				return true
			}
			gap := right.Box.L - left.Box.R
			if gap < 0 {
				gap = 0
			}
			ref := math.Max(left.FontSize, right.FontSize)
			if ref <= 0 {
				ref = math.Max(left.Box.Height(), right.Box.Height())
			}
			if gap <= math.Max(2, ref*0.45) {
				return true
			}
		}
	}
	return false
}

// sortReadingGroup returns a new slice of the cells sorted top-to-bottom then
// left-to-right. The input slice is not mutated by callers because each group is
// already a freshly appended slice of copies.
func sortReadingGroup(cells []page.TextCell) []page.TextCell {
	out := append([]page.TextCell(nil), cells...)
	sort.SliceStable(out, func(i, j int) bool {
		ti := math.Min(out[i].Box.T, out[i].Box.B)
		tj := math.Min(out[j].Box.T, out[j].Box.B)
		if ti != tj {
			return ti < tj
		}
		return out[i].Box.L < out[j].Box.L
	})
	return out
}

// columnRankFor returns the index of the band whose [lo, hi) range contains x.
// bands is sorted ascending; the last band is treated as inclusive of the right
// edge.
func columnRankFor(x float64, bands []float64) int {
	for i := 0; i+1 < len(bands); i++ {
		hi := bands[i+1]
		if i+2 == len(bands) {
			// Last band: include the right edge.
			if x >= bands[i] && x <= hi {
				return i
			}
		} else if x >= bands[i] && x < hi {
			return i
		}
	}
	// x outside [0, width]; clamp to nearest edge band.
	if x < bands[0] {
		return 0
	}
	return len(bands) - 2
}

// detectColumnBoundaries builds a coverage grid over the cells and returns the
// centre x of each interior, page-tall gutter run (a column separator). An empty
// result means the page is single-column or detection was not confident.
func detectColumnBoundaries(cells []page.TextCell, pageWidth float64) []float64 {
	// Vertical extent of the cells (top-left origin: T < B).
	minY := math.Inf(1)
	maxY := math.Inf(-1)
	for _, cell := range cells {
		top := math.Min(cell.Box.T, cell.Box.B)
		bottom := math.Max(cell.Box.T, cell.Box.B)
		if top < minY {
			minY = top
		}
		if bottom > maxY {
			maxY = bottom
		}
	}
	if !(maxY > minY) || pageWidth <= 0 {
		return nil
	}

	xBinWidth := pageWidth / gridColumns
	yBinHeight := (maxY - minY) / gridRows
	if xBinWidth <= 0 || yBinHeight <= 0 {
		return nil
	}

	// covered[x][y] true when any cell overlaps that (xbin, ybin) cell.
	covered := make([][]bool, gridColumns)
	for i := range covered {
		covered[i] = make([]bool, gridRows)
	}
	// rowHasCell[y] true when any cell occupies that y-bin anywhere across x.
	rowHasCell := make([]bool, gridRows)

	for _, cell := range cells {
		left := math.Max(0, cell.Box.L)
		right := math.Min(pageWidth, cell.Box.R)
		if right <= left {
			continue
		}
		top := math.Min(cell.Box.T, cell.Box.B)
		bottom := math.Max(cell.Box.T, cell.Box.B)

		x0 := clampBin(int(math.Floor(left/xBinWidth)), gridColumns)
		x1 := clampBin(int(math.Floor((right-1e-9)/xBinWidth)), gridColumns)
		y0 := clampBin(int(math.Floor((top-minY)/yBinHeight)), gridRows)
		y1 := clampBin(int(math.Floor((bottom-1e-9-minY)/yBinHeight)), gridRows)

		for y := y0; y <= y1; y++ {
			rowHasCell[y] = true
			for x := x0; x <= x1; x++ {
				covered[x][y] = true
			}
		}
	}

	occupiedRows := 0
	for _, has := range rowHasCell {
		if has {
			occupiedRows++
		}
	}
	if occupiedRows == 0 {
		return nil
	}

	// colCoverage[x] = number of occupied rows in which some cell overlaps x-bin
	// x; peakCoverage is the densest column bin. A gutter bin is a deep relative
	// DIP (<= relGutterFraction of peak), not necessarily empty — so a clear
	// gutter survives the handful of rows whose cells clip across it.
	colCoverage := make([]int, gridColumns)
	peakCoverage := 0
	for x := range gridColumns {
		c := 0
		for y := range gridRows {
			if rowHasCell[y] && covered[x][y] {
				c++
			}
		}
		colCoverage[x] = c
		if c > peakCoverage {
			peakCoverage = c
		}
	}
	if peakCoverage == 0 {
		return nil
	}
	gutterCeil := relGutterFraction * float64(peakCoverage)
	gutter := make([]bool, gridColumns)
	for x := range gridColumns {
		if float64(colCoverage[x]) <= gutterCeil {
			gutter[x] = true
		}
	}

	// Find maximal interior gutter runs at least minGutterBins wide. Record the
	// gutter run [start, end) bin ranges so we can derive the column bands.
	minGutterBins := max(int(math.Ceil(minGutterFraction*float64(gridColumns))), 1)

	type run struct{ start, end int }
	var runs []run
	var boundaries []float64
	x := 0
	for x < gridColumns {
		if !gutter[x] {
			x++
			continue
		}
		start := x
		for x < gridColumns && gutter[x] {
			x++
		}
		end := x // exclusive
		// Interior: must not touch the left or right page margin.
		if start == 0 || end == gridColumns {
			continue
		}
		if end-start < minGutterBins {
			continue
		}
		// Both flanks must carry a real text column (deep valley with text
		// shoulders), else this dip is a one-sided margin/caption band rather
		// than a column separator. The bar matches the ragged-column tolerance
		// (a short second column — common in bibliographies — must still count),
		// so it is relative to occupied rows, not peak coverage. start>0 and
		// end<gridColumns hold here (the interior check above rejects
		// margin-touching runs).
		shoulderMin := minRaggedColumnRowFraction * float64(occupiedRows)
		if float64(colCoverage[start-1]) < shoulderMin || float64(colCoverage[end]) < shoulderMin {
			continue
		}
		runs = append(runs, run{start: start, end: end})
		centreBin := float64(start+end) / 2
		boundaries = append(boundaries, centreBin*xBinWidth)
	}

	if len(boundaries) == 0 {
		return nil
	}

	// Confidence gates: every resulting column band must be densely filled
	// (cover at least minColumnRowFraction of the occupied rows). Real text
	// columns run most of the page height; sparse figure-label bands or thin
	// table strips do not. If any column is too sparse, reject the whole
	// detection and fall back to identity. Column bands are the bin spans
	// between gutter runs (and the page edges).
	colBins := make([][2]int, 0, len(runs)+1)
	prevEnd := 0
	for _, r := range runs {
		colBins = append(colBins, [2]int{prevEnd, r.start})
		prevEnd = r.end
	}
	colBins = append(colBins, [2]int{prevEnd, gridColumns})

	for _, span := range colBins {
		lo, hi := span[0], span[1]
		if hi <= lo {
			return nil
		}
		// Rows covered anywhere within this column band.
		colRows := 0
		for y := range gridRows {
			if !rowHasCell[y] {
				continue
			}
			for xb := lo; xb < hi; xb++ {
				if covered[xb][y] {
					colRows++
					break
				}
			}
		}
		colRowFraction := float64(colRows) / float64(occupiedRows)
		if colRowFraction >= minColumnRowFraction {
			continue
		}
		if colRowFraction < minRaggedColumnRowFraction || cellsInColumnBinSpan(cells, pageWidth, lo, hi) < minCellsForRaggedColumn {
			return nil
		}
	}

	return boundaries
}

func cellsInColumnBinSpan(cells []page.TextCell, pageWidth float64, lo, hi int) int {
	if pageWidth <= 0 || hi <= lo {
		return 0
	}
	xBinWidth := pageWidth / gridColumns
	if xBinWidth <= 0 {
		return 0
	}
	count := 0
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		left := math.Max(0, cell.Box.L)
		right := math.Min(pageWidth, cell.Box.R)
		if right <= left {
			continue
		}
		centreBin := clampBin(int(math.Floor(((left+right)/2)/xBinWidth)), gridColumns)
		if centreBin >= lo && centreBin < hi {
			count++
		}
	}
	return count
}

func clampBin(v, n int) int {
	if v < 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}
