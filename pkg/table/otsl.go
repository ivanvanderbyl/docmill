package table

import (
	"regexp"
	"strings"
)

var (
	otslContainerRE = regexp.MustCompile(`(?s)<otsl>(.*)</otsl>`)
)

type OTSLResult struct {
	Sequence []string
	Cells    []Cell
	NumRows  int
	NumCols  int
}

type otslToken struct {
	tag  string
	text string
}

func (r OTSLResult) Data() Data {
	return Data{
		Cells:   append([]Cell(nil), r.Cells...),
		NumRows: r.NumRows,
		NumCols: r.NumCols,
	}
}

func ParseOTSL(text string) OTSLResult {
	if strings.TrimSpace(text) == "" {
		return OTSLResult{}
	}

	if match := otslContainerRE.FindStringSubmatch(text); len(match) == 2 {
		text = match[1]
	}

	tokens := parseOTSLTokens(text)
	if len(tokens) == 0 {
		return OTSLResult{}
	}

	sequence := make([]string, len(tokens))
	for i, token := range tokens {
		sequence[i] = token.tag
	}

	rows := splitOTSLRows(tokens)
	if len(rows) == 0 {
		return OTSLResult{Sequence: sequence}
	}

	numRows := len(rows)
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}

	grid := make([][]otslToken, numRows)
	for i, row := range rows {
		grid[i] = make([]otslToken, numCols)
		copy(grid[i], row)
	}

	cells := make([]Cell, 0, numRows*numCols)
	for rowIdx, row := range grid {
		for colIdx, token := range row {
			if !isContentToken(token.tag) {
				continue
			}

			colSpan := 1
			for col := colIdx + 1; col < numCols; col++ {
				if grid[rowIdx][col].tag == "lcel" || grid[rowIdx][col].tag == "xcel" {
					colSpan++
					continue
				}
				break
			}

			rowSpan := 1
			for row := rowIdx + 1; row < numRows; row++ {
				if grid[row][colIdx].tag == "ucel" || grid[row][colIdx].tag == "xcel" {
					rowSpan++
					continue
				}
				break
			}

			cells = append(cells, Cell{
				Text:         token.text,
				RowSpan:      rowSpan,
				ColSpan:      colSpan,
				StartRow:     rowIdx,
				EndRow:       rowIdx + rowSpan,
				StartCol:     colIdx,
				EndCol:       colIdx + colSpan,
				ColumnHeader: token.tag == "ched",
				RowHeader:    token.tag == "rhed",
				RowSection:   token.tag == "srow",
			})
		}
	}

	return OTSLResult{
		Sequence: sequence,
		Cells:    cells,
		NumRows:  numRows,
		NumCols:  numCols,
	}
}

func parseOTSLTokens(text string) []otslToken {
	tokens := make([]otslToken, 0)
	for pos := 0; pos < len(text); {
		open := strings.IndexByte(text[pos:], '<')
		if open < 0 {
			break
		}
		open += pos

		close := strings.IndexByte(text[open:], '>')
		if close < 0 {
			break
		}
		close += open

		tag := strings.TrimSpace(text[open+1 : close])
		pos = close + 1
		if tag == "" || strings.HasPrefix(tag, "/") {
			continue
		}

		if before, ok := strings.CutSuffix(tag, "/"); ok {
			tokens = append(tokens, otslToken{tag: strings.TrimSpace(before)})
			continue
		}

		nextOpen := strings.IndexByte(text[pos:], '<')
		if nextOpen < 0 {
			tokens = append(tokens, otslToken{tag: tag, text: text[pos:]})
			break
		}
		nextOpen += pos

		token := otslToken{tag: tag, text: text[pos:nextOpen]}
		closingTag := "</" + tag + ">"
		if strings.HasPrefix(text[nextOpen:], closingTag) {
			pos = nextOpen + len(closingTag)
		} else {
			pos = nextOpen
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func splitOTSLRows(tokens []otslToken) [][]otslToken {
	rows := make([][]otslToken, 0)
	current := make([]otslToken, 0)
	for _, token := range tokens {
		if token.tag == "nl" {
			if len(current) > 0 {
				rows = append(rows, current)
				current = make([]otslToken, 0)
			}
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}
	return rows
}

func isContentToken(tag string) bool {
	switch tag {
	case "fcel", "ecel", "ched", "rhed", "srow":
		return true
	default:
		return false
	}
}
