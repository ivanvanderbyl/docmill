package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func structureTestCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index: index,
		Text:  text,
		Box:   geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

func headingTestLine(text string, l, t, r, b, fontSize float64, cells ...page.TextCell) ParagraphTextLine {
	if len(cells) == 0 {
		cells = []page.TextCell{{
			Index: 0, Text: text, FontSize: fontSize,
			Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
		}}
	}
	return ParagraphTextLine{
		Text:     text,
		FontSize: fontSize,
		BBox:     geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
		Cells:    cells,
	}
}

// --- running-header guard ---

// A numbered section heading that happens to open a page must not be mistaken
// for a running header: its only digits are the section marker.
func TestNumberedSectionAtPageTopIsNotRunningHeader(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	line := headingTestLine("12. EQUIVOCATION AND CHANNEL CAPACITY", 72, 60, 400, 72, 10)

	require.False(t, looksLikeRunningHeader(collapseSpaces(line.Text), line, size))
}

// NEGATIVE: a genuine running header carries volume/page furniture digits
// beyond any leading marker, and is still rejected.
func TestJournalRunningHeaderStillRejected(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	line := headingTestLine("IEEE TRANSACTIONS ON INFORMATION THEORY, VOL. 44, NO. 6", 72, 40, 500, 50, 8)

	require.True(t, looksLikeRunningHeader(collapseSpaces(line.Text), line, size))
}

// --- decimal-heading shape guards ---

func TestDecimalHeadingTextShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"caps section above twenty", "21. ENTROPY OF AN ENSEMBLE OF FUNCTIONS", true},
		{"ordinary numbered section", "9. THE FUNDAMENTAL THEOREM", true},
		{"list item ending in a period", "1. Zero-order approximation (symbols independent).", false},
		{"lead-in ending in a colon", "3. Absolute error criterion:", false},
		{"equation debris, no real word", "2 S N", false},
		{"equation debris with operators", "1 (W 2 + W 4", false},
		{"title-case list item above twenty", "34. Some Ordinary List Entry", false},
	} {
		require.Equal(t, tc.want, isDecimalHeadingText(tc.text), tc.name)
	}
}

// --- italic all-caps guard ---

// Display mathematics sets single-letter variables as italic capitals, which
// mostlyUppercase reads as an all-caps line. Italic dominance rejects it.
func TestItalicDominantAllCapsLineIsNotStructuralHeading(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	cells := []page.TextCell{{
		Index: 0, Text: "FN = NGN (N 1)GN1", FontSize: 10, FontName: "Times-Italic",
		Box: geom.Box{L: 200, T: 300, R: 380, B: 312, Origin: geom.TopLeft},
	}}
	line := headingTestLine("FN = NGN (N 1)GN1", 200, 300, 380, 312, 10, cells...)

	require.True(t, lineMostlyItalic(line))
	require.False(t, isStructuralHeadingLine(line, size))
}

// NEGATIVE: an upright all-caps section title at the margin is still promoted.
func TestUprightAllCapsSectionTitleRemainsStructuralHeading(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	cells := []page.TextCell{{
		Index: 0, Text: "THEOREMS ON ERGODIC SOURCES", FontSize: 10, FontName: "Times-Roman",
		Box: geom.Box{L: 72, T: 300, R: 300, B: 312, Origin: geom.TopLeft},
	}}
	line := headingTestLine("THEOREMS ON ERGODIC SOURCES", 72, 300, 300, 312, 10, cells...)

	require.False(t, lineMostlyItalic(line))
	require.True(t, isStructuralHeadingLine(line, size))
}

// An all-caps SENTENCE (typeset sample text) ends in a period; a title does not.
func TestAllCapsSentenceIsNotStructuralHeading(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	line := headingTestLine("OCRO HLI RGWR NMIELWIS EU LL NBNESEBYA TH.", 72, 300, 400, 312, 10)

	require.False(t, isStructuralHeadingLine(line, size))
}

// --- caption-anchored figure regions ---

