// End-to-end tests that drive a real *page.Page (built from object dicts +
// content stream) through the TextPage pipeline — New/CountChars/GetAllText/
// GetRectArray/GetTextByRect — proving the full ProcessObject ->
// ProcessTextObject -> ProcessTextObjectItems -> CloseTempLine path without the
// PDF corpus. The page package's TextObject constructor is unexported, so this
// is the only way to obtain real text objects from the text package.
package text

import (
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/page"
)

func name(s string) *objects.Name { return objects.NewName(s) }
func num(n int32) *objects.Number { return objects.NewNumberFromInt(n) }

// buildPage builds a US-Letter page whose /Contents is the given stream, with a
// 12pt Helvetica Type1 font (WinAnsiEncoding, /Widths for the printable ASCII
// range and a descriptor with ascent/descent so GetCharBBox yields a real box).
func buildPage(content string) *page.Page {
	return buildPageWithWidth(content, 500)
}

func buildPageWithWidth(content string, width int32) *page.Page {
	return buildPageWithWidthAndToUnicode(content, width, "")
}

func buildPageWithWidthAndToUnicode(content string, width int32, toUnicode string) *page.Page {
	fontDict := objects.NewDictionary()
	fontDict.SetFor("Type", name("Font"))
	fontDict.SetFor("Subtype", name("Type1"))
	fontDict.SetFor("BaseFont", name("Helvetica"))
	fontDict.SetFor("Encoding", name("WinAnsiEncoding"))
	if toUnicode != "" {
		fontDict.SetFor("ToUnicode", objects.NewStreamFromData([]byte(toUnicode), objects.NewDictionary()))
	}
	fontDict.SetFor("FirstChar", num(32))
	fontDict.SetFor("LastChar", num(126))
	widths := objects.NewArray()
	for c := int32(32); c <= 126; c++ {
		widths.Append(num(width)) // uniform advance keeps the math simple
	}
	fontDict.SetFor("Widths", widths)
	desc := objects.NewDictionary()
	desc.SetFor("Ascent", num(718))
	desc.SetFor("Descent", num(-207))
	bbox := objects.NewArray()
	for _, v := range []int32{-166, -225, 1000, 931} {
		bbox.Append(num(v))
	}
	desc.SetFor("FontBBox", bbox)
	fontDict.SetFor("FontDescriptor", desc)

	fontRes := objects.NewDictionary()
	fontRes.SetFor("F1", fontDict)
	resources := objects.NewDictionary()
	resources.SetFor("Font", fontRes)

	contentStream := objects.NewStreamFromData([]byte(content), objects.NewDictionary())

	pageDict := objects.NewDictionary()
	pageDict.SetFor("Type", name("Page"))
	mediabox := objects.NewArray()
	for _, v := range []int32{0, 0, 612, 792} {
		mediabox.Append(num(v))
	}
	pageDict.SetFor("MediaBox", mediabox)
	pageDict.SetFor("Resources", resources)
	pageDict.SetFor("Contents", contentStream)
	return page.LoadPage(pageDict, nil)
}

func TestE2ESingleWord(t *testing.T) {
	// Draw "Hi" at (100,700).
	p := buildPage("BT /F1 12 Tf 100 700 Td (Hi) Tj ET")
	tp := New(p, false)

	if tp.CountChars() != 2 {
		t.Fatalf("CountChars=%d want 2", tp.CountChars())
	}
	if got := tp.GetAllText(); got != "Hi" {
		t.Errorf("GetAllText=%q want \"Hi\"", got)
	}

	// One rect for the single text object, in USER space (y-up), near (100,700).
	rects := tp.GetRectArray()
	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	r := rects[0].Box
	// Left edge ~100 (origin), bottom below the baseline (descent < 0), top above.
	if r.Left < 99 || r.Left > 101 {
		t.Errorf("rect left=%v want ~100 (user space, not device)", r.Left)
	}
	if !(r.Bottom < 700 && r.Top > 700) {
		t.Errorf("rect y-extent=[%v,%v] want to straddle baseline 700 (y-up user space)", r.Bottom, r.Top)
	}
	if rects[0].Text != "Hi" {
		t.Errorf("rect text=%q want Hi", rects[0].Text)
	}
}

func TestGetRectArrayIncludesRenderedFontSize(t *testing.T) {
	p := buildPage("BT /F1 18 Tf 100 700 Td (Heading) Tj ET")
	tp := New(p, false)

	rects := tp.GetRectArray()

	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	if rects[0].FontSize != 18 {
		t.Fatalf("rect font size=%v want 18", rects[0].FontSize)
	}
}

func TestGetRectArrayKeepsUnicodeGlyphWithZeroWidth(t *testing.T) {
	p := buildPageWithWidth("BT /F1 24 Tf 100 700 Td (n) Tj ET", 0)
	tp := New(p, false)

	rects := tp.GetRectArray()

	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	if rects[0].Text != "n" {
		t.Fatalf("rect text=%q want n", rects[0].Text)
	}
	if rects[0].Box.Width() <= 0 {
		t.Fatalf("rect width=%v want positive fallback width", rects[0].Box.Width())
	}
}

