package docmill

import (
	"math"
	"sort"
)

// calculateDocumentStats computes document-wide statistics from all raw page data.
// These stats serve as hints for per-page structure detection.
func calculateDocumentStats(pages []*rawPageData) DocumentStats {
	fontSizeFreq := make(map[float64]int)
	fontNameFreq := make(map[string]int)
	var maxFontSize float64

	// Build frequency maps weighted by word count
	for _, page := range pages {
		for _, word := range page.words {
			// Round font size to 1 decimal place to group similar sizes
			rounded := math.Round(word.FontSize*10) / 10
			fontSizeFreq[rounded]++
			fontNameFreq[word.FontName]++
			if word.FontSize > maxFontSize {
				maxFontSize = word.FontSize
			}
		}
	}

	// Find most common font size
	var mostUsedFontSize float64
	var maxFontSizeCount int
	for size, count := range fontSizeFreq {
		if count > maxFontSizeCount {
			maxFontSizeCount = count
			mostUsedFontSize = size
		}
	}

	// Find most common font name
	var mostUsedFontName string
	var maxFontNameCount int
	for name, count := range fontNameFreq {
		if count > maxFontNameCount {
			maxFontNameCount = count
			mostUsedFontName = name
		}
	}

	// Calculate most common line gap across all pages
	mostUsedLineGap := calculateDocumentLineGap(pages)

	return DocumentStats{
		MostUsedFontSize: mostUsedFontSize,
		MostUsedFontName: mostUsedFontName,
		MostUsedLineGap:  mostUsedLineGap,
		FontSizeFreq:     fontSizeFreq,
		FontNameFreq:     fontNameFreq,
		MaxFontSize:      maxFontSize,
	}
}

// calculateDocumentLineGap computes the most common line gap across all pages.
// It groups words into lines per page using the same logic as the per-page pipeline,
// then builds a frequency distribution of line gaps.
func calculateDocumentLineGap(pages []*rawPageData) float64 {
	var allGaps []float64

	for _, page := range pages {
		if len(page.words) == 0 {
			continue
		}

		// Sort words by position (same logic as buildParagraphs)
		sortedWords := make([]EnrichedWord, len(page.words))
		copy(sortedWords, page.words)
		sort.Slice(sortedWords, func(i, j int) bool {
			wordI := sortedWords[i]
			wordJ := sortedWords[j]

			overlapY0 := math.Max(wordI.Box.Y0, wordJ.Box.Y0)
			overlapY1 := math.Min(wordI.Box.Y1, wordJ.Box.Y1)
			overlapHeight := overlapY1 - overlapY0
			minHeight := math.Min(wordI.Box.Height(), wordJ.Box.Height())

			if minHeight > 0 && overlapHeight > minHeight*0.3 {
				return wordI.Box.X0 < wordJ.Box.X0
			}
			return wordI.Box.Y0 < wordJ.Box.Y0
		})

		lines := groupWordsIntoLinesBaseline(sortedWords)

		for i := 0; i < len(lines)-1; i++ {
			gap := lines[i+1].Box.Y0 - lines[i].Box.Y1
			if gap > 0 {
				allGaps = append(allGaps, gap)
			}
		}
	}

	if len(allGaps) == 0 {
		return 0
	}

	// Find the most common gap by bucketing (round to 0.5 increments)
	gapFreq := make(map[float64]int)
	for _, gap := range allGaps {
		bucketed := math.Round(gap*2) / 2
		gapFreq[bucketed]++
	}

	var mostUsedGap float64
	var maxCount int
	for gap, count := range gapFreq {
		if count > maxCount {
			maxCount = count
			mostUsedGap = gap
		}
	}

	return mostUsedGap
}
