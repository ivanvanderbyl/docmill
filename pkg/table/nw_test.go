package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
	"github.com/stretchr/testify/require"
)

// nwCell builds a TextCell with a TopLeft box, matching the table detection
// fixtures used elsewhere (T < B, y increases downward).
func nwCell(index int, l, t, r, b float64) page.TextCell {
	return page.TextCell{
		Index: index,
		Box:   geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

// nwCellText is nwCell with literal text, for asserting that a continuation
// fragment folds into the correct column (text + box), not merely that the
// segmentation is correct.
func nwCellText(index int, l, t, r, b float64, text string) page.TextCell {
	cell := nwCell(index, l, t, r, b)
	cell.Text = text
	return cell
}

// nwRow groups cells into a single textline.ParagraphTextLine with the average vertical centre.
func nwRow(cells ...page.TextCell) textline.ParagraphTextLine {
	return textline.ParagraphTextLine{Cells: cells, Center: averageRowCenter(cells)}
}

// containsAll reports whether every element of sub appears in super.
func containsAll(super, sub []int) bool {
	set := make(map[int]struct{}, len(super))
	for _, v := range super {
		set[v] = struct{}{}
	}
	for _, v := range sub {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

// logicalRowIndexes returns, per logical row, the sorted source cell indexes,
// so segmentation can be asserted without depending on literal text.
func logicalRowIndexes(rows []logicalTableRow) [][]int {
	out := make([][]int, 0, len(rows))
	for _, row := range rows {
		idxs := make([]int, 0, len(row.Source))
		for _, cell := range row.Source {
			idxs = append(idxs, cell.Index)
		}
		out = append(out, idxs)
	}
	return out
}

func defaultNWOptions() DetectionOptions {
	return normaliseDetectionOptions(DetectionOptions{})
}

// (a) A clean lone-first-column wrap segments identically to the greedy merger.
func TestMergeRowsNWMatchesGreedyOnFirstColumnWrap(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		// Header row: 3 columns.
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10), nwCell(3, 200, 0, 240, 10)),
		// Data row with a first-column cell.
		nwRow(nwCell(4, 0, 20, 60, 30), nwCell(5, 100, 20, 140, 30), nwCell(6, 200, 20, 240, 30)),
		// Lone first-column wrap of the data row (single cell aligned to col 0).
		nwRow(nwCell(7, 0, 32, 50, 42)),
		// Next data row.
		nwRow(nwCell(8, 0, 54, 60, 64), nwCell(9, 100, 54, 140, 64), nwCell(10, 200, 54, 240, 64)),
	}

	greedy := logicalRowIndexes(mergeFirstColumnContinuationRows(rows, options))
	nw := logicalRowIndexes(mergeRowsNW(rows, options))

	require.Equal(t, greedy, nw, "NW must match greedy on a lone first-column wrap")
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10}}, nw)
}

// No-continuation table: every row is well-formed, NW must not merge anything.
func TestMergeRowsNWMatchesGreedyWithNoContinuation(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10), nwCell(3, 200, 0, 240, 10)),
		nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 140, 30), nwCell(6, 200, 20, 240, 30)),
		nwRow(nwCell(7, 0, 40, 40, 50), nwCell(8, 100, 40, 140, 50), nwCell(9, 200, 40, 240, 50)),
	}

	greedy := logicalRowIndexes(mergeFirstColumnContinuationRows(rows, options))
	nw := logicalRowIndexes(mergeRowsNW(rows, options))

	require.Equal(t, greedy, nw)
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}, nw)
}

