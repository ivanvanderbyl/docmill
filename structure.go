package docmill

import (
	"math"
	"sort"
	"unicode"
)

// buildParagraphs groups words into lines and paragraphs with rotation and column awareness.
// stats may be nil when document-wide statistics are not available.
func buildParagraphs(words []EnrichedWord, pageWidth float64, config Config, stats *DocumentStats) []Paragraph {
	if len(words) == 0 {
		return nil
	}

	// Detect text rotation and group into blocks
	textBlocks := detectTextRotation(words)

	// If no rotation detected, create single block with all words
	if len(textBlocks) == 0 {
		// Sort words by visual position (Y overlap, then X position)
		sortedWords := make([]EnrichedWord, len(words))
		copy(sortedWords, words)
		sort.Slice(sortedWords, func(i, j int) bool {
			wordI := sortedWords[i]
			wordJ := sortedWords[j]

			// Check if words are on the same visual line using Y-coordinate overlap
			overlapY0 := math.Max(wordI.Box.Y0, wordJ.Box.Y0)
			overlapY1 := math.Min(wordI.Box.Y1, wordJ.Box.Y1)
			overlapHeight := overlapY1 - overlapY0

			minHeight := math.Min(wordI.Box.Height(), wordJ.Box.Height())

			// If boxes overlap vertically by >30%, they're on the same visual line
			// Sort by X position (left to right)
			if overlapHeight > minHeight*0.3 {
				return wordI.Box.X0 < wordJ.Box.X0
			}

			// Different lines - sort by Y position (top to bottom)
			// Use top of bounding box for more reliable sorting
			return wordI.Box.Y0 < wordJ.Box.Y0
		})

		// Deliberately left empty - debug code removed

		lines := groupWordsIntoLinesBaseline(sortedWords)

		textBlocks = []TextBlock{
			{
				Words:            sortedWords,
				Lines:            lines,
				Rotation:         0,
				ReadingDirection: "ltr",
			},
		}
	}

	// Merge words that are too close together within each line
	for bi := range textBlocks {
		for li := range textBlocks[bi].Lines {
			textBlocks[bi].Lines[li].Words = mergeCloseWords(textBlocks[bi].Lines[li].Words)
		}
	}

	// Collect all lines from all blocks
	var allLines []Line
	for _, block := range textBlocks {
		allLines = append(allLines, block.Lines...)
	}

	// Group lines into paragraphs with adaptive spacing
	paragraphs := groupLinesIntoParagraphsAdaptive(allLines, pageWidth, stats)

	// Detect columns for reading order
	columns := detectColumns(words, pageWidth)

	// Determine reading order with column awareness
	paragraphs = determineReadingOrder(paragraphs, columns)

	// Detect heading levels
	detectHeadings(paragraphs, config, stats)

	// Detect lists
	detectLists(paragraphs)

	// Detect code blocks
	detectCodeBlocks(paragraphs)

	return paragraphs
}

// stats may be nil when document-wide statistics are not available.
func buildParagraphsNoDetection(words []EnrichedWord, pageWidth float64, config Config, stats *DocumentStats) []Paragraph {
	if len(words) == 0 {
		return nil
	}

	textBlocks := detectTextRotation(words)

	if len(textBlocks) == 0 {
		sortedWords := make([]EnrichedWord, len(words))
		copy(sortedWords, words)
		sort.Slice(sortedWords, func(i, j int) bool {
			wordI := sortedWords[i]
			wordJ := sortedWords[j]

			overlapY0 := math.Max(wordI.Box.Y0, wordJ.Box.Y0)
			overlapY1 := math.Min(wordI.Box.Y1, wordJ.Box.Y1)
			overlapHeight := overlapY1 - overlapY0
			minHeight := math.Min(wordI.Box.Height(), wordJ.Box.Height())

			if overlapHeight > minHeight*0.3 {
				return wordI.Box.X0 < wordJ.Box.X0
			}
			return wordI.Box.Y0 < wordJ.Box.Y0
		})

		lines := groupWordsIntoLinesBaseline(sortedWords)

		textBlocks = []TextBlock{
			{
				Words:            sortedWords,
				Lines:            lines,
				Rotation:         0,
				ReadingDirection: "ltr",
			},
		}
	}

	for bi := range textBlocks {
		for li := range textBlocks[bi].Lines {
			textBlocks[bi].Lines[li].Words = mergeCloseWords(textBlocks[bi].Lines[li].Words)
		}
	}

	var allLines []Line
	for _, block := range textBlocks {
		allLines = append(allLines, block.Lines...)
	}

	paragraphs := groupLinesIntoParagraphsAdaptive(allLines, pageWidth, stats)

	columns := detectColumns(words, pageWidth)
	paragraphs = determineReadingOrder(paragraphs, columns)

	return paragraphs
}

