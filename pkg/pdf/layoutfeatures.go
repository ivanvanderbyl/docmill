package pdf

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Layout feature extraction for the learned line classifier
// (docs/plans/2026-08-06-learned-layout-classifier.md, Task 2).
//
// Everything here MEASURES; nothing decides. The existing heuristics keep their
// verdicts for now — this file is the numeric view of the same signals, so the
// model can weigh them jointly instead of in an arbitrary firing order.
//
// The vector's order and meaning are a CONTRACT between the Python trainer and
// the Go predictor. LayoutFeatureNames is the single definition of that
// contract: the trainer reads it out of the binary rather than restating it, so
// a reordering here cannot silently shift an index under the model. Appending a
// feature invalidates any model trained before the change; the length check in
// the predictor is what turns that into a loud failure instead of a quiet one.

// LayoutFeatureNames is the ordered feature contract. Index i of a vector
// returned by LineLayoutFeatures is the feature named at index i here.
//
// Grouping is by what the feature measures, not by importance:
//   - page-relative geometry (where the line sits and how big it is)
//   - spacing (how it is set off from its neighbours)
//   - cell structure (what the line is made of)
//   - font
//   - list structure
//   - page context (rules nearby, repetition across pages)
//   - lexical (deliberately last and deliberately a minority — see the ablation
//     requirement in Task 2 step 2)
var LayoutFeatureNames = []string{
	// Page-relative geometry.
	"font_size_ratio",    // line font size / page dominant font size
	"height_ratio",       // line height / page median line height
	"width_frac",         // line width / page width
	"left_frac",          // line left / page width
	"right_gap_frac",     // (page width - line right) / page width
	"center_offset_frac", // |line centre x - page centre x| / page width
	"y_center_frac",      // line centre y / page height
	"page_index_frac",    // page index / page count

	// Spacing.
	"gap_above_ratio", // gap to the previous line / line height
	"gap_below_ratio", // gap to the next line / line height

	// Cell structure.
	"cell_count",          // source text cells on the line
	"baseline_count",      // distinct rounded cell bottoms
	"baseline_dispersion", // stddev of cell bottoms / line height
	"size_max_med_ratio",  // max cell font size / median cell font size
	"char_count",          // non-space characters
	"mean_char_width",     // line width / characters / line font size

	// Font.
	"italic_frac",
	"bold_frac",

	// List structure. These three are the gap the DocLayNet run diagnosed:
	// List-item scored worst of every class (0.588 F1) precisely because a list
	// item is defined by a leading marker and a hanging indent, and the spike
	// feature set measured neither.
	"has_list_marker",     // 1 when the line opens with a bullet/ordered marker
	"numbering_depth",     // dotted depth of a leading numeric marker: "4.3.1" -> 3
	"content_left_offset", // (content left - line left) / page width; the hanging indent
	"indent_vs_body",      // (line left - page dominant body left) / page width

	// Page context.
	"stroke_density",   // ruling segments near the line, per unit of line width
	"repeat_frac",      // fraction of pages carrying an equivalent box at this position
	"column_span_frac", // line width / dominant body text width

	// Lexical — a deliberate minority of the vector.
	"math_frac",
	"digit_frac",
	"letter_frac",
	"upper_frac",
	"punct_frac",
	"trailing_period", // 1 when the line ends in sentence punctuation
	"caption_marker",  // 1 when the line opens "<word> <number>" (Figure 3, Tabelle 2)
}

// PageLayoutContext holds the per-page normalisers a feature vector is
// expressed against. Nothing in the vector is an absolute point measurement, so
// a line means the same thing on A4 and on Letter.
type PageLayoutContext struct {
	Size             geom.Size
	PageIndex        int
	PageCount        int
	DominantFontSize float64
	MedianLineHeight float64
	BodyLeft         float64
	BodyWidth        float64
	Rulings          []page.RulingSegment
	// Repeat counts a normalised box signature across the document; a running
	// header or footer occupies the same slot on most pages and nothing else
	// does. Supplied by DocumentLayoutContext, nil for a single page.
	Repeat *DocumentLayoutContext
}

