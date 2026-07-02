package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/render"
	"github.com/ivanvanderbyl/docmill/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestDetectTextTablesFindsAlignedRowsAndReturnsRemainingText(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Intro paragraph", 0, 0, 90, 10),
		textCell(2, "Name", 0, 40, 35, 50),
		textCell(3, "Year", 100, 40, 132, 50),
		textCell(4, "Score", 200, 40, 242, 50),
		textCell(5, "Ada", 0, 62, 28, 72),
		textCell(6, "2024", 100, 62, 135, 72),
		textCell(7, "98", 200, 62, 218, 72),
		textCell(8, "Grace", 0, 84, 44, 94),
		textCell(9, "2025", 100, 84, 135, 94),
		textCell(10, "99", 200, 84, 218, 94),
		textCell(11, "Outro paragraph", 0, 130, 92, 140),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:              3,
		MinCols:              3,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)
	require.Len(t, result.TextCells, 2)
	require.Equal(t, "Intro paragraph", result.TextCells[0].Text)
	require.Equal(t, "Outro paragraph", result.TextCells[1].Text)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 3, data.NumCols)
	require.True(t, data.Grid()[0][0].ColumnHeader)
	require.Equal(t, "Name", data.Grid()[0][0].Text)
	require.Equal(t, "2025", data.Grid()[2][1].Text)

	markdown, err := render.Table(data)
	require.NoError(t, err)
	require.Equal(t, "| Name  | Year | Score |\n| ----- | ---: | ----: |\n| Ada   | 2024 |    98 |\n| Grace | 2025 |    99 |\n", markdown)
}

func TestDetectTextTablesKeepsStrayProseOutOfRecoveredCells(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Intro paragraph", 0, 0, 90, 10),
		textCell(2, "Name", 0, 40, 35, 50),
		textCell(3, "Year", 100, 40, 132, 50),
		textCell(4, "Score", 200, 40, 242, 50),
		textCell(5, "Ada", 0, 62, 28, 72),
		textCell(6, "2024", 100, 62, 135, 72),
		textCell(7, "98", 200, 62, 218, 72),
		textCell(8, "Grace", 0, 84, 44, 94),
		textCell(9, "2025", 100, 84, 135, 94),
		textCell(10, "99", 200, 84, 218, 94),
		textCell(11, "Outro paragraph", 0, 130, 92, 140),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:              3,
		MinCols:              3,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)

	remaining := make([]string, 0, len(result.TextCells))
	for _, cell := range result.TextCells {
		remaining = append(remaining, cell.Text)
	}
	require.ElementsMatch(t, []string{"Intro paragraph", "Outro paragraph"}, remaining)

	for _, row := range result.Tables[0].Data.Grid() {
		for _, cell := range row {
			require.NotContains(t, cell.Text, "paragraph")
		}
	}
}

func TestDetectTextTablesIgnoresShortAlignedFragments(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Label", 0, 0, 40, 10),
		textCell(2, "Value", 100, 0, 140, 10),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:         3,
		MinCols:         2,
		RowTolerance:    6,
		ColumnTolerance: 12,
	})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}

func TestDetectTextTablesIgnoresDenseMultiColumnProse(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "This is a long left-column prose line", 0, 0, 145, 10),
		textCell(2, "This is a long right-column prose line", 170, 0, 315, 10),
		textCell(3, "Another left-column sentence continues", 0, 16, 145, 26),
		textCell(4, "Another right-column sentence continues", 170, 16, 315, 26),
		textCell(5, "The paragraph keeps flowing on this side", 0, 32, 145, 42),
		textCell(6, "The paragraph keeps flowing on that side", 170, 32, 315, 42),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:         3,
		MinCols:         2,
		RowTolerance:    6,
		ColumnTolerance: 12,
	})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}

func TestDetectTextTablesFindsContinuationRowsWithBlankLeftCells(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Intro paragraph", 0, 0, 100, 10),
		textCell(2, "Area", 0, 40, 28, 50),
		textCell(3, "Competence", 180, 40, 250, 50),
		textCell(4, "1. Embodying sustainability values", 0, 78, 160, 88),
		textCell(5, "1.1 Valuing sustainability", 180, 78, 330, 88),
		textCell(6, "1.2 Supporting fairness", 180, 102, 315, 112),
		textCell(7, "1.3 Promoting nature", 180, 126, 310, 136),
		textCell(8, "2. Embracing complexity in", 0, 150, 160, 160),
		textCell(9, "sustainability", 0, 164, 80, 174),
		textCell(10, "2.1 Systems thinking", 180, 150, 305, 160),
		textCell(11, "2.2 Critical thinking", 180, 174, 305, 184),
		textCell(12, "2.3 Problem framing", 180, 198, 305, 208),
		textCell(13, "3. Envisioning sustainable futures", 0, 226, 170, 236),
		textCell(14, "3.1 Futures literacy", 180, 226, 305, 236),
		textCell(15, "3.2 Adaptability", 180, 250, 285, 260),
		textCell(16, "Outro paragraph", 0, 302, 100, 312),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:              4,
		MinCols:              2,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{cells[0], cells[15]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 9, data.NumRows)
	require.Equal(t, 2, data.NumCols)
	require.Equal(t, "Area", data.Grid()[0][0].Text)
	require.Equal(t, "Competence", data.Grid()[0][1].Text)
	require.Equal(t, "1.2 Supporting fairness", data.Grid()[2][1].Text)
	require.Equal(t, "2. Embracing complexity in sustainability", data.Grid()[4][0].Text)
	require.Equal(t, "", data.Grid()[5][0].Text)
	require.Equal(t, "2.2 Critical thinking", data.Grid()[5][1].Text)

	markdown, err := render.Table(data)
	require.NoError(t, err)
	require.Contains(t, markdown, "| Area")
	require.Regexp(t, `(?m)^\|\s*\|\s*1\.2 Supporting fairness\s*\|$`, markdown)
	require.Contains(t, markdown, "2. Embracing complexity in sustainability")
}

func TestDetectTextTablesIgnoresShortTwoColumnFragmentsWithContinuations(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Topic", 0, 0, 32, 10),
		textCell(2, "Notes", 180, 0, 216, 10),
		textCell(3, "A", 0, 22, 8, 32),
		textCell(4, "one continuation", 180, 22, 280, 32),
		textCell(5, "another continuation", 180, 46, 306, 56),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows: 4,
		MinCols: 2,
	})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}

func TestDetectTextTablesIgnoresTwoColumnProseWithContinuationShape(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Probability, Combinatorics and Control", 0, 0, 180, 10),
		textCell(2, "Combinatorial Cosmology", 240, 0, 380, 10),
		textCell(3, "With this setup and the random dynamics", 0, 28, 210, 38),
		textCell(4, "As for the normal phase the choice is simple", 240, 28, 470, 38),
		textCell(5, "contains all the information about edges", 0, 52, 210, 62),
		textCell(6, "one path is either possible or not", 240, 52, 450, 62),
		textCell(7, "time to the states at the next one", 0, 76, 190, 86),
		textCell(8, "and zero during the extreme phase", 240, 76, 430, 86),
		textCell(9, "the model remains simplified", 240, 100, 410, 110),
		textCell(10, "but it is based on physical intuition", 240, 124, 440, 134),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:              4,
		MinCols:              2,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}

func TestDetectTextTablesIgnoresBulletMarkerListsWithWrappedItems(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "•", 0, 0, 8, 10),
		textCell(2, "Liquid biomass: palm oil", 32, 0, 190, 10),
		textCell(3, "•", 0, 24, 8, 34),
		textCell(4, "Unutilised wood: domestic thinned wood", 32, 24, 260, 34),
		textCell(5, "•", 0, 48, 8, 58),
		textCell(6, "Construction wood waste: salvaged wood", 32, 48, 260, 58),
		textCell(7, "materials", 32, 72, 92, 82),
		textCell(8, "•", 0, 96, 8, 106),
		textCell(9, "Waste materials and other biomass", 32, 96, 250, 106),
		textCell(10, "cooking oil and black liquor", 32, 120, 220, 130),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{
		MinRows:              4,
		MinCols:              2,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}

func TestDetectTextTablesTrimsSparseLeadingProseFromDenseGrid(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "intro words", 108.57, 155.73, 155.38, 166.83),
		textCell(2, ".", 156.54, 162.79, 157.83, 164.20),
		textCell(3, "Group", 77.02, 187.92, 108.78, 196.39),
		textCell(4, "Metric", 186.47, 187.92, 218.19, 199.19),
		textCell(5, "-", 219.04, 192.17, 223.09, 193.36),
		textCell(6, "one values", 223.85, 189.18, 295.13, 199.02),
		textCell(7, "category detail", 186.26, 204.10, 294.76, 215.38),
		textCell(8, "(first rate)", 186.19, 219.50, 242.95, 227.74),
		textCell(9, "Metric", 310.22, 187.92, 341.94, 199.19),
		textCell(10, "-", 342.79, 192.17, 346.84, 193.36),
		textCell(11, "two values", 347.60, 187.92, 409.65, 199.19),
		textCell(12, "category", 310.12, 205.37, 354.71, 215.20),
		textCell(13, "(second rate)", 309.94, 219.50, 358.05, 227.74),
		textCell(14, "Metric", 430.27, 187.92, 458.23, 196.39),
		textCell(15, "-", 458.96, 192.17, 463.00, 193.36),
		textCell(16, "three", 463.76, 189.18, 486.97, 196.39),
		textCell(17, "results", 430.09, 204.10, 489.58, 212.57),
		textCell(18, "(third rate)", 429.94, 219.50, 533.24, 227.74),
		textCell(19, "Row Alpha", 76.89, 243.32, 154.34, 254.60),
		textCell(20, "continued", 77.05, 257.42, 118.94, 265.87),
		textCell(21, "99", 186.47, 243.75, 198.15, 251.79),
		textCell(22, ".", 198.97, 250.09, 200.57, 251.79),
		textCell(23, "58% (± 0", 201.53, 243.21, 244.58, 254.60),
		textCell(24, ".", 245.36, 250.09, 246.95, 251.79),
		textCell(25, "15%)", 247.50, 243.21, 270.76, 254.60),
		textCell(26, "0", 310.22, 243.75, 316.26, 251.79),
		textCell(27, ".", 317.04, 250.09, 318.63, 251.79),
		textCell(28, "12% (± 0", 319.18, 243.27, 360.75, 254.60),
		textCell(29, ".", 361.52, 250.09, 363.12, 251.79),
		textCell(30, "10%)", 363.67, 243.27, 387.98, 254.60),
		textCell(31, "94% (± 7%)", 430.22, 243.27, 484.98, 254.60),
		textCell(32, "Row Beta", 76.89, 282.73, 161.22, 291.20),
		textCell(33, "98%", 186.53, 283.16, 210.00, 291.20),
		textCell(34, "19%", 310.22, 283.16, 334.00, 291.20),
		textCell(35, "76%", 429.99, 282.67, 450.36, 294.00),
		textCell(36, "Row Gamma", 76.89, 308.06, 151.13, 319.16),
		textCell(37, "97%", 186.53, 308.49, 210.00, 316.53),
		textCell(38, "27%", 310.22, 308.49, 334.00, 316.53),
		textCell(39, "64%", 430.37, 308.00, 451.25, 319.33),
		textCell(40, "Note", 72.56, 327.63, 105.87, 336.71),
		textCell(41, ".", 106.64, 333.17, 107.95, 334.56),
		textCell(42, "A", 108.40, 327.98, 111.65, 334.43),
		textCell(43, ".", 112.39, 333.17, 113.70, 334.56),
		textCell(44, "B", 114.20, 327.98, 118.72, 334.43),
		textCell(45, ".", 119.41, 333.17, 120.72, 334.56),
		textCell(46, "C", 121.46, 327.98, 125.48, 334.43),
		textCell(47, ".", 126.31, 333.17, 127.61, 334.56),
		textCell(48, "summary", 128.09, 327.63, 165.78, 336.86),
		textCell(49, "-", 166.48, 331.12, 169.79, 332.09),
		textCell(50, "continues here", 170.42, 327.63, 233.17, 334.56),
		textCell(51, "-", 233.77, 331.12, 237.08, 332.09),
		textCell(52, "with more explanation", 237.70, 327.63, 425.27, 334.56),
		textCell(53, "and", 428.95, 327.63, 451.52, 334.56),
		textCell(54, ".", 452.09, 333.17, 453.40, 334.56),
		textCell(55, "another", 456.68, 327.63, 481.37, 336.86),
		textCell(56, "-", 482.32, 331.23, 485.52, 331.86),
		textCell(57, "fragment", 486.31, 327.59, 539.24, 334.56),
		textCell(58, "second line of the note crosses the page", 72.31, 340.88, 345.93, 350.11),
		textCell(59, ".", 346.88, 346.66, 347.93, 347.81),
		textCell(60, "right side continuation", 351.38, 340.84, 474.38, 350.11),
		textCell(61, "-", 475.34, 344.48, 478.53, 345.11),
		textCell(62, "tail", 479.32, 340.88, 536.99, 347.81),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Contains(t, result.TextCells, cells[0])
	require.Contains(t, result.TextCells, cells[1])

	grid := result.Tables[0].Data.Grid()
	require.Equal(t, 4, result.Tables[0].Data.NumCols)
	require.NotContains(t, grid[0][0].Text, "intro")
	require.Contains(t, grid[0][0].Text, "Group")
	require.Equal(t, 6, result.Tables[0].Data.NumRows)
	require.Equal(t, "Row Alpha continued", grid[3][0].Text)
	require.Contains(t, grid[3][1].Text, "58%")
	require.Contains(t, grid[3][2].Text, "12%")
	require.Contains(t, grid[3][3].Text, "94%")
	require.Equal(t, "Row Beta", grid[4][0].Text)
	require.Equal(t, "Row Gamma", grid[5][0].Text)
	require.NotContains(t, result.TextCells, cells[19])
	require.NotContains(t, result.TextCells, cells[31])
	require.NotContains(t, result.TextCells, cells[35])
	require.Contains(t, result.TextCells, cells[39])
	require.Contains(t, result.TextCells, cells[57])
	require.Contains(t, result.TextCells, cells[61])
}

