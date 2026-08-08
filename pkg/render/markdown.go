// Package render serialises the document model (paragraphs, headings, lists, and
// reconstructed tables) to GitHub-flavoured Markdown.
package render

import (
	"bytes"
	"strconv"
	"strings"

	md "github.com/ivanvanderbyl/markdown"

	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

func Table(data table.Data) (string, error) {
	if data.NumRows == 0 || data.NumCols == 0 {
		return "", nil
	}

	// Markdown has no rowspan/colspan, so a cell spanning several grid slots
	// must contribute its text exactly once, at its anchor (StartRow, StartCol).
	// Grid() places the same cell in every covered slot; rendering each slot's
	// text verbatim would fabricate duplicate rows/columns for spanned cells.
	grid := data.Grid()
	slotText := func(row, col int) string {
		cell := grid[row][col]
		if cell.StartRow != row || cell.StartCol != col {
			return ""
		}
		return normaliseCellText(cell.Text)
	}

	header := make([]string, data.NumCols)
	for col := 0; col < data.NumCols; col++ {
		header[col] = slotText(0, col)
	}

	rows := make([][]string, 0, data.NumRows-1)
	for row := 1; row < data.NumRows; row++ {
		renderedRow := make([]string, data.NumCols)
		empty := true
		for col := 0; col < data.NumCols; col++ {
			renderedRow[col] = slotText(row, col)
			if renderedRow[col] != "" {
				empty = false
			}
		}
		// A row whose every slot is empty (typically the continuation rows of
		// spanned cells) carries no information in Markdown; drop it.
		if empty {
			continue
		}
		rows = append(rows, renderedRow)
	}

	var buf bytes.Buffer
	builder := md.NewMarkdown(&buf)
	builder.Table(md.TableSet{
		Header:    header,
		Rows:      rows,
		Alignment: tableAlignments(rows, data.NumCols),
	})
	if err := builder.Build(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func normaliseCellText(text string) string {
	replacer := strings.NewReplacer(
		"\r\n", " ",
		"\r", " ",
		"\n", " ",
		"\u200b", "",
		"\ufeff", "",
		"|", "&#124;",
	)
	text = strings.Join(strings.Fields(replacer.Replace(text)), " ")
	return text
}

func tableAlignments(rows [][]string, numCols int) []md.TableAlignment {
	alignments := make([]md.TableAlignment, numCols)
	for col := range numCols {
		if isNumericColumn(rows, col) {
			alignments[col] = md.AlignRight
		}
	}
	return alignments
}

func isNumericColumn(rows [][]string, col int) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if col >= len(row) {
			return false
		}
		value := strings.TrimSpace(row[col])
		if value == "" {
			continue
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return false
		}
	}
	return true
}
