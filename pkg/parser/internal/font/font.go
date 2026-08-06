// Package font ports PDFium's text-extraction font layer from
// core/fpdfapi/font/ @ pdfium 0db284a42: CPDF_Font / CPDF_SimpleFont /
// CPDF_Type1Font / CPDF_TrueTypeFont / CPDF_CIDFont / CPDF_Type3Font, plus the
// encoding, CMap and ToUnicode machinery.
//
// This is a pure-Go, FACE-LESS port: there is no FreeType binding, so glyph
// indices and char bounding boxes (which need a rasterised face) are NOT
// computed. Unicode is derived from /ToUnicode (preferred) and the predefined
// encoding tables / Adobe Glyph List (fallback); widths come from /Widths
// (simple) or /W,/DW (CID). The encoding_.SetUnicode population in PDFium's
// LoadGlyphMap is reproduced face-independently for the Unicode side, which is
// all text extraction needs (see cpdf_simplefont.cpp UnicodeFromCharCode).
package font

import (
	"slices"
	"unicode/utf16"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// FontStyle flag constants (core/fxge/fx_font.h:29); only the flags this port
// consults are kept.
const (
	fontStyleSymbolic    = 1 << 2
	fontStyleNonSymbolic = 1 << 5
	fontStyleItalic      = 1 << 6
	fontStyleForceBold   = 1 << 18
	fxFontUseExternAttr  = 0x80000
)

func fontStyleIsSymbolic(s int) bool    { return s&fontStyleSymbolic != 0 }
func fontStyleIsNonSymbolic(s int) bool { return s&fontStyleNonSymbolic != 0 }

// fontKind discriminates the concrete PDFium font subclass.
type fontKind int

const (
	kindType1 fontKind = iota
	kindTrueType
	kindType3
	kindCID
)

// rect is FX_RECT (integer device-independent rect).
type rect struct{ left, top, right, bottom int }

// Font is the concrete font type exposed by this package. It folds CPDF_Font and
// its subclasses into one struct gated on kind; the public API is stable across
// kinds.
type Font struct {
	kind     fontKind
	fontDict *objects.Dictionary
	holder   objects.IndirectObjectHolder

	baseFontName string

	// ToUnicode (owned by the base CPDF_Font).
	toUnicodeMap    *toUnicodeMap
	toUnicodeLoaded bool

	// unicodeCache memoizes UnicodeStringFromCharCode: text extraction maps the
	// same handful of char codes millions of times, and each uncached lookup
	// allocates (utf16.Decode + string). Single-threaded, so a plain map is safe.
	unicodeCache map[uint32]string
	// widthCache memoizes the CID-font width (a linear /W-triple scan per call).
	widthCache map[uint32]float32
	// charBBoxCache memoizes GetCharBBox results after embedded glyph lookup.
	charBBoxCache map[uint32][4]int

	// descriptor-derived state.
	flags       int
	ascent      int
	descent     int
	fontBBox    rect
	hasFontFile bool

	// Embedded font program (for per-glyph GetCharBBox control boxes). The
	// glyph source is parsed lazily on first GetCharBBox, since text extraction
	// of a font-free page never needs it.
	fontProgram        []byte
	fontProgramTag     string // FontFile | FontFile2 | FontFile3
	fontProgramSubtype string // FontFile3 /Subtype: Type1C | CIDFontType0C | OpenType
	glyphSrc           glyphSource
	glyphSrcReady      bool
	glyphNames         [256]string // simple-font charcode -> Adobe glyph name
	glyphIndices       [256]uint16 // simple-font charcode -> GID (0xffff = none)
	glyphIndicesReady  bool
	cidGIDMap          []byte            // CIDFontType2 /CIDToGIDMap stream (nil = Identity)
	cidGIDCache        map[uint32]uint16 // CID-font charcode -> GID

	// --- simple-font (Type1/TrueType/Type3) state ---
	encoding     *charEncoding
	baseEncoding fontEncoding
	useFontWidth bool
	charNames    []string // /Differences, len 0 or 256
	charWidth    [256]uint16
	// Type3 widths (already scaled to glyph units).
	type3CharWidth [256]int

	// --- Type1 Base-14 ---
	base14    standardFont
	hasBase14 bool

	// texUnicode is the TeX-encoding fallback table resolved at load time for
	// fonts with no /ToUnicode and only synthetic glyph names (dvips Computer
	// Modern output); nil when no encoding was identified. See texencoding.go.
	texUnicode *[256]rune

	// --- CID-font state ---
	cmap            *cmap
	cid2unicodeMap  *cid2UnicodeMap
	charset         cidSet
	defaultWidth    int
	widthList       []int // flattened /W triples (low, high, val)
	ansiWidthsFixed bool
}

// Load builds a Font from a font dictionary, resolving indirect objects via
// holder. It ports CPDF_Font::Create dispatch (cpdf_font.cpp:309). Returns nil if
// the font cannot be loaded.
func Load(fontDict *objects.Dictionary, holder objects.IndirectObjectHolder) *Font {
	if fontDict == nil {
		return nil
	}
	f := &Font{
		fontDict:     fontDict,
		holder:       holder,
		baseFontName: fontDict.GetByteStringFor("BaseFont"),
	}
	for i := range f.charWidth {
		f.charWidth[i] = 0xffff
	}

	typ := fontDict.GetByteStringFor("Subtype")
	switch typ {
	case "TrueType":
		tag := first4(f.baseFontName)
		demoted := false
		if slices.Contains(kChineseFontNames, tag) {
			desc := fontDict.GetDictFor("FontDescriptor")
			if desc == nil || !desc.KeyExist("FontFile2") {
				f.kind = kindCID
				demoted = true
			}
		}
		if !demoted {
			f.kind = kindTrueType
		}
	case "Type3":
		f.kind = kindType3
	case "Type0":
		f.kind = kindCID
	default:
		f.kind = kindType1
	}

	if !f.load() {
		return nil
	}
	return f
}

// kChineseFontNames ports the GBK-encoded Chinese TrueType names whose presence
// (without FontFile2) demotes a /TrueType to a CIDFont.
var kChineseFontNames = []string{
	"\xCB\xCE\xCC\xE5",
	"\xBF\xAC\xCC\xE5",
	"\xBA\xDA\xCC\xE5",
	"\xB7\xC2\xCB\xCE",
	"\xD0\xC2\xCB\xCE",
}

func first4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[:4]
}

