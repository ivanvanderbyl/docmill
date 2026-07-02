package pdf_test

import (
	"context"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	"github.com/stretchr/testify/require"
)

// structureDoc builds a single-page fakeDocument from cells where each cell is
// vertically separated enough to assemble into its own paragraph block, so list
// detection can be exercised block-by-block.
func structureDoc(texts ...string) fakeDocument {
	cells := make([]page.TextCell, 0, len(texts))
	top := 0.0
	index := 1
	for _, text := range texts {
		// 10pt-tall lines separated by a 40pt gap (>> 0.6*height) so each line
		// becomes its own paragraph block.
		if marker, rest, ok := splitTestListLine(text); ok {
			cells = append(cells,
				pdfTextCellWithFont(index, marker, 0, top+2, 12, top+8, 10),
				pdfTextCellWithFont(index+1, rest, 28, top, 220, top+10, 10),
			)
			index += 2
		} else {
			cells = append(cells, pdfTextCell(index, text, 0, top, 200, top+10))
			index++
		}
		top += 50
	}
	return fakeDocument{pages: []fakePage{{cells: cells}}}
}

func splitTestListLine(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, marker := range []string{"•", "●", "○", "◦", "▪", "‣", "·", "–", "—", "-", "*"} {
		if !strings.HasPrefix(trimmed, marker) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, marker)
		if rest == "" {
			return "", "", false
		}
		r, _ := utf8.DecodeRuneInString(rest)
		if !unicode.IsSpace(r) {
			return "", "", false
		}
		rest = strings.TrimSpace(rest)
		return marker, rest, rest != ""
	}
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 9 || digits >= len(trimmed) {
		return "", "", false
	}
	delim, width := utf8.DecodeRuneInString(trimmed[digits:])
	if delim != '.' && delim != ')' {
		return "", "", false
	}
	rest := trimmed[digits+width:]
	if rest == "" {
		return "", "", false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	return trimmed[:digits+width], rest, rest != ""
}

func extractStructured(t *testing.T, doc fakeDocument) string {
	t.Helper()
	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectStructure: true,
	})
	require.NoError(t, err)
	return got
}

func TestDetectStructureRewritesBulletListItems(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"• First point",
		"• Second point",
		"• Third point",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "- First point\n\n- Second point\n\n- Third point", got)
}

func TestExtractMarkdownRewritesAlignedBulletCellsByDefault(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{pages: []fakePage{{
		size: geom.Size{Width: 420, Height: 240},
		cells: []page.TextCell{
			pdfTextCellWithFont(1, "●", 72, 40, 78, 46, 10),
			pdfTextCellWithFont(2, "First finding", 96, 38, 210, 48, 10),
			pdfTextCellWithFont(3, "●", 72, 54, 78, 60, 10),
			pdfTextCellWithFont(4, "Second finding", 96, 52, 220, 62, 10),
			pdfTextCellWithFont(5, "●", 72, 68, 78, 74, 10),
			pdfTextCellWithFont(6, "Third finding", 96, 66, 210, 76, 10),
		},
	}}}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "- First finding\n\n- Second finding\n\n- Third finding", got)
}

func TestExtractMarkdownMergesWrappedAlignedBulletItemsByDefault(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{pages: []fakePage{{
		size: geom.Size{Width: 420, Height: 240},
		cells: []page.TextCell{
			pdfTextCellWithFont(1, "●", 72, 40, 78, 46, 10),
			pdfTextCellWithFont(2, "First finding starts here", 96, 38, 260, 48, 10),
			pdfTextCellWithFont(3, "and continues on the next line", 96, 52, 290, 62, 10),
			pdfTextCellWithFont(4, "●", 72, 70, 78, 76, 10),
			pdfTextCellWithFont(5, "Second finding", 96, 68, 220, 78, 10),
		},
	}}}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "- First finding starts here and continues on the next line\n\n- Second finding", got)
}

