package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func gutterTestCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{Index: index, Text: text, Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}}
}

// gutterTestTable builds a DetectedTable over the given source cells with the
// given column bands, mirroring how the borderless builders construct their
// grids (FromRegions row x column intersections carry the band geometry the
// gate recovers).
func gutterTestTable(cells []page.TextCell, colBounds []float64, numRows int) DetectedTable {
	tableBox := enclosingTextCells(cells)
	rowHeight := tableBox.Height() / float64(numRows)
	rowBoxes := make([]geom.Box, 0, numRows)
	for i := range numRows {
		rowBoxes = append(rowBoxes, geom.Box{
			L: tableBox.L, R: tableBox.R,
			T: tableBox.T + float64(i)*rowHeight, B: tableBox.T + float64(i+1)*rowHeight,
			Origin: geom.TopLeft,
		})
	}
	colBoxes := make([]geom.Box, 0, len(colBounds)-1)
	for i := 0; i+1 < len(colBounds); i++ {
		colBoxes = append(colBoxes, geom.Box{
			L: colBounds[i], R: colBounds[i+1],
			T: tableBox.T, B: tableBox.B, Origin: geom.TopLeft,
		})
	}
	return DetectedTable{
		Data:      FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{}),
		Box:       tableBox,
		TextCells: cells,
	}
}

// A genuine table whose merged two-line group header spans all three columns
// must survive: up to two spanning lines are legitimate table furniture, not
// prose flowing across the grid.
func TestGutterPersistenceSparesMergedTwoLineHeaderTable(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		// Two-line merged group header spanning the full width.
		gutterTestCell(1, "Quarterly results by region", 40, 100, 300, 110),
		gutterTestCell(2, "(all values in thousands)", 40, 114, 290, 124),
		// Aligned data rows.
		gutterTestCell(3, "North", 40, 130, 80, 140),
		gutterTestCell(4, "120", 150, 130, 172, 140),
		gutterTestCell(5, "310", 250, 130, 272, 140),
		gutterTestCell(6, "South", 40, 144, 80, 154),
		gutterTestCell(7, "95", 157, 144, 172, 154),
		gutterTestCell(8, "205", 250, 144, 272, 154),
		gutterTestCell(9, "East", 40, 158, 74, 168),
		gutterTestCell(10, "141", 150, 158, 172, 168),
		gutterTestCell(11, "188", 250, 158, 272, 168),
		gutterTestCell(12, "West", 40, 172, 76, 182),
		gutterTestCell(13, "77", 157, 172, 172, 182),
		gutterTestCell(14, "164", 250, 172, 272, 182),
	}
	detected := gutterTestTable(cells, []float64{40, 120, 210, 300}, 6)

	require.False(t, tableViolatesGutterPersistence(detected, 4))
}

// A genuine table bracketed by a full-width title line and a full-width
// footnote line (two spanning lines) must survive the crossing floor.
func TestGutterPersistenceSparesFullWidthTitleAndFootnoteTable(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		gutterTestCell(1, "Observed frequencies across all trial sites and cohorts", 40, 100, 330, 110),
		gutterTestCell(2, "Site", 40, 116, 68, 126),
		gutterTestCell(3, "Count", 140, 116, 178, 126),
		gutterTestCell(4, "Share", 240, 116, 278, 126),
		gutterTestCell(5, "AAA", 40, 130, 66, 140),
		gutterTestCell(6, "412", 150, 130, 172, 140),
		gutterTestCell(7, "0.41", 248, 130, 276, 140),
		gutterTestCell(8, "BBB", 40, 144, 66, 154),
		gutterTestCell(9, "388", 150, 144, 172, 154),
		gutterTestCell(10, "0.39", 248, 144, 276, 154),
		gutterTestCell(11, "CCC", 40, 158, 66, 168),
		gutterTestCell(12, "201", 150, 158, 172, 168),
		gutterTestCell(13, "0.20", 248, 158, 276, 168),
		gutterTestCell(14, "Counts pooled over both observation windows per protocol", 40, 174, 336, 184),
	}
	detected := gutterTestTable(cells, []float64{40, 120, 210, 330}, 6)

	require.False(t, tableViolatesGutterPersistence(detected, 4))
}

