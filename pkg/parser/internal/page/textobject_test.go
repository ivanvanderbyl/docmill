// TextObject / Form unit vectors from the Phase F research spec (hand-derived).
//
// NOTE: the face-less font port has no GetCharBBox, so the OriginalRect/Rect
// numbers from spec vectors 6/7 (which assume a real glyph bbox) are NOT
// asserted here. The per-glyph ADVANCE/ORIGIN (charPos) and the returned pen
// delta ARE faithful (computed from GetCharWidthF + Tc/Tw + TJ kerning), so we
// assert those.
package page

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

func nm(s string) *objects.Name   { return objects.NewName(s) }
func nfi(n int32) *objects.Number { return objects.NewNumberFromInt(n) }

// simpleFont builds a TrueType simple font with WinAnsiEncoding and the given
// per-code widths (FirstChar..LastChar inclusive).
func simpleFont(t *testing.T, firstChar int32, widths []int32) *font.Font {
	t.Helper()
	d := objects.NewDictionary()
	d.SetFor("Subtype", nm("TrueType"))
	d.SetFor("BaseFont", nm("Helvetica"))
	d.SetFor("Encoding", nm("WinAnsiEncoding"))
	d.SetFor("FirstChar", nfi(firstChar))
	d.SetFor("LastChar", nfi(firstChar+int32(len(widths))-1))
	warr := objects.NewArray()
	for _, w := range widths {
		warr.Append(nfi(w))
	}
	d.SetFor("Widths", warr)
	f := font.Load(d, nil)
	if f == nil {
		t.Fatal("font.Load returned nil")
	}
	return f
}

// --- Spec vector 4: GetTextMatrix transposition ---

func TestGetTextMatrixTransposition(t *testing.T) {
	to := newTextObject(kNoContentStream)
	m := to.graphicStates.textState.MutableMatrix()
	*m = [4]float32{2, 3, 4, 5} // a,b,c,d packed
	to.pos = crt.PointF{X: 10, Y: 20}
	got := to.TextMatrix()
	want := crt.NewMatrix(2, 4, 3, 5, 10, 20) // b=pm[2]=4, c=pm[1]=3
	if got != want {
		t.Errorf("GetTextMatrix = %+v, want %+v", got, want)
	}

	// Round-trip via SetTextMatrix.
	to2 := newTextObject(kNoContentStream)
	to2.graphicStates.textState.SetFont(simpleFont(t, 65, []int32{700}))
	to2.setSegments([][]byte{[]byte("A")}, nil)
	to2.setTextMatrix(crt.NewMatrix(2, 4, 3, 5, 10, 20))
	if got := to2.graphicStates.textState.GetMatrix(); got != [4]float32{2, 3, 4, 5} {
		t.Errorf("internal matrix after SetTextMatrix = %v, want {2,3,4,5}", got)
	}
	if to2.pos != (crt.PointF{X: 10, Y: 20}) {
		t.Errorf("pos after SetTextMatrix = %+v, want {10,20}", to2.pos)
	}
}

// --- CalcPositionData advance: single glyph (spec vector 6, advance only) ---

func TestCalcPositionDataSingleGlyph(t *testing.T) {
	f := simpleFont(t, 65, []int32{722}) // 'A' width 722
	to := newTextObject(kNoContentStream)
	to.graphicStates.textState.SetFont(f)
	to.graphicStates.textState.SetFontSize(12)
	to.setSegments([][]byte{[]byte("A")}, nil)
	pos := to.calcPositionData(1.0)
	// curpos = 722*12/1000 = 8.664; horzScale 1.
	if !approx(pos.X, 8.664) || pos.Y != 0 {
		t.Errorf("CalcPositionData = %+v, want {8.664, 0}", pos)
	}
	if len(to.charPos) != 0 {
		t.Errorf("charPos len = %d, want 0 (single glyph)", len(to.charPos))
	}
}

