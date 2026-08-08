// Tests for TeX font-encoding resolution: the width-vector fingerprint that
// identifies which Computer Modern encoding an unlabelled (dvips-style) font
// follows, the encoding tables themselves, and the load-time gating.
package font

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// newDvipsType3Font builds a font dictionary shaped like dvips Type 3 bitmap
// output: unit-scale y-flipping FontMatrix, /Widths in device pixels, and
// /Differences names that are synthetic (#XX hex form, identity single chars,
// and dvips's comma at code 0). No /ToUnicode, no /FontDescriptor.
//
// The widths and names mirror a real cmsy10 subset (minus, periodcentered,
// multiply, plusminus, lessequal, greaterequal, lessmuch, arrowright, bar,
// radical) as emitted by dvips at 600 dpi.
func newDvipsType3Font(t *testing.T) *Font {
	t.Helper()
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))
	matrix := objects.NewArray()
	for _, v := range []int32{1, 0, 0, -1, 0, 0} {
		matrix.Append(num(v))
	}
	d.SetFor("FontMatrix", matrix)
	bbox := objects.NewArray()
	for _, v := range []int32{1, -14, 42, 30} {
		bbox.Append(num(v))
	}
	d.SetFor("FontBBox", bbox)
	d.SetFor("FirstChar", num(0))
	d.SetFor("LastChar", num(112))

	glyphWidths := map[int]int32{
		0x00: 65, 0x01: 23, 0x02: 65, 0x06: 65, 0x14: 65,
		0x15: 65, 0x1C: 83, 0x21: 83, 0x6A: 23, 0x70: 69,
	}
	widths := objects.NewArray()
	for c := 0; c <= 112; c++ {
		widths.Append(num(glyphWidths[c]))
	}
	d.SetFor("Widths", widths)

	// /Differences [0/, /#01 /#02 6/#06 20/#14 /#15 28/#1C 33/! 106/j 112/p]
	// (the #XX names arrive already unescaped from the PDF name parser).
	diffs := objects.NewArray()
	diffs.Append(num(0))
	diffs.Append(name(","))
	diffs.Append(name("#01"))
	diffs.Append(name("#02"))
	diffs.Append(num(6))
	diffs.Append(name("#06"))
	diffs.Append(num(20))
	diffs.Append(name("#14"))
	diffs.Append(name("#15"))
	diffs.Append(num(28))
	diffs.Append(name("#1C"))
	diffs.Append(num(33))
	diffs.Append(name("!"))
	diffs.Append(num(106))
	diffs.Append(name("j"))
	diffs.Append(num(112))
	diffs.Append(name("p"))
	enc := objects.NewDictionary()
	enc.SetFor("Type", name("Encoding"))
	enc.SetFor("Differences", diffs)
	d.SetFor("Encoding", enc)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil for dvips-style Type3 font")
	}
	return f
}

// TestDvipsType3ResolvesTeXSymbolEncoding is the acceptance test for the TeX
// encoding fallback: a dvips Type 3 font whose width fingerprint matches cmsy
// must resolve slot 0x14 to U+2264 (less-than-or-equal) instead of the control
// byte the raw char code would give.
func TestDvipsType3ResolvesTeXSymbolEncoding(t *testing.T) {
	f := newDvipsType3Font(t)
	if got := f.UnicodeFromCharCode(0x14); got != 0x2264 {
		t.Errorf("Unicode(0x14)=%#x (%q) want 0x2264 (≤)", got, got)
	}
	// The rest of the confirmed cmsy slot mappings from the same subset.
	for code, want := range map[uint32]rune{
		0x00: 0x2212, // minus
		0x01: 0x00B7, // periodcentered
		0x02: 0x00D7, // multiply
		0x06: 0x00B1, // plusminus
		0x15: 0x2265, // greaterequal
		0x1C: 0x226A, // lessmuch
		0x21: 0x2192, // arrowright (NOT '!')
		0x6A: '|',    // bar (NOT 'j')
		0x70: 0x221A, // radical (NOT 'p')
	} {
		if got := f.UnicodeFromCharCode(code); got != want {
			t.Errorf("Unicode(%#02x)=%#x (%q) want %#x (%q)", code, got, got, want, want)
		}
	}
	// A code with no glyph in the font and no cmsy width stays unmapped: slot
	// 0x50 is 'P' in ASCII but the font has no width there... it does have a
	// cmsy meaning (script P) but the font never uses it; mapping it is
	// harmless. What must NOT happen is inventing text for codes past 0x7F.
	if got := f.UnicodeFromCharCode(0x90); got != 0 {
		t.Errorf("Unicode(0x90)=%#x want 0 (upper half must stay unmapped)", got)
	}
}

