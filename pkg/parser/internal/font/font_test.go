// Integration tests for the public Font API (Load/NextChar/GetCharWidthF/
// UnicodeFromCharCode) covering the corpus paths: Identity-H CID + /ToUnicode,
// simple-font encoding fallback, /Widths, Base-14, subset-prefix strip.
package font

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

func name(s string) *objects.Name { return objects.NewName(s) }
func num(n int32) *objects.Number { return objects.NewNumberFromInt(n) }
func toUnicodeStream(s string) *objects.Stream {
	return objects.NewStreamFromData([]byte(s), objects.NewDictionary())
}

// --- simple TrueType font with /Widths + WinAnsiEncoding ---

func TestSimpleFontWidthsAndEncoding(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("TrueType"))
	d.SetFor("BaseFont", name("Helvetica"))
	d.SetFor("Encoding", name("WinAnsiEncoding"))
	d.SetFor("FirstChar", num(65)) // 'A'
	d.SetFor("LastChar", num(67))  // 'C'
	widths := objects.NewArray()
	widths.Append(num(700))
	widths.Append(num(710))
	widths.Append(num(720))
	d.SetFor("Widths", widths)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.IsCID() || f.IsType3() {
		t.Error("expected simple font")
	}
	if got := f.GetCharWidthF('A'); got != 700 {
		t.Errorf("width('A')=%v want 700", got)
	}
	if got := f.GetCharWidthF('C'); got != 720 {
		t.Errorf("width('C')=%v want 720", got)
	}
	// No /Widths entry for 'Z' (out of FirstChar..LastChar) -> 0.
	if got := f.GetCharWidthF('Z'); got != 0 {
		t.Errorf("width('Z')=%v want 0", got)
	}
	// WinAnsiEncoding maps 'A'(0x41) -> U+0041 via the encoding table.
	if got := f.UnicodeFromCharCode('A'); got != 'A' {
		t.Errorf("Unicode('A')=%q want 'A'", got)
	}
	// 0x80 in WinAnsi is the Euro sign U+20AC.
	if got := f.UnicodeFromCharCode(0x80); got != 0x20AC {
		t.Errorf("Unicode(0x80)=%#x want 0x20AC", got)
	}

	// Single-byte tokenisation.
	cc, off := f.NextChar([]byte{0x41, 0x42}, 0)
	if cc != 0x41 || off != 1 {
		t.Errorf("NextChar simple=(%#x,%d) want (0x41,1)", cc, off)
	}
}

func TestSimpleFontUsesEmbeddedAdvanceWhenWidthsMissing(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("TrueType"))
	d.SetFor("BaseFont", name("EmbeddedSubset"))
	d.SetFor("Encoding", name("WinAnsiEncoding"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	f.glyphSrcReady = true
	f.glyphSrc = stubGlyphSource{
		advances: map[uint16]int{1: 640, 2: 250},
		runes:    map[rune]uint16{'A': 1, ' ': 2},
	}

	if got := f.GetCharWidthF('A'); got != 640 {
		t.Errorf("width('A')=%v want embedded advance 640", got)
	}
	if got := f.GetCharWidthF(' '); got != 250 {
		t.Errorf("width(' ')=%v want embedded advance 250", got)
	}
}

func TestSimpleFontExplicitWidthsDoNotUseEmbeddedFallback(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("TrueType"))
	d.SetFor("BaseFont", name("EmbeddedSubset"))
	d.SetFor("Encoding", name("WinAnsiEncoding"))
	d.SetFor("FirstChar", num('A'))
	d.SetFor("LastChar", num('A'))
	widths := objects.NewArray()
	widths.Append(num(700))
	d.SetFor("Widths", widths)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	f.glyphSrcReady = true
	f.glyphSrc = stubGlyphSource{
		advances: map[uint16]int{1: 640, 2: 510},
		runes:    map[rune]uint16{'A': 1, 'Z': 2},
	}

	if got := f.GetCharWidthF('A'); got != 700 {
		t.Errorf("width('A')=%v want explicit width 700", got)
	}
	if got := f.GetCharWidthF('Z'); got != 0 {
		t.Errorf("width('Z')=%v want 0 for out-of-range explicit widths", got)
	}
}

// --- simple font /Differences override via AGL ---

func TestSimpleFontDifferences(t *testing.T) {
	enc := objects.NewDictionary()
	enc.SetFor("BaseEncoding", name("WinAnsiEncoding"))
	diffs := objects.NewArray()
	diffs.Append(num(65))         // start at code 65
	diffs.Append(name("Lambda"))  // 65 -> U+039B (AGL)
	diffs.Append(name("uni2603")) // 66 -> U+2603 (uniXXXX)
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("CustomFont"))
	d.SetFor("Encoding", enc)
	enc.SetFor("Differences", diffs)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if got := f.UnicodeFromCharCode(65); got != 0x039B {
		t.Errorf("Unicode(65)=%#x want 0x039B (Lambda)", got)
	}
	if got := f.UnicodeFromCharCode(66); got != 0x2603 {
		t.Errorf("Unicode(66)=%#x want 0x2603 (uni2603)", got)
	}
}

