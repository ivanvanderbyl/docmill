// Glyph-outline readers backing the per-glyph GetCharBBox. PDFium computes a
// glyph's bounding box by loading its outline through FreeType
// (FT_Load_Glyph + FT_Outline_Get_CBox) and scaling the control box to 1000-unit
// glyph space. This file reproduces that with pure-Go font-program parsers, one
// per embedded-program flavour, behind a single glyphSource interface:
//
//   - FontFile2 (TrueType glyf, incl. CIDFontType2)      -> x/image/font/sfnt
//   - FontFile3 OpenType (sfnt-wrapped CFF)              -> x/image/font/sfnt
//   - FontFile3 Type1C / CIDFontType0C (bare CFF)        -> go-text/typesetting cff
//   - FontFile  (Type1 PFB/PFA, eexec)                   -> benoitkugler/textlayout type1
//
// Every reader returns the glyph CONTROL box (min/max over all on- AND off-curve
// Bézier control points) in font units with Y increasing UP, matching
// FT_Outline_Get_CBox. The caller (GetCharBBox) scales font units -> 1000-unit
// glyph space via tt2pdf. A program flavour the readers cannot parse yields a nil
// source, and GetCharBBox falls back to the descriptor ascent/descent box (which
// is exactly what PDFium does when no glyph program is available).
package font

