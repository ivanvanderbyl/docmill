package pdf

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
)

const (
	headingScaleFactor   = 1.18
	headingBandTolerance = 0.75
	maxHeadingWords      = 24
	maxHeadingRunes      = 140
	indexFolioRightBand  = 0.82
	// footnoteFontFloor: a single-integer "decimal heading" set more than this many
	// points below the body font is sub-body content (a footnote, set in a smaller
	// point size), never a section heading.
	footnoteFontFloor = 0.5
	// centredHeadingMinInset is the minimum inset (as a fraction of page width)
	// on BOTH sides for a bold centred line to count as a heading. Body text
	// margins are typically under 12% of page width; a genuine centred title
	// (even a wide one) is inset well beyond that on both sides. See
	// isCentredBoldHeading.
	centredHeadingMinInset = 0.15
)

var (
	decimalHeadingPattern         = regexp.MustCompile(`^\d+(?:\.\d+)*\.?\s+\S`)
	decimalHeadingPrefixPattern   = regexp.MustCompile(`^(\d+(?:\.\d+)*\.?)\s+(.+)`)
	appendixDecimalHeadingPattern = regexp.MustCompile(`^[A-Z]\.\d+(?:\.\d+)*\s+\S`)
	appendixLetterHeadingPattern  = regexp.MustCompile(`^[A-Z]\s+(?:Additional|Appendix|Contributions|Related)\b`)
	romanHeadingPattern           = regexp.MustCompile(`^[IVXLCDM]+\.\s+[[:upper:]]`)
	lowercaseLetterHeadingPattern = regexp.MustCompile(`^[a-z]\.\s+[[:upper:]]`)
	lowercaseLetterMarkerPattern  = regexp.MustCompile(`[a-z]\.\s+[[:upper:]]`)
	orderedListMarkerPattern      = regexp.MustCompile(`^\d{1,3}[.)]$`)
	orderedListSequencePattern    = regexp.MustCompile(`(?:^|\s)\d{1,3}[.)]\s+[[:upper:]]`)
	headingMarkerPattern          = regexp.MustCompile(`^(?:\d+(?:\.\d+)*\.?|[IVXLCDM]+\.?)$`)
	numberedCaptionPattern        = regexp.MustCompile(`(?i)^(fig(?:ure)?\.?|table)\s+\d`)
	numberedSectionMarkerPattern  = regexp.MustCompile(`(?:^|\s)\d+(?:\.\d+)*\.?\s+[[:upper:]]`)
	standaloneMonthYearPattern    = regexp.MustCompile(`(?i)^(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d{4}$`)
	documentCodePattern           = regexp.MustCompile(`^[A-Z]{2,}(?:-[A-Z0-9]{1,}){2,}$`)
	partSectionMarkerPattern      = regexp.MustCompile(`\bPart\s*[IVXLCDM]+\.?`)
)

type headingLine struct {
	line   ParagraphTextLine
	metric float64
	level  int
}

func splitHeadingCellsProtecting(cells []page.TextCell, size geom.Size, protected map[int]bool) ([]markdownBlock, []page.TextCell) {
	if len(cells) == 0 {
		return nil, nil
	}
	if len(protected) > 0 {
		candidateCells := make([]page.TextCell, 0, len(cells))
		for _, cell := range cells {
			if !protected[cell.Index] {
				candidateCells = append(candidateCells, cell)
			}
		}
		headingBlocks, remainingCandidates := splitHeadingCellsProtecting(candidateCells, size, nil)
		if len(headingBlocks) == 0 {
			return nil, cells
		}
		remainingIndexes := make(map[int]bool, len(remainingCandidates)+len(protected))
		for index := range protected {
			remainingIndexes[index] = true
		}
		for _, cell := range remainingCandidates {
			remainingIndexes[cell.Index] = true
		}
		remaining := make([]page.TextCell, 0, len(remainingIndexes))
		for _, cell := range cells {
			if remainingIndexes[cell.Index] {
				remaining = append(remaining, cell)
			}
		}
		return headingBlocks, remaining
	}

	options := ParagraphOptions{}.withDefaults()
	pageLines := AssembleLineElements(cells, options.LineTolerance)
	groups := partitionColumns(cells, size)
	groupLineSets := make([][]ParagraphTextLine, 0, len(groups))
	var lines []ParagraphTextLine
	for _, group := range groups {
		current := AssembleLineElements(group, options.LineTolerance)
		groupLineSets = append(groupLineSets, current)
		lines = append(lines, current...)
	}
	bodyMetric := dominantBodyMetric(lines)
	if bodyMetric <= 0 {
		return nil, cells
	}

	headings := make([]headingLine, 0)
	for _, lines := range groupLineSets {
		for i, line := range lines {
			var prev, next *ParagraphTextLine
			if i > 0 {
				prev = &lines[i-1]
			}
			if i+1 < len(lines) {
				next = &lines[i+1]
			}
			following := followingLines(lines, i, 4)
			metric := lineMetric(line)
			if isHeadingLine(line, metric, bodyMetric, size, prev, next) {
				headings = append(headings, headingLine{line: line, metric: metric})
				continue
			}
			if next != nil && looksLikeLabelValueTableHeader(line, next) {
				continue
			}
			for _, candidate := range splitStructuralHeadingCandidates(line) {
				candidateMetric := lineMetric(candidate)
				if !isSplitStructuralHeadingCandidate(candidate, candidateMetric, bodyMetric, size, following) {
					continue
				}
				headings = append(headings, headingLine{line: candidate, metric: candidateMetric})
			}
		}
	}
	if len(headings) == 0 {
		return nil, cells
	}

	assignHeadingLevels(headings)
	headings = filterDenseIndexEntryHeadings(headings, pageLines, size)
	headings = filterCoverTitleLeadInHeadings(headings, pageLines, size)
	if len(headings) == 0 {
		return nil, cells
	}
	headings = mergeAdjacentHeadingLines(headings)
	headings = attachLeadingHeadingMarkers(headings, cells)
	headings = attachTrailingHeadingContinuations(headings, cells)
	headings = filterContentsPageHeadings(headings)
	headingCellIndexes := make(map[int]bool)
	blocks := make([]markdownBlock, 0, len(headings))
	for _, heading := range headings {
		text := collapseSpaces(heading.line.Text)
		for _, cell := range heading.line.Cells {
			headingCellIndexes[cell.Index] = true
		}
		blocks = append(blocks, markdownBlock{
			Index:        heading.line.MinIndex,
			Text:         strings.Repeat("#", heading.level) + " " + text,
			Box:          heading.line.BBox,
			FontSize:     heading.line.FontSize,
			LineCount:    1,
			HeadingLevel: heading.level,
		})
	}

	remaining := make([]page.TextCell, 0, len(cells)-len(headingCellIndexes))
	for _, cell := range cells {
		if !headingCellIndexes[cell.Index] {
			remaining = append(remaining, cell)
		}
	}
	return blocks, remaining
}

func followingLines(lines []ParagraphTextLine, index, limit int) []ParagraphTextLine {
	if limit <= 0 || index+1 >= len(lines) {
		return nil
	}
	end := min(index+1+limit, len(lines))
	return lines[index+1 : end]
}

func attachLeadingHeadingMarkers(headings []headingLine, cells []page.TextCell) []headingLine {
	out := make([]headingLine, len(headings))
	for i, heading := range headings {
		out[i] = attachLeadingHeadingMarker(heading, cells)
	}
	return out
}

func attachLeadingHeadingMarker(heading headingLine, cells []page.TextCell) headingLine {
	if startsWithHeadingMarker(heading.line.Text) {
		return heading
	}
	lineCellIndexes := make(map[int]bool, len(heading.line.Cells))
	for _, cell := range heading.line.Cells {
		lineCellIndexes[cell.Index] = true
	}

	var best *page.TextCell
	for i := range cells {
		cell := cells[i]
		if lineCellIndexes[cell.Index] || !isHeadingMarkerText(cell.Text) {
			continue
		}
		if !alignedHeadingMarker(cell, heading.line) {
			continue
		}
		if best == nil || cell.Box.R > best.Box.R {
			best = &cell
		}
	}
	if best == nil {
		return heading
	}
	if wouldAttachNumberedTableAcronym(*best, heading.line, cells, lineCellIndexes) {
		return heading
	}

	line := heading.line
	line.Text = joinHeadingText(best.Text, line.Text)
	line.BBox = geom.EnclosingBox(best.Box, line.BBox)
	if best.Index < line.MinIndex {
		line.MinIndex = best.Index
	}
	line.Cells = append([]page.TextCell{*best}, line.Cells...)
	return headingLine{line: line, metric: heading.metric, level: heading.level}
}

func wouldAttachNumberedTableAcronym(marker page.TextCell, line ParagraphTextLine, cells []page.TextCell, lineCellIndexes map[int]bool) bool {
	if !isPlainIntegerToken(strings.TrimSpace(marker.Text)) {
		return false
	}
	words := wordsForHeading(collapseSpaces(line.Text))
	if len(words) != 1 || utf8.RuneCountInString(words[0]) > 12 || !isAllCapsWord(words[0]) {
		return false
	}
	return hasAlignedNumericPeerRight(line, cells, lineCellIndexes)
}

