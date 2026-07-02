// charcode -> GID resolution for the per-glyph GetCharBBox. PDFium fills this in
// the face-backed LoadGlyphMap (simple fonts) / GlyphFromCharCode (CID fonts);
// this is the equivalent over the pure-Go glyph sources. It is intentionally a
// pragmatic subset of CPDF_TrueTypeFont::LoadGlyphMap: name-driven for Type1/CFF
// (the dominant corpus path), cmap/symbol-driven for TrueType, and CID->GID via
// /CIDToGIDMap (Identity or stream) for CID fonts. Codes that resolve to no glyph
// fall back to the descriptor box (exactly as PDFium does for a 0xffff glyph).
package font

// noGlyphIndex marks "no glyph" in the simple-font cache (PDFium's 0xffff).
const noGlyphIndex uint16 = 0xffff

// ensureGlyphSource parses the embedded font program on first use.
func (f *Font) ensureGlyphSource() glyphSource {
	if f.glyphSrcReady {
		return f.glyphSrc
	}
	f.glyphSrcReady = true
	if len(f.fontProgram) == 0 {
		return nil
	}
	f.glyphSrc = newGlyphSource(f.fontProgramTag, f.fontProgramSubtype, f.fontProgram)
	return f.glyphSrc
}

// glyphControlBox returns the per-glyph control box in 1000-unit glyph space
// (left, bottom, right, top). ok=false ONLY when there is no usable glyph program
// (non-embedded font / parse failed), in which case the caller uses the
// descriptor box. When a program IS present, this matches PDFium's face-backed
// path: a resolved glyph yields its control box; an empty glyph (e.g. space)
// yields the ZERO box (PDFium's FX_RECT()) — NOT a descriptor ascent/descent box,
// which would inflate the line's union rect. A code this port cannot map to a GID
// (which PDFium's real face would) falls back to the descriptor box so the text
// survives instead of collapsing to a dropped zero box.
func (f *Font) glyphControlBox(charCode uint32) (left, bottom, right, top int, ok bool) {
	src := f.ensureGlyphSource()
	if src == nil {
		return 0, 0, 0, 0, false
	}
	gid, resolved := f.glyphIndex(charCode)
	if !resolved {
		// This port could not map the code to a GID (PDFium's real face would).
		// Fall back to the descriptor box so the text is preserved rather than
		// collapsing to a zero box the textpage drops.
		return 0, 0, 0, 0, false
	}
	box, has := src.controlBox(gid)
	if !has {
		// Resolved to a glyph with no outline. For a genuine blank (space) PDFium
		// returns FX_RECT(); emit that faithful zero box so it does not inflate the
		// line's union rect — GetTextByRect re-extraction still supplies the space.
		// A NON-space code that resolves to an empty glyph is almost always a GID
		// miss in this port (e.g. symbolic Computer-Modern), so fall back to the
		// descriptor box to keep the text instead of dropping a degenerate rect.
		if isSpaceLike(f.UnicodeFromCharCode(charCode)) {
			return 0, 0, 0, 0, true
		}
		return 0, 0, 0, 0, false
	}
	upm := src.unitsPerEm()
	return normalizeFontMetric(box.xMin, upm),
		normalizeFontMetric(box.yMin, upm),
		normalizeFontMetric(box.xMax, upm),
		normalizeFontMetric(box.yMax, upm),
		true
}

// glyphAdvanceWidth returns an embedded-font width in 1000-unit glyph space for
// simple fonts whose PDF dictionary omits /Widths. Prefer the program's real
// advance metric; if that parser lacks advance access, fall back to the visible
// control-box width so glyphs do not collapse to a zero advance.
func (f *Font) glyphAdvanceWidth(charCode uint32) (int, bool) {
	src := f.ensureGlyphSource()
	if src == nil {
		return 0, false
	}
	gid, resolved := f.glyphIndex(charCode)
	if !resolved {
		return 0, false
	}
	if width, ok := src.advanceWidth(gid); ok && width > 0 {
		return normalizeFontMetric(width, src.unitsPerEm()), true
	}
	box, ok := src.controlBox(gid)
	if !ok || box.empty() {
		return 0, false
	}
	return normalizeFontMetric(box.xMax-box.xMin, src.unitsPerEm()), true
}