func (f *Font) load() bool {
	switch f.kind {
	case kindCID:
		return f.loadCID()
	case kindType3:
		return f.loadType3()
	case kindType1:
		return f.loadType1()
	default: // kindTrueType
		return f.loadCommon()
	}
}

// --- public API ---

// IsCID reports whether this is a Type0/CID font.
func (f *Font) IsCID() bool { return f.kind == kindCID }

// IsType3 reports whether this is a Type3 font.
func (f *Font) IsType3() bool { return f.kind == kindType3 }

// IsVertWriting reports vertical writing mode (CMap WMode==1).
func (f *Font) IsVertWriting() bool {
	if f.kind == kindCID {
		return f.cmap != nil && f.cmap.IsVertWriting()
	}
	return false
}

// NextChar decodes ONE character code starting at offset, returning the code and
// the offset just past it. Simple fonts consume 1 byte; CID/Type0 fonts consume
// the bytes the CMap codespace dictates (Identity-H = 2 bytes).
// Ports CPDF_Font::GetNextChar / CPDF_CIDFont::GetNextChar.
func (f *Font) NextChar(str []byte, offset int) (charCode uint32, nextOffset int) {
	if f.kind == kindCID {
		if f.cmap == nil {
			// Degenerate: behave like a single-byte font.
			return f.nextCharSimple(str, offset)
		}
		return f.cmap.GetNextChar(str, offset)
	}
	return f.nextCharSimple(str, offset)
}

// nextCharSimple ports CPDF_Font::GetNextChar (single-byte): returns str[offset]
// or, at end, str[len-1] (Back()).
func (f *Font) nextCharSimple(str []byte, offset int) (uint32, int) {
	if len(str) == 0 {
		return 0, offset
	}
	if offset < len(str) {
		b := str[offset]
		return uint32(b), offset + 1
	}
	return uint32(str[len(str)-1]), offset + 1
}

