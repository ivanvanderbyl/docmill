package table

import (
	"math"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/textline"
)

// mergeRowsNW is a Needleman-Wunsch / dynamic-programming generalisation of
// mergeFirstColumnContinuationRows. It aligns the ordered text-row sequence to
// an unknown-length logical-row sequence where each logical row consumes one
// ANCHOR text row plus zero or more CONTINUATION text rows. A continuation is a
// text row that carries no fragment in the table's anchor (left-most) column
// band; its fragments belong to cells started by the anchor row above. The
// optimal segmentation minimises a geometric incoherence cost recovered by
// backtracking (a Needleman-Wunsch-style alignment).
//
// It is intentionally conservative: any run may only span rows that are
// genuinely mergeable (no second anchor-band cell appears after the first row,
// vertical gaps stay within the run's own adaptive leading, and no two
// fragments collide in the same column). The instant a row carries an
// anchor-band cell it is a hard boundary that forces a new logical row, so the
// boundary structure matches the greedy merger on the cases it already handled.
// mergeRowsNW differs only by additionally absorbing genuine multi-line
// continuation fragments — in ANY column, not only a lone first-column wrap —
// that the greedy code left as separate rows.
func mergeRowsNW(rows []textline.ParagraphTextLine, options DetectionOptions) []logicalTableRow {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 {
		return []logicalTableRow{newLogicalTableRow(rows[0])}
	}

	band := anchorColumnBand(rows, options)

	// cost[j] = minimum cost to segment rows[0:j] into logical rows.
	// from[j]  = the run start i that achieves cost[j] (rows[i:j] form one logical row).
	const inf = math.MaxFloat64
	cost := make([]float64, len(rows)+1)
	from := make([]int, len(rows)+1)
	for j := 1; j <= len(rows); j++ {
		cost[j] = inf
		from[j] = -1
	}
	cost[0] = 0

	for j := 1; j <= len(rows); j++ {
		for i := j - 1; i >= 0; i-- {
			if cost[i] == inf {
				continue
			}
			runCost, ok := logicalRunCost(rows[i:j], band, options)
			if !ok {
				// rows[i:j] cannot form one logical row. We must NOT stop here:
				// run validity is not monotone in the run start. Decreasing i
				// prepends a row that becomes the new anchor, which changes the
				// run's adaptive leading and every isFirstColumnContinuationRow
				// check, so a longer run rows[i-1:j] can be valid even when
				// rows[i:j] is not. Keep scanning. logicalRunCost is O(run) and
				// the row counts are tiny, so the full O(n^2) DP is cheap and —
				// unlike the pruned variant — provably finds the minimum-cost
				// (coarsest valid) segmentation, which is what the conservatism
				// guarantee against under-merging requires.
				continue
			}
			candidate := cost[i] + runCost
			if candidate < cost[j] {
				cost[j] = candidate
				from[j] = i
			}
		}
	}

	// Backtrack the segmentation.
	bounds := make([]int, 0)
	for j := len(rows); j > 0; j = from[j] {
		bounds = append(bounds, from[j])
	}
	sort.Ints(bounds)

	merged := make([]logicalTableRow, 0, len(bounds))
	for idx, start := range bounds {
		end := len(rows)
		if idx+1 < len(bounds) {
			end = bounds[idx+1]
		}
		merged = append(merged, buildLogicalRunRow(rows[start:end]))
	}
	return merged
}

// anchorColumnBand returns the centre of the table's left-most column, estimated
// robustly from the L-edges of the left-most cell of each well-formed row (>=
// MinCols cells). The most populous cluster of those L-edges defines the band;
// its half-width is the column tolerance.
func anchorColumnBand(rows []textline.ParagraphTextLine, options DetectionOptions) (center float64) {
	tolerance := anchorBandTolerance(options)
	edges := make([]float64, 0, len(rows))
	for _, row := range rows {
		if len(row.Cells) < options.MinCols {
			continue
		}
		edges = append(edges, leftMostEdge(row.Cells))
	}
	if len(edges) == 0 {
		// No well-formed rows: fall back to the global left-most edge.
		for _, row := range rows {
			if len(row.Cells) == 0 {
				continue
			}
			e := leftMostEdge(row.Cells)
			if len(edges) == 0 || e < edges[0] {
				edges = []float64{e}
			}
		}
		if len(edges) == 0 {
			return 0
		}
		return edges[0]
	}
	sort.Float64s(edges)

	// Sweep the sorted edges and keep the densest tolerance-wide window.
	bestStart, bestCount := 0, 0
	for s := 0; s < len(edges); s++ {
		count := 0
		for e := s; e < len(edges) && edges[e]-edges[s] <= tolerance; e++ {
			count++
		}
		if count > bestCount {
			bestCount = count
			bestStart = s
		}
	}
	sum, n := 0.0, 0
	for e := bestStart; e < len(edges) && edges[e]-edges[bestStart] <= tolerance; e++ {
		sum += edges[e]
		n++
	}
	if n == 0 {
		return edges[0]
	}
	return sum / float64(n)
}