// --- Type1 Base-14 canonicalisation + Courier fixed width ---

func TestType1Base14CourierWidths(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("CourierNewPSMT")) // alias -> Courier
	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.BaseFontName() != "Courier" {
		t.Errorf("BaseFontName=%q want Courier", f.BaseFontName())
	}
	// IsFixedFont(Courier) -> all widths 600.
	if got := f.GetCharWidthF('a'); got != 600 {
		t.Errorf("width('a')=%v want 600", got)
	}
}

// --- subset-prefix strip (CHEESE+Swiss -> Swiss) ---

func TestSubsetPrefixStrip(t *testing.T) {
	desc := objects.NewDictionary()
	// Embedded font file presence triggers the strip; the actual bytes are
	// irrelevant to the no-face port.
	desc.SetFor("FontFile", objects.NewStreamFromData([]byte("dummy"), objects.NewDictionary()))
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("CHEESE+Swiss"))
	d.SetFor("FontDescriptor", desc)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.BaseFontName() != "Swiss" {
		t.Errorf("BaseFontName=%q want Swiss", f.BaseFontName())
	}
	if !f.IsEmbedded() {
		t.Error("expected IsEmbedded")
	}
}

// --- Identity-H CID font with embedded /ToUnicode + /W,/DW ---

func TestIdentityHCIDFont(t *testing.T) {
	// DescendantFont with /W [ 1 [ 500 ] 2 4 600 ] and /DW 1000.
	cidDict := objects.NewDictionary()
	cidDict.SetFor("Subtype", name("CIDFontType2"))
	cidDict.SetFor("BaseFont", name("ABCDEF+Arial"))
	cidDict.SetFor("DW", num(1000))
	w := objects.NewArray()
	w.Append(num(1))
	wInner := objects.NewArray()
	wInner.Append(num(500))
	w.Append(wInner) // CID 1 -> width 500
	w.Append(num(2))
	w.Append(num(4))
	w.Append(num(600)) // CIDs 2..4 -> width 600
	cidDict.SetFor("W", w)

	descs := objects.NewArray()
	descs.Append(cidDict)

	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type0"))
	d.SetFor("BaseFont", name("ABCDEF+Arial"))
	d.SetFor("Encoding", name("Identity-H"))
	d.SetFor("DescendantFonts", descs)
	// /ToUnicode: bfrange <0001><0003><0041> maps CID 1->A, 2->B, 3->C.
	d.SetFor("ToUnicode", toUnicodeStream("1 beginbfrange<0001><0003><0041>endbfrange"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if !f.IsCID() {
		t.Error("expected CID font")
	}
	if f.IsVertWriting() {
		t.Error("Identity-H is horizontal")
	}

	// Identity-H tokenises 2 bytes per code; CID == charcode.
	cc, off := f.NextChar([]byte{0x00, 0x01, 0x00, 0x02}, 0)
	if cc != 0x0001 || off != 2 {
		t.Errorf("NextChar=(%#x,%d) want (0x0001,2)", cc, off)
	}
	cc, off = f.NextChar([]byte{0x00, 0x01, 0x00, 0x02}, off)
	if cc != 0x0002 || off != 4 {
		t.Errorf("NextChar2=(%#x,%d) want (0x0002,4)", cc, off)
	}

	// Widths.
	if got := f.GetCharWidthF(1); got != 500 {
		t.Errorf("width(CID 1)=%v want 500", got)
	}
	if got := f.GetCharWidthF(3); got != 600 {
		t.Errorf("width(CID 3)=%v want 600", got)
	}
	if got := f.GetCharWidthF(99); got != 1000 {
		t.Errorf("width(CID 99)=%v want 1000 (DW)", got)
	}

	// Unicode via /ToUnicode.
	if got := f.UnicodeFromCharCode(1); got != 'A' {
		t.Errorf("Unicode(1)=%q want 'A'", got)
	}
	if got := f.UnicodeFromCharCode(3); got != 'C' {
		t.Errorf("Unicode(3)=%q want 'C'", got)
	}
}

// --- Identity-V sets vertical writing ---

func TestIdentityVVertical(t *testing.T) {
	cidDict := objects.NewDictionary()
	cidDict.SetFor("Subtype", name("CIDFontType2"))
	cidDict.SetFor("BaseFont", name("Arial"))
	descs := objects.NewArray()
	descs.Append(cidDict)
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type0"))
	d.SetFor("BaseFont", name("Arial"))
	d.SetFor("Encoding", name("Identity-V"))
	d.SetFor("DescendantFonts", descs)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if !f.IsVertWriting() {
		t.Error("Identity-V should be vertical")
	}
}

// --- CID font missing DescendantFonts fails to load ---

func TestCIDFontMissingDescendant(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type0"))
	d.SetFor("BaseFont", name("Arial"))
	d.SetFor("Encoding", name("Identity-H"))
	// No DescendantFonts -> Load fails.
	if f := Load(d, nil); f != nil {
		t.Error("expected nil for CID font without DescendantFonts")
	}
}

// --- Type3 font: IsType3 + /Widths scaled by FontMatrix ---

func TestType3Font(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))
	matrix := objects.NewArray()
	for _, v := range []int32{1, 0, 0, 1, 0, 0} { // identity *0.001? use 0.001 scale
		matrix.Append(num(v))
	}
	// Use a FontMatrix of 0.001 scale via floats is awkward with ints; identity
	// matrix means xscale=1, width*1000.
	d.SetFor("FontMatrix", matrix)
	d.SetFor("FirstChar", num(97)) // 'a'
	widths := objects.NewArray()
	widths.Append(num(1)) // 'a' width 1 (text units) -> *1000 = 1000 glyph units
	d.SetFor("Widths", widths)
	// /ToUnicode maps 'a' -> U+0061.
	d.SetFor("ToUnicode", toUnicodeStream("1 beginbfchar<61><0061>endbfchar"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if !f.IsType3() {
		t.Error("expected Type3")
	}
	if got := f.GetCharWidthF('a'); got != 1000 {
		t.Errorf("Type3 width('a')=%v want 1000", got)
	}
	if got := f.UnicodeFromCharCode('a'); got != 'a' {
		t.Errorf("Type3 Unicode('a')=%q want 'a'", got)
	}
}

// --- ToUnicode takes priority over encoding for simple fonts ---

func TestToUnicodePriorityOverEncoding(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Helvetica"))
	d.SetFor("Encoding", name("WinAnsiEncoding"))
	// /ToUnicode remaps 'A'(0x41) to U+2660 (spade) despite WinAnsi 0x41 -> 'A'.
	d.SetFor("ToUnicode", toUnicodeStream("1 beginbfchar<41><2660>endbfchar"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if got := f.UnicodeFromCharCode('A'); got != 0x2660 {
		t.Errorf("Unicode('A')=%#x want 0x2660 (ToUnicode wins)", got)
	}
}

// --- multi-char ligature surfaces in the string API ---

func TestUnicodeStringFromCharCode(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Helvetica"))
	// 'f'(0x66) -> "fi" ligature expansion U+0066 U+0069.
	d.SetFor("ToUnicode", toUnicodeStream("1 beginbfchar<66><00660069>endbfchar"))
	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if got := f.UnicodeStringFromCharCode('f'); got != "fi" {
		t.Errorf("UnicodeString('f')=%q want \"fi\"", got)
	}
	// Surrogate pair decodes to its scalar in the string API.
	d2 := objects.NewDictionary()
	d2.SetFor("Subtype", name("Type1"))
	d2.SetFor("BaseFont", name("Helvetica"))
	d2.SetFor("ToUnicode", toUnicodeStream("1 beginbfchar<01><d841de76>endbfchar"))
	f2 := Load(d2, nil)
	if got := f2.UnicodeFromCharCode(1); got != 0x20676 {
		t.Errorf("Unicode(1)=%#x want 0x20676 (surrogate decoded)", got)
	}
}

// --- FontWeight reliable signals (ForceBold flag / StemV / psWeight) ---

// fontWithWeightDescriptor loads a simple Type1 font carrying a /FontDescriptor
// with the given /Flags and (optionally) /StemV. BaseFont has no weight token
// unless one is supplied, so the descriptor signals are exercised in isolation.
func fontWithWeightDescriptor(t *testing.T, baseFont string, flags int, stemV int, hasStemV bool) *Font {
	t.Helper()
	desc := objects.NewDictionary()
	desc.SetFor("Flags", num(int32(flags)))
	if hasStemV {
		desc.SetFor("StemV", num(int32(stemV)))
	}
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name(baseFont))
	d.SetFor("FontDescriptor", desc)
	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	return f
}

func TestFontWeightForceBoldFlag(t *testing.T) {
	// fontStyleForceBold is bit 19 (0x40000). Combine with NonSymbolic so the
	// font loads via the normal non-symbolic path; the name carries no weight.
	f := fontWithWeightDescriptor(t, "PlainName", fontStyleNonSymbolic|fontStyleForceBold, 0, false)
	if got := f.FontWeight(); got < 700 {
		t.Errorf("FontWeight()=%d want >=700 (ForceBold flag set)", got)
	}
}

func TestFontWeightStemVNotUsed(t *testing.T) {
	// A high /StemV must NOT by itself make a font bold: StemV is foundry-relative
	// and an absolute threshold misfires on Regular and math fonts (CMSY/CMEX,
	// Montserrat-Regular). Only ForceBold and the embedded Type1 /Weight are used.
	f := fontWithWeightDescriptor(t, "PlainName", fontStyleNonSymbolic, 130, true)
	if got := f.FontWeight(); got >= 700 {
		t.Errorf("FontWeight()=%d want <700 (StemV alone must not indicate bold)", got)
	}
}

func TestFontWeightRegularNotBold(t *testing.T) {
	// No ForceBold flag, a name with no weight token, regardless of StemV.
	f := fontWithWeightDescriptor(t, "PlainName", fontStyleNonSymbolic, 80, true)
	if got := f.FontWeight(); got >= 700 {
		t.Errorf("FontWeight()=%d want <700 (regular font: no flag, plain name)", got)
	}
	f2 := fontWithWeightDescriptor(t, "PlainName", fontStyleNonSymbolic, 0, false)
	if got := f2.FontWeight(); got >= 700 {
		t.Errorf("FontWeight()=%d want <700 (no StemV present)", got)
	}
}

func TestFontWeightNameStillWorks(t *testing.T) {
	// With no descriptor signals, the name heuristic must still resolve (preserves
	// prior behaviour for fonts whose name carries the weight token).
	f := fontWithWeightDescriptor(t, "Helvetica-Bold", fontStyleNonSymbolic, 0, false)
	if got := f.FontWeight(); got < 700 {
		t.Errorf("FontWeight()=%d want >=700 (name token 'Bold')", got)
	}
}

func TestPSWeightToOpenType(t *testing.T) {
	cases := []struct {
		weight string
		want   int
	}{
		{"Bold", 700},
		{"bold", 700},
		{"Black", 900},
		{"Heavy", 900},
		{"SemiBold", 600},
		{"DemiBold", 600},
		{"Demi", 600},
		{"Medium", 500},
		{"Light", 300},
		{"Thin", 300},
		{"Regular", 0},
		{"", 0},
		{"Roman", 0},
	}
	for _, tc := range cases {
		if got := psWeightToOpenType(tc.weight); got != tc.want {
			t.Errorf("psWeightToOpenType(%q)=%d want %d", tc.weight, got, tc.want)
		}
	}
	// A >=600 mapping is the bold threshold for the Type1 /Weight path.
	if psWeightToOpenType("SemiBold") < 600 {
		t.Error("SemiBold should map to a bold-indicating weight (>=600)")
	}
}

// type1WeightStub stubs the psWeightProvider capability so the FontWeight Type1
// /FontInfo /Weight path is exercised without a real embedded program.
type type1WeightStub struct {
	stubGlyphSource
	weight string
}

func (s type1WeightStub) psWeight() (string, bool) {
	if s.weight == "" {
		return "", false
	}
	return s.weight, true
}

func TestFontWeightType1FontInfoWeight(t *testing.T) {
	// CMBX10 (Computer Modern Bold): PostScript NAME carries no weight token, but
	// the embedded Type1 program's /FontInfo /Weight is "Bold". Simulate the
	// embedded program with a FontFile tag + a psWeight-providing glyph source.
	f := fontWithWeightDescriptor(t, "CMBX10", fontStyleNonSymbolic, 0, false)
	if got := f.FontWeight(); got >= 700 {
		t.Fatalf("precondition: CMBX10 name alone must not be bold, got %d", got)
	}
	f.fontProgramTag = "FontFile"
	f.glyphSrcReady = true
	f.glyphSrc = type1WeightStub{weight: "Bold"}
	f.weightReady = false // drop the memo primed by the precondition probe
	if got := f.FontWeight(); got < 700 {
		t.Errorf("FontWeight()=%d want >=700 (embedded Type1 /Weight Bold)", got)
	}
}

type stubGlyphSource struct {
	advances map[uint16]int
	boxes    map[uint16]glyphBox
	names    map[string]uint16
	runes    map[rune]uint16
}

func (s stubGlyphSource) unitsPerEm() int { return 1000 }
func (s stubGlyphSource) numGlyphs() int  { return 256 }

func (s stubGlyphSource) advanceWidth(gid uint16) (int, bool) {
	width, ok := s.advances[gid]
	return width, ok
}

func (s stubGlyphSource) controlBox(gid uint16) (glyphBox, bool) {
	box, ok := s.boxes[gid]
	return box, ok
}

func (s stubGlyphSource) gidForName(name string) (uint16, bool) {
	gid, ok := s.names[name]
	return gid, ok
}

func (s stubGlyphSource) gidForRune(r rune) (uint16, bool) {
	gid, ok := s.runes[r]
	return gid, ok
}