// groupWordsIntoLines groups words that are on the same horizontal line.
func groupWordsIntoLines(words []EnrichedWord) []Line {
	if len(words) == 0 {
		return nil
	}

	var lines []Line
	var currentLine []EnrichedWord
	var lineBox Rect
	var baseline float64

	for i, word := range words {
		wordBaseline := word.Box.Y1 // Bottom of word is the baseline

		if len(currentLine) == 0 {
			// Start new line
			currentLine = []EnrichedWord{word}
			lineBox = word.Box
			baseline = wordBaseline
		} else {
			// Check if word belongs to current line
			yDiff := math.Abs(wordBaseline - baseline)
			if yDiff < 3 { // Same line threshold in points
				// Add to current line
				currentLine = append(currentLine, word)
				lineBox.X0 = math.Min(lineBox.X0, word.Box.X0)
				lineBox.Y0 = math.Min(lineBox.Y0, word.Box.Y0)
				lineBox.X1 = math.Max(lineBox.X1, word.Box.X1)
				lineBox.Y1 = math.Max(lineBox.Y1, word.Box.Y1)
			} else {
				// End current line, start new one
				lines = append(lines, Line{
					Words:    currentLine,
					Box:      lineBox,
					Baseline: baseline,
				})
				currentLine = []EnrichedWord{word}
				lineBox = word.Box
				baseline = wordBaseline
			}
		}

		// End of text
		if i == len(words)-1 && len(currentLine) > 0 {
			lines = append(lines, Line{
				Words:    currentLine,
				Box:      lineBox,
				Baseline: baseline,
			})
		}
	}

	return lines
}

