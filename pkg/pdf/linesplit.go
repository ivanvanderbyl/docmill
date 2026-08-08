package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Splitting assembled lines at persistent vertical whitespace.
//
// The line assembler clusters cells by vertical centre, so anything sharing a
// baseline joins one line however far apart it sits horizontally. Two figures
// side by side with their own captions become ONE assembled line; a running
// header of `Chapter 3 ... Page 45` becomes one line where the annotator marked
// two.
//
// Measured on DocLayNet val this caps what any proposer can reach, because the
// assembled line straddles the boundary between two annotated regions and can
// belong to neither: Page-header 26.5%, Caption 58.1%, Title 60.9%. Those three
// ceilings did not move when ink was added, and they cannot, because the defect
// is in the text itself.
//
// The fix is not "split on a wide gap". Wide gaps are everywhere — between
// sentences after a full stop, around display maths, in ragged justification. A
// gap is a COLUMN when it PERSISTS: when the lines above and below are also
// clear at the same x. That is the same principle the table gutter detector
// already uses, and it is the reason this is a structural test rather than a
// tuned width.

const (
	// splitMinGapRatio is how wide a gap must be, in multiples of the line's
	// font size, before it is even considered as a column boundary. This is a
	// cheap pre-filter, not the decision: persistence is the decision.
	splitMinGapRatio = 1.5

	// splitPersistenceRows is how many lines above and below are checked for
	// the same clear corridor.
	splitPersistenceRows = 2

	// splitMinPersistence is the fraction of those neighbouring lines that must
	// also be clear at the gap. Requiring ALL of them is too strict — a
	// two-column page has full-width headings crossing every gutter — and
	// requiring one is no test at all.
	splitMinPersistence = 0.6

	// splitDecisiveGapRatio is the gap width, in font sizes, above which a gap
	// needs no corroboration from its neighbours.
	splitDecisiveGapRatio = 4.0

	// splitMinSideWidth keeps a split from shaving a single character off an
	// end. A fragment narrower than this is punctuation, not a column.
	splitMinSideWidth = 12.0
)

// SplitLinesAtColumnGaps splits assembled lines wherever a horizontal gap
// persists across neighbouring lines.
//
// Lines are returned in the same top-to-bottom order, with ReadingOrder
// renumbered, so callers see the same shape of result with more entries.
func SplitLinesAtColumnGaps(lines []ParagraphTextLine, size geom.Size) []ParagraphTextLine {
	if len(lines) < 2 {
		return lines
	}

	out := make([]ParagraphTextLine, 0, len(lines))
	for i, line := range lines {
		splits := columnSplitPoints(lines, i, size)
		if len(splits) == 0 {
			out = append(out, line)
			continue
		}
		out = append(out, splitLineAt(line, splits)...)
	}

	sort.SliceStable(out, func(a, b int) bool {
		if math.Abs(out[a].BBox.CenterY()-out[b].BBox.CenterY()) > 0.5 {
			return out[a].BBox.CenterY() < out[b].BBox.CenterY()
		}
		return out[a].BBox.L < out[b].BBox.L
	})
	for i := range out {
		out[i].ReadingOrder = i
	}
	return out
}

// columnSplitPoints returns the x positions at which line index should be cut.
func columnSplitPoints(lines []ParagraphTextLine, index int, size geom.Size) []float64 {
	line := lines[index]
	if len(line.Cells) < 2 {
		return nil
	}

	cells := append([]page.TextCell(nil), line.Cells...)
	sort.SliceStable(cells, func(a, b int) bool { return cells[a].Box.L < cells[b].Box.L })

	minGap := math.Max(line.FontSize*splitMinGapRatio, 1)
	var splits []float64
	rightmost := cells[0].Box.R
	for _, cell := range cells[1:] {
		gap := cell.Box.L - rightmost
		if gap >= minGap {
			middle := rightmost + gap/2
			// A running header of `Chapter 3 ... Page 45` gets no help from
			// persistence: the line below it is body text spanning the very
			// corridor being tested, so the corroboration always fails. But a
			// gap this wide is a column on its own evidence — justification
			// stretches spaces, never by six ems — so an extreme gap stands
			// without corroboration.
			decisive := gap >= line.FontSize*splitDecisiveGapRatio
			if (decisive || gapPersists(lines, index, rightmost, cell.Box.L)) &&
				splitLeavesUsableSides(line, middle) {
				splits = append(splits, middle)
			}
		}
		if cell.Box.R > rightmost {
			rightmost = cell.Box.R
		}
	}
	return splits
}

// gapPersists reports whether the corridor between left and right is also clear
// on the neighbouring lines.
//
// Only lines that actually overlap the corridor's horizontal extent are
// consulted. A short line ending before the gap says nothing about whether the
// gap is a column, and counting it as evidence either way is what makes naive
// gutter detection fire on ragged right margins.
func gapPersists(lines []ParagraphTextLine, index int, left, right float64) bool {
	checked, clear := 0, 0
	for offset := -splitPersistenceRows; offset <= splitPersistenceRows; offset++ {
		if offset == 0 {
			continue
		}
		neighbour := index + offset
		if neighbour < 0 || neighbour >= len(lines) {
			continue
		}
		other := lines[neighbour]
		if other.BBox.L >= right || other.BBox.R <= left {
			continue // says nothing about this corridor
		}
		checked++
		if lineIsClearBetween(other, left, right) {
			clear++
		}
	}
	if checked == 0 {
		// Nothing to corroborate with. A gap on an isolated line is not
		// evidence of a column, and splitting on it would break exactly the
		// single-line cases — display maths, a lone centred title — that the
		// persistence test exists to protect.
		return false
	}
	return float64(clear)/float64(checked) >= splitMinPersistence
}

func lineIsClearBetween(line ParagraphTextLine, left, right float64) bool {
	for _, cell := range line.Cells {
		if cell.Box.R > left && cell.Box.L < right {
			return false
		}
	}
	return true
}

func splitLeavesUsableSides(line ParagraphTextLine, at float64) bool {
	return at-line.BBox.L >= splitMinSideWidth && line.BBox.R-at >= splitMinSideWidth
}

// splitLineAt cuts a line into pieces at the given x positions, rebuilding each
// piece from its own cells so text, words and inline elements are all consistent
// with the narrower box.
func splitLineAt(line ParagraphTextLine, splits []float64) []ParagraphTextLine {
	bounds := append([]float64(nil), splits...)
	sort.Float64s(bounds)

	buckets := make([][]page.TextCell, len(bounds)+1)
	for _, cell := range line.Cells {
		centre := (cell.Box.L + cell.Box.R) / 2
		slot := sort.SearchFloat64s(bounds, centre)
		buckets[slot] = append(buckets[slot], cell)
	}

	out := make([]ParagraphTextLine, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		piece := buildParagraphTextLine(bucket)
		// The producer recomputes geometry and text from the cells, but not the
		// per-line attributes the assembler had already settled. Carrying them
		// across keeps a split line indistinguishable from one that was never
		// joined.
		piece.FontSize = line.FontSize
		piece.WritingDirection = line.WritingDirection
		piece.Orientation = line.Orientation
		out = append(out, piece)
	}
	if len(out) == 0 {
		return []ParagraphTextLine{line}
	}
	return out
}
