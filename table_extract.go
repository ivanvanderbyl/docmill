package docmill

import (
	"math"
	"sort"
)

// mergeEdges snaps and joins edges that are close together.
func mergeEdges(edges []Edge, settings TableSettings) []Edge {
	// Snap edges that are close together
	if settings.SnapXTolerance > 0 || settings.SnapYTolerance > 0 {
		edges = snapEdges(edges, settings.SnapXTolerance, settings.SnapYTolerance)
	}

	// Group edges by orientation and position, then join within each group
	type edgeGroup struct {
		orientation string
		position    float64
	}

	grouped := make(map[edgeGroup][]Edge)
	for _, edge := range edges {
		key := edgeGroup{
			orientation: edge.Orientation,
		}
		if edge.Orientation == "h" {
			key.position = edge.Top
		} else {
			key.position = edge.X0
		}
		grouped[key] = append(grouped[key], edge)
	}

	// Join edges in each group
	var result []Edge
	for key, group := range grouped {
		joined := joinEdgeGroup(group, key.orientation, settings)
		result = append(result, joined...)
	}

	return result
}

// snapEdges snaps edges that are within tolerance to their average position.
func snapEdges(edges []Edge, xTol, yTol float64) []Edge {
	vEdges := []Edge{}
	hEdges := []Edge{}

	for _, e := range edges {
		if e.Orientation == "v" {
			vEdges = append(vEdges, e)
		} else {
			hEdges = append(hEdges, e)
		}
	}

	// Snap vertical edges by x0
	snappedV := snapObjects(vEdges, "x0", xTol)
	// Snap horizontal edges by top
	snappedH := snapObjects(hEdges, "top", yTol)

	return append(snappedV, snappedH...)
}

// snapObjects snaps objects along a dimension within tolerance.
func snapObjects(edges []Edge, dimension string, tolerance float64) []Edge {
	if len(edges) == 0 {
		return edges
	}

	getValue := func(e Edge) float64 {
		if dimension == "x0" {
			return e.X0
		}
		return e.Top
	}

	// Group edges within tolerance
	type cluster struct {
		value float64
		edges []int // indices
	}

	var clusters []cluster
	for i, edge := range edges {
		val := getValue(edge)
		found := false
		for j := range clusters {
			if math.Abs(clusters[j].value-val) <= tolerance {
				clusters[j].edges = append(clusters[j].edges, i)
				// Update cluster average
				sum := clusters[j].value * float64(len(clusters[j].edges)-1)
				clusters[j].value = (sum + val) / float64(len(clusters[j].edges))
				found = true
				break
			}
		}
		if !found {
			clusters = append(clusters, cluster{value: val, edges: []int{i}})
		}
	}

	// Create snapped edges
	result := make([]Edge, len(edges))
	copy(result, edges)

	for _, c := range clusters {
		for _, idx := range c.edges {
			if dimension == "x0" {
				diff := c.value - result[idx].X0
				result[idx].X0 = c.value
				result[idx].X1 += diff
			} else {
				diff := c.value - result[idx].Top
				result[idx].Top = c.value
				result[idx].Bottom += diff
			}
		}
	}

	return result
}

// joinEdgeGroup joins edges on the same line that are within tolerance.
func joinEdgeGroup(edges []Edge, orientation string, settings TableSettings) []Edge {
	if len(edges) == 0 {
		return edges
	}

	tolerance := settings.JoinXTolerance
	var minProp, maxProp string
	if orientation == "h" {
		minProp, maxProp = "x0", "x1"
		tolerance = settings.JoinXTolerance
	} else {
		minProp, maxProp = "top", "bottom"
		tolerance = settings.JoinYTolerance
	}

	getMin := func(e Edge) float64 {
		if minProp == "x0" {
			return e.X0
		}
		return e.Top
	}
	getMax := func(e Edge) float64 {
		if maxProp == "x1" {
			return e.X1
		}
		return e.Bottom
	}

	// Sort by min property
	sort.Slice(edges, func(i, j int) bool {
		return getMin(edges[i]) < getMin(edges[j])
	})

	joined := []Edge{edges[0]}
	for i := 1; i < len(edges); i++ {
		last := &joined[len(joined)-1]
		current := edges[i]

		if getMin(current) <= getMax(*last)+tolerance {
			// Extend the last edge if current extends beyond it
			if getMax(current) > getMax(*last) {
				if orientation == "h" {
					last.X1 = current.X1
					last.Width = last.X1 - last.X0
				} else {
					last.Bottom = current.Bottom
					last.Height = last.Bottom - last.Top
				}
			}
		} else {
			// Add as separate edge
			joined = append(joined, current)
		}
	}

	return joined
}

// filterEdgesByLength filters edges by minimum length.
func filterEdgesByLength(edges []Edge, minLength float64) []Edge {
	if minLength <= 0 {
		return edges
	}

	result := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		length := edge.Width
		if edge.Orientation == "v" {
			length = edge.Height
		}
		if length >= minLength {
			result = append(result, edge)
		}
	}
	return result
}

