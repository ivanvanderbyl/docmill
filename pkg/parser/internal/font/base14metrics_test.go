// Tests for the built-in Standard-14 AFM metric fallback (loadBase14Metrics)
// and the Type 3 /FontBBox glyph-space conversion (loadType3).
package font

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// A Base-14 Type1 font with no /Widths must fall back to the AFM advance
// widths — a conforming reader is required to know the standard fonts'
// metrics even when the PDF omits them.
func TestBase14MetricsFillMissingWidths(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Times-Roman"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	// AFM Times-Roman: A=722, r=333, space=250.
	for _, tc := range []struct {
		code uint32
		want float32
	}{{'A', 722}, {'r', 333}, {' ', 250}} {
		if got := f.GetCharWidthF(tc.code); got != tc.want {
			t.Errorf("width(%q)=%v want %v", rune(tc.code), got, tc.want)
		}
	}
}

// The AFM fallback also supplies a vertical extent, so GetCharBBox returns the
// font's real ascent/descent instead of the face-less 0.8em/-0.2em default.
func TestBase14MetricsFillVerticalExtent(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Times-Roman"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	_, bottom, _, top := f.GetCharBBox('A')
	if top != 683 || bottom != -217 {
		t.Errorf("GetCharBBox('A') vertical = (%d, %d) want (-217, 683)", bottom, top)
	}
}

// NEGATIVE: an explicit /Widths array is authoritative. The AFM table must not
// overwrite a width the document supplied, even a deliberately odd one.
func TestBase14MetricsDoNotOverrideExplicitWidths(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Times-Roman"))
	d.SetFor("FirstChar", num(65)) // 'A'
	d.SetFor("LastChar", num(65))
	widths := objects.NewArray()
	widths.Append(num(999))
	d.SetFor("Widths", widths)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if got := f.GetCharWidthF('A'); got != 999 {
		t.Errorf("width('A')=%v want 999 (explicit /Widths must win)", got)
	}
}

// NEGATIVE: a descriptor that supplies Ascent/Descent keeps them; the AFM
// vertical metrics only backfill an absent descriptor.
func TestBase14MetricsDoNotOverrideDescriptorAscentDescent(t *testing.T) {
	desc := objects.NewDictionary()
	desc.SetFor("Ascent", num(900))
	desc.SetFor("Descent", num(-100))
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("Times-Roman"))
	d.SetFor("FontDescriptor", desc)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	_, bottom, _, top := f.GetCharBBox('A')
	if top != 900 || bottom != -100 {
		t.Errorf("GetCharBBox('A') vertical = (%d, %d) want (-100, 900)", bottom, top)
	}
}

// NEGATIVE: a non-Base-14 font gets no AFM fallback — an unknown font's widths
// stay unknown rather than borrowing Times-Roman's.
func TestBase14MetricsSkipNonStandardFont(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("SomeFoundry-CustomBook"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if got := f.GetCharWidthF('A'); got == 722 {
		t.Error("non-Base-14 font must not receive Times-Roman AFM widths")
	}
}

// A Type 3 /FontBBox is in glyph space and must be scaled by the FontMatrix
// diagonal into 1000-unit space. dvips emits a y-flipping matrix, so the
// corners have to be normalised (bottom <= top) rather than copied through.
func TestType3FontBBoxScaledAndNormalised(t *testing.T) {
	matrix := objects.NewArray()
	for _, v := range []int32{1, 0, 0, -1, 0, 0} {
		matrix.Append(num(v))
	}
	bbox := objects.NewArray()
	for _, v := range []int32{1, -21, 59, 62} {
		bbox.Append(num(v))
	}
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))
	d.SetFor("FontMatrix", matrix)
	d.SetFor("FontBBox", bbox)

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	left, bottom, right, top := f.GetFontBBox()
	// y is flipped by the FontMatrix: raw (-21, 62) -> (21000, -62000),
	// normalised to bottom=-62000, top=21000.
	if left != 1000 || right != 59000 || bottom != -62000 || top != 21000 {
		t.Errorf("GetFontBBox()=(%d,%d,%d,%d) want (1000,-62000,59000,21000)",
			left, bottom, right, top)
	}
}

// NEGATIVE: a Type 3 font with no /FontBBox leaves the box zero (the text
// layer treats a zero rect as "no bounds" and falls through).
func TestType3WithoutFontBBoxLeavesBoxZero(t *testing.T) {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type3"))

	f := Load(d, nil)
	if f == nil {
		t.Fatal("Load returned nil")
	}
	if left, bottom, right, top := f.GetFontBBox(); left|bottom|right|top != 0 {
		t.Errorf("GetFontBBox()=(%d,%d,%d,%d) want all zero", left, bottom, right, top)
	}
}
