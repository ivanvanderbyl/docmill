// White-box unit tests for the pure CPDF_TextPage helpers and the char-index /
// rect-segmentation logic, plus the behavioural invariants from the research
// spec's test-vector section (no upstream cpdf_textpage_unittest.cpp exists at
// commit 0db284a42; these encode the documented invariants).
package text

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/page"
)

func box(l, b, r, t float32) crt.FloatRect { return crt.NewFloatRect(l, b, r, t) }

// --- NormalizeThreshold buckets (both call-site bucket sets) ---

func TestNormalizeThreshold(t *testing.T) {
	cases := []struct {
		v          float32
		t1, t2, t3 int
		want       float32
	}{
		{200, 300, 500, 700, 100}, // < t1 -> /2
		{400, 300, 500, 700, 100}, // < t2 -> /4
		{600, 300, 500, 700, 120}, // < t3 -> /5
		{800, 300, 500, 700, 800.0 / 6},
		{350, 400, 700, 800, 175}, // ProcessInsertObject buckets, < t1 -> /2
		{750, 400, 700, 800, 150}, // < t3 -> /5
	}
	for _, c := range cases {
		if got := normalizeThreshold(c.v, c.t1, c.t2, c.t3); got != c.want {
			t.Errorf("normalizeThreshold(%v,%d,%d,%d)=%v want %v", c.v, c.t1, c.t2, c.t3, got, c.want)
		}
	}
}

// --- GenerateSpace ---

func TestGenerateSpace(t *testing.T) {
	// Gap exactly at threshold -> no space.
	if generateSpace(crt.PointF{X: 10}, 0, 5, 5, 5) {
		t.Error("expected no space when |last_pos+last_width-pos.x| <= threshold")
	}
	// Large positive gap -> space.
	if !generateSpace(crt.PointF{X: 100}, 0, 5, 5, 5) {
		t.Error("expected space for a large gap")
	}
}

// --- IsHyphenCode / IsControlChar / IsNormalCharacter ---

func TestCharClassifiers(t *testing.T) {
	if !isHyphenCode(0x2D) || !isHyphenCode(0xAD) || isHyphenCode('a') {
		t.Error("isHyphenCode")
	}
	// 0x2 is a control char unless the char is a kHyphen.
	if !isControlChar(charInfo{unicode: 0x2}) {
		t.Error("0x2 should be control")
	}
	if isControlChar(charInfo{unicode: 0x2, charType: charHyphen}) {
		t.Error("0x2 with kHyphen type should NOT be control (joins across line breaks)")
	}
	// kHyphen char stays normal (in the stream).
	if !isNormalCharacter(charInfo{unicode: 0x2, charType: charHyphen}) {
		t.Error("kHyphen char should be a normal character")
	}
	// unicode 0 but non-zero char code is normal.
	if !isNormalCharacter(charInfo{unicode: 0, charCode: 5}) {
		t.Error("unicode 0 with char code != 0 should be normal")
	}
	if isNormalCharacter(charInfo{unicode: 0, charCode: 0}) {
		t.Error("all-zero should not be normal")
	}
}

// --- Unicode normalization (ligatures) ---

func TestUnicodeNormalizationLigature(t *testing.T) {
	// U+FB01 (fi ligature) decomposes to "fi".
	got := getUnicodeNormalization(0xFB01)
	if string(got) != "fi" {
		t.Errorf("normalize(U+FB01)=%q want \"fi\"", string(got))
	}
	// An unmapped char returns itself.
	if g := getUnicodeNormalization('A'); len(g) != 1 || g[0] != 'A' {
		t.Errorf("normalize('A')=%v want ['A']", g)
	}
}

// --- BiDi classification + segmentation ---

func TestBidiOverallDirection(t *testing.T) {
	// Latin -> left.
	if newBidiString([]rune("hello")).overallDir() != dirLeft {
		t.Error("latin should be LTR")
	}
	// Hebrew -> right.
	if newBidiString([]rune("אבג")).overallDir() != dirRight {
		t.Error("hebrew should be RTL")
	}
}

func TestMirrorChar(t *testing.T) {
	if getMirrorChar('(') != ')' || getMirrorChar(')') != '(' {
		t.Error("parentheses should mirror")
	}
	if getMirrorChar('a') != 'a' {
		t.Error("non-mirrored char returns itself")
	}
}

// --- GetRectArray grouping (spec test-vector invariants 3, 4, 5) ---

// tpWith builds a bare TextPage with a hand-seeded charList for the pure
// GetRectArray / index-map tests (no page needed).
func tpWith(chars []charInfo) *TextPage {
	tp := &TextPage{charList: chars}
	return tp
}

func normalChar(to *page.TextObject, b crt.FloatRect, u rune) charInfo {
	return charInfo{charType: charNormal, unicode: u, charBox: b, textObject: to}
}