// GetCharWidthF returns the glyph advance width in 1000-unit text space, 0 if
// unknown. Ports CPDF_SimpleFont/CPDF_CIDFont/CPDF_Type3Font GetCharWidthF.
func (f *Font) GetCharWidthF(charCode uint32) float32 {
	switch f.kind {
	case kindCID:
		if f.widthCache == nil {
			f.widthCache = make(map[uint32]float32)
		} else if w, ok := f.widthCache[charCode]; ok {
			return w
		}
		w := float32(f.cidCharWidth(charCode))
		f.widthCache[charCode] = w
		return w
	case kindType3:
		cc := charCode
		if cc >= uint32(len(f.type3CharWidth)) {
			cc = 0
		}
		return float32(f.type3CharWidth[cc])
	default:
		return float32(f.simpleCharWidth(charCode))
	}
}

// simpleCharWidth ports CPDF_SimpleFont::GetCharWidthF (without the face-backed
// LoadCharMetrics gap-filling, which needs a glyph program).
func (f *Font) simpleCharWidth(charcode uint32) int {
	if charcode > 0xff {
		charcode = 0
	}
	if f.charWidth[charcode] == 0xffff {
		if f.useFontWidth {
			if width, ok := f.glyphAdvanceWidth(charcode); ok && width > 0 {
				if width > 0xfffe {
					width = 0xfffe
				}
				f.charWidth[charcode] = uint16(width)
				return width
			}
		}
		// No /Widths entry and no embedded metric fallback -> 0.
		f.charWidth[charcode] = 0
	}
	return int(f.charWidth[charcode])
}

// cidCharWidth ports CPDF_CIDFont::GetCharWidthF.
func (f *Font) cidCharWidth(charcode uint32) int {
	if charcode < 0x80 && f.ansiWidthsFixed {
		if charcode >= 32 && charcode < 127 {
			return 500
		}
		return 0
	}
	cid := f.CIDFromCharCode(charcode)
	// widthList is flattened triples (low, high, val).
	for i := 0; i+3 <= len(f.widthList); i += 3 {
		low := f.widthList[i]
		high := f.widthList[i+1]
		val := f.widthList[i+2]
		if low <= int(cid) && int(cid) <= high {
			return val
		}
	}
	return f.defaultWidth
}

// CIDFromCharCode ports CPDF_CIDFont::CIDFromCharCode.
func (f *Font) CIDFromCharCode(charcode uint32) uint16 {
	if f.cmap != nil {
		return f.cmap.CIDFromCharCode(charcode)
	}
	return uint16(charcode)
}

// UnicodeFromCharCode returns the Unicode scalar (rune) for charCode: ToUnicode
// map first, then the encoding (simple) or CID2Unicode (CID); 0 if none.
// PDFium returns a WideString ([]uint16); here we return the first scalar value
// (decoding a surrogate pair) which is what a rune-based caller expects.
func (f *Font) UnicodeFromCharCode(charCode uint32) rune {
	// Route through the cache and return the first rune without allocating a
	// []rune (the common case is a single rune).
	for _, r := range f.UnicodeStringFromCharCode(charCode) {
		return r
	}
	return 0
}

// UnicodeStringFromCharCode returns the full WideString (as a Go string) for a
// char code, decoding surrogate pairs and keeping multi-char ligatures. This is
// the faithful PDFium WideString result; UnicodeFromCharCode returns its first
// scalar for the rune-based API. Results are memoized per char code.
func (f *Font) UnicodeStringFromCharCode(charCode uint32) string {
	if f.unicodeCache == nil {
		f.unicodeCache = make(map[uint32]string)
	} else if s, ok := f.unicodeCache[charCode]; ok {
		return s
	}
	var s string
	if units := f.unicodeUnits(charCode); len(units) != 0 {
		s = string(utf16.Decode(units))
	}
	f.unicodeCache[charCode] = s
	return s
}

