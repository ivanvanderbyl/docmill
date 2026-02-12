package pdfmarkdown

import (
	"math"
	"sort"
	"strings"
)

// Segment represents a group of horizontally adjacent content elements
// Based on PDF-TREX algorithm
type Segment struct {
	Words []EnrichedWord
	Box   Rect
}

// LineType represents the classification of a line in table detection
type LineType string

const (
	TextLine    LineType = "TxL"    // Text line (single segment spanning > 50% width)
	TableLine   LineType = "TbL"    // Table line (multiple segments)
	UnknownLine LineType = "UnL"    // Unknown line (single segment spanning < 50% width)
)

// TaggedLine is a line with its type classification
type TaggedLine struct {
	Line     Line
	Segments []Segment
	Type     LineType
}

// AdaptiveThresholds contains document-specific threshold values
type AdaptiveThresholds struct {
	HorizontalThreshold float64 // hT: for horizontal clustering
	VerticalThreshold   float64 // vT: for vertical clustering
}

// calculateAdaptiveThresholds computes thresholds based on document spacing distribution
// Implements PDF-TREX approach: analyze white space and distance distributions
func calculateAdaptiveThresholds(words []EnrichedWord) AdaptiveThresholds {
	if len(words) < 2 {
		// Fallback to default thresholds
		return AdaptiveThresholds{
			HorizontalThreshold: 20.0,
			VerticalThreshold:   5.0,
		}
	}

	// Collect horizontal gaps (between words on same line)
	var horizontalGaps []float64
	sortedWords := make([]EnrichedWord, len(words))
	copy(sortedWords, words)

	// Sort by Y then X for line detection
	sort.Slice(sortedWords, func(i, j int) bool {
		yDiff := math.Abs(sortedWords[i].Box.Y0 - sortedWords[j].Box.Y0)
		if yDiff < 5 {
			return sortedWords[i].Box.X0 < sortedWords[j].Box.X0
		}
		return sortedWords[i].Box.Y0 < sortedWords[j].Box.Y0
	})

	// Calculate horizontal gaps between consecutive words on same line
	for i := 0; i < len(sortedWords)-1; i++ {
		w1, w2 := sortedWords[i], sortedWords[i+1]

		// Check if on same line (horizontal overlap)
		if horizontalOverlapRatio(w1.Box, w2.Box) > 0.3 {
			gap := w2.Box.X0 - w1.Box.X1
			if gap > 0 && gap < 200 { // Filter outliers
				horizontalGaps = append(horizontalGaps, gap)
			}
		}
	}

	// Collect vertical gaps (between lines)
	var verticalGaps []float64
	var currentLineBottom float64
	var currentLineY float64

	for i, word := range sortedWords {
		if i == 0 {
			currentLineY = word.Box.Y0
			currentLineBottom = word.Box.Y1
			continue
		}

		// New line detected
		if math.Abs(word.Box.Y0-currentLineY) > 5 {
			gap := word.Box.Y0 - currentLineBottom
			if gap > 0 && gap < 200 { // Filter outliers
				verticalGaps = append(verticalGaps, gap)
			}
			currentLineY = word.Box.Y0
			currentLineBottom = word.Box.Y1
		} else {
			// Same line, update bottom
			currentLineBottom = math.Max(currentLineBottom, word.Box.Y1)
		}
	}

	// Calculate thresholds using median + 1.5 * stddev
	hT := calculateThresholdFromGaps(horizontalGaps, 20.0)
	vT := calculateThresholdFromGaps(verticalGaps, 5.0)

	return AdaptiveThresholds{
		HorizontalThreshold: hT,
		VerticalThreshold:   vT,
	}
}

// calculateThresholdFromGaps computes threshold using statistical analysis
func calculateThresholdFromGaps(gaps []float64, defaultValue float64) float64 {
	if len(gaps) < 3 {
		return defaultValue
	}

	median := calculateMedian(gaps)
	stdDev := calculateStdDev(gaps)

	// Threshold = median + 1.5 * stddev
	// Clamped to reasonable bounds
	threshold := median + 1.5*stdDev
	threshold = clamp(threshold, 5.0, 100.0)

	return threshold
}

