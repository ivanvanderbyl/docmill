package pdf

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

func frag(text string, l, t, r, b, fontSize float64) page.TextCell {
	return page.TextCell{
		Text:     text,
		FontSize: fontSize,
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

// A merged sub-word fragment run reports the size it OPENS at, not the largest
// size it contains. Positive cases: a run that absorbs an oversized math glyph
// must stay body-sized. Negative cases: a run whose own opening glyph is the
// largest (small caps, an isolated big operator) must keep that size, so the
// change is never larger than the previous maximum and never loses a genuinely
// prominent run.
func TestMergeCellShellFontSizeUsesLeadingFragment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		group []page.TextCell
		want  float64
	}{
		{
			// Positive: an oversized summation absorbed mid-run. Under a
			// maximum the whole merged cell claimed 14.4pt for 12 body-sized
			// characters.
			name: "absorbed oversized math glyph does not raise the run",
			group: []page.TextCell{
				frag("−", 0, 0, 6, 10, 10),
				frag("∑", 6, -6, 20, 16, 14.4),
				frag("pi log pi.", 20, 0, 70, 10, 10),
			},
			want: 10,
		},
		{
			// Negative: small caps. The capitals carry the nominal size and the
			// small capitals — the MAJORITY of the characters — are set ~0.8x.
			// The run must still measure at its nominal size, which is why the
			// leading fragment rather than the majority decides.
			name: "small caps keep their nominal size",
			group: []page.TextCell{
				frag("1. T", 0, 0, 20, 9, 10),
				frag("HE", 20, 0, 31, 7.2, 8),
				frag("D", 31, 0, 38, 9, 10),
				frag("ISCRETE", 38, 0, 73, 7.2, 8),
			},
			want: 10,
		},
		{
			// Negative: a run that genuinely opens large keeps its size, so the
			// metric never falls below what the maximum reported for a
			// prominent run.
			name: "run opening at its largest size is unchanged",
			group: []page.TextCell{
				frag("Chapter", 0, 0, 60, 18, 18),
				frag("3", 62, 4, 68, 14, 10),
			},
			want: 18,
		},
		{
			// A fragment declaring a fraction of a point inside a full-height
			// box has an unusable size (matrix-scaled or unresolved font) and
			// must not be taken as the run's size.
			name: "incoherent declared size is skipped",
			group: []page.TextCell{
				frag(")=", 0, 0, 14, 9.96, 0.12),
				frag("N", 14, 0, 21, 9, 10),
			},
			want: 10,
		},
		{
			// No credible fragment at all: fall back to the largest declared
			// size, which is what the merge reported before.
			name: "all sizes incoherent falls back to the maximum",
			group: []page.TextCell{
				frag("a", 0, 0, 6, 20, 0.12),
				frag("b", 6, 0, 12, 20, 0.5),
			},
			want: 0.5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tc.want, mergeCellShell(tc.group).FontSize, 0.001)
			require.InDelta(t, tc.want, leadingFragmentFontSize(tc.group), 0.001)
		})
	}
}

func TestCredibleCellFontSizeIsOneSided(t *testing.T) {
	t.Parallel()
	// Credible: box height in the normal range for its type size.
	require.True(t, credibleCellFontSize(frag("x", 0, 0, 6, 9, 10)))
	// Credible: a short glyph in a large font (a full stop) — the test must
	// never reject a box that is SMALLER than its declared size.
	require.True(t, credibleCellFontSize(frag(".", 0, 0, 3, 2, 18)))
	// Credible: a tall stretched delimiter still within the ratio.
	require.True(t, credibleCellFontSize(frag("(", 0, 0, 4, 22, 10)))
	// Not credible: a fraction-of-a-point size inside a body-height box.
	require.False(t, credibleCellFontSize(frag("(t", 0, 0, 4, 9.96, 0.12)))
	// Not credible: no declared size at all.
	require.False(t, credibleCellFontSize(frag("x", 0, 0, 6, 9, 0)))
}