func hasAlignedNumericPeerRight(line ParagraphTextLine, cells []page.TextCell, lineCellIndexes map[int]bool) bool {
	lineCenter := line.BBox.CenterY()
	tolerance := math.Max(3, line.BBox.Height()*0.75)
	for _, cell := range cells {
		if lineCellIndexes[cell.Index] {
			continue
		}
		if cell.Box.L <= line.BBox.R {
			continue
		}
		if math.Abs(cell.Box.CenterY()-lineCenter) > tolerance {
			continue
		}
		if isPlainIntegerToken(strings.ReplaceAll(strings.TrimSpace(cell.Text), ",", "")) {
			return true
		}
	}
	return false
}

func startsWithHeadingMarker(text string) bool {
	fields := strings.Fields(text)
	return len(fields) > 0 && isHeadingMarkerText(fields[0])
}

func isHeadingMarkerText(text string) bool {
	return headingMarkerPattern.MatchString(strings.TrimSpace(text))
}

func alignedHeadingMarker(marker page.TextCell, line ParagraphTextLine) bool {
	if marker.Box.R > line.BBox.L {
		return false
	}
	gap := line.BBox.L - marker.Box.R
	if gap > math.Max(24, line.BBox.Height()*4) {
		return false
	}
	markerCenter := (marker.Box.T + marker.Box.B) / 2
	lineCenter := (line.BBox.T + line.BBox.B) / 2
	if math.Abs(markerCenter-lineCenter) > 4 {
		return false
	}
	markerMetric := marker.FontSize
	if markerMetric <= 0 {
		markerMetric = marker.Box.Height()
	}
	lineMetricValue := lineMetric(line)
	if markerMetric > 0 && lineMetricValue > 0 && math.Abs(markerMetric-lineMetricValue) > headingBandTolerance {
		return false
	}
	return true
}

func attachTrailingHeadingContinuations(headings []headingLine, cells []page.TextCell) []headingLine {
	out := make([]headingLine, len(headings))
	for i, heading := range headings {
		out[i] = attachTrailingHeadingContinuation(heading, cells)
	}
	return out
}

func attachTrailingHeadingContinuation(heading headingLine, cells []page.TextCell) headingLine {
	lineCellIndexes := make(map[int]bool, len(heading.line.Cells))
	for _, cell := range heading.line.Cells {
		lineCellIndexes[cell.Index] = true
	}

	var best *page.TextCell
	for i := range cells {
		cell := cells[i]
		if lineCellIndexes[cell.Index] || !alignedTrailingHeadingContinuation(cell, heading.line) {
			continue
		}
		if best == nil || cell.Box.T < best.Box.T {
			best = &cell
		}
	}
	if best == nil {
		return heading
	}

	line := heading.line
	line.Text = joinHeadingText(line.Text, best.Text)
	line.BBox = geom.EnclosingBox(line.BBox, best.Box)
	line.Cells = append(line.Cells, *best)
	return headingLine{line: line, metric: heading.metric, level: heading.level}
}

func alignedTrailingHeadingContinuation(cell page.TextCell, line ParagraphTextLine) bool {
	if strings.TrimSpace(cell.Text) == "" || cell.Box.T < line.BBox.B {
		return false
	}
	gap := cell.Box.T - line.BBox.B
	if gap > math.Max(8, line.BBox.Height()*1.25) {
		return false
	}
	cellMetric := cell.FontSize
	if cellMetric <= 0 {
		cellMetric = cell.Box.Height()
	}
	lineMetricValue := lineMetric(line)
	if cellMetric > 0 && lineMetricValue > 0 && math.Abs(cellMetric-lineMetricValue) > headingBandTolerance {
		return false
	}
	if !startsWithDigit(cell.Text) {
		return startsWithDecimalHeadingText(line.Text) && alignedWrappedDecimalContinuation(collapseSpaces(cell.Text), cell.Box, cellMetric, line)
	}
	lineWidth := line.BBox.Width()
	cellWidth := cell.Box.Width()
	if lineWidth <= 0 || cellWidth <= 0 || cellWidth > lineWidth*0.9 {
		return false
	}
	lineCenter := (line.BBox.L + line.BBox.R) / 2
	cellCenter := (cell.Box.L + cell.Box.R) / 2
	return math.Abs(lineCenter-cellCenter) <= math.Max(12, lineWidth*0.2)
}

func startsWithDecimalHeadingText(text string) bool {
	return decimalHeadingPrefixPattern.MatchString(collapseSpaces(text))
}

func alignedWrappedDecimalContinuation(text string, box geom.Box, metric float64, line ParagraphTextLine) bool {
	words := wordsForHeading(text)
	if len(words) == 0 || len(words) > 6 || strings.HasSuffix(text, ".") || decimalHeadingPattern.MatchString(text) {
		return false
	}
	if strings.ContainsAny(text, "?,;:()") {
		return false
	}
	lineMetricValue := lineMetric(line)
	if metric > 0 && lineMetricValue > 0 && math.Abs(metric-lineMetricValue) > headingBandTolerance {
		return false
	}
	gap := box.T - line.BBox.B
	if gap < -line.BBox.Height() || gap > math.Max(10, line.BBox.Height()*1.1) {
		return false
	}
	leftDelta := box.L - line.BBox.L
	if leftDelta < 0 || leftDelta > math.Max(48, line.BBox.Height()*5) {
		return false
	}
	lineWidth := line.BBox.Width()
	return lineWidth > 0 && box.Width() <= lineWidth*0.75
}

func startsWithDigit(text string) bool {
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		return unicode.IsDigit(r)
	}
	return false
}

func filterContentsPageHeadings(headings []headingLine) []headingLine {
	hasContentsHeading := false
	for _, heading := range headings {
		if isContentsHeading(heading.line.Text) {
			hasContentsHeading = true
			break
		}
	}
	if !hasContentsHeading {
		return headings
	}
	filtered := make([]headingLine, 0, len(headings))
	for _, heading := range headings {
		if isContentsHeading(heading.line.Text) {
			filtered = append(filtered, heading)
		}
	}
	return filtered
}

func filterDenseIndexEntryHeadings(headings []headingLine, lines []ParagraphTextLine, size geom.Size) []headingLine {
	if len(headings) == 0 || len(lines) < 3 {
		return headings
	}
	orderedLines := append([]ParagraphTextLine(nil), lines...)
	sort.SliceStable(orderedLines, func(i, j int) bool {
		if orderedLines[i].BBox.T == orderedLines[j].BBox.T {
			return orderedLines[i].BBox.L < orderedLines[j].BBox.L
		}
		return orderedLines[i].BBox.T < orderedLines[j].BBox.T
	})

	filtered := make([]headingLine, 0, len(headings))
	for _, heading := range headings {
		sourceIndex := sourceLineIndexForHeading(heading, orderedLines)
		if sourceIndex >= 0 && belongsToDenseIndexRegion(orderedLines, sourceIndex, size) {
			continue
		}
		filtered = append(filtered, heading)
	}
	return filtered
}

func filterCoverTitleLeadInHeadings(headings []headingLine, lines []ParagraphTextLine, size geom.Size) []headingLine {
	if len(headings) == 0 || len(lines) < 2 {
		return headings
	}
	orderedLines := append([]ParagraphTextLine(nil), lines...)
	sort.SliceStable(orderedLines, func(i, j int) bool {
		if orderedLines[i].BBox.T == orderedLines[j].BBox.T {
			return orderedLines[i].BBox.L < orderedLines[j].BBox.L
		}
		return orderedLines[i].BBox.T < orderedLines[j].BBox.T
	})

	filtered := make([]headingLine, 0, len(headings))
	for _, heading := range headings {
		sourceIndex := sourceLineIndexForHeading(heading, orderedLines)
		if sourceIndex >= 0 && belongsToCoverTitleLeadInCluster(orderedLines, sourceIndex, size) {
			continue
		}
		filtered = append(filtered, heading)
	}
	return filtered
}

func belongsToCoverTitleLeadInCluster(lines []ParagraphTextLine, index int, size geom.Size) bool {
	if index < 0 || index >= len(lines) {
		return false
	}
	for start := index; start >= 0 && index-start <= 3; start-- {
		if !coverTitleLeadInLine(lines[start], size) {
			continue
		}
		end := start
		for end+1 < len(lines) && sameCoverTitleStackLine(lines[end], lines[end+1]) {
			end++
		}
		if end > start && index >= start && index <= end {
			return true
		}
	}
	return false
}

func coverTitleLeadInLine(line ParagraphTextLine, size geom.Size) bool {
	if size.Height <= 0 {
		return false
	}
	text := collapseSpaces(line.Text)
	if !strings.HasSuffix(text, ":") || startsWithDecimalHeadingText(text) {
		return false
	}
	height := math.Max(1, line.BBox.Height())
	if lineMetric(line) < 18 && height < 18 {
		return false
	}
	return line.BBox.T >= size.Height*0.15 && line.BBox.T <= size.Height*0.75
}

