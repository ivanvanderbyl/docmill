package pdf

import (
	"math"
	"slices"
	"strings"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/render"
	doctable "github.com/ivanvanderbyl/docmill/pkg/table"
)

// Cross-page table stitching: re-joins a table split across a page break into
// one logical table.
//
// A table that runs off the bottom of page N and resumes at the top of page
// N+1 is a single logical table split by the page break. We re-join such pairs
// into one table on page N and drop the continuation block on page N+1. The
// pass is purely geometric: it gates on page adjacency (bottom region of page N,
// top region of page N+1), equal column count with aligned column boundaries,
// and the absence of any intervening non-table content. The only text signal is
// a structural de-duplication: if the continuation repeats page N's header row,
// the document genuinely restated the header and the two tables are kept
// separate (the conservative behaviour).
//
// When no adjacent pair qualifies the function is a no-op, leaving every page's
// blocks byte-identical to single-page output.

const (
	// A table is in the bottom (resp. top) region of its page when its bottom
	// edge sits within this fraction of the page height from the page bottom
	// (resp. its top edge from the page top).
	pageEdgeRegionFraction = 0.15
	// Column-boundary alignment tolerance as a fraction of the table width.
	columnAlignTolerance = 0.05
)

// connectCrossPageTables merges genuine cross-page table continuations in place.
// pageBlocks is indexed by page in reading order; sizes carries each page's
// geometry.
//
// anchor tracks the page whose LAST table block is currently open for
// continuation (its table ends in the page-bottom region with nothing after
// it). Each subsequent page's FIRST table block is tested against that anchor
// table; on a successful merge the rows fold into the anchor page and the
// continuation block is removed from the lower page, so a table spanning 3+
// pages collapses transitively onto the first page. When a page break out of
// the bottom region or intervening content breaks the chain, the anchor resets
// to the lower page's own trailing table (if any).
func connectCrossPageTables(pageBlocks [][]markdownBlock, sizes []geom.Size) {
	if len(pageBlocks) < 2 {
		return
	}
	anchor := -1
	for page := range pageBlocks {
		if anchor >= 0 {
			if merged, ok := mergeAdjacentPageTables(pageBlocks[anchor], pageBlocks[page], sizeAt(sizes, anchor), sizeAt(sizes, page)); ok {
				pageBlocks[anchor] = merged.upper
				pageBlocks[page] = merged.lower
				// Keep the same anchor: the grown table may continue again on the
				// next page. The anchor is only valid to extend if it still ends
				// in the page-bottom region with nothing after it.
				if !anchorTableStillOpen(pageBlocks[anchor], sizeAt(sizes, anchor)) {
					anchor = -1
				}
				continue
			}
		}
		// No merge: this page's own trailing table (if any, bottom-region with
		// nothing after it) becomes the next continuation anchor.
		if anchorTableStillOpen(pageBlocks[page], sizeAt(sizes, page)) {
			anchor = page
		} else {
			anchor = -1
		}
	}
}

func sizeAt(sizes []geom.Size, index int) geom.Size {
	if index >= 0 && index < len(sizes) {
		return sizes[index]
	}
	return geom.Size{}
}

// anchorTableStillOpen reports whether the page's last table block is a viable
// continuation anchor: it exists, sits in the page-bottom region, and has no
// non-table content after it.
func anchorTableStillOpen(blocks []markdownBlock, size geom.Size) bool {
	idx := lastTableBlockIndex(blocks)
	if idx < 0 || blocks[idx].tableData == nil {
		return false
	}
	if hasContentBlockAfter(blocks, idx) {
		return false
	}
	return tableInBottomRegion(blocks[idx].tableBox, size)
}

type mergedPagePair struct {
	upper []markdownBlock
	lower []markdownBlock
}

