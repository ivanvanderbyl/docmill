// Ported from core/fpdfapi/font/cpdf_font.cpp (LoadFontDescriptor),
// cpdf_simplefont.cpp (LoadCommon/LoadCharWidths/LoadDifferences/LoadPDFEncoding),
// cpdf_type1font.cpp (Load/LoadGlyphMap), cpdf_truetypefont.cpp (LoadGlyphMap),
// cpdf_type3font.cpp (Load) and cpdf_cidfont.cpp (Load/LoadMetricsArray)
// @ pdfium 0db284a42.
//
// FACE-LESS port: the glyph_index_ half of every LoadGlyphMap needs a FreeType
// face and is skipped; only the encoding_.SetUnicode population (the Unicode
// source for text extraction) is reproduced.
package font

import "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"

// loadFontDescriptor ports CPDF_Font::LoadFontDescriptor (cpdf_font.cpp:153).
func (f *Font) loadFontDescriptor(desc *objects.Dictionary) {
	f.flags = desc.GetIntegerWithDefaultFor("Flags", fontStyleNonSymbolic)

	bExistItalicAngle := desc.KeyExist("ItalicAngle")
	if desc.GetIntegerFor("ItalicAngle") < 0 {
		f.flags |= fontStyleItalic
	}
	bExistStemV := desc.KeyExist("StemV")
	bExistAscent := false
	if desc.KeyExist("Ascent") {
		f.ascent = desc.GetIntegerFor("Ascent")
		bExistAscent = true
	}
	bExistDescent := false
	if desc.KeyExist("Descent") {
		f.descent = desc.GetIntegerFor("Descent")
		bExistDescent = true
	}
	bExistCapHeight := desc.KeyExist("CapHeight")
	if bExistItalicAngle && bExistAscent && bExistCapHeight && bExistDescent && bExistStemV {
		f.flags |= fxFontUseExternAttr
	}
	if f.descent > 10 {
		f.descent = -f.descent
	}
	if bbox := desc.GetArrayFor("FontBBox"); bbox != nil {
		f.fontBBox.left = bbox.GetIntegerAt(0)
		f.fontBBox.bottom = bbox.GetIntegerAt(1)
		f.fontBBox.right = bbox.GetIntegerAt(2)
		f.fontBBox.top = bbox.GetIntegerAt(3)
	}

	// Embedded font program: record its presence and retain the decoded bytes so
	// GetCharBBox can read per-glyph control boxes. PDFium probes FontFile (Type1)
	// then FontFile2 (TrueType) then FontFile3 (CFF/OpenType); keep that order.
	for _, tag := range []string{"FontFile", "FontFile2", "FontFile3"} {
		s := desc.GetStreamFor(tag)
		if s == nil {
			continue
		}
		f.hasFontFile = true
		f.fontProgramTag = tag
		if tag == "FontFile3" {
			f.fontProgramSubtype = s.GetDict().GetByteStringFor("Subtype")
		}
		acc := objects.NewStreamAcc(s)
		acc.LoadAllDataFiltered()
		f.fontProgram = acc.GetSpan()
		break
	}
}

// loadGB2312Hint is referenced from comments; the GB2312 TrueType-CID demotion
// path is deferred (corpus-rare). See loadCID step 1.

// --- simple-font loading ---

// loadCommon ports CPDF_SimpleFont::LoadCommon (cpdf_simplefont.cpp:245).
func (f *Font) loadCommon() bool {
	f.encoding = newCharEncoding(encBuiltin)
	f.baseEncoding = encBuiltin

	desc := f.fontDict.GetDictFor("FontDescriptor")
	if desc != nil {
		f.loadFontDescriptor(desc)
	}
	f.loadCharWidths(desc)
	if f.hasFontFile {
		f.baseFontName = stripSubsetPrefix(f.baseFontName)
	}
	// LoadSubstFont (substitute selection) is a no-op without a face.
	if !fontStyleIsSymbolic(f.flags) {
		f.baseEncoding = encStandard
	}
	f.loadPDFEncoding(f.hasFontFile, f.kind == kindTrueType)
	f.loadGlyphMap()
	f.resolveTeXEncoding()
	f.charNames = nil
	return true
}