func TestGetRectArrayGroupsByTextObject(t *testing.T) {
	// Two distinct non-nil text-object identities. We only need pointer identity,
	// so use two zero-value *page.TextObject addresses.
	t1 := &page.TextObject{}
	t2 := &page.TextObject{}
	chars := []charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'A'),
		normalChar(t1, box(10, 0, 20, 12), 'B'),
		normalChar(t2, box(40, 0, 50, 12), 'C'),
	}
	tp := tpWith(chars)
	rects := tp.getRectArray(0, -1)
	if len(rects) != 2 {
		t.Fatalf("got %d rects want 2", len(rects))
	}
	// rect0 = union of A,B boxes = (0,0,20,12); text "AB".
	if rects[0].Box != box(0, 0, 20, 12) || rects[0].Text != "AB" {
		t.Errorf("rect0=%+v text=%q want (0,0,20,12) AB", rects[0].Box, rects[0].Text)
	}
	if rects[1].Box != box(40, 0, 50, 12) || rects[1].Text != "C" {
		t.Errorf("rect1=%+v text=%q want (40,0,50,12) C", rects[1].Box, rects[1].Text)
	}
}

func TestGetRectArraySkipsGeneratedAndDegenerate(t *testing.T) {
	t1 := &page.TextObject{}
	gen := charInfo{charType: charGenerated, unicode: ' ', charBox: box(20, 0, 20, 0), textObject: t1}
	// Degenerate box: negative raw Width (right<left) must be skipped without
	// splitting the group (spec invariant 4).
	degenerate := normalChar(t1, box(30, 0, 25, 12), 'X') // right < left
	chars := []charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'A'),
		gen,
		degenerate,
		normalChar(t1, box(10, 0, 20, 12), 'B'),
	}
	tp := tpWith(chars)
	rects := tp.getRectArray(0, -1)
	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1 (generated+degenerate must not split)", len(rects))
	}
	if rects[0].Box != box(0, 0, 20, 12) {
		t.Errorf("rect0=%+v want (0,0,20,12)", rects[0].Box)
	}
	if rects[0].Text != "AB" {
		t.Errorf("rect0 text=%q want AB (generated space and degenerate excluded)", rects[0].Text)
	}
}

func TestGetWordArraySplitsWordsWithinTextObject(t *testing.T) {
	t1 := &page.TextObject{}
	tp := tpWith([]charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'H'),
		normalChar(t1, box(10, 0, 20, 12), 'i'),
		normalChar(t1, box(20, 0, 25, 12), ' '),
		normalChar(t1, box(30, 0, 40, 12), 'Q'),
		normalChar(t1, box(40, 0, 50, 12), '4'),
	})

	words := tp.GetWordArray()

	if len(words) != 2 {
		t.Fatalf("got %d words want 2", len(words))
	}
	if words[0].Text != "Hi" || words[0].Box != box(0, 0, 20, 12) {
		t.Errorf("word0=%+v want text Hi box (0,0,20,12)", words[0])
	}
	if words[1].Text != "Q4" || words[1].Box != box(30, 0, 50, 12) {
		t.Errorf("word1=%+v want text Q4 box (30,0,50,12)", words[1])
	}
}

func TestGetRectArrayEmpty(t *testing.T) {
	tp := tpWith(nil)
	if r := tp.getRectArray(0, -1); len(r) != 0 {
		t.Errorf("empty page should yield no rects, got %d", len(r))
	}
	if n := tp.CountRects(0, tp.CountChars()); n != 0 {
		t.Errorf("CountRects on empty=%d want 0", n)
	}
}

func TestCountRectsClampAndGetRect(t *testing.T) {
	t1 := &page.TextObject{}
	chars := []charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'A'),
		normalChar(t1, box(10, 0, 20, 12), 'B'),
	}
	tp := tpWith(chars)
	// count<0 clamps to all-from-start (the klippa FPDFText_CountRects(0,n) path).
	n := tp.CountRects(0, tp.CountChars())
	if n != 1 {
		t.Fatalf("CountRects=%d want 1", n)
	}
	r, ok := tp.GetRect(0)
	if !ok || r != box(0, 0, 20, 12) {
		t.Errorf("GetRect(0)=%+v,%v want (0,0,20,12),true", r, ok)
	}
	if _, ok := tp.GetRect(5); ok {
		t.Error("GetRect out of bounds should be false")
	}
	// start<0 -> -1.
	if tp.CountRects(-1, 1) != -1 {
		t.Error("CountRects(start<0) should be -1")
	}
}

// --- char-index map round-trips (Init-built) ---

func TestCharIndexRoundTrip(t *testing.T) {
	t1 := &page.TextObject{}
	// Three printable chars (all normal) -> char_indices = [{0,3}].
	tp := tpWith([]charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'A'),
		normalChar(t1, box(10, 0, 20, 12), 'B'),
		normalChar(t1, box(20, 0, 30, 12), 'C'),
	})
	// Re-run the Init char_indices builder over the seeded list.
	tp.buildCharIndices()
	for ci := 0; ci < tp.CountChars(); ci++ {
		ti := tp.TextIndexFromCharIndex(ci)
		if ti < 0 {
			t.Fatalf("TextIndexFromCharIndex(%d) < 0", ci)
		}
		if back := tp.CharIndexFromTextIndex(ti); back != ci {
			t.Errorf("round-trip char %d -> text %d -> char %d", ci, ti, back)
		}
	}
}

// --- GetTextByRect predicate ---

func TestGetTextByRect(t *testing.T) {
	t1 := &page.TextObject{}
	tp := tpWith([]charInfo{
		normalChar(t1, box(0, 0, 10, 12), 'A'),
		normalChar(t1, box(100, 0, 110, 12), 'B'),
	})
	// A box over the first glyph only selects 'A'.
	if got := tp.GetTextByRect(box(-1, -1, 11, 13)); got != "A" {
		t.Errorf("GetTextByRect=%q want A", got)
	}
}
