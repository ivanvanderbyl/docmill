package pdf_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	"github.com/stretchr/testify/require"
)

func TestTextRectsToCellsConvertsPDFiumRectsToTopLeft(t *testing.T) {
	t.Parallel()

	cells := pdf.TextRectsToCells([]pdf.TextRect{
		{Text: "Hello", Left: 10, Top: 190, Right: 50, Bottom: 170, FontSize: 18},
		{Text: "World", Left: 10, Top: 160, Right: 60, Bottom: 140, FontSize: 10},
	}, 200)

	require.Len(t, cells, 2)
	require.Equal(t, 0, cells[0].Index)
	require.Equal(t, "Hello", cells[0].Text)
	require.Equal(t, 18.0, cells[0].FontSize)
	require.Equal(t, geom.Box{L: 10, T: 10, R: 50, B: 30, Origin: geom.TopLeft}, cells[0].Box)
	require.Equal(t, 10.0, cells[1].FontSize)
	require.Equal(t, geom.Box{L: 10, T: 40, R: 60, B: 60, Origin: geom.TopLeft}, cells[1].Box)
}

func TestTextRectsToCellsDropsEmptyRects(t *testing.T) {
	t.Parallel()

	cells := pdf.TextRectsToCells([]pdf.TextRect{
		{Text: "  ", Left: 10, Top: 190, Right: 50, Bottom: 170},
		{Text: "Kept", Left: 10, Top: 160, Right: 60, Bottom: 140},
	}, 200)

	require.Len(t, cells, 1)
	require.Equal(t, 0, cells[0].Index)
	require.Equal(t, "Kept", cells[0].Text)
}

func TestTextRectsToCellsDropsFormatOnlyRects(t *testing.T) {
	t.Parallel()

	cells := pdf.TextRectsToCells([]pdf.TextRect{
		{Text: "\u200b", Left: 10, Top: 190, Right: 13, Bottom: 190, FontSize: 11},
		{Text: "\ufeff", Left: 14, Top: 190, Right: 17, Bottom: 190, FontSize: 11},
		{Text: "Kept", Left: 10, Top: 160, Right: 60, Bottom: 140, FontSize: 11},
	}, 200)

	require.Len(t, cells, 1)
	require.Equal(t, "Kept", cells[0].Text)
	require.Equal(t, 0, cells[0].Index)
}

func TestTextRectsToCellsDropsDegenerateFormatArtifacts(t *testing.T) {
	t.Parallel()

	cells := pdf.TextRectsToCells([]pdf.TextRect{
		{Text: "Abstract\u200b", Left: 10, Top: 90, Right: 70, Bottom: 80, FontSize: 11},
		{Text: "t\u200b", Left: 70, Top: 90, Right: 73, Bottom: 89.99, FontSize: 11},
		{Text: ".", Left: 73, Top: 88.6, Right: 75, Bottom: 87.2, FontSize: 11},
	}, 100)

	require.Len(t, cells, 2)
	require.Equal(t, "Abstract\u200b", cells[0].Text)
	require.Equal(t, ".", cells[1].Text)
	require.Equal(t, 1, cells[1].Index)
}

func TestTopLeftBoxToPDFiumBoundsConvertsToBottomLeft(t *testing.T) {
	t.Parallel()

	bounds := pdf.TopLeftBoxToPDFiumBounds(geom.Box{L: 10, T: 20, R: 40, B: 60, Origin: geom.TopLeft}, 100)

	require.Equal(t, pdf.TextRect{Left: 10, Top: 80, Right: 40, Bottom: 40}, bounds)
}

func TestTopLeftBoxToPDFiumBoundsAcceptsBottomLeftInput(t *testing.T) {
	t.Parallel()

	bounds := pdf.TopLeftBoxToPDFiumBounds(geom.Box{L: 10, T: 80, R: 40, B: 40, Origin: geom.BottomLeft}, 100)

	require.Equal(t, pdf.TextRect{Left: 10, Top: 80, Right: 40, Bottom: 40}, bounds)
}
