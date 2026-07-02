// Tests for the face-less glyph-metric accessors (GetCharBBox / GetFontBBox /
// CharCodeFromUnicode) consumed by the text-extraction layer.
package font

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// fontWithDescriptor builds a simple Type1 font with a /FontDescriptor carrying
// the given ascent/descent and /FontBBox, plus a /Widths entry for 'A'..'C'.
func fontWithDescriptor(ascent, descent int32, bbox []int32) *Font {
	d := objects.NewDictionary()
	d.SetFor("Subtype", name("Type1"))
	d.SetFor("BaseFont", name("CustomFont"))
	d.SetFor("Encoding", name("WinAnsiEncoding"))
	d.SetFor("FirstChar", num('A'))
	d.SetFor("LastChar", num('C'))
	widths := objects.NewArray()
	widths.Append(num(500)) // A
	widths.Append(num(600)) // B
	widths.Append(num(700)) // C
	d.SetFor("Widths", widths)

	desc := objects.NewDictionary()
	if ascent != 0 {
		desc.SetFor("Ascent", num(ascent))
	}
	if descent != 0 {
		desc.SetFor("Descent", num(descent))
	}
	if bbox != nil {
		arr := objects.NewArray()
		for _, v := range bbox {
			arr.Append(num(v))
		}
		desc.SetFor("FontBBox", arr)
	}
	d.SetFor("FontDescriptor", desc)
	return Load(d, nil)
}

func TestGetCharBBoxFromAscentDescent(t *testing.T) {
	f := fontWithDescriptor(720, -210, []int32{-100, -250, 800, 750})
	if f == nil {
		t.Fatal("Load returned nil")
	}
	l, b, r, top := f.GetCharBBox('A')
	if l != 0 {
		t.Errorf("left=%d want 0", l)
	}
	if r != 500 { // advance width of 'A'
		t.Errorf("right=%d want 500 (advance)", r)
	}
	if top != 720 { // descriptor ascent wins over FontBBox
		t.Errorf("top=%d want 720 (ascent)", top)
	}
	if b != -210 { // descriptor descent
		t.Errorf("bottom=%d want -210 (descent)", b)
	}
}

func TestGetCharBBoxFallsBackToFontBBox(t *testing.T) {
	// No ascent/descent: vertical extent comes from /FontBBox.
	f := fontWithDescriptor(0, 0, []int32{-100, -250, 800, 760})
	l, b, r, top := f.GetCharBBox('B')
	if l != 0 || r != 600 {
		t.Errorf("horizontal=(%d,%d) want (0,600)", l, r)
	}
	if top != 760 || b != -250 {
		t.Errorf("vertical=(top %d,bottom %d) want (760,-250) from FontBBox", top, b)
	}
}

func TestGetCharBBoxFaceDefault(t *testing.T) {
	// No descriptor metrics at all: face-default 0.8em / -0.2em.
	f := fontWithDescriptor(0, 0, nil)
	_, b, _, top := f.GetCharBBox('C')
	if top != 800 || b != -200 {
		t.Errorf("face default vertical=(top %d,bottom %d) want (800,-200)", top, b)
	}
}

func TestGetFontBBox(t *testing.T) {
	f := fontWithDescriptor(720, -210, []int32{-50, -200, 900, 800})
	l, b, r, top := f.GetFontBBox()
	if l != -50 || b != -200 || r != 900 || top != 800 {
		t.Errorf("GetFontBBox=(%d,%d,%d,%d) want (-50,-200,900,800)", l, b, r, top)
	}
}

func TestCharCodeFromUnicodeViaEncoding(t *testing.T) {
	f := fontWithDescriptor(720, -210, nil)
	// WinAnsiEncoding maps code 0x20 -> U+0020 (space) and 0x41 -> 'A'.
	if got := f.CharCodeFromUnicode(' '); got != 0x20 {
		t.Errorf("CharCodeFromUnicode(' ')=%#x want 0x20", got)
	}
	if got := f.CharCodeFromUnicode('A'); got != 'A' {
		t.Errorf("CharCodeFromUnicode('A')=%#x want 0x41", got)
	}
	// An unmapped rune yields 0.
	if got := f.CharCodeFromUnicode('中'); got != 0 {
		t.Errorf("CharCodeFromUnicode(unmapped)=%#x want 0", got)
	}
}