// buildSegmentsFromLine clusters words in a line into segments
// Uses agglomerative hierarchical clustering with horizontal threshold
func buildSegmentsFromLine(line Line, hT float64) []Segment {
	if len(line.Words) == 0 {
		return nil
	}

	if len(line.Words) == 1 {
		return []Segment{
			{
				Words: line.Words,
				Box:   line.Words[0].Box,
			},
		}
	}

	// Start with each word as a cluster
	clusters := make([]Segment, len(line.Words))
	for i, word := range line.Words {
		clusters[i] = Segment{
			Words: []EnrichedWord{word},
			Box:   word.Box,
		}
	}

	// Agglomerative clustering: merge closest clusters until distance > hT
	for {
		// Find closest pair of clusters
		minDist := math.MaxFloat64
		minI, minJ := -1, -1

		for i := 0; i < len(clusters)-1; i++ {
			for j := i + 1; j < len(clusters); j++ {
				dist := weightedEdgeDistance(clusters[i].Box, clusters[j].Box)
				if dist < minDist {
					minDist = dist
					minI, minJ = i, j
				}
			}
		}

		// Stop if minimum distance exceeds threshold
		if minDist > hT || minI == -1 {
			break
		}

		// Merge clusters[minI] and clusters[minJ]
		merged := Segment{
			Words: append(clusters[minI].Words, clusters[minJ].Words...),
			Box:   mergeRects(clusters[minI].Box, clusters[minJ].Box),
		}

		// Remove old clusters and add merged one
		newClusters := make([]Segment, 0, len(clusters)-1)
		for i := range clusters {
			if i == minI {
				newClusters = append(newClusters, merged)
			} else if i != minJ {
				newClusters = append(newClusters, clusters[i])
			}
		}
		clusters = newClusters
	}

	return clusters
}

// tagLine classifies a line based on its segments
// Implements PDF-TREX line tagging algorithm
func tagLine(line Line, segments []Segment, pageWidth float64) LineType {
	if len(segments) == 0 {
		return UnknownLine
	}

	if len(segments) == 1 {
		// Single segment: check if it spans more than half the page width
		segment := segments[0]
		if segment.Box.Width() > pageWidth*0.5 {
			return TextLine
		}
		return UnknownLine
	}

	// Multiple segments indicate table structure
	return TableLine
}

// buildTaggedLines creates tagged lines with segments from regular lines
func buildTaggedLines(lines []Line, hT float64, pageWidth float64) []TaggedLine {
	taggedLines := make([]TaggedLine, 0, len(lines))

	for _, line := range lines {
		segments := buildSegmentsFromLine(line, hT)
		lineType := tagLine(line, segments, pageWidth)

		taggedLines = append(taggedLines, TaggedLine{
			Line:     line,
			Segments: segments,
			Type:     lineType,
		})
	}

	return taggedLines
}

// TableArea represents a region containing table lines
type TableArea struct {
	Lines []TaggedLine
	Box   Rect
}

// buildTableAreas groups consecutive table/unknown lines into table areas
func buildTableAreas(taggedLines []TaggedLine) []TableArea {
	if len(taggedLines) == 0 {
		return nil
	}

	var areas []TableArea
	var currentArea []TaggedLine

	for i, tl := range taggedLines {
		// Start or continue table area if line is table or unknown
		if tl.Type == TableLine || tl.Type == UnknownLine {
			if len(currentArea) == 0 {
				currentArea = []TaggedLine{tl}
			} else {
				currentArea = append(currentArea, tl)
			}
		} else {
			// Text line ends current table area
			if len(currentArea) > 0 {
				area := createTableArea(currentArea)
				// Validate before adding
				if isValidTableArea(area) {
					areas = append(areas, area)
				}
				currentArea = nil
			}
		}

		// Handle end of lines
		if i == len(taggedLines)-1 && len(currentArea) > 0 {
			area := createTableArea(currentArea)
			if isValidTableArea(area) {
				areas = append(areas, area)
			}
		}
	}

	return areas
}

