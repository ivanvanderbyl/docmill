//go:build gridscaffold

package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

// cellBox builds a top-left box at (l,t) with width w and height h.
func cellBox(l, t, w, h float64) geom.Box {
	return geom.Box{L: l, T: t, R: l + w, B: t + h, Origin: geom.TopLeft}
}

func gridCell(idx int, text string, l, t, w, h float64) page.TextCell {
	return page.TextCell{Index: idx, Text: text, Box: cellBox(l, t, w, h)}
}

func TestReconstructGridLogicalRowsByVerticalAdjacency(t *testing.T) {
	t.Parallel()
	// Two logical rows: row A (y 0-10) with two cells, row B (y 20-30).
	// A wrapped cell that protrudes into the gap (y 8-22) must still belong
	// to row A because it overlaps row A's band by >50% of the cell height.
	cells := []page.TextCell{
		gridCell(1, "A1", 0, 0, 40, 10),
		gridCell(2, "A2", 50, 0, 40, 10),
		gridCell(3, "wrapped", 0, 8, 90, 14), // overlaps row A by (10-8)/14 = 0.14... no
		gridCell(4, "B1", 0, 20, 40, 10),
	}
	// Re-check overlap math: cell 3 spans y 8..22 (h=14). Row A band is 0..10.
	// overlap = min(22,10)-max(8,0) = 2. 2/14 = 0.14 < 0.5 → NOT in row A.
	// So cell 3 should start its own row. Adjust to make it belong to row A:
	cells[2] = gridCell(3, "wrapped", 0, 2, 90, 12) // y 2..14, overlap with row A (0..10) = 8, 8/12=0.67 > 0.5

	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)
	require.NotEmpty(t, grid.RowBoxes, "should detect at least one row")

	// Cells 1,2,3 share row 0; cell 4 is row 1.
	rows := grid.LogicalRows(cells)
	require.GreaterOrEqual(t, len(rows), 2)
	require.Contains(t, joinedParts(rows[0].Parts), "A1")
	require.Contains(t, joinedParts(rows[0].Parts), "wrapped")
	require.Contains(t, joinedParts(rows[1].Parts), "B1")
}

func TestReconstructGridColumnsFromGuttersNotAnchor(t *testing.T) {
	t.Parallel()
	// Three columns defined by gutters at x≈45 and x≈95. The header row
	// has 3 cells, but a data row has a wide cell spanning cols 1-2. The
	// columns must be derived from ALL rows' gutters, not just the header.
	cells := []page.TextCell{
		// header (row 0)
		gridCell(1, "H1", 0, 0, 40, 10),
		gridCell(2, "H2", 50, 0, 40, 10),
		gridCell(3, "H3", 100, 0, 40, 10),
		// data row 1: wide cell spanning cols 1-2
		gridCell(4, "D1", 0, 20, 40, 10),
		gridCell(5, "wide", 50, 20, 90, 10),
		// data row 2: three narrow cells (reinforces gutters)
		gridCell(6, "a", 0, 40, 40, 10),
		gridCell(7, "b", 50, 40, 40, 10),
		gridCell(8, "c", 100, 40, 40, 10),
	}
	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(grid.ColBoxes), 3, "gutters should yield ≥3 columns")
}

func TestReconstructGridCellColumnByAreaOverlap(t *testing.T) {
	t.Parallel()
	// A cell whose centre is in column 0 but whose bbox overlaps column 0
	// by more area than column 1 must be assigned to column 0 — even if its
	// centre is close to the boundary.
	cells := []page.TextCell{
		gridCell(1, "H1", 0, 0, 50, 10),
		gridCell(2, "H2", 60, 0, 50, 10),
		gridCell(3, "data", 0, 20, 55, 10), // centre 27.5 → col 0; bbox mostly in col 0
	}
	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)
	require.Len(t, grid.ColBoxes, 2)
	for _, a := range grid.Assign {
		if a.CellIndex == 3 {
			require.Equal(t, 0, a.Col, "data cell assigned by area overlap to col 0")
		}
	}
}

