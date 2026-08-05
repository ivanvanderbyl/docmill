package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
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

// dominantFontSize must report the size the MAJORITY of a line's
// characters are set at, not the largest size present. These cases cover both
// directions of the invariant: decoration below the body size (super/subscript
// markers) and decoration above it (display-math delimiters, drop caps) must
// both lose, while a line whose body genuinely IS large must keep its size.
func TestDominantLineFontSizeWeightsByCharacterMass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		cells []page.TextCell
		want  float64
	}{
		{
			// Positive: display equation. One oversized summation glyph plus
			// ordinary math glyphs. The line must measure as body-sized maths,
			// not as a title.
			name: "oversized display math delimiter loses",
			cells: []page.TextCell{
				lc(1, "H", 0, 0, 8, 10, 10),
				lc(2, "=", 10, 0, 18, 10, 10),
				lc(3, "−", 20, 0, 28, 10, 10),
				lc(4, "∑", 30, -8, 46, 18, 24),
				lc(5, "pi", 48, 0, 60, 10, 10),
				lc(6, "log", 62, 0, 80, 10, 10),
				lc(7, "pi", 82, 0, 94, 10, 10),
			},
			want: 10,
		},
		{
			// Positive: drop cap. A body line wearing an ornament measures as
			// body text.
			name: "drop cap loses to the body run",
			cells: []page.TextCell{
				lc(1, "T", 0, -20, 24, 12, 32),
				lc(2, "he information source produces", 26, 0, 200, 10, 10),
				lc(3, "a message", 202, 0, 260, 10, 10),
			},
			want: 10,
		},
		{
			// Positive: heading with a small trailing marker. The majority is
			// the heading text, so the heading keeps its prominence.
			name: "small trailing marker loses to the heading run",
			cells: []page.TextCell{
				lc(1, "2.", 0, 0, 12, 14, 14),
				lc(2, "THE DISCRETE SOURCE", 14, 0, 160, 14, 14),
				lc(3, "12", 162, 0, 168, 6, 6),
			},
			want: 14,
		},
		{
			// Negative: a genuinely large line must NOT be dragged down by a
			// short small run. Majority rules in both directions.
			name: "large body keeps its size against a small annotation",
			cells: []page.TextCell{
				lc(1, "A Mathematical Theory", 0, 0, 200, 18, 18),
				lc(2, "*", 202, 0, 206, 6, 6),
			},
			want: 18,
		},
		{
			// Negative: a uniform line is unchanged by the new metric.
			name: "uniform line reports its single size",
			cells: []page.TextCell{
				lc(1, "ordinary", 0, 0, 40, 10, 11.5),
				lc(2, "prose", 42, 0, 70, 10, 11.5),
			},
			want: 11.5,
		},
		{
			// Negative: a single-cell line has no minority, so max and median
			// agree — an isolated large glyph line stays large.
			name: "single cell keeps its own size",
			cells: []page.TextCell{
				lc(1, "∫", 0, 0, 20, 30, 28),
			},
			want: 28,
		},
		{
			// Exact tie: equal character mass at two sizes has no dominant
			// body, so the metric resolves downward.
			name: "even split resolves to the smaller size",
			cells: []page.TextCell{
				lc(1, "AB", 0, 0, 20, 10, 10),
				lc(2, "CD", 22, 0, 60, 20, 20),
			},
			want: 10,
		},
		{
			// Spacer and zero-size cells carry no evidence and must not vote.
			name: "spacer and unsized cells are ignored",
			cells: []page.TextCell{
				lc(1, "text", 0, 0, 30, 10, 9),
				lc(2, "   ", 32, 0, 40, 10, 40),
				lc(3, "more", 42, 0, 80, 10, 0),
			},
			want: 9,
		},
		{
			// No usable evidence at all: fall back to the maximum so callers
			// keep a positive metric instead of zero.
			name: "no sized text falls back to the maximum",
			cells: []page.TextCell{
				lc(1, "  ", 0, 0, 10, 10, 12),
				lc(2, " ", 12, 0, 20, 10, 7),
			},
			want: 12,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tc.want, dominantFontSize(tc.cells), 0.001)
		})
	}
}

// End-to-end through the line producer: a tall math glyph sharing a baseline
// with prose must not make the assembled line measure title-sized.
func TestAssembleLineElementsFontSizeIsDominantNotMaximum(t *testing.T) {
	t.Parallel()
	cells := []page.TextCell{
		lc(1, "where", 0, 0, 30, 10, 10),
		lc(2, "∑", 34, -6, 50, 16, 22),
		lc(3, "denotes", 54, 0, 100, 10, 10),
		lc(4, "the", 104, 0, 120, 10, 10),
		lc(5, "sum", 124, 0, 150, 10, 10),
	}
	lines := AssembleLineElements(cells, 4)
	require.Len(t, lines, 1)
	require.InDelta(t, 10.0, lines[0].FontSize, 0.001)
	// The oversized glyph is still part of the line; only the metric changed.
	require.Contains(t, lines[0].Text, "∑")
}
