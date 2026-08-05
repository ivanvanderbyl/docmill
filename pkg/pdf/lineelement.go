package pdf

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
)

// The shared visual-line model now lives in pkg/textline (a low-level package
// depending only on pkg/page and pkg/geom) so pkg/table can build on the same
// type without an import cycle. pkg/pdf keeps these aliases so all existing
// unqualified code (the producer, clustering, heading/paragraph/structure
// detectors, and the markdown writer) continues to work unchanged.
type (
	ParagraphTextLine = textline.ParagraphTextLine
	TextLineWord      = textline.Word
	LineElement       = textline.LineElement
)

// LineElement pipeline — the shared visual-line model that the whole downstream
// pipeline uses (line assembly → paragraphs / heading detection / table-grid
// estimation / markdown writer).
//
// This file defines the node types and the single producer AssembleLineElements,
// which is the existing groupLines baseline-clustering promoted to emit
// ParagraphTextLine + LineElement runs.

// AssembleLineElements is the single producer of ParagraphTextLines from raw
// text cells.
//
// It clusters cells into visual baselines by vertical centre (matching
// groupLines' lineTolerance), orders each line's words left-to-right, and
// splits each line into LineElement runs by font transitions (bold/italic/
// size/name). The result is consumed by the table-region detector, the
// heading detector, and the paragraph assembler — one shared line model.
//
// lineTolerance is the maximum vertical-centre distance for two cells to be
// on the same baseline (ParagraphOptions.LineTolerance default = 4).
func AssembleLineElements(cells []page.TextCell, lineTolerance float64) []ParagraphTextLine {
	lines := clusterParagraphTextLines(cells, lineTolerance)
	for i := range lines {
		lines[i].ReadingOrder = i
	}
	return lines
}

// clusterParagraphTextLines clusters non-empty cells into horizontal visual
// lines by vertical centre (within lineTolerance), returning ParagraphTextLines
// top-to-bottom with their text joined left-to-right. Lines whose band was
// bridged by tall outlier glyphs are re-split into their distinct baselines
// (see splitTallBridgedGroup).
func clusterParagraphTextLines(cells []page.TextCell, lineTolerance float64) []ParagraphTextLine {
	visible := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		visible = append(visible, cell)
	}
	if len(visible) == 0 {
		return nil
	}

	var lines []ParagraphTextLine
	for _, group := range clusterCellsByCentre(visible, lineTolerance) {
		for _, split := range splitTallBridgedGroup(group, lineTolerance) {
			lines = append(lines, buildParagraphTextLine(split))
		}
	}

	return foldStackedFragmentLines(mergeInlineFragmentLines(lines))
}

// clusterCellsByCentre performs the visual-line walk: cells sorted by vertical
// centre accumulate into a line while sameVisualLine accepts them against the
// line's growing band. Returns the cell groups in top-to-bottom order.
func clusterCellsByCentre(cells []page.TextCell, lineTolerance float64) [][]page.TextCell {
	type sortableCell struct {
		cell   page.TextCell
		center float64
	}

	sortable := make([]sortableCell, 0, len(cells))
	for _, cell := range cells {
		sortable = append(sortable, sortableCell{
			cell:   cell,
			center: (cell.Box.T + cell.Box.B) / 2,
		})
	}
	if len(sortable) == 0 {
		return nil
	}

	sort.SliceStable(sortable, func(i, j int) bool {
		if sortable[i].center != sortable[j].center {
			return sortable[i].center < sortable[j].center
		}
		return sortable[i].cell.Box.L < sortable[j].cell.Box.L
	})

	var groups [][]page.TextCell
	var current []page.TextCell
	lineCenter := sortable[0].center
	lineTop := math.Min(sortable[0].cell.Box.T, sortable[0].cell.Box.B)
	lineBottom := math.Max(sortable[0].cell.Box.T, sortable[0].cell.Box.B)

	flush := func() {
		if len(current) == 0 {
			return
		}
		groups = append(groups, current)
		current = nil
	}

	for _, item := range sortable {
		if len(current) > 0 && !sameVisualLine(item.cell.Box, item.center, lineTop, lineBottom, lineCenter, lineTolerance) {
			flush()
			lineCenter = item.center
			lineTop = math.Min(item.cell.Box.T, item.cell.Box.B)
			lineBottom = math.Max(item.cell.Box.T, item.cell.Box.B)
		}
		if len(current) == 0 {
			lineCenter = item.center
			lineTop = math.Min(item.cell.Box.T, item.cell.Box.B)
			lineBottom = math.Max(item.cell.Box.T, item.cell.Box.B)
		}
		current = append(current, item.cell)
		cellTop := math.Min(item.cell.Box.T, item.cell.Box.B)
		cellBottom := math.Max(item.cell.Box.T, item.cell.Box.B)
		if cellTop < lineTop {
			lineTop = cellTop
		}
		if cellBottom > lineBottom {
			lineBottom = cellBottom
		}
		lineCenter = (lineTop + lineBottom) * 0.5
	}
	flush()

	return groups
}