func TestDetectAnchoredTextTablesKeepsPrecedingProseOutOfCaptionedWordGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "intro words.", 108.57, 155.73, 157.83, 166.83),
		textCell(2, "Group Metric one values Metric two values Metric three", 77.02, 187.92, 533.24, 199.19),
		textCell(3, "(first rate) (second rate) (third rate)", 186.19, 219.50, 533.24, 227.74),
		textCell(4, "Row Alpha continued 99.58% 0.12% 94%", 76.89, 243.32, 484.98, 254.60),
		textCell(5, "Row Beta 99.48% 0.19% 76%", 76.89, 282.73, 488.13, 294.00),
		textCell(6, "Row Gamma 99.41% 0.27% 64%", 76.89, 308.06, 489.08, 319.33),
		textCell(7, "[Table 1. A] Example evaluation results", 72.26, 327.63, 539.24, 336.86),
	}
	wordCells := []page.TextCell{
		textCell(1, "intro", 108.57, 155.73, 130.78, 164.20),
		textCell(2, "words.", 134.03, 155.73, 157.83, 166.83),
		textCell(3, "Group", 77.02, 187.92, 108.78, 196.39),
		textCell(4, "Metric-one", 186.47, 187.92, 247.06, 199.19),
		textCell(5, "values", 253.10, 189.18, 295.13, 199.02),
		textCell(6, "Metric-two", 310.22, 187.92, 370.81, 199.19),
		textCell(7, "values", 376.47, 187.92, 409.65, 199.19),
		textCell(8, "Metric-three", 430.27, 187.92, 486.97, 196.39),
		textCell(9, "(first", 186.19, 219.50, 223.10, 227.74),
		textCell(10, "rate)", 225.80, 219.50, 242.95, 227.74),
		textCell(11, "(second", 309.94, 219.50, 338.25, 227.74),
		textCell(12, "rate)", 340.95, 219.50, 358.05, 227.74),
		textCell(13, "(third", 429.94, 219.50, 476.78, 227.74),
		textCell(14, "rate)", 479.60, 219.50, 514.70, 227.74),
		textCell(15, "Row", 76.89, 243.32, 112.22, 251.79),
		textCell(16, "Alpha", 115.87, 243.32, 154.34, 251.79),
		textCell(17, "continued", 77.05, 257.42, 118.94, 265.87),
		textCell(18, "99.58%", 186.47, 243.21, 222.78, 251.80),
		textCell(19, "0.12%", 310.22, 243.21, 342.90, 251.80),
		textCell(20, "94%", 430.22, 243.74, 451.60, 251.80),
		textCell(21, "Row", 76.89, 282.73, 112.22, 291.20),
		textCell(22, "Beta", 115.87, 283.16, 152.81, 291.20),
		textCell(23, "99.48%", 186.53, 283.14, 222.56, 291.21),
		textCell(24, "0.19%", 310.22, 283.14, 342.90, 291.21),
		textCell(25, "76%", 429.99, 283.14, 450.36, 291.21),
		textCell(26, "Row", 76.89, 308.06, 112.22, 316.53),
		textCell(27, "Gamma", 115.79, 308.49, 142.24, 319.16),
		textCell(28, "99.41%", 186.53, 308.46, 220.38, 316.54),
		textCell(29, "0.27%", 310.22, 308.46, 342.90, 316.54),
		textCell(30, "64%", 430.37, 308.46, 451.25, 316.54),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Contains(t, result.TextCells, lineCells[0])
	require.Contains(t, result.TextCells, lineCells[6])

	for _, cell := range result.Tables[0].TextCells {
		require.NotEqual(t, lineCells[0].Index, cell.Index)
	}
	grid := result.Tables[0].Data.Grid()
	require.NotContains(t, grid[0][0].Text, "intro")
}

func TestDetectTablesUsesRulingGrid(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Intro paragraph", 0, 0, 90, 10),
		textCell(2, "Name", 8, 24, 35, 34),
		textCell(3, "Score", 58, 24, 92, 34),
		textCell(4, "Ada", 8, 44, 28, 54),
		textCell(5, "98", 58, 44, 72, 54),
		textCell(6, "Grace", 8, 64, 44, 74),
		textCell(7, "99", 58, 64, 72, 74),
		textCell(8, "Outro paragraph", 0, 96, 92, 106),
	}
	rulings := []page.RulingSegment{
		ruling(0, 20, 100, 20),
		ruling(0, 40, 100, 40),
		ruling(0, 60, 100, 60),
		ruling(0, 80, 100, 80),
		ruling(0, 20, 0, 80),
		ruling(50, 20, 50, 80),
		ruling(100, 20, 100, 80),
	}

	result := table.DetectTables(cells, rulings, table.DetectionOptions{
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{cells[0], cells[7]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 2, data.NumCols)
	require.True(t, data.Grid()[0][0].ColumnHeader)
	require.Equal(t, "Name", data.Grid()[0][0].Text)
	require.Equal(t, "Score", data.Grid()[0][1].Text)
	require.Equal(t, "Grace", data.Grid()[2][0].Text)
	require.Equal(t, "99", data.Grid()[2][1].Text)

	markdown, err := render.Table(data)
	require.NoError(t, err)
	require.Equal(t, "| Name  | Score |\n| ----- | ----: |\n| Ada   |    98 |\n| Grace |    99 |\n", markdown)
}

func TestDetectAnchoredTextTablesUsesLineAnchorsAndWordTokens(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Model Size Score Rank", 0, 0, 260, 10),
		textCell(2, "Solar", 0, 22, 35, 32),
		textCell(3, "10B", 70, 22, 96, 32),
		textCell(4, "98", 140, 22, 156, 32),
		textCell(5, "1", 210, 22, 218, 32),
		textCell(6, "Qwen", 0, 44, 35, 54),
		textCell(7, "72B 95 2", 70, 44, 240, 54),
		textCell(8, "Mistral", 0, 66, 45, 76),
		textCell(9, "7B", 70, 66, 88, 76),
		textCell(10, "91", 140, 66, 156, 76),
		textCell(11, "3", 210, 66, 218, 76),
		textCell(12, "Table 1: Caption after table", 0, 118, 220, 128),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 0, 0, 35, 10),
		textCell(2, "Size", 70, 0, 100, 10),
		textCell(3, "Score", 140, 0, 178, 10),
		textCell(4, "Rank", 210, 0, 240, 10),
		textCell(5, "Solar", 0, 22, 35, 32),
		textCell(6, "10B", 70, 22, 96, 32),
		textCell(7, "98", 140, 22, 156, 32),
		textCell(8, "1", 210, 22, 218, 32),
		textCell(9, "Qwen", 0, 44, 35, 54),
		textCell(10, "72B", 70, 44, 96, 54),
		textCell(11, "95", 140, 44, 156, 54),
		textCell(12, "2", 210, 44, 218, 54),
		textCell(13, "Mistral", 0, 66, 45, 76),
		textCell(14, "7B", 70, 66, 88, 76),
		textCell(15, "91", 140, 66, 156, 76),
		textCell(16, "3", 210, 66, 218, 76),
		textCell(17, "Caption", 0, 118, 45, 128),
		textCell(18, "after", 70, 118, 105, 128),
		textCell(19, "table", 140, 118, 178, 128),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{
		MinRows:              4,
		MinCols:              4,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)
	data := result.Tables[0].Data
	require.Equal(t, 4, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	require.Equal(t, "Model", data.Grid()[0][0].Text)
	require.Equal(t, "Rank", data.Grid()[0][3].Text)
	require.Equal(t, "Qwen", data.Grid()[2][0].Text)
	require.Equal(t, "95", data.Grid()[2][2].Text)

	markdown, err := render.Table(data)
	require.NoError(t, err)
	require.Equal(t, "| Model   | Size | Score | Rank |\n| ------- | ---- | ----: | ---: |\n| Solar   | 10B  |    98 |    1 |\n| Qwen    | 72B  |    95 |    2 |\n| Mistral | 7B   |    91 |    3 |\n", markdown)
}

func TestDetectAnchoredTextTablesSplitsCompactCaptionedTableFromWordGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Model H6 (Avg.) ARC HellaSwag MMLU TruthfulQA Winogrande GSM8K", 156.32, 75.06, 439.05, 81.82),
		textCell(2, "Cand. 1", 156.44, 88.32, 179.25, 93.55),
		textCell(3, "73.73", 195.12, 88.28, 211.65, 93.55),
		textCell(4, "70.48 87.47 65.73 70.62 81.53", 226.98, 88.28, 394.88, 93.55),
		textCell(5, "66.57", 418.08, 88.28, 434.59, 93.54),
		textCell(6, "Cand. 2 73.28", 156.44, 97.65, 211.48, 102.88),
		textCell(7, "71.59 88.39 66.14 72.50 81.99", 226.96, 97.61, 395.19, 102.88),
		textCell(8, "59.14", 418.11, 97.61, 434.56, 102.94),
		textCell(9, "Table 6: H6 benchmark summary", 70.73, 119.05, 320, 128.02),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 156.32, 75.06, 175.68, 80.26),
		textCell(2, "H6", 188.42, 75.06, 197.22, 80.29),
		textCell(3, "(Avg.)", 199.70, 75.12, 218.24, 81.82),
		textCell(4, "ARC", 227.67, 75.12, 242.75, 80.29),
		textCell(5, "HellaSwag", 252.11, 75.06, 284.62, 81.82),
		textCell(6, "MMLU", 293.17, 75.22, 316.32, 80.29),
		textCell(7, "TruthfulQA", 324.80, 75.06, 360.17, 81.52),
		textCell(8, "Winogrande", 368.56, 75.06, 405.21, 81.82),
		textCell(9, "GSM8K", 413.83, 75.12, 439.05, 80.29),
		textCell(10, "Cand.", 156.44, 88.32, 173.45, 93.55),
		textCell(11, "1", 177.13, 88.37, 179.25, 93.45),
		textCell(12, "73.73", 195.12, 88.28, 211.65, 93.55),
		textCell(13, "70.48", 226.98, 88.37, 243.32, 93.55),
		textCell(14, "87.47", 260.37, 88.37, 276.47, 93.55),
		textCell(15, "65.73", 296.56, 88.28, 312.70, 93.55),
		textCell(16, "70.62", 334.19, 88.31, 350.75, 93.55),
		textCell(17, "81.53", 378.91, 88.28, 394.88, 93.55),
		textCell(18, "66.57", 418.08, 88.28, 434.59, 93.54),
		textCell(19, "Cand.", 156.44, 97.65, 173.45, 102.88),
		textCell(20, "2", 176.52, 97.70, 179.86, 102.78),
		textCell(21, "73.28", 195.14, 97.70, 211.48, 102.88),
		textCell(22, "71.59", 226.96, 97.61, 243.53, 102.87),
		textCell(23, "88.39", 260.16, 97.61, 276.65, 102.88),
		textCell(24, "66.14", 296.52, 97.61, 313.02, 102.87),
		textCell(25, "72.50", 334.17, 97.61, 350.76, 102.87),
		textCell(26, "81.99", 378.70, 97.61, 395.19, 102.87),
		textCell(27, "59.14", 418.11, 97.61, 434.56, 102.94),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[8]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 8, data.NumCols)
	require.Equal(t, "Model", data.Grid()[0][0].Text)
	require.Equal(t, "H6 (Avg.)", data.Grid()[0][1].Text)
	require.Equal(t, "GSM8K", data.Grid()[0][7].Text)
	require.Equal(t, "Cand. 1", data.Grid()[1][0].Text)
	require.Equal(t, "73.73", data.Grid()[1][1].Text)
	require.Equal(t, "81.99", data.Grid()[2][6].Text)
	require.Equal(t, "59.14", data.Grid()[2][7].Text)
}

