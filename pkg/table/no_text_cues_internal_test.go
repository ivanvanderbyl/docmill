package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/textline"
	"github.com/stretchr/testify/require"
)

func TestPromotePrecedingWordGridHeaderLabelUsesGeometryNotLiteralText(t *testing.T) {
	t.Parallel()

	detected := DetectedTable{
		Data: Data{
			NumRows: 1,
			NumCols: 2,
			Cells: []Cell{
				{StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, Box: boxPtr(geom.Box{L: 0, T: 24, R: 45, B: 34, Origin: geom.TopLeft})},
				{Text: "Score", StartRow: 0, EndRow: 1, StartCol: 1, EndCol: 2, Box: boxPtr(geom.Box{L: 90, T: 24, R: 130, B: 34, Origin: geom.TopLeft})},
			},
		},
		Box: geom.Box{L: 0, T: 24, R: 130, B: 34, Origin: geom.TopLeft},
	}
	rows := []textline.ParagraphTextLine{
		{Cells: []page.TextCell{{Index: 1, Text: "Group", Box: geom.Box{L: 0, T: 0, R: 35, B: 10, Origin: geom.TopLeft}}}, Center: 5},
		{Cells: []page.TextCell{{Index: 2, Text: "Score", Box: geom.Box{L: 90, T: 24, R: 130, B: 34, Origin: geom.TopLeft}}}, Center: 29},
	}

	got := promotePrecedingWordGridHeaderLabel(detected, rows, 1)

	require.Equal(t, "Group", got.Data.Grid()[0][0].Text)
	require.Len(t, got.TextCells, 1)
	require.Equal(t, "Group", got.TextCells[0].Text)
}

func TestCompactWordGridCandidateScoreIgnoresLiteralHeaderAndBodyWords(t *testing.T) {
	t.Parallel()

	modelScore := compactWordGridCandidateScore(wordGridScoreData(
		[]string{"Model", "English", "Arabic", "Chinese"},
		[]string{"Vendor", "97", "98", "99"},
	))
	neutralScore := compactWordGridCandidateScore(wordGridScoreData(
		[]string{"Group", "Metric A", "Metric B", "Metric C"},
		[]string{"Sample", "97", "98", "99"},
	))

	require.Equal(t, neutralScore, modelScore)
}

func wordGridScoreData(header, body []string) DetectedTable {
	cells := make([]Cell, 0, len(header)+len(body))
	for col, text := range header {
		cells = append(cells, Cell{
			Text:     text,
			StartRow: 0,
			EndRow:   1,
			StartCol: col,
			EndCol:   col + 1,
		})
	}
	for col, text := range body {
		cells = append(cells, Cell{
			Text:     text,
			StartRow: 1,
			EndRow:   2,
			StartCol: col,
			EndCol:   col + 1,
		})
	}
	return DetectedTable{Data: Data{Cells: cells, NumRows: 2, NumCols: len(header)}}
}