func mergeAdjacentPageTables(upper, lower []markdownBlock, upperSize, lowerSize geom.Size) (mergedPagePair, bool) {
	upperIdx := lastTableBlockIndex(upper)
	lowerIdx := firstTableBlockIndex(lower)
	if upperIdx < 0 || lowerIdx < 0 {
		return mergedPagePair{}, false
	}

	upperBlock := upper[upperIdx]
	lowerBlock := lower[lowerIdx]
	if upperBlock.tableData == nil || lowerBlock.tableData == nil {
		return mergedPagePair{}, false
	}

	if !tableInBottomRegion(upperBlock.tableBox, upperSize) {
		return mergedPagePair{}, false
	}
	if !tableInTopRegion(lowerBlock.tableBox, lowerSize) {
		return mergedPagePair{}, false
	}
	// No intervening non-table, non-empty content around the page break.
	if hasContentBlockAfter(upper, upperIdx) || hasContentBlockBefore(lower, lowerIdx) {
		return mergedPagePair{}, false
	}
	if !columnsAlign(*upperBlock.tableData, *lowerBlock.tableData) {
		return mergedPagePair{}, false
	}
	// A repeated header means the continuation restated the header: keep
	// the tables separate (conservative).
	if continuationRepeatsHeader(*upperBlock.tableData, *lowerBlock.tableData) {
		return mergedPagePair{}, false
	}

	mergedData := appendTableRows(*upperBlock.tableData, *lowerBlock.tableData)
	rendered, err := render.Table(mergedData)
	if err != nil {
		return mergedPagePair{}, false
	}

	newUpper := append([]markdownBlock(nil), upper...)
	merged := newUpper[upperIdx]
	merged.Text = strings.TrimSpace(rendered)
	merged.tableData = &mergedData
	// The merged table is rendered on the upper page, so it keeps the upper
	// table's box for any further adjacency tests against the next page.
	merged.tableBox = upperBlock.tableBox
	newUpper[upperIdx] = merged

	newLower := make([]markdownBlock, 0, len(lower)-1)
	newLower = append(newLower, lower[:lowerIdx]...)
	newLower = append(newLower, lower[lowerIdx+1:]...)

	return mergedPagePair{upper: newUpper, lower: newLower}, true
}

func lastTableBlockIndex(blocks []markdownBlock) int {
	for i, block := range slices.Backward(blocks) {
		if block.tableData != nil {
			return i
		}
	}
	return -1
}

func firstTableBlockIndex(blocks []markdownBlock) int {
	for i := range blocks {
		if blocks[i].tableData != nil {
			return i
		}
	}
	return -1
}

// hasContentBlockAfter reports whether any non-table, non-empty block follows
// the table at idx (in reading order) on the same page.
func hasContentBlockAfter(blocks []markdownBlock, idx int) bool {
	for i := idx + 1; i < len(blocks); i++ {
		if blocks[i].tableData == nil && strings.TrimSpace(blocks[i].Text) != "" {
			return true
		}
	}
	return false
}

// hasContentBlockBefore reports whether any non-table, non-empty block precedes
// the table at idx (in reading order) on the same page.
func hasContentBlockBefore(blocks []markdownBlock, idx int) bool {
	for i := range idx {
		if blocks[i].tableData == nil && strings.TrimSpace(blocks[i].Text) != "" {
			return true
		}
	}
	return false
}

func tableInBottomRegion(box geom.Box, size geom.Size) bool {
	if size.Height <= 0 {
		return false
	}
	_, bottom := verticalBounds(box)
	margin := size.Height * pageEdgeRegionFraction
	return bottom >= size.Height-margin
}

func tableInTopRegion(box geom.Box, size geom.Size) bool {
	if size.Height <= 0 {
		return false
	}
	top, _ := verticalBounds(box)
	margin := size.Height * pageEdgeRegionFraction
	return top <= margin
}

// columnsAlign reports whether two tables share the same column count and have
// column boundaries that line up once normalised to each table's own width.
// Column-left positions are derived from per-column cell boxes when available
// and otherwise fall back to even division of the table span, so the check is
// well defined for every table builder.
func columnsAlign(upper, lower doctable.Data) bool {
	if upper.NumCols != lower.NumCols || upper.NumCols == 0 {
		return false
	}
	upperEdges := normalisedColumnEdges(upper)
	lowerEdges := normalisedColumnEdges(lower)
	if len(upperEdges) != len(lowerEdges) {
		return false
	}
	for i := range upperEdges {
		if math.Abs(upperEdges[i]-lowerEdges[i]) > columnAlignTolerance {
			return false
		}
	}
	return true
}