func TestDetectAnchoredTextTablesBuildsCaptionBeforeMultilineRows(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "election integrity paragraph", 54, 58, 375, 67),
		textCell(2, "Table: The number of accredited observers as of 28 April", 54.23, 116.36, 353.53, 126.52),
		textCell(3, "202215", 54.28, 129.29, 85.41, 137.68),
		textCell(4, "No.", 62.44, 159.67, 74.98, 166.22),
		textCell(5, "Name of organization", 88.20, 159.56, 172.43, 168.00),
		textCell(6, "Number of accredited", 280.76, 159.56, 365.46, 166.22),
		textCell(7, "observers", 303.64, 170.47, 342.50, 177.02),
		textCell(8, "1", 67.24, 186.00, 69.61, 192.47),
		textCell(9, "Union of Youth Federations of Cambodia", 88.23, 185.92, 249.24, 192.57),
		textCell(10, "17,266", 310.32, 186.00, 336.44, 193.74),
		textCell(11, "(UYFC)", 88.07, 196.72, 117.69, 205.15),
		textCell(12, "2", 66.52, 212.35, 70.78, 218.82),
		textCell(13, "Cambodian Women for Peace and", 87.97, 212.27, 224.78, 218.92),
		textCell(14, "9,835", 312.22, 212.35, 334.00, 220.09),
		textCell(15, "Development", 88.21, 223.18, 140.48, 231.40),
		textCell(16, "3", 66.64, 238.70, 70.86, 245.28),
		textCell(17, "Association of Democratic Students of", 87.52, 238.62, 239.90, 245.27),
		textCell(18, "711", 316.35, 238.70, 328.63, 245.17),
		textCell(19, "Cambodia", 87.97, 249.42, 128.15, 256.07),
		textCell(20, "4", 66.37, 265.08, 70.83, 271.53),
		textCell(21, "Association of Intellectual and Youth", 87.52, 264.97, 231.01, 271.62),
		textCell(22, "46", 318.21, 265.05, 327.69, 271.62),
		textCell(23, "Volunteer", 87.56, 275.88, 125.67, 282.42),
		textCell(24, "5", 66.64, 291.52, 70.90, 297.98),
		textCell(25, "Our Friends Association", 87.95, 291.32, 182.43, 297.98),
		textCell(26, "27", 318.36, 291.41, 327.70, 297.88),
		textCell(27, "6", 66.60, 307.96, 70.85, 314.53),
		textCell(28, "COMFREL", 87.97, 307.87, 131.21, 314.53),
		textCell(29, "26", 318.36, 307.96, 327.69, 314.53),
		textCell(30, "7", 66.68, 323.62, 70.86, 329.98),
		textCell(31, "Traditional and Modern Mental Health", 87.73, 323.54, 237.13, 330.08),
		textCell(32, "15", 319.08, 323.51, 327.74, 330.08),
		textCell(33, "Organization", 87.95, 334.22, 137.92, 342.66),
		textCell(34, "Total", 87.71, 349.89, 107.73, 356.43),
		textCell(35, "27,926", 309.56, 349.86, 336.54, 357.77),
		textCell(36, "15 https://www.nec.gov.kh/khmer/content/5524", 54.65, 536.91, 185.00, 542.53),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[1], lineCells[2], lineCells[35]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 9, data.NumRows)
	require.Equal(t, 3, data.NumCols)
	require.Equal(t, "No.", data.Grid()[0][0].Text)
	require.Equal(t, "Name of organization", data.Grid()[0][1].Text)
	require.Equal(t, "Number of accredited observers", data.Grid()[0][2].Text)
	require.Equal(t, "1", data.Grid()[1][0].Text)
	require.Equal(t, "Union of Youth Federations of Cambodia (UYFC)", data.Grid()[1][1].Text)
	require.Equal(t, "17,266", data.Grid()[1][2].Text)
	require.Equal(t, "Cambodian Women for Peace and Development", data.Grid()[2][1].Text)
	require.Equal(t, "Traditional and Modern Mental Health Organization", data.Grid()[7][1].Text)
	require.Equal(t, "Total", data.Grid()[8][1].Text)
	require.Equal(t, "27,926", data.Grid()[8][2].Text)
}

func TestDetectAnchoredTextTablesBuildsCaptionBeforeAlignedNumericRows(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "TABLE 23: IMPRISONMENT CLAUSES IN ENVIRONMENT,", 99.43, 331.04, 484.25, 342.46),
		textCell(2, "HEALTH AND SAFETY LAWS", 99.68, 347.04, 289.11, 356.48),
		textCell(3, "Imprisonment term", 128.15, 383.39, 232.90, 392.94),
		textCell(4, "Number of clauses", 270.52, 383.19, 368.65, 391.17),
		textCell(5, "Number of laws", 394.71, 383.19, 478.18, 391.17),
		textCell(6, "Less than 3 months", 103.83, 403.84, 196.96, 411.65),
		textCell(7, "150", 312.13, 404.26, 331.20, 411.65),
		textCell(8, "35", 431.40, 404.26, 445.19, 411.65),
		textCell(9, "3 months to less than 1 year", 104.20, 424.39, 237.79, 434.09),
		textCell(10, "199", 312.13, 424.81, 331.20, 432.19),
		textCell(11, "14", 431.85, 424.94, 445.19, 432.05),
		textCell(12, "1 year to less than 3 years", 104.65, 444.93, 227.53, 454.64),
		textCell(13, "326", 311.67, 445.35, 331.20, 452.73),
		textCell(14, "16", 431.85, 445.35, 445.19, 452.73),
		textCell(15, "3 years to less than 5 years", 104.20, 465.48, 232.28, 475.18),
		textCell(16, "357", 311.67, 465.90, 331.20, 473.28),
		textCell(17, "22", 431.19, 465.90, 445.19, 473.14),
		textCell(18, "5 years to less than 10 years", 104.24, 486.02, 238.02, 495.72),
		textCell(19, "147", 312.13, 486.45, 331.20, 493.82),
		textCell(20, "27", 431.19, 486.44, 445.19, 493.82),
		textCell(21, "More than 10 years", 103.68, 506.57, 195.78, 516.27),
		textCell(22, "0", 317.22, 506.99, 325.46, 514.37),
		textCell(23, "0", 434.08, 506.99, 442.32, 514.37),
		textCell(24, "Source: TeamLease Regtech", 99.48, 526.48, 210.73, 534.86),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[1], lineCells[23]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 7, data.NumRows)
	require.Equal(t, 3, data.NumCols)
	require.Equal(t, "Imprisonment term", data.Grid()[0][0].Text)
	require.Equal(t, "Number of clauses", data.Grid()[0][1].Text)
	require.Equal(t, "Number of laws", data.Grid()[0][2].Text)
	require.Equal(t, "Less than 3 months", data.Grid()[1][0].Text)
	require.Equal(t, "150", data.Grid()[1][1].Text)
	require.Equal(t, "35", data.Grid()[1][2].Text)
	require.Equal(t, "More than 10 years", data.Grid()[6][0].Text)
	require.Equal(t, "0", data.Grid()[6][1].Text)
	require.Equal(t, "0", data.Grid()[6][2].Text)
}