// tallOutlierFactor classifies a cell as a tall outlier within a clustered
// line when its box is more than this factor times the line's median cell
// height. Big math delimiters, radicals and integral signs carry boxes 2-3x
// taller than their text band; legitimate same-line cells (sub/superscripts,
// footnote markers, mixed font sizes) stay well under it.
const tallOutlierFactor = 1.6

// splitTallBridgedGroup re-splits a clustered visual line whose vertical band
// was bridged by tall outlier glyphs. Big math delimiters reach through
// neighbouring lines, inflating the band until the walk's overlap fallback
// absorbs a second baseline; ordering the union left-to-right then interleaves
// equation glyphs with prose. The group's dominant-height cells are
// re-clustered on their own: if they form two or more distinct baselines the
// group splits, and each tall outlier joins the baseline cluster its box
// overlaps best. Full-cover ties resolve by top-edge proximity: a tall
// delimiter's box top starts at the band of the line it is anchored to and
// dangles down through whatever lies beneath, so of two fully-covered bands
// the one nearest the outlier's top is its anchor. A group whose dominant
// cells share one baseline is returned unchanged, so tall delimiters inside a
// single line (inline math) stay with their line.
//
// Invariant encoded: a visual text line's extent is defined by its dominant
// glyph band; oversized decoration must never fuse two distinct baselines
// into one line.
func splitTallBridgedGroup(group []page.TextCell, lineTolerance float64) [][]page.TextCell {
	if len(group) < 3 {
		return [][]page.TextCell{group}
	}

	heights := make([]float64, 0, len(group))
	for _, cell := range group {
		heights = append(heights, cell.Box.Height())
	}
	sort.Float64s(heights)
	median := heights[(len(heights)-1)/2]
	if median <= 0 {
		return [][]page.TextCell{group}
	}

	var normals, outliers []page.TextCell
	for _, cell := range group {
		if cell.Box.Height() > tallOutlierFactor*median {
			outliers = append(outliers, cell)
		} else {
			normals = append(normals, cell)
		}
	}
	if len(outliers) == 0 || len(normals) < 2 {
		return [][]page.TextCell{group}
	}

	clusters := clusterCellsByCentre(normals, lineTolerance)
	if len(clusters) < 2 {
		return [][]page.TextCell{group}
	}

	type band struct{ top, bottom float64 }
	bands := make([]band, len(clusters))
	for i, cluster := range clusters {
		top := math.Min(cluster[0].Box.T, cluster[0].Box.B)
		bottom := math.Max(cluster[0].Box.T, cluster[0].Box.B)
		for _, cell := range cluster[1:] {
			top = math.Min(top, math.Min(cell.Box.T, cell.Box.B))
			bottom = math.Max(bottom, math.Max(cell.Box.T, cell.Box.B))
		}
		bands[i] = band{top: top, bottom: bottom}
	}

	for _, outlier := range outliers {
		top := math.Min(outlier.Box.T, outlier.Box.B)
		bottom := math.Max(outlier.Box.T, outlier.Box.B)
		best := 0
		bestFraction := -1.0
		bestTopDistance := math.Inf(1)
		for i, b := range bands {
			height := b.bottom - b.top
			if height <= 0 {
				continue
			}
			fraction := (math.Min(b.bottom, bottom) - math.Max(b.top, top)) / height
			topDistance := math.Abs(top - b.top)
			if fraction > bestFraction+1e-9 ||
				(math.Abs(fraction-bestFraction) <= 1e-9 && topDistance < bestTopDistance) {
				best, bestFraction, bestTopDistance = i, fraction, topDistance
			}
		}
		clusters[best] = append(clusters[best], outlier)
	}

	return clusters
}

