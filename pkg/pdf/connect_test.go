package pdf_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	doctable "github.com/ivanvanderbyl/docmill/pkg/table"
	"github.com/stretchr/testify/require"
)

func connectTableOptions() pdf.ExtractionOptions {
	return pdf.ExtractionOptions{
		DetectTables: true,
		ReadingOrder: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              3,
			MinCols:              2,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	}
}

// twoColumnTableCells builds a Name/Value table occupying the supplied vertical
// band. It is the same column geometry on every page so continuation pages
// align column-for-column.
func twoColumnTableCells(startIndex int, topY float64, header [2]string, rows [][2]string) []page.TextCell {
	cells := make([]page.TextCell, 0, 2*(1+len(rows)))
	index := startIndex
	y := topY
	cells = append(cells,
		pdfTextCell(index, header[0], 0, y, 35, y+10),
		pdfTextCell(index+1, header[1], 100, y, 140, y+10),
	)
	index += 2
	for _, row := range rows {
		y += 22
		cells = append(cells,
			pdfTextCell(index, row[0], 0, y, 28, y+10),
			pdfTextCell(index+1, row[1], 100, y, 108, y+10),
		)
		index += 2
	}
	return cells
}

func countTableSeparatorRows(markdown string) int {
	count := 0
	for line := range strings.SplitSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
			continue
		}
		body := strings.Trim(trimmed, "|")
		if strings.TrimSpace(body) == "" {
			continue
		}
		isSeparator := true
		for _, r := range body {
			switch r {
			case '-', ':', ' ', '|':
			default:
				isSeparator = false
			}
		}
		if isSeparator {
			count++
		}
	}
	return count
}

func TestConnectCrossPageTablesMergesGenuineContinuation(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 150, [2]string{"Name", "Value"}, [][2]string{{"Ada", "1"}, {"Bob", "2"}}),
			},
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 10, [2]string{"Cal", "3"}, [][2]string{{"Dee", "4"}, {"Eve", "5"}}),
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, connectTableOptions())

	require.NoError(t, err)
	// A single merged table: one header separator row only.
	require.Equal(t, 1, countTableSeparatorRows(got), "expected exactly one merged table\n%s", got)
	require.Contains(t, got, "| Name | Value |")
	// Page 2 rows become data rows of the merged table; its first row is data,
	// not a repeated header.
	require.Contains(t, got, "| Cal")
	require.Contains(t, got, "| Eve")
	require.Contains(t, got, "| Ada")
}

func TestConnectCrossPageTablesKeepsSeparateWhenHeaderRepeats(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 150, [2]string{"Name", "Value"}, [][2]string{{"Ada", "1"}, {"Bob", "2"}}),
			},
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 10, [2]string{"Name", "Value"}, [][2]string{{"Cal", "3"}, {"Dee", "4"}}),
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, connectTableOptions())

	require.NoError(t, err)
	// The continuation restated the header, so the tables stay separate: two
	// header separator rows.
	require.Equal(t, 2, countTableSeparatorRows(got), "expected two separate tables\n%s", got)
}

func TestConnectCrossPageTablesDoesNotMergeAcrossInterveningHeading(t *testing.T) {
	t.Parallel()

	page2 := twoColumnTableCells(2, 10, [2]string{"Cal", "3"}, [][2]string{{"Dee", "4"}, {"Eve", "5"}})
	// A heading precedes the page-2 table, breaking the continuation.
	page2 = append([]page.TextCell{pdfTextCellWithFont(1, "Section Two", 0, 2, 120, 7, 18)}, page2...)

	doc := fakeDocument{
		pages: []fakePage{
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 150, [2]string{"Name", "Value"}, [][2]string{{"Ada", "1"}, {"Bob", "2"}}),
			},
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: page2,
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables:   true,
		ReadingOrder:   true,
		DetectHeadings: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              3,
			MinCols:              2,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 2, countTableSeparatorRows(got), "expected two separate tables\n%s", got)
	require.Contains(t, got, "Section Two")
}

func TestConnectCrossPageTablesLeavesSinglePageUnchanged(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Intro", 0, 0, 40, 10),
		pdfTextCell(2, "Name", 0, 40, 35, 50),
		pdfTextCell(3, "Value", 100, 40, 140, 50),
		pdfTextCell(4, "Ada", 0, 62, 28, 72),
		pdfTextCell(5, "1", 100, 62, 108, 72),
		pdfTextCell(6, "Bob", 0, 84, 28, 94),
		pdfTextCell(7, "2", 100, 84, 108, 94),
		pdfTextCell(8, "Outro", 0, 130, 42, 140),
	}
	doc := fakeDocument{pages: []fakePage{{cells: cells}}}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, connectTableOptions())

	require.NoError(t, err)
	require.Equal(t, "Intro\n\n| Name | Value |\n| ---- | ----: |\n| Ada  |     1 |\n| Bob  |     2 |\n\nOutro", got)
}

func TestConnectCrossPageTablesMergesAcrossThreePages(t *testing.T) {
	t.Parallel()

	// The middle page table spans top to bottom (top region for the merge with
	// page 1, bottom region so it remains an open anchor for page 3).
	middleRows := [][2]string{{"Cal", "3"}, {"Dee", "4"}, {"Eve", "5"}, {"Fox", "6"}, {"Gil", "7"}, {"Hal", "8"}, {"Ivy", "9"}}
	doc := fakeDocument{
		pages: []fakePage{
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 150, [2]string{"Name", "Value"}, [][2]string{{"Ada", "1"}, {"Bob", "2"}}),
			},
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 10, [2]string{"Jay", "0"}, middleRows),
			},
			{
				size:  geom.Size{Width: 200, Height: 200},
				cells: twoColumnTableCells(1, 10, [2]string{"Kit", "11"}, [][2]string{{"Lou", "12"}, {"Moe", "13"}}),
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, connectTableOptions())

	require.NoError(t, err)
	require.Equal(t, 1, countTableSeparatorRows(got), "expected one merged table across three pages\n%s", got)
	require.Contains(t, got, "| Name | Value |")
	require.Contains(t, got, "| Jay")
	require.Contains(t, got, "| Moe")
}
