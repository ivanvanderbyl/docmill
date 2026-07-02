// Face-less glyph-metric accessors used by the text-extraction layer
// (pkg/parser/internal/text). Ported behaviourally from
// core/fpdfapi/font/cpdf_font.cpp / cpdf_simplefont.cpp / cpdf_cidfont.cpp and
// cpdf_fontdescriptor @ pdfium 0db284a42.
package font

// GetCharBBox returns the glyph bounding box in 1000-unit glyph space (left,
// bottom, right, top as ints), matching CPDF_Font::GetCharBBox.
//
// FACE-BACKED PATH. When the font has an embedded program (FontFile/2/3) that one
// of the glyph-outline readers can parse, this returns the glyph's CONTROL box
// (FT_Outline_Get_CBox) scaled to 1000-unit space via NormalizeFontMetric — the
// same value PDFium derives from FreeType's glyph->metrics.
//
// DESCRIPTOR FALLBACK. When there is no glyph program, the reader cannot parse
// it, or the code maps to no glyph (0xffff), PDFium itself falls back; this port
// reproduces that fallback:
//
//   - horizontal extent: left=0, right=int(GetCharWidthF(charCode)) — the glyph
//     advance width (already /MissingWidth-aware). The textpage repairs a
//     degenerate width via GetCharWidth.
//   - vertical extent: top=/Ascent, bottom=/Descent from the font descriptor;
//     else /FontBBox top/bottom; else the face-default 0.8em / -0.2em.
func (f *Font) GetCharBBox(charCode uint32) (left, bottom, right, top int) {
	if f.charBBoxCache != nil {
		if cached, ok := f.charBBoxCache[charCode]; ok {
			return cached[0], cached[1], cached[2], cached[3]
		}
	} else {
		f.charBBoxCache = make(map[uint32][4]int)
	}

	defer func() {
		f.charBBoxCache[charCode] = [4]int{left, bottom, right, top}
	}()

	if l, b, r, t, ok := f.glyphControlBox(charCode); ok {
		return l, b, r, t
	}

	// Horizontal: advance width (already /MissingWidth-aware via GetCharWidthF).
	left = 0
	right = int(f.GetCharWidthF(charCode))

	// Vertical: descriptor ascent/descent, then /FontBBox, then face default.
	top = f.ascent
	bottom = f.descent
	if top == 0 && bottom == 0 {
		if f.fontBBox.top != 0 || f.fontBBox.bottom != 0 {
			top = f.fontBBox.top
			bottom = f.fontBBox.bottom
		} else {
			// Conventional default for a face-less font (0.8em / -0.2em).
			top = 800
			bottom = -200
		}
	}
	return left, bottom, right, top
}

// GetFontBBox returns the descriptor /FontBBox in 1000-unit glyph space as
// (left, bottom, right, top). It mirrors CPDF_Font::GetFontBBox() (the cached
// font_bbox_ from LoadFontDescriptor). The zero rect means "no /FontBBox"; the
// text layer treats that as invalid (Valid() == false) and skips loose bounds.
func (f *Font) GetFontBBox() (left, bottom, right, top int) {
	return f.fontBBox.left, f.fontBBox.bottom, f.fontBBox.right, f.fontBBox.top
}

// CharCodeFromUnicode ports CPDF_Font::CharCodeFromUnicode +
// CPDF_SimpleFont::CharCodeFromUnicode (cpdf_simplefont.cpp:338): the ToUnicode
// reverse map first, then the simple-font encoding's reverse scan. Returns 0
// when the unicode is unmapped, matching the base class (the textpage's
// CalculateSpaceThreshold checks the result against kInvalidCharCode, so a 0
// here is treated as a valid code — which is correct for the predefined space
// mapping where ' ' resolves to char code 0x20 via the encoding scan).
//
// For CID/Type3 fonts only the ToUnicode reverse lookup is available in this
// face-less port (CID byte-string reconstruction needs the CMap reverse table,
// a deferred follow-up); that matches PDFium's behaviour for the common
// Identity-H + /ToUnicode corpus.
func (f *Font) CharCodeFromUnicode(unicode rune) uint32 {
	if !f.toUnicodeLoaded {
		f.loadUnicodeMap()
	}
	if f.toUnicodeMap != nil {
		if ret := f.toUnicodeMap.ReverseLookup(unicode); ret != 0 {
			return ret
		}
	}
	if f.kind != kindCID && f.encoding != nil {
		if c := f.encoding.CharCodeFromUnicode(unicode); c >= 0 {
			return uint32(c)
		}
	}
	return 0
}
