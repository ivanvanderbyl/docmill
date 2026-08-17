package pdf

import (
	"math"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// MergeOptions tunes fragmented-cell merging. Zero values use the same defaults
// as Docling's pypdfium2 backend.
type MergeOptions struct {
	// HorizontalThresholdFactor merges two cells on the same row when the gap
	// between them is at most this factor times their average height (default 1.0).
	HorizontalThresholdFactor float64
	// VerticalThresholdFactor groups cells onto the same row when their top and
	// bottom edges are within this factor times the row height (default 0.5).
	VerticalThresholdFactor float64
}

func (o MergeOptions) withDefaults() MergeOptions {
	if o.HorizontalThresholdFactor <= 0 {
		o.HorizontalThresholdFactor = 1.0
	}
	if o.VerticalThresholdFactor <= 0 {
		o.VerticalThresholdFactor = 0.5
	}
	return o
}

// MergeFragmentedCells de-fragments raw PDF text cells into line-level cells,
// mirroring Docling's merge_horizontal_cells (pypdfium2 backend). PDFium emits
// many sub-word rects in some documents; this groups them into rows by vertical
// proximity and merges horizontally-adjacent rects within a row into a single
// cell.
//
// Cells are processed in their incoming (reading) order. reextract, if non-nil,
// is called once with every merged cell's box (top-left origin) and returns one
// text per box, in order — pass the backend's bounded-text lookup so spacing
// matches the document, exactly as Docling re-reads the merged region. The
// single batched call keeps the re-read cost independent of how many groups a
// fragmented page produces. When reextract is nil or returns empty for a box,
// that group's member texts are joined with a single space. Returned cells are
// re-indexed 0..n-1 in order.
func MergeFragmentedCells(cells []page.TextCell, reextract func([]geom.Box) []string, options MergeOptions) []page.TextCell {
	options = options.withDefaults()
	if len(cells) == 0 {
		return nil
	}

	groups := make([][]page.TextCell, 0, len(cells))
	for _, row := range groupCellRows(cells, options.VerticalThresholdFactor) {
		groups = append(groups, splitRowGroups(row, options.HorizontalThresholdFactor)...)
	}

	merged := make([]page.TextCell, len(groups))
	multi := make([]int, 0, len(groups))
	for i, group := range groups {
		merged[i] = mergeCellGroup(group)
		if len(group) > 1 {
			multi = append(multi, i)
		}
	}

	if reextract != nil && len(multi) > 0 {
		boxes := make([]geom.Box, len(multi))
		for i, index := range multi {
			boxes[i] = merged[index].Box
		}
		texts := reextract(boxes)
		for i, index := range multi {
			if i >= len(texts) {
				break
			}
			if text := strings.TrimSpace(texts[i]); text != "" {
				merged[index].Text = text
			}
		}
	}

	for index := range merged {
		merged[index].Index = index
	}
	return merged
}

func groupCellRows(cells []page.TextCell, verticalFactor float64) [][]page.TextCell {
	rows := make([][]page.TextCell, 0)
	current := []page.TextCell{cells[0]}
	rowTop := cells[0].Box.T
	rowBottom := cells[0].Box.B

	for _, cell := range cells[1:] {
		rowHeight := rowBottom - rowTop
		threshold := rowHeight * verticalFactor
		if math.Abs(cell.Box.T-rowTop) <= threshold && math.Abs(cell.Box.B-rowBottom) <= threshold {
			current = append(current, cell)
			rowTop = math.Min(rowTop, cell.Box.T)
			rowBottom = math.Max(rowBottom, cell.Box.B)
			continue
		}
		rows = append(rows, current)
		current = []page.TextCell{cell}
		rowTop = cell.Box.T
		rowBottom = cell.Box.B
	}
	rows = append(rows, current)
	return rows
}

// splitRowGroups splits one row into runs of horizontally-adjacent cells.
func splitRowGroups(row []page.TextCell, horizontalFactor float64) [][]page.TextCell {
	groups := make([][]page.TextCell, 0, len(row))
	group := []page.TextCell{row[0]}

	for _, cell := range row[1:] {
		prev := group[len(group)-1]
		avgHeight := (prev.Box.Height() + cell.Box.Height()) / 2
		if cell.Box.L-prev.Box.R <= avgHeight*horizontalFactor {
			group = append(group, cell)
			continue
		}
		groups = append(groups, group)
		group = []page.TextCell{cell}
	}
	return append(groups, group)
}

func mergeCellGroup(group []page.TextCell) page.TextCell {
	if len(group) == 1 {
		return group[0]
	}

	box := group[0].Box
	fontSize := group[0].FontSize
	// Font info: take the first cell's font as the representative. Cells in a
	// merge group are sub-word fragments on the same baseline; they share a
	// font in virtually all born-digital PDFs. If a group spans a font
	// transition (rare), the first fragment's font wins — the LineElement
	// run-splitter will re-segment by font downstream when it sees the
	// merged cell, but that requires per-word granularity which the merge
	// step intentionally collapses. Acceptable: inline formatting within a
	// single merged cell is uncommon.
	fontName := group[0].FontName
	fontWeight := group[0].FontWeight
	fontFlags := group[0].FontFlags
	color := group[0].Color
	for _, cell := range group[1:] {
		box.L = math.Min(box.L, cell.Box.L)
		box.T = math.Min(box.T, cell.Box.T)
		box.R = math.Max(box.R, cell.Box.R)
		box.B = math.Max(box.B, cell.Box.B)
		fontSize = math.Max(fontSize, cell.FontSize)
	}
	box.Origin = geom.TopLeft

	// Join the member texts as the fallback; the caller overwrites this with
	// the batched reextract result when it yields text for this box.
	parts := make([]string, 0, len(group))
	for _, cell := range group {
		if trimmed := strings.TrimSpace(cell.Text); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	text := strings.Join(parts, " ")

	return page.TextCell{
		Index:      group[0].Index,
		Text:       text,
		FontSize:   fontSize,
		FontName:   fontName,
		FontWeight: fontWeight,
		FontFlags:  fontFlags,
		Color:      color,
		Box:        box,
	}
}