func sameCoverTitleStackLine(upper, lower ParagraphTextLine) bool {
	lowerText := collapseSpaces(lower.Text)
	if lowerText == "" || strings.HasSuffix(lowerText, ":") || !looksTitleLike(lowerText) {
		return false
	}
	if math.Abs(lineMetric(upper)-lineMetric(lower)) > headingBandTolerance {
		return false
	}
	height := math.Max(1, upper.BBox.Height())
	gap := lower.BBox.T - upper.BBox.B
	if gap < -height*0.1 || gap > math.Max(12, height*0.35) {
		return false
	}
	return math.Abs(lower.BBox.L-upper.BBox.L) <= math.Max(16, height*0.35)
}

func sourceLineIndexForHeading(heading headingLine, lines []ParagraphTextLine) int {
	if len(heading.line.Cells) == 0 {
		return -1
	}
	headingIndexes := make(map[int]bool, len(heading.line.Cells))
	for _, cell := range heading.line.Cells {
		headingIndexes[cell.Index] = true
	}
	bestIndex := -1
	bestMatched := 0
	for i, line := range lines {
		matched := 0
		for _, cell := range line.Cells {
			if headingIndexes[cell.Index] {
				matched++
			}
		}
		if matched == len(headingIndexes) {
			return i
		}
		if matched > bestMatched {
			bestIndex = i
			bestMatched = matched
		}
	}
	return bestIndex
}

func belongsToDenseIndexRegion(lines []ParagraphTextLine, index int, size geom.Size) bool {
	if index < 0 || index >= len(lines) {
		return false
	}
	anchorFolio, ok := indexEntryFolioCell(lines[index], size)
	if !ok {
		return false
	}

	count := 1
	for i := index - 1; i >= 0; i-- {
		if !compactIndexNeighbour(lines[i], lines[i+1]) {
			break
		}
		folio, ok := indexEntryFolioCell(lines[i], size)
		if !ok || !alignedFolioColumn(folio, anchorFolio, lines[index]) {
			break
		}
		count++
	}
	for i := index + 1; i < len(lines); i++ {
		if !compactIndexNeighbour(lines[i-1], lines[i]) {
			break
		}
		folio, ok := indexEntryFolioCell(lines[i], size)
		if !ok || !alignedFolioColumn(folio, anchorFolio, lines[index]) {
			break
		}
		count++
	}
	return count >= 3
}

func compactIndexNeighbour(upper, lower ParagraphTextLine) bool {
	refHeight := math.Max(upper.BBox.Height(), lower.BBox.Height())
	if refHeight <= 0 {
		return false
	}
	gap := lower.BBox.T - upper.BBox.B
	return gap >= -refHeight*0.25 && gap <= math.Max(10, refHeight*1.25)
}

func alignedFolioColumn(left, right page.TextCell, reference ParagraphTextLine) bool {
	leftCenter := (left.Box.L + left.Box.R) * 0.5
	rightCenter := (right.Box.L + right.Box.R) * 0.5
	return math.Abs(leftCenter-rightCenter) <= math.Max(10, reference.BBox.Height())
}

func indexEntryFolioCell(line ParagraphTextLine, size geom.Size) (page.TextCell, bool) {
	if size.Width <= 0 {
		return page.TextCell{}, false
	}
	visible := visibleLineCells(line)
	if len(visible) < 2 {
		return mergedIndexEntryFolioCell(line, size)
	}
	folio := visible[len(visible)-1]
	prev := visible[len(visible)-2]
	height := math.Max(1, line.BBox.Height())
	if folio.Box.Width() > math.Max(28, height*3) {
		return mergedIndexEntryFolioCell(line, size)
	}
	if folio.Box.L-prev.Box.R < math.Max(40, height*4) {
		return mergedIndexEntryFolioCell(line, size)
	}
	if folio.Box.R < size.Width*indexFolioRightBand || folio.Box.R > size.Width+math.Max(24, height*4) {
		return page.TextCell{}, false
	}
	if line.BBox.Width() < math.Max(180, height*18) {
		return page.TextCell{}, false
	}
	if !isPlainIntegerToken(strings.ReplaceAll(strings.TrimSpace(folio.Text), ",", "")) {
		return page.TextCell{}, false
	}
	return folio, true
}

func mergedIndexEntryFolioCell(line ParagraphTextLine, size geom.Size) (page.TextCell, bool) {
	if size.Width <= 0 {
		return page.TextCell{}, false
	}
	height := math.Max(1, line.BBox.Height())
	if line.BBox.R < size.Width*indexFolioRightBand || line.BBox.R > size.Width+math.Max(24, height*4) {
		return page.TextCell{}, false
	}
	if line.BBox.Width() < math.Max(180, height*18) {
		return page.TextCell{}, false
	}
	fields := strings.Fields(collapseSpaces(line.Text))
	if len(fields) < 2 {
		return page.TextCell{}, false
	}
	folioText := strings.ReplaceAll(strings.Trim(fields[len(fields)-1], ".,;:()[]{}"), ",", "")
	if !isPlainIntegerToken(folioText) {
		return page.TextCell{}, false
	}
	width := math.Min(math.Max(10, height*2.2), math.Max(12, line.BBox.Width()*0.08))
	return page.TextCell{
		Text: folioText,
		Box: geom.Box{
			L:      line.BBox.R - width,
			T:      line.BBox.T,
			R:      line.BBox.R,
			B:      line.BBox.B,
			Origin: line.BBox.Origin,
		},
	}, true
}

func visibleLineCells(line ParagraphTextLine) []page.TextCell {
	cells := make([]page.TextCell, 0, len(line.Cells))
	for _, cell := range line.Cells {
		if strings.TrimSpace(cell.Text) != "" {
			cells = append(cells, cell)
		}
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Box.T < cells[j].Box.T
		}
		return cells[i].Box.L < cells[j].Box.L
	})
	return cells
}

func denseIndexLineCellIndexes(cells []page.TextCell, size geom.Size) map[int]bool {
	if len(cells) == 0 {
		return nil
	}
	options := ParagraphOptions{}.withDefaults()
	lines := AssembleLineElements(cells, options.LineTolerance)
	if len(lines) < 3 {
		return nil
	}
	indexes := make(map[int]bool)
	dense := make([]bool, len(lines))
	for i := range lines {
		dense[i] = belongsToDenseIndexRegion(lines, i, size)
	}
	for i := range lines {
		if !dense[i] {
			continue
		}
		for _, cell := range lines[i].Cells {
			indexes[cell.Index] = true
		}
		if i > 0 && wrappedIndexEntryLine(lines[i-1], lines[i], size) {
			for _, cell := range lines[i-1].Cells {
				indexes[cell.Index] = true
			}
		}
	}
	for i := 0; i+1 < len(lines); i++ {
		if !linePrecedesStandaloneIndexFolio(lines[i], lines[i+1], size) {
			continue
		}
		if !hasAdjacentIndexFolioRow(lines, i, size) {
			continue
		}
		for _, cell := range lines[i].Cells {
			indexes[cell.Index] = true
		}
		for _, cell := range lines[i+1].Cells {
			indexes[cell.Index] = true
		}
	}
	if len(indexes) == 0 {
		return nil
	}
	return indexes
}

func wrappedIndexEntryLine(upper, lower ParagraphTextLine, size geom.Size) bool {
	if _, ok := indexEntryFolioCell(upper, size); ok {
		return false
	}
	if _, ok := indexEntryFolioCell(lower, size); !ok {
		return false
	}
	if !compactIndexNeighbour(upper, lower) {
		return false
	}
	if !hasLetter(collapseSpaces(upper.Text)) {
		return false
	}
	leftDelta := lower.BBox.L - upper.BBox.L
	return leftDelta >= -math.Max(12, upper.BBox.Height()) && leftDelta <= math.Max(72, upper.BBox.Height()*8)
}

func hasAdjacentIndexFolioRow(lines []ParagraphTextLine, textLineIndex int, size geom.Size) bool {
	if textLineIndex > 0 && compactIndexNeighbour(lines[textLineIndex-1], lines[textLineIndex]) {
		if _, ok := indexEntryFolioCell(lines[textLineIndex-1], size); ok {
			return true
		}
	}
	folioLineIndex := textLineIndex + 1
	if folioLineIndex+1 < len(lines) && compactIndexNeighbour(lines[folioLineIndex], lines[folioLineIndex+1]) {
		if _, ok := indexEntryFolioCell(lines[folioLineIndex+1], size); ok {
			return true
		}
	}
	return false
}

func linePrecedesStandaloneIndexFolio(line, folioLine ParagraphTextLine, size geom.Size) bool {
	if _, ok := indexEntryFolioCell(line, size); ok {
		return false
	}
	if !standaloneIndexFolioLine(folioLine, size) && !indentedWrappedIndexFolioLine(line, folioLine, size) {
		return false
	}
	if !compactIndexNeighbour(line, folioLine) {
		return false
	}
	text := collapseSpaces(line.Text)
	return hasLetter(text) && len(wordsForHeading(text)) >= 3
}

func standaloneIndexFolioLine(line ParagraphTextLine, size geom.Size) bool {
	if size.Width <= 0 {
		return false
	}
	if !compactFolioTokenLine(line) {
		return false
	}
	height := math.Max(1, line.BBox.Height())
	return line.BBox.R >= size.Width*indexFolioRightBand && line.BBox.R <= size.Width+math.Max(24, height*4)
}