// loadCharWidths ports CPDF_SimpleFont::LoadCharWidths.
func (f *Font) loadCharWidths(desc *objects.Dictionary) {
	arr := f.fontDict.GetArrayFor("Widths")
	f.useFontWidth = arr == nil
	if arr == nil {
		return
	}
	if desc != nil && desc.KeyExist("MissingWidth") {
		mw := uint16(desc.GetIntegerFor("MissingWidth"))
		for i := range f.charWidth {
			f.charWidth[i] = mw
		}
	}
	start := f.fontDict.GetIntegerWithDefaultFor("FirstChar", 0)
	end := f.fontDict.GetIntegerWithDefaultFor("LastChar", 0)
	if start > 255 {
		return
	}
	if end == 0 || end >= start+arr.Len() {
		end = start + arr.Len() - 1
	}
	if end > 255 {
		end = 255
	}
	for i := start; i <= end; i++ {
		f.charWidth[i] = uint16(arr.GetIntegerAt(i - start))
	}
}

// loadDifferences ports CPDF_SimpleFont::LoadDifferences.
func (f *Font) loadDifferences(encoding *objects.Dictionary) {
	diffs := encoding.GetArrayFor("Differences")
	if diffs == nil {
		return
	}
	f.charNames = make([]string, 256)
	curCode := 0
	for i := 0; i < diffs.Len(); i++ {
		element := diffs.GetDirectObjectAt(i)
		if element == nil {
			continue
		}
		if name := objects.ToName(element); name != nil {
			if curCode < 256 {
				f.charNames[curCode] = name.GetString()
			}
			curCode++
		} else {
			curCode = element.GetInteger()
		}
	}
}

// getPredefinedEncoding ports the namespace GetPredefinedEncoding (only
// WinAnsi/MacRoman/MacExpert/PDFDoc are recognised; StandardEncoding is NOT).
func getPredefinedEncoding(value string, basemap *fontEncoding) {
	switch value {
	case "WinAnsiEncoding":
		*basemap = encWinAnsi
	case "MacRomanEncoding":
		*basemap = encMacRoman
	case "MacExpertEncoding":
		*basemap = encMacExpert
	case "PDFDocEncoding":
		*basemap = encPdfDoc
	}
}

// loadPDFEncoding ports CPDF_SimpleFont::LoadPDFEncoding.
func (f *Font) loadPDFEncoding(bEmbedded, bTrueType bool) {
	pEnc := f.fontDict.GetDirectObjectFor("Encoding")
	if pEnc == nil {
		if f.baseFontName == "Symbol" {
			if bTrueType {
				f.baseEncoding = encMsSymbol
			} else {
				f.baseEncoding = encAdobeSymbol
			}
		} else if !bEmbedded && f.baseEncoding == encBuiltin {
			f.baseEncoding = encWinAnsi
		}
		f.rebuildEncoding()
		return
	}
	if name := objects.ToName(pEnc); name != nil {
		if f.baseEncoding == encAdobeSymbol || f.baseEncoding == encZapfDingbats {
			f.rebuildEncoding()
			return
		}
		if fontStyleIsSymbolic(f.flags) && f.baseFontName == "Symbol" {
			if !bTrueType {
				f.baseEncoding = encAdobeSymbol
			}
			f.rebuildEncoding()
			return
		}
		s := name.GetString()
		if s == "MacExpertEncoding" {
			s = "WinAnsiEncoding"
		}
		getPredefinedEncoding(s, &f.baseEncoding)
		f.rebuildEncoding()
		return
	}

	dict := objects.ToDictionary(pEnc)
	if dict == nil {
		f.rebuildEncoding()
		return
	}
	if f.baseEncoding != encAdobeSymbol && f.baseEncoding != encZapfDingbats {
		s := dict.GetByteStringFor("BaseEncoding")
		if bTrueType && s == "MacExpertEncoding" {
			s = "WinAnsiEncoding"
		}
		getPredefinedEncoding(s, &f.baseEncoding)
	}
	if (!bEmbedded || bTrueType) && f.baseEncoding == encBuiltin {
		f.baseEncoding = encStandard
	}
	f.loadDifferences(dict)
	f.rebuildEncoding()
}