// findIntersections finds where vertical and horizontal edges intersect.
func findIntersections(edges []Edge, settings TableSettings) map[Point]map[string][]Edge {
	intersections := make(map[Point]map[string][]Edge)

	var vEdges, hEdges []Edge
	for _, e := range edges {
		if e.Orientation == "v" {
			vEdges = append(vEdges, e)
		} else {
			hEdges = append(hEdges, e)
		}
	}

	xTol := settings.IntersectionXTolerance
	yTol := settings.IntersectionYTolerance

	for _, v := range vEdges {
		for _, h := range hEdges {
			// Check if edges intersect
			if (v.Top <= h.Top+yTol) &&
				(v.Bottom >= h.Top-yTol) &&
				(v.X0 >= h.X0-xTol) &&
				(v.X0 <= h.X1+xTol) {

				point := Point{X: v.X0, Y: h.Top}
				if _, ok := intersections[point]; !ok {
					intersections[point] = map[string][]Edge{"v": {}, "h": {}}
				}
				intersections[point]["v"] = append(intersections[point]["v"], v)
				intersections[point]["h"] = append(intersections[point]["h"], h)
			}
		}
	}

	return intersections
}

// intersectionsToCells creates rectangular cells from intersections.
func intersectionsToCells(intersections map[Point]map[string][]Edge) []CellBBox {
	if len(intersections) == 0 {
		return nil
	}

	// Convert to sorted list of points
	points := make([]Point, 0, len(intersections))
	for p := range intersections {
		points = append(points, p)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Y == points[j].Y {
			return points[i].X < points[j].X
		}
		return points[i].Y < points[j].Y
	})

	// Helper to check if two points are connected by an edge
	edgeConnects := func(p1, p2 Point) bool {
		if p1.X == p2.X {
			// Check vertical connection
			edges1 := intersections[p1]["v"]
			edges2 := intersections[p2]["v"]
			for _, e1 := range edges1 {
				for _, e2 := range edges2 {
					// Same edge connects both points
					if e1.X0 == e2.X0 && e1.Top == e2.Top && e1.Bottom == e2.Bottom {
						return true
					}
				}
			}
		}
		if p1.Y == p2.Y {
			// Check horizontal connection
			edges1 := intersections[p1]["h"]
			edges2 := intersections[p2]["h"]
			for _, e1 := range edges1 {
				for _, e2 := range edges2 {
					// Same edge connects both points
					if e1.Top == e2.Top && e1.X0 == e2.X0 && e1.X1 == e2.X1 {
						return true
					}
				}
			}
		}
		return false
	}

	// Find all rectangular cells
	var cells []CellBBox
	for i, pt := range points {
		// Find the nearest point to the right and below (minimal cells only)
		var nearestRight, nearestBelow *Point

		for j := i + 1; j < len(points); j++ {
			// Find nearest point directly below
			if points[j].X == pt.X && points[j].Y > pt.Y {
				if nearestBelow == nil || points[j].Y < nearestBelow.Y {
					nearestBelow = &points[j]
				}
			}
			// Find nearest point directly to the right
			if points[j].Y == pt.Y && points[j].X > pt.X {
				if nearestRight == nil || points[j].X < nearestRight.X {
					nearestRight = &points[j]
				}
			}
		}

		// Only try to form a rectangle with the nearest neighbors
		if nearestBelow != nil && nearestRight != nil &&
			edgeConnects(pt, *nearestBelow) && edgeConnects(pt, *nearestRight) {

			// Check if bottom-right corner exists
			bottomRight := Point{X: nearestRight.X, Y: nearestBelow.Y}
			if _, exists := intersections[bottomRight]; exists {
				// Check if all four sides are connected
				if edgeConnects(bottomRight, *nearestRight) && edgeConnects(bottomRight, *nearestBelow) {
					cells = append(cells, CellBBox{
						X0:     pt.X,
						Top:    pt.Y,
						X1:     bottomRight.X,
						Bottom: bottomRight.Y,
					})
				}
			}
		}
	}

	return cells
}

// cellsToTables groups cells into contiguous tables.
func cellsToTables(cells []CellBBox) [][]CellBBox {
	if len(cells) == 0 {
		return nil
	}

	remaining := make([]CellBBox, len(cells))
	copy(remaining, cells)

	var tables [][]CellBBox
	var currentTable []CellBBox
	currentCorners := make(map[Point]bool)

	for len(remaining) > 0 {
		initialSize := len(currentTable)

		// Try to add cells to current table
		for i := 0; i < len(remaining); i++ {
			cell := remaining[i]
			corners := []Point{
				{cell.X0, cell.Top},
				{cell.X0, cell.Bottom},
				{cell.X1, cell.Top},
				{cell.X1, cell.Bottom},
			}

			// Starting a new table
			if len(currentTable) == 0 {
				currentTable = append(currentTable, cell)
				for _, c := range corners {
					currentCorners[c] = true
				}
				remaining = append(remaining[:i], remaining[i+1:]...)
				i--
				continue
			}

			// Check if cell shares corners with current table
			shareCount := 0
			for _, c := range corners {
				if currentCorners[c] {
					shareCount++
				}
			}

			if shareCount > 0 {
				currentTable = append(currentTable, cell)
				for _, c := range corners {
					currentCorners[c] = true
				}
				remaining = append(remaining[:i], remaining[i+1:]...)
				i--
			}
		}

		// If no cells added, start new table
		if len(currentTable) == initialSize {
			if len(currentTable) > 1 {
				tables = append(tables, currentTable)
			}
			currentTable = nil
			currentCorners = make(map[Point]bool)
		}
	}

	// Add final table
	if len(currentTable) > 1 {
		tables = append(tables, currentTable)
	}

	return tables
}

