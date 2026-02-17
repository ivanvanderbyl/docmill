package docmill

import (
	"strings"
	"unicode"
)

type PageQuality struct {
	AlnumRatio          float64
	MeaningfulWordRatio float64
	WordCount           int
	CharCount           int
	NonWhitespaceCount  int
	IsLowQuality        bool
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

	var alnumCount int
	for _, r := range full {
		q.CharCount++
		if !unicode.IsSpace(r) {
			q.NonWhitespaceCount++
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnumCount++
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

	if q.AlnumRatio < 0.3 {
		q.IsLowQuality = true
	}
	if q.NonWhitespaceCount < 3 {
		q.IsLowQuality = true
	}
	if q.WordCount > 5 && q.MeaningfulWordRatio < 0.1 {
		q.IsLowQuality = true
	}

	return q
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