// groupWordsIntoLinesBaseline groups words into lines using visual overlap
// Uses Y-coordinate bounding box overlap as the primary signal for same-line detection
func groupWordsIntoLinesBaseline(words []EnrichedWord) []Line {
	if len(words) == 0 {
		return nil
	}

	var lines []Line
	var currentLine []EnrichedWord
	var lineBox Rect
	var baseline float64
	var xHeight float64

	for i, word := range words {
		if len(currentLine) == 0 {
			// Start new line
			currentLine = []EnrichedWord{word}
			lineBox = word.Box
			baseline = word.Baseline
			xHeight = word.XHeight
		} else {
			// Use VISUAL POSITIONING to determine if word is on the same line
			// Calculate the vertical center of both the word and the current line
			lineCenterY := (lineBox.Y0 + lineBox.Y1) / 2
			wordCenterY := (word.Box.Y0 + word.Box.Y1) / 2

			// Calculate vertical distance between centers
			centerDistance := math.Abs(wordCenterY - lineCenterY)

			// Average height for reference
			avgHeight := (lineBox.Height() + word.Box.Height()) / 2

			// Words are on the same visual line if their centers are within 1.0× of average height
			// This is very lenient to handle cases where small elements (hyphens, periods)
			// are positioned slightly differently but visually on the same line
			visuallySameLine := centerDistance < avgHeight*1.0

			// Baseline check as fallback
			baselineDiff := math.Abs(word.Baseline - baseline)
			threshold := 0.6 * xHeight
			if threshold == 0 {
				threshold = 5.0
			}
			baselineClose := baselineDiff < threshold

			avgFontSize := (getWordsAvgFontSize(currentLine) + word.FontSize) / 2
			relax := 0.1 * avgFontSize
			expandedLine := Rect{lineBox.X0, lineBox.Y0 - relax, lineBox.X1, lineBox.Y1 + relax}
			expandedWord := Rect{word.Box.X0, word.Box.Y0 - relax, word.Box.X1, word.Box.Y1 + relax}
			relaxedOverlap := expandedWord.Y0 < expandedLine.Y1 && expandedWord.Y1 > expandedLine.Y0

			if visuallySameLine || baselineClose || relaxedOverlap {
				// Add to current line
				currentLine = append(currentLine, word)
				lineBox.X0 = math.Min(lineBox.X0, word.Box.X0)
				lineBox.Y0 = math.Min(lineBox.Y0, word.Box.Y0)
				lineBox.X1 = math.Max(lineBox.X1, word.Box.X1)
				lineBox.Y1 = math.Max(lineBox.Y1, word.Box.Y1)
				// Update baseline to weighted average
				baseline = (baseline*float64(len(currentLine)-1) + word.Baseline) / float64(len(currentLine))
			} else {
				// End current line, start new one
				lines = append(lines, Line{
					Words:    currentLine,
					Box:      lineBox,
					Baseline: baseline,
				})
				currentLine = []EnrichedWord{word}
				lineBox = word.Box
				baseline = word.Baseline
				xHeight = word.XHeight
			}
		}

		// End of text
		if i == len(words)-1 && len(currentLine) > 0 {
			lines = append(lines, Line{
				Words:    currentLine,
				Box:      lineBox,
				Baseline: baseline,
			})
		}
	}

	return lines
}

// groupLinesIntoParagraphsAdaptive groups lines into paragraphs using adaptive spacing.
// stats may be nil when document-wide statistics are not available.
func groupLinesIntoParagraphsAdaptive(lines []Line, pageWidth float64, stats *DocumentStats) []Paragraph {
	if len(lines) == 0 {
		return nil
	}

	// Calculate dynamic threshold based on line spacing distribution
	threshold := calculateDynamicThreshold(lines, stats)

	var paragraphs []Paragraph
	var currentPara []Line
	var paraBox Rect
	var prevLineBottom float64

	// Find the minimum X position (left margin) for indentation detection
	var minX float64
	for i, line := range lines {
		if i == 0 || line.Box.X0 < minX {
			minX = line.Box.X0
		}
	}

	for i, line := range lines {
		if len(currentPara) == 0 {
			// Start new paragraph
			currentPara = []Line{line}
			paraBox = line.Box
			prevLineBottom = line.Box.Y1
		} else {
			// Check if line belongs to current paragraph
			lineGap := line.Box.Y0 - prevLineBottom
			avgFontSize := getAverageFontSize(currentPara)
			currentLineFontSize := getLineFontSize(line)

			// Check for significant font size change
			fontSizeRatio := currentLineFontSize / avgFontSize
			significantFontChange := fontSizeRatio < 0.8 || fontSizeRatio > 1.2

			// Use adaptive threshold, with extra tolerance for indented items
			// (lists, code blocks, nested content). Inspired by pdf2md's
			// bigDistance() which allows mostUsedDistance * 1.5 for indented lines.
			effectiveThreshold := threshold
			lastLine := currentPara[len(currentPara)-1]
			bothIndented := lastLine.Box.X0 > minX+5 && line.Box.X0 > minX+5
			if bothIndented {
				effectiveThreshold = threshold * 1.5
			}

			normalizedGap := lineGap / avgFontSize

			if normalizedGap > effectiveThreshold || significantFontChange {
				// End current paragraph, start new one
				paragraphs = append(paragraphs, Paragraph{
					Lines:     currentPara,
					Box:       paraBox,
					Alignment: detectAlignment(currentPara, pageWidth),
					Indent:    currentPara[0].Box.X0,
				})
				currentPara = []Line{line}
				paraBox = line.Box
			} else {
				// Add to current paragraph
				currentPara = append(currentPara, line)
				paraBox.Y1 = line.Box.Y1
				paraBox.X0 = math.Min(paraBox.X0, line.Box.X0)
				paraBox.X1 = math.Max(paraBox.X1, line.Box.X1)
			}
			prevLineBottom = line.Box.Y1
		}

		// End of text
		if i == len(lines)-1 && len(currentPara) > 0 {
			paragraphs = append(paragraphs, Paragraph{
				Lines:     currentPara,
				Box:       paraBox,
				Alignment: detectAlignment(currentPara, pageWidth),
				Indent:    currentPara[0].Box.X0,
			})
		}
	}

	return paragraphs
}