func TestExtractMarkdownKeepsProminentListItemOutOfHeadings(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{pages: []fakePage{{
		size: geom.Size{Width: 420, Height: 240},
		cells: []page.TextCell{
			pdfTextCellWithFont(1, "Body paragraph establishes the prose size.", 72, 20, 320, 30, 10),
			pdfTextCellWithFont(2, "●", 72, 58, 80, 66, 18),
			pdfTextCellWithFont(3, "Prominent list item", 96, 54, 260, 72, 18),
			pdfTextCellWithFont(4, "●", 72, 92, 80, 100, 18),
			pdfTextCellWithFont(5, "Second prominent item", 96, 88, 290, 106, 18),
		},
	}}}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "- Prominent list item")
	require.Contains(t, got, "- Second prominent item")
	require.NotContains(t, got, "# Prominent list item")
}

func TestExtractMarkdownRewritesNestedBulletContextByGeometry(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{pages: []fakePage{{
		size: geom.Size{Width: 420, Height: 180},
		cells: []page.TextCell{
			pdfTextCellWithFont(1, "●", 72, 40, 78, 46, 10),
			pdfTextCellWithFont(2, "Parent list item", 96, 38, 210, 48, 10),
			pdfTextCellWithFont(3, "○", 96, 56, 102, 62, 10),
			pdfTextCellWithFont(4, "Nested list item", 120, 54, 240, 64, 10),
		},
	}}}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "- Parent list item\n\n- Nested list item", got)
}

func TestDetectStructureRewritesOrderedListItems(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"1. First",
		"2. Second",
		"3. Third",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "1. First\n\n2. Second\n\n3. Third", got)
}

func TestDetectStructureNormalisesOrderedDelimiterAndPreservesNumber(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"4) Fourth",
		"10) Tenth",
	)

	got := extractStructured(t, doc)

	// ")" is normalised to ".", but the original number is preserved.
	require.Equal(t, "4. Fourth\n\n10. Tenth", got)
}

func TestDetectStructureRewritesDashAndEmDashBullets(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"– en-dash item",
		"— em-dash item",
		"- hyphen item",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "- en-dash item\n\n- em-dash item\n\n- hyphen item", got)
}

func TestDetectStructureLeavesParagraphsThenListIntact(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"This is an introductory paragraph.",
		"• A bullet",
		"• Another bullet",
		"Trailing paragraph.",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "This is an introductory paragraph.\n\n- A bullet\n\n- Another bullet\n\nTrailing paragraph.", got)
}

func TestDetectStructureDoesNotTreatHyphenatedWordStartAsList(t *testing.T) {
	t.Parallel()

	// A hyphen or em-dash that is part of a word (no following space) must not
	// be mistaken for a bullet marker.
	doc := structureDoc(
		"-based approaches are common in this field.",
		"—however, exceptions exist.",
		"3.14 is an approximation of pi.",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "-based approaches are common in this field.\n\n—however, exceptions exist.\n\n3.14 is an approximation of pi.", got)
}

func TestDetectStructureDoesNotRewriteIsolatedBulletLookingParagraph(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"Introductory paragraph.",
		"• A single decorated callout.",
		"Trailing paragraph.",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "Introductory paragraph.\n\n• A single decorated callout.\n\nTrailing paragraph.", got)
}

func TestDetectStructureGuardsEmptyAfterMarker(t *testing.T) {
	t.Parallel()

	// A marker with nothing meaningful after it stays a paragraph. (Assembly
	// trims trailing whitespace, so a bare "•" arrives marker-only.)
	doc := structureDoc(
		"•",
		"1.",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "•\n\n1.", got)
}

func TestDetectStructureSingleLetterMarkersStayParagraphs(t *testing.T) {
	t.Parallel()

	// Single-letter "a." / "b)" markers overwhelmingly mark figure sub-captions
	// in the corpus, so they are intentionally left as paragraphs.
	doc := structureDoc(
		"a. Picture of a table",
		"b) Structure predicted",
	)

	got := extractStructured(t, doc)

	require.Equal(t, "a. Picture of a table\n\nb) Structure predicted", got)
}

func TestDetectStructureCanBeDisabled(t *testing.T) {
	t.Parallel()

	doc := structureDoc(
		"• First point",
		"• Second point",
	)

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{})
	require.NoError(t, err)

	// Explicit options can leave DetectStructure disabled, so bullets are untouched.
	require.Equal(t, "• First point\n\n• Second point", got)
}