func TestDetectAnchoredTextTablesBuildsLabelValueRowsWithContinuations(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(0, "This project has been funded with the support of the European Commission. This publication reflects the views only of the author", 91.04, 762.48, 511.56, 769.40),
		textCell(1, "and the Commission cannot be held responsible for any use which may be made of the information contained therein.", 109.46, 772.92, 492.76, 779.84),
		textCell(2, "Project No: : 2021-2-FR02-KA220-YOU-000048126", 219.60, 791.71, 383.34, 798.46),
		textCell(3, "6.", 86.15, 132.40, 97.18, 144.03),
		textCell(4, "ECO CIRCLE COMPETENCE FRAMEWORK", 122.76, 132.38, 410.85, 144.03),
		textCell(5, "Competence Area", 76.65, 194.53, 183.17, 206.01),
		textCell(6, "#1 THE 3 RS: RECYCLE", 213.41, 195.46, 344.48, 206.59),
		textCell(7, "-", 345.56, 201.39, 349.79, 203.10),
		textCell(8, "REUSE", 351.53, 195.65, 387.68, 206.56),
		textCell(9, "-", 388.64, 201.39, 392.87, 203.10),
		textCell(10, "REDUCE", 394.61, 195.65, 443.64, 206.56),
		textCell(11, "Competence Statement", 76.57, 236.86, 193.51, 246.69),
		textCell(12, "To know the basics of the 3 Rs and their importance and", 210.12, 236.04, 452.14, 245.13),
		textCell(13, "implementation into daily life in relation to green entrepreneurship", 210.74, 249.96, 500.23, 259.05),
		textCell(14, "and circular economy.", 210.53, 263.83, 303.82, 272.88),
		textCell(15, "Learning Outcomes", 76.93, 325.69, 172.06, 335.77),
		textCell(16, "Knowledge", 76.93, 358.37, 131.18, 368.65),
		textCell(17, "●", 229.09, 360.69, 234.25, 365.85),
		textCell(18, "To understand the meaning of reducing, reusing and recycling", 246.12, 359.43, 512.26, 368.52),
		textCell(19, "and how they connect", 246.53, 373.27, 340.68, 382.32),
		textCell(20, "●", 229.09, 389.73, 234.25, 394.89),
		textCell(21, "To understand the importance of the 3 Rs as waste", 246.12, 388.47, 465.80, 397.56),
		textCell(22, "management", 246.84, 403.32, 302.52, 411.36),
		textCell(23, "●", 228.97, 418.16, 233.51, 422.70),
		textCell(24, "To be familiar with the expansion of the 3 Rs - the 7 Rs", 246.12, 416.19, 477.89, 425.28),
		textCell(25, "Skills", 76.48, 450.43, 100.90, 458.71),
		textCell(26, "●", 229.09, 452.75, 234.25, 457.91),
		textCell(27, "To implement different ways of waste management into daily", 246.12, 451.49, 511.54, 460.58),
		textCell(28, "life", 246.84, 465.29, 258.83, 472.59),
		textCell(29, "●", 228.97, 481.06, 233.51, 485.60),
		textCell(30, "To properly implement recycling in day-to-day activities", 246.12, 479.13, 483.17, 488.18),
		textCell(31, "●", 228.97, 494.86, 233.51, 499.40),
		textCell(32, "To promote reducing and reusing before recycling", 246.12, 492.89, 459.06, 501.98),
		textCell(33, "Attitudes and Values", 76.24, 527.23, 179.53, 535.51),
		textCell(34, "●", 229.09, 529.55, 234.25, 534.71),
		textCell(35, "To acquire a proactive approach to implementing the 3 Rs into", 246.12, 528.33, 514.84, 537.38),
		textCell(36, "daily personal life", 246.55, 542.21, 320.76, 551.30),
		textCell(37, "●", 228.97, 557.98, 233.51, 562.52),
		textCell(38, "To educate others on the importance of sustainable waste", 246.12, 556.01, 497.17, 565.10),
		textCell(39, "management", 246.84, 570.86, 302.52, 578.90),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[1], lineCells[2], lineCells[3], lineCells[4]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 6, data.NumRows)
	require.Equal(t, 2, data.NumCols)
	require.Equal(t, "Competence Area", data.Grid()[0][0].Text)
	require.Equal(t, "#1 THE 3 RS: RECYCLE - REUSE - REDUCE", data.Grid()[0][1].Text)
	require.Equal(t, "Competence Statement", data.Grid()[1][0].Text)
	require.Contains(t, data.Grid()[1][1].Text, "green entrepreneurship and circular economy.")
	require.Equal(t, "Learning Outcomes", data.Grid()[2][0].Text)
	require.Equal(t, "", data.Grid()[2][1].Text)
	require.Equal(t, "Knowledge", data.Grid()[3][0].Text)
	require.Contains(t, data.Grid()[3][1].Text, "● To understand the importance of the 3 Rs as waste management")
	require.Equal(t, "Skills", data.Grid()[4][0].Text)
	require.Contains(t, data.Grid()[4][1].Text, "● To promote reducing and reusing before recycling")
	require.Equal(t, "Attitudes and Values", data.Grid()[5][0].Text)
	require.Contains(t, data.Grid()[5][1].Text, "● To educate others on the importance of sustainable waste management")
}

func TestDetectAnchoredTextTablesBuildsMultilineNumericContinuationRows(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "IX - Zamboanga", 49.92, 76.82, 106.87, 84.85),
		textCell(2, "Peninsula", 49.92, 87.62, 84.08, 93.98),
		textCell(3, "4", 164.56, 77.24, 168.84, 83.09),
		textCell(4, "2", 245.84, 77.14, 249.50, 83.09),
		textCell(5, "4", 326.56, 77.24, 330.84, 83.09),
		textCell(6, "X - Northern", 49.50, 102.46, 95.09, 108.82),
		textCell(7, "Mindanao", 49.73, 113.26, 85.70, 119.62),
		textCell(8, "2", 164.84, 102.78, 168.50, 108.73),
		textCell(9, "2", 245.84, 102.78, 249.50, 108.73),
		textCell(10, "2", 326.84, 102.78, 330.50, 108.73),
		textCell(11, "XI - Davao Region", 49.50, 128.29, 114.16, 136.13),
		textCell(12, "1", 165.02, 128.52, 168.57, 134.37),
		textCell(13, "3", 245.77, 128.42, 249.46, 134.46),
		textCell(14, "5", 326.72, 128.52, 330.60, 134.46),
		textCell(15, "Source: HOR 2022.", 45.40, 273.09, 120.29, 282.02),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[14]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	require.Equal(t, "IX - Zamboanga Peninsula", data.Grid()[0][0].Text)
	require.Equal(t, "4", data.Grid()[0][1].Text)
	require.Equal(t, "X - Northern Mindanao", data.Grid()[1][0].Text)
	require.Equal(t, "XI - Davao Region", data.Grid()[2][0].Text)
	require.Equal(t, "5", data.Grid()[2][3].Text)
}

func TestDetectTextTablesBuildsMultilineNumericContinuationRowsBeforePartialBand(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "IX - Zamboanga", 49.92, 76.82, 106.87, 84.85),
		textCell(2, "Peninsula", 49.92, 87.62, 84.08, 93.98),
		textCell(3, "4", 164.56, 77.24, 168.84, 83.09),
		textCell(4, "2", 245.84, 77.14, 249.50, 83.09),
		textCell(5, "4", 326.56, 77.24, 330.84, 83.09),
		textCell(6, "X - Northern", 49.50, 102.46, 95.09, 108.82),
		textCell(7, "Mindanao", 49.73, 113.26, 85.70, 119.62),
		textCell(8, "2", 164.84, 102.78, 168.50, 108.73),
		textCell(9, "2", 245.84, 102.78, 249.50, 108.73),
		textCell(10, "2", 326.84, 102.78, 330.50, 108.73),
		textCell(11, "XI - Davao Region", 49.50, 128.29, 114.16, 136.13),
		textCell(12, "1", 165.02, 128.52, 168.57, 134.37),
		textCell(13, "3", 245.77, 128.42, 249.46, 134.46),
		textCell(14, "5", 326.72, 128.52, 330.60, 134.46),
		textCell(15, "XII -", 49.51, 153.99, 73.95, 161.83),
		textCell(16, "SOCCSKSARGEN", 49.73, 164.80, 116.64, 171.15),
		textCell(17, "2", 164.84, 154.22, 168.50, 160.17),
		textCell(18, "2", 245.84, 154.22, 249.50, 160.17),
		textCell(19, "1", 326.84, 154.22, 330.50, 160.17),
		textCell(20, "XIII - Caraga", 49.50, 179.88, 105.07, 187.72),
		textCell(21, "1", 165.02, 180.10, 168.57, 185.96),
		textCell(22, "3", 245.77, 180.01, 249.46, 186.05),
		textCell(23, "3", 326.72, 180.10, 330.60, 186.05),
		textCell(24, "Source: HOR 2022.", 45.40, 273.09, 120.29, 282.02),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{cells[23]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 5, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	require.Equal(t, "IX - Zamboanga Peninsula", data.Grid()[0][0].Text)
	require.Equal(t, "X - Northern Mindanao", data.Grid()[1][0].Text)
	require.Equal(t, "XII - SOCCSKSARGEN", data.Grid()[3][0].Text)
	require.Equal(t, "3", data.Grid()[4][3].Text)
}

