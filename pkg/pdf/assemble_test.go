package pdf_test

import (
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	"github.com/stretchr/testify/require"
)

// extractBody runs ExtractMarkdownWithOptions with table detection disabled so
// the cells flow straight through the paragraph assembler.
func extractBody(t *testing.T, cells []page.TextCell) string {
	t.Helper()
	doc := fakeDocument{pages: []fakePage{{cells: cells}}}
	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{DetectTables: false})
	require.NoError(t, err)
	return got
}

func TestAssembleParagraphsMergesTightlySpacedLines(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Line one", 0, 0, 60, 10),
		pdfTextCell(2, "Line two", 0, 12, 60, 22),
		pdfTextCell(3, "Line three", 0, 24, 60, 34),
	}

	got := extractBody(t, cells)

	require.Equal(t, "Line one Line two Line three", got)
}

func TestAssembleParagraphsDehyphenatesAdjacentAlphaLines(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "The inter-", 0, 0, 70, 10),
		pdfTextCell(2, "national team published findings.", 0, 12, 190, 22),
	}

	got := extractBody(t, cells)

	require.Equal(t, "The international team published findings.", got)
}

func TestAssembleParagraphsNormalisesUnicodeLigatures(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "A ﬁ eld-speciﬁ c method", 0, 0, 140, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "A field-specific method", got)
}

func TestAssembleParagraphsDropsZeroWidthFormatRunes(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "\u200bFirst finding\ufeff", 0, 0, 100, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "First finding", got)
}

func TestAssembleParagraphsBreaksOnFullBlankLine(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "First paragraph", 0, 0, 80, 10),
		pdfTextCell(2, "Second paragraph", 0, 30, 80, 40),
	}

	got := extractBody(t, cells)

	require.Equal(t, "First paragraph\n\nSecond paragraph", got)
}

func TestAssembleParagraphsJoinsMultiCellLineLeftToRight(t *testing.T) {
	t.Parallel()

	// Two cells on the same line (same vertical centre), supplied right-to-left,
	// must join left-to-right.
	cells := []page.TextCell{
		pdfTextCell(2, "world", 50, 0, 90, 10),
		pdfTextCell(1, "Hello", 0, 0, 40, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "Hello world", got)
}

func TestAssembleParagraphsCompactsTightStandaloneHyphenBetweenWords(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "AI", 0, 0, 12, 10, 10),
		pdfTextCellWithFont(2, "-", 13, 4, 16, 6, 10),
		pdfTextCellWithFont(3, "enabled", 17, 0, 58, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "AI-enabled", got)
}

func TestAssembleParagraphsKeepsVisuallySeparatedHyphenSpaced(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "task", 0, 0, 24, 10, 10),
		pdfTextCellWithFont(2, "-", 36, 4, 39, 6, 10),
		pdfTextCellWithFont(3, "for example", 51, 0, 120, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "task - for example", got)
}

func TestAssembleParagraphsCompactsTightPossessiveApostrophe(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "models", 0, 0, 38, 10, 10),
		pdfTextCellWithFont(2, "’", 39, 0, 41, 5, 10),
		pdfTextCellWithFont(3, "already", 42, 0, 84, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "models' already", got)
}

func TestAssembleParagraphsCompactsTightApostropheSuffixAfterNumber(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "4.6", 0, 0, 18, 10, 10),
		pdfTextCellWithFont(2, "’", 19, 0, 21, 5, 10),
		pdfTextCellWithFont(3, "s", 22, 0, 28, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "4.6's", got)
}

func TestAssembleParagraphsKeepsRaisedApostropheWithBaselineLine(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "cause greater harm.", 0, 0, 84, 12, 11),
		pdfTextCellWithFont(2, "This is true given Vendor Alpha Preview", 88, 0, 300, 12, 11),
		pdfTextCellWithFont(3, "’", 302, 0, 304, 3, 11),
		pdfTextCellWithFont(4, "s", 305, 3.5, 310, 8.3, 11),
		pdfTextCellWithFont(5, "exceptional strengths.", 0, 16, 130, 28, 11),
	}

	got := extractBody(t, cells)

	require.Equal(t, "cause greater harm. This is true given Vendor Alpha Preview's exceptional strengths.", got)
}

