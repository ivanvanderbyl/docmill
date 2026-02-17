package docmill

import (
	"math"
	"testing"
)

// TestCalculateBaseline tests baseline calculation for words
func TestCalculateBaseline(t *testing.T) {
	tests := []struct {
		name     string
		word     EnrichedWord
		expected float64
	}{
		{
			name: "standard word with 12pt font",
			word: EnrichedWord{
				Box:      Rect{Y0: 90, Y1: 102},
				FontSize: 12,
			},
			expected: 100.2, // Y1 - (fontSize * 0.15) = 102 - 1.8
		},
		{
			name: "larger font 24pt",
			word: EnrichedWord{
				Box:      Rect{Y0: 80, Y1: 104},
				FontSize: 24,
			},
			expected: 100.4, // 104 - 3.6
		},
		{
			name: "small font 8pt",
			word: EnrichedWord{
				Box:      Rect{Y0: 92, Y1: 100},
				FontSize: 8,
			},
			expected: 98.8, // 100 - 1.2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBaseline(tt.word)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("calculateBaseline() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestCalculateXHeight tests x-height calculation
func TestCalculateXHeight(t *testing.T) {
	tests := []struct {
		name     string
		word     EnrichedWord
		expected float64
	}{
		{
			name: "word with lowercase letters",
			word: EnrichedWord{
				Text:     "hello",
				Box:      Rect{Y0: 90, Y1: 100},
				FontSize: 12,
			},
			expected: 7.0, // Height * 0.7 = 10 * 0.7
		},
		{
			name: "word with only uppercase",
			word: EnrichedWord{
				Text:     "HELLO",
				Box:      Rect{Y0: 90, Y1: 100},
				FontSize: 12,
			},
			expected: 6.0, // FontSize * 0.5 = 12 * 0.5
		},
		{
			name: "word with mixed case",
			word: EnrichedWord{
				Text:     "Hello",
				Box:      Rect{Y0: 90, Y1: 100},
				FontSize: 12,
			},
			expected: 7.0, // Has lowercase, so Height * 0.7
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateXHeight(tt.word)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("calculateXHeight() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestIsCJK tests CJK character detection
func TestIsCJK(t *testing.T) {
	tests := []struct {
		name     string
		char     rune
		expected bool
	}{
		{"Chinese character", '微', true},
		{"Chinese character 2", '软', true},
		{"Japanese Hiragana", 'あ', false}, // Not in CJK Unified Ideographs
		{"Latin letter", 'a', false},
		{"Number", '1', false},
		{"CJK Extension A", '\u3400', true},
		{"CJK Extension B", '\U00020000', true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCJK(tt.char)
			if result != tt.expected {
				t.Errorf("isCJK(%c) = %v, want %v", tt.char, result, tt.expected)
			}
		})
	}
}

// TestDeduplicateCJKChars tests CJK character deduplication
func TestDeduplicateCJKChars(t *testing.T) {
	tests := []struct {
		name     string
		words    []EnrichedWord
		expected string
	}{
		{
			name: "duplicate CJK characters",
			words: []EnrichedWord{
				{
					Text:     "微微软软",
					Box:      Rect{X0: 0, X1: 24},
					FontSize: 12,
				},
			},
			expected: "微软", // Deduplicates because avgCharWidth (6) < fontSize*0.7 (8.4)
		},
		{
			name: "legitimate repetition",
			words: []EnrichedWord{
				{
					Text:     "微微软软",
					Box:      Rect{X0: 0, X1: 48},
					FontSize: 12,
				},
			},
			expected: "微微软软", // Keeps because avgCharWidth (12) >= fontSize*0.7 (8.4)
		},
		{
			name: "non-CJK text",
			words: []EnrichedWord{
				{
					Text:     "hello",
					Box:      Rect{X0: 0, X1: 30},
					FontSize: 12,
				},
			},
			expected: "hello", // No change for non-CJK
		},
		{
			name: "mixed CJK and non-CJK",
			words: []EnrichedWord{
				{
					Text:     "微a微b",
					Box:      Rect{X0: 0, X1: 24},
					FontSize: 12,
				},
			},
			expected: "微a微b", // Doesn't deduplicate because not consecutive identical CJK
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateCJKChars(tt.words)
			if len(result) == 0 {
				t.Fatal("deduplicateCJKChars returned empty slice")
			}
			if result[0].Text != tt.expected {
				t.Errorf("deduplicateCJKChars() = %v, want %v", result[0].Text, tt.expected)
			}
		})
	}
}

// TestExpandLigatures tests ligature expansion
func TestExpandLigatures(t *testing.T) {
	tests := []struct {
		name     string
		words    []EnrichedWord
		expected string
	}{
		{
			name: "fi ligature",
			words: []EnrichedWord{
				{Text: "of\uFB01ce"}, // office with fi ligature
			},
			expected: "office",
		},
		{
			name: "fl ligature",
			words: []EnrichedWord{
				{Text: "\uFB02oor"}, // floor with fl ligature
			},
			expected: "floor",
		},
		{
			name: "ffi ligature",
			words: []EnrichedWord{
				{Text: "e\uFB03cient"}, // efficient with ffi ligature
			},
			expected: "efficient",
		},
		{
			name: "multiple ligatures",
			words: []EnrichedWord{
				{Text: "of\uFB01ce \uFB02oor"}, // office floor
			},
			expected: "office floor",
		},
		{
			name: "no ligatures",
			words: []EnrichedWord{
				{Text: "hello world"},
			},
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandLigatures(tt.words)
			if len(result) == 0 {
				t.Fatal("expandLigatures returned empty slice")
			}
			if result[0].Text != tt.expected {
				t.Errorf("expandLigatures() = %v, want %v", result[0].Text, tt.expected)
			}
		})
	}
}

// TestNormalizeAngle tests angle normalization
func TestNormalizeAngle(t *testing.T) {
	tests := []struct {
		name     string
		angle    float64
		expected float64
	}{
		{"positive angle", 45, 45},
		{"negative angle", -45, 315},
		{"over 360", 450, 90},
		{"under -360", -450, 270},
		{"zero", 0, 0},
		{"360", 360, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeAngle(tt.angle)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("normalizeAngle(%v) = %v, want %v", tt.angle, result, tt.expected)
			}
		})
	}
}

// TestQuantizeAngle tests angle quantization
func TestQuantizeAngle(t *testing.T) {
	tests := []struct {
		name     string
		angle    float64
		step     float64
		expected float64
	}{
		{"snap to 15", 12, 15, 15},
		{"snap to 15 down", 7, 15, 0},
		{"snap to 45", 40, 45, 45},
		{"snap to 90", 95, 90, 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quantizeAngle(tt.angle, tt.step)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("quantizeAngle(%v, %v) = %v, want %v", tt.angle, tt.step, result, tt.expected)
			}
		})
	}
}

