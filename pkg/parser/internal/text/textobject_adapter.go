// Derived accessors over the page.TextObject public API.
//
// CPDF_TextPage reads several CPDF_TextObject members that the page package does
// not export directly (GetPos, GetCharWidth, text_state().GetFontSizeH(),
// text_state().GetCharSpace()). Plan 009 Phase H may only modify internal/font
// (GetCharBBox) and internal/text, so this file reconstructs those values from
// the exported API (Font, FontSize, TextMatrix, Items, Rect, ContentMarks)
// instead of touching the frozen page package.
//
// FIDELITY GAP — Tc (char spacing): the only member that CANNOT be reconstructed
// from the exported API is text_state().GetCharSpace() (the Tc operand). It is
// used solely by CalculateBaseSpace / CalculateBaseSpaceAdjustment to subtract a
// per-glyph base spacing in ProcessTextObjectItems. We assume Tc == 0 (so
// base_space == 0), which is what PDFium itself computes whenever Tc == 0 — the
// common case for the corpus. Documents that set a non-zero Tc will have a
// slightly different synthesised-space decision; this is a documented gap of the
// frozen-page-API constraint, not an algorithmic divergence.
package text

import (
	"math"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/page"
)

// objPos ports CPDF_TextObject::GetPos: the Tm origin (e, f). TextMatrix embeds
// it as the (E, F) translation, so we read it back from there.
func objPos(to *page.TextObject) crt.PointF {
	m := to.TextMatrix()
	return crt.PointF{X: m.E, Y: m.F}
}

// objFontSizeH ports CPDF_TextState::GetFontSizeH for a text object:
// abs(hypot(matrix[0], matrix[2]) * fontSize). The cached text matrix is stored
// transposed as [a,b,c,d]=[pm0,pm2,pm1,pm3]; TextMatrix() exposes A=pm0 and
// B=pm2, i.e. exactly matrix[0] and matrix[2]. So GetFontSizeH is reconstructed
// as abs(hypot(TextMatrix().A, TextMatrix().B) * FontSize()).
func objFontSizeH(to *page.TextObject) float32 {
	tm := to.TextMatrix()
	h := float32(math.Hypot(float64(tm.A), float64(tm.B)))
	product := h * to.FontSize()
	if product < 0 {
		return -product
	}
	return product
}

// objCharWidth ports CPDF_TextObject::GetCharWidth (cpdf_textobject.cpp:245) for
// horizontal text: GetFont()->GetCharWidthF(charcode) * (GetFontSize()/1000).
// The vertical-CID branch (GetVertWidth) is unavailable face-less and is the
// page package's existing deferral; horizontal is the corpus path.
func objCharWidth(to *page.TextObject, charCode uint32) float32 {
	f := to.Font()
	if f == nil {
		return 0
	}
	fontsize := to.FontSize() / 1000
	return f.GetCharWidthF(charCode) * fontsize
}

// getCharWidthInt ports the namespace GetCharWidth(charCode, font)
// (cpdf_textpage.cpp:188): the integer glyph-space advance, with the
// GetCharBBox fallback. AppendChar+GetStringWidth (the middle fallback) reduces
// to GetCharWidthF for a single code, which has already been tried, so it is
// skipped.
func getCharWidthInt(charCode uint32, f *font.Font) int {
	if charCode == page.InvalidCharCode {
		return 0
	}
	if f == nil {
		return 0
	}
	w := int(f.GetCharWidthF(charCode))
	if w > 0 {
		return w
	}
	l, b, r, t := f.GetCharBBox(charCode)
	if !fxRectValid(l, b, r, t) {
		return 0
	}
	width := max(
		// FX_RECT::Width
		r-l, 0)
	return width
}