func indentedWrappedIndexFolioLine(line, folioLine ParagraphTextLine, size geom.Size) bool {
	if size.Width <= 0 || !compactFolioTokenLine(folioLine) {
		return false
	}
	height := math.Max(1, math.Max(line.BBox.Height(), folioLine.BBox.Height()))
	if line.BBox.R < size.Width*indexFolioRightBand || line.BBox.R > size.Width+math.Max(24, height*4) {
		return false
	}
	if line.BBox.Width() < math.Max(240, height*24) {
		return false
	}
	leftSlack := math.Max(16, height*2)
	rightSlack := math.Max(48, height*5)
	return folioLine.BBox.L >= line.BBox.L-leftSlack && folioLine.BBox.L <= line.BBox.L+rightSlack
}

func compactFolioTokenLine(line ParagraphTextLine) bool {
	text := strings.ReplaceAll(strings.TrimSpace(collapseSpaces(line.Text)), ",", "")
	if !isPlainIntegerToken(text) {
		return false
	}
	height := math.Max(1, line.BBox.Height())
	return line.BBox.Width() <= math.Max(30, height*4)
}

func dominantBodyMetric(lines []ParagraphTextLine) float64 {
	type bucket struct {
		key    int
		count  int
		weight int
	}
	buckets := make(map[int]bucket)
	total := 0
	for _, line := range lines {
		metric := lineMetric(line)
		if metric <= 0 || strings.TrimSpace(line.Text) == "" {
			continue
		}
		key := int(math.Round(metric * 2))
		entry := buckets[key]
		entry.key = key
		entry.count++
		entry.weight += max(1, utf8.RuneCountInString(collapseSpaces(line.Text)))
		buckets[key] = entry
		total++
	}
	if len(buckets) == 0 {
		return 0
	}
	if total < 5 && len(buckets) > 1 {
		minimum := 0
		first := true
		for key := range buckets {
			if first || key < minimum {
				minimum = key
				first = false
			}
		}
		return float64(minimum) / 2
	}

	best := bucket{key: 0, count: -1}
	minimum := bucket{key: 0, count: 0}
	first := true
	for key, entry := range buckets {
		if first || key < minimum.key {
			minimum = entry
			first = false
		}
		if entry.weight > best.weight ||
			(entry.weight == best.weight && entry.count > best.count) ||
			(entry.weight == best.weight && entry.count == best.count && key < best.key) {
			best = entry
		}
	}
	if total <= 12 && minimum.count >= 2 && best.key > minimum.key && float64(best.key) >= float64(minimum.key)*1.5 {
		return float64(minimum.key) / 2
	}
	return float64(best.key) / 2
}

func lineMetric(line ParagraphTextLine) float64 {
	if line.FontSize > 0 {
		return line.FontSize
	}
	return line.BBox.Height()
}

func splitStructuralHeadingCandidates(line ParagraphTextLine) []ParagraphTextLine {
	if len(line.Cells) < 2 {
		return nil
	}
	candidates := lineSegmentsByLargeGaps(line)
	if len(candidates) < 2 {
		return nil
	}
	return candidates
}

func lineSegmentsByLargeGaps(line ParagraphTextLine) []ParagraphTextLine {
	if len(line.Cells) == 0 {
		return nil
	}
	cells := append([]page.TextCell(nil), line.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Box.T < cells[j].Box.T
		}
		return cells[i].Box.L < cells[j].Box.L
	})

	threshold := math.Max(14, line.BBox.Height()*1.2)
	var segments [][]page.TextCell
	current := []page.TextCell{cells[0]}
	for i := 1; i < len(cells); i++ {
		gap := cells[i].Box.L - cells[i-1].Box.R
		if gap > threshold {
			segments = append(segments, current)
			current = nil
		}
		current = append(current, cells[i])
	}
	segments = append(segments, current)

	candidates := make([]ParagraphTextLine, 0, len(segments))
	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}
		candidates = append(candidates, buildParagraphTextLine(segment))
	}
	return candidates
}

func isSplitStructuralHeadingCandidate(line ParagraphTextLine, metric, bodyMetric float64, size geom.Size, following []ParagraphTextLine) bool {
	text := collapseSpaces(line.Text)
	if text == "" || !hasLetter(text) || numberedCaptionPattern.MatchString(text) {
		return false
	}
	if looksLikeTableCaptionHeading(text) || looksLikeStandaloneAcronymEntry(text) || looksLikeNumberedAcronymTableRow(line) {
		return false
	}
	if outsidePageHorizontally(line, size) {
		return false
	}
	if looksLikeRunningHeader(text, line, size) ||
		looksLikeMultiEntryContentsLine(text) ||
		looksLikeCompactLetteredList(text) ||
		looksLikeSparseMultiCellLine(line) ||
		looksLikeStandaloneDate(text) ||
		looksLikeDocumentCode(text) {
		return false
	}
	var next *ParagraphTextLine
	if len(following) > 0 {
		next = &following[0]
	}
	wrappedDecimalHeading := isWrappedLongDecimalHeadingLine(line, next)
	if looksLikeRejectedNumericHeading(text) && !wrappedDecimalHeading {
		return false
	}
	if utf8.RuneCountInString(text) > maxHeadingRunes || len(wordsForHeading(text)) > maxHeadingWords {
		return false
	}
	if decimalHeadingLooksLikeFootnote(line, metric, bodyMetric, size) {
		return false
	}
	if wrappedDecimalHeading {
		return true
	}
	if !isStructuralHeadingLine(line, size) {
		return false
	}
	if bodyMetric > 0 && metric >= bodyMetric*2 && prominentShare(line, bodyMetric) >= 0.8 {
		return true
	}
	return hasAlignedSplitHeadingEvidence(line, following, bodyMetric)
}

func outsidePageHorizontally(line ParagraphTextLine, size geom.Size) bool {
	return size.Width > 0 && (line.BBox.R < 0 || line.BBox.L > size.Width)
}

func hasAlignedSplitHeadingEvidence(line ParagraphTextLine, following []ParagraphTextLine, bodyMetric float64) bool {
	text := collapseSpaces(line.Text)
	maxGap := math.Max(40, line.BBox.Height()*4)
	for _, next := range following {
		if next.BBox.T-line.BBox.B > maxGap {
			break
		}
		segment, ok := alignedFollowingSegment(line, next)
		if !ok {
			continue
		}
		if hasAlignedBodyLikeFollowingLine(line, segment, bodyMetric) {
			return true
		}
		if startsWithDecimalHeadingText(text) &&
			alignedWrappedDecimalContinuation(collapseSpaces(segment.Text), segment.BBox, lineMetric(segment), line) {
			return true
		}
	}
	return false
}

func alignedFollowingSegment(line, next ParagraphTextLine) (ParagraphTextLine, bool) {
	segments := lineSegmentsByLargeGaps(next)
	if len(segments) == 0 {
		return ParagraphTextLine{}, false
	}
	limit := math.Max(24, line.BBox.Height()*2.5)
	bestIndex := -1
	bestDelta := math.MaxFloat64
	for i, segment := range segments {
		if collapseSpaces(segment.Text) == "" {
			continue
		}
		delta := math.Abs(segment.BBox.L - line.BBox.L)
		if delta > limit {
			continue
		}
		if delta < bestDelta {
			bestIndex = i
			bestDelta = delta
		}
	}
	if bestIndex < 0 {
		return ParagraphTextLine{}, false
	}
	return segments[bestIndex], true
}

func isHeadingLine(line ParagraphTextLine, metric, bodyMetric float64, size geom.Size, prev, next *ParagraphTextLine) bool {
	text := collapseSpaces(line.Text)
	if text == "" {
		return false
	}
	if !hasLetter(text) || numberedCaptionPattern.MatchString(text) {
		return false
	}
	// A bold line set apart from the body margins and centred on the page is a
	// heading regardless of its font size — the signal many documents use for
	// section titles that share the body point size (parliamentary Hansard bill
	// titles, "First Reading"/"Second Reading"). Computed up front so it can
	// exempt a genuine centred-bold single all-caps word ("BILLS") from the
	// standalone-acronym guard below, which would otherwise reject it as a table
	// acronym.
	centredBold := isCentredBoldHeading(line, metric, bodyMetric, size)
	if looksLikeTableCaptionHeading(text) ||
		(!centredBold && (looksLikeStandaloneAcronymEntry(text) || looksLikeNumberedAcronymTableRow(line) || looksLikeAcronymTableEntry(text, prev, next))) {
		return false
	}
	if looksLikeRunningHeader(text, line, size) {
		return false
	}
	if looksLikeMultiEntryContentsLine(text) {
		return false
	}
	if looksLikeCompactLetteredList(text) {
		return false
	}
	if looksLikeTightImageCaptionTitleLine(line, next, bodyMetric) {
		return false
	}
	if looksLikeLabelValueTableHeader(line, next) {
		return false
	}
	wrappedDecimalHeading := isWrappedLongDecimalHeadingLine(line, next)
	if looksLikeRejectedNumericHeading(text) && !wrappedDecimalHeading {
		return false
	}
	if looksLikeSparseMultiCellLine(line) {
		return false
	}
	if looksLikeStandaloneDate(text) || looksLikeDocumentCode(text) {
		return false
	}
	if decimalHeadingPattern.MatchString(text) && hasAdjacentNumberedListSequence(prev, next) {
		return false
	}
	if looksLikeAffiliationBeforeEmail(line, next) {
		return false
	}
	if utf8.RuneCountInString(text) > maxHeadingRunes || len(wordsForHeading(text)) > maxHeadingWords {
		return false
	}
	if size.Height > 0 && line.BBox.T > size.Height*0.92 {
		return false
	}
	if decimalHeadingLooksLikeFootnote(line, metric, bodyMetric, size) {
		return false
	}

	fontProminent := metric >= bodyMetric*headingScaleFactor && prominentShare(line, bodyMetric) >= 0.8
	// Detection is fully document-general — font prominence, numbering structure,
	// isolated title-case lines, and isolated single-word section headers — with
	// NO literal section-name keyword (AGENTS.md §11). The commonSectionHeadings
	// map that formerly decided this tail has been removed entirely.
	return fontProminent || isStructuralHeadingLine(line, size) || wrappedDecimalHeading ||
		isIsolatedTitleHeading(line, metric, bodyMetric, prev, next) ||
		isIsolatedShortHeading(line, metric, bodyMetric, size, prev, next) ||
		centredBold
}

