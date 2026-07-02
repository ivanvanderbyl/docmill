package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestTableDataWithAssignedTextMatchesCellsByIntersectionOverTextCell(t *testing.T) {
	t.Parallel()

	data := table.FromRegions(
		box(0, 0, 100, 40),
		[]geom.Box{box(0, 0, 100, 20), box(0, 20, 100, 40)},
		[]geom.Box{box(0, 0, 50, 40), box(50, 0, 100, 40)},
		nil,
		table.RegionSemantics{ColumnHeaders: []geom.Box{box(0, 0, 100, 20)}},
	)
	textCells := []page.TextCell{
		{Index: 4, Text: "A.", Box: box(24, 24, 32, 34)},
		{Index: 1, Text: "Name", Box: box(4, 4, 35, 14)},
		{Index: 6, Text: "outside", Box: box(90, 30, 130, 40)},
		{Index: 5, Text: "42", Box: box(54, 24, 70, 34)},
		{Index: 3, Text: "Alice", Box: box(4, 24, 22, 34)},
		{Index: 2, Text: "Value", Box: box(54, 4, 82, 14)},
	}

	assigned := data.WithAssignedText(textCells, 0.5)
	grid := assigned.Grid()

	require.Equal(t, "Name", grid[0][0].Text)
	require.Equal(t, "Value", grid[0][1].Text)
	require.Equal(t, "Alice A.", grid[1][0].Text)
	require.Equal(t, "42", grid[1][1].Text)
	require.Empty(t, data.Grid()[0][0].Text)
}

func TestTableDataWithAssignedTextIgnoresCellsWithoutBoundingBoxes(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 1,
		Cells: []table.Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1},
		},
	}

	assigned := data.WithAssignedText([]page.TextCell{
		{Index: 1, Text: "Text", Box: box(0, 0, 10, 10)},
	}, 0.5)

	require.Empty(t, assigned.Grid()[0][0].Text)
}

func TestTableDataWithAssignedTextRecoversOrphanWordsUsingEstimatedGrid(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []table.Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1, Box: new(box(0, 0, 45, 18))},
			{StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, RowSpan: 1, ColSpan: 1, Box: new(box(45, 0, 100, 18))},
			{StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1, Box: new(box(0, 24, 45, 42))},
			{StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2, RowSpan: 1, ColSpan: 1, Box: new(box(55, 24, 100, 42))},
		},
	}
	textCells := []page.TextCell{
		{Index: 1, Text: "Name", Box: box(4, 4, 35, 14)},
		{Index: 2, Text: "Value", Box: box(60, 4, 92, 14)},
		{Index: 3, Text: "Alice", Box: box(4, 28, 35, 38)},
		{Index: 4, Text: "42", Box: box(48, 28, 54, 38)}, // Outside the raw row-1 col-1 bbox, inside the median-aligned column band.
	}

	assigned := data.WithAssignedText(textCells, 0.8)
	grid := assigned.Grid()

	require.Equal(t, "42", grid[1][1].Text)
}

func TestTableDataWithAssignedTextDoesNotRecoverWordsOutsideGrid(t *testing.T) {
	t.Parallel()

	data := table.FromRegions(
		box(0, 0, 100, 40),
		[]geom.Box{box(0, 0, 100, 20), box(0, 20, 100, 40)},
		[]geom.Box{box(0, 0, 50, 40), box(50, 0, 100, 40)},
		nil,
		table.RegionSemantics{},
	)
	assigned := data.WithAssignedText([]page.TextCell{
		{Index: 1, Text: "outside", Box: box(120, 4, 160, 14)},
	}, 0.5)

	for _, row := range assigned.Grid() {
		for _, cell := range row {
			require.Empty(t, cell.Text)
		}
	}
}

func TestTableDataWithAssignedTextDoesNotRecoverWordsInColumnGutter(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []table.Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1, Box: new(box(0, 0, 45, 20))},
			{StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, RowSpan: 1, ColSpan: 1, Box: new(box(55, 0, 100, 20))},
		},
	}
	// A word sitting in the inter-column gutter centres outside both column
	// bands, so it must stay unassigned rather than snapping to a neighbour.
	assigned := data.WithAssignedText([]page.TextCell{
		{Index: 1, Text: "gutter", Box: box(47, 4, 53, 14)},
	}, 0.5)

	for _, row := range assigned.Grid() {
		for _, cell := range row {
			require.Empty(t, cell.Text)
		}
	}
}

func TestTableDataWithAssignedTextAssignsBoundaryWordToBestCellOnly(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []table.Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1, Box: new(box(0, 0, 60, 20))},
			{StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, RowSpan: 1, ColSpan: 1, Box: new(box(60, 0, 120, 20))},
		},
	}

	assigned := data.WithAssignedText([]page.TextCell{
		{Index: 1, Text: "boundary", Box: box(45, 4, 70, 14)},
	}, 0.3)

	require.Equal(t, "boundary", assigned.Grid()[0][0].Text)
	require.Empty(t, assigned.Grid()[0][1].Text)
}

func TestTableDataWithAssignedTextDoesNotDuplicateDirectlyAssignedWords(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 1,
		Cells: []table.Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, RowSpan: 1, ColSpan: 1, Box: new(box(0, 0, 50, 20))},
		},
	}
	// "Solo" is matched directly and also sits inside the estimated grid band;
	// orphan recovery must skip already-assigned words instead of duplicating.
	assigned := data.WithAssignedText([]page.TextCell{
		{Index: 1, Text: "Solo", Box: box(5, 5, 45, 15)},
	}, 0.5)

	require.Equal(t, "Solo", assigned.Grid()[0][0].Text)
}

//go:fix inline
func boxPtrForTest(b geom.Box) *geom.Box {
	return new(b)
}
