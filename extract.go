package docmill

import (
	"math"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/pkg/errors"
)

// rawPageData holds the data extracted from pdfium before structure detection.
// This separation allows pdfium I/O (single-threaded) to be decoupled from the
// CPU-bound structure detection pipeline which can run concurrently.
type rawPageData struct {
	pageNumber int
	pageWidth  float64
	pageHeight float64
	words      []EnrichedWord
	lines      []Edge
}

// extractRawPageData extracts raw characters, words, and line objects from a PDF
// page using the pdfium instance. This must run sequentially because pdfium is
// single-threaded (WASM).
func extractRawPageData(instance pdfium.Pdfium, page references.FPDF_PAGE, pageNumber int) (*rawPageData, error) {
	pageSize, err := instance.FPDF_GetPageWidthF(&requests.FPDF_GetPageWidthF{
		Page: requests.Page{
			ByReference: &page,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get page width")
	}

	pageHeight, err := instance.FPDF_GetPageHeightF(&requests.FPDF_GetPageHeightF{
		Page: requests.Page{
			ByReference: &page,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get page size")
	}

	textPage, err := instance.FPDFText_LoadPage(&requests.FPDFText_LoadPage{
		Page: requests.Page{
			ByReference: &page,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to load text page")
	}
	defer instance.FPDFText_ClosePage(&requests.FPDFText_ClosePage{
		TextPage: textPage.TextPage,
	})

	charCount, err := instance.FPDFText_CountChars(&requests.FPDFText_CountChars{
		TextPage: textPage.TextPage,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to count characters")
	}

	pw := float64(pageSize.PageWidth)
	ph := float64(pageHeight.PageHeight)

	if charCount.Count == 0 {
		return &rawPageData{
			pageNumber: pageNumber,
			pageWidth:  pw,
			pageHeight: ph,
		}, nil
	}

	chars, err := extractEnrichedChars(instance, textPage.TextPage, charCount.Count, ph)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract characters")
	}

	generatedSpaces := detectGeneratedSpaces(instance, textPage.TextPage, charCount.Count)

	words := groupCharsIntoWords(chars, generatedSpaces)
	words = expandLigatures(words)
	words = deduplicateCJKChars(words)

	lines, err := extractLinesFromPage(instance, page, pw, ph)
	if err != nil {
		lines = []Edge{}
	}

	return &rawPageData{
		pageNumber: pageNumber,
		pageWidth:  pw,
		pageHeight: ph,
		words:      words,
		lines:      lines,
	}, nil
}

// buildPageStructure runs the CPU-bound structure detection pipeline on
// pre-extracted raw page data. This is safe to call concurrently.
// stats may be nil when processing a single page without document context.
func buildPageStructure(raw *rawPageData, config Config, stats *DocumentStats) *Page {
	if len(raw.words) == 0 {
		return &Page{
			Number:     raw.pageNumber,
			Width:      raw.pageWidth,
			Height:     raw.pageHeight,
			Paragraphs: []Paragraph{},
		}
	}

	quality := assessPageQuality(raw.words)

	var paragraphs []Paragraph
	if quality.IsLowQuality {
		paragraphs = buildParagraphsNoDetection(raw.words, raw.pageWidth, config, stats)
	} else {
		paragraphs = buildParagraphs(raw.words, raw.pageWidth, config, stats)
	}

	columns := detectColumns(raw.words, raw.pageWidth)

	resultPage := &Page{
		Number:     raw.pageNumber,
		Width:      raw.pageWidth,
		Height:     raw.pageHeight,
		Quality:    quality,
		Paragraphs: paragraphs,
		Lines:      raw.lines,
		Columns:    columns,
	}

	if config.DetectTables {
		var tables []Table

		if config.UseSegmentBasedTables {
			var thresholds AdaptiveThresholds
			if config.UseAdaptiveThresholds {
				thresholds = calculateAdaptiveThresholds(raw.words)
			} else {
				thresholds = AdaptiveThresholds{
					HorizontalThreshold: 20.0,
					VerticalThreshold:   5.0,
				}
			}

			segmentTables := DetectTablesSegmentBased(resultPage, thresholds)
			tables = append(tables, segmentTables...)
		}

		if len(raw.lines) > 0 {
			lineTables := DetectTables(resultPage, config.TableSettings)
			tables = append(tables, lineTables...)
		}

		tables = deduplicateTables(tables)
		resultPage.Tables = tables
	}

	return resultPage
}

// ExtractPage extracts all enriched text from a PDF page.
func ExtractPage(instance pdfium.Pdfium, page references.FPDF_PAGE, pageNumber int, config Config) (*Page, error) {
	raw, err := extractRawPageData(instance, page, pageNumber)
	if err != nil {
		return nil, err
	}
	return buildPageStructure(raw, config, nil), nil
}

// deduplicateTables removes duplicate tables based on bounding box overlap.
// Uses Union-Find to cluster overlapping tables and keeps the best candidate
// from each cluster (most rows × columns). Inspired by Docling's overlap
// resolution approach.
func deduplicateTables(tables []Table) []Table {
	if len(tables) <= 1 {
		return tables
	}

	// Union-Find structure
	parent := make([]int, len(tables))
	rank := make([]int, len(tables))
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x]) // path compression
		}
		return parent[x]
	}

	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if rank[ra] < rank[rb] {
			ra, rb = rb, ra
		}
		parent[rb] = ra
		if rank[ra] == rank[rb] {
			rank[ra]++
		}
	}

	// Merge overlapping tables into clusters
	for i := 0; i < len(tables); i++ {
		for j := i + 1; j < len(tables); j++ {
			if calculateTableOverlap(tables[i], tables[j]) > 0.7 {
				union(i, j)
			}
		}
	}

	// Collect clusters and pick the best table from each
	clusters := make(map[int][]int)
	for i := range tables {
		root := find(i)
		clusters[root] = append(clusters[root], i)
	}

	unique := make([]Table, 0, len(clusters))
	for _, members := range clusters {
		bestIdx := members[0]
		bestScore := tables[bestIdx].NumRows * tables[bestIdx].NumCols
		for _, idx := range members[1:] {
			score := tables[idx].NumRows * tables[idx].NumCols
			if score > bestScore {
				bestScore = score
				bestIdx = idx
			}
		}
		unique = append(unique, tables[bestIdx])
	}

	return unique
}

// calculateTableOverlap calculates the overlap ratio between two tables
func calculateTableOverlap(t1, t2 Table) float64 {
	// Calculate intersection area
	x0 := math.Max(t1.BBox.X0, t2.BBox.X0)
	y0 := math.Max(t1.BBox.Top, t2.BBox.Top)
	x1 := math.Min(t1.BBox.X1, t2.BBox.X1)
	y1 := math.Min(t1.BBox.Bottom, t2.BBox.Bottom)

	if x1 <= x0 || y1 <= y0 {
		return 0 // No overlap
	}

	intersectionArea := (x1 - x0) * (y1 - y0)
	t1Width := t1.BBox.X1 - t1.BBox.X0
	t1Height := t1.BBox.Bottom - t1.BBox.Top
	t2Width := t2.BBox.X1 - t2.BBox.X0
	t2Height := t2.BBox.Bottom - t2.BBox.Top

	t1Area := t1Width * t1Height
	t2Area := t2Width * t2Height

	// Overlap ratio relative to smaller table
	smallerArea := math.Min(t1Area, t2Area)
	if smallerArea == 0 {
		return 0
	}

	return intersectionArea / smallerArea
}

// extractEnrichedChars extracts all characters with their metadata.
func extractEnrichedChars(instance pdfium.Pdfium, textPage references.FPDF_TEXTPAGE, count int, pageHeight float64) ([]EnrichedChar, error) {
	chars := make([]EnrichedChar, 0, count)

	for i := range count {
		// Get Unicode character
		unicodeRes, err := instance.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{
			TextPage: textPage,
			Index:    i,
		})
		if err != nil || unicodeRes.Unicode == 0 {
			continue
		}

		// Get bounding box
		charBox, err := instance.FPDFText_GetCharBox(&requests.FPDFText_GetCharBox{
			TextPage: textPage,
			Index:    i,
		})
		if err != nil {
			continue
		}

		// Convert PDF coordinates (origin bottom-left) to standard (origin top-left)
		box := Rect{
			X0: charBox.Left,
			Y0: pageHeight - charBox.Top,
			X1: charBox.Right,
			Y1: pageHeight - charBox.Bottom,
		}

		// Get font size
		fontSize, err := instance.FPDFText_GetFontSize(&requests.FPDFText_GetFontSize{
			TextPage: textPage,
			Index:    i,
		})
		fontSizeVal := 12.0 // Default
		if err == nil {
			fontSizeVal = fontSize.FontSize
		}

		// Get font weight
		fontWeight, err := instance.FPDFText_GetFontWeight(&requests.FPDFText_GetFontWeight{
			TextPage: textPage,
			Index:    i,
		})
		fontWeightVal := 400 // Default normal weight
		if err == nil {
			fontWeightVal = fontWeight.FontWeight
		}

		// Get font info
		fontInfo, err := instance.FPDFText_GetFontInfo(&requests.FPDFText_GetFontInfo{
			TextPage: textPage,
			Index:    i,
		})
		fontNameVal := ""
		fontFlagsVal := 0
		if err == nil {
			fontNameVal = fontInfo.FontName
			fontFlagsVal = fontInfo.Flags
		}

		// Get fill color
		fillColor, err := instance.FPDFText_GetFillColor(&requests.FPDFText_GetFillColor{
			TextPage: textPage,
			Index:    i,
		})
		fillColorVal := RGBA{R: 0, G: 0, B: 0, A: 255} // Default black
		if err == nil {
			fillColorVal = RGBA{
				R: fillColor.R,
				G: fillColor.G,
				B: fillColor.B,
				A: fillColor.A,
			}
		}

		// Get angle
		angle, err := instance.FPDFText_GetCharAngle(&requests.FPDFText_GetCharAngle{
			TextPage: textPage,
			Index:    i,
		})
		angleVal := float32(0)
		if err == nil {
			angleVal = angle.CharAngle
		}

		// Check if hyphen
		isHyphen, err := instance.FPDFText_IsHyphen(&requests.FPDFText_IsHyphen{
			TextPage: textPage,
			Index:    i,
		})
		isHyphenVal := false
		if err == nil {
			isHyphenVal = isHyphen.IsHyphen
		}

		chars = append(chars, EnrichedChar{
			Text:       rune(unicodeRes.Unicode),
			Box:        box,
			FontSize:   fontSizeVal,
			FontWeight: fontWeightVal,
			FontName:   fontNameVal,
			FontFlags:  fontFlagsVal,
			FillColor:  fillColorVal,
			Angle:      angleVal,
			IsHyphen:   isHyphenVal,
		})
	}

	return chars, nil
}