// buildParagraphTextLine sorts cells left-to-right, joins their trimmed texts
// using geometry-aware separators, records the enclosing box, smallest source
// index and list-marker hints, and populates the LineElement run model
// (Words/Elements) so downstream inline-formatting consumers see the same line.
func buildParagraphTextLine(cells []page.TextCell) ParagraphTextLine {
	orderCellsForReading(cells)

	boxes := make([]geom.Box, 0, len(cells))
	minIndex := cells[0].Index
	for _, cell := range cells {
		boxes = append(boxes, cell.Box)
		if cell.Index < minIndex {
			minIndex = cell.Index
		}
	}
	fontSize := dominantFontSize(cells)

	full := append([]page.TextCell(nil), cells...)
	listCandidate, listContentL := geometricListMarker(full)
	box := geom.EnclosingBox(boxes...)
	words := lineWords(full)
	return ParagraphTextLine{
		BBox:          box,
		FontBBox:      box,
		Words:         words,
		Text:          joinLineCellTexts(full),
		FontSize:      fontSize,
		Elements:      lineElements(words),
		Cells:         full,
		MinIndex:      minIndex,
		ListCandidate: listCandidate,
		ListContentL:  listContentL,
	}
}

// maxGlyphBoxToFontSize is the largest ratio of rendered box height to declared
// font size that a real glyph can reach. A glyph is drawn inside its font's em
// square plus a little overshoot, so even the tallest stretched delimiter of a
// well-formed font stays near 2x its point size. A box several times taller
// than the declared size therefore means the declared size is not the size the
// glyph was drawn at — the value came from an unresolved or matrix-scaled font
// rather than from the text state.
const maxGlyphBoxToFontSize = 3.0

// credibleCellFontSize reports whether a cell's declared font size may be used
// as evidence of the size its characters were set in.
//
// Invariant encoded: a declared font size is only believable when it is
// commensurate with the space the glyphs actually occupy. Some PDFs carry cells
// whose declared size is a fraction of a point inside a full-height box (a
// matrix-scaled or unresolved font); the number is a text-state artefact, not a
// type size. A maximum silently ignored such cells because they are tiny, but a
// median would let them outvote the run's real size, so they must be excluded
// explicitly.
//
// The test is deliberately ONE-SIDED. A box much TALLER than its declared size
// is incoherent, but a box much SHORTER is ordinary: a full stop, a hyphen or
// an apostrophe occupies a small fraction of its em.
func credibleCellFontSize(cell page.TextCell) bool {
	if cell.FontSize <= 0 {
		return false
	}
	height := cell.Box.Height()
	return height <= 0 || height <= maxGlyphBoxToFontSize*cell.FontSize
}