// (b) A multi-line cell in a NON-first column merges into one logical row,
// whereas the greedy merger leaves the wrap as a separate row.
func TestMergeRowsNWAbsorbsNonFirstColumnWrap(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		// 3-col header.
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 160, 10), nwCell(3, 220, 0, 260, 10)),
		// Data row: col1 key, col2 first line of a 2-line answer, col3 value.
		nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 175, 30), nwCell(6, 220, 20, 260, 30)),
		// Wrap of column 2 only (no first-column cell, lands under col2).
		nwRow(nwCell(7, 100, 32, 168, 42)),
		// Next data row.
		nwRow(nwCell(8, 0, 54, 40, 64), nwCell(9, 100, 54, 160, 64), nwCell(10, 220, 54, 260, 64)),
	}

	greedy := logicalRowIndexes(mergeFirstColumnContinuationRows(rows, options))
	nw := logicalRowIndexes(mergeRowsNW(rows, options))

	// Greedy cannot absorb a non-first-column wrap, so it keeps row 7 separate.
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6}, {7}, {8, 9, 10}}, greedy)
	// NW absorbs the wrap into the data row above.
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10}}, nw)
}

// (c) A genuine new row with a blank first column but its own anchor-band cell
// is NOT merged (boundary preserved).
func TestMergeRowsNWKeepsBlankFirstColumnDataRow(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		// 3-col header establishing the anchor band at L=0.
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10), nwCell(3, 200, 0, 240, 10)),
		// Data row with a first-column cell.
		nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 140, 30), nwCell(6, 200, 20, 240, 30)),
		// New data row whose first column is blank but which still owns an
		// anchor-band cell (col0 fragment) plus other-column fragments: a real
		// row, must stay separate.
		nwRow(nwCell(7, 0, 40, 40, 50), nwCell(8, 100, 40, 140, 50), nwCell(9, 200, 40, 240, 50)),
	}

	nw := logicalRowIndexes(mergeRowsNW(rows, options))
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}, nw)

	// A row that spans two distinct non-first columns (blank first column, no
	// anchor-band cell) is also a structural row, not a wrap: keep it separate.
	rowsMultiCol := []textline.ParagraphTextLine{
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10), nwCell(3, 200, 0, 240, 10)),
		nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 140, 30), nwCell(6, 200, 20, 240, 30)),
		// No col0 cell, but two fragments under col1 and col2: structural row.
		nwRow(nwCell(7, 100, 32, 140, 42), nwCell(8, 200, 32, 240, 42)),
	}
	nwMulti := logicalRowIndexes(mergeRowsNW(rowsMultiCol, options))
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6}, {7, 8}}, nwMulti)
}

// (d) Irregular leading within one multi-line cell still merges. The wrapped
// continuation lines in column 2 have uneven vertical gaps; the adaptive
// (run-median) leading keeps them in one logical row.
func TestMergeRowsNWAbsorbsIrregularLeadingWrap(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 170, 10), nwCell(3, 220, 0, 260, 10)),
		// Data row; column 2 is the first of a 3-line answer.
		nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 175, 30), nwCell(6, 220, 20, 260, 30)),
		// Second line of column 2 (gap ~11).
		nwRow(nwCell(7, 100, 33, 168, 41)),
		// Third line of column 2 with a slightly larger, irregular gap (~14).
		nwRow(nwCell(8, 100, 49, 160, 57)),
		// Next data row, clearly separated.
		nwRow(nwCell(9, 0, 80, 40, 90), nwCell(10, 100, 80, 170, 90), nwCell(11, 220, 80, 260, 90)),
	}

	nw := logicalRowIndexes(mergeRowsNW(rows, options))
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6, 7, 8}, {9, 10, 11}}, nw)
}

