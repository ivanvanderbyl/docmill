package table

import (
	"math"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Column-boundary features for the learned table-structure model.
//
// A table's grid is not something a class label can express, so the layout
// classifier stops at "this region is a table" and something else has to find
// the columns. Today that is hand-tuned gutter logic. This file is the same
// measurements with the cutoffs removed: it enumerates every plausible column
// boundary and describes each one numerically, so a model trained on FinTabNet's
// human-annotated grids can decide which are real.
//
// Nothing here decides anything. As with the line features, the existing
// detector keeps its verdicts until the model is measured against them.

// ColumnGapFeatureNames is the ordered feature contract for one candidate
// column boundary, shared between the Python trainer and the Go predictor.
var ColumnGapFeatureNames = []string{
	"gap_width_frac",      // gap width / table width
	"gap_width_chars",     // gap width / median character width
	"gap_rank_frac",       // rank of this gap by width, normalised
	"x_position_frac",     // gap centre / table width
	"persistence",         // fraction of rows in which the gap is clear
	"clear_rows",          // rows in which the gap is clear
	"row_count",           // rows in the table
	"left_align_frac",     // rows whose text ends near the gap's left edge
	"right_align_frac",    // rows whose text starts near the gap's right edge
	"crossing_rows_frac",  // rows with a cell spanning the gap
	"ruling_coverage",     // vertical ruling length inside the gap / table height
	"left_density",        // text area left of the gap / area available
	"right_density",       // text area right of the gap / area available
	"left_digit_frac",     // digit share of the text immediately left
	"right_digit_frac",    // digit share of the text immediately right
	"neighbour_gap_ratio", // this gap's width / the median gap width
}

// ColumnGap is one candidate column boundary inside a table region.
type ColumnGap struct {
	Left     float64 // gap's left edge in page points
	Right    float64
	Features []float64
}

// Center returns the gap's midpoint, the position a column split would take.
func (g ColumnGap) Center() float64 { return (g.Left + g.Right) / 2 }

// ColumnGapCandidates enumerates the plausible column boundaries in a table
// region and describes each numerically.
//
// Candidates are the maximal x-intervals containing no text, found by projecting
// every word cell onto the x-axis. That deliberately over-generates — a wide
// space inside a sentence is a candidate too — because the model's job is to
// choose, and a boundary that was never proposed can never be chosen.
//
// cells should be WORD-level. Line-level cells span whole rows, which erases
// every internal gap; the FinTabNet alignment check found the same thing
// (53% -> 86% cell match when moving from line to word granularity).
func ColumnGapCandidates(cells []page.TextCell, rulings []page.RulingSegment, box geom.Box) []ColumnGap {
	inside := cellsInsideBox(cells, box)
	if len(inside) < 2 {
		return nil
	}

	rows := groupCellsIntoRows(inside)
	if len(rows) == 0 {
		return nil
	}
	charWidth := medianCharacterWidth(inside)
	gaps := horizontalGaps(inside, box)
	if len(gaps) == 0 {
		return nil
	}

	widths := make([]float64, len(gaps))
	for i, g := range gaps {
		widths[i] = g[1] - g[0]
	}
	sorted := append([]float64(nil), widths...)
	sort.Float64s(sorted)
	medianGap := sorted[len(sorted)/2]

	tableWidth := nonZero(box.Width())
	tableHeight := nonZero(math.Abs(boxBottom(box) - boxTop(box)))

	out := make([]ColumnGap, 0, len(gaps))
	for _, g := range gaps {
		left, right := g[0], g[1]
		width := right - left

		clear, crossing := 0, 0
		leftAligned, rightAligned := 0, 0
		for _, row := range rows {
			spans, endsNear, startsNear := false, false, false
			for _, cell := range row {
				if cell.Box.L < right && cell.Box.R > left {
					spans = true
				}
				if math.Abs(cell.Box.R-left) <= charWidth {
					endsNear = true
				}
				if math.Abs(cell.Box.L-right) <= charWidth {
					startsNear = true
				}
			}
			if spans {
				crossing++
			} else {
				clear++
			}
			if endsNear {
				leftAligned++
			}
			if startsNear {
				rightAligned++
			}
		}

		leftArea, rightArea := 0.0, 0.0
		leftDigits, leftChars, rightDigits, rightChars := 0, 0, 0, 0
		for _, cell := range inside {
			area := cell.Box.Width() * cell.Box.Height()
			if cell.Box.R <= left {
				leftArea += area
				d, n := digitCounts(cell.Text)
				leftDigits += d
				leftChars += n
			} else if cell.Box.L >= right {
				rightArea += area
				d, n := digitCounts(cell.Text)
				rightDigits += d
				rightChars += n
			}
		}

		rulingLength := 0.0
		for _, ruling := range rulings {
			x0, x1 := math.Min(ruling.FromX, ruling.ToX), math.Max(ruling.FromX, ruling.ToX)
			// Vertical rules only: a horizontal rule says nothing about columns.
			if x1-x0 > width || x0 < left || x1 > right {
				continue
			}
			y0, y1 := math.Min(ruling.FromY, ruling.ToY), math.Max(ruling.FromY, ruling.ToY)
			rulingLength += math.Min(y1, boxBottom(box)) - math.Max(y0, boxTop(box))
		}

		rank := 0
		for _, w := range widths {
			if w > width {
				rank++
			}
		}

		rowCount := float64(len(rows))
		out = append(out, ColumnGap{
			Left:  left,
			Right: right,
			Features: []float64{
				width / tableWidth,
				width / nonZero(charWidth),
				float64(rank) / nonZero(float64(len(gaps))),
				((left+right)/2 - box.L) / tableWidth,
				float64(clear) / nonZero(rowCount),
				float64(clear),
				rowCount,
				float64(leftAligned) / nonZero(rowCount),
				float64(rightAligned) / nonZero(rowCount),
				float64(crossing) / nonZero(rowCount),
				rulingLength / tableHeight,
				leftArea / nonZero(tableWidth*tableHeight),
				rightArea / nonZero(tableWidth*tableHeight),
				fractionOf(leftDigits, leftChars),
				fractionOf(rightDigits, rightChars),
				width / nonZero(medianGap),
			},
		})
	}
	return out
}

// horizontalGaps returns the maximal x-intervals inside box containing no text,
// by sweeping the union of every cell's x-extent.
func horizontalGaps(cells []page.TextCell, box geom.Box) [][2]float64 {
	type span struct{ l, r float64 }
	spans := make([]span, 0, len(cells))
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		spans = append(spans, span{cell.Box.L, cell.Box.R})
	}
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].l < spans[j].l })

	var gaps [][2]float64
	reach := spans[0].r
	for _, s := range spans[1:] {
		if s.l > reach {
			gaps = append(gaps, [2]float64{reach, s.l})
		}
		if s.r > reach {
			reach = s.r
		}
	}
	return gaps
}

