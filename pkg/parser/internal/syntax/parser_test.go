// Ported from core/fpdfapi/parser/cpdf_syntax_parser_unittest.cpp @ pdfium
// 0db284a42, plus object-parsing coverage of GetObjectBody/GetIndirectObject.
package syntax

import (
	"bytes"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

type holder struct{ m map[uint32]objects.Object }

func (h holder) GetOrParseIndirectObject(n uint32) objects.Object { return h.m[n] }

func TestReadHexString(t *testing.T) {
	cases := []struct {
		in      string
		setPos  int // -1 = none
		want    []byte
		wantPos int
	}{
		{"", -1, []byte{}, 0},
		{"  ", -1, []byte{}, 2},
		{"z12b", -1, []byte{0x12, 0xb0}, 4},
		{"*<&*#$^&@1", -1, []byte{0x10}, 10},
		{"\x80zab", -1, []byte{0xab}, 4},
		{"\xffzab", -1, []byte{0xab}, 4},
		{"1A2b>abcd", -1, []byte{0x1a, 0x2b}, 5},
		{"1A2b", -1, []byte{0x1a, 0x2b}, 4},
		{"12abz", -1, []byte{0x12, 0xab}, 5},
		{"1A2>asdf", -1, []byte{0x1a, 0x20}, 4},
		{"1A2zasdf", -1, []byte{0x1a, 0x2a, 0xdf}, 8},
		{">", -1, []byte{}, 1},
		// Out-of-range positions read nothing (SetPos clamps).
		{"12ab>", 5, []byte{}, 5},
		{"12ab>", 6, []byte{}, 5},
		{"12ab>", 0, []byte{0x12, 0xab}, 5}, // '>' terminator is consumed
	}
	for _, tc := range cases {
		p := New([]byte(tc.in))
		if tc.setPos >= 0 {
			p.SetPos(tc.setPos)
		}
		got := p.ReadHexString()
		if !bytes.Equal(got, tc.want) {
			t.Errorf("ReadHexString(%q, setPos=%d) = % x, want % x", tc.in, tc.setPos, got, tc.want)
		}
		if p.GetPos() != tc.wantPos {
			t.Errorf("ReadHexString(%q) pos = %d, want %d", tc.in, p.GetPos(), tc.wantPos)
		}
	}
}

func TestGetInvalidReference(t *testing.T) {
	p := New([]byte("4294967295 0 R"))
	if obj := p.GetObjectBody(nil); obj != nil {
		t.Errorf("expected nil for invalid reference, got %v", obj.Type())
	}
}

func TestPeekNextWord(t *testing.T) {
	p := New([]byte("    WORD "))
	if w := p.PeekNextWord(); w != "WORD" {
		t.Errorf("PeekNextWord = %q, want WORD", w)
	}
	if w, _ := p.GetNextWord(); w != "WORD" {
		t.Errorf("GetNextWord after peek = %q, want WORD", w)
	}
}

func TestReadStringEscapes(t *testing.T) {
	cases := []struct {
		in   string // includes the leading '(' which GetObjectBody consumes
		want string
	}{
		{"(hello)", "hello"},
		{"(a\\nb)", "a\nb"},
		{"(a\\(b\\))", "a(b)"},
		{"(\\101)", "A"},               // octal 101 = 'A'
		{"(line\\\ncont)", "linecont"}, // backslash-newline continuation
		{"(nested (parens) ok)", "nested (parens) ok"},
	}
	for _, tc := range cases {
		p := New([]byte(tc.in))
		obj := p.GetObjectBody(nil)
		s := objects.ToString(obj)
		if s == nil {
			t.Errorf("GetObjectBody(%q) is not a String", tc.in)
			continue
		}
		if s.GetString() != tc.want {
			t.Errorf("ReadString(%q) = %q, want %q", tc.in, s.GetString(), tc.want)
		}
	}
}

func TestGetObjectBodyScalars(t *testing.T) {
	if b := objects.ToBoolean(New([]byte("true")).GetObjectBody(nil)); b == nil || !b.Value() {
		t.Error("true did not parse to Boolean(true)")
	}
	if b := objects.ToBoolean(New([]byte("false")).GetObjectBody(nil)); b == nil || b.Value() {
		t.Error("false did not parse to Boolean(false)")
	}
	if n := New([]byte("null")).GetObjectBody(nil); n == nil || n.Type() != objects.TypeNull {
		t.Error("null did not parse to Null")
	}
	if num := objects.ToNumber(New([]byte("42")).GetObjectBody(nil)); num == nil || num.GetInteger() != 42 {
		t.Error("42 did not parse to Number(42)")
	}
	if num := objects.ToNumber(New([]byte("3.5")).GetObjectBody(nil)); num == nil || num.GetNumber() != 3.5 {
		t.Error("3.5 did not parse to Number(3.5)")
	}
	if name := objects.ToName(New([]byte("/Foo#20Bar")).GetObjectBody(nil)); name == nil || name.GetString() != "Foo Bar" {
		t.Errorf("/Foo#20Bar did not decode (#20 -> space)")
	}
	// "5 0" parses as Number(5), leaving the cursor before 0.
	if num := objects.ToNumber(New([]byte("5 0")).GetObjectBody(nil)); num == nil || num.GetInteger() != 5 {
		t.Error("'5 0' did not parse to Number(5)")
	}
}

func TestGetObjectBodyArray(t *testing.T) {
	arr := objects.ToArray(New([]byte("[1 2 3]")).GetObjectBody(nil))
	if arr == nil {
		t.Fatal("[1 2 3] did not parse to Array")
	}
	if arr.Len() != 3 {
		t.Fatalf("array len = %d, want 3", arr.Len())
	}
	for i, want := range []int{1, 2, 3} {
		if arr.GetIntegerAt(i) != want {
			t.Errorf("array[%d] = %d, want %d", i, arr.GetIntegerAt(i), want)
		}
	}
}

func TestGetObjectBodyDictionary(t *testing.T) {
	dict := objects.ToDictionary(New([]byte("<< /Type /Page /Count 3 >>")).GetObjectBody(nil))
	if dict == nil {
		t.Fatal("did not parse to Dictionary")
	}
	if dict.GetNameFor("Type") != "Page" {
		t.Errorf("/Type = %q, want Page", dict.GetNameFor("Type"))
	}
	if dict.GetIntegerFor("Count") != 3 {
		t.Errorf("/Count = %d, want 3", dict.GetIntegerFor("Count"))
	}
}

func TestGetObjectBodyReference(t *testing.T) {
	h := holder{m: map[uint32]objects.Object{7: objects.NewNumberFromInt(99)}}
	ref := objects.ToReference(New([]byte("7 0 R")).GetObjectBody(h))
	if ref == nil {
		t.Fatal("7 0 R did not parse to Reference")
	}
	if ref.GetRefObjNum() != 7 {
		t.Errorf("ref objnum = %d, want 7", ref.GetRefObjNum())
	}
	if ref.GetInteger() != 99 {
		t.Errorf("resolved ref = %d, want 99", ref.GetInteger())
	}
}

func TestGetIndirectObject(t *testing.T) {
	p := New([]byte("5 0 obj 42 endobj"))
	obj := p.GetIndirectObject(nil)
	if obj == nil {
		t.Fatal("did not parse indirect object")
	}
	if obj.GetObjNum() != 5 || obj.GetGenNum() != 0 {
		t.Errorf("objnum/gennum = %d/%d, want 5/0", obj.GetObjNum(), obj.GetGenNum())
	}
	if num := objects.ToNumber(obj); num == nil || num.GetInteger() != 42 {
		t.Error("body did not parse to Number(42)")
	}
}

func TestGetObjectBodyStream(t *testing.T) {
	pdf := "<< /Length 5 >>\nstream\nhello\nendstream"
	stream := objects.ToStream(New([]byte(pdf)).GetObjectBody(nil))
	if stream == nil {
		t.Fatal("did not parse to Stream")
	}
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataRaw()
	if !bytes.Equal(acc.GetSpan(), []byte("hello")) {
		t.Errorf("stream data = %q, want hello", acc.GetSpan())
	}
}

// TestGetObjectBodyStreamIndirectLength covers a stream whose /Length is an
// indirect reference: GetNumberFor cannot resolve it at parse time, so readStream
// falls back to findStreamEndPos. With CRLF endings and a trailing 'endobj' after
// 'endstream', the end-scan must still locate the stream end (regression: the
// fallback's lower-bound guard compared against a cursor the 'endobj' search had
// advanced past 'endstream', spuriously rejecting the match and returning nil).
func TestGetObjectBodyStreamIndirectLength(t *testing.T) {
	pdf := "<< /Length 9 0 R >>\r\nstream\r\nhello\r\nendstream\r\nendobj\r\n"
	stream := objects.ToStream(New([]byte(pdf)).GetObjectBody(nil))
	if stream == nil {
		t.Fatal("did not parse to Stream")
	}
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataRaw()
	if !bytes.Equal(acc.GetSpan(), []byte("hello")) {
		t.Errorf("stream data = %q, want hello", acc.GetSpan())
	}
}
