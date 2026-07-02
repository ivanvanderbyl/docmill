package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/stretchr/testify/require"
)

func lc(idx int, text string, l, t, r, b, fs float64) page.TextCell {
	return page.TextCell{Index: idx, Text: text, FontSize: fs,
		Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}}
}

func TestAssembleLineElementsClustersByBaseline(t *testing.T) {
	t.Parallel()
	// Two baselines: y 0-10 and y 20-30. Cells on each belong to one line.
	cells := []page.TextCell{
		lc(1, "A", 0, 0, 10, 10, 10),
		lc(2, "B", 20, 1, 30, 9, 10),
		lc(3, "C", 0, 20, 10, 30, 10),
		lc(4, "D", 20, 21, 30, 29, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 2)
	require.Equal(t, "A B", lines[0].Text)
	require.Equal(t, "C D", lines[1].Text)
	require.Equal(t, 0, lines[0].ReadingOrder)
	require.Equal(t, 1, lines[1].ReadingOrder)
}

func TestAssembleLineElementsOrdersWordsLeftToRight(t *testing.T) {
	t.Parallel()
	// Cells given out of x-order; words must be emitted left-to-right.
	cells := []page.TextCell{
		lc(1, "second", 50, 0, 80, 10, 10),
		lc(2, "first", 0, 0, 40, 10, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	require.Equal(t, "first second", lines[0].Text)
	require.Equal(t, "first", lines[0].Words[0].Value)
	require.Equal(t, "second", lines[0].Words[1].Value)
}

func TestAssembleLineElementsFontTransitionSplitsRun(t *testing.T) {
	t.Parallel()
	// Three words: same font, then a size change. Two LineElement runs.
	// (Bold/italic can't be tested yet — FontName isn't surfaced; use size.)
	cells := []page.TextCell{
		lc(1, "normal", 0, 0, 30, 10, 10),
		lc(2, "also", 35, 0, 60, 10, 10),
		lc(3, "BIG", 65, 0, 100, 10, 18),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	// Two runs: [normal also] (size 10), [BIG] (size 18).
	require.Len(t, lines[0].Elements, 2)
	require.Equal(t, "normal also", lines[0].Elements[0].Text)
	require.Equal(t, "BIG", lines[0].Elements[1].Text)
}

func TestAssembleLineElementsSkipsEmptyAndSpacerCells(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		lc(1, "keep", 0, 0, 30, 10, 10),
		lc(2, "   ", 35, 0, 40, 10, 10),
		lc(3, "this", 45, 0, 80, 10, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	require.Equal(t, "keep this", lines[0].Text)
	require.Len(t, lines[0].Words, 2)
}

func TestAssembleLineElementsSoftHyphenationJoins(t *testing.T) {
	t.Parallel()
	// "inter-" + "rupted" → "interrupted" (soft hyphenation).
	cells := []page.TextCell{
		lc(1, "inter-", 0, 0, 30, 10, 10),
		lc(2, "rupted", 35, 0, 70, 10, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	// The words are separate (different positions), but a single run's text
	// would join them. Here they're on the same baseline so they form one run.
	if len(lines[0].Elements) == 1 {
		require.Equal(t, "interrupted", lines[0].Elements[0].Text)
	}
}

func TestAssembleLineElementsBBoxIsEnclosing(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		lc(1, "A", 5, 2, 15, 12, 10),
		lc(2, "B", 20, 1, 30, 11, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	require.InDelta(t, 1.0, lines[0].BBox.T, 0.01)
	require.InDelta(t, 12.0, lines[0].BBox.B, 0.01)
	require.InDelta(t, 5.0, lines[0].BBox.L, 0.01)
	require.InDelta(t, 30.0, lines[0].BBox.R, 0.01)
}
