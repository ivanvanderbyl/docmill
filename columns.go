package docmill

import (
	"math"
	"sort"
)

// detectColumns detects multi-column layout using vertical projection profile
func detectColumns(words []EnrichedWord, pageWidth float64) []Column {
	if len(words) == 0 {
		return nil
	}

	// Build vertical projection profile (histogram of text density)
	binWidth := 1.0 // 1 point resolution
	numBins := int(math.Ceil(pageWidth / binWidth))
	bins := make([]int, numBins)

	// Count words in each vertical bin
	for _, word := range words {
		startBin := int(word.Box.X0 / binWidth)
		endBin := int(math.Ceil(word.Box.X1 / binWidth))

		for bin := startBin; bin < endBin && bin < numBins; bin++ {
			if bin >= 0 {
				bins[bin]++
			}
		}
	}

	// Find valleys (gaps between columns)
	valleys := findSignificantValleys(bins, pageWidth)

	if len(valleys) == 0 {
		// Single column layout
		return []Column{
			{
				Box: Rect{
					X0: 0,
					Y0: 0,
					X1: pageWidth,
					Y1: findMaxY(words),
				},
				Words: words,
				Index: 0,
			},
		}
	}

	// Split words into columns based on valleys
	columns := make([]Column, 0, len(valleys)+1)
	colStart := 0.0

	for i, valley := range valleys {
		colEnd := valley
		colWords := filterWordsByXRange(words, colStart, colEnd)

		if len(colWords) > 0 {
			columns = append(columns, Column{
				Box: Rect{
					X0: colStart,
					Y0: 0,
					X1: colEnd,
					Y1: findMaxY(colWords),
				},
				Words: colWords,
				Index: i,
			})
		}

		colStart = valley
	}

	// Add final column
	colWords := filterWordsByXRange(words, colStart, pageWidth)
	if len(colWords) > 0 {
		columns = append(columns, Column{
			Box: Rect{
				X0: colStart,
				Y0: 0,
				X1: pageWidth,
				Y1: findMaxY(colWords),
			},
			Words: colWords,
			Index: len(valleys),
		})
	}

	return columns
}

// findSignificantValleys identifies gaps in the text density histogram
func findSignificantValleys(bins []int, pageWidth float64) []float64 {
	if len(bins) == 0 {
		return nil
	}

	// Calculate statistics
	var sum int
	var nonZero int
	for _, count := range bins {
		sum += count
		if count > 0 {
			nonZero++
		}
	}

	if nonZero == 0 {
		return nil
	}

	avgDensity := float64(sum) / float64(nonZero)

	// Find valleys (consecutive bins with density below threshold)
	const minValleyWidth = 20.0 // Minimum 20 points wide
	const valleyThreshold = 0.2  // Valley density < 20% of average

	var valleys []float64
	var valleyStart int = -1
	threshold := int(avgDensity * valleyThreshold)

	for i, count := range bins {
		if count <= threshold {
			if valleyStart == -1 {
				valleyStart = i
			}
		} else {
			if valleyStart != -1 {
				// End of valley
				valleyWidth := float64(i - valleyStart)
				if valleyWidth >= minValleyWidth {
					// Record valley center
					valleyCenter := float64(valleyStart+i) / 2.0
					valleys = append(valleys, valleyCenter)
				}
				valleyStart = -1
			}
		}
	}

	// Filter valleys that are too close to page edges
	const edgeMargin = 50.0 // Ignore valleys within 50 points of edges
	var filteredValleys []float64
	for _, valley := range valleys {
		if valley > edgeMargin && valley < pageWidth-edgeMargin {
			filteredValleys = append(filteredValleys, valley)
		}
	}

	return filteredValleys
}

// filterWordsByXRange returns words whose horizontal center is within the X range
func filterWordsByXRange(words []EnrichedWord, xStart, xEnd float64) []EnrichedWord {
	var filtered []EnrichedWord
	for _, word := range words {
		center := word.Box.CenterX()
		if center >= xStart && center < xEnd {
			filtered = append(filtered, word)
		}
	}
	return filtered
}

// findMaxY finds the maximum Y coordinate among words
func findMaxY(words []EnrichedWord) float64 {
	if len(words) == 0 {
		return 0
	}

	maxY := words[0].Box.Y1
	for _, word := range words[1:] {
		if word.Box.Y1 > maxY {
			maxY = word.Box.Y1
		}
	}
	return maxY
}

// determineReadingOrder sorts paragraphs according to reading order with column awareness
func determineReadingOrder(paragraphs []Paragraph, columns []Column) []Paragraph {
	if len(paragraphs) == 0 {
		return paragraphs
	}

	if len(columns) <= 1 {
		// Single column: simple top-to-bottom
		sorted := make([]Paragraph, len(paragraphs))
		copy(sorted, paragraphs)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Box.Y0 < sorted[j].Box.Y0
		})
		return sorted
	}

	// Multi-column: read top-to-bottom within each column, left-to-right across columns
	var ordered []Paragraph

	// Sort columns by X position
	sortedCols := make([]Column, len(columns))
	copy(sortedCols, columns)
	sort.Slice(sortedCols, func(i, j int) bool {
		return sortedCols[i].Box.X0 < sortedCols[j].Box.X0
	})

	// Process each column
	for _, col := range sortedCols {
		// Find paragraphs in this column
		var colParas []Paragraph
		for _, para := range paragraphs {
			paraCenter := para.Box.CenterX()
			if paraCenter >= col.Box.X0 && paraCenter < col.Box.X1 {
				colParas = append(colParas, para)
			}
		}

		// Sort paragraphs within column by Y position (top to bottom)
		sort.Slice(colParas, func(i, j int) bool {
			return colParas[i].Box.Y0 < colParas[j].Box.Y0
		})

		ordered = append(ordered, colParas...)
	}

	return ordered
}

// CenterX returns the horizontal center of a paragraph's bounding box
func (p Paragraph) CenterX() float64 {
	return (p.Box.X0 + p.Box.X1) / 2
}