func TestDetectTextTablesBuildsCaptionlessMultilineTextGrid(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Version History", 70.87, 337.33, 186.82, 352.65),
		textCell(2, "Version", 76.17, 391.49, 109.95, 398.59),
		textCell(3, "Date", 135.76, 391.49, 156.72, 398.59),
		textCell(4, "Change", 197.37, 391.40, 231.46, 400.15),
		textCell(5, "Affected Sections", 374.96, 391.40, 457.01, 398.59),
		textCell(6, "1.0", 76.25, 418.03, 89.33, 425.23),
		textCell(7, "April 30,", 135.51, 412.85, 171.84, 421.50),
		textCell(8, "2022", 135.57, 423.24, 158.05, 430.42),
		textCell(9, "Original", 197.56, 418.04, 230.22, 426.79),
		textCell(10, "1.0", 76.25, 450.68, 89.33, 457.87),
		textCell(11, "June 3,", 135.47, 445.49, 165.13, 453.94),
		textCell(12, "2022", 135.57, 455.89, 158.05, 463.07),
		textCell(13, "Small edits for clarity on Creative", 197.40, 445.49, 341.02, 454.15),
		textCell(14, "Commons licensing and attribution.", 197.56, 455.89, 351.90, 464.64),
		textCell(15, "1. Introduction to Open Educational", 375.12, 445.49, 527.81, 454.15),
		textCell(16, "Resources", 375.57, 455.98, 421.20, 463.08),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{cells[0]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	require.Equal(t, "Version", data.Grid()[0][0].Text)
	require.Equal(t, "Date", data.Grid()[0][1].Text)
	require.Equal(t, "Change", data.Grid()[0][2].Text)
	require.Equal(t, "Affected Sections", data.Grid()[0][3].Text)
	require.Equal(t, "1.0", data.Grid()[1][0].Text)
	require.Equal(t, "April 30, 2022", data.Grid()[1][1].Text)
	require.Equal(t, "Original", data.Grid()[1][2].Text)
	require.Equal(t, "", data.Grid()[1][3].Text)
	require.Equal(t, "June 3, 2022", data.Grid()[2][1].Text)
	require.Equal(t, "Small edits for clarity on Creative Commons licensing and attribution.", data.Grid()[2][2].Text)
	require.Equal(t, "1. Introduction to Open Educational Resources", data.Grid()[2][3].Text)
}

func TestDetectTextTablesBuildsCaptionlessServiceFlowGrid(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Service Stage", 41.90, 97.64, 88.96, 104.53),
		textCell(2, "Function Name", 145.57, 97.64, 196.15, 103.09),
		textCell(3, "Explanation", 249.07, 97.64, 288.57, 104.45),
		textCell(4, "Expected Benefit", 476.32, 97.64, 534.25, 104.45),
		textCell(5, "1. Project creation", 41.93, 117.55, 93.50, 123.39),
		textCell(6, "Project creation and", 145.50, 116.58, 203.70, 122.41),
		textCell(7, "Select document type to automatically run project creation, Pipeline configuration with", 248.88, 117.63, 454.66, 122.51),
		textCell(8, "The intuitive UI environment allows the person in charge to quickly proceed with", 476.10, 117.65, 675.82, 122.51),
		textCell(9, "management", 145.49, 128.83, 183.59, 134.18),
		textCell(10, "recommended Modelset and Endpoint deployment", 248.98, 126.65, 369.10, 131.45),
		textCell(11, "the entire process from project creation to deployment, improving work efficiency", 476.02, 126.63, 670.43, 131.51),
		textCell(12, "2. Data labeling and", 41.94, 142.08, 98.72, 147.98),
		textCell(13, "Data storage management", 145.50, 143.30, 222.57, 148.95),
		textCell(14, "Provides convenient functions for uploading raw data, viewer, and data management", 249.02, 144.10, 450.24, 148.98),
		textCell(15, "Conveniently manage raw data to be used for OCR Pack and actual date from live", 476.15, 139.75, 669.29, 144.63),
		textCell(16, "fine-tuning", 41.73, 153.78, 74.26, 159.68),
		textCell(17, "(search using image metadata, sorting, filtering, hashtags settings on image data)", 249.13, 151.00, 443.26, 155.88),
		textCell(18, "service", 476.11, 148.77, 493.10, 152.60),
		textCell(19, "Image data bookmark for Qualitative Evaluation", 249.02, 157.90, 361.07, 162.78),
		textCell(20, "Create and manage Labeling", 145.43, 170.95, 228.29, 176.86),
		textCell(21, "Creating a Labeling Space to manage raw data annotation, managing labeling resources", 248.90, 172.03, 457.75, 176.88),
		textCell(22, "Labeling work can be outsourced within the pack. Labeled data is continuously", 476.27, 170.90, 663.64, 175.76),
		textCell(23, "Space", 145.37, 182.84, 163.20, 188.49),
		textCell(24, "(Ontology, Characters to be Recognized), data set dump, data set version management", 249.13, 180.73, 456.59, 185.88),
		textCell(25, "supplied from which data sets can be created with ease. The Auto Labeling function", 476.11, 179.88, 675.67, 184.76),
		textCell(26, "3", 347.63, 189.04, 350.45, 193.43),
		textCell(27, "increases both efficiency and convenience.", 476.17, 188.88, 579.01, 193.70),
		textCell(28, "Model training", 145.50, 200.80, 187.02, 206.70),
		textCell(29, "Various basic models for each selected document, information comparison between", 248.75, 198.62, 448.53, 203.45),
		textCell(30, "Providing a foundation for customers to implement, manage, and upgrade their own", 476.27, 200.88, 674.78, 205.76),
		textCell(31, "5", 347.63, 196.32, 350.48, 200.62),
		textCell(32, "models, basic model training, training pause function, re-training, cancel function, and", 248.98, 207.62, 453.24, 212.50),
		textCell(33, "OCR model specialized to the customers' needs", 476.15, 209.90, 589.47, 214.70),
		textCell(34, "configuration support for Characters to be Recognized and Ontology that is frequently", 248.86, 216.62, 455.80, 221.50),
		textCell(35, "modified while developing specialized models", 248.98, 225.62, 357.69, 230.50),
		textCell(36, "3. Pipeline configuration and", 41.94, 235.08, 124.89, 240.98),
		textCell(37, "Pipeline, Endpoint", 145.50, 237.03, 197.77, 242.86),
		textCell(38, "Choose Detector, Recognizer, or Parser to create a Pipeline or an Endpoint", 248.90, 235.03, 425.33, 239.88),
		textCell(39, "Providing a foundation for customers to implement, manage, and upgrade their own", 476.27, 236.13, 674.78, 241.01),
		textCell(40, "deployment", 41.87, 247.03, 76.46, 252.61),
		textCell(41, "Creation and management", 145.43, 244.83, 222.62, 250.73),
		textCell(42, "Connect Pipelines to Endpoints, perform tasks such as deployment controllers,", 248.90, 244.00, 436.37, 248.83),
		textCell(43, "OCR model specialized to the customers' needs", 476.15, 245.15, 589.47, 249.95),
		textCell(44, "deployment recovery, and more", 248.87, 253.17, 324.04, 257.83),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Empty(t, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 6, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	grid := data.Grid()
	require.Equal(t, "Service Stage", grid[0][0].Text)
	require.Equal(t, "Function Name", grid[0][1].Text)
	require.Equal(t, "1. Project creation", grid[1][0].Text)
	require.Equal(t, "Project creation and management", grid[1][1].Text)
	require.Equal(t, "Create and manage Labeling Space", grid[3][1].Text)
	require.Contains(t, grid[3][2].Text, "version management 3")
	require.Equal(t, "Model training", grid[4][1].Text)
	require.Contains(t, grid[4][2].Text, "information comparison between 5 models")
	require.Equal(t, "3. Pipeline configuration and deployment", grid[5][0].Text)
}

func TestDetectAnchoredTextTablesBuildsWideMultilineTextGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Restrictions on Land Ownership by Foreigners in Selected Jurisdictions", 141.02, 38.44, 470.98, 49.03),
		textCell(2, "Jurisdiction", 77.61, 74.82, 136.62, 85.14),
		textCell(3, "GATS XVII", 149.45, 75.18, 210.63, 82.93),
		textCell(4, "Reservation", 149.32, 88.32, 211.53, 96.19),
		textCell(5, "(1994)", 149.74, 101.32, 177.48, 111.85),
		textCell(6, "Foreign", 220.08, 75.06, 260.90, 85.70),
		textCell(7, "Ownership", 220.11, 88.12, 275.93, 98.82),
		textCell(8, "Permitted", 220.10, 101.34, 271.89, 109.45),
		textCell(9, "Restrictions on Foreign", 289.00, 75.06, 408.05, 85.70),
		textCell(10, "Ownership", 289.05, 88.12, 344.87, 98.82),
		textCell(11, "Foreign", 453.86, 75.06, 494.68, 85.70),
		textCell(12, "Ownership", 453.89, 88.12, 512.93, 98.82),
		textCell(13, "Reporting", 453.84, 101.58, 506.88, 112.22),
		textCell(14, "Requirements", 453.84, 114.84, 527.03, 125.34),
		textCell(15, "by persons of same nationality", 288.70, 128.65, 439.96, 139.73),
		textCell(16, "must not exceed 40% of the", 288.86, 142.27, 423.92, 150.61),
		textCell(17, "quarter.", 288.94, 157.17, 329.92, 166.88),
		textCell(18, "Canada", 78.08, 170.11, 114.83, 178.23),
		textCell(19, "Y", 149.02, 170.44, 156.02, 178.12),
		textCell(20, "Y", 219.76, 170.44, 226.76, 178.12),
		textCell(21, "Prohibition on ownership of", 288.86, 170.11, 428.72, 181.04),
		textCell(22, "residential property with", 288.80, 183.79, 413.41, 194.87),
		textCell(23, "exceptions; some provinces", 288.93, 197.92, 424.38, 208.40),
		textCell(24, "also restrict ownership,", 288.98, 211.15, 404.89, 222.08),
		textCell(25, "including of agricultural land.", 288.89, 224.83, 434.18, 235.91),
		textCell(26, "Chile", 78.08, 238.99, 103.18, 247.11),
		textCell(27, "N", 149.18, 239.39, 157.83, 247.14),
		textCell(28, "Y", 219.76, 239.32, 226.76, 247.00),
		textCell(29, "Prohibition on acquisition of", 288.86, 238.99, 430.13, 249.92),
		textCell(30, "public lands within 10", 288.84, 252.67, 399.63, 263.60),
		textCell(31, "kilometers from the border and", 288.81, 266.29, 444.06, 274.41),
		textCell(32, "favorable military report", 288.74, 279.97, 411.66, 291.05),
		textCell(33, "required for acquisition of land", 288.80, 293.65, 443.14, 304.58),
		textCell(34, "5 kilometers from the coast;", 288.75, 307.33, 425.61, 316.90),
		textCell(35, "nationals of bordering", 288.75, 321.01, 399.43, 332.09),
		textCell(36, "countries and legal persons", 288.86, 334.69, 424.42, 345.77),
		textCell(37, "with their principal place of", 288.72, 348.31, 426.58, 359.24),
		textCell(38, "business in one of those", 288.70, 361.99, 406.76, 370.11),
		textCell(39, "countries cannot obtain rights", 288.86, 375.67, 436.42, 386.75),
		textCell(40, "to real estate located totally or", 288.85, 389.35, 437.44, 400.43),
		textCell(41, "partially in the border area.", 288.84, 403.05, 424.54, 414.13),
		textCell(42, "China", 78.08, 417.21, 109.70, 425.33),
		textCell(43, "N (2001)", 149.18, 417.21, 189.35, 428.14),
		textCell(44, "N", 219.92, 417.61, 228.57, 425.36),
		textCell(45, "No individuals, domestic or", 288.86, 417.21, 427.46, 426.78),
		textCell(46, "foreign, can privately own", 288.74, 430.89, 416.97, 441.97),
		textCell(47, "land. The state grants land use", 288.88, 444.57, 439.65, 455.65),
		textCell(48, "rights to land users for a", 288.80, 458.19, 409.87, 469.27),
		textCell(49, "certain number of years.", 288.86, 471.88, 409.49, 482.95),
		textCell(50, "Foreigners can obtain such", 288.82, 485.55, 421.38, 496.63),
		textCell(51, "land use rights, own residential", 288.88, 499.23, 444.67, 510.31),
		textCell(52, "houses and apartments, or", 288.78, 512.91, 420.05, 523.84),
		textCell(53, "incorporate foreign-invested", 288.89, 526.53, 430.28, 537.61),
		textCell(54, "enterprises to invest in real", 288.93, 540.21, 422.78, 551.14),
		textCell(55, "estate.", 288.93, 555.11, 318.52, 562.01),
		textCell(56, "Egypt", 77.93, 568.45, 106.67, 579.13),
		textCell(57, "Y", 149.02, 568.38, 156.02, 576.06),
		textCell(58, "Y", 219.76, 568.38, 226.76, 576.06),
		textCell(59, "Prohibition on ownership of", 288.86, 568.05, 428.72, 578.98),
		textCell(60, "agriculture lands, land in Sinai", 288.98, 581.73, 440.74, 592.81),
		textCell(61, "Peninsula; otherwise,", 288.86, 595.41, 395.39, 604.98),
		textCell(62, "permitted to own up to two", 288.84, 609.09, 426.08, 620.02),
		textCell(63, "properties, up to 4,000 square", 288.84, 623.06, 435.06, 633.70),
		textCell(64, "meters, for residential", 288.86, 636.39, 397.81, 645.96),
		textCell(65, "purposes; no disposition for 5", 288.84, 650.07, 436.02, 661.00),
		textCell(66, "years; approval required to", 288.69, 663.75, 423.71, 674.83),
		textCell(67, "acquire land in tourist areas;", 288.98, 677.43, 429.78, 688.36),
		textCell(68, "joint ownership with an", 288.17, 691.13, 407.68, 702.21),
		textCell(69, "Egyptian who has majority", 288.91, 704.81, 422.93, 715.89),
		textCell(70, "The Law Library of Congress", 72.18, 745.48, 207.19, 756.07),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[69]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 6, data.NumRows)
	require.Equal(t, 5, data.NumCols)
	require.Equal(t, "Jurisdiction", data.Grid()[0][0].Text)
	require.Equal(t, "GATS XVII Reservation (1994)", data.Grid()[0][1].Text)
	require.Equal(t, "Foreign Ownership Permitted", data.Grid()[0][2].Text)
	require.Equal(t, "Restrictions on Foreign Ownership", data.Grid()[0][3].Text)
	require.Equal(t, "Foreign Ownership Reporting Requirements", data.Grid()[0][4].Text)
	require.Equal(t, "by persons of same nationality must not exceed 40% of the quarter.", data.Grid()[1][3].Text)
	require.Equal(t, "Canada", data.Grid()[2][0].Text)
	require.Equal(t, "Y", data.Grid()[2][1].Text)
	require.Equal(t, "Chile", data.Grid()[3][0].Text)
	require.Equal(t, "China", data.Grid()[4][0].Text)
	require.Contains(t, data.Grid()[4][3].Text, "No individuals, domestic or foreign")
	require.Equal(t, "Egypt", data.Grid()[5][0].Text)
	require.Contains(t, data.Grid()[5][3].Text, "Egyptian who has majority")
}

