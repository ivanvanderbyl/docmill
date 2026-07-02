package benchdp

import (
	"strings"
	"unicode"
)

// EvaluateMarkdown scores predicted Markdown against groundTruth.
//
// ReadingOrderNID is the DP-Bench Normalized Indel Distance (NID) for reading
// order: a character-level indel (insertion/deletion only) similarity over the
// document's text in reading order, with table rows excluded —
// NID = 1 - indel/(len_gt + len_pred) = 2·LCS/(len_gt + len_pred). It matches
// the published metric scale (raw-text-in-order parsers score high; interleaved
// or out-of-order text scores low). Extraction is token F1, table shape an
// edit-distance similarity over row signatures, heading hierarchy a heading
// signature F1 — those remain deterministic approximations of the published
// TEDS / MHS labels.
func EvaluateMarkdown(groundTruth, predicted string) Scores {
	return Scores{
		ExtractionAccuracy: tokenF1(markdownTokens(groundTruth), markdownTokens(predicted)),
		ReadingOrderNID:    normalizedIndelSimilarity([]rune(readingOrderText(groundTruth)), []rune(readingOrderText(predicted))),
		TableStructureTEDS: sequenceSimilarity(tableSignatures(groundTruth), tableSignatures(predicted)),
		HeadingLevelMHS:    tokenF1(headingSignatures(groundTruth), headingSignatures(predicted)),
	}
}

// readingOrderText returns the document's reading-order text for NID: every line
// joined in order with markdown table rows dropped (tables/figures are excluded
// from reading-order scoring) and reduced to lowercased alphanumeric words, so
// the metric measures the ORDER of the text content, not its formatting (a
// heading level or emphasis marker must not change reading order).
func readingOrderText(markdown string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(markdown, "\n") {
		if tableColumnCount(line) >= 2 {
			continue
		}
		b.WriteString(line)
		b.WriteByte(' ')
	}
	return strings.Join(words(b.String()), " ")
}

// normalizedIndelSimilarity is the Normalized Indel Distance similarity:
// 2·LCS(a,b) / (len(a)+len(b)), equal to 1 - indelDistance/(len(a)+len(b)).
// Empty-vs-empty is a perfect match.
func normalizedIndelSimilarity(a, b []rune) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	return 2 * float64(lcsLength(a, b)) / float64(len(a)+len(b))
}

// lcsLength returns the length of the longest common subsequence of a and b,
// computed with an O(min(len)) rolling row.
func lcsLength(a, b []rune) int {
	if len(b) > len(a) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func markdownTokens(s string) []string {
	return words(s)
}

func words(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func tokenF1(want, got []string) float64 {
	if len(want) == 0 && len(got) == 0 {
		return 1
	}
	if len(want) == 0 || len(got) == 0 {
		return 0
	}
	wantCounts := counts(want)
	gotCounts := counts(got)
	matches := 0
	for token, wantCount := range wantCounts {
		gotCount := gotCounts[token]
		if gotCount < wantCount {
			matches += gotCount
		} else {
			matches += wantCount
		}
	}
	if matches == 0 {
		return 0
	}
	precision := float64(matches) / float64(len(got))
	recall := float64(matches) / float64(len(want))
	return 2 * precision * recall / (precision + recall)
}

func counts(values []string) map[string]int {
	out := make(map[string]int, len(values))
	for _, value := range values {
		out[value]++
	}
	return out
}

func tableSignatures(markdown string) []string {
	var signatures []string
	for line := range strings.SplitSeq(markdown, "\n") {
		cols := tableColumnCount(line)
		if cols < 2 {
			continue
		}
		kind := "row"
		if isSeparatorRow(line) {
			kind = "sep"
		}
		signatures = append(signatures, kind+":"+itoa(cols))
	}
	return signatures
}

func tableColumnCount(line string) int {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return 0
	}
	parts := strings.Split(trimmed, "|")
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func isSeparatorRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, r := range trimmed {
		switch r {
		case '|', '-', ':', ' ':
		default:
			return false
		}
	}
	return strings.Contains(trimmed, "-")
}

func headingSignatures(markdown string) []string {
	var signatures []string
	for line := range strings.SplitSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		level := headingLevel(trimmed)
		if level == 0 {
			continue
		}
		text := strings.TrimSpace(trimmed[level:])
		if text == "" {
			continue
		}
		signatures = append(signatures, itoa(level)+":"+strings.Join(words(text), " "))
	}
	return signatures
}

func headingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

func sequenceSimilarity(want, got []string) float64 {
	if len(want) == 0 && len(got) == 0 {
		return 1
	}
	maxLen := max(len(got), len(want))
	if maxLen == 0 {
		return 0
	}
	distance := levenshtein(want, got)
	score := 1 - float64(distance)/float64(maxLen)
	if score < 0 {
		return 0
	}
	return score
}

func levenshtein(a, b []string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