// --- Task 2: encoding table spot checks ---

func TestTeXEncodingTables(t *testing.T) {
	// Known slots per the published TeX encodings.
	if got := texCMSYUnicode[0x00]; got != 0x2212 {
		t.Errorf("cmsy[0x00]=%#x want 0x2212 (minus)", got)
	}
	if got := texCMSYUnicode[0x70]; got != 0x221A {
		t.Errorf("cmsy[0x70]=%#x want 0x221A (radical)", got)
	}
	if got := texCMMIUnicode[0x0B]; got != 0x03B1 {
		t.Errorf("cmmi[0x0B]=%#x want 0x03B1 (alpha)", got)
	}
	if got := texCMMIUnicode[0x21]; got != 0x03C9 {
		t.Errorf("cmmi[0x21]=%#x want 0x03C9 (omega)", got)
	}
	if got := texCMMIUnicode[0x3D]; got != '/' {
		t.Errorf("cmmi[0x3D]=%#x want '/' (solidus)", got)
	}
	if got := texCMEXUnicode[0x5A]; got != 0x222B {
		t.Errorf("cmex[0x5A]=%#x want 0x222B (integraldisplay)", got)
	}
	if got := texCMRUnicode[0x3C]; got != 0x00A1 {
		t.Errorf("cmr[0x3C]=%#x want 0x00A1 (exclamdown, OT1 quirk)", got)
	}
	// Negative assertions: unassigned slots stay 0.
	if got := texCMRUnicode[0x20]; got != 0 {
		t.Errorf("cmr[0x20]=%#x want 0 (OT1 'suppress' has no Unicode)", got)
	}
	if got := texCMEXUnicode[0x30]; got != 0 {
		t.Errorf("cmex[0x30]=%#x want 0 (extensible paren piece)", got)
	}
	for _, tab := range []*[256]rune{&texCMRUnicode, &texCMMIUnicode, &texCMSYUnicode, &texCMEXUnicode} {
		for c := 128; c < 256; c++ {
			if tab[c] != 0 {
				t.Fatalf("slot %#x nonzero: TeX encodings are 128-glyph fonts", c)
			}
		}
	}
}

// --- Task 3: width-fingerprint identification ---

func widthsAt(m map[int]float32) []float32 {
	w := make([]float32, 256)
	for c, v := range m {
		w[c] = v
	}
	return w
}