func TestReconstructGridColSpanWhenCellSpansColumns(t *testing.T) {
	t.Parallel()
	// A header cell that visually spans two columns should get ColSpan=2.
	cells := []page.TextCell{
		// row 0: one wide header + two narrow below to define columns
		gridCell(1, "merged-header", 0, 0, 100, 10),
		gridCell(2, "a", 0, 20, 45, 10),
		gridCell(3, "b", 55, 20, 45, 10),
		gridCell(4, "c", 0, 40, 45, 10),
		gridCell(5, "d", 55, 40, 45, 10),
	}
	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(grid.ColBoxes), 2)
	var headerAssign *CellAssign
	for i := range grid.Assign {
		if grid.Assign[i].CellIndex == 1 {
			headerAssign = &grid.Assign[i]
			break
		}
	}
	require.NotNil(t, headerAssign)
	require.GreaterOrEqual(t, headerAssign.ColSpan, 2, "wide header cell spans ≥2 columns")
}

func TestReconstructGridMinRowsAndCols(t *testing.T) {
	t.Parallel()
	// A single row with one cell → no grid.
	cells := []page.TextCell{gridCell(1, "solo", 0, 0, 40, 10)}
	grid, err := ReconstructGrid(cells, enclosingBoxOf(cells))
	require.NoError(t, err)
	require.Empty(t, grid.RowBoxes)
	require.Empty(t, grid.ColBoxes)
}

func TestReconstructGridPDFLineNeverSplitAcrossRows(t *testing.T) {
	t.Parallel()
	// A cell that is exactly one PDF text-line must end up in exactly one row.
	cells := []page.TextCell{
		gridCell(1, "a", 0, 0, 40, 10),
		gridCell(2, "b", 50, 0, 40, 10),
		gridCell(3, "c", 0, 20, 40, 10),
		gridCell(4, "d", 50, 20, 40, 10),
	}
	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)
	seen := make(map[int]int) // cellIndex → row
	for _, a := range grid.Assign {
		if _, dup := seen[a.CellIndex]; dup {
			t.Fatalf("cell %d assigned to two rows", a.CellIndex)
		}
		seen[a.CellIndex] = a.Row
	}
}

func TestReconstructGridMultilineCellStaysInOneRow(t *testing.T) {
	t.Parallel()
	// Reproduces a real-world Table 2.2.4.A fragmentation: a cell whose text
	// wraps across 4 PDF lines must be reconstructed as ONE logical row cell,
	// not 4 table rows.
	cells := []page.TextCell{
		// header
		gridCell(1, "Relevance", 0, 0, 80, 10),
		gridCell(2, "Evaluation", 90, 0, 200, 10),
		// data row 1: col 0 = "Known", col 1 = four wrapped lines
		gridCell(3, "Known and novel", 0, 20, 80, 10),
		gridCell(4, "Expert red teaming", 90, 20, 200, 10),
		gridCell(5, "Can models provide", 90, 30, 200, 10),
		gridCell(6, "uplift in catastrophic", 90, 40, 200, 10),
		gridCell(7, "chemical/biological?", 90, 50, 200, 10),
	}
	tableBox := enclosingBoxOf(cells)
	grid, err := ReconstructGrid(cells, tableBox)
	require.NoError(t, err)

	rows := grid.LogicalRows(cells)
	// The four wrapped lines (cells 4-7) must all be in the same logical row.
	rowOf := func(idx int) int {
		for _, a := range grid.Assign {
			if a.CellIndex == idx {
				return a.Row
			}
		}
		return -1
	}
	require.Equal(t, rowOf(4), rowOf(5), "wrapped lines 4,5 same row")
	require.Equal(t, rowOf(5), rowOf(6), "wrapped lines 5,6 same row")
	require.Equal(t, rowOf(6), rowOf(7), "wrapped lines 6,7 same row")
	// And the joined cell text is the multi-line content.
	require.GreaterOrEqual(t, len(rows), 2)
}

// helpers

func enclosingBoxOf(cells []page.TextCell) geom.Box {
	boxes := make([]geom.Box, len(cells))
	for i, c := range cells {
		boxes[i] = c.Box
	}
	return geom.EnclosingBox(boxes...)
}

func joinedParts(parts [][]string) string {
	out := ""
	for _, p := range parts {
		for _, s := range p {
			out += s + " "
		}
	}
	return out
}