// isValidTableArea validates whether a table area is likely a real table
// Implements validation rules to reduce false positives
func isValidTableArea(area TableArea) bool {
	// Must have at least 3 lines for a valid table (header + at least 2 data rows)
	if len(area.Lines) < 3 {
		return false
	}

	// Count how many lines are actually tagged as TableLine
	tableLineCount := 0
	for _, line := range area.Lines {
		if line.Type == TableLine {
			tableLineCount++
		}
	}

	// Must have at least 3 table lines (not just unknown lines)
	if tableLineCount < 3 {
		return false
	}

	// Check segment consistency across lines
	// Real tables have consistent segment patterns (similar number of columns)
	segmentCounts := make(map[int]int)
	var tableLines []TaggedLine

	for _, line := range area.Lines {
		if line.Type == TableLine {
			segmentCounts[len(line.Segments)]++
			tableLines = append(tableLines, line)
		}
	}

	// If segment counts are highly variable, likely not a table
	// Find most common segment count
	var maxCount, maxSegments int
	for segments, count := range segmentCounts {
		if count > maxCount {
			maxCount = count
			maxSegments = segments
		}
	}

	// At least 60% of table lines should have the same number of segments
	if float64(maxCount)/float64(tableLineCount) < 0.6 {
		return false
	}

	// Must have at least 2 segments (columns) for a table
	if maxSegments < 2 {
		return false
	}

	// Check vertical alignment of segments
	// Real tables have segments that align vertically across rows
	if !hasVerticalAlignment(tableLines, maxSegments) {
		return false
	}

	return true
}

// hasVerticalAlignment checks if segments align vertically across rows
func hasVerticalAlignment(lines []TaggedLine, expectedSegments int) bool {
	if len(lines) < 2 {
		return true
	}

	// For each column position, collect segment X positions
	columnPositions := make([][]float64, expectedSegments)

	for _, line := range lines {
		// Only check lines with expected segment count
		if len(line.Segments) != expectedSegments {
			continue
		}

		for i, seg := range line.Segments {
			if i < expectedSegments {
				columnPositions[i] = append(columnPositions[i], seg.Box.X0)
			}
		}
	}

	// Check if positions are consistent (low standard deviation)
	for _, positions := range columnPositions {
		if len(positions) < 2 {
			continue
		}

		stdDev := calculateStdDev(positions)
		avgX := average(positions)

		// If standard deviation is > 20% of average position, not aligned
		if stdDev > avgX*0.2 && stdDev > 20 {
			return false
		}
	}

	return true
}

// createTableArea creates a table area from tagged lines
func createTableArea(lines []TaggedLine) TableArea {
	if len(lines) == 0 {
		return TableArea{}
	}

	// Calculate bounding box
	box := lines[0].Line.Box
	for i := 1; i < len(lines); i++ {
		box = mergeRects(box, lines[i].Line.Box)
	}

	return TableArea{
		Lines: lines,
		Box:   box,
	}
}

// Block represents vertically aligned segments across multiple lines
type Block struct {
	Segments []Segment
	Box      Rect
	LineIndices []int // Which lines this block spans
}

// buildBlocksFromTableArea creates blocks by vertically clustering segments
// Implements PDF-TREX block building for multi-line header recognition
func buildBlocksFromTableArea(area TableArea, vT float64) []Block {
	if len(area.Lines) == 0 {
		return nil
	}

	// Collect all segments with their line indices
	type indexedSegment struct {
		segment   Segment
		lineIndex int
	}

	var allSegments []indexedSegment
	for lineIdx, tl := range area.Lines {
		for _, seg := range tl.Segments {
			allSegments = append(allSegments, indexedSegment{
				segment:   seg,
				lineIndex: lineIdx,
			})
		}
	}

	if len(allSegments) == 0 {
		return nil
	}

	// Start with each segment as a cluster
	clusters := make([]Block, len(allSegments))
	for i, is := range allSegments {
		clusters[i] = Block{
			Segments:    []Segment{is.segment},
			Box:         is.segment.Box,
			LineIndices: []int{is.lineIndex},
		}
	}

	// Agglomerative clustering: merge vertically close clusters
	for {
		minDist := math.MaxFloat64
		minI, minJ := -1, -1

		for i := 0; i < len(clusters)-1; i++ {
			for j := i + 1; j < len(clusters); j++ {
				// Check vertical overlap
				if verticalOverlapRatio(clusters[i].Box, clusters[j].Box) > 0.3 {
					dist := verticalDistance(clusters[i].Box, clusters[j].Box)
					if dist < minDist {
						minDist = dist
						minI, minJ = i, j
					}
				}
			}
		}

		// Stop if minimum distance exceeds threshold
		if minDist > vT || minI == -1 {
			break
		}

		// Merge clusters
		merged := Block{
			Segments:    append(clusters[minI].Segments, clusters[minJ].Segments...),
			Box:         mergeRects(clusters[minI].Box, clusters[minJ].Box),
			LineIndices: append(clusters[minI].LineIndices, clusters[minJ].LineIndices...),
		}

		// Remove duplicates from line indices
		lineIdxMap := make(map[int]bool)
		for _, idx := range merged.LineIndices {
			lineIdxMap[idx] = true
		}
		merged.LineIndices = nil
		for idx := range lineIdxMap {
			merged.LineIndices = append(merged.LineIndices, idx)
		}
		sort.Ints(merged.LineIndices)

		// Remove old clusters and add merged one
		newClusters := make([]Block, 0, len(clusters)-1)
		for i := range clusters {
			if i == minI {
				newClusters = append(newClusters, merged)
			} else if i != minJ {
				newClusters = append(newClusters, clusters[i])
			}
		}
		clusters = newClusters
	}

	return clusters
}

