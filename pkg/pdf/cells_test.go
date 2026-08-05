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

// echoTexts returns a reextractAll that asserts the box count and returns the
// given texts.
func echoTexts(t *testing.T, want int, texts ...string) func([]geom.Box) []string {
	return func(boxes []geom.Box) []string {
		require.Len(t, boxes, want)
		return texts
	}
}

func TestMergeExclusiveEmpty(t *testing.T) {
	t.Parallel()
	called := false
	got := pdf.MergeFragmentedCellsExclusive(nil, func([]geom.Box) []string {
		called = true
		return nil
	}, pdf.MergeOptions{})
	require.Nil(t, got)
	require.False(t, called, "extractor must not run for an empty page")
}

func TestMergeExclusiveKeepsSeparateRows(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "one", 0, 0, 30, 10),
		mergeCell(1, "two", 0, 30, 30, 40),
		mergeCell(2, "three", 0, 60, 30, 70),
	}
	got := pdf.MergeFragmentedCellsExclusive(cells, echoTexts(t, 3, "one", "two", "three"), pdf.MergeOptions{})
	require.Equal(t, []string{"one", "two", "three"}, cellTexts(got))
	require.Equal(t, 0, got[0].Index)
	require.Equal(t, 2, got[2].Index)
}

func TestMergeExclusiveMergesAdjacentFragments(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCellWithFont(0, "He", 0, 0, 8, 10, 10),
		mergeCellWithFont(1, "llo", 9, 0, 20, 10, 14),
	}
	got := pdf.MergeFragmentedCellsExclusive(cells, echoTexts(t, 1, "Hello"), pdf.MergeOptions{})
	require.Len(t, got, 1)
	require.Equal(t, "Hello", got[0].Text)
	require.Equal(t, 14.0, got[0].FontSize)
	require.Equal(t, geom.Box{L: 0, T: 0, R: 20, B: 10, Origin: geom.TopLeft}, got[0].Box)
}

func TestMergeExclusiveSplitsOnLargeGap(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "He", 0, 0, 8, 10),
		mergeCell(1, "world", 50, 0, 70, 10),
	}
	got := pdf.MergeFragmentedCellsExclusive(cells, echoTexts(t, 2, "He", "world"), pdf.MergeOptions{})
	require.Len(t, got, 2)
}

func TestMergeExclusiveGroupsVerticalJitter(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "a", 0, 0, 8, 10),
		mergeCell(1, "b", 9, 2, 18, 11), // within 0.5*rowHeight vertical jitter, small gap
	}
	got := pdf.MergeFragmentedCellsExclusive(cells, echoTexts(t, 1, "a b"), pdf.MergeOptions{})
	require.Len(t, got, 1)
}
