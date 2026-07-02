package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func TestMedianFloat64MatchesNativeAOTBehaviour(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0.0, medianFloat64(nil))
	require.Equal(t, 4.0, medianFloat64([]float64{9, 1, 4}))
	require.Equal(t, 4.5, medianFloat64([]float64{8, 1, 5, 4}))
}

func TestFindAlignmentChoosesSmallestAnchorSpread(t *testing.T) {
	t.Parallel()

	cells := []Cell{
		{Box: boxPtr(geom.Box{L: 0, T: 0, R: 42, B: 10})},
		{Box: boxPtr(geom.Box{L: 8, T: 12, R: 42, B: 22})},
		{Box: boxPtr(geom.Box{L: 14, T: 24, R: 42, B: 34})},
	}

	require.Equal(t, alignRight, findAlignment(cells))
	require.Equal(t, alignLeft, findAlignment(nil))
}

func TestEstimateTableGridUsesMedianEdges(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 0, T: 0, R: 48, B: 20})},
			{StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, Box: boxPtr(geom.Box{L: 52, T: 2, R: 100, B: 22})},
			{StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 0, T: 24, R: 48, B: 44})},
			{StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2, Box: boxPtr(geom.Box{L: 52, T: 26, R: 100, B: 46})},
		},
	}

	grid := estimateTableGrid(data)

	require.Equal(t, []gridBand{{Index: 0, Start: 1, End: 21}, {Index: 1, Start: 25, End: 45}}, grid.Rows)
	require.Equal(t, []gridBand{{Index: 0, Start: 0, End: 48}, {Index: 1, Start: 52, End: 100}}, grid.Cols)
}

func TestAlignColumnsSnapsByMedianAnchorWithoutShrinkingOriginalBox(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 3,
		NumCols: 1,
		Cells: []Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 0, T: 0, R: 40, B: 10})},
			{StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 10, T: 12, R: 40, B: 22})},
			{StartRow: 2, EndRow: 3, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 20, T: 24, R: 40, B: 34})},
		},
	}

	aligned := alignColumns(data)

	require.Equal(t, 0.0, aligned.Cells[0].Box.L)
	require.Equal(t, 40.0, aligned.Cells[0].Box.R)
	require.Equal(t, 10.0, aligned.Cells[1].Box.L)
	require.Equal(t, 40.0, aligned.Cells[1].Box.R)
	require.Equal(t, 10.0, aligned.Cells[2].Box.L)
	require.Equal(t, 40.0, aligned.Cells[2].Box.R)
	require.Equal(t, 20.0, data.Cells[2].Box.L, "original data must remain immutable")
}

func TestRecoverOrphanWordsAssignsByContainingGridBand(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 0, T: 0, R: 50, B: 20})},
			{StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, Box: boxPtr(geom.Box{L: 50, T: 0, R: 100, B: 20})},
		},
	}
	grid := estimateTableGrid(data)
	words := []page.TextCell{
		{Index: 1, Text: "outside", Box: geom.Box{L: 120, T: 4, R: 150, B: 14}},
		{Index: 2, Text: "recovered", Box: geom.Box{L: 60, T: 4, R: 88, B: 14}},
	}

	assignments := recoverOrphanWords(data, grid, words, map[int]bool{})

	require.Len(t, assignments, 1)
	require.Equal(t, 1, assignments[0].CellIndex)
	require.Equal(t, "recovered", assignments[0].Word.Text)
}

func TestAlignCellsToWordsExpandsCellBoxToAssignedWords(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 1,
		NumCols: 1,
		Cells: []Cell{
			{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 10, T: 10, R: 40, B: 20})},
		},
	}
	assignments := []cellWordAssignment{
		{CellIndex: 0, Word: page.TextCell{Index: 1, Text: "wide", Box: geom.Box{L: 5, T: 8, R: 48, B: 22}}},
	}

	aligned := alignCellsToWords(data, assignments)

	require.Equal(t, geom.Box{L: 5, T: 8, R: 48, B: 22}, *aligned.Cells[0].Box)
	require.Equal(t, geom.Box{L: 10, T: 10, R: 40, B: 20}, *data.Cells[0].Box)
}

func TestMergeWordsIntoCellsSortsAndJoinsText(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 1,
		NumCols: 1,
		Cells:   []Cell{{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1}},
	}
	assignments := []cellWordAssignment{
		{CellIndex: 0, Word: page.TextCell{Index: 3, Text: "Beta", Box: geom.Box{L: 30, T: 0, R: 50, B: 10}}},
		{CellIndex: 0, Word: page.TextCell{Index: 1, Text: "Alpha", Box: geom.Box{L: 0, T: 0, R: 20, B: 10}}},
		{CellIndex: 0, Word: page.TextCell{Index: 2, Text: " ", Box: geom.Box{L: 22, T: 0, R: 24, B: 10}}},
	}

	merged := mergeWordsIntoCells(data, assignments)

	require.Equal(t, "Alpha Beta", merged.Cells[0].Text)
}

func TestMergeWordsIntoCellsOrdersSameLineWordsByHorizontalPositionDespiteTopJitter(t *testing.T) {
	t.Parallel()

	data := Data{
		NumRows: 1,
		NumCols: 1,
		Cells:   []Cell{{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1}},
	}
	assignments := []cellWordAssignment{
		{CellIndex: 0, Word: page.TextCell{Index: 2, Text: "(±", Box: geom.Box{L: 342.53, T: 243.27, R: 350.89, B: 254.60}}},
		{CellIndex: 0, Word: page.TextCell{Index: 3, Text: "0.10%)", Box: geom.Box{L: 354.71, T: 243.27, R: 387.98, B: 254.60}}},
		{CellIndex: 0, Word: page.TextCell{Index: 1, Text: "0.12%", Box: geom.Box{L: 310.22, T: 243.74, R: 338.94, B: 251.80}}},
	}

	merged := mergeWordsIntoCells(data, assignments)

	require.Equal(t, "0.12% (± 0.10%)", merged.Cells[0].Text)
}
