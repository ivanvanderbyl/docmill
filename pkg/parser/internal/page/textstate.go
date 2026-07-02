// Ported from core/fpdfapi/page/cpdf_textstate.{h,cpp} @ pdfium 0db284a42.
//
// CPDF_TextState is copy-on-write in C++ (SharedCopyOnWrite<TextData>); here it
// is a plain value-type struct that is copied by assignment, since the COW only
// dedupes allocations (irrelevant under Go's GC) and the interpreter copies the
// graphic states by value at each q/Q.
package page

import (
	"math"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
)

// TextRenderingMode mirrors the Tr operand (cpdf_textstate.h:21).
type TextRenderingMode int

const (
	ModeUnknown        TextRenderingMode = -1
	ModeFill           TextRenderingMode = 0
	ModeStroke         TextRenderingMode = 1
	ModeFillStroke     TextRenderingMode = 2
	ModeInvisible      TextRenderingMode = 3
	ModeFillClip       TextRenderingMode = 4
	ModeStrokeClip     TextRenderingMode = 5
	ModeFillStrokeClip TextRenderingMode = 6
	ModeClip           TextRenderingMode = 7 // == ModeLast
)

// TextState holds the text graphic state. The defaults shown are load-bearing
// (cpdf_textstate.cpp TextData ctor).
type TextState struct {
	font      *font.Font
	fontSize  float32 // default 1.0
	charSpace float32 // Tc, default 0
	wordSpace float32 // Tw, default 0
	textMode  TextRenderingMode

	// matrix is the cached, TRANSPOSED text-space matrix [a, b, c, d], set by
	// OnChangeTextMatrix (NOT the Tm operator's matrix; that lives in
	// AllStates.textMatrix). GetTextMatrix re-transposes it back.
	matrix [4]float32

	// ctm caches the CTM at draw time for stroke-mode text [a, c, b, d].
	ctm [4]float32
}

// newTextState returns a TextState with the PDFium defaults.
func newTextState() TextState {
	return TextState{
		fontSize: 1.0,
		textMode: ModeFill,
		matrix:   [4]float32{1, 0, 0, 1},
		ctm:      [4]float32{1, 0, 0, 1},
	}
}

// GetFont returns the active font (nil until Tf/gs sets one).
func (t *TextState) GetFont() *font.Font { return t.font }

// SetFont sets the active font.
func (t *TextState) SetFont(f *font.Font) { t.font = f }

// GetFontSize returns the Tf size.
func (t *TextState) GetFontSize() float32 { return t.fontSize }

// SetFontSize sets the Tf size.
func (t *TextState) SetFontSize(v float32) { t.fontSize = v }

// GetCharSpace returns Tc.
func (t *TextState) GetCharSpace() float32 { return t.charSpace }

// SetCharSpace sets Tc.
func (t *TextState) SetCharSpace(v float32) { t.charSpace = v }

// GetWordSpace returns Tw.
func (t *TextState) GetWordSpace() float32 { return t.wordSpace }

// SetWordSpace sets Tw.
func (t *TextState) SetWordSpace(v float32) { t.wordSpace = v }

// GetTextMode returns Tr.
func (t *TextState) GetTextMode() TextRenderingMode { return t.textMode }

// SetTextMode sets Tr.
func (t *TextState) SetTextMode(m TextRenderingMode) { t.textMode = m }

// GetMatrix returns the cached transposed matrix [a, b, c, d].
func (t *TextState) GetMatrix() [4]float32 { return t.matrix }

// MutableMatrix returns a pointer to the cached transposed matrix so the
// interpreter (OnChangeTextMatrix) and the text object (SetTextMatrix) can
// write its four entries.
func (t *TextState) MutableMatrix() *[4]float32 { return &t.matrix }

// GetCTM returns the cached draw-time CTM [a, c, b, d].
func (t *TextState) GetCTM() [4]float32 { return t.ctm }

// MutableCTM returns a pointer to the cached draw-time CTM.
func (t *TextState) MutableCTM() *[4]float32 { return &t.ctm }

// GetFontSizeH ports CPDF_TextState::TextData::GetFontSizeH
// (cpdf_textstate.cpp:124): abs(hypot(matrix[0], matrix[2]) * fontSize). The
// indices are [0] and [2] (a and c of the cached matrix), not [0],[1].
func (t *TextState) GetFontSizeH() float32 {
	h := float32(math.Hypot(float64(t.matrix[0]), float64(t.matrix[2])))
	product := h * t.fontSize
	if product < 0 {
		return -product
	}
	return product
}

// SetTextRenderingModeFromInt maps the Tr operand (cpdf_textstate.cpp:128).
// Valid iff 0 <= iMode <= 7.
func SetTextRenderingModeFromInt(iMode int) (TextRenderingMode, bool) {
	if iMode < 0 || iMode > 7 {
		return ModeUnknown, false
	}
	return TextRenderingMode(iMode), true
}

// TextRenderingModeIsClipMode ports cpdf_textstate.cpp:145.
func TextRenderingModeIsClipMode(m TextRenderingMode) bool {
	switch m {
	case ModeFillClip, ModeStrokeClip, ModeFillStrokeClip, ModeClip:
		return true
	default:
		return false
	}
}

// TextRenderingModeIsStrokeMode ports cpdf_textstate.cpp:155.
func TextRenderingModeIsStrokeMode(m TextRenderingMode) bool {
	switch m {
	case ModeStroke, ModeFillStroke, ModeStrokeClip, ModeFillStrokeClip:
		return true
	default:
		return false
	}
}