// SegmentTableRow represents a logical table row (may span multiple lines)
// Used internally by segment-based table detection
type SegmentTableRow struct {
	Lines    []TaggedLine
	Segments []Segment
	Box      Rect
}

// buildRowsFromBlocks creates table rows from blocks
// Implements PDF-TREX row recognition: multi-line headers as single row
func buildRowsFromBlocks(area TableArea, blocks []Block) []SegmentTableRow {
	if len(area.Lines) == 0 {
		return nil
	}

	// Track which lines have been assigned to rows
	assignedLines := make(map[int]bool)
	var rows []SegmentTableRow

	// Process each block
	for _, block := range blocks {
		// Check if block spans multiple lines with only 1 TbL
		if len(block.LineIndices) > 1 {
			tableLineCount := 0
			for _, idx := range block.LineIndices {
				if area.Lines[idx].Type == TableLine {
					tableLineCount++
				}
			}

			// If block spans multiple lines with 1 TbL, merge into single row
			if tableLineCount == 1 {
				var rowLines []TaggedLine
				for _, idx := range block.LineIndices {
					if !assignedLines[idx] {
						rowLines = append(rowLines, area.Lines[idx])
						assignedLines[idx] = true
					}
				}

				if len(rowLines) > 0 {
					rows = append(rows, SegmentTableRow{
						Lines:    rowLines,
						Segments: block.Segments,
						Box:      block.Box,
					})
				}
				continue
			}
		}

		// Otherwise, each line in block is separate row
		for _, idx := range block.LineIndices {
			if !assignedLines[idx] {
				rows = append(rows, SegmentTableRow{
					Lines:    []TaggedLine{area.Lines[idx]},
					Segments: area.Lines[idx].Segments,
					Box:      area.Lines[idx].Line.Box,
				})
				assignedLines[idx] = true
			}
		}
	}

	// Add any unassigned lines as individual rows
	for i, tl := range area.Lines {
		if !assignedLines[i] {
			rows = append(rows, SegmentTableRow{
				Lines:    []TaggedLine{tl},
				Segments: tl.Segments,
				Box:      tl.Line.Box,
			})
		}
	}

	// Sort rows by Y position
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Box.Y0 < rows[j].Box.Y0
	})

	return rows
}

// TableColumn represents a logical table column
type TableColumn struct {
	Segments []Segment
	Box      Rect
}

// buildColumnsFromRows creates table columns from rows
// Implements PDF-TREX column building with spanning header duplication
func buildColumnsFromRows(rows []SegmentTableRow, hT float64) []TableColumn {
	if len(rows) == 0 {
		return nil
	}

	// Collect all segments
	var allSegments []Segment
	for _, row := range rows {
		allSegments = append(allSegments, row.Segments...)
	}

	if len(allSegments) == 0 {
		return nil
	}

	// Group segments into columns based on vertical overlap
	var columns []TableColumn

	for _, seg := range allSegments {
		// Find columns this segment overlaps with
		var overlappingCols []*TableColumn
		for i := range columns {
			if verticalOverlapRatio(seg.Box, columns[i].Box) > 0.3 {
				overlappingCols = append(overlappingCols, &columns[i])
			}
		}

		if len(overlappingCols) > 1 {
			// Segment spans multiple columns - duplicate it to each
			for _, col := range overlappingCols {
				col.Segments = append(col.Segments, seg)
				col.Box = mergeRects(col.Box, seg.Box)
			}
		} else if len(overlappingCols) == 1 {
			// Segment belongs to one column
			overlappingCols[0].Segments = append(overlappingCols[0].Segments, seg)
			overlappingCols[0].Box = mergeRects(overlappingCols[0].Box, seg.Box)
		} else {
			// New column
			columns = append(columns, TableColumn{
				Segments: []Segment{seg},
				Box:      seg.Box,
			})
		}
	}

	// Merge single-segment columns that are close to multi-segment columns
	merged := mergeSingleSegmentColumns(columns, hT)

	// Sort columns left to right
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Box.X0 < merged[j].Box.X0
	})

	// Adjust column boundaries to be contiguous
	merged = makeColumnsContiguous(merged)

	return merged
}

