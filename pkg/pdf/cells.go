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
	// ExclusiveReextract marks reextract as CONSUMING: every group — including
	// single-cell groups — is re-read through it in processing order, an empty
	// result is authoritative (that region's characters were already claimed by
	// an earlier cell, so falling back to the member texts would duplicate
	// them), and cells left with no text are dropped. Rect queries alone cannot
	// give this guarantee: a glyph whose box straddles two regions (big-operator
	// limits, sub/superscripts crossing line rects) is otherwise emitted into
	// both.
	ExclusiveReextract bool
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
// returns the text for a merged cell's box (top-left origin) — pass the backend's
// bounded-text lookup so spacing matches the document, exactly as Docling
// re-reads the merged region. When reextract is nil or returns empty, the member
// cells' texts are joined with a single space. Returned cells are re-indexed
// 0..n-1 in order.
func MergeFragmentedCells(cells []page.TextCell, reextract func(geom.Box) string, options MergeOptions) []page.TextCell {
	options = options.withDefaults()
	if len(cells) == 0 {
		return nil
	}

	merged := make([]page.TextCell, 0, len(cells))
	for _, row := range groupCellRows(cells, options.VerticalThresholdFactor) {
		merged = append(merged, mergeCellRow(row, options.HorizontalThresholdFactor, reextract, options.ExclusiveReextract)...)
	}
	if options.ExclusiveReextract && reextract != nil {
		kept := merged[:0]
		for _, cell := range merged {
			if strings.TrimSpace(cell.Text) == "" {
				continue
			}
			kept = append(kept, cell)
		}
		merged = kept
	}
	for index := range merged {
		merged[index].Index = index
	}
	return merged
}

// MergeFragmentedCellsExclusive merges exactly like MergeFragmentedCells with
// ExclusiveReextract, but re-reads ALL merged groups in one batched call:
// reextractAll receives every group's enclosing box (top-left origin, in
// processing order) and returns one text per box. Handing the extractor the
// complete set of boxes lets it partition the page's characters by best
// overlap instead of first-query-wins — a tall math delimiter's box that
// merely grazes glyphs of a neighbouring prose line no longer steals them
// from that line's own query. The extractor stays authoritative: a group
// whose text comes back empty was claimed by better-overlapping groups and is
// dropped. Returned cells are re-indexed 0..n-1.
func MergeFragmentedCellsExclusive(cells []page.TextCell, reextractAll func([]geom.Box) []string, options MergeOptions) []page.TextCell {
	options = options.withDefaults()
	if len(cells) == 0 {
		return nil
	}

	var groups [][]page.TextCell
	for _, row := range groupCellRows(cells, options.VerticalThresholdFactor) {
		groups = append(groups, splitCellRowGroups(row, options.HorizontalThresholdFactor)...)
	}

	shells := make([]page.TextCell, len(groups))
	boxes := make([]geom.Box, len(groups))
	for i, group := range groups {
		shells[i] = mergeCellShell(group)
		boxes[i] = shells[i].Box
	}

	texts := reextractAll(boxes)
	merged := make([]page.TextCell, 0, len(groups))
	for i, shell := range shells {
		text := ""
		if i < len(texts) {
			text = strings.TrimSpace(texts[i])
		}
		if text == "" {
			continue
		}
		shell.Text = text
		merged = append(merged, shell)
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

func mergeCellRow(row []page.TextCell, horizontalFactor float64, reextract func(geom.Box) string, exclusive bool) []page.TextCell {
	groups := splitCellRowGroups(row, horizontalFactor)
	merged := make([]page.TextCell, 0, len(groups))
	for _, group := range groups {
		merged = append(merged, mergeCellGroup(group, reextract, exclusive))
	}
	return merged
}

// splitCellRowGroups splits a vertical row into horizontally-contiguous merge
// groups: adjacent cells stay in one group while the gap between them is at
// most horizontalFactor times their average height.
func splitCellRowGroups(row []page.TextCell, horizontalFactor float64) [][]page.TextCell {
	groups := make([][]page.TextCell, 0, 1)
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

func mergeCellGroup(group []page.TextCell, reextract func(geom.Box) string, exclusive bool) page.TextCell {
	if len(group) == 1 {
		if exclusive && reextract != nil {
			cell := group[0]
			cell.Text = strings.TrimSpace(reextract(cell.Box))
			return cell
		}
		return group[0]
	}

	merged := mergeCellShell(group)

	text := ""
	if reextract != nil {
		text = strings.TrimSpace(reextract(merged.Box))
	}
	if text == "" && !(exclusive && reextract != nil) {
		parts := make([]string, 0, len(group))
		for _, cell := range group {
			if trimmed := strings.TrimSpace(cell.Text); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		text = strings.Join(parts, " ")
	}

	merged.Text = text
	return merged
}

// mergeCellShell unions a group's geometry and font metadata into one cell,
// leaving Text as the first member's (callers overwrite it).
func mergeCellShell(group []page.TextCell) page.TextCell {
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

	return page.TextCell{
		Index:      group[0].Index,
		Text:       group[0].Text,
		FontSize:   fontSize,
		FontName:   fontName,
		FontWeight: fontWeight,
		FontFlags:  fontFlags,
		Color:      color,
		Box:        box,
	}
}
