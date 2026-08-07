// Param-ring + text-positioning unit vectors from the Phase F research spec
// (derived, hand-verified against the algorithm), ported as Go tests.
package page

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
)

// newTestParser builds a bare StreamContentParser with a default AllStates,
// suitable for exercising the param ring and the text-position math directly.
func newTestParser() *StreamContentParser {
	holder := &PageObjectHolder{}
	return newStreamContentParser(nil, nil, nil, holder, nil, crt.FloatRect{}, nil, newFormRecursionState())
}

// --- Derived vector B: GetMatrix/GetNumbers ordering ---

func TestGetMatrixAndNumbersOrdering(t *testing.T) {
	p := newTestParser()
	for _, n := range []string{"1", "2", "3", "4", "5", "6"} {
		p.addNumberParam([]byte(n))
	}
	m := p.getMatrix()
	want := crt.NewMatrix(1, 2, 3, 4, 5, 6)
	if m != want {
		t.Errorf("GetMatrix = %+v, want %+v", m, want)
	}

	p2 := newTestParser()
	for _, n := range []string{"1", "2", "3"} {
		p2.addNumberParam([]byte(n))
	}
	got := p2.getNumbers(3)
	if got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("GetNumbers(3) = %v, want [1 2 3] (forward order)", got)
	}
}

func TestPathNumberBufferResetsWithoutLosingFixedStorage(t *testing.T) {
	var numbers pathNumberBuffer
	for _, value := range []float32{1, 2, 3, 4, 5, 6, 7} {
		numbers.append(value)
	}
	if got := numbers.valuesSlice(); len(got) != 6 || got[0] != 1 || got[5] != 6 {
		t.Fatalf("values = %v, want first six operands", got)
	}

	numbers.reset()
	numbers.append(42)
	if got := numbers.valuesSlice(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("values after reset = %v, want [42]", got)
	}
}

func TestGraphicsStateSaveRestoreReusesValueStack(t *testing.T) {
	p := newTestParser()
	p.syntax = newStreamParser(nil)
	p.curStates.MutableGraphState().SetLineWidth(3)

	allocs := testing.AllocsPerRun(100, func() {
		for range 8 {
			p.handleSaveGraphState()
			p.curStates.MutableGraphState().SetLineWidth(9)
		}
		for range 8 {
			p.handleRestoreGraphState()
		}
	})

	if allocs != 0 {
		t.Fatalf("save/restore allocations = %v, want 0 after stack warm-up", allocs)
	}
	if got := p.curStates.MutableGraphState().GetLineWidth(); got != 3 {
		t.Fatalf("restored line width = %v, want 3", got)
	}
}

// --- Param ring wrap-around (cpdf_streamcontentparser.cpp:433) ---

func TestParamRingWrapDropsOldest(t *testing.T) {
	p := newTestParser()
	// Push 17 numbers into the 16-slot ring. Faithful to GetNextParamPos
	// (cpdf_streamcontentparser.cpp:433): the 17th push advances paramStartPos to
	// 1 and overwrites slot 1 (which held value 1) with value 16, so value 1 is
	// the one dropped — NOT the absolute oldest (0). The reverse-index window
	// then reads index 0 from slot 0 (=0) and index 15 from slot 1 (=16).
	for i := range 17 {
		p.addNumberParam([]byte(itoa(i)))
	}
	if p.paramCount != kParamBufSize {
		t.Fatalf("paramCount = %d, want %d", p.paramCount, kParamBufSize)
	}
	if got := p.getNumber(0); got != 0 {
		t.Errorf("getNumber(0) = %v, want 0", got)
	}
	if got := p.getNumber(15); got != 16 {
		t.Errorf("getNumber(15) = %v, want 16", got)
	}
	// Value 1 was the overwritten slot, so it appears nowhere in the window.
	for i := uint32(0); i < p.paramCount; i++ {
		if p.getNumber(i) == 1 {
			t.Errorf("value 1 should have been dropped, found at index %d", i)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// --- Derived vector C: Tz fraction ---

func TestTzFraction(t *testing.T) {
	p := newTestParser()
	p.addNumberParam([]byte("50"))
	p.handleSetHorzScale()
	if got := p.curStates.TextHorzScale(); got != 0.5 {
		t.Errorf("50 Tz -> horzScale = %v, want 0.5", got)
	}
	p.clearAllParams()
	p.addNumberParam([]byte("100"))
	p.handleSetHorzScale()
	if got := p.curStates.TextHorzScale(); got != 1.0 {
		t.Errorf("100 Tz -> horzScale = %v, want 1.0", got)
	}
}

// --- Derived vector D: TD leading ---

func TestTDLeading(t *testing.T) {
	p := newTestParser()
	// tx ty TD -> textLinePos += {tx,ty}; textLeading == -ty.
	p.addNumberParam([]byte("3"))  // tx
	p.addNumberParam([]byte("-7")) // ty
	p.handleMoveTextPointSetLeading()
	if p.curStates.textLinePos.X != 3 || p.curStates.textLinePos.Y != -7 {
		t.Errorf("textLinePos = %+v, want {3,-7}", p.curStates.textLinePos)
	}
	if p.curStates.textLeading != 7 {
		t.Errorf("textLeading = %v, want 7 (= -ty)", p.curStates.textLeading)
	}
}

// --- Derived vector E: GetHorizontalTextSize sign + magnitude ---

func TestHorizontalTextSize(t *testing.T) {
	p := newTestParser()
	p.curStates.MutableTextState().SetFontSize(12)
	p.curStates.SetTextHorzScale(1)
	// kerning -250 -> vertical = -250*12/1000 = -3; horizontal = -3*1 = -3.
	if got := p.getVerticalTextSize(-250); got != -3 {
		t.Errorf("GetVerticalTextSize(-250) = %v, want -3", got)
	}
	if got := p.getHorizontalTextSize(-250); got != -3 {
		t.Errorf("GetHorizontalTextSize(-250) = %v, want -3", got)
	}
}

// --- BT resets text position; GetTransformedTextPosition with rise ---

func TestTextPositionMath(t *testing.T) {
	s := newAllStates()
	s.SetTextMatrix(crt.NewMatrix(1, 0, 0, 1, 100, 700))
	s.MoveTextPoint(crt.PointF{X: 0, Y: 0})
	s.textRise = 5
	got := s.GetTransformedTextPosition()
	// inner = {0, 0+5}; Tm.Transform -> {100, 705}; ctm identity.
	if got.X != 100 || got.Y != 705 {
		t.Errorf("GetTransformedTextPosition = %+v, want {100,705}", got)
	}
}
