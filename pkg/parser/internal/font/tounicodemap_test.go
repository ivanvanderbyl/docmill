// Verbatim port of cpdf_tounicodemap_unittest.cpp @ pdfium 0db284a42.
package font

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

func mapFromString(s string) *toUnicodeMap {
	stream := objects.NewStreamFromData([]byte(s), objects.NewDictionary())
	return newToUnicodeMap(stream)
}

func TestStringToCode(t *testing.T) {
	type tc struct {
		in   string
		want uint32
		ok   bool
	}
	cases := []tc{
		{"<0001>", 1, true},
		{"<c2>", 194, true},
		{"<A2>", 162, true},
		{"<Af2>", 2802, true},
		{"<FFFFFFFF>", 4294967295, true},
		{"<00\n0\r1>", 1, true},
		{"<c 2>", 194, true},
		{"<A2\r\n>", 162, true},
		// overflow / invalid
		{"<100000000>", 0, false},
		{"<1abcdFFFF>", 0, false},
		{"", 0, false},
		{"<>", 0, false},
		{"12", 0, false},
		{"<12", 0, false},
		{"12>", 0, false},
		{"<1-7>", 0, false},
		{"00AB", 0, false},
		{"<00NN>", 0, false},
	}
	for _, c := range cases {
		got, ok := stringToCode(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("StringToCode(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestStringToWideString(t *testing.T) {
	type tc struct {
		in   string
		want []uint16
	}
	cases := []tc{
		{"", nil},
		{"1234", nil},
		{"<c2", nil},
		{"<c2D2", nil},
		{"c2ab>", nil},
		{"<c2ab>", []uint16{0xc2ab}},
		{"<c2abab>", []uint16{0xc2ab}},
		{"<c2abFaAb>", []uint16{0xc2ab, 0xfaab}},
		{"<c2abFaAb12>", []uint16{0xc2ab, 0xfaab}},
		{"<c2ab FaAb>", []uint16{0xc2ab, 0xfaab}},
		{"<c2ab FaAb12>", []uint16{0xc2ab, 0xfaab}},
		{"<c2ab FaAb 12>", []uint16{0xc2ab, 0xfaab}},
		{"< c 2 a b  F a A b  1 2 >", []uint16{0xc2ab, 0xfaab}},
	}
	for _, c := range cases {
		got := stringToWideString(c.in)
		if !equalUint16(got, c.want) {
			t.Errorf("StringToWideString(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func equalUint16(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHandleBeginBFCharBadCount(t *testing.T) {
	for _, in := range []string{
		"1 beginbfchar<1><0041><2><0042>endbfchar",
		"3 beginbfchar<1><0041><2><0042>endbfchar",
	} {
		m := mapFromString(in)
		if got := m.ReverseLookup(0x0041); got != 0 {
			t.Errorf("%q ReverseLookup(0x41)=%d want 0", in, got)
		}
		if got := m.ReverseLookup(0x0042); got != 0 {
			t.Errorf("%q ReverseLookup(0x42)=%d want 0", in, got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(1); got != 0 {
			t.Errorf("%q count(1)=%d want 0", in, got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(2); got != 0 {
			t.Errorf("%q count(2)=%d want 0", in, got)
		}
	}
}

func TestHandleBeginBFCharTolerateOutOfSpecCount(t *testing.T) {
	in := "112 beginbfchar" +
		"<0000><0008><0001><0009><0002><000A><0003><000B><0004><000C>" +
		"<0005><000D><0006><000E><0007><000F><0008><0000><0009><0001>" +
		"<000A><0002><000B><0003><000C><0004><000D><0005><000E><0006>" +
		"<000F><0007><0010><0018><0011><0019><0012><001A><0013><001B>" +
		"<0014><001C><0015><001D><0016><001E><0017><001F><0018><0010>" +
		"<0019><0011><001A><0012><001B><0013><001C><0014><001D><0015>" +
		"<001E><0016><001F><0017><0020><0028><0021><0029><0022><002A>" +
		"<0023><002B><0024><002C><0025><002D><0026><002E><0027><002F>" +
		"<0028><0020><0029><0021><002A><0022><002B><0023><002C><0024>" +
		"<002D><0025><002E><0026><002F><0027><0030><0038><0031><0039>" +
		"<0032><003A><0033><003B><0034><003C><0035><003D><0036><003E>" +
		"<0037><003F><0038><0030><0039><0031><003A><0032><003B><0033>" +
		"<003C><0034><003D><0035><003E><0036><003F><0037><0040><0048>" +
		"<0041><0049><0042><004A><0043><004B><0044><004C><0045><004D>" +
		"<0046><004E><0047><004F><0048><0040><0049><0041><004A><0042>" +
		"<004B><0043><004C><0044><004D><0045><004E><0046><004F><0047>" +
		"<0050><0058><0051><0059><0052><005A><0053><005B><0054><005C>" +
		"<0055><005D><0056><005E><0057><005F><0058><0050><0059><0051>" +
		"<005A><0052><005B><0053><005C><0054><005D><0055><005E><0056>" +
		"<005F><0057><0060><0068><0061><0069><0062><006A><0063><006B>" +
		"<0064><006C><0065><006D><0066><006E><0067><006F><0068><0060>" +
		"<0069><0061><006A><0062><006B><0063><006C><0064><006D><0065>" +
		"<006E><0066><006F><0067>" +
		"endbfchar"
	m := mapFromString(in)
	if got := m.ReverseLookup(0x0001); got != 9 {
		t.Errorf("ReverseLookup(0x0001)=%d want 9", got)
	}
	if got := m.ReverseLookup(0x0067); got != 111 {
		t.Errorf("ReverseLookup(0x0067)=%d want 111", got)
	}
	if got := m.getUnicodeCountByCharcodeForTesting(1); got != 1 {
		t.Errorf("count(1)=%d want 1", got)
	}
	if got := m.getUnicodeCountByCharcodeForTesting(111); got != 1 {
		t.Errorf("count(111)=%d want 1", got)
	}
}

func TestHandleBeginBFRangeRejectsInvalidCidValues(t *testing.T) {
	{
		m := mapFromString("1 beginbfrange<FFFFFFFF><FFFFFFFF>[<0041>]endbfrange")
		if got := m.Lookup(0xffffffff); len(got) != 0 {
			t.Errorf("Lookup(0xffffffff)=%v want empty", got)
		}
	}
	{
		m := mapFromString("1 beginbfrange<FFFFFFFF><FFFFFFFF><0042>endbfrange")
		if got := m.Lookup(0xffffffff); len(got) != 0 {
			t.Errorf("Lookup(0xffffffff)=%v want empty", got)
		}
	}
	{
		m := mapFromString("1 beginbfrange<FFFFFFFF><FFFFFFFF><00410042>endbfrange")
		if got := m.Lookup(0xffffffff); len(got) != 0 {
			t.Errorf("Lookup(0xffffffff)=%v want empty", got)
		}
	}
	{
		m := mapFromString("1 beginbfrange<0001><10000>[<0041>]endbfrange")
		for _, cc := range []uint32{0xffffffff, 0x0001, 0xffff, 0x10000} {
			if got := m.Lookup(cc); len(got) != 0 {
				t.Errorf("Lookup(%#x)=%v want empty", cc, got)
			}
		}
	}
	{
		m := mapFromString("1 beginbfrange<10000><10001>[<0041>]endbfrange")
		for _, cc := range []uint32{0x10000, 0x10001} {
			if got := m.Lookup(cc); len(got) != 0 {
				t.Errorf("Lookup(%#x)=%v want empty", cc, got)
			}
		}
	}
	{
		m := mapFromString("1 beginbfrange<0006><0004>[<0041>]endbfrange")
		for _, cc := range []uint32{0x0004, 0x0005, 0x0006} {
			if got := m.Lookup(cc); len(got) != 0 {
				t.Errorf("Lookup(%#x)=%v want empty", cc, got)
			}
		}
	}
}

func TestHandleBeginBFRangeRejectsMismatchedBracket(t *testing.T) {
	m := mapFromString("1 beginbfrange<3><3>[<0041>}endbfrange")
	if got := m.ReverseLookup(0x0041); got != 0 {
		t.Errorf("ReverseLookup(0x41)=%d want 0", got)
	}
	if got := m.getUnicodeCountByCharcodeForTesting(3); got != 0 {
		t.Errorf("count(3)=%d want 0", got)
	}
}

func TestHandleBeginBFRangeBadCount(t *testing.T) {
	for _, in := range []string{
		"1 beginbfrange<1><2><0040><4><5><0050>endbfrange",
		"3 beginbfrange<1><2><0040><4><5><0050>endbfrange",
	} {
		m := mapFromString(in)
		for u := rune(0x0039); u < 0x0053; u++ {
			if got := m.ReverseLookup(u); got != 0 {
				t.Errorf("%q ReverseLookup(%#x)=%d want 0", in, u, got)
			}
		}
		for cc := range uint32(7) {
			if got := m.getUnicodeCountByCharcodeForTesting(cc); got != 0 {
				t.Errorf("%q count(%d)=%d want 0", in, cc, got)
			}
		}
	}
}

func TestHandleBeginBFRangeGoodCount(t *testing.T) {
	m := mapFromString("2 beginbfrange<1><2><0040><4><5><0050>endbfrange")
	checks := []struct {
		u    rune
		want uint32
	}{
		{0x0039, 0}, {0x0040, 1}, {0x0041, 2}, {0x0042, 0},
		{0x0049, 0}, {0x0050, 4}, {0x0051, 5}, {0x0052, 0},
	}
	for _, c := range checks {
		if got := m.ReverseLookup(c.u); got != c.want {
			t.Errorf("ReverseLookup(%#x)=%d want %d", c.u, got, c.want)
		}
	}
	counts := []struct {
		cc   uint32
		want int
	}{
		{0, 0}, {1, 1}, {2, 1}, {3, 0}, {4, 1}, {5, 1}, {6, 0},
	}
	for _, c := range counts {
		if got := m.getUnicodeCountByCharcodeForTesting(c.cc); got != c.want {
			t.Errorf("count(%d)=%d want %d", c.cc, got, c.want)
		}
	}
}

func TestInsertIntoMultimap(t *testing.T) {
	{
		m := mapFromString("2 beginbfchar<1><0041><2><0042>endbfchar")
		if got := m.ReverseLookup(0x0041); got != 1 {
			t.Errorf("ReverseLookup(0x41)=%d want 1", got)
		}
		if got := m.ReverseLookup(0x0042); got != 2 {
			t.Errorf("ReverseLookup(0x42)=%d want 2", got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(1); got != 1 {
			t.Errorf("count(1)=%d want 1", got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(2); got != 1 {
			t.Errorf("count(2)=%d want 1", got)
		}
	}
	{
		m := mapFromString("2 beginbfrange<0><0><0041><0><0><0042>endbfrange")
		if got := m.ReverseLookup(0x0041); got != 0 {
			t.Errorf("ReverseLookup(0x41)=%d want 0", got)
		}
		if got := m.ReverseLookup(0x0042); got != 0 {
			t.Errorf("ReverseLookup(0x42)=%d want 0", got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(0); got != 2 {
			t.Errorf("count(0)=%d want 2", got)
		}
	}
	{
		m := mapFromString("1 beginbfrange<0><0>[<0041>]endbfrange\n1 beginbfchar<0><0041>endbfchar")
		if got := m.ReverseLookup(0x0041); got != 0 {
			t.Errorf("ReverseLookup(0x41)=%d want 0", got)
		}
		if got := m.getUnicodeCountByCharcodeForTesting(0); got != 1 {
			t.Errorf("count(0)=%d want 1", got)
		}
	}
}

func TestNonBmpUnicodeLookup(t *testing.T) {
	m := mapFromString("1 beginbfchar<01><d841de76>endbfchar")
	got := m.Lookup(0x01)
	want := []uint16{0xd841, 0xde76}
	if !equalUint16(got, want) {
		t.Errorf("Lookup(0x01)=%v want %v", got, want)
	}
}
