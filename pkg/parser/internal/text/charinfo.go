// CharInfo model + char-classification helpers ported from
// core/fpdftext/cpdf_textpage.{h,cpp} @ pdfium 0db284a42.
package text

import (
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/page"
)

// charType mirrors CPDF_TextPage::CharType.
type charType uint8

const (
	charNormal charType = iota
	charGenerated
	charNotUnicode
	charHyphen
	charPiece
)

// charInfo mirrors CPDF_TextPage::CharInfo. text_object_ is a *page.TextObject
// pointer (used both as group key in GetRectArray and for font/size lookups);
// nil for generated characters.
type charInfo struct {
	charType   charType
	unicode    rune
	charCode   uint32
	origin     crt.PointF
	charBox    crt.FloatRect
	matrix     crt.Matrix
	textObject *page.TextObject
}

// newCharInfo ports the 7-arg CharInfo constructor. PDFium also derives
// loose_char_box_ (FPDFText_GetLooseCharBox) here; this port has no consumer
// for it, so that computation is omitted.
func newCharInfo(ct charType, charCode uint32, unicode rune, origin crt.PointF,
	charBox crt.FloatRect, matrix crt.Matrix, textObject *page.TextObject) charInfo {
	return charInfo{
		charType:   ct,
		unicode:    unicode,
		charCode:   charCode,
		origin:     origin,
		charBox:    charBox,
		matrix:     matrix,
		textObject: textObject,
	}
}

// isControlChar ports the namespace IsControlChar (cpdf_textpage.cpp:131).
func isControlChar(ci charInfo) bool {
	switch ci.unicode {
	case 0x2, 0x3, 0x93, 0x94, 0x96, 0x97, 0x98, 0xfffe:
		return ci.charType != charHyphen
	default:
		return false
	}
}

// isNormalCharacter ports the namespace IsNormalCharacter (cpdf_textpage.cpp:
// 154).
func isNormalCharacter(ci charInfo) bool {
	if ci.unicode != 0 {
		return !isControlChar(ci)
	}
	return ci.charCode != 0
}

// isHyphenCode ports IsHyphenCode (cpdf_textpage.cpp:150).
func isHyphenCode(c rune) bool { return c == 0x2D || c == 0xAD }

// fxRectValid ports FX_RECT::Valid (left<=right && top<=bottom in FX_RECT's
// y-down convention). For a /FontBBox stored as (left, bottom, right, top) the
// equivalent test is left<=right and bottom<=top? PDFium's FX_RECT.Valid uses
// the raw fields; GetFontBBox returns them in (left, bottom, right, top) order
// matching the descriptor array. We treat "has area" as the validity gate.
func fxRectValid(l, b, r, t int) bool {
	return l != r || b != t
}
