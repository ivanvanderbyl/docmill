// Ported from core/fpdfapi/parser/cpdf_simple_parser_unittest.cpp @ pdfium
// 0db284a42.
package syntax

import "testing"

func TestSimpleParserGetWord(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{" \t \x00 \n", ""},
		{"%this is a test case\r\n%2nd line", ""},
		{"/", ""},
		{"/99", ""},
		{"/99}", "/99"},
		{" /Tester ", "/Tester"},
		{"\t(nice day)!\n ", "(nice day)"},
		{"\t(It is a (long) day)!\n ", "(It is a (long) day)"},
		{"\t(It is a \\(long\\) day!)hi\n ", "(It is a \\(long\\) day!)"},
		{"<", "<"},
		{">", ">"},
		{" \n<4545acdfedertt>abc ", "<4545acdfedertt>"},
		{" \n<4545a<ed>ertt>abc ", "<4545a<ed>"},
		{"<</oc 234 /color 2 3 R>>", "<<"},
		{"\t\t<< /abc>>", "<<"},
		{"(\\", "(\\"},
		{"> little bear", ">"},
		{") another bear", ")"},
		{">> end ", ">>"},
		{"(sdfgfgbcv", "(sdfgfgbcv"},
		{"}", "}"},
		{"apple pear", "apple"},
		{" pi=3.1415 ", "pi=3.1415"},
		{" p t x c ", "p"},
		{" pt\x00xc ", "pt"},
		{" $^&&*\t\x00sdff ", "$^&&*"},
		{"\n\r+3.5656 -11.0", "+3.5656"},
	}
	for _, tc := range cases {
		got := NewSimpleParser([]byte(tc.in)).GetWord()
		if got != tc.want {
			t.Errorf("GetWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSimpleParserSequence(t *testing.T) {
	// Bug358381390: <> is an empty-body hex token returned whole.
	sp := NewSimpleParser([]byte("1 beginbfchar\n<01> <>\nendbfchar\n1 beginbfchar"))
	want := []string{"1", "beginbfchar", "<01>", "<>", "endbfchar", "1", "beginbfchar", ""}
	for i, w := range want {
		if got := sp.GetWord(); got != w {
			t.Errorf("token %d = %q, want %q", i, got, w)
		}
	}
}
