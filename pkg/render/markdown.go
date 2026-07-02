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

	grid := data.Grid()
	header := make([]string, data.NumCols)
	for col := 0; col < data.NumCols; col++ {
		header[col] = normaliseCellText(grid[0][col].Text)
	}

	rows := make([][]string, 0, data.NumRows-1)
	for row := 1; row < data.NumRows; row++ {
		renderedRow := make([]string, data.NumCols)
		for col := 0; col < data.NumCols; col++ {
			renderedRow[col] = normaliseCellText(grid[row][col].Text)
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
