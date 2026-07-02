package render_test

import (
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/render"
	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestRenderTableUsesMarkdownRendererForDoclingGroundedHeaderOnlyTable(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 1,
		NumCols: 2,
		Cells: []table.Cell{
			{Text: "foo", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "bar", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Equal(t, "| foo | bar |\n| --- | --- |\n", got)
}

func TestRenderTableNormalisesCellTextLikeDocling(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []table.Cell{
			{Text: "A", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "B", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2},
			{Text: "x|y", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1},
			{Text: "line one\nline two", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Contains(t, got, "x&#124;y")
	require.Contains(t, got, "line one line two")
	require.NotContains(t, got, "x|y")
	require.False(t, strings.Contains(got, "line one\nline two"))
}

func TestRenderTableDropsZeroWidthFormatRunes(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 1,
		Cells: []table.Cell{
			{Text: "Header\u200b", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "\ufeffValue", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Contains(t, got, "Header")
	require.Contains(t, got, "Value")
	require.NotContains(t, got, "\u200b")
	require.NotContains(t, got, "\ufeff")
}

func TestRenderTableDoesNotRepairDecimalFragmentsByLiteralText(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 5,
		Cells: []table.Cell{
			{Text: "Evaluation", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "Vendor Gamma 4 6 .", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2},
			{Text: "Vendor Alpha Preview", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 2, EndCol: 3},
			{Text: "Threshold", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 3, EndCol: 4},
			{Text: "Rate", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 4, EndCol: 5},
			{Text: "Kernel task", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1},
			{Text: "252 42× .", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2},
			{Text: "0 604 .", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 2, EndCol: 3},
			{Text: "8 (0 20%) .", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 3, EndCol: 4},
			{Text: "97 . 64%", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 4, EndCol: 5},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Contains(t, got, "Vendor Gamma 4 6 .")
	require.Contains(t, got, "252 42× .")
	require.Contains(t, got, "0 604 .")
	require.Contains(t, got, "8 (0 20%) .")
	require.Contains(t, got, "97 . 64%")
	require.NotContains(t, got, "Vendor Gamma 4.6")
	require.NotContains(t, got, "252.42×")
	require.NotContains(t, got, "0.604")
	require.NotContains(t, got, "97.64%")
}

func TestRenderTableRightAlignsNumericDataColumns(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 2,
		Cells: []table.Cell{
			{Text: "Metric", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "Value", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2},
			{Text: "Rows", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1},
			{Text: "42", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 1, EndCol: 2},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Equal(t, "| Metric | Value |\n| ------ | ----: |\n| Rows   |    42 |\n", got)
}

func TestRenderTableReturnsEmptyForEmptyTable(t *testing.T) {
	t.Parallel()

	got, err := render.Table(table.Data{})

	require.NoError(t, err)
	require.Empty(t, got)
}
