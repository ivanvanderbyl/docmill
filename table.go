package docmill

import (
	"math"
	"sort"
)

// wordsToEdgesHorizontal finds imaginary horizontal lines connecting word tops/bottoms.
// Based on pdfplumber's words_to_edges_h function.
func wordsToEdgesHorizontal(words []EnrichedWord, minWords int) []Edge {
	if len(words) == 0 {
		return nil
	}

	// Group words by their top position (within 1 pixel tolerance)
	type cluster struct {
		top   float64
		words []EnrichedWord
	}

	var clusters []cluster
	for _, word := range words {
		found := false
		for i := range clusters {
			if math.Abs(clusters[i].top-word.Box.Y0) < 1.0 {
				clusters[i].words = append(clusters[i].words, word)
				found = true
				break
			}
		}
		if !found {
			clusters = append(clusters, cluster{top: word.Box.Y0, words: []EnrichedWord{word}})
		}
	}

	// Filter to clusters with enough words
	var largeClusters []cluster
	for _, c := range clusters {
		if len(c.words) >= minWords {
			largeClusters = append(largeClusters, c)
		}
	}

	if len(largeClusters) == 0 {
		return nil
	}

	// Find the min/max x coordinates across all clusters
	minX0 := math.MaxFloat64
	maxX1 := -math.MaxFloat64
	for _, c := range largeClusters {
		for _, w := range c.words {
			if w.Box.X0 < minX0 {
				minX0 = w.Box.X0
			}
			if w.Box.X1 > maxX1 {
				maxX1 = w.Box.X1
			}
		}
	}

	// Create edges for each cluster (top and bottom)
	var edges []Edge
	for _, c := range largeClusters {
		// Find bottom of this cluster
		bottom := c.top
		for _, w := range c.words {
			if w.Box.Y1 > bottom {
				bottom = w.Box.Y1
			}
		}

		// Top edge
		edges = append(edges, Edge{
			X0:          minX0,
			X1:          maxX1,
			Top:         c.top,
			Bottom:      c.top,
			Width:       maxX1 - minX0,
			Orientation: "h",
		})

		// Bottom edge
		edges = append(edges, Edge{
			X0:          minX0,
			X1:          maxX1,
			Top:         bottom,
			Bottom:      bottom,
			Width:       maxX1 - minX0,
			Orientation: "h",
		})
	}

	return edges
}

// wordsToEdgesVertical finds imaginary vertical lines connecting word left/right/center positions.
// Based on pdfplumber's words_to_edges_v function.
func wordsToEdgesVertical(words []EnrichedWord, minWords int) []Edge {
	if len(words) == 0 {
		return nil
	}

	// Group words by x0, x1, and center
	type cluster struct {
		x     float64
		words []EnrichedWord
	}

	groupByPosition := func(getX func(EnrichedWord) float64) []cluster {
		var clusters []cluster
		for _, word := range words {
			x := getX(word)
			found := false
			for i := range clusters {
				if math.Abs(clusters[i].x-x) < 1.0 {
					clusters[i].words = append(clusters[i].words, word)
					found = true
					break
				}
			}
			if !found {
				clusters = append(clusters, cluster{x: x, words: []EnrichedWord{word}})
			}
		}
		return clusters
	}

	// Group by left edge (x0)
	byX0 := groupByPosition(func(w EnrichedWord) float64 { return w.Box.X0 })
	// Group by right edge (x1)
	byX1 := groupByPosition(func(w EnrichedWord) float64 { return w.Box.X1 })
	// Group by center
	byCenter := groupByPosition(func(w EnrichedWord) float64 { return (w.Box.X0 + w.Box.X1) / 2 })

	// Combine all clusters
	allClusters := append(append(byX0, byX1...), byCenter...)

	// Sort by size (largest first) and filter to large clusters
	sort.Slice(allClusters, func(i, j int) bool {
		return len(allClusters[i].words) > len(allClusters[j].words)
	})

	var largeClusters []cluster
	for _, c := range allClusters {
		if len(c.words) >= minWords {
			largeClusters = append(largeClusters, c)
		}
	}

	if len(largeClusters) == 0 {
		return nil
	}

	// Find bounding boxes for each cluster and remove overlaps
	type bbox struct {
		x0, y0, x1, y1 float64
	}

	bboxes := make([]bbox, 0, len(largeClusters))
	for _, c := range largeClusters {
		if len(c.words) == 0 {
			continue
		}
		bb := bbox{
			x0: math.MaxFloat64,
			y0: math.MaxFloat64,
			x1: -math.MaxFloat64,
			y1: -math.MaxFloat64,
		}
		for _, w := range c.words {
			if w.Box.X0 < bb.x0 {
				bb.x0 = w.Box.X0
			}
			if w.Box.Y0 < bb.y0 {
				bb.y0 = w.Box.Y0
			}
			if w.Box.X1 > bb.x1 {
				bb.x1 = w.Box.X1
			}
			if w.Box.Y1 > bb.y1 {
				bb.y1 = w.Box.Y1
			}
		}
		bboxes = append(bboxes, bb)
	}

	// Remove overlapping bboxes
	condensed := []bbox{}
	for _, bb := range bboxes {
		overlap := false
		for _, existing := range condensed {
			// Check if bboxes overlap
			if !(bb.x1 < existing.x0 || bb.x0 > existing.x1 ||
				bb.y1 < existing.y0 || bb.y0 > existing.y1) {
				overlap = true
				break
			}
		}
		if !overlap {
			condensed = append(condensed, bb)
		}
	}

	if len(condensed) == 0 {
		return nil
	}

	// Sort by x0
	sort.Slice(condensed, func(i, j int) bool {
		return condensed[i].x0 < condensed[j].x0
	})

	// Find global min/max y
	minTop := math.MaxFloat64
	maxBottom := -math.MaxFloat64
	maxX1 := -math.MaxFloat64
	for _, bb := range condensed {
		if bb.y0 < minTop {
			minTop = bb.y0
		}
		if bb.y1 > maxBottom {
			maxBottom = bb.y1
		}
		if bb.x1 > maxX1 {
			maxX1 = bb.x1
		}
	}

	// Create vertical edges at each x0 position, plus one at the rightmost x1
	var edges []Edge
	for _, bb := range condensed {
		edges = append(edges, Edge{
			X0:          bb.x0,
			X1:          bb.x0,
			Top:         minTop,
			Bottom:      maxBottom,
			Height:      maxBottom - minTop,
			Orientation: "v",
		})
	}

	// Add rightmost edge
	edges = append(edges, Edge{
		X0:          maxX1,
		X1:          maxX1,
		Top:         minTop,
		Bottom:      maxBottom,
		Height:      maxBottom - minTop,
		Orientation: "v",
	})

	return edges
}

