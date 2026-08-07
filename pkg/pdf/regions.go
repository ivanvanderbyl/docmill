package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

// The REGION stage of the cascade.
//
// The line model answers "what is this line". Some questions are not line
// questions at all: whether a run of lines is a table depends on gutter
// persistence, column-count stability and row regularity across the whole run,
// and no feature of any single line can express them. Two separate measurements
// have now run into this — `Table` line labels barely moved when routing was
// handed to the model, and the learned column model turned a display equation
// into a four-column table because nothing had asked "is this a table at all".
//
// So candidates are formed from runs of same-label lines, described with
// region-scoped features, and accepted or rejected by a second model. Rejected
// candidates fall back to their line labels; nothing is lost.
//
// The candidate extent is deliberately simple — a maximal run of same-label
// lines with a small gap tolerance. Growing or shrinking a candidate by a line
// is a later refinement, worth doing only if boundary errors turn out to matter.

// LineRegion is a candidate region: a run of adjacent lines the line model gave
// the same label.
type LineRegion struct {
	Class string   // the label its lines share
	Box   geom.Box // union of the lines' boxes
	Lines []int    // indices into the page's line slice
}

// regionGapTolerance is how far apart two same-label lines may sit and still
// belong to one candidate, in multiples of the taller line's height. Generous
// on purpose: a candidate that is too large can be rejected by the region
// model, but a table split into three candidates can never be reassembled.
const regionGapTolerance = 2.5

// GroupLineRegions collects runs of same-label lines into candidates. lines and
// labels must be the same length, with lines ordered top-to-bottom.
func GroupLineRegions(lines []ParagraphTextLine, labels []string) []LineRegion {
	if len(lines) == 0 || len(lines) != len(labels) {
		return nil
	}

	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return lines[order[a]].BBox.CenterY() < lines[order[b]].BBox.CenterY()
	})

	var regions []LineRegion
	var current LineRegion
	flush := func() {
		if len(current.Lines) > 0 {
			regions = append(regions, current)
		}
		current = LineRegion{}
	}

	for _, index := range order {
		line := lines[index]
		if len(current.Lines) > 0 && current.Class == labels[index] && withinRegionGap(current.Box, line.BBox) {
			current.Box = unionBoxes(current.Box, line.BBox)
			current.Lines = append(current.Lines, index)
			continue
		}
		flush()
		current = LineRegion{Class: labels[index], Box: line.BBox, Lines: []int{index}}
	}
	flush()
	return regions
}

func withinRegionGap(region, line geom.Box) bool {
	gap := topEdgeOf(line) - bottomEdgeOf(region)
	if gap <= 0 {
		return true // overlapping or touching
	}
	height := math.Max(region.Height(), line.Height())
	if height <= 0 {
		return false
	}
	return gap <= height*regionGapTolerance
}

func unionBoxes(a, b geom.Box) geom.Box {
	return geom.Box{
		L:      math.Min(a.L, b.L),
		R:      math.Max(a.R, b.R),
		T:      math.Min(topEdgeOf(a), topEdgeOf(b)),
		B:      math.Max(bottomEdgeOf(a), bottomEdgeOf(b)),
		Origin: geom.TopLeft,
	}
}

// RegionFeatureNames is the ordered feature contract for a candidate region.
//
// Every entry is a property of the RUN, not of any line in it. That is the
// whole point: if a feature could be computed from one line, the line model
// already had it.
var RegionFeatureNames = []string{
	// Extent.
	"line_count",
	"width_frac",  // region width / page width
	"height_frac", // region height / page height
	"area_frac",
	"y_center_frac",

	// Column structure — the signals that separate a table from stacked prose.
	"gutter_count",       // candidate column gaps found inside the region
	"gutter_persistence", // best gap's clear-row fraction
	"gutter_mean_persistence",
	"column_count_stability", // 1 - spread of per-row cell counts / mean
	"mean_cells_per_row",
	"max_cells_per_row",

	// Row structure.
	"row_height_regularity", // 1 - stddev/mean of row heights
	"row_gap_regularity",
	"row_count",

	// Ink.
	"ruling_coverage",     // ruling length inside the region / region perimeter
	"vertical_rule_count", // distinct vertical rules spanning most of the region

	// Content.
	"math_frac",
	"digit_frac",
	"mean_line_width_frac",
	"line_width_variance",

	// The distribution of line labels inside the candidate. This is what makes
	// "80% of these lines scored Formula" a region feature — the plan's own
	// example, and the reason the equation-versus-table arbitration becomes
	// learned rather than ordered.
	"frac_Background",
	"frac_Caption",
	"frac_Footnote",
	"frac_Formula",
	"frac_List-item",
	"frac_Page-footer",
	"frac_Page-header",
	"frac_Picture",
	"frac_Section-header",
	"frac_Table",
	"frac_Text",
	"frac_Title",
}