// calculateDynamicThreshold calculates adaptive paragraph spacing threshold.
// stats may be nil when document-wide statistics are not available.
func calculateDynamicThreshold(lines []Line, stats *DocumentStats) float64 {
	if len(lines) < 3 {
		return 0.9 // Fallback to default
	}

	// Calculate all line gaps and font sizes
	var gaps []float64
	var fontSizes []float64

	for i := 0; i < len(lines)-1; i++ {
		gap := lines[i+1].Box.Y0 - lines[i].Box.Y1
		gaps = append(gaps, gap)
		fontSizes = append(fontSizes, getLineFontSize(lines[i]))
	}

	if len(gaps) == 0 {
		return 0.9
	}

	// Calculate median gap and standard deviation
	medianGap := calculateMedian(gaps)
	stdDev := calculateStdDev(gaps)
	medianFontSize := calculateMedian(fontSizes)

	// Paragraph break threshold: median + 1.5 * stdDev, normalized by font size
	if medianFontSize == 0 {
		medianFontSize = 12.0
	}

	threshold := (medianGap + 1.5*stdDev) / medianFontSize

	// Use document-wide MostUsedLineGap as a hint when available.
	// If the per-page median gap is within 30% of the document-wide value,
	// blend them (70% page-local, 30% document-wide) for stability.
	if stats != nil && stats.MostUsedLineGap > 0 && medianGap > 0 {
		ratio := medianGap / stats.MostUsedLineGap
		if ratio >= 0.7 && ratio <= 1.3 {
			blendedGap := medianGap*0.7 + stats.MostUsedLineGap*0.3
			blendedThreshold := (blendedGap + 1.5*stdDev) / medianFontSize
			threshold = blendedThreshold
		}
	}

	// Clamp to reasonable bounds (0.6x to 1.5x font size)
	return clamp(threshold, 0.6, 1.5)
}

// groupLinesIntoParagraphs groups lines into paragraphs based on spacing and alignment.
func groupLinesIntoParagraphs(lines []Line, pageWidth float64) []Paragraph {
	if len(lines) == 0 {
		return nil
	}

	var paragraphs []Paragraph
	var currentPara []Line
	var paraBox Rect
	var prevLineBottom float64

	for i, line := range lines {
		if len(currentPara) == 0 {
			// Start new paragraph
			currentPara = []Line{line}
			paraBox = line.Box
			prevLineBottom = line.Box.Y1
		} else {
			// Check if line belongs to current paragraph
			lineGap := line.Box.Y0 - prevLineBottom
			avgFontSize := getAverageFontSize(currentPara)
			currentLineFontSize := getLineFontSize(line)

			// Check for significant font size change
			// A decrease of more than 20% suggests a new paragraph (e.g., title followed by metadata)
			fontSizeRatio := currentLineFontSize / avgFontSize
			significantFontChange := fontSizeRatio < 0.8 || fontSizeRatio > 1.2

			// Large gap indicates new paragraph
			// Typical line spacing is ~0.3-0.4x font size, paragraph breaks are ~1.2x+
			// Use 0.9x as threshold to catch paragraph breaks while avoiding false positives
			// Also start new paragraph if there's a significant font size change
			if lineGap > avgFontSize*0.9 || significantFontChange {
				// End current paragraph, start new one
				paragraphs = append(paragraphs, Paragraph{
					Lines:     currentPara,
					Box:       paraBox,
					Alignment: detectAlignment(currentPara, pageWidth),
					Indent:    currentPara[0].Box.X0,
				})
				currentPara = []Line{line}
				paraBox = line.Box
			} else {
				// Add to current paragraph
				currentPara = append(currentPara, line)
				paraBox.Y1 = line.Box.Y1
				paraBox.X0 = math.Min(paraBox.X0, line.Box.X0)
				paraBox.X1 = math.Max(paraBox.X1, line.Box.X1)
			}
			prevLineBottom = line.Box.Y1
		}

		// End of text
		if i == len(lines)-1 && len(currentPara) > 0 {
			paragraphs = append(paragraphs, Paragraph{
				Lines:     currentPara,
				Box:       paraBox,
				Alignment: detectAlignment(currentPara, pageWidth),
				Indent:    currentPara[0].Box.X0,
			})
		}
	}

	return paragraphs
}

