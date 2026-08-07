package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Features for the new class-agnostic region proposals.
//
// The old region model answered "should this candidate stand", because the old
// proposer attached a class to every candidate by construction — it grouped
// lines that already shared a predicted label. The new proposer does not: it
// splits on geometry alone and clusters ink, so a proposal arrives with no
// opinion about what it is.
//
// That makes the region stage a CLASSIFIER rather than a gate, and it is
// strictly more useful. A gate can only reject; a classifier assigns a class and
// a confidence, and confidence is what lets overlapping proposals compete. With
// 375 candidates per page, competition is the whole game — the old model scored
// each candidate in isolation and could therefore never fix an extent, only
// veto one.
//
// The features split into four groups: the shape of the region, its internal
// structure, what the line model made of its lines, and what ink it contains.

// ProposalFeatureNames is the ordered feature contract for a region proposal.
//
// It is defined here, in the shipping package, and read out of the binary by
// the trainer. Restating it in Python is how training/serving skew gets in, and
// this project has already paid for that lesson once.
var ProposalFeatureNames = []string{
	// --- Shape.
	"line_count",
	"width_frac",  // of page width
	"height_frac", // of page height
	"area_frac",
	"aspect", // width / height
	"x_center_frac",
	"y_center_frac",
	"left_frac",
	"right_frac",

	// --- Isolation. How much whitespace surrounds the candidate, in multiples
	// of its own median line height. A region that is a complete unit is
	// bounded by space above and below; one that stops mid-paragraph is not,
	// and no other feature says so.
	"gap_above",
	"gap_below",
	"touches_left_margin",
	"touches_right_margin",

	// --- Column structure, from the same candidate-gap code the column model
	// uses, so both stages agree what a gutter is.
	"gutter_count",
	"gutter_persistence",
	"gutter_mean_persistence",
	"column_count_stability",
	"mean_cells_per_row",
	"max_cells_per_row",

	// --- Row structure.
	"row_height_regularity",
	"row_gap_regularity",
	"row_count",

	// --- Rules.
	"ruling_coverage",
	"vertical_rule_count",
	"horizontal_rule_count",

	// --- Content.
	"math_frac",
	"digit_frac",
	"upper_frac",
	"mean_line_width_frac",
	"line_width_variance",
	"left_align_frac", // fraction of lines sharing the modal left edge
	"font_size_mean",
	"font_size_spread",

	// --- What the line model made of the lines inside. This was the single
	// strongest feature group in the previous region model (frac_Background had
	// the highest gain of any feature), and it is what makes the
	// equation-versus-table arbitration learned rather than ordered.
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

	// --- Ink. The proposer can now see what the page draws, and "one image" and
	// "four hundred paths" are different kinds of region.
	"ink_objects",
	"ink_images",
	"ink_paths",
	"ink_shadings",
	"ink_is_single",
	"ink_density", // ink objects per 1000 square points

	// --- Provenance. Text candidates and ink candidates fail in different
	// ways, and the model should be allowed to learn that rather than being
	// told to treat them alike.
	"source_text",
	"source_ink",
	"source_ink_text",
	"atomic_span",
}

// ProposalFeatureContract returns a copy of the contract, for the trainer.
func ProposalFeatureContract() []string {
	return append([]string(nil), ProposalFeatureNames...)
}

// proposalLabelOrder fixes which frac_* slot each class occupies. It must match
// the frac_ block of ProposalFeatureNames exactly.
var proposalLabelOrder = []string{
	"Background", "Caption", "Footnote", "Formula", "List-item", "Page-footer",
	"Page-header", "Picture", "Section-header", "Table", "Text", "Title",
}

