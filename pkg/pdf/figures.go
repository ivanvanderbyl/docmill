package pdf

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
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
	text := collapseSpaces(strings.TrimLeft(block.Text, "# "))
	if text == "" || isNumberedFigureCaptionText(text) {
		return false
	}
	// Heading blocks are NOT exempt: a short sub-body-size label inside a
	// figure ("REASONABLE CAUSES" atop a channel fan) can win a heading
	// promotion first; the sub-body font gate below is what a genuine section
	// heading fails.
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

const (
	// figureClusterGridStep is the coarse-grid resolution (points) used to
	// connect nearby vector strokes into one drawing.
	figureClusterGridStep = 24.0
	// figureRegionMinExtent: a drawing extends in BOTH axes. A rule, underline,
	// or table border line is one-dimensional (near-zero in one axis) and fails
	// this, which separates a figure from page furniture far more reliably than
	// an area threshold does — real figures range from tiny state diagrams
	// (~1% of the page) to full-width plots, so any area floor high enough to
	// exclude a rule also excludes small genuine figures.
	figureRegionMinExtent = 20.0
	// figureRegionMinSegments: fewer strokes than this is a rule, not a drawing.
	figureRegionMinSegments = 3
	// figureCaptionMaxGap is how far below a cluster a "Fig." caption may sit.
	figureCaptionMaxGap = 56.0
	// figureCellOverlapThreshold: a text cell at least this covered by the
	// region belongs to the drawing.
	figureCellOverlapThreshold = 0.5
)

// figureRegions returns the bounding boxes of caption-anchored vector drawings:
// clusters of stroked path segments that are figure-sized and sit directly
// above a "Fig..."-cue caption cell. The caption anchor is what separates a
// figure from a ruled table (table captions cue "Table") and keeps prose-only
// pages untouched; the geometry (stroke clustering, area) is document-general.
func figureRegions(cells []page.TextCell, rulings []page.RulingSegment, size geom.Size) []geom.Box {
	if len(rulings) < figureRegionMinSegments || size.Width <= 0 || size.Height <= 0 {
		return nil
	}

	// Cheap gate before the clustering pass: a figure region must be anchored by
	// a figure caption, so a page without one can be rejected in a single scan
	// over the cells. Most pages of most documents take this exit.
	captions := figureCaptionCells(cells)
	if len(captions) == 0 {
		return nil
	}

	clusters := clusterRulingSegments(rulings)
	var regions []geom.Box
	for _, cluster := range clusters {
		if cluster.segments < figureRegionMinSegments {
			continue
		}
		if cluster.box.Width() < figureRegionMinExtent || cluster.box.Height() < figureRegionMinExtent {
			continue
		}
		if !hasFigureCaptionBelow(cluster.box, captions) {
			continue
		}
		// Labels hug a drawing from just outside its stroke extent (box titles
		// above the boxes, edge labels beside the arrows), so the text region
		// extends one label-line beyond the strokes. Prose-like cells and the
		// caption are exempt from suppression, which keeps adjacent body text
		// safe from the padding.
		region := cluster.box
		region.L -= 12
		region.R += 12
		region.T -= figureClusterGridStep
		region.B += 4
		regions = append(regions, region)
	}
	return regions
}

type strokeCluster struct {
	box      geom.Box
	segments int
}

// clusterRulingSegments connects segments through a coarse occupancy grid
// (8-way flood fill), which keeps clustering linear in the segment count.
func clusterRulingSegments(rulings []page.RulingSegment) []strokeCluster {
	type cellKey struct{ x, y int }
	occupied := make(map[cellKey][]int)
	boxes := make([]geom.Box, len(rulings))
	for i, segment := range rulings {
		box := geom.Box{
			L: math.Min(segment.FromX, segment.ToX), T: math.Min(segment.FromY, segment.ToY),
			R: math.Max(segment.FromX, segment.ToX), B: math.Max(segment.FromY, segment.ToY),
			Origin: geom.TopLeft,
		}
		boxes[i] = box
		x0 := int(math.Floor(box.L / figureClusterGridStep))
		x1 := int(math.Floor(box.R / figureClusterGridStep))
		y0 := int(math.Floor(box.T / figureClusterGridStep))
		y1 := int(math.Floor(box.B / figureClusterGridStep))
		for x := x0; x <= x1; x++ {
			for y := y0; y <= y1; y++ {
				key := cellKey{x, y}
				occupied[key] = append(occupied[key], i)
			}
		}
	}

	visited := make([]bool, len(rulings))
	visitedCell := make(map[cellKey]bool, len(occupied))
	var clusters []strokeCluster
	for start := range boxes {
		if visited[start] {
			continue
		}
		cluster := strokeCluster{box: boxes[start]}
		stack := []int{start}
		visited[start] = true
		for len(stack) > 0 {
			index := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cluster.segments++
			cluster.box = geom.EnclosingBox(cluster.box, boxes[index])
			box := boxes[index]
			x0 := int(math.Floor(box.L/figureClusterGridStep)) - 1
			x1 := int(math.Floor(box.R/figureClusterGridStep)) + 1
			y0 := int(math.Floor(box.T/figureClusterGridStep)) - 1
			y1 := int(math.Floor(box.B/figureClusterGridStep)) + 1
			for x := x0; x <= x1; x++ {
				for y := y0; y <= y1; y++ {
					key := cellKey{x, y}
					if visitedCell[key] {
						continue
					}
					visitedCell[key] = true
					for _, neighbour := range occupied[key] {
						if !visited[neighbour] {
							visited[neighbour] = true
							stack = append(stack, neighbour)
						}
					}
				}
			}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func figureCaptionCells(cells []page.TextCell) []page.TextCell {
	var out []page.TextCell
	for _, cell := range cells {
		if isNumberedFigureCaptionText(collapseSpaces(cell.Text)) {
			out = append(out, cell)
		}
	}
	return out
}

func hasFigureCaptionBelow(region geom.Box, captions []page.TextCell) bool {
	for _, cell := range captions {
		top := math.Min(cell.Box.T, cell.Box.B)
		if top < region.B-4 || top > region.B+figureCaptionMaxGap {
			continue
		}
		if cell.Box.R < region.L || cell.Box.L > region.R {
			continue
		}
		return true
	}
	return false
}

// dropCellsInFigureRegions removes text cells that belong to a figure drawing:
// mostly-contained, non-prose fragments (node labels, axis ticks, box labels).
// The caption itself and prose-like cells (a legend paragraph) are kept.
func dropCellsInFigureRegions(cells []page.TextCell, regions []geom.Box) []page.TextCell {
	if len(regions) == 0 {
		return cells
	}
	kept := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if cellBelongsToFigure(cell, regions) {
			continue
		}
		kept = append(kept, cell)
	}
	return kept
}

func cellBelongsToFigure(cell page.TextCell, regions []geom.Box) bool {
	text := collapseSpaces(cell.Text)
	if text == "" {
		return false
	}
	if isNumberedFigureCaptionText(text) || looksLikeProseText(text) {
		return false
	}
	for _, region := range regions {
		if cell.Box.IntersectionOverSelf(region) >= figureCellOverlapThreshold {
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
