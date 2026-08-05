package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func marginalTestCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{Index: index, Text: text, Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}}
}

func TestSplitMarginalPageNumberCellsExtractsBottomMarginDigit(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	cells := []page.TextCell{
		marginalTestCell(1, "body text near the bottom of the page", 92, 700, 400, 710),
		// Standalone page number centred in the bottom margin.
		marginalTestCell(2, "3", 303, 737, 308, 747),
	}

	remaining, marginal := splitMarginalPageNumberCells(cells, size)

	require.Len(t, marginal, 1)
	require.Equal(t, "3", marginal[0].Text)
	require.Len(t, remaining, 1)
	require.Equal(t, 1, remaining[0].Index)
}

func TestSplitMarginalPageNumberCellsKeepsBodyDigitsAndSharedLines(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 612, Height: 792}
	cells := []page.TextCell{
		// A digit in the body of the page is data, not furniture.
		marginalTestCell(1, "3", 303, 400, 308, 410),
		// A digit sharing its line with other text (e.g. a footer sentence) is
		// handled by the existing trailing-page-number split, not here.
		marginalTestCell(2, "Journal of Examples", 92, 737, 250, 747),
		marginalTestCell(3, "17", 500, 737, 510, 747),
	}

	remaining, marginal := splitMarginalPageNumberCells(cells, size)

	require.Empty(t, marginal)
	require.Len(t, remaining, 3)
}
