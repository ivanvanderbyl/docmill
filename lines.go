package docmill

import (
	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// extractLinesFromPage extracts explicit line objects from a PDF page.
// This handles PDFs with actual line objects (not just text alignment).
// Filters out page borders to prevent entire pages from being treated as tables.
func extractLinesFromPage(instance pdfium.Pdfium, page references.FPDF_PAGE, pageWidth, pageHeight float64) ([]Edge, error) {
	// Get object count
	countResp, err := instance.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{
		Page: requests.Page{
			ByReference: &page,
		},
	})
	if err != nil {
		return nil, err
	}

	var edges []Edge

	// Iterate through all page objects
	for i := 0; i < countResp.Count; i++ {
		// Get the object
		objResp, err := instance.FPDFPage_GetObject(&requests.FPDFPage_GetObject{
			Page: requests.Page{
				ByReference: &page,
			},
			Index: i,
		})
		if err != nil {
			continue
		}

		// Check if it's a path object
		typeResp, err := instance.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{
			PageObject: objResp.PageObject,
		})
		if err != nil || typeResp.Type != enums.FPDF_PAGEOBJ_PATH {
			continue
		}

		// Get the path bounds
		boundsResp, err := instance.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{
			PageObject: objResp.PageObject,
		})
		if err != nil {
			continue
		}

		// Convert PDF coordinates (origin bottom-left) to standard (origin top-left)
		x0 := float64(boundsResp.Left)
		y0 := pageHeight - float64(boundsResp.Top)
		x1 := float64(boundsResp.Right)
		y1 := pageHeight - float64(boundsResp.Bottom)

		// Get path segments to determine if it's a line
		segCountResp, err := instance.FPDFPath_CountSegments(&requests.FPDFPath_CountSegments{
			PageObject: objResp.PageObject,
		})
		if err != nil {
			continue
		}

		// Check if it's a simple line (typically 2 segments: MOVETO and LINETO)
		// or a rectangle (5 segments: MOVETO and 4 LINETOs forming a closed path)
		if segCountResp.Count < 2 {
			continue
		}

		// For simple horizontal or vertical lines
		if segCountResp.Count == 2 {
			edge := pathToEdge(x0, y0, x1, y1)
			if edge != nil && !isPageBorder(*edge, pageWidth, pageHeight) {
				edges = append(edges, *edge)
			}
		} else if segCountResp.Count >= 4 {
			// For rectangles or complex paths, extract edges from the bounding box
			rectEdges := boundsToEdges(x0, y0, x1, y1)
			for _, edge := range rectEdges {
				if !isPageBorder(edge, pageWidth, pageHeight) {
					edges = append(edges, edge)
				}
			}
		}
	}

	return edges, nil
}

// isPageBorder checks if an edge is at the page boundary or is a full-page border.
// Returns true for lines that are page/content borders (should be filtered out).
func isPageBorder(edge Edge, pageWidth, pageHeight float64) bool {
	const borderTolerance = 20.0   // pixels from page edge
	const fullSpanThreshold = 0.90 // 90% of page dimension

	if edge.Orientation == "h" {
		// Horizontal line at top or bottom of page
		if edge.Top < borderTolerance || edge.Top > pageHeight-borderTolerance {
			return true
		}
		// Check if it spans most of the page width (likely a border)
		if edge.Width > pageWidth*fullSpanThreshold {
			return true
		}
	}

	if edge.Orientation == "v" {
		// Vertical line at left or right of page
		if edge.X0 < borderTolerance || edge.X0 > pageWidth-borderTolerance {
			return true
		}
		// Check if it spans most of the page height (likely a border)
		if edge.Height > pageHeight*fullSpanThreshold {
			return true
		}
	}

	return false
}

// pathToEdge converts a simple path to an edge if it's horizontal or vertical.
func pathToEdge(x0, y0, x1, y1 float64) *Edge {
	width := x1 - x0
	height := y1 - y0

	// Check if it's approximately horizontal (small height variation)
	if height < 2.0 && width > 1.0 {
		return &Edge{
			X0:          x0,
			X1:          x1,
			Top:         y0,
			Bottom:      y1,
			Width:       width,
			Height:      height,
			Orientation: "h",
		}
	}

	// Check if it's approximately vertical (small width variation)
	if width < 2.0 && height > 1.0 {
		return &Edge{
			X0:          x0,
			X1:          x1,
			Top:         y0,
			Bottom:      y1,
			Width:       width,
			Height:      height,
			Orientation: "v",
		}
	}

	return nil
}

// boundsToEdges converts a bounding box to four edges (for rectangles).
func boundsToEdges(x0, y0, x1, y1 float64) []Edge {
	return []Edge{
		// Top edge
		{
			X0:          x0,
			X1:          x1,
			Top:         y0,
			Bottom:      y0,
			Width:       x1 - x0,
			Orientation: "h",
		},
		// Bottom edge
		{
			X0:          x0,
			X1:          x1,
			Top:         y1,
			Bottom:      y1,
			Width:       x1 - x0,
			Orientation: "h",
		},
		// Left edge
		{
			X0:          x0,
			X1:          x0,
			Top:         y0,
			Bottom:      y1,
			Height:      y1 - y0,
			Orientation: "v",
		},
		// Right edge
		{
			X0:          x1,
			X1:          x1,
			Top:         y0,
			Bottom:      y1,
			Height:      y1 - y0,
			Orientation: "v",
		},
	}
}