// rebuildEncoding seeds encoding_ from the resolved base encoding table. PDFium
// seeds encoding_ via LoadGlyphMap's SetUnicode calls; without a face we instead
// (a) copy the predefined base-encoding unicode table directly, then (b) overlay
// /Differences names via the AGL in loadGlyphMap. The net Unicode result matches
// PDFium's encoding_ for the non-symbolic Latin path.
func (f *Font) rebuildEncoding() {
	if f.encoding == nil {
		f.encoding = newCharEncoding(encBuiltin)
	}
	if src := unicodesForPredefinedCharSet(f.baseEncoding); src != nil {
		for i := range 256 {
			f.encoding.unicodes[i] = rune(src[i])
		}
	}
}

// loadGlyphMap reproduces the encoding_.SetUnicode population of the Type1/
// TrueType LoadGlyphMap, face-independently. For each charcode it resolves the
// Adobe glyph name (/Differences > predefined base encoding) and sets the
// encoding's unicode via the Adobe Glyph List, exactly as PDFium's loops do.
func (f *Font) loadGlyphMap() {
	if f.encoding == nil {
		f.encoding = newCharEncoding(encBuiltin)
	}
	for charcode := range uint32(256) {
		name := getAdobeCharName(f.baseEncoding, f.charNames, charcode)
		if name == "" {
			continue
		}
		// Retain the resolved glyph name for the charcode->GID mapping used by
		// GetCharBBox (charNames is cleared after load).
		f.glyphNames[charcode] = name
		f.encoding.SetUnicode(uint8(charcode), unicodeFromAdobeName(name))
	}
}

// --- Type1 ---

// loadType1 ports CPDF_Type1Font::Load (cpdf_type1font.cpp:96).
func (f *Font) loadType1() bool {
	id, ok := getStandardFontName(&f.baseFontName)
	f.base14 = id
	f.hasBase14 = ok
	if !ok {
		return f.loadCommon()
	}

	f.encoding = newCharEncoding(encBuiltin)
	f.baseEncoding = encBuiltin

	desc := f.fontDict.GetDictFor("FontDescriptor")
	if desc != nil && desc.KeyExist("Flags") {
		f.flags = desc.GetIntegerFor("Flags")
	} else if isSymbolicStandardFont(id) {
		f.flags = fontStyleSymbolic
	} else {
		f.flags = fontStyleNonSymbolic
	}
	if isFixedStandardFont(id) {
		for i := range f.charWidth {
			f.charWidth[i] = 600
		}
	}
	switch {
	case id == stdSymbol:
		f.baseEncoding = encAdobeSymbol
	case id == stdDingbats:
		f.baseEncoding = encZapfDingbats
	case fontStyleIsNonSymbolic(f.flags):
		f.baseEncoding = encStandard
	}
	return f.loadCommonBase14()
}

// loadCommonBase14 is LoadCommon but preserving the Base-14 width/encoding
// defaults that loadType1 set (it does not re-zero baseEncoding/charWidth).
func (f *Font) loadCommonBase14() bool {
	desc := f.fontDict.GetDictFor("FontDescriptor")
	if desc != nil {
		f.loadFontDescriptor(desc)
	}
	f.loadCharWidths(desc)
	if f.hasFontFile {
		f.baseFontName = stripSubsetPrefix(f.baseFontName)
	}
	if !fontStyleIsSymbolic(f.flags) {
		f.baseEncoding = encStandard
	}
	f.loadPDFEncoding(f.hasFontFile, false)
	f.loadGlyphMap()
	f.resolveTeXEncoding()
	f.charNames = nil
	f.loadBase14Metrics()
	return true
}

// loadBase14Metrics supplies the built-in Standard-14 AFM metrics for glyphs
// whose advance width is otherwise unknown. A conforming reader must have
// metrics for the standard fonts even when the PDF omits /Widths (ISO 32000-1
// 9.6.2.2, PDF 1.0-1.4 practice); PDFium satisfies this through the substitute
// face, which this face-less port lacks. Only sentinel (unset) charWidth
// entries are filled, so an explicit /Widths array, /MissingWidth default, the
// Courier fixed-pitch prefill, and embedded-program metrics all keep
// precedence. The AFM Ascender/Descender/FontBBox likewise only backfill a
// descriptor that supplied none, giving GetCharBBox a real vertical extent
// instead of the 0.8em/-0.2em face-less default.
func (f *Font) loadBase14Metrics() {
	if !f.hasBase14 || f.hasFontFile {
		return
	}
	if widths := base14Widths(f.base14); widths != nil {
		for code := range f.charWidth {
			if f.charWidth[code] != 0xffff {
				continue
			}
			name := f.glyphNames[code]
			if name == "" {
				continue
			}
			if w, ok := widths[name]; ok {
				f.charWidth[code] = w
			}
		}
	}
	vm := base14VerticalMetrics[f.base14]
	if f.ascent == 0 && f.descent == 0 {
		f.ascent = vm.ascender
		f.descent = vm.descender
	}
	if f.fontBBox == (rect{}) {
		f.fontBBox = rect{
			left: vm.bbox[0], bottom: vm.bbox[1],
			right: vm.bbox[2], top: vm.bbox[3],
		}
	}
}