// ProposalFeatureInput is everything the feature extractor needs about a page.
// Bundling it keeps the argument list from growing without limit as features
// are added, and keeps the emitter and the pipeline passing the same thing.
type ProposalFeatureInput struct {
	Lines   []ParagraphTextLine
	Labels  []string // line-model class per line, parallel to Lines
	Cells   []page.TextCell
	Rulings []page.RulingSegment
	Size    geom.Size

	// gutters is built lazily from Cells on first use and shared by every
	// candidate on the page. The per-candidate gutter computation was 722us a
	// call through doctable.ColumnGapCandidates — times ~375 candidates that
	// was 271 of the 291 ms/page this stage cost.
	gutters *gutterIndex
}

// gutterIndexFor returns the page's gutter index, building it once.
func (in *ProposalFeatureInput) gutterIndexFor() *gutterIndex {
	if in.gutters == nil {
		in.gutters = newGutterIndex(in.Cells)
	}
	return in.gutters
}

// ProposalFeatures describes one candidate numerically.
func ProposalFeatures(proposal RegionProposal, in *ProposalFeatureInput) []float64 {
	pageW := nonZeroValue(in.Size.Width)
	pageH := nonZeroValue(in.Size.Height)
	box := proposal.Box
	top, bottom := topEdgeOf(box), bottomEdgeOf(box)
	height := math.Abs(bottom - top)
	width := box.Width()

	member := make([]ParagraphTextLine, 0, len(proposal.Lines))
	for _, index := range proposal.Lines {
		if index >= 0 && index < len(in.Lines) {
			member = append(member, in.Lines[index])
		}
	}

	gutterCount, bestPersistence, meanPersistence := in.gutterIndexFor().gutterFeatures(box)

	cellCounts := make([]float64, 0, len(member))
	heights := make([]float64, 0, len(member))
	widths := make([]float64, 0, len(member))
	lefts := make([]float64, 0, len(member))
	fontSizes := make([]float64, 0, len(member))
	tops := make([]float64, 0, len(member))
	mathChars, digitChars, upperChars, totalChars := 0, 0, 0, 0
	for _, line := range member {
		cellCounts = append(cellCounts, float64(len(line.Cells)))
		heights = append(heights, line.BBox.Height())
		widths = append(widths, line.BBox.Width()/pageW)
		lefts = append(lefts, line.BBox.L)
		fontSizes = append(fontSizes, line.FontSize)
		tops = append(tops, topEdgeOf(line.BBox))
		counts := countLineRunes(line)
		mathChars += counts.math
		digitChars += counts.digit
		upperChars += counts.upper
		totalChars += counts.chars
	}
	sort.Float64s(tops)
	rowGaps := make([]float64, 0, len(tops))
	for i := 1; i < len(tops); i++ {
		rowGaps = append(rowGaps, tops[i]-tops[i-1])
	}

	rulingLength, verticalRules, horizontalRules := 0.0, 0, 0
	for _, ruling := range in.Rulings {
		x0, x1 := math.Min(ruling.FromX, ruling.ToX), math.Max(ruling.FromX, ruling.ToX)
		y0, y1 := math.Min(ruling.FromY, ruling.ToY), math.Max(ruling.FromY, ruling.ToY)
		if x1 < box.L || x0 > box.R || y1 < top || y0 > bottom {
			continue
		}
		rulingLength += math.Hypot(x1-x0, y1-y0)
		thickness := math.Max(2, ruling.Width)
		if y1-y0 > height*0.5 && x1-x0 <= thickness {
			verticalRules++
		}
		if x1-x0 > width*0.5 && y1-y0 <= thickness {
			horizontalRules++
		}
	}

	medianHeight := medianOf(heights)
	if medianHeight <= 0 {
		medianHeight = 1
	}
	above, below := verticalIsolation(box, in.Lines, proposal.Lines)

	features := []float64{
		float64(len(proposal.Lines)),
		width / pageW,
		height / pageH,
		(width * height) / (pageW * pageH),
		width / nonZeroValue(height),
		box.CenterX() / pageW,
		box.CenterY() / pageH,
		box.L / pageW,
		box.R / pageW,

		math.Min(above/medianHeight, 20),
		math.Min(below/medianHeight, 20),
		boolFeature(box.L <= minLineLeft(in.Lines)+2),
		boolFeature(box.R >= maxLineRight(in.Lines)-2),

		gutterCount,
		bestPersistence,
		meanPersistence,
		stability(cellCounts),
		meanOf(cellCounts),
		maxOf(cellCounts),

		stability(heights),
		stability(rowGaps),
		float64(len(member)),

		rulingLength / nonZeroValue(2*(width+height)),
		float64(verticalRules),
		float64(horizontalRules),

		fractionOf(mathChars, totalChars),
		fractionOf(digitChars, totalChars),
		fractionOf(upperChars, totalChars),
		meanOf(widths),
		varianceOf(widths),
		modalShare(lefts, 2),
		meanOf(fontSizes),
		spreadOf(fontSizes),
	}

	counts := map[string]int{}
	for _, index := range proposal.Lines {
		if index >= 0 && index < len(in.Labels) {
			counts[in.Labels[index]]++
		}
	}
	for _, class := range proposalLabelOrder {
		features = append(features, fractionOf(counts[class], len(proposal.Lines)))
	}

	area := math.Max(width*height, 1)
	features = append(features,
		float64(proposal.Ink.Objects),
		float64(proposal.Ink.Images),
		float64(proposal.Ink.Paths),
		float64(proposal.Ink.Shadings),
		boolFeature(proposal.Ink.Single),
		float64(proposal.Ink.Ink)*1000/area,

		boolFeature(proposal.Source == ProposalText),
		boolFeature(proposal.Source == ProposalInk),
		boolFeature(proposal.Source == ProposalInkText),
		float64(proposal.AtomicSpan),
	)
	return features
}