// mergeSingleSegmentColumns merges single-segment columns close to multi-segment ones
func mergeSingleSegmentColumns(columns []TableColumn, hT float64) []TableColumn {
	if len(columns) <= 1 {
		return columns
	}

	// Identify single and multi-segment columns
	var singleSeg []int
	var multiSeg []int

	for i, col := range columns {
		if len(col.Segments) == 1 {
			singleSeg = append(singleSeg, i)
		} else {
			multiSeg = append(multiSeg, i)
		}
	}

	// For each single-segment column, check if it should merge with adjacent multi-segment
	toMerge := make(map[int]int) // single index -> multi index to merge with

	for _, sIdx := range singleSeg {
		sCol := columns[sIdx]

		// Check adjacent multi-segment columns
		for _, mIdx := range multiSeg {
			mCol := columns[mIdx]

			// Calculate horizontal distance
			dist := math.Abs(sCol.Box.CenterX() - mCol.Box.CenterX())

			// If close enough, mark for merging
			if dist < hT {
				toMerge[sIdx] = mIdx
				break
			}
		}
	}

	// Perform merges
	for sIdx, mIdx := range toMerge {
		columns[mIdx].Segments = append(columns[mIdx].Segments, columns[sIdx].Segments...)
		columns[mIdx].Box = mergeRects(columns[mIdx].Box, columns[sIdx].Box)
	}

	// Remove merged single-segment columns
	var result []TableColumn
	for i, col := range columns {
		if _, merged := toMerge[i]; !merged {
			result = append(result, col)
		}
	}

	return result
}

// makeColumnsContiguous adjusts column boundaries to be contiguous
func makeColumnsContiguous(columns []TableColumn) []TableColumn {
	if len(columns) <= 1 {
		return columns
	}

	// Sort by X position
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Box.X0 < columns[j].Box.X0
	})

	// Adjust boundaries
	for i := 0; i < len(columns)-1; i++ {
		// Set right edge of column i to midpoint with column i+1
		midpoint := (columns[i].Box.X1 + columns[i+1].Box.X0) / 2
		columns[i].Box.X1 = midpoint
		columns[i+1].Box.X0 = midpoint
	}

	return columns
}

// SegmentTableCell represents a final table cell with 2D coordinates
// Used internally by segment-based table detection
type SegmentTableCell struct {
	Content string
	Row     int
	Column  int
	Box     Rect
}

// buildCellsFromRowsAndColumns creates the final 2D cell grid
// Implements PDF-TREX table building
func buildCellsFromRowsAndColumns(rows []SegmentTableRow, columns []TableColumn) [][]SegmentTableCell {
	if len(rows) == 0 || len(columns) == 0 {
		return nil
	}

	// Create 2D grid
	grid := make([][]SegmentTableCell, len(rows))
	for r := range grid {
		grid[r] = make([]SegmentTableCell, len(columns))
	}

	// Fill grid
	for r, row := range rows {
		for c, col := range columns {
			// Find intersection of row and column
			cellBox := Rect{
				X0: col.Box.X0,
				Y0: row.Box.Y0,
				X1: col.Box.X1,
				Y1: row.Box.Y1,
			}

			// Find all words in this cell
			var cellWords []EnrichedWord
			for _, seg := range row.Segments {
				for _, word := range seg.Words {
					// Check if word is in cell box
					if wordInBox(word, cellBox) {
						cellWords = append(cellWords, word)
					}
				}
			}

			// Sort words left-to-right, top-to-bottom
			sort.Slice(cellWords, func(i, j int) bool {
				if math.Abs(cellWords[i].Box.Y0-cellWords[j].Box.Y0) < 3 {
					return cellWords[i].Box.X0 < cellWords[j].Box.X0
				}
				return cellWords[i].Box.Y0 < cellWords[j].Box.Y0
			})

			// Concatenate text
			var content string
			for i, word := range cellWords {
				content += word.Text
				if i < len(cellWords)-1 {
					// Add space between words on same line
					if math.Abs(cellWords[i].Box.Y0-cellWords[i+1].Box.Y0) < 3 {
						content += " "
					} else {
						// Newline for multi-line cells
						content += "\n"
					}
				}
			}

			grid[r][c] = SegmentTableCell{
				Content: content,
				Row:     r,
				Column:  c,
				Box:     cellBox,
			}
		}
	}

	return grid
}