// detectAlignment detects the alignment of lines in a paragraph.
func detectAlignment(lines []Line, pageWidth float64) Alignment {
	if len(lines) == 0 {
		return AlignmentLeft
	}

	// Check if all lines start at similar X positions (left aligned)
	var startPositions []float64
	for _, line := range lines {
		startPositions = append(startPositions, line.Box.X0)
	}

	// Check if centered (lines centered around page center)
	pageCenter := pageWidth / 2
	var centerOffsets []float64
	for _, line := range lines {
		lineCenter := (line.Box.X0 + line.Box.X1) / 2
		centerOffsets = append(centerOffsets, math.Abs(lineCenter-pageCenter))
	}

	avgCenterOffset := average(centerOffsets)
	if avgCenterOffset < 20 { // Within 20 points of center
		return AlignmentCenter
	}

	// Check if right aligned
	var endPositions []float64
	for _, line := range lines {
		endPositions = append(endPositions, line.Box.X1)
	}

	endStdDev := stdDev(endPositions)
	startStdDev := stdDev(startPositions)

	if endStdDev < 5 && endStdDev < startStdDev {
		return AlignmentRight
	}

	return AlignmentLeft
}

func getWordsAvgFontSize(words []EnrichedWord) float64 {
	if len(words) == 0 {
		return 12
	}
	var total float64
	for _, w := range words {
		total += w.FontSize
	}
	return total / float64(len(words))
}

