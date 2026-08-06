package main

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
)

// featureNames is THE contract between this emitter and benchmarks/layout/spike/train.py.
// The Python trainer asserts this exact order (it is written into the JSONL sidecar
// meta record), and the Go predictor builds its vector from the same slice, so an
// index shift cannot go unnoticed. Task 2 will promote this into pkg/pdf; for the
// spike it lives here because the whole tool is throwaway.
var featureNames = []string{
	"font_size_ratio",     // line font size / page dominant font size
	"height_ratio",        // line box height / page median line height
	"width_frac",          // line width / page width
	"left_frac",           // line left / page width
	"right_gap_frac",      // (page width - line right) / page width
	"center_offset_frac",  // |line centre x - page centre x| / page width
	"y_center_frac",       // line centre y / page height
	"gap_above_ratio",     // vertical gap to previous line / line height
	"gap_below_ratio",     // vertical gap to next line / line height
	"cell_count",          // source text cells on the line
	"baseline_count",      // distinct rounded cell bottoms on the line
	"baseline_dispersion", // stddev of cell bottoms / line height
	"size_max_med_ratio",  // max cell font size / median cell font size
	"italic_frac",         // italic characters / characters
	"bold_frac",           // bold characters / characters
	"math_frac",           // mathematical characters / characters
	"digit_frac",          // digits / characters
	"letter_frac",         // letters / characters
	"char_count",          // characters on the line
	"mean_char_width",     // line width / characters / line font size
}

// pageContext carries the per-page normalisers the line features are expressed
// against: nothing here is an absolute point measurement, so a feature vector
// means the same thing on A4 and on Letter.
type pageContext struct {
	size            geom.Size
	dominantSize    float64
	medianLineHight float64
}

func newPageContext(size geom.Size, cells []page.TextCell, lines []textline.ParagraphTextLine) pageContext {
	return pageContext{
		size:            size,
		dominantSize:    dominantFontSize(cells),
		medianLineHight: medianLineHeight(lines),
	}
}

// dominantFontSize is the character-weighted median font size on the page — the
// body-text size on a prose page, and the normaliser every size ratio divides by.
func dominantFontSize(cells []page.TextCell) float64 {
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
		seen += s.weight
		if seen*2 >= total {
			return s.size
		}
	}
	return samples[len(samples)-1].size
}