// DocumentLayoutContext carries the only genuinely cross-page signal in the
// vector: whether an equivalent box recurs at the same position on other pages.
// It is what separates a running header from a section heading that happens to
// sit near the top of its page.
type DocumentLayoutContext struct {
	pages      int
	signatures map[int64]int
}

// NewDocumentLayoutContext builds the repetition index from every page's lines.
func NewDocumentLayoutContext(pageLines [][]ParagraphTextLine, sizes []geom.Size) *DocumentLayoutContext {
	doc := &DocumentLayoutContext{pages: len(pageLines), signatures: map[int64]int{}}
	for i, lines := range pageLines {
		if i >= len(sizes) {
			break
		}
		// Count each signature at most once per page: a page with three lines
		// in the same slot must not look like three pages' worth of evidence.
		seen := map[int64]bool{}
		for _, line := range lines {
			signature := boxSignature(line.BBox, sizes[i])
			if !seen[signature] {
				seen[signature] = true
				doc.signatures[signature]++
			}
		}
	}
	return doc
}

// boxSignature quantises a box's page-relative position into a single key.
// The grid is coarse (2% of the page) so that a header drifting a point
// between pages still lands in the same bucket.
func boxSignature(box geom.Box, size geom.Size) int64 {
	if size.Width <= 0 || size.Height <= 0 {
		return 0
	}
	x := int64(math.Round(box.L / size.Width * 50))
	y := int64(math.Round(topEdgeOf(box) / size.Height * 50))
	w := int64(math.Round(box.Width() / size.Width * 50))
	return x*1_000_000 + y*1_000 + w
}

// fraction reports how much of the document carries an equivalent box here.
//
// It returns 0 for a single-page document, and that is a correctness fix rather
// than a guard: repetition ACROSS pages is undefined with one page, and the
// naive answer (1/1 = 1.0) would say "this box repeats on every page" about
// every line on the page. Training corpora of single-page PDFs — DocLayNet is
// exactly this — would then learn from a constant 1.0 that becomes a real,
// varying signal at inference on multi-page documents. That is training/serving
// skew of the quietest kind. Reporting 0 keeps the feature constant and
// therefore simply unused by such a model, instead of actively misleading.
func (d *DocumentLayoutContext) fraction(box geom.Box, size geom.Size) float64 {
	if d == nil || d.pages < 2 {
		return 0
	}
	return float64(d.signatures[boxSignature(box, size)]) / float64(d.pages)
}

// NewPageLayoutContext computes a page's normalisers from its cells and lines.
func NewPageLayoutContext(size geom.Size, cells []page.TextCell, lines []ParagraphTextLine, rulings []page.RulingSegment, pageIndex, pageCount int) PageLayoutContext {
	left, width := bodyTextMetrics(lines)
	return PageLayoutContext{
		Size:             size,
		PageIndex:        pageIndex,
		PageCount:        pageCount,
		DominantFontSize: dominantCellFontSize(cells),
		MedianLineHeight: medianLineHeight(lines),
		BodyLeft:         left,
		BodyWidth:        width,
		Rulings:          rulings,
	}
}

// dominantCellFontSize is the character-weighted median font size on the page —
// the body-text size on a prose page, and the normaliser every size ratio
// divides by. Weighting by characters rather than by cell stops a page of
// headings from redefining "normal".
func dominantCellFontSize(cells []page.TextCell) float64 {
	type sample struct {
		size   float64
		weight int
	}
	samples := make([]sample, 0, len(cells))
	total := 0
	for _, cell := range cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || cell.FontSize <= 0 {
			continue
		}
		weight := len([]rune(text))
		samples = append(samples, sample{size: cell.FontSize, weight: weight})
		total += weight
	}
	if total == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].size < samples[j].size })
	seen := 0
	for _, s := range samples {
		if seen += s.weight; seen*2 >= total {
			return s.size
		}
	}
	return samples[len(samples)-1].size
}