// verticalIsolation measures the whitespace immediately above and below the
// candidate: the distance to the nearest line that is NOT part of it and does
// overlap it horizontally.
//
// The horizontal test matters. A line in the opposite column is not evidence
// about whether this candidate is bounded, and counting it would report every
// candidate on a two-column page as tightly packed.
func verticalIsolation(box geom.Box, lines []ParagraphTextLine, members []int) (above, below float64) {
	inside := make(map[int]bool, len(members))
	for _, index := range members {
		inside[index] = true
	}
	top, bottom := topEdgeOf(box), bottomEdgeOf(box)
	above, below = math.Inf(1), math.Inf(1)
	for i, line := range lines {
		if inside[i] || horizontalOverlap(box, line.BBox) <= 0 {
			continue
		}
		lineTop, lineBottom := topEdgeOf(line.BBox), bottomEdgeOf(line.BBox)
		if lineBottom <= top {
			above = math.Min(above, top-lineBottom)
		}
		if lineTop >= bottom {
			below = math.Min(below, lineTop-bottom)
		}
	}
	// Nothing above means the candidate reaches the top of the content, which
	// is maximal isolation rather than none. Infinity is clamped by the caller.
	if math.IsInf(above, 1) {
		above = math.Inf(1)
	}
	return above, below
}

func minLineLeft(lines []ParagraphTextLine) float64 {
	best := math.Inf(1)
	for _, line := range lines {
		best = math.Min(best, line.BBox.L)
	}
	if math.IsInf(best, 1) {
		return 0
	}
	return best
}

func maxLineRight(lines []ParagraphTextLine) float64 {
	best := math.Inf(-1)
	for _, line := range lines {
		best = math.Max(best, line.BBox.R)
	}
	if math.IsInf(best, -1) {
		return 0
	}
	return best
}

// modalShare returns the fraction of values falling in the most popular bucket
// of the given width — how many lines share a left edge, for flush-left prose.
func modalShare(values []float64, bucket float64) float64 {
	if len(values) == 0 || bucket <= 0 {
		return 0
	}
	counts := map[int]int{}
	best := 0
	for _, value := range values {
		key := int(math.Round(value / bucket))
		counts[key]++
		if counts[key] > best {
			best = counts[key]
		}
	}
	return float64(best) / float64(len(values))
}

func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func spreadOf(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	return standardDeviation(values)
}