// dominantFontSize returns a run of cells' representative type size: the
// character-count-weighted median of their font sizes. It is the shared
// definition of "how big is this text set", used both for a visual line
// (buildParagraphTextLine) and for a merged sub-word fragment run
// (mergeCellShell).
//
// Invariant encoded: a run of text's font-size metric is the size at which the
// majority of its rendered characters are set. Every line carries a
// typographic body plus, optionally, a minority of differently-sized
// decoration — oversized display-math delimiters, integrals, radicals and
// drop caps above it; super/subscripts, footnote daggers, trailing folio
// numbers and section markers below it. Decoration is by definition a
// minority of the glyph mass, so only a size carrying at least half the
// characters may define the metric. This replaces the previous maximum,
// under which a single oversized glyph made a whole line measure as
// title-sized.
//
// Weighting is by rune count rather than by box width or area precisely so
// that one wide glyph cannot outvote the run of ordinary characters beside it:
// a summation sign is one character however much page it covers.
//
// Mixed lines resolve by majority, not by prominence, in both directions. A
// drop-cap line ("T" at 30pt followed by 60 body characters at 10pt) measures
// 10pt, because it is a body line wearing an ornament. A heading with a small
// trailing marker ("2. THE DISCRETE SOURCE" at 12pt plus a 7pt footnote
// index) measures 12pt, because the heading text is the majority. Exact ties
// resolve DOWNWARD (lower weighted median): a line split evenly between two
// sizes has no dominant body, and the smaller reading is the conservative one
// for the prominence gates downstream, which promote on size.
//
// The result is always a size that genuinely occurs on the line — never an
// average — so it stays directly comparable with page.TextCell.FontSize in the
// cell-level heading guards that measure a marker or continuation cell against
// its line. Cells with no credible font size or no visible text carry no
// evidence and are skipped; when a run has no such evidence at all the maximum
// declared size is returned, which is what callers saw before.
func dominantFontSize(cells []page.TextCell) float64 {
	type sample struct {
		size   float64
		weight int
	}

	maxSize := 0.0
	samples := make([]sample, 0, len(cells))
	total := 0
	for _, cell := range cells {
		if cell.FontSize > maxSize {
			maxSize = cell.FontSize
		}
		text := strings.TrimSpace(cell.Text)
		if !credibleCellFontSize(cell) || isListSpacerText(text) {
			continue
		}
		weight := utf8.RuneCountInString(text)
		samples = append(samples, sample{size: cell.FontSize, weight: weight})
		total += weight
	}
	if total <= 0 {
		return maxSize
	}

	sort.SliceStable(samples, func(i, j int) bool { return samples[i].size < samples[j].size })
	cumulative := 0
	for _, s := range samples {
		cumulative += s.weight
		if 2*cumulative >= total {
			return s.size
		}
	}
	return maxSize
}

// orderCellsForReading sorts a line's cells into reading order: columns
// left-to-right, and within a column top-to-bottom, so a fraction's numerator
// reads before its denominator at the stack's x-position. Column membership
// is assigned up front by a single left-to-right sweep — a cell joins the
// current column when it overlaps the column's x-extent by at least half the
// narrower width, otherwise it starts the next column — and the final sort
// key is (column, top, left, index). Pre-assigned keys give a total order; a
// pairwise left-vs-stacked comparator is not transitive over the overlapping
// geometry stacks create and can produce cycles. Ordinary neighbouring cells
// abut with at most sliver overlaps, so each gets its own column and the
// order degenerates to plain left-to-right.
func orderCellsForReading(cells []page.TextCell) {
	if len(cells) < 2 {
		return
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L != cells[j].Box.L {
			return cells[i].Box.L < cells[j].Box.L
		}
		return cellBoxTop(cells[i]) < cellBoxTop(cells[j])
	})

	columns := make([]int, len(cells))
	column := 0
	extentL, extentR := cells[0].Box.L, cells[0].Box.R
	for i := 1; i < len(cells); i++ {
		box := cells[i].Box
		overlap := math.Min(extentR, box.R) - box.L // cells sorted by L, so box.L >= extentL
		minWidth := math.Min(extentR-extentL, box.R-box.L)
		if minWidth > 0 && overlap >= 0.5*minWidth {
			extentR = math.Max(extentR, box.R)
		} else {
			column++
			extentL, extentR = box.L, box.R
		}
		columns[i] = column
	}

	indices := make([]int, len(cells))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := indices[i], indices[j]
		if columns[a] != columns[b] {
			return columns[a] < columns[b]
		}
		aTop, bTop := cellBoxTop(cells[a]), cellBoxTop(cells[b])
		if aTop != bTop {
			return aTop < bTop
		}
		if cells[a].Box.L != cells[b].Box.L {
			return cells[a].Box.L < cells[b].Box.L
		}
		return a < b
	})
	ordered := make([]page.TextCell, len(cells))
	for i, index := range indices {
		ordered[i] = cells[index]
	}
	copy(cells, ordered)
}

