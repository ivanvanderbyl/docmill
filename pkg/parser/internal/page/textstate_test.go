// TextState unit vectors from the Phase F research spec (hand-derived).
package page

import "testing"

func TestTextStateDefaults(t *testing.T) {
	ts := newTextState()
	if ts.GetFontSize() != 1.0 {
		t.Errorf("FontSize = %v, want 1.0", ts.GetFontSize())
	}
	if ts.GetCharSpace() != 0 || ts.GetWordSpace() != 0 {
		t.Errorf("CharSpace/WordSpace = %v/%v, want 0/0", ts.GetCharSpace(), ts.GetWordSpace())
	}
	if ts.GetTextMode() != ModeFill {
		t.Errorf("TextMode = %v, want ModeFill", ts.GetTextMode())
	}
	if ts.GetMatrix() != [4]float32{1, 0, 0, 1} {
		t.Errorf("Matrix = %v, want {1,0,0,1}", ts.GetMatrix())
	}
	if ts.GetCTM() != [4]float32{1, 0, 0, 1} {
		t.Errorf("CTM = %v, want {1,0,0,1}", ts.GetCTM())
	}
}

func TestSetTextRenderingModeFromInt(t *testing.T) {
	if _, ok := SetTextRenderingModeFromInt(-1); ok {
		t.Error("-1 should be invalid")
	}
	for i := 0; i <= 7; i++ {
		m, ok := SetTextRenderingModeFromInt(i)
		if !ok || int(m) != i {
			t.Errorf("%d -> (%v, %v), want (%d, true)", i, m, ok, i)
		}
	}
	if _, ok := SetTextRenderingModeFromInt(8); ok {
		t.Error("8 should be invalid")
	}
}

func TestTextRenderingModeHelpers(t *testing.T) {
	stroke := map[TextRenderingMode]bool{ModeStroke: true, ModeFillStroke: true, ModeStrokeClip: true, ModeFillStrokeClip: true}
	clip := map[TextRenderingMode]bool{ModeFillClip: true, ModeStrokeClip: true, ModeFillStrokeClip: true, ModeClip: true}
	for m := ModeUnknown; m <= ModeClip; m++ {
		if got := TextRenderingModeIsStrokeMode(m); got != stroke[m] {
			t.Errorf("IsStrokeMode(%d) = %v, want %v", m, got, stroke[m])
		}
		if got := TextRenderingModeIsClipMode(m); got != clip[m] {
			t.Errorf("IsClipMode(%d) = %v, want %v", m, got, clip[m])
		}
	}
}

func TestGetFontSizeH(t *testing.T) {
	ts := newTextState()
	m := ts.MutableMatrix()
	m[0] = 3
	m[2] = 4
	ts.SetFontSize(2)
	// sqrt(3*3+4*4)*2 = 5*2 = 10.
	if got := ts.GetFontSizeH(); got != 10 {
		t.Errorf("GetFontSizeH = %v, want 10", got)
	}
}
