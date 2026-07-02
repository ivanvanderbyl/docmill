// End-to-end smoke test: build a trivial page dict + content stream and run the
// interpreter, proving the whole pipeline (LoadPage -> parseContent ->
// AddTextObject) without the PDF corpus. Also covers page dimensions/matrices.
package page

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// buildTestPage builds a page dict whose /Contents stream draws "Hi" at
// (100,700) with a 12pt Type1 font (Widths H=600, i=300, WinAnsiEncoding).
func buildTestPage(content string) *objects.Dictionary {
	// Font: Type1 with WinAnsiEncoding + /Widths for 'H'(72) and 'i'(105).
	fontDict := objects.NewDictionary()
	fontDict.SetFor("Type", objects.NewName("Font"))
	fontDict.SetFor("Subtype", objects.NewName("Type1"))
	fontDict.SetFor("BaseFont", objects.NewName("Helvetica"))
	fontDict.SetFor("Encoding", objects.NewName("WinAnsiEncoding"))
	fontDict.SetFor("FirstChar", objects.NewNumberFromInt('H')) // 72
	fontDict.SetFor("LastChar", objects.NewNumberFromInt('i'))  // 105
	widths := objects.NewArray()
	for c := int32('H'); c <= 'i'; c++ {
		switch c {
		case 'H':
			widths.Append(objects.NewNumberFromInt(600))
		case 'i':
			widths.Append(objects.NewNumberFromInt(300))
		default:
			widths.Append(objects.NewNumberFromInt(0))
		}
	}
	fontDict.SetFor("Widths", widths)

	fontRes := objects.NewDictionary()
	fontRes.SetFor("F1", fontDict)
	resources := objects.NewDictionary()
	resources.SetFor("Font", fontRes)

	contentStream := objects.NewStreamFromData([]byte(content), objects.NewDictionary())

	page := objects.NewDictionary()
	page.SetFor("Type", objects.NewName("Page"))
	mediabox := objects.NewArray()
	for _, v := range []int32{0, 0, 612, 792} {
		mediabox.Append(objects.NewNumberFromInt(v))
	}
	page.SetFor("MediaBox", mediabox)
	page.SetFor("Resources", resources)
	page.SetFor("Contents", contentStream)
	return page
}

func TestSmokeShowText(t *testing.T) {
	page := buildTestPage("BT /F1 12 Tf 100 700 Td (Hi) Tj ET")
	p := LoadPage(page, nil)
	if p == nil {
		t.Fatal("LoadPage returned nil")
	}
	if p.Width() != 612 || p.Height() != 792 {
		t.Errorf("page size = %vx%v, want 612x792", p.Width(), p.Height())
	}

	objs := p.Objects()
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1", len(objs))
	}
	to, ok := objs[0].(*TextObject)
	if !ok {
		t.Fatalf("object 0 is %T, want *TextObject", objs[0])
	}
	if to.FontSize() != 12 {
		t.Errorf("FontSize = %v, want 12", to.FontSize())
	}
	if to.Font() == nil {
		t.Fatal("Font() is nil")
	}
	if to.GetResourceName() != "F1" {
		t.Errorf("resource name = %q, want F1", to.GetResourceName())
	}

	items := to.Items()
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (H, i)", len(items))
	}
	if items[0].CharCode != 'H' || items[1].CharCode != 'i' {
		t.Errorf("char codes = %c,%c, want H,i", items[0].CharCode, items[1].CharCode)
	}
	// Item 0 origin is the run start (0,0) in text space; item 1 origin is the
	// advance of 'H' = 600*12/1000 = 7.2.
	if items[0].Origin.X != 0 {
		t.Errorf("item 0 origin.x = %v, want 0", items[0].Origin.X)
	}
	if !approx(items[1].Origin.X, 7.2) {
		t.Errorf("item 1 origin.x = %v, want 7.2", items[1].Origin.X)
	}

	// The text matrix carries the Td translation (100,700) as (e,f).
	tm := to.TextMatrix()
	if tm.E != 100 || tm.F != 700 {
		t.Errorf("TextMatrix translation = (%v,%v), want (100,700)", tm.E, tm.F)
	}
	// Glyph device position of 'i' = DisplayMatrix * TextMatrix * Origin_i.
	glyphUser := tm.Transform(items[1].Origin)
	if !approx(glyphUser.X, 107.2) || !approx(glyphUser.Y, 700) {
		t.Errorf("glyph 'i' user pos = %+v, want ~(107.2,700)", glyphUser)
	}
}