func detectGeneratedSpaces(instance pdfium.Pdfium, textPage references.FPDF_TEXTPAGE, count int) map[int]bool {
	generated := make(map[int]bool)

	textResp, err := instance.FPDFText_GetText(&requests.FPDFText_GetText{
		TextPage:   textPage,
		StartIndex: 0,
		Count:      count,
	})
	if err != nil || textResp.Text == "" {
		return generated
	}

	fullText := []rune(textResp.Text)
	rawIdx := 0

	for _, r := range fullText {
		if rawIdx >= count {
			break
		}
		unicodeRes, err := instance.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{
			TextPage: textPage,
			Index:    rawIdx,
		})
		if err != nil {
			rawIdx++
			continue
		}
		rawChar := rune(unicodeRes.Unicode)

		if r == ' ' && rawChar != ' ' && rawChar != '\t' {
			generated[rawIdx] = true
			continue
		}
		rawIdx++
	}

	return generated
}

// isLowerCase returns true if the rune is a lowercase letter
func isLowerCase(r rune) bool {
	return r >= 'a' && r <= 'z'
}

// isUpperCase returns true if the rune is an uppercase letter
func isUpperCase(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

// isDigit returns true if the rune is a digit
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// isAlpha returns true if the rune is a letter
func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isCurrency returns true if the rune is a currency symbol
func isCurrency(r rune) bool {
	return r == '$' || r == '€' || r == '£' || r == '¥' || r == '¢'
}

// isPunctuation returns true if the rune is punctuation
func isPunctuation(r rune) bool {
	return r == '.' || r == ',' || r == ';' || r == ':' || r == '!' || r == '?'
}

// calculateAverageCharWidth calculates the average character width for a set of chars
func calculateAverageCharWidth(chars []EnrichedChar) float64 {
	if len(chars) == 0 {
		return 0
	}
	var totalWidth float64
	for _, char := range chars {
		totalWidth += char.Box.Width()
	}
	return totalWidth / float64(len(chars))
}

func detectWordBoundaries(chars []EnrichedChar, generatedSpaces map[int]bool) []int {
	if len(chars) <= 1 {
		return nil
	}

	var boundaries []int

	for i := 1; i < len(chars); i++ {
		curr := chars[i]

		if curr.Text == ' ' || curr.Text == '\t' || curr.Text == '\n' || curr.Text == '\r' {
			boundaries = append(boundaries, i)
			continue
		}

		if generatedSpaces[i] {
			boundaries = append(boundaries, i)
			continue
		}
	}

	return boundaries
}

// isRotatedText checks if a character is rotated (not horizontal)
// angle is in radians (0 = horizontal, π/2 ≈ 1.57 = 90°, π ≈ 3.14 = 180°, 3π/2 ≈ 4.71 = 270°)
func isRotatedText(angle float32) bool {
	// Convert radians to degrees
	degrees := float64(angle) * 180.0 / math.Pi

	// Normalize to 0-360 range
	for degrees < 0 {
		degrees += 360
	}
	for degrees >= 360 {
		degrees -= 360
	}

	// Consider text rotated if angle is not close to 0 or 180 degrees
	// Allow 10 degree tolerance
	tolerance := 10.0
	return !(degrees < tolerance || degrees > 360-tolerance || (degrees > 180-tolerance && degrees < 180+tolerance))
}

// shouldReverseCharOrder checks if character order should be reversed based on rotation
func shouldReverseCharOrder(angle float32) bool {
	degrees := float64(angle) * 180.0 / math.Pi
	for degrees < 0 {
		degrees += 360
	}
	for degrees >= 360 {
		degrees -= 360
	}

	// Reverse for 270° rotation (bottom-to-top text)
	// Allow 45 degree range: 225-315 degrees
	return degrees > 225 && degrees < 315
}

func detectWordBoundariesRotationAware(chars []EnrichedChar, generatedSpaces map[int]bool) []int {
	if len(chars) <= 1 {
		return nil
	}

	var boundaries []int

	// Check if text is rotated
	isRotated := false
	if len(chars) > 0 {
		isRotated = isRotatedText(chars[0].Angle)
	}

	if isRotated {
		// For rotated text (90°, 270°), use Y-axis gaps instead of X-axis
		avgCharHeight := 0.0
		for _, char := range chars {
			avgCharHeight += char.Box.Height()
		}
		avgCharHeight /= float64(len(chars))

		for i := 1; i < len(chars); i++ {
			prev, curr := chars[i-1], chars[i]

			// For rotated text, check Y-axis gap
			gapY := math.Abs(curr.Box.Y0 - prev.Box.Y1)
			if avgCharHeight > 0 && gapY > avgCharHeight*0.3 {
				boundaries = append(boundaries, i)
				continue
			}

			// Still apply other heuristics
			if curr.Text == ' ' || curr.Text == '\t' || curr.Text == '\n' || curr.Text == '\r' {
				boundaries = append(boundaries, i)
				continue
			}

			if isLowerCase(prev.Text) && isUpperCase(curr.Text) {
				boundaries = append(boundaries, i)
				continue
			}

			if isDigit(prev.Text) && isAlpha(curr.Text) {
				boundaries = append(boundaries, i)
				continue
			}

			if isAlpha(prev.Text) && isDigit(curr.Text) {
				boundaries = append(boundaries, i)
				continue
			}
		}
	} else {
		boundaries = detectWordBoundaries(chars, generatedSpaces)
	}

	return boundaries
}

func groupCharsIntoWords(chars []EnrichedChar, generatedSpaces map[int]bool) []EnrichedWord {
	if len(chars) == 0 {
		return nil
	}

	boundaries := detectWordBoundariesRotationAware(chars, generatedSpaces)

	// Check if we need to reverse character order (for 270° rotated text)
	shouldReverse := len(chars) > 0 && shouldReverseCharOrder(chars[0].Angle)
	if shouldReverse {
		// Reverse the chars slice
		reversed := make([]EnrichedChar, len(chars))
		for i, char := range chars {
			reversed[len(chars)-1-i] = char
		}
		chars = reversed

		// Reverse the boundary indices too
		reversedBoundaries := make([]int, len(boundaries))
		for i, b := range boundaries {
			reversedBoundaries[i] = len(chars) - b
		}
		boundaries = reversedBoundaries
	}

	boundarySet := make(map[int]bool)
	for _, b := range boundaries {
		boundarySet[b] = true
	}

	var words []EnrichedWord
	var currentWord []EnrichedChar
	var wordBox Rect
	wordStarted := false

	for i, char := range chars {
		isWhitespace := char.Text == ' ' || char.Text == '\t' || char.Text == '\n' || char.Text == '\r'
		isBoundary := boundarySet[i]

		// Start new word at boundary (but skip if it's whitespace)
		if isBoundary && !isWhitespace && len(currentWord) > 0 {
			words = append(words, aggregateWord(currentWord, wordBox))
			currentWord = nil
			wordStarted = false
		}

		if !isWhitespace {
			if !wordStarted {
				wordBox = char.Box
				wordStarted = true
			} else {
				// Expand bounding box
				wordBox.X0 = math.Min(wordBox.X0, char.Box.X0)
				wordBox.Y0 = math.Min(wordBox.Y0, char.Box.Y0)
				wordBox.X1 = math.Max(wordBox.X1, char.Box.X1)
				wordBox.Y1 = math.Max(wordBox.Y1, char.Box.Y1)
			}
			currentWord = append(currentWord, char)
		}

		// End word on whitespace or end of text
		if (isWhitespace || i == len(chars)-1) && len(currentWord) > 0 {
			words = append(words, aggregateWord(currentWord, wordBox))
			currentWord = nil
			wordStarted = false
		}
	}

	return words
}

// aggregateWord creates an EnrichedWord from a slice of characters.
func aggregateWord(chars []EnrichedChar, box Rect) EnrichedWord {
	if len(chars) == 0 {
		return EnrichedWord{}
	}

	// Build text
	var text string
	for _, char := range chars {
		text += string(char.Text)
	}

	// Calculate average font size
	var totalFontSize float64
	for _, char := range chars {
		totalFontSize += char.FontSize
	}
	avgFontSize := totalFontSize / float64(len(chars))

	// Find dominant font weight (most common)
	weightCounts := make(map[int]int)
	for _, char := range chars {
		weightCounts[char.FontWeight]++
	}
	var dominantWeight int
	var maxCount int
	for weight, count := range weightCounts {
		if count > maxCount {
			dominantWeight = weight
			maxCount = count
		}
	}

	// Find dominant font name
	fontCounts := make(map[string]int)
	for _, char := range chars {
		fontCounts[char.FontName]++
	}
	var dominantFont string
	maxCount = 0
	for font, count := range fontCounts {
		if count > maxCount {
			dominantFont = font
			maxCount = count
		}
	}

	// Get first char's font flags (usually consistent within a word)
	fontFlags := chars[0].FontFlags

	// Determine style flags
	isBold := dominantWeight >= 700
	isItalic := (fontFlags & 0x40) != 0    // Italic flag from PDF spec
	isMonospace := (fontFlags & 0x01) != 0 // FixedPitch flag

	// Calculate average rotation angle
	var totalAngle float64
	for _, char := range chars {
		totalAngle += float64(char.Angle)
	}
	avgAngle := totalAngle / float64(len(chars))

	word := EnrichedWord{
		Text:        text,
		Box:         box,
		FontSize:    avgFontSize,
		FontWeight:  dominantWeight,
		FontName:    dominantFont,
		FontFlags:   fontFlags,
		FillColor:   chars[0].FillColor,
		IsBold:      isBold,
		IsItalic:    isItalic,
		IsMonospace: isMonospace,
		Rotation:    float64(avgAngle) * 180 / 3.14159, // Convert radians to degrees
	}

	// Calculate baseline and x-height
	word.Baseline = calculateBaseline(word)
	word.XHeight = calculateXHeight(word)

	return word
}

// ligatureMap maps ligature unicode codepoints to their expanded forms
var ligatureMap = map[rune]string{
	0xFB00: "ff",
	0xFB01: "fi",
	0xFB02: "fl",
	0xFB03: "ffi",
	0xFB04: "ffl",
	0xFB05: "ft",
	0xFB06: "st",
}

// expandLigatures expands ligature characters into their component letters
func expandLigatures(words []EnrichedWord) []EnrichedWord {
	for i := range words {
		word := &words[i]
		runes := []rune(word.Text)
		hasLigature := false

		// Check if word contains any ligatures
		for _, r := range runes {
			if _, isLigature := ligatureMap[r]; isLigature {
				hasLigature = true
				break
			}
		}

		if !hasLigature {
			continue
		}

		// Expand ligatures
		var expanded []rune
		for _, r := range runes {
			if expansion, isLigature := ligatureMap[r]; isLigature {
				expanded = append(expanded, []rune(expansion)...)
			} else {
				expanded = append(expanded, r)
			}
		}

		word.Text = string(expanded)
	}
	return words
}

// isCJK checks if a rune is in a CJK unicode block
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Unified Ideographs Extension B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK Unified Ideographs Extension C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK Unified Ideographs Extension D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK Unified Ideographs Extension E
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0x2F800 && r <= 0x2FA1F) // CJK Compatibility Ideographs Supplement
}

