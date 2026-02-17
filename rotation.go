package docmill

import (
	"math"
	"sort"
)

// detectTextRotation analyzes words and groups them by rotation angle
func detectTextRotation(words []EnrichedWord) []TextBlock {
	if len(words) == 0 {
		return nil
	}

	// Build angle histogram (quantize to 15-degree buckets)
	const angleBucket = 15.0
	angleHistogram := make(map[float64][]EnrichedWord)

	for _, word := range words {
		// Normalize and quantize angle
		normalizedAngle := normalizeAngle(word.Rotation)
		quantized := quantizeAngle(normalizedAngle, angleBucket)
		angleHistogram[quantized] = append(angleHistogram[quantized], word)
	}

	// Find dominant angles (angles with significant word counts)
	type angleCount struct {
		angle float64
		count int
		words []EnrichedWord
	}

	var angles []angleCount
	for angle, wordsAtAngle := range angleHistogram {
		angles = append(angles, angleCount{
			angle: angle,
			count: len(wordsAtAngle),
			words: wordsAtAngle,
		})
	}

	// Sort by count descending
	sort.Slice(angles, func(i, j int) bool {
		return angles[i].count > angles[j].count
	})

	// Create text blocks for each significant angle
	// (angles with at least 5% of total words)
	totalWords := len(words)
	threshold := int(math.Max(5, float64(totalWords)*0.05))

	var blocks []TextBlock
	for _, ac := range angles {
		if ac.count < threshold {
			break
		}

		// Group words at this angle into lines
		lines := groupWordsIntoLinesWithRotation(ac.words, ac.angle)

		block := TextBlock{
			Words:            ac.words,
			Lines:            lines,
			Rotation:         ac.angle,
			ReadingDirection: inferReadingDirection(ac.angle),
		}

		blocks = append(blocks, block)
	}

	return blocks
}

// groupWordsIntoLinesWithRotation groups words into lines accounting for rotation
func groupWordsIntoLinesWithRotation(words []EnrichedWord, rotation float64) []Line {
	if len(words) == 0 {
		return nil
	}

	// For vertical text (90° or 270°), we need different grouping logic
	isVertical := (rotation >= 45 && rotation < 135) || (rotation >= 225 && rotation < 315)

	if isVertical {
		return groupWordsIntoVerticalLines(words, rotation)
	}

	// For horizontal text (0° or 180°), use baseline-aware grouping
	return groupWordsIntoHorizontalLines(words)
}

// groupWordsIntoVerticalLines groups words into vertical lines
func groupWordsIntoVerticalLines(words []EnrichedWord, rotation float64) []Line {
	if len(words) == 0 {
		return nil
	}

	// Sort words by X position (vertical columns)
	sortedWords := make([]EnrichedWord, len(words))
	copy(sortedWords, words)

	sort.Slice(sortedWords, func(i, j int) bool {
		xDiff := math.Abs(sortedWords[i].Box.CenterX() - sortedWords[j].Box.CenterX())
		if xDiff < 3 { // Same column threshold
			// Sort by Y within column
			return sortedWords[i].Box.Y0 < sortedWords[j].Box.Y0
		}
		return sortedWords[i].Box.CenterX() < sortedWords[j].Box.CenterX()
	})

	// Group into vertical lines (columns)
	var lines []Line
	var currentLine []EnrichedWord
	var lineBox Rect
	var centerX float64

	for i, word := range sortedWords {
		wordCenterX := word.Box.CenterX()

		if len(currentLine) == 0 {
			currentLine = []EnrichedWord{word}
			lineBox = word.Box
			centerX = wordCenterX
		} else {
			// Check if word belongs to current vertical line
			xDiff := math.Abs(wordCenterX - centerX)
			if xDiff < word.FontSize*0.8 { // Same column threshold
				currentLine = append(currentLine, word)
				lineBox = mergeRects(lineBox, word.Box)
			} else {
				// End current line, start new one
				lines = append(lines, Line{
					Words:    currentLine,
					Box:      lineBox,
					Baseline: centerX, // For vertical text, "baseline" is the X position
				})
				currentLine = []EnrichedWord{word}
				lineBox = word.Box
				centerX = wordCenterX
			}
		}

		// End of words
		if i == len(sortedWords)-1 && len(currentLine) > 0 {
			lines = append(lines, Line{
				Words:    currentLine,
				Box:      lineBox,
				Baseline: centerX,
			})
		}
	}

	return lines
}

