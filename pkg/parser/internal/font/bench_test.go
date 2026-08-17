package font

import "testing"

// BenchmarkFontWeight measures the memoized path GetWordArray hits once per
// word during word-level extraction.
func BenchmarkFontWeight(b *testing.B) {
	f := &Font{baseFontName: "ABCDEF+Montserrat-SemiBoldItalic"}
	b.ReportAllocs()
	for b.Loop() {
		_ = f.FontWeight()
	}
}

// BenchmarkFontWeightUncached measures the pre-memoization cost: the name-token
// scan re-ran (and allocated) on every probe.
func BenchmarkFontWeightUncached(b *testing.B) {
	f := &Font{baseFontName: "ABCDEF+Montserrat-SemiBoldItalic"}
	b.ReportAllocs()
	for b.Loop() {
		_ = f.resolveWeight()
	}
}
