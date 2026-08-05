package pdf

import (
	"math"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// ParagraphOptions tunes line/paragraph grouping. Zero values get sane defaults.
type ParagraphOptions struct {
	LineTolerance      float64 // max vertical-centre delta for cells on one line (default 4)
	ParagraphGapFactor float64 // new paragraph when line gap > factor*prevLineHeight (default 0.6)
	// EnableInlineFormatting opts paragraph text reconstruction into inline
	// bold/italic/code emission from each line's LineElement runs. OFF by
	// default; with it off the legacy line.Text path is used byte-for-byte.
	EnableInlineFormatting bool
}

func (o ParagraphOptions) withDefaults() ParagraphOptions {
	if o.LineTolerance <= 0 {
		o.LineTolerance = 4
	}
	if o.ParagraphGapFactor <= 0 {
		o.ParagraphGapFactor = 0.6
	}
	return o
}

// assembleParagraphs merges already-assembled visual lines (ParagraphTextLines)
// into paragraphs. Each returned block is one paragraph; Index is the smallest
// source cell index in the paragraph (for ordering vs tables).
func assembleParagraphs(lines []ParagraphTextLine, options ParagraphOptions) []markdownBlock {
	return mergeLines(lines, options.withDefaults())
}

func joinLineCellTexts(cells []page.TextCell) string {
	visible := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || isListSpacerText(text) {
			continue
		}
		visible = append(visible, cell)
	}
	if len(visible) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(visible[0].Text))
	for i := 1; i < len(visible); i++ {
		if shouldSeparateLineCells(visible, i) {
			builder.WriteByte(' ')
		}
		builder.WriteString(strings.TrimSpace(visible[i].Text))
	}
	return builder.String()
}

func shouldSeparateLineCells(cells []page.TextCell, index int) bool {
	current := cells[index]
	previous := cells[index-1]
	if isStandaloneHyphenText(current.Text) && index+1 < len(cells) && shouldCompactStandaloneHyphen(previous, current, cells[index+1]) {
		return false
	}
	if isStandaloneHyphenText(previous.Text) && index >= 2 && shouldCompactStandaloneHyphen(cells[index-2], previous, current) {
		return false
	}
	if isStandaloneApostropheText(current.Text) && shouldCompactApostropheWithLeft(previous, current) {
		return false
	}
	if isStandaloneApostropheText(previous.Text) && shouldCompactApostropheWithRight(previous, current) {
		return false
	}
	if isStandalonePeriodText(current.Text) && shouldCompactPeriodWithLeft(previous, current) {
		return false
	}
	if isStandalonePeriodText(previous.Text) && shouldCompactPeriodWithRight(previous, current) {
		return false
	}
	if shouldCompactTightAlphaNumericRuns(previous, current) {
		return false
	}
	return true
}

func shouldCompactStandaloneHyphen(left, hyphen, right page.TextCell) bool {
	if !isStandaloneHyphenText(hyphen.Text) {
		return false
	}
	if !hasCompactableHyphenBoundary(left.Text, right.Text) {
		return false
	}
	maxGap := maxCompactGlyphGap(left, hyphen, right)
	return horizontalGap(left.Box, hyphen.Box) <= maxGap && horizontalGap(hyphen.Box, right.Box) <= maxGap
}

func isStandaloneHyphenText(text string) bool {
	switch strings.TrimSpace(text) {
	case "-", "\u2010", "\u2011":
		return true
	default:
		return false
	}
}

func shouldCompactApostropheWithLeft(left, apostrophe page.TextCell) bool {
	if !isStandaloneApostropheText(apostrophe.Text) {
		return false
	}
	leftRune, ok := lastNonSpaceRune(left.Text)
	if !ok || !isAlphaNumeric(leftRune) {
		return false
	}
	return horizontalGap(left.Box, apostrophe.Box) <= maxCompactGlyphGap(left, apostrophe)
}

func shouldCompactApostropheWithRight(apostrophe, right page.TextCell) bool {
	if !isStandaloneApostropheText(apostrophe.Text) {
		return false
	}
	if !isShortApostropheSuffix(right.Text) {
		return false
	}
	rightRune, ok := firstNonSpaceRune(right.Text)
	if !ok || !isAlphaNumeric(rightRune) {
		return false
	}
	return horizontalGap(apostrophe.Box, right.Box) <= maxCompactGlyphGap(apostrophe, right)
}