// groupCellsIntoRows clusters cells into visual rows by vertical centre. Rows
// are what "persistence" is measured over: a real column boundary is clear in
// most rows, a chance gap in few.
func groupCellsIntoRows(cells []page.TextCell) [][]page.TextCell {
	if len(cells) == 0 {
		return nil
	}
	sorted := append([]page.TextCell(nil), cells...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Box.CenterY() < sorted[j].Box.CenterY() })

	heights := make([]float64, 0, len(sorted))
	for _, cell := range sorted {
		if h := cell.Box.Height(); h > 0 {
			heights = append(heights, h)
		}
	}
	if len(heights) == 0 {
		return nil
	}
	sort.Float64s(heights)
	tolerance := heights[len(heights)/2] * 0.6

	var rows [][]page.TextCell
	current := []page.TextCell{sorted[0]}
	centre := sorted[0].Box.CenterY()
	for _, cell := range sorted[1:] {
		if math.Abs(cell.Box.CenterY()-centre) <= tolerance {
			current = append(current, cell)
			continue
		}
		rows = append(rows, current)
		current = []page.TextCell{cell}
		centre = cell.Box.CenterY()
	}
	return append(rows, current)
}

func cellsInsideBox(cells []page.TextCell, box geom.Box) []page.TextCell {
	out := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		w := math.Min(cell.Box.R, box.R) - math.Max(cell.Box.L, box.L)
		h := math.Min(boxBottom(cell.Box), boxBottom(box)) - math.Max(boxTop(cell.Box), boxTop(box))
		if w <= 0 || h <= 0 {
			continue
		}
		area := cell.Box.Width() * cell.Box.Height()
		if area <= 0 || (w*h)/area < 0.5 {
			continue
		}
		out = append(out, cell)
	}
	return out
}

func medianCharacterWidth(cells []page.TextCell) float64 {
	widths := make([]float64, 0, len(cells))
	for _, cell := range cells {
		n := len([]rune(strings.TrimSpace(cell.Text)))
		if n > 0 && cell.Box.Width() > 0 {
			widths = append(widths, cell.Box.Width()/float64(n))
		}
	}
	if len(widths) == 0 {
		return 1
	}
	sort.Float64s(widths)
	return widths[len(widths)/2]
}

func digitCounts(text string) (digits, total int) {
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		total++
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits, total
}

func boxTop(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.B
	}
	return box.T
}

func boxBottom(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.T
	}
	return box.B
}

func fractionOf(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func nonZero(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
