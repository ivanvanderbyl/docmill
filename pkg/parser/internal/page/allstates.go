// Ported from core/fpdfapi/page/cpdf_allstates.{h,cpp} and
// cpdf_graphicstates.{h} @ pdfium 0db284a42.
//
// GraphicStates is the q/Q-saved bundle (clip/graph/color/text/general). For
// text extraction only the text state is load-bearing; graph state carries the
// line width (used for the stroke-mode rect inflate) and the rest is tracked
// only so operands advance correctly. AllStates is the live interpreter
// register file that the operator table reads and writes.
package page

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"

// GraphState is the stroking state (line width/cap/join/miter/dash). Only
// lineWidth is consumed (stroke-mode rect inflate); the rest is stored so the
// w/J/j/M/d operators have somewhere to land.
type GraphState struct {
	lineWidth float32
	lineCap   int
	lineJoin  int
	miter     float32
	dash      []float32
	dashPhase float32
}

// GetLineWidth ports CFX_GraphStateData::GetLineWidth (used by
// CalcPositionDataInternal for the stroke-mode inflate).
func (g *GraphState) GetLineWidth() float32 { return g.lineWidth }

// SetLineWidth sets the stroke width (w operator / ExtGState LW).
func (g *GraphState) SetLineWidth(v float32) { g.lineWidth = v }

// ColorState is the fill/stroke colour. Inert for text extraction (we never
// evaluate colour); kept as an empty placeholder so SetDefaultStates and the
// colour operators have a target.
type ColorState struct{}

// GeneralState is alpha/blend/softmask/etc. Inert for text extraction.
type GeneralState struct{}

// GraphicStates is the persisted bundle saved by q and restored by Q. In C++
// these are copy-on-write shared refs; here a whole-struct value copy is taken
// on save and restored on Q (a few floats, a font pointer, and matrices).
type GraphicStates struct {
	clipPath     ClipPath
	graphState   GraphState
	colorState   ColorState
	textState    TextState
	generalState GeneralState
}

// newGraphicStates returns the default graphic-state bundle (text-state defaults
// applied).
func newGraphicStates() GraphicStates {
	return GraphicStates{textState: newTextState()}
}

// SetDefaultStates ports CPDF_GraphicStates::SetDefaultStates: emplace + default
// the colour state. (Text-state defaults live in newTextState.)
func (g *GraphicStates) SetDefaultStates() { g.colorState = ColorState{} }

// AllStates is the live interpreter state (cpdf_allstates.h). It is what q/Q
// saves and what every operator mutates.
type AllStates struct {
	graphicStates GraphicStates
	textMatrix    crt.Matrix // Tm
	ctm           crt.Matrix // current transformation matrix
	parentMatrix  crt.Matrix // matrix in effect when this stream began (form parent)
	textPos       crt.PointF // running text position within the current line
	textLinePos   crt.PointF // start-of-line position
	textLeading   float32    // TL
	textRise      float32    // Ts
	textHorzScale float32    // Tz/100 (1.0 == 100%)
}

// newAllStates returns the default interpreter state: identity matrices,
// textHorzScale=1, text-state defaults.
func newAllStates() *AllStates {
	return &AllStates{
		graphicStates: newGraphicStates(),
		textMatrix:    crt.IdentityMatrix(),
		ctm:           crt.IdentityMatrix(),
		parentMatrix:  crt.IdentityMatrix(),
		textHorzScale: 1.0,
	}
}

// clone returns a deep value copy (the dash slice is shared but never mutated
// in place; everything else is value-copied), matching the C++ q-save.
func (s *AllStates) clone() *AllStates {
	cp := *s
	return &cp
}

// --- sub-state accessors ---

// TextState returns a read-only view of the text state.
func (s *AllStates) TextState() *TextState { return &s.graphicStates.textState }

// MutableTextState returns the writable text state.
func (s *AllStates) MutableTextState() *TextState { return &s.graphicStates.textState }

// MutableGraphState returns the writable graph (stroking) state.
func (s *AllStates) MutableGraphState() *GraphState { return &s.graphicStates.graphState }

// --- matrix accessors ---

// TextMatrix returns Tm.
func (s *AllStates) TextMatrix() crt.Matrix { return s.textMatrix }

// SetTextMatrix sets Tm (the Tm operator).
func (s *AllStates) SetTextMatrix(m crt.Matrix) { s.textMatrix = m }

// CTM returns the current transformation matrix.
func (s *AllStates) CTM() crt.Matrix { return s.ctm }

// SetCTM sets the current transformation matrix.
func (s *AllStates) SetCTM(m crt.Matrix) { s.ctm = m }

// PrependToCTM ports prepend_to_current_transformation_matrix: ctm = m * ctm.
func (s *AllStates) PrependToCTM(m crt.Matrix) { s.ctm = m.Multiply(s.ctm) }

// ParentMatrix returns the form-parent matrix.
func (s *AllStates) ParentMatrix() crt.Matrix { return s.parentMatrix }

// SetParentMatrix sets the form-parent matrix.
func (s *AllStates) SetParentMatrix(m crt.Matrix) { s.parentMatrix = m }

// --- scalar text-state accessors ---

// SetTextLeading sets TL.
func (s *AllStates) SetTextLeading(v float32) { s.textLeading = v }

// SetTextRise sets Ts.
func (s *AllStates) SetTextRise(v float32) { s.textRise = v }

// TextHorzScale returns Tz as a fraction.
func (s *AllStates) TextHorzScale() float32 { return s.textHorzScale }

// SetTextHorzScale sets Tz/100 (the operator passes value/100).
func (s *AllStates) SetTextHorzScale(v float32) { s.textHorzScale = v }

// --- text-position math (cpdf_allstates.cpp) ---

// ResetTextPosition ports ResetTextPosition (the BT operator).
func (s *AllStates) ResetTextPosition() {
	s.textLinePos = crt.PointF{}
	s.textPos = crt.PointF{}
}

// GetTransformedTextPosition ports GetTransformedTextPosition
// (cpdf_allstates.cpp:181): ctm.Transform(textMatrix.Transform({textPos.x,
// textPos.y + textRise})). Rise is added to y INSIDE the inner point before Tm.
func (s *AllStates) GetTransformedTextPosition() crt.PointF {
	inner := crt.PointF{X: s.textPos.X, Y: s.textPos.Y + s.textRise}
	return s.ctm.Transform(s.textMatrix.Transform(inner))
}

// MoveTextPoint ports MoveTextPoint (Td): textLinePos += point; textPos = textLinePos.
func (s *AllStates) MoveTextPoint(point crt.PointF) {
	s.textLinePos.X += point.X
	s.textLinePos.Y += point.Y
	s.textPos = s.textLinePos
}

// MoveTextToNextLine ports MoveTextToNextLine (T*): textLinePos.y -= leading.
func (s *AllStates) MoveTextToNextLine() {
	s.textLinePos.Y -= s.textLeading
	s.textPos = s.textLinePos
}

// IncrementTextPositionX ports IncrementTextPositionX.
func (s *AllStates) IncrementTextPositionX(v float32) { s.textPos.X += v }

// IncrementTextPositionY ports IncrementTextPositionY.
func (s *AllStates) IncrementTextPositionY(v float32) { s.textPos.Y += v }
