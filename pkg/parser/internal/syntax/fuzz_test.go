package syntax

import "testing"

// FuzzGetObjectBody is the Phase C robustness gate: arbitrary bytes through the
// recursive object parser must not panic or loop forever (the oracle is "returns
// without crashing"). The kParserMaxRecursionDepth guard bounds nesting.
func FuzzGetObjectBody(f *testing.F) {
	seeds := []string{
		"[1 2 3]",
		"<< /Type /Page /Count 3 >>",
		"(hello \\(world\\) \\101)",
		"<48656c6c6f>",
		"true false null",
		"7 0 R",
		"<< /Length 5 >>\nstream\nhello\nendstream",
		"4294967295 0 R",
		"[[[[[[[[[[",
		"<<<<<<<<<<",
		"((((((((((",
		"<< /A << /B << /C 1 >> >> >>",
		"/Name#GG#",
		"5 0",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = New(data).GetObjectBody(nil)
		_ = New(data).GetIndirectObject(nil)
		NewSimpleParser(data).GetWord()
	})
}