func medianLineHeight(lines []textline.ParagraphTextLine) float64 {
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

// lineFeatures computes the spike feature vector for one assembled line.
// prev and next are the vertically adjacent lines on the same page (nil at the
// page edges), which is where the gap features come from.
func lineFeatures(line textline.ParagraphTextLine, prev, next *textline.ParagraphTextLine, ctx pageContext) []float64 {
	box := line.BBox
	height := box.Height()
	width := box.Width()

	pageW := ctx.size.Width
	pageH := ctx.size.Height
	if pageW <= 0 {
		pageW = 1
	}
	if pageH <= 0 {
		pageH = 1
	}

	// Character-level counters walk the words rather than line.Text so the
	// italic/bold fractions weigh by how much text carries the attribute.
	var chars, italicChars, boldChars, mathChars, digitChars, letterChars int
	for _, word := range line.Words {
		runes := []rune(word.Value)
		n := 0
		for _, r := range runes {
			if unicode.IsSpace(r) {
				continue
			}
			n++
			if isMathRune(r) {
				mathChars++
			}
			if unicode.IsDigit(r) {
				digitChars++
			}
			if unicode.IsLetter(r) {
				letterChars++
			}
		}
		chars += n
		if word.Italic {
			italicChars += n
		}
		if word.Bold {
			boldChars += n
		}
	}

	// Cell-level geometry: how many distinct baselines the "line" really spans
	// and how far its cells' bottoms scatter. A stacked fraction or a
	// sub/superscripted equation scatters; a prose line does not.
	bottoms := make([]float64, 0, len(line.Cells))
	sizes := make([]float64, 0, len(line.Cells))
	baselines := map[int]struct{}{}
	for _, cell := range line.Cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		bottom := math.Max(cell.Box.T, cell.Box.B)
		bottoms = append(bottoms, bottom)
		baselines[int(math.Round(bottom))] = struct{}{}
		if cell.FontSize > 0 {
			sizes = append(sizes, cell.FontSize)
		}
	}

	fontRatio := 0.0
	if ctx.dominantSize > 0 && line.FontSize > 0 {
		fontRatio = line.FontSize / ctx.dominantSize
	}
	heightRatio := 0.0
	if ctx.medianLineHight > 0 {
		heightRatio = height / ctx.medianLineHight
	}

	sizeRatio := 1.0
	if len(sizes) > 0 {
		sort.Float64s(sizes)
		median := sizes[len(sizes)/2]
		if median > 0 {
			sizeRatio = sizes[len(sizes)-1] / median
		}
	}

	meanCharWidth := 0.0
	if chars > 0 && line.FontSize > 0 {
		meanCharWidth = width / float64(chars) / line.FontSize
	}

	return []float64{
		fontRatio,
		heightRatio,
		width / pageW,
		box.L / pageW,
		(pageW - box.R) / pageW,
		math.Abs(box.CenterX()-pageW/2) / pageW,
		box.CenterY() / pageH,
		gapRatio(line, prev, height, true),
		gapRatio(line, next, height, false),
		float64(len(line.Cells)),
		float64(len(baselines)),
		dispersion(bottoms) / nonZero(height),
		sizeRatio,
		frac(italicChars, chars),
		frac(boldChars, chars),
		frac(mathChars, chars),
		frac(digitChars, chars),
		frac(letterChars, chars),
		float64(chars),
		meanCharWidth,
	}
}

// gapRatio measures the whitespace between this line and its neighbour in units
// of this line's own height. Display equations are set off by extra leading;
// prose lines are not. Missing neighbours (page edges) report a large sentinel
// rather than zero, because "nothing above me" is not "touching what is above me".
func gapRatio(line textline.ParagraphTextLine, neighbour *textline.ParagraphTextLine, height float64, above bool) float64 {
	if neighbour == nil {
		return 10
	}
	var gap float64
	if above {
		gap = math.Min(line.BBox.T, line.BBox.B) - math.Max(neighbour.BBox.T, neighbour.BBox.B)
	} else {
		gap = math.Min(neighbour.BBox.T, neighbour.BBox.B) - math.Max(line.BBox.T, line.BBox.B)
	}
	return math.Max(-2, math.Min(10, gap/nonZero(height)))
}

func dispersion(values []float64) float64 {
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

func frac(n, total int) float64 {
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

// isMathRune reports whether r is typographically mathematical: an operator, a
// Greek letter, a math-alphanumeric glyph, or one of the ASCII symbols that
// carry mathematical meaning. This is a script/category test, not a keyword
// list, so it holds across languages.
func isMathRune(r rune) bool {
	switch {
	case unicode.Is(unicode.Sm, r): // math symbols
		return true
	case unicode.Is(unicode.Greek, r):
		return true
	case r >= 0x2200 && r <= 0x22FF: // mathematical operators
		return true
	case r >= 0x2A00 && r <= 0x2AFF: // supplemental mathematical operators
		return true
	case r >= 0x27C0 && r <= 0x27EF: // miscellaneous mathematical symbols-A
		return true
	case r >= 0x1D400 && r <= 0x1D7FF: // mathematical alphanumeric symbols
		return true
	case r >= 0x2070 && r <= 0x209F: // super/subscripts
		return true
	}
	switch r {
	case '=', '+', '<', '>', '±', '×', '÷', '∑', '∫', '√', '∞', '≈', '≤', '≥', '≠', '/', '^', '_':
		return true
	}
	return false
}
