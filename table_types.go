package docmill

// Edge represents a horizontal or vertical line segment used for table detection.
// Based on pdfplumber's edge structure.
type Edge struct {
	X0          float64 // Left x coordinate
	X1          float64 // Right x coordinate
	Top         float64 // Top y coordinate
	Bottom      float64 // Bottom y coordinate
	Width       float64 // Width (for horizontal edges)
	Height      float64 // Height (for vertical edges)
	Orientation string  // "h" for horizontal, "v" for vertical
}

// Point represents an (x, y) coordinate where edges intersect.
type Point struct {
	X float64
	Y float64
}

// CellBBox represents a table cell as a bounding box.
type CellBBox struct {
	X0     float64
	Top    float64
	X1     float64
	Bottom float64
}

// TableCell represents a detected table cell with its content.
type TableCell struct {
	BBox    CellBBox
	Content string
	Words   []EnrichedWord
}

// TableRow represents a row of cells in a table.
type TableRow struct {
	Cells []TableCell
	BBox  CellBBox
}

// Table represents a detected table with its structure and content.
type Table struct {
	BBox    CellBBox
	Rows    []TableRow
	Cells   []CellBBox // Raw cell bounding boxes
	NumRows int
	NumCols int
}

// TableSettings configures table detection behavior.
// Based on pdfplumber's TableSettings.
type TableSettings struct {
	// Strategy for detecting table edges: "text", "lines", "lines_strict", "explicit"
	VerticalStrategy   string
	HorizontalStrategy string

	// Tolerances for snapping close edges together
	SnapTolerance  float64
	SnapXTolerance float64
	SnapYTolerance float64

	// Tolerances for joining edges on the same line
	JoinTolerance  float64
	JoinXTolerance float64
	JoinYTolerance float64

	// Minimum edge length to consider
	EdgeMinLength float64

	// Minimum number of words required to infer edges from text alignment
	MinWordsVertical   int
	MinWordsHorizontal int

	// Tolerances for finding edge intersections
	IntersectionTolerance  float64
	IntersectionXTolerance float64
	IntersectionYTolerance float64
}

// DefaultTableSettings returns default settings for table detection.
// Uses "lines" strategy by default to detect explicit line objects in PDFs.
func DefaultTableSettings() TableSettings {
	return TableSettings{
		VerticalStrategy:       "lines",
		HorizontalStrategy:     "lines",
		SnapTolerance:          3.0,
		SnapXTolerance:         3.0,
		SnapYTolerance:         3.0,
		JoinTolerance:          3.0,
		JoinXTolerance:         3.0,
		JoinYTolerance:         3.0,
		EdgeMinLength:          3.0,
		MinWordsVertical:       3,
		MinWordsHorizontal:     1,
		IntersectionTolerance:  3.0,
		IntersectionXTolerance: 3.0,
		IntersectionYTolerance: 3.0,
	}
}