func anchorBandTolerance(options DetectionOptions) float64 {
	if options.ColumnTolerance > 8 {
		return options.ColumnTolerance
	}
	return 8
}

// logicalRunCost reports the incoherence cost of merging the consecutive text
// rows run[0:] into one logical row, and whether the merge is valid. The first
// row is the anchor; every later row must be a continuation (a lone first-column
// wrap, or a single wrapped cell within the run's adaptive leading that collapses
// to exactly one existing column). Any boundary row — a later well-formed row or
// one carrying its own anchor-band cell — makes the run invalid.
func logicalRunCost(run []textline.ParagraphTextLine, band float64, options DetectionOptions) (float64, bool) {
	if len(run) == 0 {
		return 0, false
	}
	tolerance := anchorBandTolerance(options)
	// base dominates the incoherence terms so the DP minimises the number of
	// logical rows (merge maximally among valid runs), matching the greedy
	// merger's maximal-merge behaviour. The incoherence terms only break ties.
	const base = 1000.0
	if len(run) == 1 {
		return base, true
	}

	// Adaptive leading: the median of the per-step centre gaps within the run.
	// Genuine multi-line cells keep a small, regular leading; an irregular but
	// still single-cell wrap is judged relative to this median, not a global
	// threshold. This is the irregular-leading robustness.
	gaps := make([]float64, 0, len(run)-1)
	for k := 1; k < len(run); k++ {
		gaps = append(gaps, math.Abs(run[k].Center-run[k-1].Center))
	}
	median := medianFloat64(gaps)
	maxLeading := median * 2.5
	if min := minLeading(run); maxLeading < min {
		maxLeading = min
	}

	// Track the cells accumulated so far so column conflicts can be detected.
	cur := newLogicalTableRow(run[0])
	incoherence := 0.0
	for k := 1; k < len(run); k++ {
		row := run[k]
		gap := math.Abs(row.Center - run[k-1].Center)
		// The greedy lone-first-column wrap is always allowed exactly as the
		// greedy merger handled it (preserves greedy behaviour and boundary
		// structure). Such a wrap legitimately owns an anchor-band cell.
		greedy := isFirstColumnContinuationRow(row, cur.Row, options)
		if !greedy {
			// A well-formed row (>= MinCols cells) or a row that owns its own
			// anchor-band cell starts a new logical row: hard boundary.
			if len(row.Cells) >= options.MinCols || rowHasAnchorBandCell(row, band, tolerance) {
				return 0, false
			}
			// Otherwise it may be a single wrapped cell in any column, judged
			// against the run's adaptive leading. A row whose fragments span
			// more than one column is a structural row with a blank first
			// column, not a wrap: boundary.
			if gap > maxLeading {
				return 0, false
			}
			if !isSingleColumnWrap(cur.Row, row) {
				return 0, false
			}
		}
		// Incoherence: penalise larger-than-median gaps relative to the run's
		// own leading (adaptive), so a tight, regular multi-line cell is the
		// cheapest merge.
		if median > 0 {
			incoherence += math.Max(0, gap-median) / median
		}
		cur = mergeContinuationRow(cur, row)
	}
	return base + incoherence, true
}

// buildLogicalRunRow merges the consecutive text rows of one logical run into a
// single logicalTableRow, reusing the existing cell-merge helpers so downstream
// geometry is unchanged.
func buildLogicalRunRow(run []textline.ParagraphTextLine) logicalTableRow {
	row := newLogicalTableRow(run[0])
	for k := 1; k < len(run); k++ {
		row = mergeContinuationRow(row, run[k])
	}
	return row
}

// mergeContinuationRow folds a continuation text row into the accumulated
// logical row. A lone wrap that actually lands in the left-most (first) column
// routes through the existing greedy helper so its behaviour is byte-identical
// to mergeFirstColumnContinuationRows; every other continuation — a single-cell
// wrap belonging to a non-first column, or a multi-cell wrap — folds each
// fragment into the run column it overlaps most via bestColumnTarget. Routing a
// non-first-column single-cell wrap through the first-column helper would
// mis-fold it into column 0 and stretch that column's box, corrupting both the
// merged text and the downstream gridRowBoxes geometry.
func mergeContinuationRow(previous logicalTableRow, continuation textline.ParagraphTextLine) logicalTableRow {
	if len(continuation.Cells) == 1 && continuationTargetsFirstColumn(previous.Row, continuation) {
		return mergeLogicalTableRowContinuation(previous, continuation)
	}
	previous.Row = mergeMultiCellContinuationIntoRow(previous.Row, continuation)
	previous.Source = append(previous.Source, continuation.Cells...)
	return previous
}

