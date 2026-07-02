// Package textline defines the shared visual-line model used across the docmill
// pipeline. It is a low-level package (depending only on pkg/page and pkg/geom)
// so both pkg/pdf and pkg/table can build on the same ParagraphTextLine type
// without introducing an import cycle.
//
// This package contains TYPES ONLY: the producer (AssembleLineElements), the
// baseline-clustering, and the text/run helpers live in pkg/pdf and are
// unchanged.
package textline

import (
	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
)

// ParagraphTextLine is a visual line — one PDF baseline's worth of words,
// produced before reflow or table gridding.
//
// Field set: BBox, FontBBox, Words[], Text, ReadingOrder, FontSize,
// WritingDirection, Orientation, Elements[] (inline runs).
type ParagraphTextLine struct {
	BBox             geom.Box
	FontBBox         geom.Box
	Words            []Word
	Text             string
	ReadingOrder     int
	FontSize         float64
	WritingDirection int
	Orientation      int
	Elements         []LineElement

	// Cells is the full cell set this visual line was built from (the
	// geometric source, not the filtered Words run model — Words drops spacers
	// and empties via lineWords). The paragraph/heading/structure clustering
	// pipeline reads Cells when it needs every source cell.
	Cells []page.TextCell

	// MinIndex, ListCandidate and ListContentL are paragraph-merge hints
	// computed by the clustering producer (smallest source cell index, list
	// marker candidacy and its content-left x) consumed by the paragraph
	// assembler and the list/structure detectors.
	MinIndex      int
	ListCandidate bool
	ListContentL  float64

	// Center is the average row centre, used by the table row model in a later
	// stage; unused by the paragraph/heading path.
	Center float64
}

// LineElement is an inline element inside a visual line: a run of words
// sharing formatting. The markdown writer walks these for inline formatting
// (bold/italic/code) and hyperlinks.
//
// Fields: BBox, Text, Bold, Italic, Underline, Strike, Superscript,
// Subscript, Color, Words[].
type LineElement struct {
	BBox        geom.Box
	Text        string
	Bold        bool
	Italic      bool
	Underline   bool
	Strike      bool
	Superscript bool
	Subscript   bool
	Color       uint32
	Words       []Word
}

// Word is a word on a line.
//
// Fields: Value, BBox, FontBBox, FontSize, FontName, Bold, Italic, Color,
// Confidence, SpecialFormat, SpecialFormatData, ParentTable, ReadingOrder,
// Source (back-ref to the extracting cell).
type Word struct {
	Value             string
	BBox              geom.Box
	FontBBox          geom.Box
	FontSize          float64
	FontName          string
	Bold              bool
	Italic            bool
	Color             uint32
	Confidence        float64
	SpecialFormat     int
	SpecialFormatData string
	ParentTable       bool
	ReadingOrder      int
	Source            page.TextCell
}