func TestCalcPositionDataSetsGlyphMetricRect(t *testing.T) {
	f := simpleFont(t, 65, []int32{700}) // 'A' width 700
	to := newTextObject(kNoContentStream)
	to.graphicStates.textState.SetFont(f)
	to.graphicStates.textState.SetFontSize(10)
	to.setSegments([][]byte{[]byte("A")}, nil)

	to.calcPositionData(1.0)

	if got := to.Rect(); got != (crt.FloatRect{Left: 0, Bottom: -2, Right: 7, Top: 8}) {
		t.Errorf("Rect = %+v, want glyph metric box {0,-2,7,8}", got)
	}
}

// --- CalcPositionData advance: char-space + word-space (spec vector 7) ---

func TestCalcPositionDataCharWordSpace(t *testing.T) {
	// 'A' width 722 at code 65, space width 250 at code 32.
	// Build a font covering 32..65 with the two relevant widths.
	widths := make([]int32, 65-32+1)
	widths[32-32] = 250 // space
	widths[65-32] = 722 // 'A'
	f := simpleFont(t, 32, widths)
	to := newTextObject(kNoContentStream)
	to.graphicStates.textState.SetFont(f)
	to.graphicStates.textState.SetFontSize(10)
	to.graphicStates.textState.SetCharSpace(0.5)
	to.graphicStates.textState.SetWordSpace(2)
	to.setSegments([][]byte{[]byte("A ")}, nil)
	pos := to.calcPositionData(1.0)
	// After A: 7.22 + Tc 0.5 = 7.72 (charPos[0]). After space: +2.5 -> 10.22;
	// +Tw 2 -> 12.22; +Tc 0.5 -> 12.72.
	if !approx(pos.X, 12.72) {
		t.Errorf("CalcPositionData.X = %v, want 12.72", pos.X)
	}
	if len(to.charPos) != 1 || !approx(to.charPos[0], 7.72) {
		t.Errorf("charPos = %v, want [7.72]", to.charPos)
	}
}

// --- Kerning sentinel (spec vector 8): SetSegments builds the sentinel ---

func TestSetSegmentsKerningSentinel(t *testing.T) {
	f := simpleFont(t, 65, []int32{722, 667}) // A=722, B=667
	to := newTextObject(kNoContentStream)
	to.graphicStates.textState.SetFont(f)
	to.graphicStates.textState.SetFontSize(12)
	to.setSegments([][]byte{[]byte("A"), []byte("B")}, []float32{100})
	if len(to.charCodes) != 3 {
		t.Fatalf("charCodes len = %d, want 3 (A, sentinel, B)", len(to.charCodes))
	}
	if to.charCodes[1] != InvalidCharCode {
		t.Errorf("charCodes[1] = %x, want InvalidCharCode", to.charCodes[1])
	}
	// charPos[0] stashes the kerning (100) before CalcPositionData overwrites it.
	if to.charPos[0] != 100 {
		t.Errorf("stashed kerning charPos[0] = %v, want 100", to.charPos[0])
	}
	to.calcPositionData(1.0)
	// After CalcPositionData, item 0 origin is 0; item 2 (B) origin is the
	// running advance: wA(8.664) - kerning(100*12/1000=1.2) = 7.464.
	items := to.Items()
	if items[0].Origin.X != 0 {
		t.Errorf("item 0 origin.x = %v, want 0", items[0].Origin.X)
	}
	if !approx(items[2].Origin.X, 7.464) {
		t.Errorf("item 2 (B) origin.x = %v, want 7.464", items[2].Origin.X)
	}
}

// --- Spec vector 9: Form.ChooseResourcesDict precedence ---

func TestChooseResourcesDict(t *testing.T) {
	R := objects.NewDictionary()
	P := objects.NewDictionary()
	G := objects.NewDictionary()
	if chooseResourcesDict(R, P, G) != R {
		t.Error("(R,P,G) should pick R")
	}
	if chooseResourcesDict(nil, P, G) != P {
		t.Error("(nil,P,G) should pick P")
	}
	if chooseResourcesDict(nil, nil, G) != G {
		t.Error("(nil,nil,G) should pick G")
	}
	if chooseResourcesDict(nil, nil, nil) != nil {
		t.Error("(nil,nil,nil) should be nil")
	}
}

func approx(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-3
}