func figureRuling(fromX, fromY, toX, toY float64) page.RulingSegment {
	return page.RulingSegment{FromX: fromX, FromY: fromY, ToX: toX, ToY: toY, Width: 1, Origin: geom.TopLeft}
}

// A cluster of strokes with a "Fig." caption directly beneath is a figure; its
// internal labels are suppressed while the caption and body prose survive.
func TestFigureRegionSuppressesInternalLabelsOnly(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	rulings := []page.RulingSegment{
		figureRuling(100, 200, 400, 200),
		figureRuling(100, 200, 100, 320),
		figureRuling(400, 200, 400, 320),
		figureRuling(100, 320, 400, 320),
	}
	label := structureTestCell(1, "TRANSMITTER", 150, 240, 260, 252)
	caption := structureTestCell(2, "Fig. 1—Schematic diagram of a general communication system.", 100, 330, 470, 342)
	prose := structureTestCell(3, "A decimal digit is about three bits, and a digit wheel on a desk computing machine has ten stable positions.", 72, 400, 540, 412)

	regions := figureRegions([]page.TextCell{label, caption, prose}, rulings, size)
	require.Len(t, regions, 1)

	kept := dropCellsInFigureRegions([]page.TextCell{label, caption, prose}, regions)
	texts := make([]string, 0, len(kept))
	for _, cell := range kept {
		texts = append(texts, cell.Text)
	}
	require.Equal(t, []string{caption.Text, prose.Text}, texts)
}

// NEGATIVE: the same strokes with no figure caption beneath are not a figure
// region (a ruled table's strokes must not swallow its cells).
func TestStrokeClusterWithoutFigureCaptionIsNotFigureRegion(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	rulings := []page.RulingSegment{
		figureRuling(100, 200, 400, 200),
		figureRuling(100, 200, 100, 320),
		figureRuling(400, 200, 400, 320),
		figureRuling(100, 320, 400, 320),
	}
	cells := []page.TextCell{
		structureTestCell(1, "Gain", 150, 240, 200, 252),
		structureTestCell(2, "Table I — Entropy power gain.", 100, 330, 400, 342),
	}

	require.Empty(t, figureRegions(cells, rulings, size))
}

// NEGATIVE: a caption under a single short rule (an underline, not a drawing)
// is below the minimum stroke count and area.
func TestSingleRuleUnderCaptionIsNotFigureRegion(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	rulings := []page.RulingSegment{figureRuling(100, 300, 180, 300)}
	cells := []page.TextCell{structureTestCell(1, "Fig. 4—A caption.", 100, 310, 300, 322)}

	require.Empty(t, figureRegions(cells, rulings, size))
}

// --- exclusive re-extraction ---

// With ExclusiveReextract the re-extractor is authoritative for every group,
// including single-cell ones, and a group it returns empty for is dropped
// rather than falling back to the member text (which would duplicate glyphs
// already claimed by an earlier cell).
func TestMergeFragmentedCellsExclusiveReextractDropsClaimedGroups(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		structureTestCell(1, "first", 0, 0, 40, 10),
		structureTestCell(2, "second", 200, 0, 260, 10),
	}
	claimed := false
	reextract := func(geom.Box) string {
		if claimed {
			return ""
		}
		claimed = true
		return "first"
	}

	merged := MergeFragmentedCells(cells, reextract, MergeOptions{ExclusiveReextract: true})
	require.Len(t, merged, 1)
	require.Equal(t, "first", merged[0].Text)
}

// NEGATIVE: without the flag, an empty re-extraction still falls back to the
// member cells' own text and no cell is dropped.
func TestMergeFragmentedCellsNonExclusiveKeepsFallbackText(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		structureTestCell(1, "first", 0, 0, 40, 10),
		structureTestCell(2, "second", 200, 0, 260, 10),
	}

	merged := MergeFragmentedCells(cells, func(geom.Box) string { return "" }, MergeOptions{})
	require.Len(t, merged, 2)
	require.Equal(t, "first", merged[0].Text)
	require.Equal(t, "second", merged[1].Text)
}