// containsCJK checks if a slice of runes contains any CJK characters
func containsCJK(runes []rune) bool {
	for _, r := range runes {
		if isCJK(r) {
			return true
		}
	}
	return false
}

// deduplicateCJKChars removes duplicate consecutive CJK characters that appear
// at nearly identical positions (common rendering artifact in some PDFs)
func deduplicateCJKChars(words []EnrichedWord) []EnrichedWord {
	for i := range words {
		word := &words[i]
		runes := []rune(word.Text)

		// Only process words containing CJK characters
		if !containsCJK(runes) {
			continue
		}

		if len(runes) <= 1 {
			continue
		}

		// Build deduplicated text by checking consecutive identical characters
		deduplicated := []rune{runes[0]}

		for j := 1; j < len(runes); j++ {
			// Check if current character is identical to previous AND is CJK
			if runes[j] == runes[j-1] && isCJK(runes[j]) {
				// Calculate approximate horizontal spacing
				// Since we've already grouped into words, we use the word width
				// divided by character count as an approximation
				avgCharWidth := word.Box.Width() / float64(len(runes))

				// If this looks like a duplicate (same char, CJK, typical spacing suggests overlap)
				// Skip it. This heuristic catches cases like "微微软软" -> "微软"
				if avgCharWidth < word.FontSize*0.7 {
					continue
				}
			}

			deduplicated = append(deduplicated, runes[j])
		}

		word.Text = string(deduplicated)
	}
	return words
}