// wordInBox checks if a word's center is within the box
func wordInBox(word EnrichedWord, box Rect) bool {
	centerX := word.Box.CenterX()
	centerY := word.Box.CenterY()

	return centerX >= box.X0 && centerX <= box.X1 &&
		centerY >= box.Y0 && centerY <= box.Y1
}

// DetectTablesSegmentBased detects tables using segment-based approach
// This is an alternative to line-based detection for PDFs without ruling lines
func DetectTablesSegmentBased(page *Page, thresholds AdaptiveThresholds) []Table {
	if len(page.Paragraphs) == 0 {
		return nil
	}

	// Collect all words from paragraphs
	var words []EnrichedWord
	for _, para := range page.Paragraphs {
		for _, line := range para.Lines {
			words = append(words, line.Words...)
		}
	}

	if len(words) == 0 {
		return nil
	}

	// Build lines from words (use existing baseline-aware grouping)
	lines := groupWordsIntoLinesBaseline(words)

	// Build tagged lines with segments
	taggedLines := buildTaggedLines(lines, thresholds.HorizontalThreshold, page.Width)

	// Build table areas
	tableAreas := buildTableAreas(taggedLines)

	// Convert table areas to tables
	var tables []Table
	for _, area := range tableAreas {
		// Build blocks
		blocks := buildBlocksFromTableArea(area, thresholds.VerticalThreshold)

		// Build rows
		rows := buildRowsFromBlocks(area, blocks)

		// Build columns
		columns := buildColumnsFromRows(rows, thresholds.HorizontalThreshold)

		// Build cells
		cellGrid := buildCellsFromRowsAndColumns(rows, columns)

		// Convert to Table type
		if len(cellGrid) > 0 && len(cellGrid[0]) > 0 {
			table := convertCellGridToTable(cellGrid, area.Box)

			// Final validation: ensure table meets minimum requirements
			if isValidTable(table) {
				tables = append(tables, table)
			}
		}
	}

	return tables
}

// isValidTable performs final validation on extracted table
func isValidTable(table Table) bool {
	// Must have at least 4 rows and 2 columns (header + 3 data rows minimum)
	// This reduces false positives from formatted text
	if table.NumRows < 4 || table.NumCols < 2 {
		return false
	}

	// Count non-empty cells
	nonEmptyCells := 0
	totalCells := 0

	for _, row := range table.Rows {
		for _, cell := range row.Cells {
			totalCells++
			if strings.TrimSpace(cell.Content) != "" {
				nonEmptyCells++
			}
		}
	}

	// At least 40% of cells must have content (stricter than 30%)
	if totalCells > 0 && float64(nonEmptyCells)/float64(totalCells) < 0.4 {
		return false
	}

	// Check for consistent column count across rows
	if len(table.Rows) > 0 {
		expectedCols := len(table.Rows[0].Cells)
		for _, row := range table.Rows {
			if len(row.Cells) != expectedCols {
				// Inconsistent column count suggests malformed table
				return false
			}
		}
	}

	return true
}

// convertCellGridToTable converts cell grid to Table structure
func convertCellGridToTable(grid [][]SegmentTableCell, box Rect) Table {
	// Convert Rect to CellBBox
	bbox := CellBBox{
		X0:     box.X0,
		Top:    box.Y0,
		X1:     box.X1,
		Bottom: box.Y1,
	}

	// Convert to TableRow format
	tableRows := make([]TableRow, len(grid))
	for r := range grid {
		cells := make([]TableCell, len(grid[r]))
		for c := range grid[r] {
			cells[c] = TableCell{
				BBox: CellBBox{
					X0:     grid[r][c].Box.X0,
					Top:    grid[r][c].Box.Y0,
					X1:     grid[r][c].Box.X1,
					Bottom: grid[r][c].Box.Y1,
				},
				Content: grid[r][c].Content,
			}
		}
		tableRows[r] = TableRow{
			Cells: cells,
			BBox:  bbox,
		}
	}

	return Table{
		BBox:    bbox,
		Rows:    tableRows,
		NumRows: len(grid),
		NumCols: len(grid[0]),
	}
}

