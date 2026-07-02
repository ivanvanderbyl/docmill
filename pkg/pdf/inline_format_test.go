package pdf_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	"github.com/stretchr/testify/require"
)

// boldFontCell builds a text cell whose /BaseFont name marks it bold, so the
// LineElement run-splitter segments it into a bold run (font metadata, not text
// patterns).
func boldFontCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index:    index,
		Text:     text,
		FontSize: 10,
		FontName: "Helvetica-Bold",
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

// regularFontCell builds a plain (non-bold, non-italic, proportional) cell.
func regularFontCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index:    index,
		Text:     text,
		FontSize: 10,
		FontName: "Helvetica",
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

// monospaceFontCell builds a fixed-pitch cell (so IsMonospace reports true),
// the signal for an inline code run.
func monospaceFontCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index:     index,
		Text:      text,
		FontSize:  10,
		FontName:  "Courier",
		FontFlags: page.FontFlagFixedPitch,
		Box:       geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

func TestInlineFormattingWrapsBoldRunWhenEnabled(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				boldFontCell(1, "Bold", 0, 0, 30, 10),
				regularFontCell(2, "plain", 32, 0, 70, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder:           true,
		EnableInlineFormatting: true,
	})

	require.NoError(t, err)
	require.Equal(t, "**Bold** plain", got)
}

func TestInlineFormattingWrapsMonospaceRunAsCodeWhenEnabled(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				regularFontCell(1, "run", 0, 0, 24, 10),
				monospaceFontCell(2, "code", 26, 0, 60, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder:           true,
		EnableInlineFormatting: true,
	})

	require.NoError(t, err)
	require.Equal(t, "run `code`", got)
}

// TestInlineFormattingPreservesWithinRunGlyphCompaction guards that the opt-in
// formatted path rebuilds run text with the same geometry-aware glyph compaction
// the default path uses: a tight bold run "Sec"+"3" must render "**Sec3**", not
// "**Sec 3**". Driven by cell geometry/font, not text patterns.
func TestInlineFormattingPreservesWithinRunGlyphCompaction(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				boldFontCell(1, "Sec", 0, 0, 30, 10),
				boldFontCell(2, "3", 31, 0, 38, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder:           true,
		EnableInlineFormatting: true,
	})

	require.NoError(t, err)
	require.Equal(t, "**Sec3**", got)
}

func TestInlineFormattingDisabledEmitsNoMarkers(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				boldFontCell(1, "Bold", 0, 0, 30, 10),
				regularFontCell(2, "plain", 32, 0, 70, 10),
				monospaceFontCell(3, "code", 72, 0, 106, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder:           true,
		EnableInlineFormatting: false,
	})

	require.NoError(t, err)
	require.NotContains(t, got, "*")
	require.NotContains(t, got, "`")
	require.Equal(t, "Bold plain code", got)
}

// TestInlineFormattingDefaultConstructorStaysOff guards that the default
// ExtractMarkdown constructor leaves EnableInlineFormatting off, so bold/code
// fonts render as plain text (no markers) under the default path.
func TestInlineFormattingDefaultConstructorStaysOff(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				boldFontCell(1, "Bold", 0, 0, 30, 10),
				regularFontCell(2, "plain", 32, 0, 70, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.False(t, strings.Contains(got, "**"))
}
