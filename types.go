package pdfmarkdown

import "slices"

import "github.com/klippa-app/go-pdfium/references"

// Rect represents a bounding box in PDF coordinates.
type Rect struct {
	X0 float64 // Left
	Y0 float64 // Top (after conversion from PDF coordinates)
	X1 float64 // Right
	Y1 float64 // Bottom (after conversion from PDF coordinates)
}

// Width returns the width of the rectangle.
func (r Rect) Width() float64 {
	return r.X1 - r.X0
}

// Height returns the height of the rectangle.
func (r Rect) Height() float64 {
	return r.Y1 - r.Y0
}

// CenterY returns the vertical center of the rectangle.
func (r Rect) CenterY() float64 {
	return (r.Y0 + r.Y1) / 2
}

// RGBA represents a color.
type RGBA struct {
	R, G, B, A uint
}

// EnrichedChar represents a single character with all its metadata.
type EnrichedChar struct {
	Text       rune
	Box        Rect
	FontSize   float64
	FontWeight int
	FontName   string
	FontFlags  int
	FillColor  RGBA
	Angle      float32
	IsHyphen   bool
}

// EnrichedWord represents a word with aggregated style information.
type EnrichedWord struct {
	Text        string
	Box         Rect
	FontSize    float64 // Average font size
	FontWeight  int     // Dominant font weight
	FontName    string  // Dominant font name
	FontFlags   int     // Dominant font flags
	FillColor   RGBA    // Dominant fill color
	IsBold      bool
	IsItalic    bool
	IsMonospace bool
	Baseline    float64 // Y-coordinate of the text baseline
	XHeight     float64 // Height of lowercase letters
	Rotation    float64 // Rotation angle in degrees (0, 90, 180, 270, etc.)
}

// IsBulletOrNumber checks if the word looks like a list marker.
func (w EnrichedWord) IsBulletOrNumber() bool {
	if len(w.Text) == 0 {
		return false
	}

	// Get the first rune (properly handles multi-byte UTF-8)
	runes := []rune(w.Text)
	firstChar := runes[0]

	// Common bullet characters
	bullets := []rune{'•', '◦', '▪', '▫', '–', '-', '*', '→'}
	if slices.Contains(bullets, firstChar) {
		return true
	}

	// Number followed by period or parenthesis
	if len(runes) >= 2 {
		if firstChar >= '0' && firstChar <= '9' {
			lastChar := runes[len(runes)-1]
			if lastChar == '.' || lastChar == ')' {
				return true
			}
		}
	}

	return false
}

// Line represents a horizontal line of text.
type Line struct {
	Words    []EnrichedWord
	Box      Rect
	Baseline float64 // Y-coordinate of the baseline
}

// Paragraph represents a block of text.
type Paragraph struct {
	Lines        []Line
	Box          Rect
	Alignment    Alignment
	IsHeading    bool
	HeadingLevel int // 1-6 for markdown headings
	IsList       bool
	IsCode       bool
	Indent       float64 // Left indentation
}

// Text returns the full text of the paragraph.
func (p Paragraph) Text() string {
	var result string
	for i, line := range p.Lines {
		for j, word := range line.Words {
			result += word.Text
			if j < len(line.Words)-1 {
				result += " "
			}
		}
		if i < len(p.Lines)-1 {
			result += "\n"
		}
	}
	return result
}

// Alignment represents text alignment.
type Alignment int

const (
	AlignmentLeft Alignment = iota
	AlignmentCenter
	AlignmentRight
	AlignmentJustified
)

// Column represents a vertical column of text in a multi-column layout.
type Column struct {
	Box        Rect
	Words      []EnrichedWord
	Paragraphs []Paragraph
	Index      int // Column number (0-indexed from left to right)
}

// TextBlock represents a block of text with consistent rotation/orientation.
type TextBlock struct {
	Words            []EnrichedWord
	Lines            []Line
	Rotation         float64 // Rotation angle in degrees
	ReadingDirection string  // "ltr", "rtl", "ttb", "btt"
}

// Page represents all extracted content from a PDF page.
type Page struct {
	Number     int
	Width      float64
	Height     float64
	Quality    PageQuality
	Paragraphs []Paragraph
	Tables     []Table
	Lines      []Edge   // Explicit line objects extracted from PDF
	Columns    []Column // Detected column layout
}

// Document represents the complete extracted document structure.
type Document struct {
	Pages []Page
}

// PageExtractor provides context for extracting text from a page.
type PageExtractor struct {
	textPage   references.FPDF_TEXTPAGE
	pageHeight float64
	pageWidth  float64
}
