package text

// Tests for GetTextByRectsExclusive — the batched, best-overlap partition of
// the page's characters across query boxes.
//
// Mechanism this replaces (measured on real scientific PDFs): sequential
// first-query-wins claiming let a big math delimiter's rect — a narrow box
// 2-3x taller than its visual line, reaching down THROUGH the next prose
// line — steal individual prose glyphs that merely graze it (entropy.pdf p22:
// the big "(" claimed the 'e' of "possible" and the 'i' of "information").
// Best-overlap assignment keeps each character with the box that covers it
// best: a prose glyph is covered 100% by its own line's rect but only
// partially by a grazing delimiter, so it stays with its line.

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
)

// exChar builds a charInfo with a tight glyph box in PDF user space (y-up).
func exChar(r rune, l, b, rr, t float32) charInfo {
	return charInfo{
		charType: charNormal,
		unicode:  r,
		charCode: uint32(r),
		charBox:  box(l, b, rr, t),
	}
}

// Geometry from entropy.pdf p22 (y-up): equation band y 607.86-617.82, big
// paren rect x 292.68-297.24 spanning y 589.02-618.90, prose band y
// 586.25-595.25 with the 'e' of "possible" directly beneath the paren.
func TestExclusiveKeepsGrazedProseGlyphWithItsOwnLine(t *testing.T) {
	tp := &TextPage{charList: []charInfo{
		exChar('C', 255.5, 608.0, 262.0, 617.0),  // equation glyph
		exChar('(', 292.7, 589.02, 297.2, 618.9), // tall delimiter glyph
		exChar('a', 280.0, 586.9, 284.0, 595.0),  // prose, clear of the paren
		exChar('e', 292.9, 588.1, 296.5, 592.8),  // prose x-height glyph UNDER the paren
		exChar('b', 300.0, 586.9, 304.0, 595.0),  // prose, clear of the paren
	}}

	texts := tp.GetTextByRectsExclusive([]crt.FloatRect{
		box(255.48, 607.86, 292.75, 617.82), // equation row rect
		box(292.68, 589.02, 297.24, 618.90), // tall paren rect
		box(91.92, 586.25, 519.16, 595.25),  // prose line rect
	})

	if texts[0] != "C" {
		t.Errorf("equation rect text = %q, want %q", texts[0], "C")
	}
	if texts[1] != "(" {
		t.Errorf("delimiter rect stole prose glyphs: %q, want %q", texts[1], "(")
	}
	if texts[2] != "aeb" {
		t.Errorf("prose text = %q, want %q (grazed glyph must stay with its line)", texts[2], "aeb")
	}
}

// An ascender glyph (the 'i' of "variables", entropy.pdf p37) sits ENTIRELY
// inside both its own line's rect and a tall delimiter box reaching through
// the line: overlap fractions tie at 1.0 for both. The vertical-centre
// tie-break must keep it with its line — the line rect's centre is at the
// glyph, the delimiter's centre a row away.
func TestExclusiveFullyContainedGlyphStaysWithNearestCentreRect(t *testing.T) {
	tp := &TextPage{charList: []charInfo{
		exChar('(', 158.76, 99.90, 163.32, 129.78), // tall delimiter glyph
		exChar('v', 148.60, 106.5, 152.50, 111.4),  // prose
		exChar('i', 159.20, 107.2, 161.50, 114.9),  // prose ascender under the delimiter
		exChar('n', 164.20, 106.5, 168.20, 111.4),  // prose
	}}

	texts := tp.GetTextByRectsExclusive([]crt.FloatRect{
		box(158.76, 99.90, 163.32, 129.78),  // delimiter rect (fully contains 'i')
		box(116.88, 106.21, 192.65, 116.09), // prose line rect (also fully contains 'i')
	})

	if texts[0] != "(" {
		t.Errorf("delimiter rect stole a fully-contained prose glyph: %q, want %q", texts[0], "(")
	}
	if texts[1] != "vin" {
		t.Errorf("prose text = %q, want %q", texts[1], "vin")
	}
}

// The original claiming motivation still holds: a glyph whose box straddles
// two row rects is emitted exactly once, into the rect that covers it best.
func TestExclusiveEmitsStraddlingGlyphExactlyOnce(t *testing.T) {
	tp := &TextPage{charList: []charInfo{
		exChar('S', 100, 600, 106, 610), // straddles both rects
	}}

	texts := tp.GetTextByRectsExclusive([]crt.FloatRect{
		box(90, 605, 200, 618), // covers 5pt of the glyph (50%)
		box(90, 588, 200, 603), // covers 3pt of the glyph (30%)
	})

	if texts[0] != "S" {
		t.Errorf("best-overlap rect text = %q, want %q", texts[0], "S")
	}
	if texts[1] != "" {
		t.Errorf("worse rect must not duplicate the glyph: %q", texts[1])
	}
}

// Determinism on exact ties: a glyph fully contained in two rects goes to the
// earlier rect, and only that rect.
func TestExclusiveTieBreaksByRectOrder(t *testing.T) {
	tp := &TextPage{charList: []charInfo{
		exChar('T', 100, 600, 106, 610),
	}}

	texts := tp.GetTextByRectsExclusive([]crt.FloatRect{
		box(90, 595, 200, 615),
		box(95, 595, 150, 615),
	})

	if texts[0] != "T" || texts[1] != "" {
		t.Errorf("tie must resolve to the first rect once: %q / %q", texts[0], texts[1])
	}
}

// A glyph intersecting no rect is dropped, exactly as the non-exclusive
// readers drop it.
func TestExclusiveIgnoresUncoveredGlyphs(t *testing.T) {
	tp := &TextPage{charList: []charInfo{
		exChar('A', 100, 600, 106, 610),
		exChar('Z', 400, 300, 406, 310), // outside every rect
	}}

	texts := tp.GetTextByRectsExclusive([]crt.FloatRect{
		box(90, 595, 200, 615),
	})

	if texts[0] != "A" {
		t.Errorf("texts[0] = %q, want %q", texts[0], "A")
	}
}