import (
	"bytes"
	"encoding/binary"
	"sort"

	"github.com/benoitkugler/textlayout/fonts"
	t1 "github.com/benoitkugler/textlayout/fonts/type1"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/cff"
	cffot "github.com/go-text/typesetting/font/opentype"
	cfftables "github.com/go-text/typesetting/font/opentype/tables"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// glyphBox is a glyph control box in font units, Y-up (yMax >= yMin).
type glyphBox struct{ xMin, yMin, xMax, yMax int }

func (b glyphBox) empty() bool { return b.xMin == b.xMax && b.yMin == b.yMax }

// glyphSource yields per-glyph control boxes from an embedded font program. All
// lookups are single-threaded (one Font is used by one textpage at a time).
type glyphSource interface {
	unitsPerEm() int
	numGlyphs() int
	// advanceWidth returns the glyph advance in font units when the embedded
	// program exposes it. Missing advances fall back to control-box width.
	advanceWidth(gid uint16) (int, bool)
	// controlBox returns the glyph's control box in font units (Y-up); ok=false
	// for an empty glyph (PDFium treats that as a degenerate/skipped box) or an
	// out-of-range GID.
	controlBox(gid uint16) (glyphBox, bool)
	// gidForName maps a glyph name -> GID (CFF/Type1 charset, TrueType post).
	gidForName(name string) (uint16, bool)
	// gidForRune maps a Unicode scalar -> GID via the program's own cmap
	// (TrueType) or builtin encoding (Type1); ok=false if unavailable.
	gidForRune(r rune) (uint16, bool)
}

// psWeightProvider is an OPTIONAL capability some glyph sources expose: the
// embedded program's PostScript /FontInfo /Weight string (e.g. "Bold",
// "Medium"). Only Type1 programs carry this; FontWeight() consults it to detect
// bold fonts whose PostScript NAME lacks a weight token (e.g. CMBX10 = Computer
// Modern Bold, which embeds /Weight (Bold)).
type psWeightProvider interface {
	// psWeight returns the embedded /FontInfo /Weight string and whether it is
	// present (non-empty).
	psWeight() (string, bool)
}

// newGlyphSource builds a reader for an embedded font program. tag is the
// descriptor key the stream came from (FontFile/FontFile2/FontFile3); subtype is
// the FontFile3 /Subtype (Type1C, CIDFontType0C, OpenType) or "". Returns nil if
// the program cannot be parsed (caller falls back to the descriptor box).
func newGlyphSource(tag, subtype string, data []byte) glyphSource {
	if len(data) == 0 {
		return nil
	}
	switch tag {
	case "FontFile2":
		if s, ok := newSfntGlyphSource(data); ok {
			return s
		}
	case "FontFile3":
		if subtype == "OpenType" {
			if s, ok := newSfntGlyphSource(data); ok {
				return s
			}
		}
		if s, ok := newCFFGlyphSource(data); ok {
			return s
		}
		// A mislabelled OTTO-wrapped program still parses via sfnt.
		if s, ok := newSfntGlyphSource(data); ok {
			return s
		}
	case "FontFile":
		if s, ok := newType1GlyphSource(data); ok {
			return s
		}
	}
	return nil
}

// --- TrueType / OpenType (x/image/font/sfnt) ---

type sfntGlyphSource struct {
	f     *sfnt.Font
	upm   int
	ng    int
	ppem  fixed.Int26_6
	buf   sfnt.Buffer
	names map[string]uint16
	cmap  gtfont.Cmap // lenient go-text cmap (rune -> GID); nil if absent/bad
}

func newSfntGlyphSource(data []byte) (*sfntGlyphSource, bool) {
	f, err := sfnt.Parse(data)
	if err != nil {
		// Many embedded TrueType subsets carry a malformed cmap/post that sfnt
		// rejects wholesale even though glyf/loca/head/maxp are fine. Rebuild a
		// minimal program with only the outline tables and reparse — FreeType
		// (PDFium) is similarly lenient.
		if san, ok := sanitizeTrueType(data); ok {
			f, err = sfnt.Parse(san)
		}
		if err != nil {
			return nil, false
		}
	}
	upm := int(f.UnitsPerEm())
	if upm <= 0 {
		return nil, false
	}
	s := &sfntGlyphSource{f: f, upm: upm, ng: f.NumGlyphs(), ppem: fixed.I(upm)}
	// Parse the original cmap leniently for charcode->GID (sfnt's own cmap is
	// gone if we sanitised, and may have been rejected in the first place).
	s.cmap = parseLenientCmap(data)
	return s, true
}

func (s *sfntGlyphSource) unitsPerEm() int { return s.upm }
func (s *sfntGlyphSource) numGlyphs() int  { return s.ng }

func (s *sfntGlyphSource) advanceWidth(gid uint16) (int, bool) {
	if int(gid) >= s.ng {
		return 0, false
	}
	advance, err := s.f.GlyphAdvance(&s.buf, sfnt.GlyphIndex(gid), s.ppem, xfont.HintingNone)
	if err != nil {
		return 0, false
	}
	width := round26(advance)
	if width < 0 {
		width = -width
	}
	return width, true
}

func (s *sfntGlyphSource) controlBox(gid uint16) (glyphBox, bool) {
	if int(gid) >= s.ng {
		return glyphBox{}, false
	}
	segs, err := s.f.LoadGlyph(&s.buf, sfnt.GlyphIndex(gid), s.ppem, nil)
	if err != nil || len(segs) == 0 {
		return glyphBox{}, false
	}
	b := segs.Bounds() // 26.6 font units, Y increasing DOWN
	// Flip Y to Y-up: top = -min.Y, bottom = -max.Y.
	box := glyphBox{
		xMin: round26(b.Min.X),
		yMin: -round26(b.Max.Y),
		xMax: round26(b.Max.X),
		yMax: -round26(b.Min.Y),
	}
	if box.empty() {
		return glyphBox{}, false
	}
	return box, true
}

func (s *sfntGlyphSource) gidForRune(r rune) (uint16, bool) {
	if s.cmap != nil {
		if g, ok := s.cmap.Lookup(r); ok && g != 0 {
			return uint16(g), true
		}
	}
	gi, err := s.f.GlyphIndex(&s.buf, r)
	if err != nil || gi == 0 {
		return 0, false
	}
	return uint16(gi), true
}

func (s *sfntGlyphSource) gidForName(name string) (uint16, bool) {
	if s.names == nil {
		s.names = make(map[string]uint16, s.ng)
		for g := 0; g < s.ng; g++ {
			nm, err := s.f.GlyphName(&s.buf, sfnt.GlyphIndex(g))
			if err == nil && nm != "" {
				if _, dup := s.names[nm]; !dup {
					s.names[nm] = uint16(g)
				}
			}
		}
	}
	g, ok := s.names[name]
	return g, ok
}

// --- bare CFF (go-text/typesetting) ---

type cffGlyphSource struct {
	f     *cff.CFF
	ng    int
	names map[string]uint16
}

func newCFFGlyphSource(data []byte) (*cffGlyphSource, bool) {
	f, err := cff.Parse(data)
	if err != nil {
		return nil, false
	}
	return &cffGlyphSource{f: f, ng: len(f.Charstrings)}, true
}

// CFF charstring coordinates use a 1000-unit em by PDF convention (FontMatrix
// 0.001); go-text reports raw font units, so units-per-em is 1000.
func (s *cffGlyphSource) unitsPerEm() int { return 1000 }
func (s *cffGlyphSource) numGlyphs() int  { return s.ng }

func (s *cffGlyphSource) advanceWidth(uint16) (int, bool) { return 0, false }

func (s *cffGlyphSource) controlBox(gid uint16) (glyphBox, bool) {
	if int(gid) >= s.ng {
		return glyphBox{}, false
	}
	_, b, err := s.f.LoadGlyph(cfftables.GlyphID(gid))
	if err != nil {
		return glyphBox{}, false
	}
	// PathBounds is already Y-up (PostScript convention). Use Min/Max directly;
	// ToExtents() would re-round and reshape.
	box := glyphBox{
		xMin: roundf64(b.Min.X),
		yMin: roundf64(b.Min.Y),
		xMax: roundf64(b.Max.X),
		yMax: roundf64(b.Max.Y),
	}
	if box.empty() {
		return glyphBox{}, false
	}
	return box, true
}

func (s *cffGlyphSource) gidForName(name string) (uint16, bool) {
	if s.names == nil {
		s.names = make(map[string]uint16, s.ng)
		for g := 0; g < s.ng; g++ {
			if nm := s.f.GlyphName(cffot.GID(g)); nm != "" {
				if _, dup := s.names[nm]; !dup {
					s.names[nm] = uint16(g)
				}
			}
		}
	}
	g, ok := s.names[name]
	return g, ok
}

func (s *cffGlyphSource) gidForRune(rune) (uint16, bool) { return 0, false }

// --- Type1 (benoitkugler/textlayout) ---

type type1GlyphSource struct {
	f     *t1.Font
	upm   int
	ng    int
	names map[string]uint16
}

func newType1GlyphSource(data []byte) (*type1GlyphSource, bool) {
	f, err := t1.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	upm := int(f.Upem())
	if upm <= 0 {
		upm = 1000
	}
	ng := 0
	for f.GlyphName(fonts.GID(ng)) != "" {
		ng++
		if ng > 65535 {
			break
		}
	}
	if ng == 0 {
		return nil, false
	}
	return &type1GlyphSource{f: f, upm: upm, ng: ng}, true
}

func (s *type1GlyphSource) unitsPerEm() int { return s.upm }
func (s *type1GlyphSource) numGlyphs() int  { return s.ng }

func (s *type1GlyphSource) advanceWidth(gid uint16) (int, bool) {
	if int(gid) >= s.ng {
		return 0, false
	}
	width := roundf32(s.f.HorizontalAdvance(fonts.GID(gid)))
	if width <= 0 {
		return 0, false
	}
	return width, true
}

func (s *type1GlyphSource) controlBox(gid uint16) (glyphBox, bool) {
	if int(gid) >= s.ng {
		return glyphBox{}, false
	}
	ext, ok := s.f.GlyphExtents(fonts.GID(gid), 0, 0)
	if !ok {
		return glyphBox{}, false
	}
	// GlyphExtents: XBearing/YBearing = left/top, Width >= 0, Height <= 0
	// (top-to-bottom). Reconstruct a Y-up control box; the type1 ToExtents is
	// lossless (no rounding) so this matches FreeType's CBox.
	box := glyphBox{
		xMin: roundf32(ext.XBearing),
		yMax: roundf32(ext.YBearing),
		xMax: roundf32(ext.XBearing + ext.Width),
		yMin: roundf32(ext.YBearing + ext.Height),
	}
	if box.empty() {
		return glyphBox{}, false
	}
	return box, true
}

func (s *type1GlyphSource) gidForName(name string) (uint16, bool) {
	if s.names == nil {
		s.names = make(map[string]uint16, s.ng)
		for g := 0; g < s.ng; g++ {
			if nm := s.f.GlyphName(fonts.GID(g)); nm != "" {
				if _, dup := s.names[nm]; !dup {
					s.names[nm] = uint16(g)
				}
			}
		}
	}
	g, ok := s.names[name]
	return g, ok
}

func (s *type1GlyphSource) gidForRune(r rune) (uint16, bool) {
	g, ok := s.f.NominalGlyph(r)
	if !ok {
		return 0, false
	}
	return uint16(g), true
}

// psWeight implements psWeightProvider: the embedded Type1 program's
// /FontInfo /Weight string. PSInfo.Weight is "" when the program omits /Weight.
func (s *type1GlyphSource) psWeight() (string, bool) {
	info, ok := s.f.PostscriptInfo()
	if !ok || info.Weight == "" {
		return "", false
	}
	return info.Weight, true
}

// sanitizeTrueType rebuilds a minimal sfnt program retaining only the tables
// sfnt needs to load glyph outlines (head, hhea, hmtx, maxp, loca, glyf and the
// hinting tables) and dropping the rest (cmap, post, name, OS/2, …) which in
// subset fonts are frequently malformed enough that sfnt rejects the whole file.
// Returns false if the program is not a glyf-flavoured sfnt or an essential
// outline table is missing/out of bounds.
func sanitizeTrueType(data []byte) ([]byte, bool) {
	if len(data) < 12 {
		return nil, false
	}
	version := binary.BigEndian.Uint32(data)
	// 0x00010000 (TrueType) or 'true' (Apple) — not 'OTTO' (CFF) or 'ttcf'.
	if version != 0x00010000 && version != 0x74727565 {
		return nil, false
	}
	numTables := int(binary.BigEndian.Uint16(data[4:]))
	if 12+numTables*16 > len(data) {
		return nil, false
	}
	keep := map[string]bool{
		"head": true, "hhea": true, "hmtx": true, "maxp": true,
		"loca": true, "glyf": true, "cvt ": true, "fpgm": true,
		"prep": true, "gasp": true,
	}
	type tbl struct {
		tag  string
		data []byte
	}
	var kept []tbl
	haveGlyf, haveLoca := false, false
	for i := range numTables {
		e := 12 + i*16
		tag := string(data[e : e+4])
		if !keep[tag] {
			continue
		}
		off := int(binary.BigEndian.Uint32(data[e+8:]))
		length := int(binary.BigEndian.Uint32(data[e+12:]))
		if off < 0 || length < 0 || off+length > len(data) {
			return nil, false // essential table out of bounds -> truly broken
		}
		kept = append(kept, tbl{tag, data[off : off+length]})
		switch tag {
		case "glyf":
			haveGlyf = true
		case "loca":
			haveLoca = true
		}
	}
	if !haveGlyf || !haveLoca {
		return nil, false
	}
	// sfnt mandates a parseable cmap and post even though glyph loading does not
	// need them (CIDFontType2 subsets routinely ship without either). Synthesise
	// minimal valid tables so sfnt accepts the program; GID mapping uses the real
	// cmap (parseLenientCmap) or /CIDToGIDMap, never these stubs.
	kept = append(kept,
		tbl{"cmap", syntheticCmap},
		tbl{"post", syntheticPost},
	)
	sort.Slice(kept, func(i, j int) bool { return kept[i].tag < kept[j].tag })

	n := len(kept)
	// searchRange = largest power of two <= n, times 16.
	pow, sel := 1, 0
	for pow*2 <= n {
		pow *= 2
		sel++
	}
	searchRange := pow * 16
	rangeShift := n*16 - searchRange

	headerLen := 12 + n*16
	out := make([]byte, headerLen)
	binary.BigEndian.PutUint32(out, version)
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	binary.BigEndian.PutUint16(out[6:], uint16(searchRange))
	binary.BigEndian.PutUint16(out[8:], uint16(sel))
	binary.BigEndian.PutUint16(out[10:], uint16(rangeShift))

	offset := headerLen
	for i, t := range kept {
		e := 12 + i*16
		copy(out[e:e+4], t.tag)
		// checksum left zero (sfnt does not verify table checksums).
		binary.BigEndian.PutUint32(out[e+8:], uint32(offset))
		binary.BigEndian.PutUint32(out[e+12:], uint32(len(t.data)))
		out = append(out, t.data...)
		if pad := (4 - len(t.data)%4) % 4; pad != 0 {
			out = append(out, make([]byte, pad)...)
		}
		offset += len(t.data)
		offset += (4 - len(t.data)%4) % 4
	}
	return out, true
}

// syntheticCmap is a minimal, valid (3,1) format-4 cmap with a single empty
// segment [0xFFFF,0xFFFF] — enough for sfnt.Parse, mapping no real codes.
var syntheticCmap = []byte{
	0x00, 0x00, 0x00, 0x01, // version 0, numTables 1
	0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x0C, // platform 3, enc 1, offset 12
	0x00, 0x04, 0x00, 0x18, 0x00, 0x00, // format 4, length 24, language 0
	0x00, 0x02, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, // segCountX2 2, searchRange 2, entrySel 0, rangeShift 0
	0xFF, 0xFF, // endCode[0]
	0x00, 0x00, // reservedPad
	0xFF, 0xFF, // startCode[0]
	0x00, 0x01, // idDelta[0]
	0x00, 0x00, // idRangeOffset[0]
}

// syntheticPost is a version-3.0 post table (no glyph names), 32 bytes.
var syntheticPost = []byte{
	0x00, 0x03, 0x00, 0x00, // version 3.0
	0x00, 0x00, 0x00, 0x00, // italicAngle
	0x00, 0x00, 0x00, 0x00, // underlinePosition, underlineThickness
	0x00, 0x00, 0x00, 0x00, // isFixedPitch
	0x00, 0x00, 0x00, 0x00, // minMemType42
	0x00, 0x00, 0x00, 0x00, // maxMemType42
	0x00, 0x00, 0x00, 0x00, // minMemType1
	0x00, 0x00, 0x00, 0x00, // maxMemType1
}

// parseLenientCmap parses the program's cmap via go-text (which tolerates more
// than x/image/sfnt) and returns a rune->GID lookup, or nil if unavailable.
func parseLenientCmap(data []byte) gtfont.Cmap {
	ld, err := cffot.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	raw, err := ld.RawTable(cffot.MustNewTag("cmap"))
	if err != nil || len(raw) == 0 {
		return nil
	}
	tb, _, err := cfftables.ParseCmap(raw)
	if err != nil {
		return nil
	}
	cm, _, err := gtfont.ProcessCmap(tb, cfftables.FontPage(0))
	if err != nil || cm == nil {
		return nil
	}
	return cm
}

// --- scaling / rounding helpers ---

// round26 rounds a 26.6 fixed-point value to the nearest integer (half away from
// zero). Exact multiples of 64 (the lossless case) round to themselves.
func round26(v fixed.Int26_6) int {
	if v >= 0 {
		return int((v + 32) >> 6)
	}
	return -int((-v + 32) >> 6)
}

func roundf64(v float64) int {
	if v >= 0 {
		return int(v + 0.5)
	}
	return int(v - 0.5)
}

func roundf32(v float32) int { return roundf64(float64(v)) }

// normalizeFontMetric scales m font units to 1000-unit glyph space, porting
// NormalizeFontMetric (core/fxge/fx_font.cpp) byte-for-byte:
//
//	if upem == 0 { return m }                          // saturated_cast<int>(value)
//	scaled := (value*1000.0 + upem/2) / upem           // upem/2 is INTEGER division
//	return int(scaled)                                 // saturated_cast truncates toward 0
//
// The +upem/2 bias is integer (uint16/2), added to the DOUBLE value*1000.0; the
// final cast truncates toward zero (NOT round-half-away — verified against the
// PDFium source). This is applied uniformly, so upem == 1000 is NOT the identity
// for negatives (e.g. -5 -> -4).
func normalizeFontMetric(m, upm int) int {
	if upm <= 0 {
		return m
	}
	scaled := (float64(m)*1000.0 + float64(upm/2)) / float64(upm)
	return int(scaled) // float64 -> int truncates toward zero, matching saturated_cast
}