func cellBoxTop(cell page.TextCell) float64 {
	return math.Min(cell.Box.T, cell.Box.B)
}

// foldStackedFragmentLines inlines fraction-style stacks. An inline stacked
// construct (fraction, stacked sub/superscript column) leaves its raised and
// lowered parts as small fragment lines above/below the prose line, inside
// the horizontal gap the construct occupies in that line's text. Such a
// fragment — a few cells, vertically interpenetrating a neighbour line, every
// cell fitting inside an interior gap between that line's cells — is folded
// into the line at its x-position; cellReadingLess then reads column mates
// top-to-bottom. Ordinary short lines never qualify: stacked prose lines do
// not vertically overlap, and a line-initial or line-final cell is not inside
// an interior gap.
func foldStackedFragmentLines(lines []ParagraphTextLine) []ParagraphTextLine {
	if len(lines) < 2 {
		return lines
	}
	// A single stacked construct sheds at most a numerator or denominator
	// plus its wrapping delimiter glyphs onto one residue line — 1-3 cells in
	// every measured construct. Anything wider is an independent visual line
	// (a table row, a prose line) and must never be folded.
	const maxStackFragmentCells = 3
	out := make([]ParagraphTextLine, 0, len(lines))
	for i := range lines {
		line := lines[i]
		if len(line.Cells) <= maxStackFragmentCells {
			if len(out) > 0 && stackedFragmentLineFits(line, out[len(out)-1]) {
				merged := append(append([]page.TextCell(nil), out[len(out)-1].Cells...), line.Cells...)
				out[len(out)-1] = buildParagraphTextLine(merged)
				continue
			}
			if i+1 < len(lines) && stackedFragmentLineFits(line, lines[i+1]) {
				merged := append(append([]page.TextCell(nil), lines[i+1].Cells...), line.Cells...)
				lines[i+1] = buildParagraphTextLine(merged)
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// stackedFragmentLineFits reports whether every cell of the fragment line
// slots into the target line as part of an inline stack.
func stackedFragmentLineFits(fragment, target ParagraphTextLine) bool {
	if len(fragment.Cells) == 0 {
		return false
	}
	for _, cell := range fragment.Cells {
		if !stackedFragmentFitsGap(cell, target) {
			return false
		}
	}
	return true
}

// stackInterpenetrationMin is the minimum fraction of the smaller of the two
// heights by which a stacked fragment's box must overlap the band of EACH
// cell flanking its gap. Measured inline stacks dip 25-56% of a glyph height
// into their line's text band; separate rows overlap their upper neighbours'
// text bands not at all (line spacing keeps bands apart even when one
// oversized box makes the lines' box UNIONS touch).
const stackInterpenetrationMin = 0.2

// stackedFragmentFitsGap reports whether cell is the raised or lowered part of
// an inline stack in target. Horizontally it must fit inside one interior gap
// of the target's cells: cells it would stack with (substantial x-overlap)
// are ignored, any other x-overlap disqualifies, and a target cell must lie
// fully on each side so marginalia and line-initial fragments never fold.
// Vertically it must interpenetrate the TEXT band of both cells flanking that
// gap by stackInterpenetrationMin — a stacked construct hangs into the band
// of the words around it, while a table row beneath never overlaps the band
// of the specific cells around its column position. Judging against the
// flanking cells' own boxes (never the line's box union) keeps one oversized
// cell elsewhere in the line from opening the gate.
func stackedFragmentFitsGap(cell page.TextCell, target ParagraphTextLine) bool {
	if len(target.Cells) < 2 {
		return false
	}
	// Sub-glyph abutment tolerance for x-fit tests: adjacent PDF glyph boxes
	// routinely overlap by fractions of a point of coordinate jitter.
	const slack = 0.5
	width := cell.Box.R - cell.Box.L
	if width <= 0 {
		return false
	}
	var left, right *page.TextCell
	for i := range target.Cells {
		tc := &target.Cells[i]
		overlap := math.Min(cell.Box.R, tc.Box.R) - math.Max(cell.Box.L, tc.Box.L)
		minWidth := math.Min(width, tc.Box.R-tc.Box.L)
		if minWidth > 0 && overlap >= 0.5*minWidth {
			continue // column mate — the fragment stacks with it in place
		}
		if overlap > slack {
			return false // would cut into a foreign cell's span
		}
		if tc.Box.R <= cell.Box.L+slack && (left == nil || tc.Box.R > left.Box.R) {
			left = tc
		}
		if tc.Box.L >= cell.Box.R-slack && (right == nil || tc.Box.L < right.Box.L) {
			right = tc
		}
	}
	if left == nil || right == nil {
		return false
	}
	return fragmentInterpenetratesBand(cell, *left) && fragmentInterpenetratesBand(cell, *right)
}

// fragmentInterpenetratesBand reports whether the fragment's box overlaps the
// flanking cell's band by at least stackInterpenetrationMin of the smaller
// height.
func fragmentInterpenetratesBand(cell, flank page.TextCell) bool {
	top := cellBoxTop(cell)
	bottom := math.Max(cell.Box.T, cell.Box.B)
	flankTop := cellBoxTop(flank)
	flankBottom := math.Max(flank.Box.T, flank.Box.B)
	overlap := math.Min(bottom, flankBottom) - math.Max(top, flankTop)
	minHeight := math.Min(bottom-top, flankBottom-flankTop)
	return minHeight > 0 && overlap >= stackInterpenetrationMin*minHeight
}

// sameVisualLine reports whether a cell belongs on the line currently being
// accumulated, by vertical-centre proximity with a height-overlap fallback.
func sameVisualLine(box geom.Box, center, lineTop, lineBottom, lineCenter, lineTolerance float64) bool {
	if math.Abs(center-lineCenter) <= lineTolerance {
		return true
	}
	cellTop := math.Min(box.T, box.B)
	cellBottom := math.Max(box.T, box.B)
	cellHeight := cellBottom - cellTop
	lineHeight := lineBottom - lineTop
	if cellHeight <= 0 || lineHeight <= 0 {
		return false
	}
	overlap := math.Min(lineBottom, cellBottom) - math.Max(lineTop, cellTop)
	if overlap <= 0 {
		return false
	}
	overlapRatio := overlap / math.Min(lineHeight, cellHeight)
	if overlapRatio < 0.6 {
		return false
	}
	return math.Abs(center-lineCenter) <= math.Max(lineTolerance, math.Max(lineHeight, cellHeight)*0.75)
}

// mergeInlineFragmentLines folds a tiny single-glyph fragment line into the
// adjacent body line it visually overlaps (super/subscripts, stray accents).
func mergeInlineFragmentLines(lines []ParagraphTextLine) []ParagraphTextLine {
	if len(lines) < 2 {
		return lines
	}
	out := make([]ParagraphTextLine, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if isSmallInlineFragmentLine(line) {
			if i+1 < len(lines) && inlineFragmentBelongsToLine(line, lines[i+1]) {
				mergedCells := append(append([]page.TextCell(nil), line.Cells...), lines[i+1].Cells...)
				out = append(out, buildParagraphTextLine(mergedCells))
				i++
				continue
			}
			if len(out) > 0 && inlineFragmentBelongsToLine(line, out[len(out)-1]) {
				mergedCells := append(append([]page.TextCell(nil), out[len(out)-1].Cells...), line.Cells...)
				out[len(out)-1] = buildParagraphTextLine(mergedCells)
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func isSmallInlineFragmentLine(line ParagraphTextLine) bool {
	visible := 0
	for _, cell := range line.Cells {
		if strings.TrimSpace(cell.Text) == "" || isListSpacerText(cell.Text) {
			continue
		}
		visible++
		if visible > 1 {
			return false
		}
	}
	if visible != 1 || line.BBox.Height() <= 0 {
		return false
	}
	reference := line.FontSize
	if reference <= 0 {
		reference = 10
	}
	return line.BBox.Height() <= math.Max(4, reference*0.45)
}

func inlineFragmentBelongsToLine(fragment, line ParagraphTextLine) bool {
	fragmentHeight := fragment.BBox.Height()
	lineHeight := line.BBox.Height()
	if fragmentHeight <= 0 || lineHeight <= 0 {
		return false
	}
	overlap := math.Min(fragment.BBox.B, line.BBox.B) - math.Max(fragment.BBox.T, line.BBox.T)
	if overlap <= 0 || overlap/fragmentHeight < 0.6 {
		return false
	}
	slack := math.Max(6, math.Max(fragment.FontSize, line.FontSize))
	centerX := (fragment.BBox.L + fragment.BBox.R) * 0.5
	return centerX >= line.BBox.L-slack && centerX <= line.BBox.R+slack
}

// lineWords converts a line's cells (left-to-right) into TextLineWords.
// Font formatting is taken from the cell's FontName/FontWeight/FontFlags
// (surfaced from PDFium with CollectFontInformation). This is the faithful
// source for the LineElement run-splitter and the markdown writer's inline
// formatting.
func lineWords(cells []page.TextCell) []TextLineWord {
	ordered := append([]page.TextCell(nil), cells...)
	orderCellsForReading(ordered)
	words := make([]TextLineWord, 0, len(ordered))
	for _, c := range ordered {
		text := strings.TrimSpace(c.Text)
		if text == "" || isListSpacerText(text) {
			continue
		}
		words = append(words, TextLineWord{
			Value:      text,
			BBox:       c.Box,
			FontBBox:   c.Box,
			FontSize:   c.FontSize,
			FontName:   c.FontName,
			Bold:       c.IsBold(),
			Italic:     c.IsItalic(),
			Color:      c.Color,
			Source:     c,
			Confidence: 1.0,
		})
	}
	return words
}

// lineElements splits a line's words into formatting runs (LineElements).
// A new run starts on any font transition: bold, italic, font size, or
// font name. Each run is an inline-element segment the markdown writer walks
// for bold/italic/code emission.
func lineElements(words []TextLineWord) []LineElement {
	if len(words) == 0 {
		return nil
	}
	runs := make([]LineElement, 0, 1)
	current := LineElement{
		Bold: words[0].Bold, Italic: words[0].Italic,
		Words: []TextLineWord{words[0]},
	}
	current.BBox = words[0].BBox
	current.Text = words[0].Value
	for i := 1; i < len(words); i++ {
		w := words[i]
		sameRun := w.Bold == current.Bold &&
			w.Italic == current.Italic &&
			w.Source.IsMonospace() == current.Words[0].Source.IsMonospace() &&
			math.Abs(w.FontSize-current.Words[0].FontSize) < 0.5 &&
			w.FontName == current.Words[0].FontName
		if sameRun {
			current.Words = append(current.Words, w)
			current.BBox = geom.EnclosingBox(current.BBox, w.BBox)
			current.Text = joinRunText(current.Text, w.Value)
			continue
		}
		runs = append(runs, current)
		current = LineElement{
			Bold: w.Bold, Italic: w.Italic,
			Words: []TextLineWord{w},
			BBox:  w.BBox,
			Text:  w.Value,
		}
	}
	runs = append(runs, current)
	return runs
}

// formatLineElements reconstructs a visual line's text from its LineElement
// runs, wrapping each run with Markdown inline-formatting markers driven by the
// run's font metadata (Bold/Italic and per-word IsMonospace). This is the
// opt-in path used by mergeLines when EnableInlineFormatting is set; the
// default path uses line.Text unchanged.
//
// Wrapping rules (code takes precedence over bold/italic for a run):
//   - all words monospace -> `text` (inline code)
//   - bold && italic       -> ***text***
//   - bold                 -> **text**
//   - italic               -> *text*
//   - otherwise            -> text (plain)
//
// Whitespace-only or empty runs are emitted unwrapped. Runs are concatenated in
// order; each run's internal text is rebuilt from its source cells via the same
// geometry-aware joinLineCellTexts the default path uses (runSourceText), and the
// separator inserted between two adjacent runs is decided geometrically from
// their boundary source cells via shouldSeparateLineCells. So the formatted text
// equals the default line text plus the formatting markers, except in the narrow
// case of a standalone hyphen that forms its own run by a font transition
// relative to both neighbours (its 3-cell compaction context is truncated at the
// run boundary) — a rare edge that never affects the default (off) output.
func formatLineElements(line ParagraphTextLine) string {
	if len(line.Elements) == 0 {
		return line.Text
	}
	var builder strings.Builder
	for i := range line.Elements {
		element := line.Elements[i]
		if i > 0 {
			builder.WriteString(interRunSeparator(line.Elements[i-1], element))
		}
		builder.WriteString(formatLineElement(element))
	}
	return builder.String()
}

// interRunSeparator returns " " when the plain-text join would put a space
// between the last word of left and the first word of right, and "" otherwise.
// It reuses shouldSeparateLineCells (geometry/font-metric) so spacing matches
// the default joinLineCellTexts path.
func interRunSeparator(left, right LineElement) string {
	if len(left.Words) == 0 || len(right.Words) == 0 {
		return " "
	}
	boundary := []page.TextCell{
		left.Words[len(left.Words)-1].Source,
		right.Words[0].Source,
	}
	if shouldSeparateLineCells(boundary, 1) {
		return " "
	}
	return ""
}

// runSourceText reconstructs a run's text from its source cells using the same
// geometry-aware joinLineCellTexts the default (plain) path uses, so within-run
// glyph compaction (tight alphanumeric runs, standalone hyphen/period/apostrophe)
// is identical to line.Text. This is what keeps "Sec3", "co-op" and "www.com"
// intact when inline formatting is enabled, rather than the always-spaced
// joinRunText that the run-splitter uses for element.Text.
func runSourceText(element LineElement) string {
	if len(element.Words) == 0 {
		return element.Text
	}
	cells := make([]page.TextCell, 0, len(element.Words))
	for _, word := range element.Words {
		cells = append(cells, word.Source)
	}
	return joinLineCellTexts(cells)
}

// formatLineElement wraps a single run's text with inline-formatting markers.
func formatLineElement(element LineElement) string {
	text := runSourceText(element)
	if strings.TrimSpace(text) == "" {
		return text
	}
	if isMonospaceRun(element) {
		return "`" + text + "`"
	}
	switch {
	case element.Bold && element.Italic:
		return "***" + text + "***"
	case element.Bold:
		return "**" + text + "**"
	case element.Italic:
		return "*" + text + "*"
	default:
		return text
	}
}

// isMonospaceRun reports whether every word in the run comes from a fixed-pitch
// (monospace) font cell — the signal for an inline code span.
func isMonospaceRun(element LineElement) bool {
	if len(element.Words) == 0 {
		return false
	}
	for _, word := range element.Words {
		if !word.Source.IsMonospace() {
			return false
		}
	}
	return true
}

// joinRunText joins two words in a formatting run, adding a space unless
// the words are hyphenated (the previous word ends with a hyphen and the
// next starts lowercase — a soft hyphenation break).
func joinRunText(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if isSoftHyphenation(left, right) {
		return strings.TrimSuffix(left, "-") + right
	}
	return left + " " + right
}

func isSoftHyphenation(left, right string) bool {
	if !strings.HasSuffix(left, "-") || len(left) < 2 {
		return false
	}
	if len(right) == 0 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(right)
	return unicode.IsLower(first)
}

// (utf8DecodeRune removed — use unicode/utf8.DecodeRuneInString directly)