func kMeansClusters1D(values []float64, k int, maxIter int, convergence float64) []float64 {
	if len(values) == 0 {
		return nil
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	unique := []float64{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if sorted[i] != sorted[len(unique)-1] {
			unique = append(unique, sorted[i])
		}
	}

	if k > len(unique) {
		k = len(unique)
	}
	if k == 0 {
		return nil
	}

	centroids := make([]float64, k)
	for i := 0; i < k; i++ {
		idx := i * (len(unique) - 1) / max(k-1, 1)
		centroids[i] = unique[idx]
	}

	assignments := make([]int, len(values))

	for iter := 0; iter < maxIter; iter++ {
		for vi, v := range values {
			bestCluster := 0
			bestDist := math.Abs(v - centroids[0])
			for ci := 1; ci < k; ci++ {
				d := math.Abs(v - centroids[ci])
				if d < bestDist {
					bestDist = d
					bestCluster = ci
				}
			}
			assignments[vi] = bestCluster
		}

		newCentroids := make([]float64, k)
		counts := make([]int, k)
		for vi, v := range values {
			c := assignments[vi]
			newCentroids[c] += v
			counts[c]++
		}

		maxShift := 0.0
		for ci := 0; ci < k; ci++ {
			if counts[ci] > 0 {
				newCentroids[ci] /= float64(counts[ci])
			} else {
				newCentroids[ci] = centroids[ci]
			}
			shift := math.Abs(newCentroids[ci] - centroids[ci])
			if shift > maxShift {
				maxShift = shift
			}
		}
		centroids = newCentroids

		if maxShift < convergence {
			break
		}
	}

	sort.Float64s(centroids)
	return centroids
}

func detectHeadings(paragraphs []Paragraph, config Config, stats *DocumentStats) {
	if len(paragraphs) == 0 || config.MinHeadingFontSize == 0 {
		return
	}

	var allFontSizes []float64
	for _, para := range paragraphs {
		for _, line := range para.Lines {
			for _, word := range line.Words {
				allFontSizes = append(allFontSizes, word.FontSize)
			}
		}
	}
	if len(allFontSizes) == 0 {
		return
	}

	sort.Float64s(allFontSizes)
	pageMedianFontSize := allFontSizes[len(allFontSizes)/2]

	// Use document-wide MostUsedFontSize as a hint when available.
	// If the per-page median is within 20% of the document-wide value,
	// prefer the document-wide value for consistency across pages.
	// Otherwise fall back to per-page median (the page may have unusual content).
	bodyFontSize := pageMedianFontSize
	if stats != nil && stats.MostUsedFontSize > 0 {
		ratio := pageMedianFontSize / stats.MostUsedFontSize
		if ratio >= 0.8 && ratio <= 1.2 {
			bodyFontSize = stats.MostUsedFontSize
		}
	}

	// Find most common font name (body text font)
	fontNameCounts := make(map[string]int)
	for _, para := range paragraphs {
		for _, line := range para.Lines {
			for _, word := range line.Words {
				if word.FontName != "" {
					fontNameCounts[word.FontName]++
				}
			}
		}
	}
	var bodyFontName string
	var maxFontNameCount int
	for name, count := range fontNameCounts {
		if count > maxFontNameCount {
			maxFontNameCount = count
			bodyFontName = name
		}
	}

	var samples []float64
	for _, para := range paragraphs {
		if len(para.Lines) == 0 || len(para.Lines[0].Words) == 0 {
			continue
		}
		var maxFS float64
		for _, word := range para.Lines[0].Words {
			if word.FontSize > maxFS {
				maxFS = word.FontSize
			}
		}
		samples = append(samples, maxFS)
	}
	if len(samples) == 0 {
		return
	}

	centroids := kMeansClusters1D(samples, 6, 100, 0.01)

	clusterMembers := make(map[int]int)
	for _, v := range samples {
		bestCluster := 0
		bestDist := math.Abs(v - centroids[0])
		for ci := 1; ci < len(centroids); ci++ {
			d := math.Abs(v - centroids[ci])
			if d < bestDist {
				bestDist = d
				bestCluster = ci
			}
		}
		clusterMembers[bestCluster]++
	}

	bodyClusterIdx := 0
	bodyClusterCount := 0
	for ci, cnt := range clusterMembers {
		if cnt > bodyClusterCount {
			bodyClusterCount = cnt
			bodyClusterIdx = ci
		}
	}
	bodyClusterCentroid := centroids[bodyClusterIdx]

	var headingCentroids []float64
	for ci := len(centroids) - 1; ci >= 0; ci-- {
		if centroids[ci] > bodyClusterCentroid && centroids[ci] >= bodyFontSize*config.MinHeadingFontSize {
			headingCentroids = append(headingCentroids, centroids[ci])
		}
	}

	centroidToLevel := make(map[int]int)
	for hi, hc := range headingCentroids {
		for ci, c := range centroids {
			if c == hc {
				centroidToLevel[ci] = hi + 1
				break
			}
		}
	}

	assignCluster := func(fontSize float64) int {
		bestCluster := 0
		bestDist := math.Abs(fontSize - centroids[0])
		for ci := 1; ci < len(centroids); ci++ {
			d := math.Abs(fontSize - centroids[ci])
			if d < bestDist {
				bestDist = d
				bestCluster = ci
			}
		}
		return bestCluster
	}

	for i := range paragraphs {
		para := &paragraphs[i]

		if len(para.Lines) == 0 || len(para.Lines[0].Words) == 0 {
			continue
		}

		if len(para.Lines) > 1 {
			var firstLineMaxSize float64
			for _, word := range para.Lines[0].Words {
				if word.FontSize > firstLineMaxSize {
					firstLineMaxSize = word.FontSize
				}
			}

			var totalSize float64
			var wordCount int
			for li := 1; li < len(para.Lines); li++ {
				for _, word := range para.Lines[li].Words {
					totalSize += word.FontSize
					wordCount++
				}
			}

			if wordCount > 0 {
				avgRestSize := totalSize / float64(wordCount)
				if firstLineMaxSize >= avgRestSize*1.15 && firstLineMaxSize >= bodyFontSize*config.MinHeadingFontSize {
					cluster := assignCluster(firstLineMaxSize)
					if level, ok := centroidToLevel[cluster]; ok {
						para.IsHeading = true
						para.HeadingLevel = level
					}
				}
			}
			continue
		}

		line := para.Lines[0]

		var maxFontSize float64
		for _, word := range line.Words {
			if word.FontSize > maxFontSize {
				maxFontSize = word.FontSize
			}
		}

		cluster := assignCluster(maxFontSize)
		if level, ok := centroidToLevel[cluster]; ok {
			para.IsHeading = true
			para.HeadingLevel = level
		} else {
			isBold := false
			for _, word := range line.Words {
				if word.IsBold {
					isBold = true
					break
				}
			}

			if isBold && maxFontSize >= bodyFontSize*1.05 && maxFontSize >= bodyFontSize*config.MinHeadingFontSize {
				para.IsHeading = true
				para.HeadingLevel = 6
			}
		}

		// Fallback: Uppercase text with different font (pdf2md strategy 4)
		// Some documents use ALL CAPS with a different font family as headings
		if !para.IsHeading && len(line.Words) > 0 {
			// Check if the paragraph is short (typical heading length)
			totalWords := 0
			for _, l := range para.Lines {
				totalWords += len(l.Words)
			}

			if totalWords <= 12 { // Headings are typically short
				allUpper := true
				differentFont := false
				hasLetters := false

				for _, word := range line.Words {
					for _, r := range word.Text {
						if unicode.IsLetter(r) {
							hasLetters = true
							if !unicode.IsUpper(r) {
								allUpper = false
								break
							}
						}
					}
					if !allUpper {
						break
					}
					// Check if font differs from body text
					if word.FontName != "" && bodyFontName != "" && word.FontName != bodyFontName {
						differentFont = true
					}
				}

				if allUpper && hasLetters && differentFont && maxFontSize >= bodyFontSize*0.95 {
					para.IsHeading = true
					para.HeadingLevel = 6 // Lowest level, will be renormalized
				}
			}
		}
	}
}

// detectLists identifies paragraphs that are list items.
func detectLists(paragraphs []Paragraph) {
	for i := range paragraphs {
		para := &paragraphs[i]

		// Check first word of first line
		if len(para.Lines) == 0 || len(para.Lines[0].Words) == 0 {
			continue
		}

		firstWord := para.Lines[0].Words[0]
		if firstWord.IsBulletOrNumber() {
			para.IsList = true
		}
	}
}

// detectCodeBlocks identifies paragraphs that are code blocks.
func detectCodeBlocks(paragraphs []Paragraph) {
	for i := range paragraphs {
		para := &paragraphs[i]

		// Check if most words are monospace
		var monoCount int
		var totalWords int
		for _, line := range para.Lines {
			for _, word := range line.Words {
				totalWords++
				if word.IsMonospace {
					monoCount++
				}
			}
		}

		if totalWords > 0 && float64(monoCount)/float64(totalWords) > 0.8 {
			para.IsCode = true
		}
	}
}

// getAverageFontSize calculates the average font size in a set of lines.
func getAverageFontSize(lines []Line) float64 {
	var total float64
	var count int
	for _, line := range lines {
		for _, word := range line.Words {
			total += word.FontSize
			count++
		}
	}
	if count == 0 {
		return 12 // Default
	}
	return total / float64(count)
}

// getLineFontSize calculates the average font size for a single line.
func getLineFontSize(line Line) float64 {
	var total float64
	var count int
	for _, word := range line.Words {
		total += word.FontSize
		count++
	}
	if count == 0 {
		return 12 // Default
	}
	return total / float64(count)
}

// average calculates the average of a slice of floats.
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stdDev calculates the standard deviation of a slice of floats.
func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	avg := average(values)
	var sumSquares float64
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	return math.Sqrt(sumSquares / float64(len(values)))
}

