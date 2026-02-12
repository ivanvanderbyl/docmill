package pdfmarkdown_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	pdfmarkdown "github.com/ivanvanderbyl/pdfmarkdown"
)

func TestTableDetection_SimpleGrid(t *testing.T) {
	// Test with a simple manufactured table structure
	page := &pdfmarkdown.Page{
		Number: 1,
		Width:  612,
		Height: 792,
		Paragraphs: []pdfmarkdown.Paragraph{
			{
				Lines: []pdfmarkdown.Line{
					{
						Words: []pdfmarkdown.EnrichedWord{
							{Text: "Name", Box: pdfmarkdown.Rect{X0: 100, Y0: 100, X1: 150, Y1: 115}},
							{Text: "Age", Box: pdfmarkdown.Rect{X0: 200, Y0: 100, X1: 230, Y1: 115}},
							{Text: "City", Box: pdfmarkdown.Rect{X0: 300, Y0: 100, X1: 340, Y1: 115}},
						},
					},
				},
			},
			{
				Lines: []pdfmarkdown.Line{
					{
						Words: []pdfmarkdown.EnrichedWord{
							{Text: "John", Box: pdfmarkdown.Rect{X0: 100, Y0: 130, X1: 140, Y1: 145}},
							{Text: "25", Box: pdfmarkdown.Rect{X0: 200, Y0: 130, X1: 220, Y1: 145}},
							{Text: "NYC", Box: pdfmarkdown.Rect{X0: 300, Y0: 130, X1: 330, Y1: 145}},
						},
					},
				},
			},
			{
				Lines: []pdfmarkdown.Line{
					{
						Words: []pdfmarkdown.EnrichedWord{
							{Text: "Jane", Box: pdfmarkdown.Rect{X0: 100, Y0: 160, X1: 140, Y1: 175}},
							{Text: "30", Box: pdfmarkdown.Rect{X0: 200, Y0: 160, X1: 220, Y1: 175}},
							{Text: "LA", Box: pdfmarkdown.Rect{X0: 300, Y0: 160, X1: 320, Y1: 175}},
						},
					},
				},
			},
		},
	}

	settings := pdfmarkdown.DefaultTableSettings()
	tables := pdfmarkdown.DetectTables(page, settings)

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