// continuationTargetsFirstColumn reports whether a single-cell continuation
// resolves to the left-most column of the accumulated row, i.e. it is the lone
// first-column wrap the greedy merger handles. Only then is the greedy
// first-column fold (which always targets the min-L column) correct.
func continuationTargetsFirstColumn(previous, continuation textline.ParagraphTextLine) bool {
	if len(previous.Cells) == 0 || len(continuation.Cells) != 1 {
		return false
	}
	target := bestColumnTarget(previous.Cells, continuation.Cells[0].Box)
	if target < 0 {
		return false
	}
	firstIndex := 0
	for i := range previous.Cells {
		if previous.Cells[i].Box.L < previous.Cells[firstIndex].Box.L {
			firstIndex = i
		}
	}
	return target == firstIndex
}

// mergeMultiCellContinuationIntoRow appends each continuation fragment to the
// run column whose horizontal extent it overlaps most (nearest L on a tie),
// mirroring mergeFirstColumnContinuationIntoRow for the multi-cell case.
func mergeMultiCellContinuationIntoRow(previous, continuation textline.ParagraphTextLine) textline.ParagraphTextLine {
	if len(previous.Cells) == 0 {
		return previous
	}
	cells := append([]page.TextCell(nil), previous.Cells...)
	for _, next := range continuation.Cells {
		target := bestColumnTarget(cells, next.Box)
		if target < 0 {
			cells = append(cells, next)
			continue
		}
		cells[target].Text = strings.TrimSpace(cells[target].Text + " " + next.Text)
		cells[target].Box = geom.EnclosingBox(cells[target].Box, next.Box)
		cells[target].Index = min(cells[target].Index, next.Index)
		if next.FontSize > cells[target].FontSize {
			cells[target].FontSize = next.FontSize
		}
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Index < cells[j].Index
		}
		return cells[i].Box.L < cells[j].Box.L
	})
	return textline.ParagraphTextLine{Cells: cells, Center: averageRowCenter(cells)}
}

// bestColumnTarget returns the index of the run cell whose horizontal span best
// matches box, or -1 if none overlaps.
func bestColumnTarget(cells []page.TextCell, box geom.Box) int {
	best, bestOverlap, bestDist := -1, 0.0, math.MaxFloat64
	for i, cell := range cells {
		overlap := math.Min(cell.Box.R, box.R) - math.Max(cell.Box.L, box.L)
		dist := math.Abs(cell.Box.L - box.L)
		if overlap > bestOverlap || (overlap == bestOverlap && dist < bestDist) {
			best, bestOverlap, bestDist = i, overlap, dist
		}
	}
	if bestOverlap <= 0 {
		return -1
	}
	return best
}

// isSingleColumnWrap reports whether the continuation row is the wrapped second
// (or later) line of exactly ONE existing column: every fragment must overlap an
// existing run column, and all fragments must collapse to the SAME column. A row
// whose fragments land in two or more distinct columns is a structural data row
// with a blank first column (e.g. a header sub-row spanning several columns), not
// a wrap, so it is treated as a boundary and kept separate.
func isSingleColumnWrap(run, continuation textline.ParagraphTextLine) bool {
	if len(run.Cells) == 0 || len(continuation.Cells) == 0 {
		return false
	}
	target := -1
	for _, next := range continuation.Cells {
		col := bestColumnTarget(run.Cells, next.Box)
		if col < 0 {
			// Overlaps no existing column: a brand-new cell, i.e. new row data.
			return false
		}
		if target == -1 {
			target = col
			continue
		}
		if col != target {
			// Fragments span more than one column: structural row, not a wrap.
			return false
		}
	}
	return true
}

func rowHasAnchorBandCell(row textline.ParagraphTextLine, band, tolerance float64) bool {
	for _, cell := range row.Cells {
		if math.Abs(cell.Box.L-band) <= tolerance {
			return true
		}
	}
	return false
}

func leftMostEdge(cells []page.TextCell) float64 {
	edge := math.MaxFloat64
	for _, cell := range cells {
		if cell.Box.L < edge {
			edge = cell.Box.L
		}
	}
	if edge == math.MaxFloat64 {
		return 0
	}
	return edge
}

func minLeading(run []textline.ParagraphTextLine) float64 {
	height := 0.0
	count := 0
	for _, row := range run {
		for _, cell := range row.Cells {
			height += cell.Box.Height()
			count++
		}
	}
	if count == 0 {
		return 18
	}
	limit := (height / float64(count)) * 1.6
	if limit < 18 {
		return 18
	}
	return limit
}