func getLineAvgFontSize(words []EnrichedWord) float64 {
	if len(words) == 0 {
		return 12.0
	}
	var total float64
	for _, w := range words {
		total += w.FontSize
	}
	return total / float64(len(words))
}

func mergeCloseWords(words []EnrichedWord) []EnrichedWord {
	if len(words) <= 1 {
		return words
	}

	var merged []EnrichedWord
	var currentMerge []EnrichedWord

	for i, word := range words {
		if len(currentMerge) == 0 {
			currentMerge = []EnrichedWord{word}
			continue
		}

		prevWord := currentMerge[len(currentMerge)-1]
		gap := word.Box.X0 - prevWord.Box.X1

		isPunct := false
		if len(word.Text) == 1 {
			r := []rune(word.Text)[0]
			isPunct = r == '.' || r == ',' || r == ';' || r == ':' ||
				r == '!' || r == '?' || r == '-' || r == '(' || r == ')' ||
				r == '[' || r == ']' || r == '{' || r == '}'
		}

		avgFontSize := (prevWord.FontSize + word.FontSize) / 2
		gapThreshold := 0.15 * avgFontSize

		overlapY0 := math.Max(prevWord.Box.Y0, word.Box.Y0)
		overlapY1 := math.Min(prevWord.Box.Y1, word.Box.Y1)
		overlapHeight := overlapY1 - overlapY0
		minHeight := math.Min(prevWord.Box.Height(), word.Box.Height())
		verticalOverlap := false
		if minHeight > 0 {
			verticalOverlap = overlapHeight/minHeight > 0.3
		}

		if gap < gapThreshold && !isPunct && verticalOverlap {
			currentMerge = append(currentMerge, word)
		} else {
			if len(currentMerge) > 1 {
				merged = append(merged, mergeWordGroup(currentMerge))
			} else {
				merged = append(merged, currentMerge[0])
			}
			currentMerge = []EnrichedWord{word}
		}

		if i == len(words)-1 {
			if len(currentMerge) > 1 {
				merged = append(merged, mergeWordGroup(currentMerge))
			} else {
				merged = append(merged, currentMerge[0])
			}
		}
	}

	return merged
}