func TestAssembleParagraphsRendersGeneratedHyphenAsVisibleHyphen(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Hen\x02", 0, 0, 50, 10),
		pdfTextCell(2, "nigen", 0, 12, 50, 22),
	}

	got := extractBody(t, cells)

	require.Equal(t, "Hen- nigen", got)
}

func TestAssembleParagraphsRemovesSpaceBeforePunctuation(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "arXiv:2303.00980", 0, 0, 80, 10),
		pdfTextCell(2, ".", 82, 0, 84, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "arXiv:2303.00980.", got)
}

func TestAssembleParagraphsCompactsSpaceAfterDecimalPoint(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Vendor Gamma 4.", 0, 0, 70, 10),
		pdfTextCell(2, "6 improved from 61.", 72, 0, 160, 10),
		pdfTextCell(3, "5% to 69.", 162, 0, 220, 10),
		pdfTextCell(4, "1%", 222, 0, 238, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "Vendor Gamma 4.6 improved from 61.5% to 69.1%", got)
}

func TestAssembleParagraphsCompactsTightPeriodSuffix(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "example", 0, 0, 42, 10, 10),
		pdfTextCellWithFont(2, ".", 43, 7, 45, 9, 10),
		pdfTextCellWithFont(3, "org", 46, 0, 64, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "example.org", got)
}

func TestAssembleParagraphsKeepsVisuallySeparatedPeriodSpaced(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "The finding", 0, 0, 60, 10, 10),
		pdfTextCellWithFont(2, ".", 61, 7, 63, 9, 10),
		pdfTextCellWithFont(3, "Next", 78, 0, 102, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "The finding. Next", got)
}

func TestAssembleParagraphsKeepsWordSpaceAfterSentencePeriod(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "The finding", 0, 0, 60, 10, 11),
		pdfTextCellWithFont(2, ".", 61, 7, 62.3, 9, 11),
		pdfTextCellWithFont(3, "Next", 65.9, 0, 90, 10, 11),
	}

	got := extractBody(t, cells)

	require.Equal(t, "The finding. Next", got)
}

func TestAssembleParagraphsCompactsTightAlphaFragments(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "w", 0, 2, 8.7, 8, 11),
		pdfTextCellWithFont(2, "hich model", 9.0, 0, 70, 10, 11),
	}

	got := extractBody(t, cells)

	require.Equal(t, "which model", got)
}

func TestAssembleParagraphsKeepsWordGapBetweenAlphaCells(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "frontier", 0, 0, 42, 10, 10),
		pdfTextCellWithFont(2, "model", 47, 0, 78, 10, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "frontier model", got)
}

func TestAssembleParagraphsKeepsSpaceBetweenAdjacentPunctuation(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Project No:", 0, 0, 60, 10),
		pdfTextCell(2, ":", 62, 0, 64, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "Project No: :", got)
}

func TestAssembleParagraphsCompactsSplitApostropheInsideWord(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "other", 0, 0, 36, 10),
		pdfTextCell(2, "’", 38, 0, 40, 10),
		pdfTextCell(3, "s sketches", 42, 0, 90, 10),
	}

	got := extractBody(t, cells)

	require.Equal(t, "other's sketches", got)
}

func TestAssembleParagraphsDropsEmptyCells(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Visible line", 0, 0, 80, 10),
		pdfTextCell(2, "   ", 0, 12, 80, 22),
		pdfTextCell(3, "", 0, 24, 80, 34),
		pdfTextCell(4, "Another line", 0, 36, 80, 46),
	}

	got := extractBody(t, cells)

	// The two blank cells are dropped; the remaining lines are at normal leading
	// (gap 26 vs height 10 between visible and another), so they break into two
	// paragraphs because the blank gap exceeds the threshold.
	require.Equal(t, "Visible line\n\nAnother line", got)
}
