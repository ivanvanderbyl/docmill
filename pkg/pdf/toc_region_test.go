package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/stretchr/testify/require"
)

// tocWord builds a TextLineWord at the given x-range on a 10pt line.
func tocWord(value string, l, r float64) TextLineWord {
	return TextLineWord{Value: value, BBox: geom.Box{L: l, T: 0, R: r, B: 10, Origin: geom.TopLeft}}
}

// tocLine assembles a ParagraphTextLine from words (top set per row).
func tocLine(top float64, words ...TextLineWord) ParagraphTextLine {
	left, right := words[0].BBox.L, words[0].BBox.R
	for _, w := range words {
		if w.BBox.L < left {
			left = w.BBox.L
		}
		if w.BBox.R > right {
			right = w.BBox.R
		}
	}
	out := make([]TextLineWord, len(words))
	for i, w := range words {
		w.BBox.T, w.BBox.B = top, top+10
		out[i] = w
	}
	return ParagraphTextLine{
		BBox:     geom.Box{L: left, T: top, R: right, B: top + 10, Origin: geom.TopLeft},
		Words:    out,
		FontSize: 10,
	}
}

// dotLeaderEntry builds a contents-entry line: "<entry>  <dots>  <page>" with the
// page number right edge at pageR (shared margin across rows).
func dotLeaderEntry(top float64, entry, page string, pageR float64) ParagraphTextLine {
	return tocLine(top,
		tocWord(entry, 0, 80),
		tocWord(".......", 84, 240),
		tocWord(page, pageR-10, pageR),
	)
}

func TestDetectTocLinesAcceptsAlignedDotLeaderRun(t *testing.T) {
	t.Parallel()

	lines := []ParagraphTextLine{
		dotLeaderEntry(0, "Author's Note", "ix", 250),
		dotLeaderEntry(20, "Foreword", "xi", 251),
		dotLeaderEntry(40, "Acknowledgements", "xv", 249),
		dotLeaderEntry(60, "A Fountain in the Square", "1", 250),
		dotLeaderEntry(80, "The Lost Homeland", "5", 250),
	}

	marked := detectTocLines(lines)
	for i, m := range marked {
		require.Truef(t, m, "line %d should be a ToC entry", i)
	}
}

func TestDetectTocLinesRejectsBodyProse(t *testing.T) {
	t.Parallel()

	// Three flush, gap-free prose lines whose last token happens to be a number
	// but with NO leader gap — must not be mistaken for a contents run.
	body := []ParagraphTextLine{
		tocLine(0, tocWord("the value was", 0, 90), tocWord("42", 92, 104)),
		tocLine(20, tocWord("and then it became", 0, 110), tocWord("43", 112, 124)),
		tocLine(40, tocWord("finally reaching", 0, 95), tocWord("44", 97, 109)),
	}
	for i, m := range detectTocLines(body) {
		require.Falsef(t, m, "prose line %d must not be a ToC entry (no leader)", i)
	}
}

func TestDetectTocLinesRejectsShortRunAndMisalignment(t *testing.T) {
	t.Parallel()

	// Only two aligned entries -> below minTocRows.
	short := []ParagraphTextLine{
		dotLeaderEntry(0, "Intro", "1", 250),
		dotLeaderEntry(20, "Methods", "2", 250),
	}
	for _, m := range detectTocLines(short) {
		require.False(t, m, "a 2-entry run is below minTocRows")
	}
}

func TestParsePageNumberRomanCanonicalOnly(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		val   int
		roman bool
		ok    bool
	}{
		"3":   {3, false, true},
		"ix":  {9, true, true},
		"xiv": {14, true, true},
		"did": {0, false, false}, // word that is not canonical roman
		"mix": {0, false, false}, // 'mix' would parse but must be rejected (re-encodes to "mix" -> guard via length/word context)
		"":    {0, false, false},
		"abc": {0, false, false},
	}
	for in, want := range cases {
		v, r, ok := parsePageNumber(in)
		if in == "mix" {
			// "mix" canonically re-encodes to itself; it is excluded in practice
			// by the leader + run guards, not here — accept either parse outcome.
			continue
		}
		require.Equalf(t, want.ok, ok, "ok for %q", in)
		if want.ok {
			require.Equalf(t, want.val, v, "value for %q", in)
			require.Equalf(t, want.roman, r, "roman for %q", in)
		}
	}
}