// isSpaceLike reports whether a Unicode scalar is a whitespace separator whose
// glyph is legitimately blank (so an empty outline is expected, not a GID miss).
func isSpaceLike(r rune) bool {
	switch r {
	case 0x20, 0xA0, 0x09, 0x0A, 0x0D, 0x2007, 0x202F, 0x2000, 0x2001, 0x2002,
		0x2003, 0x2004, 0x2005, 0x2006, 0x2008, 0x2009, 0x200A, 0x205F, 0x3000:
		return true
	}
	return false
}

// glyphIndex maps a char code to a GID via the appropriate path for the kind.
func (f *Font) glyphIndex(charCode uint32) (uint16, bool) {
	if f.kind == kindCID {
		return f.cidGlyphIndex(charCode)
	}
	return f.simpleGlyphIndex(charCode)
}

// cidGlyphIndex ports CPDF_CIDFont::GlyphFromCharCode: charcode -> CID (via the
// CMap) -> GID (via /CIDToGIDMap, or GID==CID for Identity / CIDFontType0C).
func (f *Font) cidGlyphIndex(charCode uint32) (uint16, bool) {
	if f.cidGIDCache == nil {
		f.cidGIDCache = make(map[uint32]uint16)
	} else if g, ok := f.cidGIDCache[charCode]; ok {
		return g, g != noGlyphIndex
	}
	cid := f.CIDFromCharCode(charCode)
	gid := f.cidToGID(cid)
	f.cidGIDCache[charCode] = gid
	return gid, gid != noGlyphIndex
}

// cidToGID applies /CIDToGIDMap: a stream is a big-endian uint16 table indexed by
// CID; nil means Identity (GID==CID), which also covers CIDFontType0C (FreeType
// resolves CID->GID through the CFF charset internally — here go-text indexes
// charstrings by GID, so Identity-charset subsets map directly).
func (f *Font) cidToGID(cid uint16) uint16 {
	if f.cidGIDMap == nil {
		return cid
	}
	i := int(cid) * 2
	if i+1 >= len(f.cidGIDMap) {
		return 0
	}
	return uint16(f.cidGIDMap[i])<<8 | uint16(f.cidGIDMap[i+1])
}

// simpleGlyphIndex resolves a simple-font charcode to a GID, caching all 256 on
// first use. Type1/CFF resolve by glyph name (the font program's charset/post);
// TrueType prefers the program cmap (Unicode, then 0xF000 symbol, then Mac/raw).
func (f *Font) simpleGlyphIndex(charCode uint32) (uint16, bool) {
	if charCode > 0xff {
		return noGlyphIndex, false
	}
	if !f.glyphIndicesReady {
		f.buildSimpleGlyphIndices()
	}
	g := f.glyphIndices[charCode]
	return g, g != noGlyphIndex
}

func (f *Font) buildSimpleGlyphIndices() {
	f.glyphIndicesReady = true
	for i := range f.glyphIndices {
		f.glyphIndices[i] = noGlyphIndex
	}
	src := f.glyphSrc
	if src == nil {
		return
	}
	for c := range uint32(256) {
		if gid, ok := f.resolveSimpleGID(src, c); ok {
			f.glyphIndices[c] = gid
		}
	}
}

// resolveSimpleGID is the per-charcode resolution for a simple font.
func (f *Font) resolveSimpleGID(src glyphSource, charCode uint32) (uint16, bool) {
	name := f.glyphNames[charCode]
	var uni rune
	if f.encoding != nil {
		uni = f.encoding.UnicodeFromCharCode(uint8(charCode))
	}

	if f.kind == kindTrueType {
		// Prefer the Unicode cmap (driven by the encoding's Unicode), then the
		// post table by name, then the (3,0) symbol cmap (0xF000 | code), then
		// the raw code as a cmap key / GID.
		if uni != 0 {
			if gid, ok := src.gidForRune(uni); ok {
				return gid, true
			}
		}
		if name != "" {
			if gid, ok := src.gidForName(name); ok {
				return gid, true
			}
		}
		if gid, ok := src.gidForRune(rune(0xF000 | charCode)); ok {
			return gid, true
		}
		if gid, ok := src.gidForRune(rune(charCode)); ok {
			return gid, true
		}
		return 0, false
	}

	// Type1 / CFF: glyph name first (FT_Get_Name_Index), then the builtin
	// encoding by Unicode.
	if name != "" {
		if gid, ok := src.gidForName(name); ok {
			return gid, true
		}
	}
	if uni != 0 {
		if gid, ok := src.gidForRune(uni); ok {
			return gid, true
		}
	}
	return 0, false
}