// isCentredBoldHeading reports whether a line is a bold, page-centred, narrow
// heading — the signal documents use for section titles set at (or near) the
// body point size, which every font-prominence path misses. It is
// document-general geometry + font weight (AGENTS.md §11): no literal text.
//
// Three conjuncts make it specific enough not to promote body text:
//   - the line is mostly bold (lineMostlyBold);
//   - its horizontal centre sits within 12% of the page centre; and
//   - it is inset from BOTH page margins by at least centredHeadingMinInset of
//     the page width.
//
// The both-sides inset is what separates a centred heading from a full-width
// bold paragraph line (a bold run-in lead spans the column, so one inset is
// tiny) and from a left-aligned bold fragment (small left inset). A font FLOOR
// (not set materially smaller than the body) rejects a centred bold caption
// beneath a figure. Isolation is deliberately NOT required: genuine headings
// (a bill title stacked directly above "First Reading") are tightly packed, and
// the centred+bold+narrow signal is already specific on its own.
//
// A text-cleanliness gate (isCleanHeadingText) rejects the bold-centred
// NON-headings common on figure/title pages: prose fragments and sentences,
// chart source captions ("Source: …"), and author/affiliation lines carrying
// footnote-reference markers. Lines caught by other paths (a large-font paper
// title) are unaffected — this only narrows the centred-bold path's own
// additions.
func isCentredBoldHeading(line ParagraphTextLine, metric, bodyMetric float64, size geom.Size) bool {
	if size.Width <= 0 || !lineMostlyBold(line) {
		return false
	}
	if bodyMetric > 0 && metric < bodyMetric-footnoteFontFloor {
		return false
	}
	if !isCleanHeadingText(collapseSpaces(line.Text)) {
		return false
	}
	centre := (line.BBox.L + line.BBox.R) / 2
	if math.Abs(centre-size.Width/2) > size.Width*0.12 {
		return false
	}
	minInset := size.Width * centredHeadingMinInset
	return line.BBox.L >= minInset && size.Width-line.BBox.R >= minInset
}

// isCleanHeadingText reports whether text reads like a heading rather than the
// bold-centred non-headings that share a heading's geometry on figure/title
// pages. A heading is title-like (its major words capitalised, so it does not
// begin lower-case mid-sentence), does not end in a sentence period, carries no
// interior colon (a chart caption "Source: …" or a "label: value" line), and
// bears no footnote-reference marker (author / affiliation lines). It
// deliberately does NOT use a naive ". "-sentence-break test, which a legitimate
// abbreviation in a title trips ("Tax Reform No. 2").
func isCleanHeadingText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasSuffix(text, ".") || hasInteriorColon(text) || !looksTitleLike(text) {
		return false
	}
	return !strings.ContainsAny(text, "∗†‡")
}

// lineStandsApart reports whether a line is vertically separated from its
// neighbours by more than ordinary paragraph leading — the geometric signature
// of a standalone heading versus a line packed inside a paragraph (e.g. a bold
// run-in whose next line is the paragraph continuation, tight below it). A
// missing neighbour (page top/bottom) counts as separated on that side.
func lineStandsApart(line ParagraphTextLine, prev, next *ParagraphTextLine, bodyMetric float64) bool {
	ref := bodyMetric
	if ref <= 0 {
		ref = line.BBox.Height()
	}
	if ref <= 0 {
		return false
	}
	gap := ref * 0.5
	aboveOK := prev == nil || line.BBox.T-prev.BBox.B >= gap
	belowOK := next == nil || next.BBox.T-line.BBox.B >= gap
	return aboveOK && belowOK
}

// hasInteriorColon reports whether text contains a colon before its final
// character. A heading-label ends in a single trailing colon ("Abstract:"); a
// line with an interior colon is typically a transcript/speaker attribution with
// an embedded timestamp ("Dr CHALMERS (Rankin—Treasurer) (09:01): I move:") —
// not a heading.
func hasInteriorColon(text string) bool {
	return strings.Contains(strings.TrimSuffix(text, ":"), ":")
}

func looksLikeLabelValueTableHeader(line ParagraphTextLine, next *ParagraphTextLine) bool {
	if next == nil {
		return false
	}
	left, right, ok := labelValueLineParts(line)
	if !ok || !isShortLabelValueLabel(left) || strings.TrimSpace(right) == "" {
		return false
	}
	nextLeft, nextRight, ok := labelValueLineParts(*next)
	return ok && isShortLabelValueLabel(nextLeft) && strings.TrimSpace(nextRight) != ""
}

func labelValueLineParts(line ParagraphTextLine) (string, string, bool) {
	segments := lineSegmentsByLargeGaps(line)
	if len(segments) < 2 {
		return "", "", false
	}
	rightParts := make([]string, 0, len(segments)-1)
	for _, segment := range segments[1:] {
		if text := collapseSpaces(segment.Text); text != "" {
			rightParts = append(rightParts, text)
		}
	}
	left := collapseSpaces(segments[0].Text)
	right := collapseSpaces(strings.Join(rightParts, " "))
	return left, right, left != "" && right != ""
}

