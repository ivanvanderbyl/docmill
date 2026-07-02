package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestTableDataGridFillsCoveredSpanCoordinates(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 3,
		Cells: []table.Cell{
			{
				Text:     "Merged",
				RowSpan:  2,
				ColSpan:  2,
				StartRow: 0,
				EndRow:   2,
				StartCol: 0,
				EndCol:   2,
			},
			{
				Text:     "Right",
				RowSpan:  1,
				ColSpan:  1,
				StartRow: 0,
				EndRow:   1,
				StartCol: 2,
				EndCol:   3,
			},
		},
	}

	grid := data.Grid()

	require.Equal(t, "Merged", grid[0][0].Text)
	require.Equal(t, "Merged", grid[0][1].Text)
	require.Equal(t, "Merged", grid[1][0].Text)
	require.Equal(t, "Merged", grid[1][1].Text)
	require.Equal(t, "Right", grid[0][2].Text)
}

func TestTableDataGridCreatesEmptyCellsForMissingCoordinates(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []table.Cell{
			{Text: "Only", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
		},
	}

	grid := data.Grid()

	require.Equal(t, "Only", grid[0][0].Text)
	require.Empty(t, grid[0][1].Text)
	require.Equal(t, 0, grid[0][1].StartRow)
	require.Equal(t, 1, grid[0][1].StartCol)
	require.Empty(t, grid[1][0].Text)
	require.Empty(t, grid[1][1].Text)
}

func TestTableDataGridPreservesHeaderFlags(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []table.Cell{
			{Text: "Name", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, ColumnHeader: true},
			{Text: "Section", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, RowSection: true},
		},
	}

	grid := data.Grid()

	require.True(t, grid[0][0].ColumnHeader)
	require.True(t, grid[0][1].RowSection)
}

func TestOTSLResultBuildsTableData(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("<ched>Name</ched><ched>Value</ched><nl><fcel>Foo</fcel><fcel>42</fcel><nl>")
	data := result.Data()

	require.Equal(t, 2, data.NumRows)
	require.Equal(t, 2, data.NumCols)
	require.Equal(t, "Name", data.Grid()[0][0].Text)
	require.Equal(t, "42", data.Grid()[1][1].Text)
}