// normalisedColumnEdges returns each column's left edge normalised to [0,1]
// within the table's horizontal span. When cell boxes are present the left edge
// of each column is taken from the minimum cell L in that column; otherwise the
// columns are assumed evenly spaced, which still aligns two tables that share
// NumCols and a common physical layout.
func normalisedColumnEdges(data doctable.Data) []float64 {
	edges := make([]float64, data.NumCols)
	grid := data.Grid()
	tableL, tableR, haveSpan := tableHorizontalSpan(grid, data.NumCols)
	width := tableR - tableL
	for col := 0; col < data.NumCols; col++ {
		left, ok := columnLeft(grid, col)
		if haveSpan && width > 0 && ok {
			edges[col] = (left - tableL) / width
			continue
		}
		// Even-division fallback when boxes are unavailable.
		edges[col] = float64(col) / float64(data.NumCols)
	}
	return edges
}

func columnLeft(grid [][]doctable.Cell, col int) (float64, bool) {
	left := 0.0
	found := false
	for _, row := range grid {
		if col >= len(row) || row[col].Box == nil {
			continue
		}
		l := row[col].Box.L
		if !found || l < left {
			left = l
			found = true
		}
	}
	return left, found
}

func tableHorizontalSpan(grid [][]doctable.Cell, numCols int) (float64, float64, bool) {
	left := 0.0
	right := 0.0
	found := false
	for _, row := range grid {
		for col := 0; col < numCols && col < len(row); col++ {
			if row[col].Box == nil {
				continue
			}
			l := row[col].Box.L
			r := row[col].Box.R
			if !found {
				left, right = l, r
				found = true
				continue
			}
			if l < left {
				left = l
			}
			if r > right {
				right = r
			}
		}
	}
	return left, right, found
}

// continuationRepeatsHeader reports whether the continuation table's first row
// equals the upper table's header row (cell-for-cell after whitespace
// normalisation). A repeated header is a restated header, not a continuation.
func continuationRepeatsHeader(upper, lower doctable.Data) bool {
	if upper.NumRows == 0 || lower.NumRows == 0 {
		return false
	}
	upperGrid := upper.Grid()
	lowerGrid := lower.Grid()
	cols := min(lower.NumCols, upper.NumCols)
	if cols == 0 {
		return false
	}
	for col := range cols {
		if normaliseHeaderText(upperGrid[0][col].Text) != normaliseHeaderText(lowerGrid[0][col].Text) {
			return false
		}
	}
	return true
}

func normaliseHeaderText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// appendTableRows builds a merged Data whose rows are all of the upper
// table's rows followed by all of the lower table's rows, preserving the upper
// table's column count. The caller only reaches here when the lower table does
// NOT repeat the upper header (continuationRepeatsHeader is false), so the
// lower table's first row is genuine continuation data and must be kept; the
// "do not duplicate the header" requirement is satisfied by that gate rather
// than by blindly dropping the lower row 0.
func appendTableRows(upper, lower doctable.Data) doctable.Data {
	upperGrid := upper.Grid()
	lowerGrid := lower.Grid()
	cols := upper.NumCols

	rows := make([][]string, 0, upper.NumRows+lower.NumRows)
	for r := 0; r < upper.NumRows; r++ {
		rows = append(rows, gridRowText(upperGrid[r], cols))
	}
	for r := 0; r < lower.NumRows; r++ {
		rows = append(rows, gridRowText(lowerGrid[r], cols))
	}
	return doctable.FromGrid(rows)
}

func gridRowText(row []doctable.Cell, cols int) []string {
	out := make([]string, cols)
	for col := range cols {
		if col < len(row) {
			out[col] = row[col].Text
		}
	}
	return out
}