func isShortLabelValueLabel(text string) bool {
	text = collapseSpaces(text)
	if text == "" || strings.HasSuffix(text, ".") || orderedListMarkerPattern.MatchString(text) || isPlainIntegerToken(text) {
		return false
	}
	words := wordsForHeading(text)
	if len(words) == 0 || len(words) > 5 {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// decimalHeadingLooksLikeFootnote reports whether a single-integer "decimal
// heading" is actually sub-body content rather than a section heading. Two shapes
// masquerade as a single-integer decimal heading ("4 …", as opposed to a dotted
// subsection "2.1.3" which is unambiguous):
//
//   - a footnote ("4 Note that:" — footnote 4, the same digit referenced as a
//     superscript in the body above), set in a smaller point size than the body;
//   - a numeric-scale / rubric row ("4 Rare, crucial insights comparable to world"
//     atop a 0-4 scale), whose text is a sentence/description, not a title.
//
// Both are caught geometrically/typographically, narrowly, so real headings are
// untouched (broader signals — page-foot position, word count — reject genuine
// headings and regress DPBench MHS, measured): a font FLOOR (a heading is never
// set smaller than the body) and an internal comma followed by a lowercase word
// (a title capitalises its major words; ", crucial" is a prose continuation).
// Both signals leave DPBench MHS unchanged.
func decimalHeadingLooksLikeFootnote(line ParagraphTextLine, metric, bodyMetric float64, size geom.Size) bool {
	text := collapseSpaces(line.Text)
	if bodyMetric <= 0 || !isSingleIntegerDecimalHeading(text) {
		return false
	}
	if metric < bodyMetric-footnoteFontFloor {
		return true
	}
	match := decimalHeadingPrefixPattern.FindStringSubmatch(text)
	return match != nil && hasProseCommaContinuation(match[2])
}

// hasProseCommaContinuation reports whether the text has an internal comma
// followed by a lowercase word that is not a coordinating conjunction — a
// sentence/description continuation ("Rare, crucial insights …"), not a title.
// Titles capitalise their major words, so a lowercase word after a comma marks
// prose; "and"/"or"/"nor" are excluded because they legitimately follow a comma
// in a title ("Methods, Results, and Discussion").
func hasProseCommaContinuation(text string) bool {
	for idx := 0; idx < len(text); {
		comma := strings.IndexByte(text[idx:], ',')
		if comma < 0 {
			return false
		}
		idx += comma + 1
		rest := strings.TrimLeft(text[idx:], " ")
		word := rest
		if cut := strings.IndexAny(rest, " ,"); cut >= 0 {
			word = rest[:cut]
		}
		first, _ := utf8.DecodeRuneInString(word)
		if unicode.IsLower(first) {
			switch strings.ToLower(word) {
			case "and", "or", "nor":
			default:
				return true
			}
		}
	}
	return false
}

// isSingleIntegerDecimalHeading reports whether a line begins with a single
// (un-dotted) integer marker followed by text, e.g. "4 Results" — but not a
// dotted subsection like "2.1.3 …".
func isSingleIntegerDecimalHeading(text string) bool {
	match := decimalHeadingPrefixPattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return false
	}
	marker := strings.TrimSuffix(match[1], ".")
	return marker != "" && !strings.Contains(marker, ".")
}

func isStructuralHeadingLine(line ParagraphTextLine, size geom.Size) bool {
	text := collapseSpaces(line.Text)
	if text == "" || (isOrderedListItemLine(line) && !isDecimalHeadingText(text)) {
		return false
	}
	words := wordsForHeading(text)
	if len(words) == 0 || len(words) > 14 {
		return false
	}
	// NOTE: a literal-word section-name map (commonSectionHeadings) was previously
	// an inclusion here. It classified a heading purely by matching the document's
	// literal text ("abstract"/"references"/...), an AGENTS.md §11 violation
	// (character content as the primary heading signal). It has been removed
	// entirely; font prominence, numbering structure, colon-suffix and all-caps
	// below remain document-general signals.
	if isDecimalHeadingText(text) ||
		appendixDecimalHeadingPattern.MatchString(text) ||
		appendixLetterHeadingPattern.MatchString(text) ||
		romanHeadingPattern.MatchString(text) ||
		isLowercaseLetterHeadingText(text) {
		return true
	}
	// A trailing-colon label is promoted only when visually distinguished (bold).
	// A non-bold body-size colon line is prose — a sentence lead-in ("This
	// investment will:") or a transcript fragment — not a section header.
	if strings.HasSuffix(text, ":") && len(words) <= 3 && !startsWithLowercaseLetter(text) && !looksLikeBibliographyTitleLine(text) && !hasInteriorColon(text) && lineMostlyBold(line) {
		return true
	}
	// An all-caps line is a heading only if it is positioned like one (see
	// headingPositioned): a body-size all-caps word floating mid-page (e.g.
	// "MINISTER" atop the right column of a two-column key/value list) is a column
	// header, not a section header.
	if len(words) <= 12 && mostlyUppercase(text) && !isShortAllCapsAcronym(text) && !isNumericDominantLine(words) && headingPositioned(line, size) {
		return true
	}
	return false
}

// isNumericDominantLine reports whether a line is dominated by numeric tokens, e.g. a
// table data row such as "OTSL 0.965 0.934 0.955 0.88 2.73" or "EDD PTN 91.1 88.7 89.9".
// mostlyUppercase counts only letters and therefore treats a single all-caps acronym
// followed by several numbers as an all-caps heading. Such numeric-dominant lines are
// table data, not structural headings, so they must be excluded from the all-caps
// heading branch. A genuine all-caps heading remains alphabetic-dominant (e.g.
// "TABLE 23: IMPRISONMENT CLAUSES" has a single number among many words and is kept).
func isNumericDominantLine(words []string) bool {
	if len(words) < 2 {
		return false
	}
	numeric := 0
	for _, word := range words {
		if isNumericHeadingToken(word) {
			numeric++
		}
	}
	return numeric >= 2 && numeric*2 >= len(words)
}

// isNumericHeadingToken reports whether a whitespace-delimited token is a standalone
// number, allowing common numeric punctuation (decimal point, sign, thousands comma,
// slash, and surrounding parentheses/brackets/percent). A bare "-" placeholder used in
// numeric tables ("- -") is treated as numeric so that placeholder-bearing data rows
// are still recognised as data rather than headings.
func isNumericHeadingToken(word string) bool {
	trimmed := strings.Trim(word, "()[]%")
	if trimmed == "" {
		return false
	}
	if trimmed == "-" {
		return true
	}
	hasDigit := false
	for _, r := range trimmed {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case r == '.' || r == ',' || r == '-' || r == '+' || r == '/':
		default:
			return false
		}
	}
	return hasDigit
}

func looksLikeBibliographyTitleLine(text string) bool {
	text = collapseSpaces(text)
	if !strings.HasSuffix(text, ":") {
		return false
	}
	colon := strings.LastIndex(text, ":")
	if colon < 0 {
		return false
	}
	prefix := text[:colon]
	if !containsPublicationYear(prefix) || !strings.Contains(prefix, ",") {
		return false
	}
	return strings.Contains(strings.ToLower(prefix), " and ") || strings.Count(prefix, ",") >= 2
}

func containsPublicationYear(text string) bool {
	for field := range strings.FieldsSeq(text) {
		trimmed := strings.Trim(field, ".,;:()[]{}")
		if len(trimmed) != 4 {
			continue
		}
		year, err := strconv.Atoi(trimmed)
		if err != nil {
			continue
		}
		if year >= 1800 && year <= 2099 {
			return true
		}
	}
	return false
}

func isIsolatedTitleHeading(line ParagraphTextLine, metric, bodyMetric float64, prev, next *ParagraphTextLine) bool {
	text := collapseSpaces(line.Text)
	words := wordsForHeading(text)
	if len(words) < 2 || len(words) > 8 {
		return false
	}
	if metric < bodyMetric*1.03 || hasDigit(text) || strings.HasSuffix(text, ".") {
		return false
	}
	if !looksTitleLike(text) {
		return false
	}
	height := math.Max(1, line.BBox.Height())
	if next == nil || next.BBox.T-line.BBox.B < height || !hasAlignedBodyLikeFollowingLine(line, *next, bodyMetric) {
		return false
	}
	if prev != nil && line.BBox.T-prev.BBox.B < height*0.75 {
		return false
	}
	return true
}

// headingPositioned reports whether a line sits where a section header sits: at
// the text margin, horizontally centred, or bold. A body-size line floating
// mid-page — a column header in a multi-column list — fails this. The gate is
// permissive when page width is unknown (synthetic inputs) so it never
// spuriously rejects. It deliberately admits a two-column paper's right-column
// header (that column's text also starts at that column's margin, but such
// headers are bold or prominent and reach detection via other branches).
func headingPositioned(line ParagraphTextLine, size geom.Size) bool {
	if size.Width <= 0 {
		return true
	}
	lineCentre := (line.BBox.L + line.BBox.R) / 2
	centred := math.Abs(lineCentre-size.Width/2) <= size.Width*0.12
	return line.BBox.L <= size.Width*0.15 || centred || lineMostlyBold(line)
}

// lineMostlyBold reports whether ≥80% of a line's characters sit in bold cells.
func lineMostlyBold(line ParagraphTextLine) bool {
	total, bold := 0, 0
	for _, cell := range line.Cells {
		t := strings.TrimSpace(cell.Text)
		if t == "" {
			continue
		}
		n := utf8.RuneCountInString(t)
		total += n
		if cell.IsBold() {
			bold += n
		}
	}
	return total > 0 && float64(bold)/float64(total) >= 0.8
}

// isIsolatedShortHeading promotes a SINGLE-WORD line (section names like
// "Abstract"/"Introduction"/"References" that isIsolatedTitleHeading's ≥2-word
// rule skips — multi-word title-case lines are already its domain) on
// document-general layout signals only — no literal text (AGENTS.md §11). It is
// the keyword-free replacement for the commonSectionHeadings crutch: the line is
// a heading when it is heading-SHAPED (bold, all-caps, prominent, or
// title-case), strongly ISOLATED above and below, and FOLLOWED by body-like
// prose. The following-prose guard is what keeps bold table-header rows out (a
// table header is followed by another data row, not a paragraph) — the failure
// mode of a bold-only signal.
func isIsolatedShortHeading(line ParagraphTextLine, metric, bodyMetric float64, size geom.Size, prev, next *ParagraphTextLine) bool {
	text := collapseSpaces(line.Text)
	words := wordsForHeading(text)
	if len(words) != 1 {
		return false
	}
	if !startsWithUppercaseLetter(text) || hasDigit(text) || strings.HasSuffix(text, ".") {
		return false
	}
	// A lone word must also be positioned like a heading — this rejects an
	// all-caps or capitalized column label ("MINISTER") floating mid-page atop a
	// tabular list, which the shape test alone would admit.
	if !headingPositioned(line, size) {
		return false
	}
	// Visual distinction is required: a lone word must be bold, all-caps, or
	// larger than body. Title-case alone is NOT enough — a plain body-size
	// capitalized word is typically a column label ("Members", "Division") atop a
	// tabular list, not a section header.
	headingShaped := lineMostlyBold(line) || mostlyUppercase(text) || metric >= bodyMetric*1.03
	if !headingShaped {
		return false
	}
	if next == nil || !hasAlignedBodyLikeFollowingLine(line, *next, bodyMetric) {
		return false
	}
	height := math.Max(1, line.BBox.Height())
	if next.BBox.T-line.BBox.B < height {
		return false
	}
	if prev != nil && line.BBox.T-prev.BBox.B < height*0.75 {
		return false
	}
	return true
}