// dropGutterCrossingTables must release a suppressed table's cell claims so
// the cells flow back to the paragraph path, and must leave kept tables'
// claims untouched. The fake here is prose gridded into three columns: every
// body line is a full-width run covering both gutters.
func TestDropGutterCrossingTablesReleasesClaims(t *testing.T) {
	t.Parallel()

	fakeCells := []page.TextCell{
		gutterTestCell(1, "lead fragment", 40, 100, 120, 110),
		gutterTestCell(2, "middle fragment", 150, 100, 230, 110),
		gutterTestCell(3, "tail fragment", 260, 100, 330, 110),
		gutterTestCell(4, "a full width prose run covering both claimed gutters", 40, 114, 330, 124),
		gutterTestCell(5, "another full width prose run covering both gutters", 40, 128, 330, 138),
		gutterTestCell(6, "a third full width prose run covering both gutters", 40, 142, 330, 152),
	}
	fake := gutterTestTable(fakeCells, []float64{40, 135, 245, 330}, 4)
	claimed := map[int]bool{}
	for _, cell := range fakeCells {
		claimed[cell.Index] = true
	}

	kept := dropGutterCrossingTables([]DetectedTable{fake}, claimed, 4)

	require.Empty(t, kept)
	require.Empty(t, claimed)
}

// tableViolatesGutterPersistenceWithTokens re-judges a table whose lines are
// merged runs (no line-level column evidence) on the word tokens inside its
// box: prose tokens interpenetrate the claimed columns (degenerate corridors),
// while a genuine word grid's tokens keep the corridors open.
func TestGutterPersistenceWithTokensJudgesMergedRunTables(t *testing.T) {
	t.Parallel()

	// Both variants: four merged-run lines over a claimed 3-column grid.
	lineCells := []page.TextCell{
		gutterTestCell(1, "alpha beta gamma", 40, 100, 330, 110),
		gutterTestCell(2, "delta epsilon zeta", 40, 114, 330, 124),
		gutterTestCell(3, "eta theta iota", 40, 128, 330, 138),
		gutterTestCell(4, "kappa lambda mu", 40, 142, 330, 152),
	}
	detected := gutterTestTable(lineCells, []float64{40, 135, 245, 330}, 4)

	// Prose tokens: on three of the four lines a long word runs across a
	// claimed gutter into the neighbouring column's content.
	proseTokens := []page.TextCell{
		gutterTestCell(101, "alpha", 40, 100, 110, 110),
		gutterTestCell(102, "beta", 150, 100, 220, 110),
		gutterTestCell(103, "gamma", 260, 100, 330, 110),
		gutterTestCell(104, "de", 40, 114, 88, 124),
		gutterTestCell(105, "longcrossing", 95, 114, 220, 124),
		gutterTestCell(106, "zeta", 260, 114, 330, 124),
		gutterTestCell(107, "anotherlongone", 40, 128, 180, 138),
		gutterTestCell(108, "th", 188, 128, 220, 138),
		gutterTestCell(109, "iota", 260, 128, 330, 138),
		gutterTestCell(110, "kappa", 40, 142, 112, 152),
		gutterTestCell(111, "la", 150, 142, 212, 152),
		gutterTestCell(112, "wideacrossgap", 216, 142, 330, 152),
	}
	require.True(t, tableViolatesGutterPersistenceWithTokens(detected, proseTokens, normaliseDetectionOptions(DetectionOptions{})))

	// Grid tokens: words sit inside their columns, corridors stay open.
	gridTokens := []page.TextCell{
		gutterTestCell(101, "alpha", 40, 100, 100, 110),
		gutterTestCell(102, "beta", 150, 100, 200, 110),
		gutterTestCell(103, "gamma", 260, 100, 320, 110),
		gutterTestCell(104, "delta", 40, 114, 100, 124),
		gutterTestCell(105, "epsilon", 150, 114, 200, 124),
		gutterTestCell(106, "zeta", 260, 114, 320, 124),
		gutterTestCell(107, "eta", 40, 128, 100, 138),
		gutterTestCell(108, "theta", 150, 128, 200, 138),
		gutterTestCell(109, "iota", 260, 128, 320, 138),
		gutterTestCell(110, "kappa", 40, 142, 100, 152),
		gutterTestCell(111, "lambda", 150, 142, 200, 152),
		gutterTestCell(112, "mu", 260, 142, 320, 152),
	}
	require.False(t, tableViolatesGutterPersistenceWithTokens(detected, gridTokens, normaliseDetectionOptions(DetectionOptions{})))
}