func TestSmokeStrokedPathLine(t *testing.T) {
	page := buildTestPage("2 w 10 20 m 110 20 l S")
	p := LoadPage(page, nil)

	objs := p.Objects()
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1", len(objs))
	}
	path, ok := objs[0].(*PathObject)
	if !ok {
		t.Fatalf("object 0 is %T, want *PathObject", objs[0])
	}
	if path.StrokeWidth() != 2 {
		t.Errorf("StrokeWidth = %v, want 2", path.StrokeWidth())
	}
	segments := path.Segments()
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if segments[0].From.X != 10 || segments[0].From.Y != 20 || segments[0].To.X != 110 || segments[0].To.Y != 20 {
		t.Errorf("segment = %+v, want (10,20)->(110,20)", segments[0])
	}
	rect := path.Rect()
	if rect.Left != 10 || rect.Right != 110 || rect.Bottom != 20 || rect.Top != 20 {
		t.Errorf("rect = %+v, want left=10 right=110 bottom=20 top=20", rect)
	}
}

func TestFilledPathIsNotExposedAsRulingPath(t *testing.T) {
	page := buildTestPage("10 20 m 110 20 l 110 40 l 10 40 l h f")
	p := LoadPage(page, nil)

	objs := p.Objects()
	if len(objs) != 0 {
		t.Fatalf("got %d objects, want 0", len(objs))
	}
}

func TestSmokeTJKerning(t *testing.T) {
	// [(H) -1000 (i)] TJ : the -1000 kerning shifts 'i' right by 1000*12/1000=12.
	page := buildTestPage("BT /F1 12 Tf 100 700 Td [(H) -1000 (i)] TJ ET")
	p := LoadPage(page, nil)
	objs := p.Objects()
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1", len(objs))
	}
	to := objs[0].(*TextObject)
	items := to.Items()
	// items: H, sentinel, i. 'i' origin = advance(H)=7.2 + kerning shift 12 = 19.2.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[1].CharCode != InvalidCharCode {
		t.Errorf("item 1 should be the kerning sentinel")
	}
	if !approx(items[2].Origin.X, 19.2) {
		t.Errorf("item 2 ('i') origin.x = %v, want 19.2", items[2].Origin.X)
	}
}

func TestDimensionsRotated(t *testing.T) {
	page := buildTestPage("BT ET")
	page.SetFor("Rotate", objects.NewNumberFromInt(90))
	p := LoadPage(page, nil)
	// Rotation 1 swaps width/height.
	if p.Width() != 792 || p.Height() != 612 {
		t.Errorf("rotated size = %vx%v, want 792x612", p.Width(), p.Height())
	}
}

func TestDisplayMatrixIdentityRotation(t *testing.T) {
	page := buildTestPage("BT ET")
	p := LoadPage(page, nil)
	dm := p.DisplayMatrix()
	// For a {0,0,W,H} MediaBox at rotation 0, the display matrix flips y:
	// maps (0,0)->(0,H) and (0,H)->(0,0). Verify the y-flip corner mapping.
	topLeftUser := crt.PointF{X: 0, Y: 792}
	got := dm.Transform(topLeftUser)
	if !approx(got.X, 0) || !approx(got.Y, 0) {
		t.Errorf("DisplayMatrix maps (0,792)->%+v, want (0,0)", got)
	}
	bottomLeftUser := crt.PointF{X: 0, Y: 0}
	got2 := dm.Transform(bottomLeftUser)
	if !approx(got2.X, 0) || !approx(got2.Y, 792) {
		t.Errorf("DisplayMatrix maps (0,0)->%+v, want (0,792)", got2)
	}
}