// groupWordsIntoHorizontalLines groups words into horizontal lines using baseline
func groupWordsIntoHorizontalLines(words []EnrichedWord) []Line {
	if len(words) == 0 {
		return nil
	}

	// Sort words by VISUAL POSITION (Y-overlap, then X)
	// Same logic as structure.go for consistency
	sortedWords := make([]EnrichedWord, len(words))
	copy(sortedWords, words)

	sort.Slice(sortedWords, func(i, j int) bool {
		wordI := sortedWords[i]
		wordJ := sortedWords[j]

		// Check Y-coordinate overlap
		overlapY0 := math.Max(wordI.Box.Y0, wordJ.Box.Y0)
		overlapY1 := math.Min(wordI.Box.Y1, wordJ.Box.Y1)
		overlapHeight := overlapY1 - overlapY0
		minHeight := math.Min(wordI.Box.Height(), wordJ.Box.Height())

		// Same visual line - sort by X position
		if overlapHeight > minHeight*0.3 {
			return wordI.Box.X0 < wordJ.Box.X0
		}

		// Different lines - sort by Y position
		return wordI.Box.Y0 < wordJ.Box.Y0
	})

	// Group into lines using VISUAL CENTER-BASED approach
	var lines []Line
	var currentLine []EnrichedWord
	var lineBox Rect
	var baseline float64
	var xHeight float64

	for i, word := range sortedWords {
		if len(currentLine) == 0 {
			currentLine = []EnrichedWord{word}
			lineBox = word.Box
			baseline = word.Baseline
			xHeight = word.XHeight
		} else {
			// Use VISUAL CENTER-BASED grouping (same as structure.go)
			lineCenterY := (lineBox.Y0 + lineBox.Y1) / 2
			wordCenterY := (word.Box.Y0 + word.Box.Y1) / 2
			centerDistance := math.Abs(wordCenterY - lineCenterY)
			avgHeight := (lineBox.Height() + word.Box.Height()) / 2

			visuallySameLine := centerDistance < avgHeight*1.0

			// Baseline check as fallback
			baselineDiff := math.Abs(word.Baseline - baseline)
			threshold := 0.6 * xHeight
			if threshold == 0 {
				threshold = 5.0
			}
			baselineClose := baselineDiff < threshold

			avgFontSize := (getLineAvgFontSize(currentLine) + word.FontSize) / 2
			relax := 0.1 * avgFontSize
			expandedLine := Rect{lineBox.X0, lineBox.Y0 - relax, lineBox.X1, lineBox.Y1 + relax}
			expandedWord := Rect{word.Box.X0, word.Box.Y0 - relax, word.Box.X1, word.Box.Y1 + relax}
			relaxedOverlap := expandedWord.Y0 < expandedLine.Y1 && expandedWord.Y1 > expandedLine.Y0

			if visuallySameLine || baselineClose || relaxedOverlap {
				currentLine = append(currentLine, word)
				lineBox.X0 = math.Min(lineBox.X0, word.Box.X0)
				lineBox.Y0 = math.Min(lineBox.Y0, word.Box.Y0)
				lineBox.X1 = math.Max(lineBox.X1, word.Box.X1)
				lineBox.Y1 = math.Max(lineBox.Y1, word.Box.Y1)
				baseline = (baseline*float64(len(currentLine)-1) + word.Baseline) / float64(len(currentLine))
			} else {
				// New line
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

		// End of words
		if i == len(sortedWords)-1 && len(currentLine) > 0 {
			lines = append(lines, Line{
				Words:    currentLine,
				Box:      lineBox,
				Baseline: baseline,
			})
		}
	}

	return lines
}

// CenterX returns the horizontal center of the rectangle
func (r Rect) CenterX() float64 {
	return (r.X0 + r.X1) / 2
}