func isStandaloneApostropheText(text string) bool {
	switch strings.TrimSpace(text) {
	case "'", "\u2019", "\u2018":
		return true
	default:
		return false
	}
}

func isShortApostropheSuffix(text string) bool {
	letters := 0
	for _, r := range strings.TrimSpace(text) {
		if !unicode.IsLetter(r) {
			break
		}
		letters++
		if letters > 2 {
			return false
		}
	}
	return letters > 0
}

func shouldCompactPeriodWithLeft(left, period page.TextCell) bool {
	if !isStandalonePeriodText(period.Text) {
		return false
	}
	leftRune, ok := lastNonSpaceRune(left.Text)
	if !ok || !isAlphaNumeric(leftRune) {
		return false
	}
	return horizontalGap(left.Box, period.Box) <= maxCompactGlyphGap(left, period)
}

func shouldCompactPeriodWithRight(period, right page.TextCell) bool {
	if !isStandalonePeriodText(period.Text) {
		return false
	}
	rightRune, ok := firstNonSpaceRune(right.Text)
	if !ok || !isAlphaNumeric(rightRune) {
		return false
	}
	return horizontalGap(period.Box, right.Box) <= maxPeriodContinuationGap(period, right)
}

func isStandalonePeriodText(text string) bool {
	return strings.TrimSpace(text) == "."
}

func shouldCompactTightAlphaNumericRuns(left, right page.TextCell) bool {
	leftRune, ok := lastNonSpaceRune(left.Text)
	if !ok || !isAlphaNumeric(leftRune) {
		return false
	}
	rightRune, ok := firstNonSpaceRune(right.Text)
	if !ok || !isAlphaNumeric(rightRune) {
		return false
	}
	return horizontalGap(left.Box, right.Box) <= maxTightTextRunGap(left, right)
}

func maxTightTextRunGap(cells ...page.TextCell) float64 {
	ref := 0.0
	for _, cell := range cells {
		if cell.FontSize > ref {
			ref = cell.FontSize
		}
		if height := cell.Box.Height(); height > ref {
			ref = height
		}
	}
	return math.Max(0.75, ref*0.12)
}

func hasCompactableHyphenBoundary(left, right string) bool {
	leftRune, ok := lastNonSpaceRune(left)
	if !ok {
		return false
	}
	rightRune, ok := firstNonSpaceRune(right)
	if !ok {
		return false
	}
	if !isAlphaNumeric(leftRune) || !isAlphaNumeric(rightRune) {
		return false
	}
	return unicode.IsLetter(leftRune) || unicode.IsLetter(rightRune)
}

func firstNonSpaceRune(text string) (rune, bool) {
	for _, r := range strings.TrimSpace(text) {
		return r, true
	}
	return 0, false
}

func lastNonSpaceRune(text string) (rune, bool) {
	var last rune
	ok := false
	for _, r := range strings.TrimSpace(text) {
		last = r
		ok = true
	}
	return last, ok
}