func TestDetectAnchoredTextTablesBuildsCaptionlessThreeColumnMultilineTextGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Risk area", 78, 80, 130, 90),
		textCell(2, "Evaluation", 150, 80, 206, 90),
		textCell(3, "Question", 260, 80, 322, 90),
		textCell(4, "Known and", 78, 106, 136, 116),
		textCell(5, "published issues", 78, 120, 154, 130),
		textCell(6, "cases", 78, 136, 122, 146),
		textCell(7, "Expert review", 150, 106, 222, 116),
		textCell(8, "field study", 150, 120, 210, 130),
		textCell(9, "Can teams complete a long process?", 260, 106, 470, 116),
		textCell(10, "Can reviewers identify failures?", 260, 120, 442, 130),
		textCell(11, "Novel system", 78, 160, 142, 170),
		textCell(12, "behaviour", 78, 174, 128, 184),
		textCell(13, "Scenario trial", 150, 160, 224, 170),
		textCell(14, "model check", 150, 174, 214, 184),
		textCell(15, "Can specialists construct a safe plan?", 260, 160, 486, 170),
		textCell(16, "Can monitors catch regressions?", 260, 174, 446, 184),
		textCell(17, "late review", 150, 148, 214, 158),
		textCell(18, "Can late continuation stay attached?", 260, 148, 498, 158),
		textCell(19, "Follow up", 150, 208, 206, 218),
		textCell(20, "Can reviewers confirm mitigations?", 260, 208, 470, 218),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Empty(t, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 3, data.NumCols)
	require.Equal(t, "Risk area", data.Grid()[0][0].Text)
	require.Equal(t, "Evaluation", data.Grid()[0][1].Text)
	require.Equal(t, "Question", data.Grid()[0][2].Text)
	require.Equal(t, "Known and published issues cases", data.Grid()[1][0].Text)
	require.Equal(t, "Expert review field study late review", data.Grid()[1][1].Text)
	require.Equal(t, "Can teams complete a long process? Can reviewers identify failures? Can late continuation stay attached?", data.Grid()[1][2].Text)
	require.Equal(t, "Novel system behaviour", data.Grid()[2][0].Text)
	require.Equal(t, "Scenario trial model check Follow up", data.Grid()[2][1].Text)
	require.Equal(t, "Can specialists construct a safe plan? Can monitors catch regressions? Can reviewers confirm mitigations?", data.Grid()[2][2].Text)
}

func TestDetectTextTablesKeepsCaptionBelowCaptionlessThreeColumnTable(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "Risk area", 78, 80, 130, 90),
		textCell(2, "Evaluation", 150, 80, 206, 90),
		textCell(3, "Question", 260, 80, 322, 90),
		textCell(4, "Known and", 78, 106, 136, 116),
		textCell(5, "published issues", 78, 120, 154, 130),
		textCell(6, "cases", 78, 136, 122, 146),
		textCell(7, "Expert review", 150, 106, 222, 116),
		textCell(8, "field study", 150, 120, 210, 130),
		textCell(9, "Can teams complete a long process?", 260, 106, 470, 116),
		textCell(10, "Can reviewers identify failures?", 260, 120, 442, 130),
		textCell(11, "Novel system", 78, 160, 142, 170),
		textCell(12, "behaviour", 78, 174, 128, 184),
		textCell(13, "Scenario trial", 150, 160, 224, 170),
		textCell(14, "model check", 150, 174, 214, 184),
		textCell(15, "Can specialists construct a safe plan?", 260, 160, 486, 170),
		textCell(16, "Can monitors catch regressions?", 260, 174, 446, 184),
		textCell(17, "Follow up", 150, 208, 206, 218),
		textCell(18, "Can reviewers confirm mitigations?", 260, 208, 470, 218),
		textCell(19, "[Table A] Evaluation catalogue.", 78, 242, 254, 252),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Len(t, result.TextCells, 1)
	require.Equal(t, "[Table A] Evaluation catalogue.", result.TextCells[0].Text)

	data := result.Tables[0].Data
	require.Equal(t, 3, data.NumRows)
	require.Equal(t, 3, data.NumCols)
	require.Equal(t, "Risk area", data.Grid()[0][0].Text)
	require.Equal(t, "Novel system behaviour", data.Grid()[2][0].Text)
	require.Equal(t, "Scenario trial model check Follow up", data.Grid()[2][1].Text)
}

func TestDetectAnchoredTextTablesDoesNotPromoteNumericChartGridToWideTextTable(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Figure 2.1: Surveyed MSMEs by size across sectors (%)", 41.16, 256.29, 286.13, 265.48),
		textCell(2, "100", 134.28, 295.72, 145.00, 304.50),
		textCell(3, "80", 137.85, 320.64, 145.00, 329.42),
		textCell(4, "60", 137.85, 345.56, 145.00, 354.34),
		textCell(5, "40", 137.85, 370.48, 145.00, 379.26),
		textCell(6, "20", 137.85, 395.40, 145.00, 404.18),
		textCell(7, "0", 141.43, 420.32, 145.00, 429.10),
		textCell(8, "2", 180.85, 291.57, 184.43, 300.35),
		textCell(9, "1", 244.77, 291.57, 248.34, 300.35),
		textCell(10, "4", 308.78, 291.57, 312.35, 300.35),
		textCell(11, "1", 372.79, 291.57, 376.36, 300.35),
		textCell(12, "40", 179.07, 322.14, 186.22, 330.92),
		textCell(13, "37", 242.98, 319.32, 250.13, 328.10),
		textCell(14, "40", 306.99, 324.55, 314.14, 333.33),
		textCell(15, "50", 371.00, 327.36, 378.15, 336.14),
		textCell(16, "58", 179.07, 384.00, 186.22, 392.78),
		textCell(17, "62", 242.98, 381.29, 250.13, 390.06),
		textCell(18, "56", 306.99, 385.11, 314.14, 393.88),
		textCell(19, "49", 371.00, 389.52, 378.15, 398.30),
		textCell(20, "All MSMEs", 168.89, 429.47, 196.40, 437.15),
		textCell(21, "Tourism", 232.00, 429.47, 260.00, 437.15),
		textCell(22, "Handicraft/Textile", 292.00, 429.47, 352.00, 437.15),
		textCell(23, "Agriculture", 360.00, 429.47, 400.00, 437.15),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Empty(t, result.Tables)
	require.Equal(t, lineCells, result.TextCells)
}

func TestDetectAnchoredTextTablesDoesNotPromoteCitationFragmentsToWideTextTable(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "els made it difficult to ensure precise and task\u0002", 71.14, 520.30, 290.41, 530.11),
		textCell(2, "oriented responses. The need for a more targeted", 71.19, 533.85, 289.04, 543.67),
		textCell(3, "approach arose from the limitations of existing", 71.28, 547.40, 288.80, 557.22),
		textCell(4, "methods, leading to the development of instruc\u0002", 71.04, 560.95, 290.41, 570.77),
		textCell(5, "tion tuning. This targeted approach enables better", 71.01, 574.50, 289.35, 584.32),
		textCell(6, "control over the model’s behavior, making it more", 71.14, 588.05, 288.92, 597.87),
		textCell(7, "suitable for specific tasks and improving its overall", 71.41, 601.60, 288.90, 611.41),
		textCell(8, "performance in alignment with user-defined objec\u0002", 70.92, 615.15, 290.43, 624.96),
		textCell(9, "tives. Therefore, instruction tuning is computation\u0002", 71.01, 628.70, 290.43, 638.51),
		textCell(10, "ally efficient and facilitates the rapid adaptation", 71.28, 642.24, 288.97, 652.06),
		textCell(11, "of LLMs to a specific domain without requiring", 71.19, 655.79, 288.80, 665.61),
		textCell(12, "extensive retraining or architectural changes.", 71.14, 669.34, 265.52, 679.16),
		textCell(13, "B.5 Alignment Tuning", 71.04, 691.75, 181.82, 701.52),
		textCell(14, "LLM has been observed to generate sentences that", 71.00, 709.38, 289.15, 719.19),
		textCell(15, "may be perceived as linguistically incongruent by", 71.04, 722.93, 289.24, 732.74),
		textCell(16, "human readers since they learned not human inten\u0002", 70.96, 736.47, 290.43, 746.29),
		textCell(17, "tion, but only vast knowledge across various do\u0002", 71.01, 750.02, 290.41, 759.84),
		textCell(18, "mains in the pretraining step (Ziegler et al.", 71.04, 763.57, 256.55, 773.39),
		textCell(19, ",", 257.92, 769.91, 259.44, 772.55),
		textCell(20, "2019).", 263.11, 763.65, 290.29, 772.94),
		textCell(21, "B.6 Data Contamination", 306.32, 494.10, 427.78, 501.83),
		textCell(22, "Recent researches (Zhou et al.", 306.33, 519.69, 439.59, 529.06),
		textCell(23, ",", 440.98, 526.03, 442.53, 528.67),
		textCell(24, "2023; Sainz et al.", 446.20, 519.69, 522.22, 528.67),
		textCell(25, ",", 523.61, 526.03, 525.16, 528.67),
		textCell(26, "2023; Golchin and Surdeanu", 306.48, 533.24, 436.55, 542.22),
		textCell(27, ",", 437.41, 539.58, 438.96, 542.22),
		textCell(28, "2023; Deng et al.", 443.68, 533.24, 522.22, 543.06),
		textCell(29, ",", 523.61, 539.58, 525.16, 542.22),
		textCell(30, "2023) emphasize the need to measure whether a", 306.48, 546.79, 524.39, 556.59),
		textCell(31, "specific benchmark was used to train the large lan\u0002", 306.70, 560.34, 525.70, 570.15),
		textCell(32, "guage models. There are three types of the data", 306.45, 573.88, 524.39, 583.70),
		textCell(33, "contamination: guideline, raw text and annota\u0002", 306.42, 587.43, 525.68, 597.25),
		textCell(34, "tion (Sainz et al.", 306.28, 600.98, 374.76, 610.35),
		textCell(35, ",", 376.09, 607.32, 377.58, 609.96),
		textCell(36, "2023). Guideline contamination", 380.91, 600.90, 524.23, 610.35),
		textCell(37, "occurs when a model accesses detailed annotation", 306.46, 614.53, 524.25, 622.12),
		textCell(38, "guidelines for a dataset, providing advantages in", 306.45, 628.08, 524.25, 637.90),
		textCell(39, "specific tasks, and its impact should be considered,", 306.69, 641.63, 525.18, 651.44),
		textCell(40, "especially in zero and few-shot evaluations. Raw", 306.42, 655.18, 524.64, 665.00),
		textCell(41, "text contamination occurs when a model has ac\u0002", 306.36, 668.64, 525.69, 676.32),
		textCell(42, "cess to the original text. Wikipedia is widely used", 306.42, 682.28, 524.31, 692.10),
		textCell(43, "as a pretraining data, but also as a source for cre\u0002", 306.55, 695.83, 525.68, 705.65),
		textCell(44, "ating new datasets. The caution is advised in the", 306.55, 709.38, 524.19, 719.19),
		textCell(45, "development of automatically annotated datasets", 306.44, 722.93, 523.96, 732.74),
		textCell(46, "sourced from the web. Annotation contamina\u0002", 306.71, 736.39, 525.71, 744.07),
		textCell(47, "tion occurs when the annotations of the specific", 306.36, 749.94, 524.05, 759.83),
		textCell(48, "benchmark are exposed during model training.", 306.17, 763.57, 508.82, 773.39),
	}

	result := table.DetectAnchoredTextTables(lineCells, lineCells, table.DetectionOptions{})

	require.Empty(t, result.Tables)
	require.Equal(t, lineCells, result.TextCells)
}

