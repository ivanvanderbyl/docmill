package docmill

import (
	"math"
	"testing"
)

// TestHorizontalOverlapRatio tests horizontal overlap ratio calculation
func TestHorizontalOverlapRatio(t *testing.T) {
	tests := []struct {
		name     string
		r1       Rect
		r2       Rect
		expected float64
	}{
		{
			name:     "no overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 0, Y0: 20, X1: 10, Y1: 30},
			expected: 0,
		},
		{
			name:     "complete overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			expected: 1.0,
		},
		{
			name:     "partial overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 0, Y0: 5, X1: 10, Y1: 15},
			expected: 0.5, // 5 points overlap out of 10 points height
		},
		{
			name:     "one contains other",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 20},
			r2:       Rect{X0: 0, Y0: 5, X1: 10, Y1: 15},
			expected: 1.0, // r2 completely inside r1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := horizontalOverlapRatio(tt.r1, tt.r2)
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("horizontalOverlapRatio() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestVerticalOverlapRatio tests vertical overlap ratio calculation
func TestVerticalOverlapRatio(t *testing.T) {
	tests := []struct {
		name     string
		r1       Rect
		r2       Rect
		expected float64
	}{
		{
			name:     "no overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 20, Y0: 0, X1: 30, Y1: 10},
			expected: 0,
		},
		{
			name:     "complete overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			expected: 1.0,
		},
		{
			name:     "partial overlap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 5, Y0: 0, X1: 15, Y1: 10},
			expected: 0.5, // 5 points overlap out of 10 points width
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verticalOverlapRatio(tt.r1, tt.r2)
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("verticalOverlapRatio() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestHorizontalDistance tests horizontal distance calculation
func TestHorizontalDistance(t *testing.T) {
	tests := []struct {
		name     string
		r1       Rect
		r2       Rect
		expected float64
	}{
		{
			name:     "horizontally overlapped, with gap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 15, Y0: 5, X1: 25, Y1: 15},
			expected: 5, // Gap of 5 points
		},
		{
			name:     "not horizontally overlapped",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 15, Y0: 20, X1: 25, Y1: 30},
			expected: math.MaxFloat64,
		},
		{
			name:     "touching edges",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 10, Y0: 5, X1: 20, Y1: 15},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := horizontalDistance(tt.r1, tt.r2)
			if math.Abs(result-tt.expected) > 0.01 && result != math.MaxFloat64 && tt.expected != math.MaxFloat64 {
				t.Errorf("horizontalDistance() = %v, want %v", result, tt.expected)
			} else if result == math.MaxFloat64 && tt.expected != math.MaxFloat64 {
				t.Errorf("horizontalDistance() = MaxFloat64, want %v", tt.expected)
			} else if result != math.MaxFloat64 && tt.expected == math.MaxFloat64 {
				t.Errorf("horizontalDistance() = %v, want MaxFloat64", result)
			}
		})
	}
}

// TestVerticalDistance tests vertical distance calculation
func TestVerticalDistance(t *testing.T) {
	tests := []struct {
		name     string
		r1       Rect
		r2       Rect
		expected float64
	}{
		{
			name:     "vertically overlapped, with gap",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 5, Y0: 15, X1: 15, Y1: 25},
			expected: 5, // Gap of 5 points
		},
		{
			name:     "not vertically overlapped",
			r1:       Rect{X0: 0, Y0: 0, X1: 10, Y1: 10},
			r2:       Rect{X0: 20, Y0: 15, X1: 30, Y1: 25},
			expected: math.MaxFloat64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verticalDistance(tt.r1, tt.r2)
			if result != math.MaxFloat64 && tt.expected != math.MaxFloat64 {
				if math.Abs(result-tt.expected) > 0.01 {
					t.Errorf("verticalDistance() = %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

// TestBuildSegmentsFromLine tests segment clustering
func TestBuildSegmentsFromLine(t *testing.T) {
	// Create a line with words that should cluster into 3 segments
	line := Line{
		Words: []EnrichedWord{
			{Text: "Name", Box: Rect{X0: 10, Y0: 0, X1: 40, Y1: 10}},
			{Text: "Age", Box: Rect{X0: 100, Y0: 0, X1: 120, Y1: 10}},
			{Text: "City", Box: Rect{X0: 200, Y0: 0, X1: 230, Y1: 10}},
		},
	}

	hT := 30.0 // Horizontal threshold
	segments := buildSegmentsFromLine(line, hT)

	// Should create 3 segments (words are far apart)
	if len(segments) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(segments))
	}

	// Test with close words
	closeLine := Line{
		Words: []EnrichedWord{
			{Text: "First", Box: Rect{X0: 10, Y0: 0, X1: 40, Y1: 10}},
			{Text: "Last", Box: Rect{X0: 45, Y0: 0, X1: 70, Y1: 10}},
		},
	}

	closeSegments := buildSegmentsFromLine(closeLine, hT)

	// Should merge into 1 segment (gap is only 5, less than threshold 30)
	if len(closeSegments) != 1 {
		t.Errorf("Expected 1 segment for close words, got %d", len(closeSegments))
	}
}

// TestTagLine tests line type classification
func TestTagLine(t *testing.T) {
	pageWidth := 612.0 // Standard page width

	tests := []struct {
		name     string
		line     Line
		segments []Segment
		expected LineType
	}{
		{
			name: "text line - single segment spanning > 50% width",
			line: Line{},
			segments: []Segment{
				{Box: Rect{X0: 10, Y0: 0, X1: 400, Y1: 10}},
			},
			expected: TextLine,
		},
		{
			name: "unknown line - single segment spanning < 50% width",
			line: Line{},
			segments: []Segment{
				{Box: Rect{X0: 10, Y0: 0, X1: 200, Y1: 10}},
			},
			expected: UnknownLine,
		},
		{
			name: "table line - multiple segments",
			line: Line{},
			segments: []Segment{
				{Box: Rect{X0: 10, Y0: 0, X1: 100, Y1: 10}},
				{Box: Rect{X0: 200, Y0: 0, X1: 300, Y1: 10}},
			},
			expected: TableLine,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tagLine(tt.line, tt.segments, pageWidth)
			if result != tt.expected {
				t.Errorf("tagLine() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCalculateAdaptiveThresholds tests adaptive threshold calculation
func TestCalculateAdaptiveThresholds(t *testing.T) {
	// Create words with consistent spacing
	words := []EnrichedWord{
		// Line 1
		{Box: Rect{X0: 0, Y0: 0, X1: 30, Y1: 10}},
		{Box: Rect{X0: 40, Y0: 0, X1: 70, Y1: 10}},
		{Box: Rect{X0: 80, Y0: 0, X1: 110, Y1: 10}},
		// Line 2 (15 points below)
		{Box: Rect{X0: 0, Y0: 15, X1: 30, Y1: 25}},
		{Box: Rect{X0: 40, Y0: 15, X1: 70, Y1: 25}},
		// Line 3 (15 points below)
		{Box: Rect{X0: 0, Y0: 30, X1: 30, Y1: 40}},
	}

	thresholds := calculateAdaptiveThresholds(words)

	// Should have reasonable threshold values
	if thresholds.HorizontalThreshold < 5 || thresholds.HorizontalThreshold > 100 {
		t.Errorf("HorizontalThreshold %v outside reasonable range [5, 100]", thresholds.HorizontalThreshold)
	}

	if thresholds.VerticalThreshold < 5 || thresholds.VerticalThreshold > 100 {
		t.Errorf("VerticalThreshold %v outside reasonable range [5, 100]", thresholds.VerticalThreshold)
	}
}

// TestBuildTableAreas tests table area grouping
func TestBuildTableAreas(t *testing.T) {
	// Create tagged lines with enough TableLines to pass validation
	taggedLines := []TaggedLine{
		{Type: TextLine, Line: Line{Box: Rect{Y0: 0, Y1: 10}}},
		{
			Type: TableLine,
			Line: Line{Box: Rect{Y0: 15, Y1: 25}},
			Segments: []Segment{
				{Box: Rect{X0: 10, Y0: 15, X1: 100, Y1: 25}},
				{Box: Rect{X0: 200, Y0: 15, X1: 290, Y1: 25}},
			},
		},
		{
			Type: TableLine,
			Line: Line{Box: Rect{Y0: 30, Y1: 40}},
			Segments: []Segment{
				{Box: Rect{X0: 10, Y0: 30, X1: 100, Y1: 40}},
				{Box: Rect{X0: 200, Y0: 30, X1: 290, Y1: 40}},
			},
		},
		{Type: UnknownLine, Line: Line{Box: Rect{Y0: 45, Y1: 55}}},
		{
			Type: TableLine,
			Line: Line{Box: Rect{Y0: 60, Y1: 70}},
			Segments: []Segment{
				{Box: Rect{X0: 10, Y0: 60, X1: 100, Y1: 70}},
				{Box: Rect{X0: 200, Y0: 60, X1: 290, Y1: 70}},
			},
		},
		{Type: TextLine, Line: Line{Box: Rect{Y0: 75, Y1: 85}}},
	}

	areas := buildTableAreas(taggedLines)

	// Should create 1 table area (3 TbL + 1 UnL with consistent segments)
	if len(areas) != 1 {
		t.Errorf("Expected 1 table area, got %d", len(areas))
	}

	if len(areas) > 0 {
		// Table area should contain 4 lines
		if len(areas[0].Lines) != 4 {
			t.Errorf("Expected 4 lines in table area, got %d", len(areas[0].Lines))
		}
	}
}

// TestBuildBlocksFromTableArea tests block clustering for multi-line headers
func TestBuildBlocksFromTableArea(t *testing.T) {
	// Create table area with segments that should cluster vertically
	area := TableArea{
		Lines: []TaggedLine{
			{
				Type: TableLine,
				Segments: []Segment{
					{Box: Rect{X0: 10, Y0: 0, X1: 100, Y1: 10}},
					{Box: Rect{X0: 200, Y0: 0, X1: 290, Y1: 10}},
				},
			},
			{
				Type: UnknownLine,
				Segments: []Segment{
					{Box: Rect{X0: 10, Y0: 12, X1: 100, Y1: 22}}, // Vertically aligned with first segment
				},
			},
		},
	}

	vT := 5.0 // Vertical threshold
	blocks := buildBlocksFromTableArea(area, vT)

	// Should create at least 2 blocks
	if len(blocks) < 2 {
		t.Errorf("Expected at least 2 blocks, got %d", len(blocks))
	}

	// One block should span 2 lines (vertically aligned segments)
	hasMultiLineBlock := false
	for _, block := range blocks {
		if len(block.LineIndices) > 1 {
			hasMultiLineBlock = true
			break
		}
	}

	if !hasMultiLineBlock {
		t.Error("Expected at least one block spanning multiple lines")
	}
}

// TestMergeSingleSegmentColumns tests single-segment column merging
func TestMergeSingleSegmentColumns(t *testing.T) {
	columns := []TableColumn{
		// Multi-segment column
		{
			Segments: []Segment{
				{Box: Rect{X0: 10, Y0: 0, X1: 100, Y1: 10}},
				{Box: Rect{X0: 15, Y0: 20, X1: 95, Y1: 30}},
			},
			Box: Rect{X0: 10, Y0: 0, X1: 100, Y1: 30},
		},
		// Single-segment column close to multi-segment (should merge)
		{
			Segments: []Segment{
				{Box: Rect{X0: 50, Y0: 40, X1: 80, Y1: 50}},
			},
			Box: Rect{X0: 50, Y0: 40, X1: 80, Y1: 50},
		},
		// Another multi-segment column far away
		{
			Segments: []Segment{
				{Box: Rect{X0: 300, Y0: 0, X1: 400, Y1: 10}},
				{Box: Rect{X0: 310, Y0: 20, X1: 390, Y1: 30}},
			},
			Box: Rect{X0: 300, Y0: 0, X1: 400, Y1: 30},
		},
	}

	hT := 100.0
	merged := mergeSingleSegmentColumns(columns, hT)

	// Should merge single-segment into first multi-segment column
	// Resulting in 2 columns instead of 3
	if len(merged) != 2 {
		t.Errorf("Expected 2 columns after merging, got %d", len(merged))
	}
}

// TestBuildCellsFromRowsAndColumns tests cell grid generation
func TestBuildCellsFromRowsAndColumns(t *testing.T) {
	// Create simple 2x2 table
	rows := []SegmentTableRow{
		{
			Box: Rect{X0: 0, Y0: 0, X1: 200, Y1: 10},
			Segments: []Segment{
				{
					Words: []EnrichedWord{
						{Text: "Name", Box: Rect{X0: 10, Y0: 0, X1: 40, Y1: 10}},
					},
				},
				{
					Words: []EnrichedWord{
						{Text: "Age", Box: Rect{X0: 110, Y0: 0, X1: 130, Y1: 10}},
					},
				},
			},
		},
		{
			Box: Rect{X0: 0, Y0: 15, X1: 200, Y1: 25},
			Segments: []Segment{
				{
					Words: []EnrichedWord{
						{Text: "John", Box: Rect{X0: 10, Y0: 15, X1: 40, Y1: 25}},
					},
				},
				{
					Words: []EnrichedWord{
						{Text: "30", Box: Rect{X0: 110, Y0: 15, X1: 125, Y1: 25}},
					},
				},
			},
		},
	}

	columns := []TableColumn{
		{Box: Rect{X0: 0, Y0: 0, X1: 100, Y1: 30}},
		{Box: Rect{X0: 100, Y0: 0, X1: 200, Y1: 30}},
	}

	grid := buildCellsFromRowsAndColumns(rows, columns)

	// Should create 2x2 grid
	if len(grid) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(grid))
	}

	if len(grid) > 0 && len(grid[0]) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(grid[0]))
	}

	// Check cell contents
	if len(grid) >= 2 && len(grid[0]) >= 2 {
		if grid[0][0].Content != "Name" {
			t.Errorf("Expected 'Name' in cell [0,0], got '%s'", grid[0][0].Content)
		}
		if grid[0][1].Content != "Age" {
			t.Errorf("Expected 'Age' in cell [0,1], got '%s'", grid[0][1].Content)
		}
		if grid[1][0].Content != "John" {
			t.Errorf("Expected 'John' in cell [1,0], got '%s'", grid[1][0].Content)
		}
		if grid[1][1].Content != "30" {
			t.Errorf("Expected '30' in cell [1,1], got '%s'", grid[1][1].Content)
		}
	}
}

// TestCalculateTableOverlap tests table deduplication logic
func TestCalculateTableOverlap(t *testing.T) {
	t1 := Table{
		BBox: CellBBox{X0: 0, Top: 0, X1: 100, Bottom: 100},
	}

	tests := []struct {
		name     string
		t2       Table
		expected float64
	}{
		{
			name:     "identical tables",
			t2:       Table{BBox: CellBBox{X0: 0, Top: 0, X1: 100, Bottom: 100}},
			expected: 1.0,
		},
		{
			name:     "50% overlap",
			t2:       Table{BBox: CellBBox{X0: 50, Top: 0, X1: 150, Bottom: 100}},
			expected: 0.5,
		},
		{
			name:     "no overlap",
			t2:       Table{BBox: CellBBox{X0: 200, Top: 0, X1: 300, Bottom: 100}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTableOverlap(t1, tt.t2)
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("calculateTableOverlap() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestDeduplicateTables tests table deduplication
func TestDeduplicateTables(t *testing.T) {
	tables := []Table{
		{BBox: CellBBox{X0: 0, Top: 0, X1: 100, Bottom: 100}},
		{BBox: CellBBox{X0: 5, Top: 5, X1: 105, Bottom: 105}},   // ~80% overlap with first
		{BBox: CellBBox{X0: 200, Top: 0, X1: 300, Bottom: 100}}, // No overlap
	}

	unique := deduplicateTables(tables)

	// Should keep 2 tables (first and third, second is duplicate of first)
	if len(unique) != 2 {
		t.Errorf("Expected 2 unique tables, got %d", len(unique))
	}
}