// regionLabelOrder fixes which frac_* slot each class occupies. It must match
// the tail of RegionFeatureNames exactly.
var regionLabelOrder = []string{
	"Background", "Caption", "Footnote", "Formula", "List-item", "Page-footer",
	"Page-header", "Picture", "Section-header", "Table", "Text", "Title",
}

// RegionFeatures describes one candidate region numerically.
func RegionFeatures(region LineRegion, lines []ParagraphTextLine, labels []string, cells []page.TextCell, rulings []page.RulingSegment, size geom.Size) []float64 {
	pageW := nonZeroValue(size.Width)
	pageH := nonZeroValue(size.Height)
	box := region.Box
	height := math.Abs(bottomEdgeOf(box) - topEdgeOf(box))

	member := make([]ParagraphTextLine, 0, len(region.Lines))
	for _, index := range region.Lines {
		member = append(member, lines[index])
	}

	// Column structure, measured with the same candidate-gap code the column
	// model uses, so the two stages agree about what a gutter is.
	gaps := doctable.ColumnGapCandidates(cells, rulings, box)
	bestPersistence, meanPersistence := 0.0, 0.0
	if len(gaps) > 0 {
		persistenceIndex := 4 // "persistence" in ColumnGapFeatureNames
		for _, gap := range gaps {
			p := gap.Features[persistenceIndex]
			meanPersistence += p
			if p > bestPersistence {
				bestPersistence = p
			}
		}
		meanPersistence /= float64(len(gaps))
	}

	// Per-line cell counts and geometry.
	cellCounts := make([]float64, 0, len(member))
	heights := make([]float64, 0, len(member))
	widths := make([]float64, 0, len(member))
	tops := make([]float64, 0, len(member))
	mathChars, digitChars, totalChars := 0, 0, 0
	for _, line := range member {
		cellCounts = append(cellCounts, float64(len(line.Cells)))
		heights = append(heights, line.BBox.Height())
		widths = append(widths, line.BBox.Width()/pageW)
		tops = append(tops, topEdgeOf(line.BBox))
		counts := countLineRunes(line)
		mathChars += counts.math
		digitChars += counts.digit
		totalChars += counts.chars
	}
	sort.Float64s(tops)
	gapsBetweenRows := make([]float64, 0, len(tops))
	for i := 1; i < len(tops); i++ {
		gapsBetweenRows = append(gapsBetweenRows, tops[i]-tops[i-1])
	}

	rulingLength := 0.0
	verticalRules := 0
	for _, ruling := range rulings {
		x0, x1 := math.Min(ruling.FromX, ruling.ToX), math.Max(ruling.FromX, ruling.ToX)
		y0, y1 := math.Min(ruling.FromY, ruling.ToY), math.Max(ruling.FromY, ruling.ToY)
		if x1 < box.L || x0 > box.R || y1 < topEdgeOf(box) || y0 > bottomEdgeOf(box) {
			continue
		}
		rulingLength += math.Hypot(x1-x0, y1-y0)
		if y1-y0 > height*0.5 && x1-x0 <= math.Max(2, ruling.Width) {
			verticalRules++
		}
	}

	features := []float64{
		float64(len(region.Lines)),
		box.Width() / pageW,
		height / pageH,
		(box.Width() * height) / (pageW * pageH),
		box.CenterY() / pageH,

		float64(len(gaps)),
		bestPersistence,
		meanPersistence,
		stability(cellCounts),
		meanOf(cellCounts),
		maxOf(cellCounts),

		stability(heights),
		stability(gapsBetweenRows),
		float64(len(member)),

		rulingLength / nonZeroValue(2*(box.Width()+height)),
		float64(verticalRules),

		fractionOf(mathChars, totalChars),
		fractionOf(digitChars, totalChars),
		meanOf(widths),
		varianceOf(widths),
	}

	counts := map[string]int{}
	for _, index := range region.Lines {
		counts[labels[index]]++
	}
	for _, class := range regionLabelOrder {
		features = append(features, fractionOf(counts[class], len(region.Lines)))
	}
	return features
}

// stability is 1 - (stddev / mean), clamped to [0, 1]: 1 when every value is
// identical, 0 when they scatter as much as their own size. A table's rows have
// stable cell counts and heights; stacked prose does not.
func stability(values []float64) float64 {
	if len(values) < 2 {
		return 1
	}
	mean := meanOf(values)
	if mean == 0 {
		return 1
	}
	return math.Max(0, math.Min(1, 1-standardDeviation(values)/mean))
}

func meanOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func maxOf(values []float64) float64 {
	best := 0.0
	for _, v := range values {
		if v > best {
			best = v
		}
	}
	return best
}

func varianceOf(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sd := standardDeviation(values)
	return sd * sd
}
