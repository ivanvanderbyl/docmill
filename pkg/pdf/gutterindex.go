package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Page-scope gutter features.
//
// The previous gutter features called doctable.ColumnGapCandidates once per
// candidate: 722 microseconds each, times ~375 candidates, is 271 of the
// measured 291 ms/page — against a whole-pipeline budget of 12-14 ms/page.
// Almost all of that work is re-grouping the same page's cells into the same
// rows 375 times.
//
// This index does the page-wide work ONCE: cells grouped into rows, each row's
// occupied x-intervals sorted. Per candidate it selects the rows the box spans
// by binary search and sweeps a coarse occupancy grid across the box width.
// A gutter is a run of x-cells clear in most of those rows; the discretisation
// is the same 2pt cell the ink clustering uses, and for the same reason — the
// exact-interval version of this computation is what cost 722us a call.
//
// The features keep their contract names (gutter_count, gutter_persistence,
// gutter_mean_persistence) but their values shift slightly: rows here are the
// index's own y-grouping of word cells rather than doctable's. That is fine
// ONLY because both models are retrained against the new emitter in the same
// change — the count-based contract check cannot catch a semantic shift, so
// the blobs and the extractor must move together.

const (
	// gutterCellSize matches inkCellSize: fine enough that a real gutter is
	// several cells wide, coarse enough that a page row is ~300 cells.
	gutterCellSize = 2.0

	// gutterMinWidth is the narrowest run that counts as a gutter. Below ~6pt
	// it is word spacing, and counting word spacing as column structure is how
	// prose grows imaginary columns.
	gutterMinWidth = 6.0

	// gutterMinClear is the fraction of spanned rows that must be clear at an
	// x-run for it to count as a gutter at all. The persistence VALUES are what
	// the model consumes; this floor only keeps the count feature from tallying
	// every ragged margin.
	gutterMinClear = 0.6

	// gutterRowTolerance groups cells into rows by vertical centre, in
	// multiples of the cell height. Same idea as the line assembler's
	// tolerance, restated here because the index sees raw word cells.
	gutterRowTolerance = 0.6
)

// gutterRow is one horizontal band of the page with its occupied x-spans.
type gutterRow struct {
	centerY float64
	spans   [][2]float64 // sorted by left edge, merged where touching
}

// gutterIndex is the per-page structure. Build once, query per candidate.
type gutterIndex struct {
	rows []gutterRow // sorted by centerY
}

// newGutterIndex groups the page's cells into rows.
func newGutterIndex(cells []page.TextCell) *gutterIndex {
	if len(cells) == 0 {
		return &gutterIndex{}
	}
	sorted := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if cell.Box.Width() > 0 {
			sorted = append(sorted, cell)
		}
	}
	sort.SliceStable(sorted, func(a, b int) bool {
		return sorted[a].Box.CenterY() < sorted[b].Box.CenterY()
	})

	index := &gutterIndex{}
	var current []page.TextCell
	flush := func() {
		if len(current) == 0 {
			return
		}
		row := gutterRow{}
		sumY, sumH := 0.0, 0.0
		spans := make([][2]float64, 0, len(current))
		for _, cell := range current {
			sumY += cell.Box.CenterY()
			sumH += cell.Box.Height()
			spans = append(spans, [2]float64{cell.Box.L, cell.Box.R})
		}
		row.centerY = sumY / float64(len(current))
		sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
		// Merge touching spans so a row is a short list even when the page has
		// hundreds of word cells on one line.
		merged := spans[:0]
		for _, span := range spans {
			if n := len(merged); n > 0 && span[0] <= merged[n-1][1]+1 {
				if span[1] > merged[n-1][1] {
					merged[n-1][1] = span[1]
				}
				continue
			}
			merged = append(merged, span)
		}
		row.spans = merged
		index.rows = append(index.rows, row)
		current = current[:0]
	}

	for _, cell := range sorted {
		if len(current) > 0 {
			height := math.Max(cell.Box.Height(), 1)
			if cell.Box.CenterY()-current[len(current)-1].Box.CenterY() > height*gutterRowTolerance {
				flush()
			}
		}
		current = append(current, cell)
	}
	flush()
	return index
}

// gutterFeatures reports the column-gutter structure inside box: how many
// gutters, the best gutter's clear-row fraction, and the mean over gutters.
func (g *gutterIndex) gutterFeatures(box geom.Box) (count, best, mean float64) {
	if g == nil || len(g.rows) == 0 || box.Width() <= gutterMinWidth {
		return 0, 0, 0
	}
	top, bottom := topEdgeOf(box), bottomEdgeOf(box)

	// Rows the candidate spans, by binary search over the sorted centres.
	first := sort.Search(len(g.rows), func(i int) bool { return g.rows[i].centerY >= top })
	last := sort.Search(len(g.rows), func(i int) bool { return g.rows[i].centerY > bottom })
	spanned := g.rows[first:last]
	if len(spanned) < 2 {
		// One row has no "persistence across rows" to measure; reporting its
		// word gaps as gutters is exactly the prose-grows-columns failure.
		return 0, 0, 0
	}

	cols := int(box.Width()/gutterCellSize) + 1
	if cols < 2 {
		return 0, 0, 0
	}
	occupied := make([]int16, cols)
	contributing := 0
	for _, row := range spanned {
		touches := false
		for _, span := range row.spans {
			left, right := span[0], span[1]
			if right <= box.L || left >= box.R {
				continue
			}
			touches = true
			x0 := clampInt(int((left-box.L)/gutterCellSize), 0, cols-1)
			x1 := clampInt(int((right-box.L)/gutterCellSize), 0, cols-1)
			for x := x0; x <= x1; x++ {
				occupied[x]++
			}
		}
		if touches {
			contributing++
		}
	}
	if contributing < 2 {
		return 0, 0, 0
	}

	// Interior runs of mostly-clear cells. Runs touching either edge are the
	// margins, not gutters.
	minCells := int(gutterMinWidth / gutterCellSize)
	maxOccupied := float64(contributing) * (1 - gutterMinClear)
	runStart := -1
	sum := 0.0
	flushRun := func(end int) {
		if runStart < 0 {
			return
		}
		width := end - runStart
		if runStart > 0 && end < cols && width >= minCells {
			clear := 0.0
			for x := runStart; x < end; x++ {
				clear += 1 - float64(occupied[x])/float64(contributing)
			}
			clear /= float64(width)
			count++
			sum += clear
			if clear > best {
				best = clear
			}
		}
		runStart = -1
	}
	for x := 0; x < cols; x++ {
		if float64(occupied[x]) <= maxOccupied {
			if runStart < 0 {
				runStart = x
			}
		} else {
			flushRun(x)
		}
	}
	flushRun(cols)

	if count > 0 {
		mean = sum / count
	}
	return count, best, mean
}