func looksLikeSparseMultiCellLine(line ParagraphTextLine) bool {
	if len(line.Cells) < 2 {
		return false
	}
	cells := append([]page.TextCell(nil), line.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Box.T < cells[j].Box.T
		}
		return cells[i].Box.L < cells[j].Box.L
	})

	largeGaps := 0
	threshold := math.Max(36, line.BBox.Height()*4)
	for i := 1; i < len(cells); i++ {
		left := strings.TrimSpace(cells[i-1].Text)
		right := strings.TrimSpace(cells[i].Text)
		if left == "" || right == "" {
			continue
		}
		if cells[i].Box.L-cells[i-1].Box.R > threshold {
			largeGaps++
		}
	}
	return largeGaps > 0 && line.BBox.Width() > math.Max(180, line.BBox.Height()*18)
}

func isWrappedLongDecimalHeadingLine(line ParagraphTextLine, next *ParagraphTextLine) bool {
	if next == nil {
		return false
	}
	text := collapseSpaces(line.Text)
	match := decimalHeadingPrefixPattern.FindStringSubmatch(text)
	if match == nil {
		return false
	}
	marker := strings.TrimSuffix(match[1], ".")
	if strings.HasPrefix(marker, "0.") {
		return false
	}
	firstNumber := marker
	if dot := strings.IndexByte(firstNumber, '.'); dot >= 0 {
		firstNumber = firstNumber[:dot]
	}
	if value, err := strconv.Atoi(firstNumber); err != nil || value > 20 {
		return false
	}
	rest := match[2]
	if startsWithLowercaseLetter(rest) || len(wordsForHeading(rest)) <= 7 {
		return false
	}
	return alignedWrappedDecimalContinuation(collapseSpaces(next.Text), next.BBox, lineMetric(*next), line)
}

func hasAlignedBodyLikeFollowingLine(line, next ParagraphTextLine, bodyMetric float64) bool {
	text := collapseSpaces(next.Text)
	if text == "" || strings.ContainsAny(text, "@{}") {
		return false
	}
	if bodyMetric > 0 && lineMetric(next) > bodyMetric*1.08 {
		return false
	}
	if line.BBox.L-next.BBox.L > math.Max(24, line.BBox.Height()*2.5) {
		return false
	}
	words := wordsForHeading(text)
	return len(words) >= 4 || isOrderedListItemLine(next)
}

func looksLikeAffiliationBeforeEmail(line ParagraphTextLine, next *ParagraphTextLine) bool {
	if next == nil || !strings.Contains(collapseSpaces(next.Text), "@") {
		return false
	}
	text := collapseSpaces(line.Text)
	words := wordsForHeading(text)
	if len(words) < 2 || len(words) > 8 || hasDigit(text) || startsWithHeadingMarker(text) {
		return false
	}
	if !looksTitleLike(text) {
		return false
	}
	return line.BBox.L-next.BBox.L > math.Max(24, line.BBox.Height()*2.5)
}

func looksLikeStandaloneDate(text string) bool {
	return standaloneMonthYearPattern.MatchString(strings.TrimSpace(text))
}

func looksLikeDocumentCode(text string) bool {
	text = strings.TrimSpace(text)
	if !documentCodePattern.MatchString(text) {
		return false
	}
	return hasDigit(text)
}

func hasAdjacentNumberedListSequence(prev, next *ParagraphTextLine) bool {
	return numberedListMarkerCount(prev) >= 1 || numberedListMarkerCount(next) >= 1
}

func numberedListMarkerCount(line *ParagraphTextLine) int {
	if line == nil {
		return 0
	}
	return len(orderedListSequencePattern.FindAllString(collapseSpaces(line.Text), -1))
}

func looksTitleLike(text string) bool {
	words := strings.Fields(text)
	if len(words) == 0 || startsWithLowercaseLetter(text) {
		return false
	}
	titleWords := 0
	for _, word := range words {
		if isSmallHeadingWord(word) {
			continue
		}
		if startsWithUppercaseLetter(word) || isAllCapsWord(word) {
			titleWords++
		}
	}
	return titleWords >= 1 && float64(titleWords)/float64(len(words)) >= 0.5
}

func looksLikeTableCaptionHeading(text string) bool {
	fields := strings.Fields(headingLabelKey(text))
	if len(fields) == 0 {
		return false
	}
	if fields[0] != "table" && fields[0] != "tab" {
		return false
	}
	return len(fields) < 2 || fields[1] != "of"
}

func looksLikeStandaloneAcronymEntry(text string) bool {
	words := strings.Fields(collapseSpaces(text))
	if len(words) != 1 {
		return false
	}
	word := strings.TrimFunc(words[0], func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return utf8.RuneCountInString(word) <= 7 && isAllCapsWord(word)
}

func looksLikeNumberedAcronymTableRow(line ParagraphTextLine) bool {
	fields := strings.Fields(collapseSpaces(line.Text))
	if len(fields) != 2 || !isPlainIntegerToken(fields[0]) {
		return false
	}
	word := strings.TrimFunc(fields[1], func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return utf8.RuneCountInString(word) <= 12 && isAllCapsWord(word)
}

func looksLikeAcronymTableEntry(text string, prev, next *ParagraphTextLine) bool {
	words := wordsForHeading(text)
	if len(words) != 1 || utf8.RuneCountInString(words[0]) > 12 || !isAllCapsWord(words[0]) {
		return false
	}
	return hasNumericTableCue(prev) || hasNumericTableCue(next)
}

func hasNumericTableCue(line *ParagraphTextLine) bool {
	if line == nil {
		return false
	}
	for field := range strings.FieldsSeq(collapseSpaces(line.Text)) {
		field = strings.Trim(field, ".,;:()[]{}")
		if isPlainIntegerToken(strings.ReplaceAll(field, ",", "")) {
			return true
		}
	}
	return false
}

func isPlainIntegerToken(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func startsWithUppercaseLetter(text string) bool {
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		return unicode.IsUpper(r)
	}
	return false
}

func isSmallHeadingWord(word string) bool {
	key := headingLabelKey(word)
	switch key {
	case "a", "an", "and", "as", "between", "for", "in", "of", "on", "or", "the", "to", "vs", "with":
		return true
	default:
		return false
	}
}

func isAllCapsWord(word string) bool {
	letters := 0
	for _, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsLower(r) {
			return false
		}
	}
	return letters >= 2
}

func isContentsHeading(text string) bool {
	key := headingLabelKey(text)
	return key == "contents" || key == "table of contents"
}

func isDecimalHeadingText(text string) bool {
	match := decimalHeadingPrefixPattern.FindStringSubmatch(text)
	if match == nil || !decimalHeadingPattern.MatchString(text) {
		return false
	}
	marker := strings.TrimSuffix(match[1], ".")
	rest := match[2]
	if strings.HasPrefix(marker, "0.") {
		return false
	}
	firstNumber := marker
	if dot := strings.IndexByte(firstNumber, '.'); dot >= 0 {
		firstNumber = firstNumber[:dot]
	}
	if value, err := strconv.Atoi(firstNumber); err == nil {
		if value >= 100 {
			return false
		}
		if !strings.Contains(marker, ".") && value > 20 {
			return false
		}
	}
	if !strings.Contains(marker, ".") && len(wordsForHeading(rest)) > 7 {
		return false
	}
	return !startsWithLowercaseLetter(rest)
}

func looksLikeRejectedNumericHeading(text string) bool {
	return decimalHeadingPattern.MatchString(text) && !isDecimalHeadingText(text)
}

func isLowercaseLetterHeadingText(text string) bool {
	if !lowercaseLetterHeadingPattern.MatchString(text) {
		return false
	}
	if len(wordsForHeading(text)) > 8 {
		return false
	}
	rest := strings.TrimSpace(text[2:])
	return !hasInternalSentenceBreak(rest)
}

func hasInternalSentenceBreak(text string) bool {
	for i := 0; i+2 < len(text); i++ {
		if text[i] == '.' && text[i+1] == ' ' {
			return true
		}
	}
	return false
}

func isOrderedListItemLine(line ParagraphTextLine) bool {
	if len(line.Cells) < 2 {
		return false
	}
	first := strings.TrimSpace(line.Cells[0].Text)
	return orderedListMarkerPattern.MatchString(first)
}

func looksLikeRunningHeader(text string, line ParagraphTextLine, size geom.Size) bool {
	if size.Height <= 0 || line.BBox.T > size.Height*0.12 {
		return false
	}
	if !mostlyUppercase(text) || hasLowercaseLetter(text) || !hasDigit(text) {
		return false
	}
	words := wordsForHeading(text)
	return len(words) >= 3
}

func looksLikeMultiEntryContentsLine(text string) bool {
	return len(numberedSectionMarkerPattern.FindAllString(text, -1)) >= 2 ||
		len(partSectionMarkerPattern.FindAllString(text, -1)) >= 2
}

func looksLikeCompactLetteredList(text string) bool {
	return len(lowercaseLetterMarkerPattern.FindAllString(text, -1)) >= 2
}

func mostlyUppercase(text string) bool {
	letters := 0
	uppercase := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			uppercase++
		}
	}
	return letters >= 4 && float64(uppercase)/float64(letters) >= 0.8
}