// A non-first-column wrap must fold into ITS OWN column (text + geometry), not
// be mis-folded into the left-most column. Guards the mergeContinuationRow
// routing so the wrap's content lands in the right cell and the first column's
// box is not stretched to cover it.
func TestMergeRowsNWFoldsNonFirstColumnWrapIntoOwnColumn(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()
	rows := []textline.ParagraphTextLine{
		nwRow(nwCellText(1, 0, 0, 40, 10, "h0"), nwCellText(2, 100, 0, 160, 10, "h1"), nwCellText(3, 220, 0, 260, 10, "h2")),
		// Data row: col0 key 'A', col1 first line 'line1', col2 value 'V'.
		nwRow(nwCellText(4, 0, 20, 40, 30, "A"), nwCellText(5, 100, 20, 175, 30, "line1"), nwCellText(6, 220, 20, 260, 30, "V")),
		// Single-cell wrap of column 1 only ('line2', lands under col1).
		nwRow(nwCellText(7, 100, 32, 168, 42, "line2")),
		nwRow(nwCellText(8, 0, 54, 40, 64, "B"), nwCellText(9, 100, 54, 160, 64, "k1"), nwCellText(10, 220, 54, 260, 64, "k2")),
	}

	merged := mergeRowsNW(rows, options)
	require.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10}}, logicalRowIndexes(merged))

	// The merged data row is index 1. Locate its cells by column.
	dataRow := merged[1].Row
	require.Len(t, dataRow.Cells, 3, "wrap must fold into an existing column, not add one")

	col0, col1, col2 := dataRow.Cells[0], dataRow.Cells[1], dataRow.Cells[2]

	// The wrap text lands in column 1 (its own column), not column 0.
	require.Equal(t, "A", col0.Text, "first column must NOT absorb the non-first-column wrap")
	require.Equal(t, "line1 line2", col1.Text, "wrap text must fold into its own column")
	require.Equal(t, "V", col2.Text)

	// The first column's box must NOT be stretched to cover the wrap (R stays at
	// the original col0 R=40, not reaching the wrap's R=168).
	require.Equal(t, 40.0, col0.Box.R, "first column box must not stretch to the wrap")
	// The wrap extends column 1's box rather than column 0's.
	require.GreaterOrEqual(t, col1.Box.R, 168.0, "wrap must extend its own column's box")
}

// Conservatism invariant (spec requirement #3): NW must only ever produce a
// COARSER partition than the greedy merger — every greedy logical-row group must
// be contained within a single NW logical row. NW may absorb MORE rows but must
// never split a group greedy merged. This locks the sound DP (no unsound
// pruning that under-merges below the greedy baseline).
func TestMergeRowsNWNeverSplitsGreedyGroups(t *testing.T) {
	t.Parallel()

	options := defaultNWOptions()

	fixtures := [][]textline.ParagraphTextLine{
		// Lone first-column wrap chain (multiple consecutive wraps under one anchor).
		{
			nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10)),
			nwRow(nwCell(3, 0, 20, 60, 30), nwCell(4, 100, 20, 140, 30)),
			nwRow(nwCell(5, 0, 32, 50, 42)),
			nwRow(nwCell(6, 0, 44, 50, 54)),
			nwRow(nwCell(7, 0, 70, 60, 80), nwCell(8, 100, 70, 140, 80)),
		},
		// Non-first-column wrap interleaved with a first-column wrap.
		{
			nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 170, 10), nwCell(3, 220, 0, 260, 10)),
			nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 175, 30), nwCell(6, 220, 20, 260, 30)),
			nwRow(nwCell(7, 0, 32, 50, 42)),
			nwRow(nwCell(8, 100, 44, 168, 54)),
			nwRow(nwCell(9, 0, 70, 40, 80), nwCell(10, 100, 70, 170, 80), nwCell(11, 220, 70, 260, 80)),
		},
		// No-continuation table: NW must equal greedy exactly.
		{
			nwRow(nwCell(1, 0, 0, 40, 10), nwCell(2, 100, 0, 140, 10), nwCell(3, 200, 0, 240, 10)),
			nwRow(nwCell(4, 0, 20, 40, 30), nwCell(5, 100, 20, 140, 30), nwCell(6, 200, 20, 240, 30)),
			nwRow(nwCell(7, 40, 40, 40, 50), nwCell(8, 100, 40, 140, 50), nwCell(9, 200, 40, 240, 50)),
		},
	}

	for fi, rows := range fixtures {
		greedy := logicalRowIndexes(mergeFirstColumnContinuationRows(rows, options))
		nw := logicalRowIndexes(mergeRowsNW(rows, options))
		for _, group := range greedy {
			found := false
			for _, nwGroup := range nw {
				if containsAll(nwGroup, group) {
					found = true
					break
				}
			}
			require.Truef(t, found,
				"fixture %d: greedy group %v is split across NW rows %v (NW must never under-merge below greedy)",
				fi, group, nw)
		}
	}
}