// TestInferReadingDirection tests reading direction inference
func TestInferReadingDirection(t *testing.T) {
	tests := []struct {
		name     string
		angle    float64
		expected string
	}{
		{"horizontal ltr", 0, "ltr"},
		{"horizontal ltr near 0", 20, "ltr"},
		{"horizontal ltr near 360", 350, "ltr"},
		{"vertical ttb", 90, "ttb"},
		{"vertical ttb near 90", 80, "ttb"},
		{"horizontal rtl", 180, "rtl"},
		{"vertical btt", 270, "btt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferReadingDirection(tt.angle)
			if result != tt.expected {
				t.Errorf("inferReadingDirection(%v) = %v, want %v", tt.angle, result, tt.expected)
			}
		})
	}
}

// TestCalculateMedian tests median calculation
func TestCalculateMedian(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"odd count", []float64{1, 3, 5, 7, 9}, 5},
		{"even count", []float64{1, 2, 3, 4}, 2.5},
		{"single value", []float64{42}, 42},
		{"unsorted", []float64{9, 1, 5, 3, 7}, 5},
		{"empty", []float64{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateMedian(tt.values)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("calculateMedian(%v) = %v, want %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestCalculateStdDev tests standard deviation calculation
func TestCalculateStdDev(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"uniform values", []float64{5, 5, 5, 5}, 0},
		{"simple spread", []float64{2, 4, 6, 8}, 2.236}, // √5 ≈ 2.236
		{"single value", []float64{10}, 0},
		{"empty", []float64{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateStdDev(tt.values)
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("calculateStdDev(%v) = %v, want %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestRotatePoint tests point rotation
func TestRotatePoint(t *testing.T) {
	tests := []struct {
		name      string
		x, y      float64
		angle     float64
		expectedX float64
		expectedY float64
	}{
		{"90 degrees", 1, 0, 90, 0, 1},
		{"180 degrees", 1, 0, 180, -1, 0},
		{"270 degrees", 1, 0, 270, 0, -1},
		{"0 degrees", 1, 0, 0, 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := rotatePoint(tt.x, tt.y, tt.angle)
			if math.Abs(x-tt.expectedX) > 0.01 || math.Abs(y-tt.expectedY) > 0.01 {
				t.Errorf("rotatePoint(%v, %v, %v) = (%v, %v), want (%v, %v)",
					tt.x, tt.y, tt.angle, x, y, tt.expectedX, tt.expectedY)
			}
		})
	}
}

// TestMergeRects tests rectangle merging
func TestMergeRects(t *testing.T) {
	r1 := Rect{X0: 0, Y0: 0, X1: 10, Y1: 10}
	r2 := Rect{X0: 5, Y0: 5, X1: 15, Y1: 15}

	result := mergeRects(r1, r2)

	expected := Rect{X0: 0, Y0: 0, X1: 15, Y1: 15}

	if result != expected {
		t.Errorf("mergeRects() = %v, want %v", result, expected)
	}
}

// TestClamp tests value clamping
func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min      float64
		max      float64
		expected float64
	}{
		{"within range", 5, 0, 10, 5},
		{"below min", -5, 0, 10, 0},
		{"above max", 15, 0, 10, 10},
		{"at min", 0, 0, 10, 0},
		{"at max", 10, 0, 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clamp(tt.value, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("clamp(%v, %v, %v) = %v, want %v",
					tt.value, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

// TestGroupWordsIntoLinesBaseline tests baseline-aware line grouping
func TestGroupWordsIntoLinesBaseline(t *testing.T) {
	words := []EnrichedWord{
		{Text: "Hello", Box: Rect{X0: 0, Y0: 0, X1: 30, Y1: 10}, Baseline: 9, XHeight: 5},
		{Text: "World", Box: Rect{X0: 35, Y0: 0, X1: 65, Y1: 10}, Baseline: 9, XHeight: 5},
		{Text: "Next", Box: Rect{X0: 0, Y0: 20, X1: 25, Y1: 30}, Baseline: 29, XHeight: 5},
		{Text: "Line", Box: Rect{X0: 30, Y0: 20, X1: 50, Y1: 30}, Baseline: 29, XHeight: 5},
	}

	lines := groupWordsIntoLinesBaseline(words)

	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}

	if len(lines[0].Words) != 2 {
		t.Errorf("Expected 2 words in first line, got %d", len(lines[0].Words))
	}

	if lines[0].Words[0].Text != "Hello" || lines[0].Words[1].Text != "World" {
		t.Errorf("First line words incorrect: %v", lines[0].Words)
	}

	if len(lines[1].Words) != 2 {
		t.Errorf("Expected 2 words in second line, got %d", len(lines[1].Words))
	}
}

// TestCalculateDynamicThreshold tests adaptive paragraph spacing threshold
func TestCalculateDynamicThreshold(t *testing.T) {
	// Create lines with consistent spacing
	lines := []Line{
		{Box: Rect{Y0: 0, Y1: 10}},
		{Box: Rect{Y0: 12, Y1: 22}}, // gap: 2
		{Box: Rect{Y0: 24, Y1: 34}}, // gap: 2
		{Box: Rect{Y0: 36, Y1: 46}}, // gap: 2
		{Box: Rect{Y0: 60, Y1: 70}}, // gap: 14 (paragraph break)
		{Box: Rect{Y0: 72, Y1: 82}}, // gap: 2
	}

	// Set font sizes for all lines
	for i := range lines {
		lines[i].Words = []EnrichedWord{
			{FontSize: 12},
		}
	}

	threshold := calculateDynamicThreshold(lines, nil)

	// Threshold should be between 0.6 and 1.5 (clamped)
	if threshold < 0.6 || threshold > 1.5 {
		t.Errorf("Threshold %v outside expected range [0.6, 1.5]", threshold)
	}

	// With mostly small gaps and one large gap, threshold should be somewhere
	// around the small gaps normalized by font size
	// median gap ≈ 2, stdDev ≈ 4, so (2 + 1.5*4)/12 ≈ 0.67
	if threshold < 0.6 || threshold > 1.0 {
		t.Errorf("Threshold %v outside reasonable range [0.6, 1.0] for test data", threshold)
	}
}

// TestFilterWordsByXRange tests column word filtering
func TestFilterWordsByXRange(t *testing.T) {
	words := []EnrichedWord{
		{Text: "Left1", Box: Rect{X0: 10, Y0: 0, X1: 40, Y1: 10}},
		{Text: "Left2", Box: Rect{X0: 10, Y0: 20, X1: 40, Y1: 30}},
		{Text: "Right1", Box: Rect{X0: 310, Y0: 0, X1: 340, Y1: 10}},
		{Text: "Right2", Box: Rect{X0: 310, Y0: 20, X1: 340, Y1: 30}},
	}

	// Filter left column (0-200)
	leftWords := filterWordsByXRange(words, 0, 200)
	if len(leftWords) != 2 {
		t.Errorf("Expected 2 words in left column, got %d", len(leftWords))
	}
	if leftWords[0].Text != "Left1" || leftWords[1].Text != "Left2" {
		t.Errorf("Wrong words in left column: %v", leftWords)
	}

	// Filter right column (200-400)
	rightWords := filterWordsByXRange(words, 200, 400)
	if len(rightWords) != 2 {
		t.Errorf("Expected 2 words in right column, got %d", len(rightWords))
	}
	if rightWords[0].Text != "Right1" || rightWords[1].Text != "Right2" {
		t.Errorf("Wrong words in right column: %v", rightWords)
	}
}

// TestDetectColumns tests multi-column detection
func TestDetectColumns(t *testing.T) {
	// Create two distinct columns of words
	var words []EnrichedWord

	// Left column (X: 50-150)
	for i := 0; i < 10; i++ {
		words = append(words, EnrichedWord{
			Text: "Left",
			Box:  Rect{X0: 50, Y0: float64(i * 15), X1: 150, Y1: float64(i*15 + 10)},
		})
	}

	// Right column (X: 350-450)
	for i := 0; i < 10; i++ {
		words = append(words, EnrichedWord{
			Text: "Right",
			Box:  Rect{X0: 350, Y0: float64(i * 15), X1: 450, Y1: float64(i*15 + 10)},
		})
	}

	columns := detectColumns(words, 612) // Standard page width

	if len(columns) < 2 {
		t.Errorf("Expected at least 2 columns, got %d", len(columns))
	}

	// Verify columns are ordered left to right
	if len(columns) >= 2 && columns[0].Box.X0 > columns[1].Box.X0 {
		t.Error("Columns not ordered left to right")
	}
}