func medianLineHeight(lines []ParagraphTextLine) float64 {
	heights := make([]float64, 0, len(lines))
	for _, line := range lines {
		if h := line.BBox.Height(); h > 0 {
			heights = append(heights, h)
		}
	}
	if len(heights) == 0 {
		return 0
	}
	sort.Float64s(heights)
	return heights[len(heights)/2]
}

// bodyTextMetrics returns the page's dominant left edge and text width, taken
// as the median over lines. They are the reference an indent is measured
// against: "indented" only means anything relative to where body text starts.
func bodyTextMetrics(lines []ParagraphTextLine) (left, width float64) {
	if len(lines) == 0 {
		return 0, 0
	}
	lefts := make([]float64, 0, len(lines))
	widths := make([]float64, 0, len(lines))
	for _, line := range lines {
		lefts = append(lefts, line.BBox.L)
		widths = append(widths, line.BBox.Width())
	}
	sort.Float64s(lefts)
	sort.Float64s(widths)
	return lefts[len(lefts)/2], widths[len(widths)/2]
}

// LineLayoutFeatures computes the feature vector for one assembled line. prev
// and next are the vertically adjacent lines on the same page (nil at the page
// edges). The returned slice always has len(LayoutFeatureNames) entries in that
// order.
func LineLayoutFeatures(line ParagraphTextLine, prev, next *ParagraphTextLine, ctx PageLayoutContext) []float64 {
	box := line.BBox
	height := box.Height()
	width := box.Width()

	pageW := nonZeroValue(ctx.Size.Width)
	pageH := nonZeroValue(ctx.Size.Height)

	text := strings.TrimSpace(line.Text)
	counts := countLineRunes(line)

	fontRatio := 0.0
	if ctx.DominantFontSize > 0 && line.FontSize > 0 {
		fontRatio = line.FontSize / ctx.DominantFontSize
	}
	heightRatio := 0.0
	if ctx.MedianLineHeight > 0 {
		heightRatio = height / ctx.MedianLineHeight
	}
	meanCharWidth := 0.0
	if counts.chars > 0 && line.FontSize > 0 {
		meanCharWidth = width / float64(counts.chars) / line.FontSize
	}
	pageIndexFrac := 0.0
	if ctx.PageCount > 0 {
		pageIndexFrac = float64(ctx.PageIndex) / float64(ctx.PageCount)
	}

	contentOffset := 0.0
	if line.ListCandidate && line.ListContentL > box.L {
		contentOffset = (line.ListContentL - box.L) / pageW
	}
	columnSpan := 0.0
	if ctx.BodyWidth > 0 {
		columnSpan = width / ctx.BodyWidth
	}

	marker, depth := leadingMarkerShape(text)

	return []float64{
		fontRatio,
		heightRatio,
		width / pageW,
		box.L / pageW,
		(pageW - box.R) / pageW,
		math.Abs(box.CenterX()-pageW/2) / pageW,
		box.CenterY() / pageH,
		pageIndexFrac,

		neighbourGapRatio(line, prev, height, true),
		neighbourGapRatio(line, next, height, false),

		float64(len(line.Cells)),
		float64(counts.baselines),
		standardDeviation(counts.bottoms) / nonZeroValue(height),
		counts.sizeRatio,
		float64(counts.chars),
		meanCharWidth,

		fractionOf(counts.italic, counts.chars),
		fractionOf(counts.bold, counts.chars),

		boolFeature(line.ListCandidate || marker),
		float64(depth),
		contentOffset,
		(box.L - ctx.BodyLeft) / pageW,

		strokeDensityNear(box, ctx.Rulings) / nonZeroValue(width),
		ctx.Repeat.fraction(box, ctx.Size),
		columnSpan,

		fractionOf(counts.math, counts.chars),
		fractionOf(counts.digit, counts.chars),
		fractionOf(counts.letter, counts.chars),
		fractionOf(counts.upper, counts.letter),
		fractionOf(counts.punct, counts.chars),
		boolFeature(endsWithSentencePunctuation(text)),
		boolFeature(hasCaptionMarkerShape(text)),
	}
}