func TestDetectAnchoredTextTablesSplitsMergedHeaderOverAlignedDataRows(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Intro", 0, 0, 40, 10),
		textCell(2, "Saccharometer DI Water Glucose Solution Yeast Suspension", 74, 40, 385, 51),
		textCell(3, "2", 74, 58, 80, 68),
		textCell(4, "24 ml", 155, 58, 182, 68),
		textCell(5, "0 ml", 207, 58, 228, 68),
		textCell(6, "4 ml", 296, 58, 317, 68),
		textCell(7, "3", 75, 76, 80, 86),
		textCell(8, "12 ml", 156, 76, 182, 86),
		textCell(9, "12 ml", 208, 76, 234, 86),
		textCell(10, "4 ml", 296, 76, 317, 86),
		textCell(11, "4", 74, 94, 80, 104),
		textCell(12, "4 ml", 155, 94, 176, 104),
		textCell(13, "12 ml", 208, 94, 234, 104),
		textCell(14, "12 ml", 297, 94, 323, 104),
		textCell(15, "Outro", 0, 140, 40, 150),
	}
	wordCells := []page.TextCell{
		textCell(1, "Saccharometer", 74, 40, 151, 51),
		textCell(2, "DI", 155, 40, 168, 51),
		textCell(3, "Water", 171, 40, 203, 51),
		textCell(4, "Glucose", 207, 40, 247, 51),
		textCell(5, "Solution", 251, 40, 293, 51),
		textCell(6, "Yeast", 296, 40, 324, 51),
		textCell(7, "Suspension", 328, 40, 385, 51),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{
		MinRows:              4,
		MinCols:              4,
		RowTolerance:         6,
		ColumnTolerance:      12,
		TextOverlapThreshold: 0.3,
	})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[14]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 4, data.NumRows)
	require.Equal(t, 4, data.NumCols)
	require.Equal(t, "Saccharometer", data.Grid()[0][0].Text)
	require.Equal(t, "DI Water", data.Grid()[0][1].Text)
	require.Equal(t, "Glucose Solution", data.Grid()[0][2].Text)
	require.Equal(t, "Yeast Suspension", data.Grid()[0][3].Text)
	require.Equal(t, "12 ml", data.Grid()[3][3].Text)

	markdown, err := render.Table(data)
	require.NoError(t, err)
	require.Equal(t, "| Saccharometer | DI Water | Glucose Solution | Yeast Suspension |\n| ------------: | -------- | ---------------- | ---------------- |\n|             2 | 24 ml    | 0 ml             | 4 ml             |\n|             3 | 12 ml    | 12 ml            | 4 ml             |\n|             4 | 4 ml     | 12 ml            | 12 ml            |\n", markdown)
}

func TestDetectAnchoredTextTablesIgnoresTwoColumnProse(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Alpha beta", 0, 0, 100, 10),
		textCell(2, "gamma", 140, 0, 180, 10),
		textCell(3, "Delta epsilon", 0, 22, 120, 32),
		textCell(4, "zeta", 140, 22, 170, 32),
		textCell(5, "Eta theta", 0, 44, 108, 54),
		textCell(6, "iota", 140, 44, 170, 54),
		textCell(7, "Kappa lambda", 0, 66, 118, 76),
		textCell(8, "mu", 140, 66, 158, 76),
	}
	wordCells := []page.TextCell{
		textCell(1, "Alpha", 0, 0, 35, 10),
		textCell(2, "beta", 70, 0, 100, 10),
		textCell(3, "gamma", 140, 0, 180, 10),
		textCell(4, "Delta", 0, 22, 35, 32),
		textCell(5, "epsilon", 70, 22, 122, 32),
		textCell(6, "zeta", 140, 22, 170, 32),
		textCell(7, "Eta", 0, 44, 22, 54),
		textCell(8, "theta", 70, 44, 108, 54),
		textCell(9, "iota", 140, 44, 170, 54),
		textCell(10, "Kappa", 0, 66, 42, 76),
		textCell(11, "lambda", 70, 66, 118, 76),
		textCell(12, "mu", 140, 66, 158, 76),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{
		MinRows: 4,
		MinCols: 2,
	})

	require.Empty(t, result.Tables)
}

func TestDetectAnchoredTextTablesDoesNotTreatNotableAsTableCaption(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Model Size Score Rank", 0, 0, 260, 10),
		textCell(2, "Solar", 0, 22, 35, 32),
		textCell(3, "10B", 70, 22, 96, 32),
		textCell(4, "98", 140, 22, 156, 32),
		textCell(5, "1", 210, 22, 218, 32),
		textCell(6, "Qwen", 0, 44, 35, 54),
		textCell(7, "72B", 70, 44, 96, 54),
		textCell(8, "95", 140, 44, 156, 54),
		textCell(9, "2", 210, 44, 218, 54),
		textCell(10, "A Comparative Summary Table appears later", 0, 92, 220, 102),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 0, 0, 35, 10),
		textCell(2, "Size", 70, 0, 100, 10),
		textCell(3, "Score", 140, 0, 178, 10),
		textCell(4, "Rank", 210, 0, 240, 10),
		textCell(5, "Solar", 0, 22, 35, 32),
		textCell(6, "10B", 70, 22, 96, 32),
		textCell(7, "98", 140, 22, 156, 32),
		textCell(8, "1", 210, 22, 218, 32),
		textCell(9, "Qwen", 0, 44, 35, 54),
		textCell(10, "72B", 70, 44, 96, 54),
		textCell(11, "95", 140, 44, 156, 54),
		textCell(12, "2", 210, 44, 218, 54),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{
		MinRows: 3,
		MinCols: 4,
	})

	require.Empty(t, result.Tables)
}

func TestDetectAnchoredTextTablesRequiresFollowingCaptionCue(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Table 1: Previous caption", 0, 0, 180, 10),
		textCell(2, "Model Size Score Rank", 0, 36, 260, 46),
		textCell(3, "Solar", 0, 58, 35, 68),
		textCell(4, "10B", 70, 58, 96, 68),
		textCell(5, "98", 140, 58, 156, 68),
		textCell(6, "1", 210, 58, 218, 68),
		textCell(7, "Qwen", 0, 80, 35, 90),
		textCell(8, "72B", 70, 80, 96, 90),
		textCell(9, "95", 140, 80, 156, 90),
		textCell(10, "2", 210, 80, 218, 90),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 0, 36, 35, 46),
		textCell(2, "Size", 70, 36, 100, 46),
		textCell(3, "Score", 140, 36, 178, 46),
		textCell(4, "Rank", 210, 36, 240, 46),
		textCell(5, "Solar", 0, 58, 35, 68),
		textCell(6, "10B", 70, 58, 96, 68),
		textCell(7, "98", 140, 58, 156, 68),
		textCell(8, "1", 210, 58, 218, 68),
		textCell(9, "Qwen", 0, 80, 35, 90),
		textCell(10, "72B", 70, 80, 96, 90),
		textCell(11, "95", 140, 80, 156, 90),
		textCell(12, "2", 210, 80, 218, 90),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{
		MinRows: 3,
		MinCols: 4,
	})

	require.Empty(t, result.Tables)
}

func TestDetectAnchoredTextTablesBuildsCaptionedWordGridWithSplitDecimalTokens(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Model English Arabic Chinese French", 0, 0, 370, 10),
		textCell(2, "Vendor Alpha 97 . 64% 97 . 90% 97 . 53% 97 . 78%", 0, 24, 370, 34),
		textCell(3, "Vendor Beta 4 . 6 98 . 00% 98 . 93% 98 . 36% 98 . 29%", 0, 48, 370, 58),
		textCell(4, "Vendor Gamma 4 . 6 98 . 37% 99 . 71% 99 . 36% 99 . 16%", 0, 72, 370, 82),
		textCell(5, "[Table 8.1.1.1.B] Results by language.", 0, 108, 220, 118),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 0, 0, 35, 10),
		textCell(2, "English", 120, 0, 158, 10),
		textCell(3, "Arabic", 190, 0, 224, 10),
		textCell(4, "Chinese", 260, 0, 302, 10),
		textCell(5, "French", 330, 0, 366, 10),
		textCell(6, "Vendor", 0, 24, 36, 34),
		textCell(7, "Alpha", 40, 24, 78, 34),
		textCell(8, "97", 120, 24, 132, 34),
		textCell(9, ".", 133, 30, 136, 40),
		textCell(10, "64%", 138, 24, 160, 34),
		textCell(11, "97", 190, 24, 202, 34),
		textCell(12, ".", 203, 30, 206, 40),
		textCell(13, "90%", 208, 24, 230, 34),
		textCell(14, "97", 260, 24, 272, 34),
		textCell(15, ".", 273, 30, 276, 40),
		textCell(16, "53%", 278, 24, 300, 34),
		textCell(17, "97", 330, 24, 342, 34),
		textCell(18, ".", 343, 30, 346, 40),
		textCell(19, "78%", 348, 24, 370, 34),
		textCell(20, "Vendor", 0, 48, 36, 58),
		textCell(21, "Beta", 40, 48, 78, 58),
		textCell(22, "4", 82, 48, 88, 58),
		textCell(23, ".", 89, 54, 92, 64),
		textCell(24, "6", 94, 48, 100, 58),
		textCell(25, "98", 120, 48, 132, 58),
		textCell(26, ".", 133, 54, 136, 64),
		textCell(27, "00%", 138, 48, 160, 58),
		textCell(28, "98", 190, 48, 202, 58),
		textCell(29, ".", 203, 54, 206, 64),
		textCell(30, "93%", 208, 48, 230, 58),
		textCell(31, "98", 260, 48, 272, 58),
		textCell(32, ".", 273, 54, 276, 64),
		textCell(33, "36%", 278, 48, 300, 58),
		textCell(34, "98", 330, 48, 342, 58),
		textCell(35, ".", 343, 54, 346, 64),
		textCell(36, "29%", 348, 48, 370, 58),
		textCell(37, "Vendor", 0, 72, 36, 82),
		textCell(38, "Gamma", 40, 72, 66, 82),
		textCell(39, "4", 70, 72, 76, 82),
		textCell(40, ".", 77, 78, 80, 88),
		textCell(41, "6", 82, 72, 88, 82),
		textCell(42, "98", 120, 72, 132, 82),
		textCell(43, ".", 133, 78, 136, 88),
		textCell(44, "37%", 138, 72, 160, 82),
		textCell(45, "99", 190, 72, 202, 82),
		textCell(46, ".", 203, 78, 206, 88),
		textCell(47, "71%", 208, 72, 230, 82),
		textCell(48, "99", 260, 72, 272, 82),
		textCell(49, ".", 273, 78, 276, 88),
		textCell(50, "36%", 278, 72, 300, 82),
		textCell(51, "99", 330, 72, 342, 82),
		textCell(52, ".", 343, 78, 346, 88),
		textCell(53, "16%", 348, 72, 370, 82),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	data := result.Tables[0].Data
	require.Equal(t, 4, data.NumRows)
	require.Equal(t, 5, data.NumCols)
	require.Equal(t, "Model", data.Grid()[0][0].Text)
	require.Equal(t, "Vendor Alpha", data.Grid()[1][0].Text)
	require.Equal(t, "97 . 64%", data.Grid()[1][1].Text)
	require.Equal(t, "99 . 16%", data.Grid()[3][4].Text)
}

