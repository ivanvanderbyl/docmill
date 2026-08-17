package pdf_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func mergeCell(index int, text string, l, t, r, b float64) page.TextCell {
	return mergeCellWithFont(index, text, l, t, r, b, 0)
}

func mergeCellWithFont(index int, text string, l, t, r, b, fontSize float64) page.TextCell {
	return page.TextCell{
		Index:    index,
		Text:     text,
		FontSize: fontSize,
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

func cellTexts(cells []page.TextCell) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = c.Text
	}
	return out
}

func TestMergeFragmentedCellsEmpty(t *testing.T) {
	t.Parallel()
	require.Nil(t, pdf.MergeFragmentedCells(nil, nil, pdf.MergeOptions{}))
}

func TestMergeFragmentedCellsKeepsSeparateRows(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "one", 0, 0, 30, 10),
		mergeCell(1, "two", 0, 30, 30, 40),
		mergeCell(2, "three", 0, 60, 30, 70),
	}
	got := pdf.MergeFragmentedCells(cells, nil, pdf.MergeOptions{})
	require.Equal(t, []string{"one", "two", "three"}, cellTexts(got))
	require.Equal(t, 0, got[0].Index)
	require.Equal(t, 2, got[2].Index)
}

func TestMergeFragmentedCellsMergesAdjacentFragments(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCellWithFont(0, "He", 0, 0, 8, 10, 10),
		mergeCellWithFont(1, "llo", 9, 0, 20, 10, 14),
	}
	got := pdf.MergeFragmentedCells(cells, nil, pdf.MergeOptions{})
	require.Len(t, got, 1)
	require.Equal(t, "He llo", got[0].Text)
	require.Equal(t, 14.0, got[0].FontSize)
	require.Equal(t, geom.Box{L: 0, T: 0, R: 20, B: 10, Origin: geom.TopLeft}, got[0].Box)
}

func TestMergeFragmentedCellsSplitsOnLargeGap(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "He", 0, 0, 8, 10),
		mergeCell(1, "world", 50, 0, 70, 10),
	}
	got := pdf.MergeFragmentedCells(cells, nil, pdf.MergeOptions{})
	require.Len(t, got, 2)
}

func TestMergeFragmentedCellsUsesReextractForMergedText(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "He", 0, 0, 8, 10),
		mergeCell(1, "llo", 9, 0, 20, 10),
	}
	var seen [][]geom.Box
	got := pdf.MergeFragmentedCells(cells, func(boxes []geom.Box) []string {
		seen = append(seen, boxes)
		return []string{"Hello"}
	}, pdf.MergeOptions{})
	require.Len(t, got, 1)
	require.Equal(t, "Hello", got[0].Text)
	require.Equal(t, [][]geom.Box{{{L: 0, T: 0, R: 20, B: 10, Origin: geom.TopLeft}}}, seen)
}

func TestMergeFragmentedCellsBatchesReextractAcrossGroups(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		// Row 1: two fragments that merge into one cell.
		mergeCell(0, "He", 0, 0, 8, 10),
		mergeCell(1, "llo", 9, 0, 20, 10),
		// Row 2: a single cell — must not be re-extracted.
		mergeCell(2, "world", 0, 30, 20, 40),
		// Row 3: two fragments whose re-extract comes back empty.
		mergeCell(3, "fa", 0, 60, 8, 70),
		mergeCell(4, "ll", 9, 60, 20, 70),
	}
	calls := 0
	got := pdf.MergeFragmentedCells(cells, func(boxes []geom.Box) []string {
		calls++
		require.Len(t, boxes, 2)
		return []string{"Hello", ""}
	}, pdf.MergeOptions{})
	require.Equal(t, 1, calls, "reextract must be called exactly once for all groups")
	require.Len(t, got, 3)
	require.Equal(t, "Hello", got[0].Text)
	require.Equal(t, "world", got[1].Text)
	require.Equal(t, "fa ll", got[2].Text, "empty reextract falls back to joined member texts")
}

func TestMergeFragmentedCellsGroupsVerticalJitter(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "a", 0, 0, 8, 10),
		mergeCell(1, "b", 9, 2, 18, 11), // within 0.5*rowHeight vertical jitter, small gap
	}
	got := pdf.MergeFragmentedCells(cells, nil, pdf.MergeOptions{})
	require.Len(t, got, 1)
}