// unicodeUnits returns the []uint16 WideString for a char code following the
// PDFium subclass precedence.
func (f *Font) unicodeUnits(charcode uint32) []uint16 {
	switch f.kind {
	case kindCID:
		// CPDF_CIDFont::UnicodeFromCharCode: ToUnicode (base) first.
		if u := f.baseUnicodeUnits(charcode); len(u) > 0 {
			return u
		}
		ret := f.cidUnicodeFromCharCode(charcode)
		if ret != 0 {
			return []uint16{ret}
		}
		return nil
	case kindType3:
		// Type3: ToUnicode, then the TeX-encoding fallback (a Type 3 font has
		// no face/encoding text source, so without ToUnicode its codes would
		// otherwise yield nothing).
		if u := f.baseUnicodeUnits(charcode); len(u) > 0 {
			return u
		}
		if f.texUnicode != nil && charcode < 256 {
			if r := f.texUnicode[charcode]; r != 0 {
				return utf16.Encode([]rune{r})
			}
		}
		return nil
	default:
		// CPDF_SimpleFont::UnicodeFromCharCode: ToUnicode then encoding_.
		if u := f.baseUnicodeUnits(charcode); len(u) > 0 {
			return u
		}
		var ret rune
		if f.encoding != nil {
			ret = f.encoding.UnicodeFromCharCode(uint8(charcode))
		}
		// TeX fallback: only when the encoding path produced nothing usable
		// (zero or a control scalar); a successful mapping is never
		// overridden.
		if f.texUnicode != nil && charcode < 256 && (ret == 0 || ret < 0x20) {
			if r := f.texUnicode[charcode]; r != 0 {
				return utf16.Encode([]rune{r})
			}
		}
		if ret != 0 {
			return utf16.Encode([]rune{ret})
		}
		return nil
	}
}

// baseUnicodeUnits ports CPDF_Font::UnicodeFromCharCode (the ToUnicode lookup).
func (f *Font) baseUnicodeUnits(charcode uint32) []uint16 {
	if !f.toUnicodeLoaded {
		f.loadUnicodeMap()
	}
	if f.toUnicodeMap == nil {
		return nil
	}
	return f.toUnicodeMap.Lookup(charcode)
}

// loadUnicodeMap ports CPDF_Font::LoadUnicodeMap.
func (f *Font) loadUnicodeMap() {
	f.toUnicodeLoaded = true
	s := f.fontDict.GetStreamFor("ToUnicode")
	if s == nil {
		return
	}
	f.toUnicodeMap = newToUnicodeMap(s)
}

// cidUnicodeFromCharCode ports CPDF_CIDFont::GetUnicodeFromCharCode. For the
// corpus (Identity-H, no compiled CJK tables) this returns 0.
func (f *Font) cidUnicodeFromCharCode(charcode uint32) uint16 {
	if f.cmap == nil {
		return 0
	}
	switch f.cmap.GetCoding() {
	case cidCodingUCS2, cidCodingUTF16:
		return uint16(charcode)
	case cidCodingCID:
		if f.cid2unicodeMap == nil || !f.cid2unicodeMap.IsLoaded() {
			return 0
		}
		return f.cid2unicodeMap.UnicodeFromCID(uint16(charcode))
	}
	if f.cid2unicodeMap != nil && f.cid2unicodeMap.IsLoaded() && f.cmap.IsLoaded() {
		return f.cid2unicodeMap.UnicodeFromCID(f.CIDFromCharCode(charcode))
	}
	// non-Windows: embedMap is nil for the corpus.
	// TODO(plan 009): predefined CJK embedded CMap -> EmbeddedUnicodeFromCharcode.
	return 0
}

// BaseFontName returns the base font name with any subset prefix removed.
// PDF subset fonts are named "XXXXXX+RealName" (6 uppercase tag chars + '+');
// stripping the prefix yields the real PostScript name used for bold/italic
// detection.
func (f *Font) BaseFontName() string { return stripSubsetTag(f.baseFontName) }

// stripSubsetTag removes the "XXXXXX+" subset prefix if present.
func stripSubsetTag(name string) string {
	if len(name) < 8 || name[6] != '+' {
		return name
	}
	for i := range 6 {
		c := name[i]
		if !(c >= 'A' && c <= 'Z') {
			return name
		}
	}
	return name[7:]
}

// Flags returns the PDF font-descriptor flags (PDF spec §5.7.1). Bit 6
// (value 64) is the Italic flag; bit 1 (value 1) is FixedPitch. These are
// the faithful source for italic/monospace detection (vs. font-name
// heuristics). Returns 0 when no /FontDescriptor is present.
func (f *Font) Flags() int { return f.flags }

