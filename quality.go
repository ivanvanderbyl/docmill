package docmill

import (
	"strings"
	"unicode"
)

type PageQuality struct {
	AlnumRatio           float64
	MeaningfulWordRatio  float64
	ReplacementCharRatio float64
	FragmentedWordRatio  float64
	PUARatio             float64
	WordCount            int
	CharCount            int
	NonWhitespaceCount   int
	IsLowQuality         bool
}

func assessPageQuality(words []EnrichedWord) PageQuality {
	var q PageQuality
	q.WordCount = len(words)

	var allText strings.Builder
	for _, w := range words {
		allText.WriteString(w.Text)
		allText.WriteRune(' ')
	}
	full := allText.String()

	var alnumCount, replacementCount, puaCount int
	for _, r := range full {
		q.CharCount++
		if !unicode.IsSpace(r) {
			q.NonWhitespaceCount++
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnumCount++
		}
		if r == '\uFFFD' {
			replacementCount++
		}
		if (r >= 0xE000 && r <= 0xF8FF) || (r >= 0xF0000 && r <= 0xFFFFD) {
			puaCount++
		}
	}

	if q.CharCount > 0 {
		q.AlnumRatio = float64(alnumCount) / float64(q.CharCount)
	}

	var meaningfulCount int
	for _, w := range words {
		if isMeaningfulWord(w.Text) {
			meaningfulCount++
		}
	}
	if q.WordCount > 0 {
		q.MeaningfulWordRatio = float64(meaningfulCount) / float64(q.WordCount)
	}

	if q.NonWhitespaceCount > 0 {
		q.ReplacementCharRatio = float64(replacementCount) / float64(q.NonWhitespaceCount)
		q.PUARatio = float64(puaCount) / float64(q.NonWhitespaceCount)
	}

	var singleCharWords int
	for _, w := range words {
		if len([]rune(w.Text)) == 1 && !isSingleCharWord(w.Text) {
			singleCharWords++
		}
	}
	if q.WordCount > 10 {
		q.FragmentedWordRatio = float64(singleCharWords) / float64(q.WordCount)
	}

	if q.AlnumRatio < 0.3 {
		q.IsLowQuality = true
	}
	if q.NonWhitespaceCount < 3 {
		q.IsLowQuality = true
	}
	if q.WordCount > 5 && q.MeaningfulWordRatio < 0.1 {
		q.IsLowQuality = true
	}
	if q.ReplacementCharRatio > 0.05 {
		q.IsLowQuality = true
	}
	if q.FragmentedWordRatio > 0.5 {
		q.IsLowQuality = true
	}
	if q.PUARatio > 0.1 {
		q.IsLowQuality = true
	}

	return q
}

func isSingleCharWord(s string) bool {
	r := []rune(s)[0]
	if r == 'I' || r == 'a' || r == 'A' {
		return true
	}
	if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return true
	}
	return false
}

func isMeaningfulWord(s string) bool {
	if len([]rune(s)) < 3 {
		return false
	}
	for _, r := range s {
		switch unicode.ToLower(r) {
		case 'a', 'e', 'i', 'o', 'u':
			return true
		}
	}
	return false
}