type lineRuneCounts struct {
	chars, italic, bold, math, digit, letter, upper, punct int
	baselines                                              int
	bottoms                                                []float64
	sizeRatio                                              float64
}

func countLineRunes(line ParagraphTextLine) lineRuneCounts {
	out := lineRuneCounts{sizeRatio: 1}
	for _, word := range line.Words {
		n := 0
		for _, r := range word.Value {
			if unicode.IsSpace(r) {
				continue
			}
			n++
			switch {
			case isMathRune(r):
				out.math++
			case unicode.IsDigit(r):
				out.digit++
			case unicode.IsPunct(r):
				out.punct++
			}
			if unicode.IsLetter(r) {
				out.letter++
				if unicode.IsUpper(r) {
					out.upper++
				}
			}
		}
		out.chars += n
		if word.Italic {
			out.italic += n
		}
		if word.Bold {
			out.bold += n
		}
	}

	// Cell-level geometry: how many distinct baselines the "line" really spans
	// and how far its cells' bottoms scatter. A stacked fraction or a
	// sub/superscripted equation scatters; a prose line does not.
	baselines := map[int]struct{}{}
	sizes := make([]float64, 0, len(line.Cells))
	for _, cell := range line.Cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		bottom := math.Max(cell.Box.T, cell.Box.B)
		out.bottoms = append(out.bottoms, bottom)
		baselines[int(math.Round(bottom))] = struct{}{}
		if cell.FontSize > 0 {
			sizes = append(sizes, cell.FontSize)
		}
	}
	out.baselines = len(baselines)
	if len(sizes) > 0 {
		sort.Float64s(sizes)
		if median := sizes[len(sizes)/2]; median > 0 {
			out.sizeRatio = sizes[len(sizes)-1] / median
		}
	}
	return out
}

// neighbourGapRatio measures the whitespace between this line and its neighbour
// in units of this line's own height. Display equations, headings and captions
// are all set off by extra leading; prose lines are not. A missing neighbour
// (a page edge) reports a large sentinel rather than zero, because "nothing
// above me" is not "touching what is above me".
func neighbourGapRatio(line ParagraphTextLine, neighbour *ParagraphTextLine, height float64, above bool) float64 {
	if neighbour == nil {
		return 10
	}
	var gap float64
	if above {
		gap = topEdgeOf(line.BBox) - bottomEdgeOf(neighbour.BBox)
	} else {
		gap = topEdgeOf(neighbour.BBox) - bottomEdgeOf(line.BBox)
	}
	return math.Max(-2, math.Min(10, gap/nonZeroValue(height)))
}

// strokeDensityNear returns the length of ruling segments running along or just
// outside the line's box. It is the signal that a "line" is really a table row
// or sits under a rule, and it is the one the plan warns the line vector may
// capture poorly (see the Task 6 note on stroke clustering).
func strokeDensityNear(box geom.Box, rulings []page.RulingSegment) float64 {
	if len(rulings) == 0 {
		return 0
	}
	top, bottom := topEdgeOf(box), bottomEdgeOf(box)
	margin := math.Max(2, box.Height())
	total := 0.0
	for _, ruling := range rulings {
		y0 := math.Min(ruling.FromY, ruling.ToY)
		y1 := math.Max(ruling.FromY, ruling.ToY)
		if y1 < top-margin || y0 > bottom+margin {
			continue
		}
		x0 := math.Min(ruling.FromX, ruling.ToX)
		x1 := math.Max(ruling.FromX, ruling.ToX)
		if x1 < box.L || x0 > box.R {
			continue
		}
		total += math.Hypot(x1-x0, y1-y0)
	}
	return total
}

