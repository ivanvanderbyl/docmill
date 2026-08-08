package pdf

import (
	"strings"
	"testing"

	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

// A spanned cell in a cross-page-stitched table must contribute its text once,
// at its anchor slot, exactly as render.Table does — Grid() places the same
// cell in every slot it covers, and reading each slot verbatim duplicates the
// text across the rows/columns the span covers.
func TestAppendTableRowsEmitsSpannedCellTextOnce(t *testing.T) {
	t.Parallel()

	upper := doctable.Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []doctable.Cell{
			{Text: "Tall", RowSpan: 2, ColSpan: 1, StartRow: 0, EndRow: 2, StartCol: 0, EndCol: 1},
			{Text: "a1", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2},
			{Text: "a2", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2},
		},
	}
	lower := doctable.Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []doctable.Cell{
			{Text: "Wide", RowSpan: 1, ColSpan: 2, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 2},
		},
	}

	merged := appendTableRows(upper, lower)

	grid := merged.Grid()
	texts := make([]string, 0)
	for row := 0; row < merged.NumRows; row++ {
		for col := 0; col < merged.NumCols; col++ {
			if text := strings.TrimSpace(grid[row][col].Text); text != "" {
				texts = append(texts, text)
			}
		}
	}
	joined := strings.Join(texts, "|")
	require.Equal(t, 1, strings.Count(joined, "Tall"))
	require.Equal(t, 1, strings.Count(joined, "Wide"))
	require.Contains(t, joined, "a1")
	require.Contains(t, joined, "a2")
}