func isShortAllCapsAcronym(text string) bool {
	words := wordsForHeading(text)
	if len(words) != 1 {
		return false
	}
	letters := 0
	for _, r := range text {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters > 0 && letters <= 5
}

func startsWithLowercaseLetter(text string) bool {
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		return unicode.IsLower(r)
	}
	return false
}

func hasLowercaseLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLower(r) {
			return true
		}
	}
	return false
}

func hasDigit(text string) bool {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func hasLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func prominentShare(line ParagraphTextLine, bodyMetric float64) float64 {
	total := 0
	prominent := 0
	for _, cell := range line.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		count := utf8.RuneCountInString(text)
		total += count
		metric := cell.FontSize
		if metric <= 0 {
			metric = cell.Box.Height()
		}
		if metric >= bodyMetric*headingScaleFactor {
			prominent += count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(prominent) / float64(total)
}

func headingLabelKey(text string) string {
	var b strings.Builder
	needsSpace := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if needsSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			needsSpace = false
			continue
		}
		if b.Len() > 0 {
			needsSpace = true
		}
	}
	return b.String()
}

// maxHeadingDepth caps the deepest Markdown heading level emitted. Real documents
// rarely express more than four levels of hierarchy, and capping guards against a
// noisy font-size spread fanning out into ###### headings.
const maxHeadingDepth = 4

// assignHeadingLevels assigns each heading a Markdown level (#, ##, ###, …).
//
// Decimal section numbering is the hierarchy signal: a heading prefixed "N.M.…"
// sits at the depth of its number (1 → level 1, 1.2 → level 2, 1.2.3 → level 3),
// because "1.2" is unambiguously a subsection of "1". Unnumbered headings stay at
// level 1. When a document numbers its sections the numbered subsections are not
// shifted to level 1 just because their font matches a top-level heading, so the
// emitted Markdown reflects the real outline. Font size is deliberately NOT used
// to tier headings: a measured size step does not imply nesting (a chapter LABEL
// is often set smaller than the chapter TITLE beneath it), and font-tiering was
// measured to invert order (a ## emitted before its #) and fragment titles.
//
// When no heading is numbered the document is left flat (every heading level 1),
// which is correct for prose/book pages and matches the common ground-truth
// convention; deriving a reliable nested outline from font sizes alone needs a
// document-flow nesting model that is deferred.
func assignHeadingLevels(headings []headingLine) {
	sort.SliceStable(headings, func(i, j int) bool {
		return headings[i].line.MinIndex < headings[j].line.MinIndex
	})

	// Depths per heading (0 = unnumbered). Normalise so the page's shallowest
	// heading sits at level 1: when every heading is a numbered subsection with no
	// shallower parent on the page (e.g. a page whose only heading is "1.5 …"),
	// shift the numbering up so it renders as # rather than an orphan ##. When an
	// unnumbered heading is present it anchors level 1, so numbered subsections
	// keep their true depth beneath it (a "## 4.1" under a "# Chapter 4").
	depths := make([]int, len(headings))
	hasUnnumbered := false
	minNumbered := 0
	for i := range headings {
		d := headingNumberDepth(collapseSpaces(headings[i].line.Text))
		depths[i] = d
		if d == 0 {
			hasUnnumbered = true
		} else if minNumbered == 0 || d < minNumbered {
			minNumbered = d
		}
	}
	shift := 0
	if !hasUnnumbered && minNumbered > 1 {
		shift = minNumbered - 1
	}

	for i := range headings {
		level := 1
		if depths[i] > 0 {
			level = depths[i] - shift
		}
		if level < 1 {
			level = 1
		}
		if level > maxHeadingDepth {
			level = maxHeadingDepth
		}
		headings[i].level = level
	}
}

// assignDocumentHeadingLevels re-levels every heading block across ALL pages from
// one global decimal-numbering hierarchy, then rewrites each block's "#" prefix.
// Per-page assignHeadingLevels cannot see the document outline: a page whose only
// heading is a deep subsection ("4.3.1") has no shallower parent on it, so the
// per-page normaliser collapses it to level 1 — wrong, because across the whole
// document "4" and "4.3" appear on earlier pages and "4.3.1" is genuinely level
// 3. Re-normalising against the global minimum numbering depth restores the
// coherent outline (2 → 2.1 → 2.1.3 → 2.1.3.1 = #, ##, ###, ####).
//
// For a single-page document this is identical to the per-page result (the global
// minimum is the page minimum), so it does not disturb single-page extraction.
func assignDocumentHeadingLevels(pageBlocks [][]markdownBlock) {
	type ref struct {
		page, index, depth int
	}
	refs := make([]ref, 0)
	hasUnnumbered := false
	minNumbered := 0
	for p := range pageBlocks {
		for i := range pageBlocks[p] {
			if pageBlocks[p][i].HeadingLevel <= 0 {
				continue
			}
			depth := headingNumberDepth(headingTextBody(pageBlocks[p][i].Text))
			refs = append(refs, ref{page: p, index: i, depth: depth})
			if depth == 0 {
				hasUnnumbered = true
			} else if minNumbered == 0 || depth < minNumbered {
				minNumbered = depth
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	shift := 0
	if !hasUnnumbered && minNumbered > 1 {
		shift = minNumbered - 1
	}
	for _, r := range refs {
		level := 1
		if r.depth > 0 {
			level = r.depth - shift
		}
		if level < 1 {
			level = 1
		}
		if level > maxHeadingDepth {
			level = maxHeadingDepth
		}
		block := &pageBlocks[r.page][r.index]
		block.HeadingLevel = level
		block.Text = strings.Repeat("#", level) + " " + headingTextBody(block.Text)
	}
}

// headingTextBody returns a heading block's text with its leading "#" prefix and
// following space removed ("## 4.1 Intro" → "4.1 Intro").
func headingTextBody(text string) string {
	return strings.TrimSpace(strings.TrimLeft(text, "#"))
}

// headingNumberDepth returns the decimal section-numbering depth of a heading
// ("1" → 1, "1.2" → 2, "2.10.3" → 3) or 0 when the heading is not numbered. A
// trailing dot ("4.") is a label, not a deeper level, so it is stripped first.
func headingNumberDepth(text string) int {
	match := decimalHeadingPrefixPattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return 0
	}
	prefix := strings.TrimSuffix(match[1], ".")
	if prefix == "" {
		return 0
	}
	return strings.Count(prefix, ".") + 1
}

func mergeAdjacentHeadingLines(headings []headingLine) []headingLine {
	if len(headings) < 2 {
		return headings
	}

	merged := make([]headingLine, 0, len(headings))
	current := headings[0]
	for _, next := range headings[1:] {
		if canMergeHeadingLines(current, next) {
			current = mergeHeadingLine(current, next)
			continue
		}
		merged = append(merged, current)
		current = next
	}
	merged = append(merged, current)
	return merged
}

func canMergeHeadingLines(left, right headingLine) bool {
	if left.level != right.level {
		return false
	}
	if math.Abs(left.metric-right.metric) > headingBandTolerance {
		return false
	}
	gap := right.line.BBox.T - left.line.BBox.B
	limit := math.Max(6, left.line.BBox.Height()*0.5)
	if startsWithLowercaseLetter(right.line.Text) {
		limit = math.Max(limit, left.line.BBox.Height()*0.9)
	}
	if looksLikeAlignedWrappedHeadingLine(left.line, right.line) {
		limit = math.Max(limit, left.line.BBox.Height()*0.9)
	}
	return gap >= -left.line.BBox.Height() && gap <= limit
}

func looksLikeAlignedWrappedHeadingLine(left, right ParagraphTextLine) bool {
	leftWidth := left.BBox.Width()
	rightWidth := right.BBox.Width()
	if leftWidth <= 0 || rightWidth <= 0 {
		return false
	}
	return math.Abs(left.BBox.L-right.BBox.L) <= 4 && rightWidth <= leftWidth*0.75
}

func mergeHeadingLine(left, right headingLine) headingLine {
	line := ParagraphTextLine{
		Text:     joinHeadingText(left.line.Text, right.line.Text),
		BBox:     geom.EnclosingBox(left.line.BBox, right.line.BBox),
		MinIndex: left.line.MinIndex,
		FontSize: math.Max(left.line.FontSize, right.line.FontSize),
		Cells:    append(append([]page.TextCell(nil), left.line.Cells...), right.line.Cells...),
	}
	if right.line.MinIndex < line.MinIndex {
		line.MinIndex = right.line.MinIndex
	}
	return headingLine{
		line:   line,
		metric: math.Max(left.metric, right.metric),
		level:  left.level,
	}
}

func joinHeadingText(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if joinsWordContinuation(left, right) {
		return left + right
	}
	return left + " " + right
}

func joinsWordContinuation(left, right string) bool {
	last, _ := utf8.DecodeLastRuneInString(left)
	first, _ := utf8.DecodeRuneInString(right)
	return unicode.IsLetter(last) && unicode.IsLower(first) && len(strings.Fields(right)) == 1 && utf8.RuneCountInString(right) <= 6
}

func wordsForHeading(s string) []string {
	return strings.Fields(s)
}
