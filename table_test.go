package docmill_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	docmill "github.com/ivanvanderbyl/docmill"
)

func TestTableDetection_SimpleGrid(t *testing.T) {
	// Test with a simple manufactured table structure
	page := &docmill.Page{
		Number: 1,
		Width:  612,
		Height: 792,
		Paragraphs: []docmill.Paragraph{
			{
				Lines: []docmill.Line{
					{
						Words: []docmill.EnrichedWord{
							{Text: "Name", Box: docmill.Rect{X0: 100, Y0: 100, X1: 150, Y1: 115}},
							{Text: "Age", Box: docmill.Rect{X0: 200, Y0: 100, X1: 230, Y1: 115}},
							{Text: "City", Box: docmill.Rect{X0: 300, Y0: 100, X1: 340, Y1: 115}},
						},
					},
				},
			},
			{
				Lines: []docmill.Line{
					{
						Words: []docmill.EnrichedWord{
							{Text: "John", Box: docmill.Rect{X0: 100, Y0: 130, X1: 140, Y1: 145}},
							{Text: "25", Box: docmill.Rect{X0: 200, Y0: 130, X1: 220, Y1: 145}},
							{Text: "NYC", Box: docmill.Rect{X0: 300, Y0: 130, X1: 330, Y1: 145}},
						},
					},
				},
			},
			{
				Lines: []docmill.Line{
					{
						Words: []docmill.EnrichedWord{
							{Text: "Jane", Box: docmill.Rect{X0: 100, Y0: 160, X1: 140, Y1: 175}},
							{Text: "30", Box: docmill.Rect{X0: 200, Y0: 160, X1: 220, Y1: 175}},
							{Text: "LA", Box: docmill.Rect{X0: 300, Y0: 160, X1: 320, Y1: 175}},
						},
					},
				},
			},
		},
	}

	settings := docmill.DefaultTableSettings()
	tables := docmill.DetectTables(page, settings)

	require.Greater(t, len(tables), 0, "Expected to detect at least one table")

	table := tables[0]
	fmt.Printf("\nDetected table: %d rows x %d cols\n", table.NumRows, table.NumCols)

	for i, row := range table.Rows {
		fmt.Printf("Row %d: ", i+1)
		for j, cell := range row.Cells {
			if j > 0 {
				fmt.Print(" | ")
			}
			fmt.Printf("%q", cell.Content)
		}
		fmt.Println()
	}

	require.Equal(t, 3, table.NumRows, "Expected 3 rows")
	require.Equal(t, 3, table.NumCols, "Expected 3 columns")
}
