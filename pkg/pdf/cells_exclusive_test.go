package pdf_test

// Tests for MergeFragmentedCellsExclusive — the batched variant of exclusive
// re-extraction. All merged groups' boxes are handed to the extractor in ONE
// call so it can partition the page's characters by best overlap, instead of
// each group greedily claiming whatever its box grazes (which let tall math
// delimiter rects steal glyphs from neighbouring prose lines).

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func TestMergeExclusiveBatchesAllGroupBoxes(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "He", 0, 0, 8, 10),
		mergeCell(1, "llo", 9, 0, 20, 10), // merges with "He" into one group
		mergeCell(2, "below", 0, 30, 30, 40),
	}

	var seen [][]geom.Box
	got := pdf.MergeFragmentedCellsExclusive(cells, func(boxes []geom.Box) []string {
		seen = append(seen, append([]geom.Box(nil), boxes...))
		return []string{"Hello", "below"}
	}, pdf.MergeOptions{})

	require.Len(t, seen, 1, "extractor must be called exactly once")
	require.Equal(t, []geom.Box{
		{L: 0, T: 0, R: 20, B: 10, Origin: geom.TopLeft},
		{L: 0, T: 30, R: 30, B: 40, Origin: geom.TopLeft},
	}, seen[0])
	require.Equal(t, []string{"Hello", "below"}, cellTexts(got))
	require.Equal(t, []int{0, 1}, []int{got[0].Index, got[1].Index})
}

// The extractor is authoritative: a group whose text comes back empty was
// fully claimed by better-overlapping groups and is dropped, not restored
// from member text (which would duplicate those glyphs).
func TestMergeExclusiveDropsGroupsAssignedNoText(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		mergeCell(0, "kept", 0, 0, 30, 10),
		mergeCell(1, "stolen", 0, 30, 30, 40),
	}

	got := pdf.MergeFragmentedCellsExclusive(cells, func(boxes []geom.Box) []string {
		require.Len(t, boxes, 2)
		return []string{"kept", ""}
	}, pdf.MergeOptions{})

	require.Equal(t, []string{"kept"}, cellTexts(got))
}