// createTable creates a Table structure from cells and extracts content.
func createTable(page *Page, cells []CellBBox, words []EnrichedWord) Table {
	if len(cells) == 0 {
		return Table{}
	}

	// Calculate table bounding box
	bbox := CellBBox{
		X0:     math.MaxFloat64,
		Top:    math.MaxFloat64,
		X1:     -math.MaxFloat64,
		Bottom: -math.MaxFloat64,
	}
	for _, cell := range cells {
		if cell.X0 < bbox.X0 {
			bbox.X0 = cell.X0
		}
		if cell.Top < bbox.Top {
			bbox.Top = cell.Top
		}
		if cell.X1 > bbox.X1 {
			bbox.X1 = cell.X1
		}
		if cell.Bottom > bbox.Bottom {
			bbox.Bottom = cell.Bottom
		}
	}

	// Organize cells into rows by their Top position
	type rowGroup struct {
		top   float64
		cells []CellBBox
	}

	var rows []rowGroup
	for _, cell := range cells {
		found := false
		for i := range rows {
			// Cells are in the same row if tops are within 1 pixel
			if math.Abs(rows[i].top-cell.Top) < 1.0 {
				rows[i].cells = append(rows[i].cells, cell)
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, rowGroup{top: cell.Top, cells: []CellBBox{cell}})
		}
	}

	// Sort rows by top position
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].top < rows[j].top
	})

	// For each row, sort cells by X0 position
	for i := range rows {
		sort.Slice(rows[i].cells, func(j, k int) bool {
			return rows[i].cells[j].X0 < rows[i].cells[k].X0
		})
	}

	// Extract content for each cell
	tableRows := make([]TableRow, 0, len(rows))
	maxCols := 0

	for _, row := range rows {
		tableCells := make([]TableCell, 0, len(row.cells))

		for _, cellBBox := range row.cells {
			// Find words within this cell (with small tolerance for boundary)
			const tolerance = 1.0
			cellWords := []EnrichedWord{}
			for _, word := range words {
				// Check if word center is inside cell
				wordCenterX := (word.Box.X0 + word.Box.X1) / 2
				wordCenterY := (word.Box.Y0 + word.Box.Y1) / 2

				if wordCenterX >= cellBBox.X0-tolerance &&
					wordCenterX <= cellBBox.X1+tolerance &&
					wordCenterY >= cellBBox.Top-tolerance &&
					wordCenterY <= cellBBox.Bottom+tolerance {
					cellWords = append(cellWords, word)
				}
			}

			// Sort words by position (top to bottom, left to right)
			sort.Slice(cellWords, func(i, j int) bool {
				if math.Abs(cellWords[i].Box.Y0-cellWords[j].Box.Y0) < 2.0 {
					return cellWords[i].Box.X0 < cellWords[j].Box.X0
				}
				return cellWords[i].Box.Y0 < cellWords[j].Box.Y0
			})

			// Build cell content
			content := ""
			for i, word := range cellWords {
				if i > 0 {
					prevWord := cellWords[i-1]
					// Check if this is a new line (vertical gap)
					if word.Box.Y0-prevWord.Box.Y1 > 2.0 {
						content += "\n"
					} else {
						content += " "
					}
				}
				content += word.Text
			}

			tableCells = append(tableCells, TableCell{
				BBox:    cellBBox,
				Content: content,
				Words:   cellWords,
			})
		}

		if len(tableCells) > maxCols {
			maxCols = len(tableCells)
		}

		// Calculate row bounding box
		rowBBox := CellBBox{
			X0:     row.cells[0].X0,
			Top:    row.top,
			X1:     row.cells[len(row.cells)-1].X1,
			Bottom: row.cells[0].Bottom,
		}

		tableRows = append(tableRows, TableRow{
			Cells: tableCells,
			BBox:  rowBBox,
		})
	}

	// Filter out empty rows (rows where all cells are empty)
	nonEmptyRows := make([]TableRow, 0, len(tableRows))
	for _, row := range tableRows {
		hasContent := false
		for _, cell := range row.Cells {
			if len(cell.Content) > 0 {
				hasContent = true
				break
			}
		}
		if hasContent {
			nonEmptyRows = append(nonEmptyRows, row)
		}
	}

	return Table{
		BBox:    bbox,
		Rows:    nonEmptyRows,
		Cells:   cells,
		NumRows: len(nonEmptyRows),
		NumCols: maxCols,
	}
}
