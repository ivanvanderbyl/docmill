package pdf

// Display-equation / tall-glyph line-segmentation tests.
//
// Mechanism these guard (measured on real scientific PDFs): big math
// delimiters (parens, brackets, integral signs) are emitted as their own
// text cells with boxes 2-3x taller than the surrounding text band, spanning
// vertically THROUGH neighbouring prose lines. During line clustering such a
// cell inflates the line's vertical band, and the height-overlap fallback
// then absorbs the next prose line into the same visual "line", interleaving
// equation glyphs with prose left-to-right.
//
// Invariant encoded: a visual line's vertical extent is defined by its
// dominant glyph band; oversized delimiter glyphs may belong to a line but
// must not widen the corridor used to decide line membership. Geometry from
// entropy.pdf page 22 (the C = Max(H(x) - Hy(x)) display) and page 37.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

func mathTestCell(index int, text string, l, t, r, b, fontSize float64) page.TextCell {
	return page.TextCell{
		Index:    index,
		Text:     text,
		FontSize: fontSize,
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

const proseAboveEquation = "when the source is properly matched to the channel. We therefore define the channel capacity by"
const proseBelowEquation = "where the maximum is with respect to all possible information sources used as input to the channel. If the"

// displayEquationPage reproduces entropy.pdf p22: a prose line, a display
// equation whose big parens have 30pt-tall boxes reaching down through the
// following prose line, then that prose line.
func displayEquationPage() []page.TextCell {
	return []page.TextCell{
		mathTestCell(0, proseAboveEquation, 91.92, 152.95, 477.70, 161.95, 10),
		mathTestCell(1, "C = Max", 255.48, 174.18, 292.75, 184.14, 10),
		mathTestCell(2, "\x01", 292.68, 173.10, 297.24, 202.98, 0.12), // big "("
		mathTestCell(3, "H (x)", 297.24, 174.18, 317.28, 184.14, 10),
		mathTestCell(4, "Hy(x)", 327.96, 174.18, 350.64, 184.67, 10),
		mathTestCell(5, "\x01", 350.64, 173.10, 355.20, 202.98, 0.12), // big ")"
		mathTestCell(6, proseBelowEquation, 91.92, 196.75, 519.16, 205.75, 10),
	}
}

// The prose line below a display equation must come out intact and contiguous
// as its own line: no equation glyph inside it, none of its words pulled into
// the equation line.
func TestTallDelimiterDoesNotBridgeEquationIntoProse(t *testing.T) {
	t.Parallel()

	lines := AssembleLineElements(displayEquationPage(), 4)

	var proseLine, equationLine string
	proseIndex, equationIndex := -1, -1
	for i, line := range lines {
		if strings.Contains(line.Text, "where the maximum") {
			proseLine = line.Text
			proseIndex = i
		}
		if strings.Contains(line.Text, "C = Max") {
			equationLine = line.Text
			equationIndex = i
		}
	}

	require.NotEqual(t, -1, proseIndex, "prose line missing entirely")
	require.NotEqual(t, -1, equationIndex, "equation line missing entirely")
	require.NotEqual(t, proseIndex, equationIndex,
		"equation and prose merged into one line: %q", proseLine)
	require.Equal(t, proseBelowEquation, proseLine,
		"prose line no longer contiguous")
	require.NotContains(t, equationLine, "where",
		"prose words pulled into the equation line")
	require.Less(t, equationIndex, proseIndex,
		"equation must precede the prose that follows it on the page")
}

// NEGATIVE: a prose line with a raised footnote-marker superscript AND a tall
// delimiter is one visual line. The tall cell is a genuine height outlier, so
// this reaches splitTallBridgedGroup's re-clustering; the marker and the
// prose cells share one baseline cluster, so no split may happen and the
// marker must stay in place.
func TestFootnoteSuperscriptStaysInProseLine(t *testing.T) {
	t.Parallel()

	// Prose geometry measured from entropy.pdf p22 (the "R1" marker): base
	// band 391.75-400.75, 7.4pt marker at 394.97-401.63. The delimiter box
	// mirrors the 29.88pt-tall CM parens.
	cells := []page.TextCell{
		mathTestCell(0, "at a higher rate than R", 91.92, 391.75, 221.86, 400.75, 10),
		mathTestCell(1, "1", 221.88, 394.97, 225.58, 401.63, 7.4), // raised marker
		mathTestCell(2, ", then there will necessarily be", 225.96, 391.75, 480.00, 400.75, 10),
		mathTestCell(3, "\x01", 482.00, 390.50, 486.56, 420.38, 0.12), // tall delimiter outlier
	}

	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1, "prose line with marker and tall delimiter split apart")
	require.Equal(t, "at a higher rate than R1 , then there will necessarily be \x01", lines[0].Text)
}

// NEGATIVE: a tall delimiter attached to a SINGLE baseline (inline math inside
// one prose line, nothing beneath) must stay in that line — the split only
// applies when the tall cell is bridging two distinct baselines.
func TestTallDelimiterStaysWithSingleBaseline(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		mathTestCell(0, "where J", 116.88, 663.91, 148.08, 672.91, 10),
		mathTestCell(1, "\x01", 148.56, 662.22, 153.12, 692.10, 0.12),
		mathTestCell(2, "is the Jacobian of the transformation.", 165.84, 663.91, 519.66, 672.91, 10),
	}

	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1, "tall delimiter detached from its only baseline")
}