// leadingMarkerShape reports whether the line opens with a list marker and how
// deep any dotted numbering runs ("4.3.1" -> 3).
//
// This is a STRUCTURAL test, not a word test: it looks at the shape of the
// leading token (digits and separators, or a single bullet glyph), never at
// which words a document uses. AGENTS.md permits character cues only as
// supporting evidence inside a broader layout algorithm, which is exactly the
// role they play as two features among thirty-two.
func leadingMarkerShape(text string) (marker bool, depth int) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false, 0
	}
	token := strings.TrimRight(fields[0], ".)")
	if token == "" {
		return false, 0
	}

	if len([]rune(token)) == 1 {
		r := []rune(token)[0]
		if isBulletRune(r) {
			return true, 0
		}
	}

	segments := strings.Split(token, ".")
	numeric := 0
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		allDigits := true
		for _, r := range segment {
			if !unicode.IsDigit(r) {
				allDigits = false
				break
			}
		}
		if !allDigits {
			return false, 0
		}
		numeric++
	}
	if numeric == 0 {
		return false, 0
	}
	return true, numeric
}

func isBulletRune(r rune) bool {
	switch r {
	case '•', '·', '‣', '▪', '◦', '-', '–', '—', '*', '+':
		return true
	}
	return false
}

// hasCaptionMarkerShape reports the "<word> <number>" opening shared by
// "Figure 3", "Table 2", "Tabelle 2" and "図 3". Matching the SHAPE rather than
// an English keyword list keeps the feature usable on documents this project
// has never seen, which is what the plan's English-bias caveat asks for.
func hasCaptionMarkerShape(text string) bool {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return false
	}
	for _, r := range fields[0] {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	second := strings.TrimRight(fields[1], ".:)")
	if second == "" {
		return false
	}
	for _, r := range second {
		if !unicode.IsDigit(r) && r != '.' {
			return false
		}
	}
	return true
}

func endsWithSentencePunctuation(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	switch []rune(trimmed)[len([]rune(trimmed))-1] {
	case '.', '!', '?', '。', '！', '？':
		return true
	}
	return false
}

// isMathRune reports whether r is typographically mathematical: an operator, a
// Greek letter, a math-alphanumeric glyph, or one of the ASCII symbols carrying
// mathematical meaning. A script/category test rather than a symbol list, so it
// holds across languages.
func isMathRune(r rune) bool {
	switch {
	case unicode.Is(unicode.Sm, r):
		return true
	case unicode.Is(unicode.Greek, r):
		return true
	case r >= 0x2200 && r <= 0x22FF, // mathematical operators
		r >= 0x2A00 && r <= 0x2AFF, // supplemental operators
		r >= 0x27C0 && r <= 0x27EF, // miscellaneous symbols-A
		r >= 0x1D400 && r <= 0x1D7FF, // mathematical alphanumerics
		r >= 0x2070 && r <= 0x209F: // super/subscripts
		return true
	}
	switch r {
	case '=', '+', '<', '>', '±', '×', '÷', '∑', '∫', '√', '∞', '≈', '≤', '≥', '≠', '/', '^', '_':
		return true
	}
	return false
}

// topEdgeOf and bottomEdgeOf return a box's edges as TOP-LEFT-origin values —
// smaller y nearer the top — whatever origin the box records. docmill's text
// cells are already TopLeft (TextRectsToCells), so this is a guard rather than
// a conversion, but it keeps every vertical feature origin-independent.
func topEdgeOf(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.B
	}
	return box.T
}

func bottomEdgeOf(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.T
	}
	return box.B
}

func standardDeviation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	sum := 0.0
	for _, v := range values {
		sum += (v - mean) * (v - mean)
	}
	return math.Sqrt(sum / float64(len(values)))
}

func fractionOf(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func boolFeature(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func nonZeroValue(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