// --- Type3 ---

// loadType3 ports CPDF_Type3Font::Load (cpdf_type3font.cpp:62) — the widths and
// encoding portion. The /CharProcs glyph rasterisation is out of scope.
func (f *Font) loadType3() bool {
	f.encoding = newCharEncoding(encBuiltin)
	f.baseEncoding = encBuiltin

	matrix := f.fontDict.GetArrayFor("FontMatrix")
	var xscale float32 = 1.0
	var yscale float32 = 1.0
	if matrix != nil && matrix.Len() == 6 {
		xscale = matrix.GetFloatAt(0)
		yscale = matrix.GetFloatAt(3)
	}

	// CPDF_Type3Font::Load scales the font-dict /FontBBox by the FontMatrix
	// diagonal and converts to 1000-unit glyph space (TextUnitRectToGlyphUnit-
	// Rect). GetCharBBox falls back to this vertical extent when the char
	// program is not rasterised, so without it every Type 3 glyph degenerates
	// to a baseline sliver. Stored normalised (bottom <= top): a y-flipping
	// FontMatrix (dvips emits [1 0 0 -1 0 0]) inverts the raw corners.
	if bbox := f.fontDict.GetArrayFor("FontBBox"); bbox != nil && bbox.Len() == 4 {
		x0 := bbox.GetFloatAt(0) * xscale * 1000
		y0 := bbox.GetFloatAt(1) * yscale * 1000
		x1 := bbox.GetFloatAt(2) * xscale * 1000
		y1 := bbox.GetFloatAt(3) * yscale * 1000
		f.fontBBox = rect{
			left:   roundf(min(x0, x1)),
			bottom: roundf(min(y0, y1)),
			right:  roundf(max(x0, x1)),
			top:    roundf(max(y0, y1)),
		}
	}

	start := f.fontDict.GetIntegerFor("FirstChar")
	if start >= 0 && start < len(f.type3CharWidth) {
		widths := f.fontDict.GetArrayFor("Widths")
		if widths != nil {
			count := min(widths.Len(), len(f.type3CharWidth), len(f.type3CharWidth)-start)
			for i := range count {
				// TextUnitToGlyphUnit multiplies by 1000 (the glyph-unit scale).
				w := widths.GetFloatAt(i) * xscale
				f.type3CharWidth[start+i] = roundf(w * 1000.0)
			}
		}
	}
	if f.fontDict.GetDirectObjectFor("Encoding") != nil {
		f.loadPDFEncoding(false, false)
	}
	f.resolveTeXEncoding()
	return true
}

// roundf mirrors FXSYS_roundf (round half away from zero).
func roundf(v float32) int {
	if v >= 0 {
		return int(v + 0.5)
	}
	return int(v - 0.5)
}

// --- CID ---