func TestDetectTeXEncoding(t *testing.T) {
	cases := []struct {
		name   string
		widths []float32
		want   texEncoding
	}{
		{
			// cmsy10 subset at 600dpi device pixels (real dvips output).
			name: "cmsy device pixels",
			widths: widthsAt(map[int]float32{
				0x00: 65, 0x01: 23, 0x02: 65, 0x06: 65, 0x14: 65,
				0x15: 65, 0x1C: 83, 0x21: 83, 0x6A: 23, 0x70: 69,
			}),
			want: texEncCMSY,
		},
		{
			// cmmi7 Greek subset at 600dpi (real dvips output): exercises both
			// scale-freeness and the design-size tolerance (7pt metrics differ
			// slightly from the embedded 10pt reference).
			name: "cmmi7 device pixels",
			widths: widthsAt(map[int]float32{
				11: 43, 12: 38, 14: 30, 15: 28, 17: 34,
				18: 32, 21: 39, 25: 39, 26: 34, 27: 38,
			}),
			want: texEncCMMI,
		},
		{
			// cmsy10 widths in native 1000-unit glyph space (an unlabelled
			// Type 1 embedding): same encoding, different unit.
			name: "cmsy glyph units",
			widths: widthsAt(map[int]float32{
				0x00: 777, 0x14: 777, 0x15: 777, 0x21: 1000, 0x70: 833,
			}),
			want: texEncCMSY,
		},
		{
			// Helvetica metrics do not follow any TeX encoding.
			name: "helvetica rejected",
			widths: widthsAt(map[int]float32{
				33: 278, 40: 333, 41: 333, 44: 278, 45: 333, 46: 278,
				48: 556, 49: 556, 50: 556, 65: 667, 66: 667, 67: 722,
				97: 556, 98: 556, 99: 500, 101: 556, 105: 222, 109: 833,
			}),
			want: texEncNone,
		},
		{
			// A fixed-pitch (all-equal) vector fits nothing well.
			name: "monospace rejected",
			widths: widthsAt(map[int]float32{
				33: 600, 40: 600, 48: 600, 65: 600, 97: 600, 105: 600,
			}),
			want: texEncNone,
		},
		{
			// Equal-width digits fit cmr (lining) and cmmi (oldstyle) equally:
			// ambiguous, so no identification.
			name: "ambiguous digits",
			widths: widthsAt(map[int]float32{
				48: 42, 49: 42, 50: 42, 51: 42,
			}),
			want: texEncNone,
		},
		{
			// Below the minimum glyph count nothing is trusted, even a
			// perfect cmsy-shaped triple.
			name: "too few codes",
			widths: widthsAt(map[int]float32{
				0x00: 65, 0x15: 65, 0x70: 69,
			}),
			want: texEncNone,
		},
		{
			// TeX encodings are 128-glyph fonts: any glyph above 0x7F rules
			// the family out even if the low slots fit.
			name: "high code rejected",
			widths: widthsAt(map[int]float32{
				0x00: 65, 0x14: 65, 0x15: 65, 0x21: 83, 0x70: 69, 0x90: 40,
			}),
			want: texEncNone,
		},
		{
			name:   "empty",
			widths: make([]float32, 256),
			want:   texEncNone,
		},
	}
	for _, tc := range cases {
		if got := detectTeXEncoding(tc.widths); got != tc.want {
			t.Errorf("%s: detectTeXEncoding=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestTeXSyntheticGlyphNames(t *testing.T) {
	cases := []struct {
		name string
		code int
		want bool
	}{
		{"#14", 0x14, true},   // hex form of the code
		{"#1c", 0x1C, true},   // lowercase hex accepted
		{"#14", 0x15, false},  // hex form of a DIFFERENT code is informative
		{"j", 0x6A, true},     // identity single char
		{"j", 0x6B, false},    // single char of a different code
		{",", 0, true},        // dvips's comma-at-zero quirk
		{",", 0x2C, true},     // ... and at its own code it is identity
		{",", 5, false},       // comma anywhere else is informative
		{"minus", 0, false},   // real Adobe glyph name
		{"alpha", 11, false},  // real Adobe glyph name
		{"#123", 0x23, false}, // not the two-digit hex shape
	}
	for _, tc := range cases {
		if got := texSyntheticGlyphName(tc.name, tc.code); got != tc.want {
			t.Errorf("texSyntheticGlyphName(%q, %#x)=%v want %v", tc.name, tc.code, got, tc.want)
		}
	}
	if !texNamesUninformative(nil) {
		t.Error("absent names must gate as uninformative")
	}
	names := make([]string, 256)
	names[0] = ","
	names[0x14] = "#14"
	names[0x6A] = "j"
	if !texNamesUninformative(names) {
		t.Error("dvips-style names must gate as uninformative")
	}
	names[0x0B] = "alpha"
	if texNamesUninformative(names) {
		t.Error("a single real glyph name must disable the gate")
	}
}

// --- Task 3/4: gating on the loaded font ---

// TestTeXGateToUnicodeWins: an explicit /ToUnicode always wins; the TeX
// fingerprint must not even be consulted.
func TestTeXGateToUnicodeWins(t *testing.T) {
	f := func() *Font {
		d := objects.NewDictionary()
		d.SetFor("Subtype", name("Type3"))
		d.SetFor("FirstChar", num(0))
		d.SetFor("LastChar", num(112))
		widths := objects.NewArray()
		glyphWidths := map[int]int32{
			0x00: 65, 0x01: 23, 0x02: 65, 0x06: 65, 0x14: 65,
			0x15: 65, 0x1C: 83, 0x21: 83, 0x6A: 23, 0x70: 69,
		}
		for c := 0; c <= 112; c++ {
			widths.Append(num(glyphWidths[c]))
		}
		d.SetFor("Widths", widths)
		// A ToUnicode CMap that deliberately maps 0x14 to 'X'.
		d.SetFor("ToUnicode", toUnicodeStream(`
/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
1 begincodespacerange
<00> <FF>
endcodespacerange
1 beginbfchar
<14> <0058>
endbfchar
endcmap
`))
		f := Load(d, nil)
		if f == nil {
			t.Fatal("Load returned nil")
		}
		return f
	}()
	if f.texUnicode != nil {
		t.Error("texUnicode resolved despite /ToUnicode being present")
	}
	if got := f.UnicodeFromCharCode(0x14); got != 'X' {
		t.Errorf("Unicode(0x14)=%#x want 'X' from ToUnicode", got)
	}
}

// TestTeXGateRealGlyphNames: a font whose /Differences carry real Adobe glyph
// names is left entirely alone, even with a perfect cmsy width fingerprint.
func TestTeXGateRealGlyphNames(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))
	d.SetFor("FirstChar", num(0))
	d.SetFor("LastChar", num(112))
	glyphWidths := map[int]int32{
		0x00: 65, 0x01: 23, 0x02: 65, 0x06: 65, 0x14: 65,
		0x15: 65, 0x1C: 83, 0x21: 83, 0x6A: 23, 0x70: 69,
	}
	widths := objects.NewArray()
	for c := 0; c <= 112; c++ {
		widths.Append(num(glyphWidths[c]))
	}
	d.SetFor("Widths", widths)
	diffs := objects.NewArray()
	diffs.Append(num(0))
	diffs.Append(name("minus"))
	enc := objects.NewDictionary()
	enc.SetFor("Differences", diffs)
	d.SetFor("Encoding", enc)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.texUnicode != nil {
		t.Error("texUnicode resolved despite real Adobe glyph names")
	}
	if got := f.UnicodeFromCharCode(0x14); got != 0 {
		t.Errorf("Unicode(0x14)=%#x want 0 (behaviour unchanged)", got)
	}
}

// TestTeXFallbackNeverOverridesEncoding: on a simple font the TeX table only
// fills codes the encoding path leaves empty; a successful encoding mapping
// is kept even where OT1 disagrees with it.
func TestTeXFallbackNeverOverridesEncoding(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("UnknownRoman"))
	d.SetFor("FirstChar", num(0))
	d.SetFor("LastChar", num(127))
	widths := objects.NewArray()
	cmr := &texWidthCandidates[0] // cmr10 reference widths
	if cmr.name != "cmr10" {
		t.Fatalf("candidate 0 is %s, want cmr10", cmr.name)
	}
	for c := range 128 {
		widths.Append(num(int32(cmr.widths[c])))
	}
	d.SetFor("Widths", widths)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.texUnicode == nil {
		t.Fatal("cmr10-width font with no names and no ToUnicode should resolve as OT1")
	}
	// StandardEncoding maps 34 to quotedbl; OT1 says quotedblright. The
	// encoding's successful mapping must win.
	if got := f.UnicodeFromCharCode(34); got != '"' {
		t.Errorf("Unicode(34)=%#x want '\"' (encoding path must not be overridden)", got)
	}
	// StandardEncoding leaves slot 11 empty; OT1 has the ff ligature there.
	if got := f.UnicodeFromCharCode(11); got != 0xFB00 {
		t.Errorf("Unicode(11)=%#x want U+FB00 (TeX fallback fills the gap)", got)
	}
}

// TestTeXTooFewGlyphsLeavesFontAlone: a 2-glyph subset (e.g. a paren pair)
// cannot be identified and must keep its baseline behaviour.
func TestTeXTooFewGlyphsLeavesFontAlone(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))
	d.SetFor("FirstChar", num(40))
	d.SetFor("LastChar", num(41))
	widths := objects.NewArray()
	widths.Append(num(23))
	widths.Append(num(23))
	d.SetFor("Widths", widths)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if f.texUnicode != nil {
		t.Error("texUnicode resolved from a 2-glyph fingerprint")
	}
	if got := f.UnicodeFromCharCode(40); got != 0 {
		t.Errorf("Unicode(40)=%#x want 0 (behaviour unchanged)", got)
	}
}