// mergeWordGroup combines multiple words into a single word.
func mergeWordGroup(words []EnrichedWord) EnrichedWord {
	if len(words) == 0 {
		return EnrichedWord{}
	}
	if len(words) == 1 {
		return words[0]
	}

	// Concatenate text, inserting a space at case boundaries to avoid
	// fusing words like "by" + "Netwealth" → "byNetwealth".
	var text string
	for _, word := range words {
		if len(text) > 0 && len(word.Text) > 0 {
			lastRune := rune(text[len(text)-1])
			firstRune := []rune(word.Text)[0]
			if isLower(lastRune) && isUpper(firstRune) {
				text += " "
			}
		}
		text += word.Text
	}

	// Calculate merged bounding box
	box := words[0].Box
	for i := 1; i < len(words); i++ {
		box.X0 = math.Min(box.X0, words[i].Box.X0)
		box.Y0 = math.Min(box.Y0, words[i].Box.Y0)
		box.X1 = math.Max(box.X1, words[i].Box.X1)
		box.Y1 = math.Max(box.Y1, words[i].Box.Y1)
	}

	// Use first word's properties (should be similar for close words)
	return EnrichedWord{
		Text:        text,
		Box:         box,
		FontSize:    words[0].FontSize,
		FontWeight:  words[0].FontWeight,
		FontName:    words[0].FontName,
		FontFlags:   words[0].FontFlags,
		FillColor:   words[0].FillColor,
		IsBold:      words[0].IsBold,
		IsItalic:    words[0].IsItalic,
		IsMonospace: words[0].IsMonospace,
	}
}
