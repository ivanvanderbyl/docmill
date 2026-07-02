// Verbatim port of cpdf_cmapparser_unittest.cpp @ pdfium 0db284a42.
package font

import "testing"

func TestGetCode(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"", 0},
		{"<", 0},
		{"<c2", 194},
		{"<A2", 162},
		{"<Af2", 2802},
		{"<A2z", 162}, // stops at non-hex 'z'
		{"12", 12},
		{"12d", 12}, // stops at non-decimal 'd'
		{"128", 128},
		{"<FFFFFFFF", 4294967295},
		{"<100000000", 0}, // uint32 overflow
	}
	for _, c := range cases {
		if got := getCMapCode(c.in); got != c.want {
			t.Errorf("GetCode(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestGetCodeRange(t *testing.T) {
	if _, ok := getCodeRange("", ""); ok {
		t.Error(`GetCodeRange("","") should be nullopt`)
	}
	if _, ok := getCodeRange("A", ""); ok {
		t.Error(`GetCodeRange("A","") should be nullopt (must start with <)`)
	}
	if _, ok := getCodeRange("<aaaaaaaaaa>", ""); ok {
		t.Error("GetCodeRange char_size>4 should be nullopt")
	}

	r, ok := getCodeRange("<12345678>", "<87654321>")
	if !ok || r.charSize != 4 {
		t.Fatalf("GetCodeRange 4-byte: ok=%v charSize=%d", ok, r.charSize)
	}
	wantLower := [4]uint8{0x12, 0x34, 0x56, 0x78}
	wantUpper := [4]uint8{0x87, 0x65, 0x43, 0x21}
	if r.lower != wantLower || r.upper != wantUpper {
		t.Errorf("GetCodeRange 4-byte lower=%v upper=%v", r.lower, r.upper)
	}

	r, ok = getCodeRange("<a1>", "<F3>")
	if !ok || r.charSize != 1 || r.lower[0] != 161 || r.upper[0] != 243 {
		t.Errorf("GetCodeRange <a1>/<F3>: ok=%v %+v", ok, r)
	}

	r, ok = getCodeRange("<a1>", "")
	if !ok || r.charSize != 1 || r.lower[0] != 161 || r.upper[0] != 0 {
		t.Errorf("GetCodeRange short upper: ok=%v %+v", ok, r)
	}
}
