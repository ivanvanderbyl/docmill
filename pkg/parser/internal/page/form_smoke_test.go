package page

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// A page whose content does `/Fm1 Do`, where Fm1 is a Form XObject that draws
// text. Confirms form recursion produces a FormObject whose nested Objects()
// contain the text.
func TestFormRecursion(t *testing.T) {
	// Font dict reused.
	fontDict := objects.NewDictionary()
	fontDict.SetFor("Type", objects.NewName("Font"))
	fontDict.SetFor("Subtype", objects.NewName("Type1"))
	fontDict.SetFor("BaseFont", objects.NewName("Helvetica"))
	fontDict.SetFor("Encoding", objects.NewName("WinAnsiEncoding"))
	fontDict.SetFor("FirstChar", objects.NewNumberFromInt('H'))
	fontDict.SetFor("LastChar", objects.NewNumberFromInt('H'))
	w := objects.NewArray()
	w.Append(objects.NewNumberFromInt(600))
	fontDict.SetFor("Widths", w)

	fontRes := objects.NewDictionary()
	fontRes.SetFor("F1", fontDict)
	resources := objects.NewDictionary()
	resources.SetFor("Font", fontRes)

	// Form XObject stream drawing "H".
	formDict := objects.NewDictionary()
	formDict.SetFor("Type", objects.NewName("XObject"))
	formDict.SetFor("Subtype", objects.NewName("Form"))
	bbox := objects.NewArray()
	for _, v := range []int32{0, 0, 100, 100} {
		bbox.Append(objects.NewNumberFromInt(v))
	}
	formDict.SetFor("BBox", bbox)
	formStream := objects.NewStreamFromData([]byte("BT /F1 12 Tf 10 10 Td (H) Tj ET"), formDict)

	xobjRes := objects.NewDictionary()
	xobjRes.SetFor("Fm1", formStream)
	resources.SetFor("XObject", xobjRes)

	content := objects.NewStreamFromData([]byte("/Fm1 Do"), objects.NewDictionary())

	page := objects.NewDictionary()
	page.SetFor("Type", objects.NewName("Page"))
	mb := objects.NewArray()
	for _, v := range []int32{0, 0, 612, 792} {
		mb.Append(objects.NewNumberFromInt(v))
	}
	page.SetFor("MediaBox", mb)
	page.SetFor("Resources", resources)
	page.SetFor("Contents", content)

	p := LoadPage(page, nil)
	objs := p.Objects()
	if len(objs) != 1 {
		t.Fatalf("got %d top objects, want 1 (the form)", len(objs))
	}
	fo, ok := objs[0].(*FormObject)
	if !ok {
		t.Fatalf("object 0 is %T, want *FormObject", objs[0])
	}
	nested := fo.Objects()
	if len(nested) != 1 {
		t.Fatalf("form has %d nested objects, want 1 text", len(nested))
	}
	to, ok := nested[0].(*TextObject)
	if !ok {
		t.Fatalf("nested 0 is %T, want *TextObject", nested[0])
	}
	if to.Items()[0].CharCode != 'H' {
		t.Errorf("nested text char = %c, want H", to.Items()[0].CharCode)
	}
}