func TestDetectAnchoredTextTablesBuildsCaptionedWordGridAcrossLooseRowsWithFirstColumnContinuation(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "previous system cards due to routine evaluation updates.", 0, 0, 320, 10),
		textCell(2, "Model Overall harmless response rate", 0, 36, 260, 46),
		textCell(3, "English Arabic Chinese French Korean Russian Hindi", 120, 68, 470, 78),
		textCell(4, "Vendor Alpha 97.64% 97.90% 97.53% 97.78% 98.01% 97.97% 98.06%", 0, 100, 470, 110),
		textCell(5, "Preview", 0, 114, 42, 124),
		textCell(6, "Vendor Beta 4.6 98.00% 98.93% 98.36% 98.29% 98.78% 98.04% 99.32%", 0, 146, 470, 156),
		textCell(7, "Vendor Gamma 4.6 98.37% 99.71% 99.36% 99.16% 99.51% 99.20% 99.59%", 0, 178, 470, 188),
		textCell(8, "[Table 8.1.1.1.B] Results by language.", 0, 210, 240, 220),
	}
	wordCells := []page.TextCell{
		textCell(1, "Model", 0, 36, 35, 46),
		textCell(2, "Overall", 250, 36, 292, 46),
		textCell(3, "harmless", 296, 36, 346, 46),
		textCell(4, "response", 350, 36, 402, 46),
		textCell(5, "rate", 406, 36, 430, 46),
		textCell(6, "English", 120, 68, 158, 78),
		textCell(7, "Arabic", 170, 68, 204, 78),
		textCell(8, "Chinese", 220, 68, 262, 78),
		textCell(9, "French", 270, 68, 306, 78),
		textCell(10, "Korean", 320, 68, 358, 78),
		textCell(11, "Russian", 370, 68, 414, 78),
		textCell(12, "Hindi", 420, 68, 450, 78),
		textCell(13, "Vendor", 0, 100, 36, 110),
		textCell(14, "Alpha", 40, 100, 78, 110),
		textCell(15, "97.64%", 120, 100, 160, 110),
		textCell(16, "97.90%", 170, 100, 210, 110),
		textCell(17, "97.53%", 220, 100, 260, 110),
		textCell(18, "97.78%", 270, 100, 310, 110),
		textCell(19, "98.01%", 320, 100, 360, 110),
		textCell(20, "97.97%", 370, 100, 410, 110),
		textCell(21, "98.06%", 420, 100, 460, 110),
		textCell(22, "Preview", 0, 114, 42, 124),
		textCell(23, "Vendor", 0, 146, 36, 156),
		textCell(24, "Beta", 40, 146, 78, 156),
		textCell(25, "4.6", 82, 146, 100, 156),
		textCell(26, "98.00%", 120, 146, 160, 156),
		textCell(27, "98.93%", 170, 146, 210, 156),
		textCell(28, "98.36%", 220, 146, 260, 156),
		textCell(29, "98.29%", 270, 146, 310, 156),
		textCell(30, "98.78%", 320, 146, 360, 156),
		textCell(31, "98.04%", 370, 146, 410, 156),
		textCell(32, "99.32%", 420, 146, 460, 156),
		textCell(33, "Vendor", 0, 178, 36, 188),
		textCell(34, "Gamma", 40, 178, 66, 188),
		textCell(35, "4.6", 70, 178, 88, 188),
		textCell(36, "98.37%", 120, 178, 160, 188),
		textCell(37, "99.71%", 170, 178, 210, 188),
		textCell(38, "99.36%", 220, 178, 260, 188),
		textCell(39, "99.16%", 270, 178, 310, 188),
		textCell(40, "99.51%", 320, 178, 360, 188),
		textCell(41, "99.20%", 370, 178, 410, 188),
		textCell(42, "99.59%", 420, 178, 460, 188),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	grid := result.Tables[0].Data.Grid()
	require.Equal(t, "Model", grid[0][0].Text)
	require.Equal(t, "English", grid[0][1].Text)
	require.Equal(t, "Vendor Alpha Preview", grid[1][0].Text)
	require.Equal(t, "98.06%", grid[1][7].Text)
	require.Equal(t, "Vendor Gamma 4.6", grid[3][0].Text)
	require.Equal(t, "99.59%", grid[3][7].Text)
}

func TestDetectAnchoredTextTablesBuildsShortCaptionedWordGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Table 8: Task names that we use to filter data for FLAN", 70.72, 344.54, 288.99, 351.48),
		textCell(2, "derived datasets such as OpenOrca.", 71.13, 356.50, 211.15, 365.46),
		textCell(3, "ARC HellaSwag MMLU TruthfulQA Winogrande GSM8K", 76.85, 394.91, 283.27, 401.54),
		textCell(4, "0.06 N/A 0.15 0.28 N/A 0.70", 78.04, 407.87, 277.05, 413.02),
		textCell(5, "Table 9: Data contamination test results for SOLAR", 70.73, 429.11, 289.06, 436.13),
		textCell(6, "10.7B-Instruct. We show 'result < 0.1, %' values where", 71.21, 441.07, 288.94, 449.27),
	}
	wordCells := []page.TextCell{
		textCell(1, "ARC", 76.85, 394.97, 91.62, 400.04),
		textCell(2, "HellaSwag", 100.08, 394.91, 131.94, 401.54),
		textCell(3, "MMLU", 140.32, 395.07, 163.00, 400.04),
		textCell(4, "TruthfulQA", 171.32, 394.91, 205.98, 401.24),
		textCell(5, "Winogrande", 214.20, 394.91, 250.10, 401.54),
		textCell(6, "GSM8K", 258.55, 394.97, 283.27, 400.04),
		textCell(7, "0.06", 78.04, 407.89, 90.51, 413.02),
		textCell(8, "N/A", 109.80, 407.95, 122.27, 413.02),
		textCell(9, "0.15", 145.41, 407.87, 157.66, 413.02),
		textCell(10, "0.28", 182.38, 407.95, 194.68, 413.02),
		textCell(11, "N/A", 225.96, 407.95, 238.43, 413.02),
		textCell(12, "0.70", 264.53, 407.95, 277.05, 413.02),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[0], lineCells[1], lineCells[4], lineCells[5]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 2, data.NumRows)
	require.Equal(t, 6, data.NumCols)
	require.Equal(t, "ARC", data.Grid()[0][0].Text)
	require.Equal(t, "HellaSwag", data.Grid()[0][1].Text)
	require.Equal(t, "MMLU", data.Grid()[0][2].Text)
	require.Equal(t, "TruthfulQA", data.Grid()[0][3].Text)
	require.Equal(t, "Winogrande", data.Grid()[0][4].Text)
	require.Equal(t, "GSM8K", data.Grid()[0][5].Text)
	require.Equal(t, "0.06", data.Grid()[1][0].Text)
	require.Equal(t, "N/A", data.Grid()[1][1].Text)
	require.Equal(t, "0.15", data.Grid()[1][2].Text)
	require.Equal(t, "0.28", data.Grid()[1][3].Text)
	require.Equal(t, "N/A", data.Grid()[1][4].Text)
	require.Equal(t, "0.70", data.Grid()[1][5].Text)
}

func TestDetectAnchoredTextTablesBuildsCaptionedHierarchicalWordGrid(t *testing.T) {
	t.Parallel()

	lineCells := []page.TextCell{
		textCell(1, "Properties", 103.51, 83.84, 132.31, 90.32),
		textCell(2, "Training Datasets", 315.45, 74.89, 365.80, 81.38),
		textCell(3, "Instruction Alignment", 222.48, 83.84, 433.02, 90.33),
		textCell(4, "Alpaca-GPT4 OpenOrca Synth. Math-Instruct Orca DPO Pairs Ultrafeedback Cleaned Synth. Math-Alignment", 165.13, 96.41, 516.41, 102.90),
		textCell(5, "Total # Samples 52K 2.91M 126K 12.9K 60.8K 126K", 94.99, 108.94, 489.77, 115.46),
		textCell(6, "Maximum # Samples Used 52K 100K 52K 12.9K 60.8K 20.1K", 78.96, 117.89, 490.68, 124.41),
		textCell(7, "Open Source", 99.54, 126.93, 136.56, 133.35),
		textCell(8, "O", 182.67, 126.93, 187.39, 131.89),
		textCell(9, "O", 225.38, 126.93, 230.09, 131.89),
		textCell(10, "X", 278.61, 126.70, 282.73, 131.80),
		textCell(11, "O", 339.60, 126.93, 344.31, 131.89),
		textCell(12, "O", 403.76, 126.93, 408.47, 131.89),
		textCell(13, "X", 479.71, 126.70, 483.82, 131.80),
		textCell(14, "Table 1: Training datasets used for the instruction and alignment tuning stages, respectively.", 70.73, 147.88, 524.25, 156.85),
	}
	wordCells := []page.TextCell{
		textCell(1, "Properties", 103.51, 83.84, 132.31, 90.32),
		textCell(2, "Training", 315.45, 74.89, 339.67, 81.38),
		textCell(3, "Datasets", 341.80, 75.04, 365.80, 79.88),
		textCell(4, "Instruction", 222.48, 83.84, 253.46, 88.83),
		textCell(5, "Alignment", 402.30, 83.84, 433.02, 90.33),
		textCell(6, "Alpaca-GPT4", 165.13, 96.41, 204.84, 102.89),
		textCell(7, "OpenOrca", 213.18, 96.46, 242.52, 102.89),
		textCell(8, "Synth.", 250.74, 96.41, 268.56, 102.90),
		textCell(9, "Math-Instruct", 271.37, 96.41, 310.91, 101.40),
		textCell(10, "Orca", 319.05, 96.46, 332.78, 101.43),
		textCell(11, "DPO", 334.71, 96.46, 348.76, 101.43),
		textCell(12, "Pairs", 350.92, 96.41, 364.81, 101.40),
		textCell(13, "Ultrafeedback", 373.11, 96.41, 413.85, 101.43),
		textCell(14, "Cleaned", 415.82, 96.41, 439.16, 101.43),
		textCell(15, "Synth.", 447.43, 96.41, 465.24, 102.90),
		textCell(16, "Math-Alignment", 468.06, 96.41, 516.41, 102.90),
		textCell(17, "Total", 94.99, 108.98, 109.35, 113.96),
		textCell(18, "#", 111.34, 109.13, 114.88, 113.90),
		textCell(19, "Samples", 117.01, 108.98, 140.83, 115.46),
		textCell(20, "52K", 179.05, 108.94, 191.24, 113.99),
		textCell(21, "2.91M", 218.44, 109.03, 237.05, 114.05),
		textCell(22, "126K", 273.46, 108.97, 288.68, 113.99),
		textCell(23, "12.9K", 333.85, 109.03, 350.87, 114.05),
		textCell(24, "60.8K", 397.46, 108.97, 415.03, 113.99),
		textCell(25, "126K", 474.56, 108.97, 489.77, 113.99),
		textCell(26, "Maximum", 78.96, 117.93, 108.88, 122.91),
		textCell(27, "#", 110.73, 118.08, 114.27, 122.85),
		textCell(28, "Samples", 116.40, 117.93, 140.22, 124.41),
		textCell(29, "Used", 142.42, 117.93, 157.06, 122.94),
		textCell(30, "52K", 179.06, 117.89, 191.24, 122.94),
		textCell(31, "100K", 220.53, 117.98, 235.75, 122.94),
		textCell(32, "52K", 274.70, 117.89, 286.88, 122.94),
		textCell(33, "12.9K", 333.85, 117.98, 350.87, 123.00),
		textCell(34, "60.8K", 397.46, 117.92, 415.03, 122.94),
		textCell(35, "20.1K", 473.08, 117.98, 490.68, 122.94),
		textCell(36, "Open", 99.54, 126.93, 114.79, 133.35),
		textCell(37, "Source", 117.00, 126.93, 136.56, 131.89),
		textCell(38, "O", 182.67, 126.93, 187.39, 131.89),
		textCell(39, "O", 225.38, 126.93, 230.09, 131.89),
		textCell(40, "X", 278.61, 126.70, 282.73, 131.80),
		textCell(41, "O", 339.60, 126.93, 344.31, 131.89),
		textCell(42, "O", 403.76, 126.93, 408.47, 131.89),
		textCell(43, "X", 479.71, 126.70, 483.82, 131.80),
	}

	result := table.DetectAnchoredTextTables(lineCells, wordCells, table.DetectionOptions{})

	require.Len(t, result.Tables, 1)
	require.Equal(t, []page.TextCell{lineCells[13]}, result.TextCells)

	data := result.Tables[0].Data
	require.Equal(t, 6, data.NumRows)
	require.Equal(t, 7, data.NumCols)
	grid := data.Grid()
	require.Equal(t, "Properties", grid[1][0].Text)
	require.Equal(t, "Instruction", grid[1][2].Text)
	require.Equal(t, "Alignment", grid[1][5].Text)
	require.Equal(t, "Alpaca-GPT4", grid[2][1].Text)
	require.Equal(t, "Total # Samples", grid[3][0].Text)
	require.Equal(t, "100K", grid[4][2].Text)
	require.Equal(t, "X", grid[5][6].Text)
}

func textCell(index int, text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index: index,
		Text:  text,
		Box:   geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

func ruling(x1, y1, x2, y2 float64) page.RulingSegment {
	return page.RulingSegment{
		FromX:  x1,
		FromY:  y1,
		ToX:    x2,
		ToY:    y2,
		Width:  1,
		Origin: geom.TopLeft,
	}
}