// DetectTables finds tables in a page using word alignment or explicit lines.
// Based on pdfplumber's TableFinder supporting multiple strategies.
func DetectTables(page *Page, settings TableSettings) []Table {
	// Get all words from paragraphs
	var words []EnrichedWord
	for _, para := range page.Paragraphs {
		for _, line := range para.Lines {
			words = append(words, line.Words...)
		}
	}

	// Get edges based on strategy
	var edges []Edge

	// Vertical edges - try "lines" strategy first, fall back to "text" if no lines found
	vLineEdges := 0
	if settings.VerticalStrategy == "lines" || settings.VerticalStrategy == "lines_text" {
		// Use explicit line objects from PDF
		for _, line := range page.Lines {
			if line.Orientation == "v" {
				edges = append(edges, line)
				vLineEdges++
			}
		}
	}

	// If lines strategy found no edges, or strategy is "text" or "lines_text", use text-based detection
	if (vLineEdges == 0 && settings.VerticalStrategy == "lines") ||
		settings.VerticalStrategy == "text" ||
		settings.VerticalStrategy == "lines_text" {
		if len(words) > 0 {
			vEdges := wordsToEdgesVertical(words, settings.MinWordsVertical)
			edges = append(edges, vEdges...)
		}
	}

	// Horizontal edges - try "lines" strategy first, fall back to "text" if no lines found
	hLineEdges := 0
	if settings.HorizontalStrategy == "lines" || settings.HorizontalStrategy == "lines_text" {
		// Use explicit line objects from PDF
		for _, line := range page.Lines {
			if line.Orientation == "h" {
				edges = append(edges, line)
				hLineEdges++
			}
		}
	}

	// If lines strategy found no edges, or strategy is "text" or "lines_text", use text-based detection
	if (hLineEdges == 0 && settings.HorizontalStrategy == "lines") ||
		settings.HorizontalStrategy == "text" ||
		settings.HorizontalStrategy == "lines_text" {
		if len(words) > 0 {
			hEdges := wordsToEdgesHorizontal(words, settings.MinWordsHorizontal)
			edges = append(edges, hEdges...)
		}
	}

	if len(edges) == 0 || len(words) == 0 {
		return nil
	}

	// Merge edges (snap and join)
	edges = mergeEdges(edges, settings)

	// Filter by minimum length
	edges = filterEdgesByLength(edges, settings.EdgeMinLength)

	// Find intersections
	intersections := findIntersections(edges, settings)

	// Extract cells from intersections
	cells := intersectionsToCells(intersections)

	// Group cells into tables
	tableGroups := cellsToTables(cells)

	// Create table structures
	tables := make([]Table, 0, len(tableGroups))
	for _, cellGroup := range tableGroups {
		table := createTable(page, cellGroup, words)
		tables = append(tables, table)
	}

	return tables
}