func isAlphaNumeric(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func maxCompactGlyphGap(cells ...page.TextCell) float64 {
	ref := 0.0
	for _, cell := range cells {
		if cell.FontSize > ref {
			ref = cell.FontSize
		}
		if height := cell.Box.Height(); height > ref {
			ref = height
		}
	}
	return math.Max(2.5, ref*0.35)
}

func maxPeriodContinuationGap(cells ...page.TextCell) float64 {
	ref := 0.0
	for _, cell := range cells {
		if cell.FontSize > ref {
			ref = cell.FontSize
		}
		if height := cell.Box.Height(); height > ref {
			ref = height
		}
	}
	return math.Max(0.75, ref*0.25)
}

func horizontalGap(left, right geom.Box) float64 {
	gap := right.L - left.R
	if gap < 0 {
		return 0
	}
	return gap
}

// mergeLines walks lines top-to-bottom, starting a new paragraph when the
// vertical gap to the previous line exceeds gapFactor times the previous line's
// height. Returns one block per paragraph in top-to-bottom order.
//
// When options.EnableInlineFormatting is set, each line's text contribution is
// reconstructed from its LineElement runs (bold/italic/code) via
// formatLineElements; otherwise the legacy line.Text path is used unchanged.
func mergeLines(lines []ParagraphTextLine, options ParagraphOptions) []markdownBlock {
	if len(lines) == 0 {
		return nil
	}
	gapFactor := options.ParagraphGapFactor

	var blocks []markdownBlock
	var paragraph []ParagraphTextLine

	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		parts := make([]string, 0, len(paragraph))
		boxes := make([]geom.Box, 0, len(paragraph))
		minIndex := paragraph[0].MinIndex
		fontSize := paragraph[0].FontSize
		for _, line := range paragraph {
			if options.EnableInlineFormatting {
				parts = append(parts, formatLineElements(line))
			} else {
				parts = append(parts, line.Text)
			}
			boxes = append(boxes, line.BBox)
			if line.MinIndex < minIndex {
				minIndex = line.MinIndex
			}
			if line.FontSize > fontSize {
				fontSize = line.FontSize
			}
		}
		blocks = append(blocks, markdownBlock{
			Index:         minIndex,
			Text:          collapseSpaces(joinParagraphLineTexts(parts)),
			Box:           geom.EnclosingBox(boxes...),
			FontSize:      fontSize,
			LineCount:     len(paragraph),
			ListCandidate: paragraph[0].ListCandidate,
			ListContentL:  paragraph[0].ListContentL,
		})
		paragraph = nil
	}

	for i, line := range lines {
		if line.ListCandidate {
			flush()
			paragraph = append(paragraph, line)
			continue
		}
		if len(paragraph) > 0 && paragraph[0].ListCandidate {
			prev := paragraph[len(paragraph)-1]
			if lineContinuesListItem(paragraph[0], prev, line) {
				paragraph = append(paragraph, line)
				continue
			}
			flush()
		}
		if i > 0 {
			prev := lines[i-1]
			refHeight := prev.BBox.Height()
			gap := line.BBox.T - prev.BBox.B
			if (refHeight <= 0 || gap > gapFactor*refHeight) && !shouldJoinTightImageCaptionTitle(prev, line) {
				flush()
			}
		}
		paragraph = append(paragraph, line)
	}
	flush()

	return blocks
}

func lineContinuesListItem(first, prev, next ParagraphTextLine) bool {
	if !first.ListCandidate || next.ListCandidate {
		return false
	}
	refHeight := math.Max(prev.BBox.Height(), next.BBox.Height())
	if refHeight <= 0 {
		return false
	}
	gap := next.BBox.T - prev.BBox.B
	if gap < 0 || gap > refHeight*1.75 {
		return false
	}
	tolerance := math.Max(8, math.Max(first.FontSize, next.FontSize)*1.25)
	return math.Abs(next.BBox.L-first.ListContentL) <= tolerance
}

func joinParagraphLineTexts(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := strings.TrimSpace(lines[0])
	for _, line := range lines[1:] {
		next := strings.TrimSpace(line)
		if shouldDehyphenateLineJoin(out, next) {
			out = trimTrailingLineHyphen(strings.TrimSpace(out)) + next
			continue
		}
		out += " " + next
	}
	return out
}

// lineHyphenSuffixes are the line-end hyphen forms a reflow removes: the plain
// hyphen-minus, its Unicode twin, and the soft hyphen (typeset only at a break
// by definition).
//
// \x02 — the marker the text page substitutes for a line-break hyphen it has
// judged mid-word (charHyphen) — is deliberately NOT here. That marker fires
// on the hyphen of a hard compound split across lines ("German-/to-Polish")
// just as readily as on a soft one, and removing it silently welds the
// compound shut. collapseSpaces restores it to a visible "-" instead.
var lineHyphenSuffixes = []string{"-", "‐", "­"}

func trailingLineHyphen(text string) string {
	for _, suffix := range lineHyphenSuffixes {
		if strings.HasSuffix(text, suffix) {
			return suffix
		}
	}
	return ""
}

