package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

// A display-equation block whose fragment gaps happen to align on one line is
// not a table: the following prose lines flow straight across the imaginary
// column boundaries. Geometry mirrors a real scientific-paper page (a display
// equation above body prose), with neutral text: a five-fragment anchor line,
// full-width prose runs crossing every gutter, and a second equation line whose
// fragments drift across the anchor's gaps.
func equationProseLineCells() []page.TextCell {
	return []page.TextCell{
		// Display-equation anchor line: five fragments with aligned gaps.
		textCell(1, "Na = Nb", 215, 633, 259, 643),
		textCell(2, "tc + Nd", 270, 633, 305, 643),
		textCell(3, "te +", 316, 633, 336, 643),
		textCell(4, "plus Nf", 337, 636, 372, 646),
		textCell(5, "tg :", 383, 633, 396, 643),
		// Body prose: single runs spanning the full region width.
		textCell(6, "long run of body prose spanning the whole region width", 92, 653, 519, 663),
		textCell(7, "Nh", 92, 664, 106, 674),
		textCell(8, "another long run of body prose spanning the whole width", 108, 668, 519, 678),
		textCell(9, "short lead fragment", 92, 676, 228, 686),
		// Second equation line: fragments drifting across the anchor's gaps.
		textCell(10, "Xa", 248, 699, 254, 709),
		textCell(11, "tb", 255, 696, 266, 706),
		textCell(12, "+ Xc", 268, 698, 283, 708),
		textCell(13, "td", 284, 696, 295, 706),
		textCell(14, "+", 297, 698, 305, 708),
		textCell(15, "plus Xe", 306, 702, 334, 712),
		textCell(16, "tf", 335, 695, 345, 705),
		textCell(17, "= 1", 348, 698, 363, 708),
	}
}

func equationProseTokenCells() []page.TextCell {
	return []page.TextCell{
		textCell(101, "Na", 215, 633, 236, 643),
		textCell(102, "Nb", 246, 633, 259, 643),
		textCell(103, "long", 92, 653, 118, 663),
		textCell(104, "run", 124, 653, 145, 663),
		textCell(105, "prose", 200, 653, 232, 663),
		textCell(106, "width", 480, 653, 519, 663),
	}
}

func TestDetectAnchoredTextTablesSuppressesEquationProseGrid(t *testing.T) {
	t.Parallel()

	result := table.DetectAnchoredTextTables(equationProseLineCells(), equationProseTokenCells(), table.DetectionOptions{})

	require.Empty(t, result.Tables)
	// Every line cell must be released back to the text flow.
	require.Len(t, result.TextCells, len(equationProseLineCells()))
}

// The same five-column, five-row shape with genuine per-column cells (no run
// crossing a column boundary) must still be detected: this is the negative case
// that keeps the gutter-persistence gate safe for real borderless tables.
func TestDetectAnchoredTextTablesKeepsGenuineWideTableWithPersistentGutters(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		// Header anchor line: five label cells (same x layout as the equation
		// anchor above).
		textCell(1, "Na = Nb", 215, 633, 259, 643),
		textCell(2, "tc + Nd", 270, 633, 305, 643),
		textCell(3, "te +", 316, 633, 336, 643),
		textCell(4, "plus Nf", 345, 636, 372, 646),
		textCell(5, "tg :", 383, 633, 396, 643),
		// Body rows: cells confined to their columns.
		textCell(6, "alpha", 92, 653, 150, 663),
		textCell(7, "one a", 270, 653, 300, 663),
		textCell(8, "two b", 316, 653, 336, 663),
		textCell(9, "buzz", 383, 653, 396, 663),
		textCell(10, "beta", 92, 668, 150, 678),
		textCell(11, "three c", 270, 668, 300, 678),
		textCell(12, "four d", 345, 668, 370, 678),
		textCell(13, "fizz", 383, 668, 396, 678),
		textCell(14, "gamma", 92, 683, 150, 693),
		textCell(15, "five e", 270, 683, 300, 693),
		textCell(16, "six f", 316, 683, 336, 693),
		textCell(17, "whirr", 383, 683, 396, 693),
	}
	tokenCells := []page.TextCell{
		textCell(101, "alpha", 92, 653, 150, 663),
		textCell(102, "beta", 92, 668, 150, 678),
		textCell(103, "gamma", 92, 683, 150, 693),
	}

	result := table.DetectAnchoredTextTables(lineCells, tokenCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, 5, result.Tables[0].Data.NumCols)
}

// raggedNumericTableCells is the reviewed false-positive case: a four-column
// numeric table with tight gutters and right-aligned numbers of mixed
// magnitude. Wide entries ("14231", "12345.67") reach far left of their
// column's median content extent, but each sits alone in its own column — the
// gutter persists on every line — so the gate must not fire.
func raggedNumericTableCells() []page.TextCell {
	return []page.TextCell{
		textCell(1, "Case", 40, 100, 68, 110),
		textCell(2, "N", 110, 100, 118, 110),
		textCell(3, "Cost", 158, 100, 186, 110),
		textCell(4, "Rate", 210, 100, 238, 110),
		textCell(5, "a", 40, 114, 48, 124),
		textCell(6, "7", 111, 114, 118, 124),
		textCell(7, "1.5", 164, 114, 186, 124),
		textCell(8, "0.9", 217, 114, 238, 124),
		textCell(9, "b", 40, 128, 48, 138),
		textCell(10, "14231", 78, 128, 118, 138),
		textCell(11, "12345.67", 128, 128, 186, 138),
		textCell(12, "9812.4", 196, 128, 238, 138),
		textCell(13, "c", 40, 142, 48, 152),
		textCell(14, "9", 111, 142, 118, 152),
		textCell(15, "2.5", 164, 142, 186, 152),
		textCell(16, "0.7", 217, 142, 238, 152),
		textCell(17, "d", 40, 156, 48, 166),
		textCell(18, "98341", 78, 156, 118, 166),
		textCell(19, "98765.43", 128, 156, 186, 166),
		textCell(20, "4471.9", 196, 156, 238, 166),
		textCell(21, "e", 40, 170, 48, 180),
		textCell(22, "3", 111, 170, 118, 180),
		textCell(23, "3.5", 164, 170, 186, 180),
		textCell(24, "0.5", 217, 170, 238, 180),
		textCell(25, "f", 40, 184, 48, 194),
		textCell(26, "45129", 78, 184, 118, 194),
		textCell(27, "45678.90", 128, 184, 186, 194),
		textCell(28, "7723.1", 196, 184, 238, 194),
	}
}

func TestDetectTextTablesKeepsRaggedRightAlignedNumericTable(t *testing.T) {
	t.Parallel()

	result := table.DetectTextTables(raggedNumericTableCells(), table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, 4, result.Tables[0].Data.NumCols)
}

func TestDetectAnchoredTextTablesKeepsRaggedRightAlignedNumericTable(t *testing.T) {
	t.Parallel()

	cells := raggedNumericTableCells()

	result := table.DetectAnchoredTextTables(cells, cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, 4, result.Tables[0].Data.NumCols)
}
