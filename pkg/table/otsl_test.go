package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestParseOTSLExtractsSimpleTableWithHeaders(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("<ched>Name</ched><ched>Value</ched><nl><fcel>Foo</fcel><fcel>42</fcel><nl>")

	require.Equal(t, []string{"ched", "ched", "nl", "fcel", "fcel", "nl"}, result.Sequence)
	require.Equal(t, 2, result.NumRows)
	require.Equal(t, 2, result.NumCols)
	require.Len(t, result.Cells, 4)
	require.True(t, result.Cells[0].ColumnHeader)
	require.True(t, result.Cells[1].ColumnHeader)
	require.Equal(t, "Foo", result.Cells[2].Text)
	require.Equal(t, 1, result.Cells[2].StartRow)
	require.Equal(t, 0, result.Cells[2].StartCol)
}

func TestParseOTSLPreservesEmptyCells(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("<ched>A</ched><ched>B</ched><nl><fcel>x</fcel><ecel></ecel><nl>")

	require.Equal(t, 2, result.NumRows)
	require.Equal(t, 2, result.NumCols)
	require.Len(t, result.Cells, 4)
	require.Equal(t, "", result.Cells[3].Text)
	require.Equal(t, 1, result.Cells[3].StartRow)
	require.Equal(t, 1, result.Cells[3].StartCol)
}

func TestParseOTSLHandlesColumnAndRowSpans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		text    string
		rowSpan int
		colSpan int
		endRow  int
		endCol  int
	}{
		{
			name:    "lcel colspan",
			input:   "<fcel>Merged</fcel><lcel><nl><fcel>A</fcel><fcel>B</fcel><nl>",
			text:    "Merged",
			rowSpan: 1,
			colSpan: 2,
			endRow:  1,
			endCol:  2,
		},
		{
			name:    "ucel rowspan",
			input:   "<fcel>Tall</fcel><fcel>A</fcel><nl><ucel><fcel>B</fcel><nl>",
			text:    "Tall",
			rowSpan: 2,
			colSpan: 1,
			endRow:  2,
			endCol:  1,
		},
		{
			name:    "xcel two dimensional merge",
			input:   "<fcel>Big</fcel><lcel><nl><ucel><xcel><nl>",
			text:    "Big",
			rowSpan: 2,
			colSpan: 2,
			endRow:  2,
			endCol:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := table.ParseOTSL(tt.input)
			cell := findCellByText(t, result.Cells, tt.text)

			require.Equal(t, tt.rowSpan, cell.RowSpan)
			require.Equal(t, tt.colSpan, cell.ColSpan)
			require.Equal(t, tt.endRow, cell.EndRow)
			require.Equal(t, tt.endCol, cell.EndCol)
		})
	}
}

func TestParseOTSLHandlesSemanticCellTagsAndSelfClosingCells(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("<rhed>Section</rhed><srow>Category</srow><ecel/><nl>")

	require.Len(t, result.Cells, 3)
	require.True(t, result.Cells[0].RowHeader)
	require.Equal(t, "Section", result.Cells[0].Text)
	require.True(t, result.Cells[1].RowSection)
	require.Equal(t, "Category", result.Cells[1].Text)
	require.Equal(t, "", result.Cells[2].Text)
}

func TestParseOTSLHandlesContainerAndOpenTagForm(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("<otsl><ched>Name<ched>Value<nl><fcel>Foo<fcel>42<nl></otsl>")

	require.Equal(t, []string{"ched", "ched", "nl", "fcel", "fcel", "nl"}, result.Sequence)
	require.Equal(t, "Name", result.Cells[0].Text)
	require.Equal(t, "Value", result.Cells[1].Text)
	require.Equal(t, "42", result.Cells[3].Text)
}

func TestParseOTSLEmptyInputReturnsEmptyResult(t *testing.T) {
	t.Parallel()

	result := table.ParseOTSL("  ")

	require.Empty(t, result.Sequence)
	require.Empty(t, result.Cells)
	require.Zero(t, result.NumRows)
	require.Zero(t, result.NumCols)
}

func TestParseOTSLDoesNotPanicOnMalformedInput(t *testing.T) {
	t.Parallel()

	inputs := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"empty container", "<otsl></otsl>"},
		{"unterminated tag", "<fcel"},
		{"unterminated tag with trailing text", "<fcel>x"},
		{"stray close tags", "</fcel><nl>"},
		{"ragged rows", "<fcel>a<fcel>b<nl><fcel>c<nl>"},
		{"self closing only", "<ecel/><nl>"},
		{"nested garbage", "<<>><fcel>>a</fcel>"},
		{"only newlines", "<nl><nl><nl>"},
		{"unbalanced container", "<otsl><fcel>a"},
		{"lone angle brackets", "<<<>>>"},
	}

	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// A panic here fails the subtest naturally.
			result := table.ParseOTSL(tc.in)

			require.GreaterOrEqual(t, result.NumRows, 0)
			require.GreaterOrEqual(t, result.NumCols, 0)
			require.Len(t, result.Data().Cells, len(result.Cells))
		})
	}
}

func FuzzParseOTSL(f *testing.F) {
	seeds := []string{
		"",
		"<fcel>a</fcel><nl>",
		"<otsl><ched>Name<ched>Value<nl><fcel>Foo<fcel>42<nl></otsl>",
		"<fcel",
		"<<>><fcel>>a</fcel>",
		"<ecel/><nl>",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		result := table.ParseOTSL(in)

		if result.NumRows < 0 || result.NumCols < 0 {
			t.Fatalf("negative dimensions: rows=%d cols=%d", result.NumRows, result.NumCols)
		}
		if got, want := len(result.Data().Cells), len(result.Cells); got != want {
			t.Fatalf("Data cell count %d != result cell count %d", got, want)
		}
	})
}

func findCellByText(t *testing.T, cells []table.Cell, text string) table.Cell {
	t.Helper()

	for _, cell := range cells {
		if cell.Text == text {
			return cell
		}
	}
	require.Failf(t, "cell not found", "text %q not found in %#v", text, cells)
	return table.Cell{}
}