func trimTrailingLineHyphen(text string) string {
	if suffix := trailingLineHyphen(text); suffix != "" {
		return strings.TrimSuffix(text, suffix)
	}
	return text
}

func shouldDehyphenateLineJoin(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	suffix := trailingLineHyphen(left)
	if suffix == "" {
		return false
	}
	leftRunes := []rune(strings.TrimSuffix(left, suffix))
	rightRunes := []rune(right)
	if len(leftRunes) == 0 || len(rightRunes) == 0 {
		return false
	}
	return unicode.IsLetter(leftRunes[len(leftRunes)-1]) && unicode.IsLetter(rightRunes[0])
}

// collapseSpaces replaces any run of whitespace with a single space.
func collapseSpaces(s string) string {
	s = expandUnicodeLigatures(s)
	s = removeZeroWidthFormatRunes(s)
	s = strings.ReplaceAll(s, "\x02", "-")
	s = strings.ReplaceAll(s, "’", "'")
	s = strings.ReplaceAll(s, "‘", "'")
	s = compactApostropheSpaces(strings.Join(strings.Fields(s), " "))
	s = compactPunctuationSpaces(s)
	return compactDecimalSpaces(s)
}

func removeZeroWidthFormatRunes(s string) string {
	if s == "" {
		return s
	}
	var out []rune
	for _, r := range s {
		switch r {
		case '\u200b', '\ufeff':
			continue
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func expandUnicodeLigatures(s string) string {
	if s == "" {
		return s
	}
	var out []rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		replacement, ok := unicodeLigatureReplacement(runes[i])
		if !ok {
			out = append(out, runes[i])
			continue
		}
		out = append(out, []rune(replacement)...)
		if '\ufb00' <= runes[i] && runes[i] <= '\ufb06' && i+1 < len(runes) && runes[i+1] == ' ' && i+2 < len(runes) && unicode.IsLetter(runes[i+2]) {
			i++
		}
	}
	return string(out)
}

func unicodeLigatureReplacement(r rune) (string, bool) {
	switch r {
	case '\ufb00':
		return "ff", true
	case '\ufb01':
		return "fi", true
	case '\ufb02':
		return "fl", true
	case '\ufb03':
		return "ffi", true
	case '\ufb04':
		return "ffl", true
	case '\ufb05', '\ufb06':
		return "st", true
	case '\u0132':
		return "IJ", true
	case '\u0133':
		return "ij", true
	case '\uf0a0':
		return "", true
	default:
		return "", false
	}
}

func compactApostropheSpaces(s string) string {
	runes := []rune(s)
	if len(runes) < 3 {
		return s
	}
	var out []rune
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) {
			if runes[i+1] == '\'' && unicode.IsLetter(runes[i-1]) && shortApostropheContinuationAfter(runes, i+1) {
				continue
			}
			if runes[i-1] == '\'' && unicode.IsLetter(runes[i+1]) && shortApostropheContinuation(runes, i+1) {
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func shortApostropheContinuationAfter(runes []rune, apostropheIndex int) bool {
	start := apostropheIndex + 1
	for start < len(runes) && runes[start] == ' ' {
		start++
	}
	return shortApostropheContinuation(runes, start)
}

func shortApostropheContinuation(runes []rune, start int) bool {
	letters := 0
	for i := start; i < len(runes); i++ {
		r := runes[i]
		if !unicode.IsLetter(r) {
			break
		}
		letters++
		if letters > 2 {
			return false
		}
	}
	return letters > 0
}

func compactPunctuationSpaces(s string) string {
	runes := []rune(s)
	if len(runes) < 3 {
		return s
	}
	var out []rune
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) && compactableTrailingPunctuation(runes[i+1]) && !compactableTrailingPunctuation(runes[i-1]) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func compactDecimalSpaces(s string) string {
	runes := []rune(s)
	if len(runes) < 4 {
		return s
	}
	var out []rune
	for i, r := range runes {
		if r == ' ' && i >= 2 && i+1 < len(runes) && runes[i-1] == '.' && unicode.IsDigit(runes[i-2]) && unicode.IsDigit(runes[i+1]) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func compactableTrailingPunctuation(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?':
		return true
	default:
		return false
	}
}
