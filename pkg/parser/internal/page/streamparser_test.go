// Vectors ported verbatim from core/fpdfapi/page/cpdf_streamparser_unittest.cpp
// and cpdf_streamcontentparser_unittest.cpp @ pdfium 0db284a42, plus the derived
// tokenizer/param-ring integration vectors pinned in the Phase F research spec.
package page

import (
	"bytes"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// --- cpdf_streamparser_unittest.cpp: ReadHexString ---

func TestReadHexString(t *testing.T) {
	cases := []struct {
		in      []byte
		setPos  uint32
		want    []byte
		wantPos uint32
	}{
		{[]byte("12ab>\x00"), 6, nil, 6},
		{[]byte("1A2b>abcd"), 0, []byte{0x1A, 0x2B}, 5},
		{[]byte("1A2b\x00"), 0, []byte{0x1A, 0x2B}, 5},
		{[]byte("1A2>asdf"), 0, []byte{0x1A, 0x20}, 4},
		{[]byte(">"), 0, nil, 1},
	}
	for i, c := range cases {
		p := newStreamParser(c.in)
		p.SetPos(c.setPos)
		got := p.readHexString()
		if !bytes.Equal(got, c.want) {
			t.Errorf("case %d: bytes = % x, want % x", i, got, c.want)
		}
		if p.GetPos() != c.wantPos {
			t.Errorf("case %d: pos = %d, want %d", i, p.GetPos(), c.wantPos)
		}
	}
}

// --- Derived integration vector A: ParseNextElement classification ---

func TestParseNextElementClassification(t *testing.T) {
	in := []byte("12 -3.5 /Name (str) [1 2] BT Tj")
	p := newStreamParser(in)

	type tok struct {
		typ  StreamElementType
		word string
	}
	want := []tok{
		{ElemNumber, "12"},
		{ElemNumber, "-3.5"},
		{ElemName, "/Name"},
		{ElemOther, ""}, // String "str"
		{ElemOther, ""}, // Array [1 2]
		{ElemKeyword, "BT"},
		{ElemKeyword, "Tj"},
	}
	for i, w := range want {
		typ := p.ParseNextElement()
		if typ != w.typ {
			t.Fatalf("token %d: type = %d, want %d", i, typ, w.typ)
		}
		if w.word != "" && string(p.GetWord()) != w.word {
			t.Errorf("token %d: word = %q, want %q", i, p.GetWord(), w.word)
		}
	}
	// Verify the two ElemOther objects are the expected kinds.
	p2 := newStreamParser(in)
	p2.ParseNextElement() // 12
	p2.ParseNextElement() // -3.5
	p2.ParseNextElement() // /Name
	if p2.ParseNextElement() != ElemOther || p2.GetObject().Type() != objects.TypeString {
		t.Errorf("expected String object for (str)")
	}
	if got := p2.GetObject().GetString(); got != "str" {
		t.Errorf("string value = %q, want %q", got, "str")
	}
	if p2.ParseNextElement() != ElemOther || p2.GetObject().Type() != objects.TypeArray {
		t.Errorf("expected Array object for [1 2]")
	}
	arr := objects.ToArray(p2.GetObject())
	if arr.Len() != 2 || arr.GetIntegerAt(0) != 1 || arr.GetIntegerAt(1) != 2 {
		t.Errorf("array = %v, want [1 2]", arr)
	}
}

// --- Derived integration vector F: comment skip ---

func TestParseNextElementCommentSkip(t *testing.T) {
	p := newStreamParser([]byte("% comment\n42"))
	if typ := p.ParseNextElement(); typ != ElemNumber || string(p.GetWord()) != "42" {
		t.Errorf("got type=%d word=%q, want Number 42", typ, p.GetWord())
	}
}

// --- ReadString escape machine spot checks (cpdf_streamparser.cpp:505) ---

func TestReadStringEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"(abc)", "abc"},
		{"(a\\(b\\)c)", "a(b)c"},
		{"((nested))", "(nested)"},
		{"(a\\nb)", "a\nb"},
		{"(\\101)", "A"}, // octal 101 = 'A'
		{"(\\1010)", "A0"},
	}
	for _, c := range cases {
		p := newStreamParser([]byte(c.in))
		// skip the leading '(' the way readNextObject would: ParseNextElement
		// routes a literal string through ElemOther -> a String object.
		if typ := p.ParseNextElement(); typ != ElemOther {
			t.Fatalf("input %q: expected ElemOther", c.in)
		}
		got := p.GetObject().GetString()
		if got != c.want {
			t.Errorf("ReadString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