// loadCID ports CPDF_CIDFont::Load (cpdf_cidfont.cpp:425) — the extraction subset.
func (f *Font) loadCID() bool {
	f.charset = cidSetUnknown
	f.defaultWidth = 1000

	if f.fontDict.GetByteStringFor("Subtype") == "TrueType" {
		// LoadGB2312: predefined GBK-EUC-H CMap + GB1 tables. Deferred (the
		// tables are not compiled in); treat as a load success with a degenerate
		// CMap so width/Unicode fall through to defaults / ToUnicode.
		// TODO(plan 009): LoadGB2312 predefined CMap + GB1 CID2Unicode tables.
		f.baseFontName = f.fontDict.GetByteStringFor("BaseFont")
		f.charset = cidSetGB1
		f.cmap = newCMapPredefined("GBK-EUC-H")
		f.cid2unicodeMap = getCID2UnicodeMap(f.charset)
		f.ansiWidthsFixed = true
		return true
	}

	fonts := f.fontDict.GetArrayFor("DescendantFonts")
	if fonts == nil || fonts.Len() != 1 {
		return false
	}
	cidFontDict := fonts.GetDictAt(0)
	if cidFontDict == nil {
		return false
	}

	f.baseFontName = cidFontDict.GetByteStringFor("BaseFont")
	// adobeCourierStd glyph fallback is a rendering concern; skipped.

	pEncoding := f.fontDict.GetDirectObjectFor("Encoding")
	if pEncoding == nil {
		return false
	}

	// CIDFontType0 -> Type1; else TrueType (only affects glyph rendering).

	encName := objects.ToName(pEncoding)
	encStream := objects.ToStream(pEncoding)
	if encName == nil && encStream == nil {
		return false
	}

	if encStream != nil {
		acc := objects.NewStreamAcc(encStream)
		acc.LoadAllDataFiltered()
		f.cmap = newCMapEmbedded(acc.GetSpan())
	} else {
		f.cmap = newCMapPredefined(encName.GetString())
	}

	if desc := cidFontDict.GetDictFor("FontDescriptor"); desc != nil {
		f.loadFontDescriptor(desc)
	}

	f.charset = f.cmap.GetCharset()
	if f.charset == cidSetUnknown {
		if info := cidFontDict.GetDictFor("CIDSystemInfo"); info != nil {
			f.charset = charsetFromOrdering(info.GetByteStringFor("Ordering"))
		}
	}
	if f.charset != cidSetUnknown {
		f.cid2unicodeMap = getCID2UnicodeMap(f.charset)
	}

	f.defaultWidth = cidFontDict.GetIntegerWithDefaultFor("DW", 1000)
	if w := cidFontDict.GetArrayFor("W"); w != nil {
		loadMetricsArray(w, &f.widthList, 1)
	}

	// CIDToGIDMap (CIDFontType2 only): a /CIDToGIDMap stream gives CID->GID as a
	// big-endian uint16 table; the name "Identity" (or absent, and CIDFontType0C)
	// means GID==CID. Retain the decoded stream for glyphIndex.
	if obj := cidFontDict.GetDirectObjectFor("CIDToGIDMap"); obj != nil {
		if s := objects.ToStream(obj); s != nil {
			acc := objects.NewStreamAcc(s)
			acc.LoadAllDataFiltered()
			f.cidGIDMap = acc.GetSpan()
		}
	}

	// CIDToGIDMap, CheckFontMetrics, vertical /W2,/DW2 and SetFontType are
	// rendering concerns; skipped for extraction.
	return true
}

// loadMetricsArray ports the namespace LoadMetricsArray (cpdf_cidfont.cpp:227):
// the /W (and /W2) flattener into (low, high, vals...) records.
func loadMetricsArray(arr *objects.Array, result *[]int, nElements int) {
	widthStatus := 0
	iCurElement := 0
	firstCode := 0
	lastCode := 0
	for i := 0; i < arr.Len(); i++ {
		obj := arr.GetDirectObjectAt(i)
		if obj == nil {
			continue
		}
		if objArray := objects.ToArray(obj); objArray != nil {
			if widthStatus != 1 {
				return
			}
			const maxInt = int(^uint(0) >> 1)
			if firstCode > maxInt-objArray.Len() {
				widthStatus = 0
				continue
			}
			for j := 0; j < objArray.Len(); j += nElements {
				*result = append(*result, firstCode, firstCode)
				for k := range nElements {
					*result = append(*result, objArray.GetIntegerAt(j+k))
				}
				firstCode++
			}
			widthStatus = 0
		} else {
			switch widthStatus {
			case 0:
				firstCode = obj.GetInteger()
				widthStatus = 1
			case 1:
				lastCode = obj.GetInteger()
				widthStatus = 2
				iCurElement = 0
			default:
				if iCurElement == 0 {
					*result = append(*result, firstCode, lastCode)
				}
				*result = append(*result, obj.GetInteger())
				iCurElement++
				if iCurElement == nElements {
					widthStatus = 0
				}
			}
		}
	}
}

// stripSubsetPrefix drops the six-character subset tag from an embedded
// font's /BaseFont, mirroring CPDF_Font::LoadCommon: any six bytes before a
// '+' count (unlike the stricter uppercase-only stripSubsetTag used for
// display names).
func stripSubsetPrefix(name string) string {
	if len(name) > 7 && name[6] == '+' {
		return name[7:]
	}
	return name
}
