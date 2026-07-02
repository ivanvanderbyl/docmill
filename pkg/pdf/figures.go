package pdf

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
)

const (
	figureInternalCaptionWindowFraction = 0.22
	figureInternalMaxWidthFraction      = 0.45
	figureInternalMaxBodyFontFactor     = 0.95
	figureInternalMaxWords              = 7
	figureInternalMaxRunes              = 64
)

func filterFigureInternalLabelBlocks(blocks []markdownBlock, size geom.Size) []markdownBlock {
	if len(blocks) == 0 || size.Width <= 0 || size.Height <= 0 {
		return blocks
	}

	var captions []markdownBlock
	for _, block := range blocks {
		if isNumberedFigureCaptionText(block.Text) {
			captions = append(captions, block)
		}
	}
	if len(captions) == 0 {
		return blocks
	}

	bodyFont := dominantProseBlockFontSize(blocks)
	if bodyFont <= 0 {
		return blocks
	}

	out := make([]markdownBlock, 0, len(blocks))
	for _, block := range blocks {
		if isFigureInternalLabelNearAnyCaption(block, captions, size, bodyFont) {
			continue
		}
		out = append(out, block)
	}
	return out
}

func dominantProseBlockFontSize(blocks []markdownBlock) float64 {
	type bucket struct {
		key    int
		weight int
		count  int
	}
	buckets := make(map[int]bucket)
	for _, block := range blocks {
		if block.FontSize <= 0 || block.HeadingLevel > 0 || isNumberedFigureCaptionText(block.Text) {
			continue
		}
		text := collapseSpaces(block.Text)
		if !looksLikeProseText(text) {
			continue
		}
		key := int(math.Round(block.FontSize * 2))
		entry := buckets[key]
		entry.key = key
		entry.weight += max(1, utf8.RuneCountInString(text))
		entry.count++
		buckets[key] = entry
	}
	best := bucket{count: -1}
	for key, entry := range buckets {
		if entry.weight > best.weight ||
			(entry.weight == best.weight && entry.count > best.count) ||
			(entry.weight == best.weight && entry.count == best.count && key < best.key) {
			best = entry
		}
	}
	if best.count < 0 {
		return 0
	}
	return float64(best.key) / 2
}

func isFigureInternalLabelNearAnyCaption(block markdownBlock, captions []markdownBlock, size geom.Size, bodyFont float64) bool {
	if !looksLikeFigureInternalLabelBlock(block, size, bodyFont) {
		return false
	}
	for _, caption := range captions {
		if isBlockInFigureInteriorAboveCaption(block, caption, size) {
			return true
		}
	}
	return false
}

func looksLikeFigureInternalLabelBlock(block markdownBlock, size geom.Size, bodyFont float64) bool {
	text := collapseSpaces(block.Text)
	if text == "" || block.HeadingLevel > 0 || isNumberedFigureCaptionText(text) {
		return false
	}
	if block.FontSize <= 0 || block.FontSize > bodyFont*figureInternalMaxBodyFontFactor {
		return false
	}
	if block.LineCount > 2 {
		return false
	}
	if block.Box.Width() > size.Width*figureInternalMaxWidthFraction {
		return false
	}
	return looksLikeFigureInternalLabelText(text)
}

func isBlockInFigureInteriorAboveCaption(block, caption markdownBlock, size geom.Size) bool {
	blockTop, blockBottom := blockVerticalSpan(block)
	captionTop, _ := blockVerticalSpan(caption)
	if blockBottom > captionTop+math.Max(4, caption.Box.Height()*1.5) {
		return false
	}

	window := size.Height * figureInternalCaptionWindowFraction
	if window < 96 {
		window = 96
	}
	if window > 180 {
		window = 180
	}
	if captionTop-blockTop > window {
		return false
	}

	horizontalSlack := size.Width * 0.2
	return block.Box.R >= caption.Box.L-horizontalSlack && block.Box.L <= caption.Box.R+horizontalSlack
}

func blockVerticalSpan(block markdownBlock) (float64, float64) {
	top := block.Box.T
	bottom := block.Box.B
	if bottom < top {
		top, bottom = bottom, top
	}
	return top, bottom
}

func isNumberedFigureCaptionText(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(trimmed, "figure ") ||
		strings.HasPrefix(trimmed, "fig. ") ||
		strings.HasPrefix(trimmed, "fig ")
}

func looksLikeProseText(text string) bool {
	words := strings.Fields(text)
	if len(words) >= 8 {
		return true
	}
	return len(words) >= 4 && strings.ContainsAny(text, ".;:!?")
}

func looksLikeFigureInternalLabelText(text string) bool {
	if utf8.RuneCountInString(text) > figureInternalMaxRunes {
		return false
	}
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > figureInternalMaxWords {
		return false
	}
	if strings.ContainsAny(text, ".;:!?") && !strings.Contains(text, "%") {
		return false
	}
	if containsDigitOrPercent(text) {
		return true
	}
	return len(words) <= 4
}

func containsDigitOrPercent(text string) bool {
	for _, r := range text {
		if r == '%' || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func shouldJoinTightImageCaptionTitle(line, next ParagraphTextLine) bool {
	return looksLikeTightImageCaptionTitleLine(line, &next, 0)
}

func looksLikeTightImageCaptionTitleLine(line ParagraphTextLine, next *ParagraphTextLine, bodyMetric float64) bool {
	if next == nil || !isBareNumberedCaptionTitleText(collapseSpaces(line.Text)) {
		return false
	}
	if bodyMetric > 0 {
		metric := lineMetric(line)
		if metric > bodyMetric*1.05 || lineMetric(*next) > bodyMetric*1.08 {
			return false
		}
	}
	if !looksLikeCaptionBodyText(collapseSpaces(next.Text)) {
		return false
	}

	height := math.Max(1, line.BBox.Height())
	gap := next.BBox.T - line.BBox.B
	if gap < -height*0.2 || gap > math.Max(8, height*1.1) {
		return false
	}
	return math.Abs(line.BBox.L-next.BBox.L) <= math.Max(6, height)
}

func isBareNumberedCaptionTitleText(text string) bool {
	match := decimalHeadingPrefixPattern.FindStringSubmatch(text)
	if match == nil {
		return false
	}
	marker := match[1]
	if strings.HasSuffix(marker, ".") || strings.Contains(marker, ".") {
		return false
	}
	value, err := strconv.Atoi(marker)
	if err != nil || value <= 0 || value > 20 {
		return false
	}
	rest := strings.TrimSpace(match[2])
	words := wordsForHeading(rest)
	if len(words) < 2 || len(words) > 6 {
		return false
	}
	return looksTitleLike(rest)
}

func looksLikeCaptionBodyText(text string) bool {
	if text == "" || decimalHeadingPattern.MatchString(text) || isNumberedFigureCaptionText(text) {
		return false
	}
	return len(wordsForHeading(text)) >= 6
}
