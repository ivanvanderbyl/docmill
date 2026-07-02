package pdf

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/pkg/page"
)

// bulletMarkers are leading glyphs that, when followed by a space and non-empty
// content, mark a block as an unordered list item. They are rewritten to "-".
var bulletMarkers = []string{"•", "●", "○", "◦", "▪", "‣", "·", "–", "—", "-", "*"}

// detectStructure reclassifies aligned runs of assembled paragraph blocks whose
// leading text begins with a list marker. Character markers are treated only as
// candidates; a block is rewritten when it belongs to a neighbouring run with
// consistent left indentation and vertical spacing.
func detectStructure(blocks []markdownBlock) []markdownBlock {
	out := append([]markdownBlock(nil), blocks...)
	for index := range out {
		if !isListBlockCandidate(out[index]) || !listBlockHasContext(out, index) {
			continue
		}
		if rewritten, ok := rewriteListItem(out[index].Text); ok {
			out[index].Text = rewritten
		}
	}
	return out
}

func listBlocksContinueRun(prev, next markdownBlock) bool {
	if !isListBlockCandidate(next) {
		return false
	}
	if !listBlocksHaveCompatibleIndent(prev, next) {
		return false
	}
	gap := next.Box.T - prev.Box.B
	height := math.Max(prev.Box.Height(), next.Box.Height())
	if height <= 0 {
		return false
	}
	return gap >= 0 && gap <= height*5
}

func listBlockHasContext(blocks []markdownBlock, index int) bool {
	block := blocks[index]
	for _, neighborIndex := range []int{index - 1, index + 1} {
		if neighborIndex < 0 || neighborIndex >= len(blocks) {
			continue
		}
		neighbor := blocks[neighborIndex]
		if !isListBlockCandidate(neighbor) {
			continue
		}
		if neighborIndex < index {
			if listBlocksContinueRun(neighbor, block) {
				return true
			}
			continue
		}
		if listBlocksContinueRun(block, neighbor) {
			return true
		}
	}
	return false
}

func listBlocksHaveCompatibleIndent(prev, next markdownBlock) bool {
	fontSize := math.Max(prev.FontSize, next.FontSize)
	indentTolerance := math.Max(8, fontSize)
	diff := math.Abs(prev.ListContentL - next.ListContentL)
	if diff <= indentTolerance {
		return true
	}
	minNestedStep := math.Max(12, fontSize*1.5)
	maxNestedStep := math.Max(64, fontSize*8)
	return diff >= minNestedStep && diff <= maxNestedStep
}

func isListBlockCandidate(block markdownBlock) bool {
	if !block.ListCandidate {
		return false
	}
	_, ok := rewriteListItem(block.Text)
	return ok
}

func protectedListLineCellIndexes(cells []page.TextCell) map[int]bool {
	options := ParagraphOptions{}.withDefaults()
	lines := AssembleLineElements(cells, options.LineTolerance)
	if len(lines) == 0 {
		return nil
	}
	protected := make(map[int]bool)
	for i, line := range lines {
		if !line.ListCandidate {
			continue
		}
		marker := lineListMarkerText(line)
		if isOrderedMarkerOnly(marker) && !hasAdjacentListLine(lines, i) {
			continue
		}
		for _, cell := range line.Cells {
			protected[cell.Index] = true
		}
	}
	if len(protected) == 0 {
		return nil
	}
	return protected
}

// rewriteListItem returns the block text rewritten as a Markdown list item and
// true when the text starts with a recognised list marker; otherwise it returns
// the input unchanged and false.
func rewriteListItem(text string) (string, bool) {
	trimmed := strings.TrimLeft(text, " \t")
	if trimmed == "" {
		return text, false
	}

	if rest, ok := stripBulletMarker(trimmed); ok {
		return "- " + rest, true
	}
	if marker, rest, ok := stripOrderedMarker(trimmed); ok {
		return marker + " " + rest, true
	}
	return text, false
}

// stripBulletMarker reports whether trimmed begins with a bullet glyph followed
// by whitespace and non-empty content, returning the content after the marker.
func stripBulletMarker(trimmed string) (string, bool) {
	for _, marker := range bulletMarkers {
		if !strings.HasPrefix(trimmed, marker) {
			continue
		}
		rest := trimmed[len(marker):]
		// The marker must be followed by whitespace; this rejects hyphenated
		// or em-dashed words such as "—based" or "-foo" mid-sentence starts.
		if rest == "" || !startsWithSpace(rest) {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return "", false
		}
		return rest, true
	}
	return "", false
}

