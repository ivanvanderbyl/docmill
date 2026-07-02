package pdf

import (
	"math"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

type TextRect struct {
	Text     string
	Left     float64
	Top      float64
	Right    float64
	Bottom   float64
	FontSize float64

	// Font formatting from PDFium (GetPageTextStructured with
	// CollectFontInformation). Carried into page.TextCell.
	FontName   string
	FontWeight int
	FontFlags  int
	Color      uint32
}

func TextRectsToCells(rects []TextRect, pageHeight float64) []page.TextCell {
	cells := make([]page.TextCell, 0, len(rects))
	for _, rect := range rects {
		text := strings.TrimSpace(rect.Text)
		if text == "" {
			continue
		}
		if trimZeroWidthAndSpace(text) == "" {
			continue
		}
		if isDegenerateFormatArtifact(rect, text) {
			continue
		}
		cells = append(cells, page.TextCell{
			Index:      len(cells),
			Text:       text,
			FontSize:   rect.FontSize,
			FontName:   rect.FontName,
			FontWeight: rect.FontWeight,
			FontFlags:  rect.FontFlags,
			Color:      rect.Color,
			Box: geom.Box{
				L:      rect.Left,
				T:      pageHeight - rect.Top,
				R:      rect.Right,
				B:      pageHeight - rect.Bottom,
				Origin: geom.TopLeft,
			},
		})
	}
	return cells
}

// Degenerate format-marked runs are extraction artefacts, not visible glyphs.
func isDegenerateFormatArtifact(rect TextRect, text string) bool {
	if rect.FontSize <= 0 || !containsZeroWidthFormatRune(text) {
		return false
	}
	if trimZeroWidthAndSpace(text) == "" {
		return false
	}

	height := math.Abs(rect.Top - rect.Bottom)
	maxArtifactHeight := math.Max(0.05, rect.FontSize*0.02)
	return height <= maxArtifactHeight
}

func containsZeroWidthFormatRune(text string) bool {
	for _, r := range text {
		if isZeroWidthFormatRune(r) {
			return true
		}
	}
	return false
}

func trimZeroWidthAndSpace(text string) string {
	return strings.TrimFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || isZeroWidthFormatRune(r)
	})
}

func isZeroWidthFormatRune(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
		return true
	default:
		return false
	}
}

func TopLeftBoxToPDFiumBounds(box geom.Box, pageHeight float64) TextRect {
	converted := box
	if converted.Origin != geom.BottomLeft {
		converted = box.WithOrigin(geom.BottomLeft, pageHeight)
	}
	return TextRect{
		Left:   converted.L,
		Top:    converted.T,
		Right:  converted.R,
		Bottom: converted.B,
	}
}