func TestE2EExpandsMultiRuneToUnicodeMapping(t *testing.T) {
	p := buildPageWithWidthAndToUnicode(
		"BT /F1 12 Tf 100 700 Td (fndings) Tj ET",
		500,
		"1 beginbfchar<66><00660069>endbfchar",
	)
	tp := New(p, false)

	if got := tp.GetAllText(); got != "findings" {
		t.Fatalf("GetAllText=%q want findings", got)
	}
	rects := tp.GetRectArray()
	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	if got := rects[0].Text; got != "findings" {
		t.Fatalf("rect text=%q want findings", got)
	}
	if got := tp.GetTextByRect(rects[0].Box); got != "findings" {
		t.Fatalf("GetTextByRect=%q want findings", got)
	}
}

func TestE2EActualTextReplacementHasSelectableGeometry(t *testing.T) {
	p := buildPage("/Span << /ActualText (fi) >> BDC BT /F1 12 Tf 100 700 Td (x) Tj ET EMC")
	tp := New(p, false)

	if got := tp.GetAllText(); got != "fi" {
		t.Fatalf("GetAllText=%q want fi", got)
	}
	rects := tp.GetRectArray()
	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	if rects[0].Box.IsEmpty() {
		t.Fatalf("actual text rect is empty: %+v", rects[0].Box)
	}
	if got := rects[0].Text; got != "fi" {
		t.Fatalf("rect text=%q want fi", got)
	}
	if got := tp.GetTextByRect(rects[0].Box); got != "fi" {
		t.Fatalf("GetTextByRect=%q want fi", got)
	}
}

func TestE2EGetRectArrayDoesNotEmitStandaloneWhitespaceRects(t *testing.T) {
	p := buildPage("BT /F1 12 Tf 100 700 Td (of) Tj ( ) Tj (their) Tj ET")
	tp := New(p, false)

	rects := tp.GetRectArray()

	if len(rects) != 2 {
		t.Fatalf("got %d rects want 2 non-whitespace rects", len(rects))
	}
	if got := rects[0].Text; got != "of" {
		t.Fatalf("first rect text=%q want of", got)
	}
	if got := rects[1].Text; got != "their" {
		t.Fatalf("second rect text=%q want their", got)
	}
}

func TestE2EUserSpaceNotDeviceSpace(t *testing.T) {
	// The page is 792 tall. A glyph drawn at user-y=700 is near the TOP of the
	// page; in device (top-left) space its y would be ~92. Asserting the rect's
	// bottom/top straddle ~700 proves Box is in USER space (y-up), not device.
	//
	// Uses a multi-glyph object so this assertion measures user-space placement
	// over a stable word-shaped box; single-glyph objects are covered separately.
	p := buildPage("BT /F1 12 Tf 100 700 Td (XY) Tj ET")
	tp := New(p, false)
	rects := tp.GetRectArray()
	if len(rects) != 1 {
		t.Fatalf("got %d rects want 1", len(rects))
	}
	mid := (rects[0].Box.Bottom + rects[0].Box.Top) / 2
	if mid < 690 || mid > 715 {
		t.Errorf("rect vertical midpoint=%v want ~700 (USER space y-up); a device-space "+
			"rect would be near %v", mid, 792-700)
	}
}

func TestE2EWordSpacingTwoObjects(t *testing.T) {
	// Two separate text objects far apart on the same line should not be glued
	// into one word; a generated space may appear, and they are two rects.
	p := buildPage("BT /F1 12 Tf 100 700 Td (Hello) Tj 200 0 Td (World) Tj ET")
	tp := New(p, false)
	text := tp.GetAllText()
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "World") {
		t.Errorf("GetAllText=%q want to contain Hello and World", text)
	}
	rects := tp.GetRectArray()
	if len(rects) < 2 {
		t.Fatalf("got %d rects want >=2 (two text objects)", len(rects))
	}
}

func TestIsRightToLeftASCIIHasNoAllocations(t *testing.T) {
	p := buildPage("BT /F1 12 Tf 100 700 Td (Hello World) Tj ET")
	objs := p.Objects()
	if len(objs) != 1 {
		t.Fatalf("got %d page objects want 1", len(objs))
	}
	to, ok := objs[0].(*page.TextObject)
	if !ok {
		t.Fatalf("object 0 is %T, want *page.TextObject", objs[0])
	}

	var got bool
	allocs := testing.AllocsPerRun(100, func() {
		got = isRightToLeft(to)
	})

	if got {
		t.Fatal("ASCII text should not be right-to-left")
	}
	if allocs != 0 {
		t.Fatalf("isRightToLeft ASCII allocations=%v want 0", allocs)
	}
}

func TestE2EEmptyPage(t *testing.T) {
	p := buildPage("BT ET")
	tp := New(p, false)
	if tp.CountChars() != 0 {
		t.Errorf("empty page CountChars=%d want 0", tp.CountChars())
	}
	if tp.GetAllText() != "" {
		t.Errorf("empty page text=%q want empty", tp.GetAllText())
	}
	if len(tp.GetRectArray()) != 0 {
		t.Error("empty page should produce no rects")
	}
}

func TestE2EGetTextByRect(t *testing.T) {
	p := buildPage("BT /F1 12 Tf 100 700 Td (AB) Tj ET")
	tp := New(p, false)
	// The whole-line user-space box should select the text.
	got := tp.GetTextByRect(tp.GetRectArray()[0].Box)
	if !strings.Contains(got, "A") {
		t.Errorf("GetTextByRect=%q want to contain the glyphs", got)
	}
	// A far-away box selects nothing.
	if g := tp.GetTextByRect(box(0, 0, 1, 1)); g != "" {
		t.Errorf("GetTextByRect(far box)=%q want empty", g)
	}
}
