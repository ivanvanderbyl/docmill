// Package page defines the positioned text model shared across the extraction
// pipeline: TextCell (a glyph run with its bounding box and font attributes) and
// segmented-page queries over collections of cells.
package page

import (
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
)

type TextCell struct {
	Index    int
	Text     string
	FontSize float64
	Box      geom.Box

	// Font formatting, surfaced from the PDFium backend
	// (GetPageTextStructured with CollectFontInformation). Carried
	// through to TextLineWord / LineElement so the line-element pipeline
	// can split runs by font and the markdown writer can emit inline
	// formatting (bold/italic/code) through the LineElement model.
	FontName   string // /BaseFont name, e.g. "ArialMT-Bold"
	FontWeight int    // OpenType weight; >= 700 is bold
	FontFlags  int    // PDF spec §5.7.1 font descriptor flags (bit 6 value 64 = Italic)
	Color      uint32 // text color (ARGB); 0 when unavailable — TODO: content-stream extraction
}

type FormField struct {
	Name  string
	Value string
	Box   geom.Box
}

type RulingSegment struct {
	FromX  float64
	FromY  float64
	ToX    float64
	ToY    float64
	Width  float64
	Origin geom.CoordOrigin
}

type SegmentedPage struct {
	TextlineCells []TextCell
}

// CellsInBox returns the text cells whose own area overlaps box above the given
// intersection-over-self ratio, mirroring docling-core get_cells_in_bbox
// (docling_core/types/doc/page.py, default ios=0.8, strict ">"). It is a core
// primitive for the PDF pipeline; production table-cell assignment currently
// uses table.WithAssignedText, so this method is exercised only by tests today.
func (p SegmentedPage) CellsInBox(box geom.Box, intersectionOverSelf float64, pageHeight float64) []TextCell {
	cells := make([]TextCell, 0, len(p.TextlineCells))
	for _, cell := range p.TextlineCells {
		candidate := cell
		if candidate.Box.Origin != box.Origin {
			candidate.Box = candidate.Box.WithOrigin(box.Origin, pageHeight)
		}
		if candidate.Box.IntersectionOverSelf(box) > intersectionOverSelf {
			cells = append(cells, candidate)
		}
	}

	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Index < cells[j].Index
	})
	return cells
}

// Font flag bits per PDF spec §5.7.1 (Font Descriptor Flags).
const (
	FontFlagFixedPitch  = 1 << 0 // bit 1
	FontFlagSerif       = 1 << 1 // bit 2
	FontFlagSymbolic    = 1 << 2 // bit 3
	FontFlagScript      = 1 << 3 // bit 4
	FontFlagNonsymbolic = 1 << 5 // bit 6
	FontFlagItalic      = 1 << 6 // bit 7
)

// IsItalic returns true when the font is italic. The PDF font-descriptor
// Italic flag (bit 7) is the faithful source in the pure-Go port (populated
// from /FontDescriptor /Flags); the font name is checked as a fallback for
// the rare font whose descriptor omits the flag. If Flags is a sane PDF-spec
// value it takes precedence.
func (c TextCell) IsItalic() bool {
	// Italic if the PDF font-descriptor Italic flag is set (when flags are a
	// sane non-zero value), OR the font name indicates italic/oblique.
	if c.FontFlags != 0 && isSaneFontFlags(c.FontFlags) && c.FontFlags&FontFlagItalic != 0 {
		return true
	}
	return nameIndicatesItalic(c.FontName)
}

// IsBold returns true when the font weight is ≥ 700 (OpenType bold threshold)
// or the font name indicates a bold/black/heavy weight. FontWeight is
// meaningful in the pure-Go port: font.Font.FontWeight() derives it from the
// descriptor ForceBold flag, the embedded Type1 /FontInfo /Weight, and /StemV
// before falling back to the name, so weight-only-bold fonts (e.g. CMBX10)
// are detected even when their name carries no weight token. Weight is trusted
// only when it is a sane OpenType value (1–1000); the name remains a fallback.
func (c TextCell) IsBold() bool {
	if isSaneWeight(c.FontWeight) && c.FontWeight >= 700 {
		return true
	}
	return nameIndicatesBold(c.FontName)
}

// IsMonospace returns true when the font is fixed-pitch. Used to detect
// inline code spans.
func (c TextCell) IsMonospace() bool { return c.FontFlags&FontFlagFixedPitch != 0 }

func nameIndicatesBold(name string) bool {
	if name == "" {
		return false
	}
	// Match the PostScript weight tokens that appear in /BaseFont names.
	for _, token := range []string{"-Bold", "-Black", "-Heavy", "-Demi", "-SemiBold", "Bold", "Black"} {
		if containsFold(name, token) {
			return true
		}
	}
	return false
}

func nameIndicatesItalic(name string) bool {
	if name == "" {
		return false
	}
	return containsFold(name, "Italic") || containsFold(name, "Oblique")
}

// isSaneWeight reports whether a FontWeight value is a plausible OpenType
// weight (1–1000). The pure-Go port returns 0 when no weight signal resolves;
// values outside the OpenType range would be corrupt and must not drive bold
// detection.
func isSaneWeight(w int) bool { return w >= 1 && w <= 1000 }

// isSaneFontFlags reports whether FontFlags is a plausible PDF-spec font
// descriptor flag set (below bit 19). Values outside that range are corrupt
// and must not drive italic detection.
func isSaneFontFlags(f int) bool { return f >= 0 && f < (1<<18) }

func containsFold(s, sub string) bool {
	// case-insensitive substring without pulling in strings (page pkg avoids it).
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