// A tall inline construct (the p37 Jacobian's parens) bridging into the NEXT
// prose line: the following line must come out intact.
func TestInlineTallConstructDoesNotCaptureNextProseLine(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		mathTestCell(0, "where J", 116.88, 663.91, 148.08, 672.91, 10),
		mathTestCell(1, "\x01", 148.56, 662.22, 153.12, 692.10, 0.12),
		mathTestCell(2, "x", 154.32, 661.73, 157.61, 668.39, 7.4),
		mathTestCell(3, "y", 154.32, 669.17, 157.61, 675.83, 7.4),
		mathTestCell(4, "\x01", 158.76, 662.22, 163.32, 692.10, 0.12),
		mathTestCell(5, "is the Jacobian of the coordinate transformation.", 165.84, 663.91, 519.66, 672.91, 10),
		mathTestCell(6, "ing the variables to x1", 116.88, 675.91, 203.26, 685.79, 10),
	}

	lines := AssembleLineElements(cells, 4)

	var proseLine string
	for _, line := range lines {
		if strings.Contains(line.Text, "ing the variables") {
			require.Empty(t, proseLine, "prose duplicated across lines")
			proseLine = line.Text
		}
	}
	require.Equal(t, "ing the variables to x1", proseLine,
		"following prose line polluted or shredded")
}

// An inline fraction (numerator above the baseline, denominator below, both
// inside the horizontal gap the fraction occupies in the prose line) reads in
// place and in column order: numerator before denominator, at the fraction's
// x-position — never hoisted to the start of the line or left dangling as
// stray one-glyph lines. Geometry from entropy.pdf p16 (Theorem 9's C/H).
func TestInlineFractionReadsInPlaceNumeratorFirst(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		mathTestCell(0, "rate", 91.56, 371.23, 108.05, 380.23, 10),
		mathTestCell(1, "C", 110.88, 364.51, 117.55, 373.51, 10), // numerator
		mathTestCell(2, "H", 110.64, 378.07, 117.86, 387.07, 10), // denominator
		mathTestCell(3, "symbols per second over the channel", 136.44, 371.23, 311.65, 380.23, 10),
	}

	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1, "fraction parts left as stray lines")
	require.Equal(t, "rate C H symbols per second over the channel", lines[0].Text,
		"fraction must read in place, numerator before denominator")
}

// NEGATIVE: a big operator's limit line sits BELOW the base line but under the
// operator glyph itself (inside a base cell's horizontal span, not in a gap
// between cells). It must stay its own line, after the base line — not be
// hoisted into it. Geometry from entropy.pdf p16 (Bi = ∑ BjW... with "s; j"
// under the summation sign).
func TestSummationLimitsStayBelowBaseLine(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		mathTestCell(0, "Bi =", 189.24, 217.36, 214.00, 227.32, 10),
		mathTestCell(1, "∑ BjW", 216.48, 217.36, 260.00, 236.12, 10),
		mathTestCell(2, "s; j", 218.16, 234.42, 231.24, 241.74, 7.32), // limits under the ∑
	}

	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 2, "limits must remain their own line")
	require.Equal(t, "Bi = ∑ BjW", lines[0].Text)
	require.Equal(t, "s; j", lines[1].Text, "limits hoisted into the base line")
}

// NEGATIVE (blocker regression guard): two table rows must never be folded
// into one line with their columns interleaved. The second row is populated
// only in interior columns — each of its cells x-overlaps a column of the row
// above (a "column mate") and sits between other cells of that row — and one
// cell of the upper row carries a taller box, so the rows' box unions
// interpenetrate. The rows still do not interpenetrate each other's TEXT
// bands, which is what must keep the fold shut.
func TestTableRowsAreNeverInterleaved(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		mathTestCell(0, "Alpha", 100, 100, 150, 110, 10),
		mathTestCell(1, "Beta", 200, 100, 250, 116, 10), // taller box in the header row
		mathTestCell(2, "Gamma", 300, 100, 350, 110, 10),
		mathTestCell(3, "Delta", 400, 100, 450, 110, 10),
		mathTestCell(4, "12", 210, 112, 240, 122, 10),
		mathTestCell(5, "34", 310, 112, 340, 122, 10),
	}

	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 2, "table rows merged into one line")
	require.Equal(t, "Alpha Beta Gamma Delta", lines[0].Text)
	require.Equal(t, "12 34", lines[1].Text)
}

// NEGATIVE: an ordinary short line below a line with interior gaps is never
// folded into it. Both fragments here reach stackedFragmentFitsGap (the
// target has several cells): the margin-aligned closing line of a paragraph
// fails because it cuts into the upper line's cell spans, and the gap-aligned
// footer word fails because it does not interpenetrate the text band of the
// cells flanking the gap.
func TestShortParagraphLineIsNotFolded(t *testing.T) {
	t.Parallel()

	closing := []page.TextCell{
		mathTestCell(0, "By proper assignment", 91.92, 264.19, 250.00, 273.19, 10),
		mathTestCell(1, "of the transition probabilities", 260.00, 264.19, 519.00, 273.19, 10),
		mathTestCell(2, "mized at the channel capacity.", 91.92, 276.19, 224.00, 285.19, 10),
	}
	lines := AssembleLineElements(closing, 4)
	require.Len(t, lines, 2, "paragraph closing line folded into the line above")

	gapAligned := []page.TextCell{
		mathTestCell(0, "Left header", 91.92, 100.00, 200.00, 109.00, 10),
		mathTestCell(1, "right header", 350.00, 100.00, 519.00, 109.00, 10),
		mathTestCell(2, "42", 260.00, 112.00, 280.00, 121.00, 10), // in the gap, normal spacing
	}
	lines = AssembleLineElements(gapAligned, 4)
	require.Len(t, lines, 2, "gap-aligned line folded into the line above")
}