// stripOrderedMarker reports whether trimmed begins with an ordered-list marker
// of the form "<n>." / "<n>)" (one or more digits) followed by whitespace and
// non-empty content. It returns the normalised marker (original number preserved,
// "." enforced as the delimiter) and the remaining content.
//
// Single-letter markers (a./b)) are intentionally NOT treated as list items:
// across the corpus they overwhelmingly mark figure sub-captions and inline
// references rather than lists, so accepting them produces frequent false
// positives. This is documented in the design doc.
func stripOrderedMarker(trimmed string) (string, string, bool) {
	digits := leadingDigits(trimmed)
	if digits == 0 {
		return "", "", false
	}
	if digits > 9 {
		// Bare numbers this long at a line start are far more likely to be
		// figures, years, or data than list ordinals; stay conservative.
		return "", "", false
	}
	number := trimmed[:digits]
	rest := trimmed[digits:]
	if rest == "" {
		return "", "", false
	}
	delim, width := utf8.DecodeRuneInString(rest)
	if delim != '.' && delim != ')' {
		return "", "", false
	}
	rest = rest[width:]
	if rest == "" || !startsWithSpace(rest) {
		return "", "", false
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return "", "", false
	}
	return number + ".", rest, true
}

// leadingDigits returns the count of leading ASCII digit bytes in s.
func leadingDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// startsWithSpace reports whether s begins with a Unicode whitespace rune.
func startsWithSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func geometricListMarker(cells []page.TextCell) (bool, float64) {
	if len(cells) < 2 {
		return false, 0
	}
	markerIndex := -1
	contentIndex := -1
	for i, cell := range cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || isListSpacerText(text) {
			continue
		}
		if markerIndex < 0 {
			if !isListMarkerCellText(text) {
				return false, 0
			}
			markerIndex = i
			continue
		}
		contentIndex = i
		break
	}
	if markerIndex < 0 || contentIndex < 0 {
		return false, 0
	}
	marker := cells[markerIndex]
	content := cells[contentIndex]
	if marker.Box.L >= content.Box.L {
		return false, 0
	}
	markerWidth := marker.Box.Width()
	contentHeight := content.Box.Height()
	fontSize := math.Max(marker.FontSize, content.FontSize)
	if fontSize <= 0 {
		fontSize = math.Max(marker.Box.Height(), contentHeight)
	}
	maxMarkerWidth := math.Max(18, fontSize*2.5)
	if markerWidth <= 0 || markerWidth > maxMarkerWidth {
		return false, 0
	}
	gap := content.Box.L - marker.Box.R
	if gap < -math.Max(2, fontSize*0.25) {
		return false, 0
	}
	minIndent := math.Max(markerWidth*1.5, fontSize*0.75)
	if content.Box.L-marker.Box.L < minIndent {
		return false, 0
	}
	centerDelta := math.Abs(marker.Box.CenterY() - content.Box.CenterY())
	tolerance := math.Max(4, math.Max(marker.Box.Height(), contentHeight)*0.75)
	if centerDelta > tolerance {
		return false, 0
	}
	return true, content.Box.L
}

func isListSpacerText(text string) bool {
	if text == "" {
		return true
	}
	for _, r := range text {
		if r == '\u200b' || r == '\ufeff' || unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return true
}

func isListMarkerCellText(text string) bool {
	return isBulletMarkerOnly(text) || isOrderedMarkerOnly(text)
}

func isBulletMarkerOnly(text string) bool {
	if _, ok := stripBulletMarker(text + " x"); ok && len([]rune(strings.TrimSpace(text))) == 1 {
		return true
	}
	return false
}

func isOrderedMarkerOnly(text string) bool {
	trimmed := strings.TrimSpace(text)
	digits := leadingDigits(trimmed)
	if digits == 0 || digits > 9 {
		return false
	}
	rest := trimmed[digits:]
	if rest == "" {
		return false
	}
	delim, width := utf8.DecodeRuneInString(rest)
	if delim != '.' && delim != ')' {
		return false
	}
	return strings.TrimSpace(rest[width:]) == ""
}

func lineListMarkerText(line ParagraphTextLine) string {
	for _, cell := range line.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || isListSpacerText(text) {
			continue
		}
		return text
	}
	return ""
}

func hasAdjacentListLine(lines []ParagraphTextLine, index int) bool {
	line := lines[index]
	for _, neighborIndex := range []int{index - 1, index + 1} {
		if neighborIndex < 0 || neighborIndex >= len(lines) {
			continue
		}
		neighbor := lines[neighborIndex]
		if !neighbor.ListCandidate {
			continue
		}
		tolerance := math.Max(8, math.Max(line.FontSize, neighbor.FontSize)*1.25)
		if math.Abs(line.ListContentL-neighbor.ListContentL) <= tolerance {
			return true
		}
	}
	return false
}