// FontWeight returns an OpenType-style weight (100-900). It consults the
// reliable, descriptor- and program-level signals FIRST, and only falls back
// to the base-font NAME heuristic when none of them resolve:
//
//	a. /FontDescriptor /Flags ForceBold bit (bit 19, 0x40000) -> 700.
//	b. Embedded Type1 /FontInfo /Weight string (e.g. CMBX10's "Bold") -> mapped.
//	c. Base-font NAME weight token (PostScript convention) -> mapped.
//
// (a) and (b) catch bold fonts whose NAME carries no weight token — most notably
// LaTeX's Computer Modern Bold (CMBX10), which is bold but whose PostScript
// name has no "Bold" substring. Returns 0 when nothing indicates a weight
// (callers fall back to regular).
func (f *Font) FontWeight() int {
	// (a) FontDescriptor ForceBold flag.
	if f.flags&fontStyleForceBold != 0 {
		return 700
	}
	// (b) Embedded Type1 /FontInfo /Weight. Only Type1 programs (FontFile)
	// carry a PS /Weight; restrict the (lazy) glyph-source build to that case so
	// non-Type1 fonts never parse their program just for a weight probe.
	if f.fontProgramTag == "FontFile" {
		if src, ok := f.ensureGlyphSource().(psWeightProvider); ok {
			if w, ok := src.psWeight(); ok {
				if weight := psWeightToOpenType(w); weight != 0 {
					return weight
				}
			}
		}
	}
	// NB: a /StemV >= 120 absolute backstop was evaluated and REMOVED. StemV is
	// foundry-relative, so an absolute threshold misfires badly — on a real
	// corpus sample it flagged whole Regular fonts (Montserrat-Regular,
	// PPNeueMachina-Regular) and Computer Modern MATH fonts (CMSY10/CMEX10, whose
	// heavy symbol stems exceed 120) as bold. ForceBold (a) and the embedded
	// Type1 /Weight (b) are the reliable name-independent signals (they correctly
	// catch CMBX without the false positives). A relative "bolder than the body
	// font" StemV signal belongs at the document layer, not here.
	// (c) Base-font NAME heuristic (preserves prior behaviour).
	return fontWeightFromName(f.baseFontName)
}

// fontWeightFromName maps a PostScript /BaseFont name's weight token to an
// OpenType weight (100-900), 0 when no token is present.
func fontWeightFromName(name string) int {
	lower := lowerASCII(name)
	switch {
	case containsFold(lower, "black"), containsFold(lower, "heavy"):
		return 900
	case containsFold(lower, "extrabold"), containsFold(lower, "ultrabold"):
		return 800
	case containsFold(lower, "bold"), containsFold(lower, "semibold"), containsFold(lower, "demi"):
		return 700
	case containsFold(lower, "medium"):
		return 500
	case containsFold(lower, "light"), containsFold(lower, "thin"), containsFold(lower, "extralight"):
		return 200
	default:
		return 0
	}
}

// psWeightToOpenType maps an embedded Type1 /FontInfo /Weight string to an
// OpenType weight (100-900), 0 when the string carries no recognised token.
// The match is case-insensitive substring, mirroring fontWeightFromName so the
// same token vocabulary resolves consistently regardless of source.
func psWeightToOpenType(weight string) int {
	lower := lowerASCII(weight)
	switch {
	case containsFold(lower, "black"), containsFold(lower, "heavy"):
		return 900
	case containsFold(lower, "semibold"), containsFold(lower, "demibold"), containsFold(lower, "demi"):
		return 600
	case containsFold(lower, "bold"):
		return 700
	case containsFold(lower, "medium"):
		return 500
	case containsFold(lower, "light"), containsFold(lower, "thin"):
		return 300
	default:
		return 0
	}
}

// IsEmbedded reports whether the font has an embedded font-file stream.
func (f *Font) IsEmbedded() bool { return f.hasFontFile }

// lowerASCII lowercases ASCII letters in s (PostScript font names are ASCII).
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// containsFold reports whether s contains sub (case-insensitive, ASCII).
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
