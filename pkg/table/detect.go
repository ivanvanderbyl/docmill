package table

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
)

type DetectionOptions struct {
	MinRows              int
	MinCols              int
	RowTolerance         float64
	ColumnTolerance      float64
	TextOverlapThreshold float64
	MaxRowFillRatio      float64
	// LearnedColumns derives column boundaries with the model trained on
	// FinTabNet instead of the densest-row heuristic. It falls back to the
	// heuristic whenever the model accepts too few boundaries, so a table is
	// never lost — a wrong grid is bad, no table at all is worse. OFF by
	// default.
	LearnedColumns bool
	// ColumnRulings are the page's ruling segments, used as boundary evidence
	// by the learned path. Optional.
	ColumnRulings []page.RulingSegment
}

type DetectionResult struct {
	Tables    []DetectedTable
	TextCells []page.TextCell
}

type DetectedTable struct {
	Data      Data
	Box       geom.Box
	TextCells []page.TextCell
}

type logicalTableRow struct {
	Row    textline.ParagraphTextLine
	Source []page.TextCell
}

type continuationLogicalRow struct {
	Box      geom.Box
	Cells    []page.TextCell
	HasLeft  bool
	HasRight bool
}

type multilineLogicalRow struct {
	Box   geom.Box
	Cells []page.TextCell
	Parts [][]string
}

type captionlessRowParts struct {
	cells  []page.TextCell
	parts  [][]string
	seen   map[int]bool
	center float64
}

type columnCluster struct {
	Cells  []page.TextCell
	Center float64
}

type rulingLine struct {
	Position float64
	Start    float64
	End      float64
}

type wordGridAnchorMode int

type compactWordGridCandidateBand struct {
	Start int
	Rows  []textline.ParagraphTextLine
}

const (
	compactWordGridMinRows      = 3
	compactWordGridMinCols      = 4
	compactWordGridMergeGap     = 4.0
	compactWordGridMinNumerics  = 4
	compactWordGridMaxLookback  = 12
	shortCaptionedWordGridRows  = 2
	captionlessMultilineMinCols = 4

	wordGridAnchorCenter wordGridAnchorMode = iota
	wordGridAnchorLeft
)

func DetectTables(cells []page.TextCell, rulings []page.RulingSegment, options DetectionOptions) DetectionResult {
	options = normaliseDetectionOptions(options)
	ruled := detectRuledTables(cells, rulings, options)
	if len(ruled.Tables) == 0 {
		return DetectTextTables(cells, options)
	}

	tableCellIndexes := make(map[int]bool)
	for _, detected := range ruled.Tables {
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}
	textFallback := DetectTextTables(unclaimedTextCells(cells, tableCellIndexes), options)
	return DetectionResult{
		Tables:    append(ruled.Tables, textFallback.Tables...),
		TextCells: textFallback.TextCells,
	}
}

func DetectTextTables(cells []page.TextCell, options DetectionOptions) DetectionResult {
	options = normaliseDetectionOptions(options)
	if len(cells) == 0 {
		return DetectionResult{}
	}

	rows := groupTextRows(cells, options.RowTolerance)
	tables := make([]DetectedTable, 0)
	tableCellIndexes := make(map[int]bool)

	for _, detected := range detectMultilineNumericContinuationTables(rows, options, tableCellIndexes) {
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}

	for _, detected := range detectCaptionlessMultilineTextTables(rows, options, tableCellIndexes) {
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}

	for start := 0; start < len(rows); {
		if rowHasClaimedCell(rows[start], tableCellIndexes) || len(rows[start].Cells) < options.MinCols {
			start++
			continue
		}

		candidateRows := []logicalTableRow{newLogicalTableRow(rows[start])}
		end := start + 1
		for end < len(rows) && !rowHasClaimedCell(rows[end], tableCellIndexes) {
			row := rows[end]
			if len(row.Cells) >= options.MinCols {
				candidateRows = append(candidateRows, newLogicalTableRow(row))
				end++
				continue
			}
			if len(candidateRows) > 0 && isFirstColumnContinuationRow(row, candidateRows[len(candidateRows)-1].Row, options) {
				candidateRows[len(candidateRows)-1] = mergeLogicalTableRowContinuation(candidateRows[len(candidateRows)-1], row)
				end++
				continue
			}
			break
		}

		if len(candidateRows) >= options.MinRows {
			if detected, consumedRows, ok := buildDetectedTablePrefix(rows[start:end], options); ok {
				tables = append(tables, detected)
				for _, cell := range detected.TextCells {
					tableCellIndexes[cell.Index] = true
				}
				start += consumedRows
				continue
			}
		}
		start = end
	}

	for _, detected := range detectCaptionlessThreeColumnMultilineTextTables(rows, options, tableCellIndexes) {
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}

	for _, detected := range detectLabelValueTextTables(rows, options, tableCellIndexes) {
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}

	for _, detected := range detectContinuationTextTables(unclaimedTextCells(cells, tableCellIndexes), options) {
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			tableCellIndexes[cell.Index] = true
		}
	}

	// Suppress multi-column-prose false positives: a borderless region whose
	// grid puts long, sentence-like text in >= 2 columns is far more likely to
	// be multi-column page prose split into a fake grid than a real table. Real
	// tables confine long prose to at most one description column. Rejected
	// tables release their cells back to the paragraph path. This is the
	// dominant borderless false positive on diverse corpora.
	tables = dropMultiColumnProseTables(tables, tableCellIndexes)

	// Suppress equation/prose grids whose column boundaries are not persistent
	// whitespace gutters (see the gutter-persistence invariant above).
	tables = dropGutterCrossingTables(tables, tableCellIndexes, options.RowTolerance)

	remaining := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if !tableCellIndexes[cell.Index] {
			remaining = append(remaining, cell)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return remaining[i].Index < remaining[j].Index
	})

	return DetectionResult{
		Tables:    tables,
		TextCells: remaining,
	}
}

// dropMultiColumnProseTables removes detected tables that look like multi-column
// page prose (tableHasMultiColumnProse) and releases their cells from claimed so
// they flow back to the paragraph path. Kept tables' claims are untouched. Used
// by both the borderless cascade (DetectTextTables) and the anchored detector
// (DetectAnchoredTextTables) so the dominant borderless false positive is
// suppressed regardless of which path produced it.
func dropMultiColumnProseTables(tables []DetectedTable, claimed map[int]bool) []DetectedTable {
	kept := make([]DetectedTable, 0, len(tables))
	for _, detected := range tables {
		if tableHasMultiColumnProse(detected.Data) {
			for _, cell := range detected.TextCells {
				delete(claimed, cell.Index)
			}
			continue
		}
		kept = append(kept, detected)
	}
	return kept
}

// Gutter-persistence gate. The invariant: a real table's columns are separated
// by whitespace corridors that persist down the ENTIRE table — text runs sit
// inside their column, so (almost) no source run straddles an interior column
// boundary. Display mathematics and body prose that merely happen to align on
// one anchor line violate this: subsequent lines flow straight across the
// imaginary boundaries (a prose line is one full-width run; a second equation
// line's fragments drift across the anchor's gaps). Legitimate exceptions —
// merged multi-column headers and full-width section rows — are a small
// minority of lines, so the gate fires only when crossings are frequent:
// at least half the lines contain a crossing run, or a single boundary is
// crossed on at least 40% of lines. Both signals are pure geometry.
const (
	// gutterCrossMinTolerance ignores glyph bleed across a boundary (points on
	// each side); scaled up for large type via gutterCrossToleranceHeightRatio.
	gutterCrossMinTolerance         = 2.0
	gutterCrossToleranceHeightRatio = 0.25
	// gutterMinCrossingLines is the absolute floor before the crossing rules
	// may fire. Genuine tables legitimately contain up to two spanning lines
	// (a merged multi-column header plus a full-width title, section, or
	// footnote line), so fewer than three crossing lines is never treated as
	// prose flowing across the grid.
	gutterMinCrossingLines = 3
	// gutterCrossingLineFraction: suppress when this share of lines contains a
	// boundary-crossing run.
	gutterCrossingLineFraction = 0.5
	// gutterBoundaryCrossFraction: suppress when one boundary is crossed on
	// this share of lines (a corridor that plainly is not whitespace).
	gutterBoundaryCrossFraction = 0.4
	// gutterMinDegenerateGutters: suppress when at least this many gutters have
	// (nearly) no width AND they make up at least half the gutters.
	gutterMinDegenerateGutters = 2
	// gutterDegenerateHeightRatio and gutterDegenerateMaxWidth bound the width
	// below which a corridor is too narrow to be a visual column separator
	// (about half a character height, capped for outsized display glyphs).
	gutterDegenerateHeightRatio = 0.5
	gutterDegenerateMaxWidth    = 6.0
)

// dropGutterCrossingTables removes detected borderless tables whose interior
// column boundaries are not persistent whitespace gutters (see the invariant
// above) and releases their cells back to the text flow. Applied by both the
// borderless cascade and the anchored detector; ruled tables never pass
// through it (their boundaries are real ink).
func dropGutterCrossingTables(tables []DetectedTable, claimed map[int]bool, rowTolerance float64) []DetectedTable {
	kept := make([]DetectedTable, 0, len(tables))
	for _, detected := range tables {
		if tableViolatesGutterPersistence(detected, rowTolerance) {
			for _, cell := range detected.TextCells {
				delete(claimed, cell.Index)
			}
			continue
		}
		kept = append(kept, detected)
	}
	return kept
}

// dropGutterCrossingTablesWhere is dropGutterCrossingTables restricted to the
// tables flagged as line-built; word-token grids pass through unjudged. When a
// line-built table offers no line-level column evidence (its lines are merged
// runs), the invariant is re-judged on the word tokens inside its box — the
// finest granularity at which gutters are observable.
func dropGutterCrossingTablesWhere(tables []DetectedTable, lineBuilt []bool, tokenCells []page.TextCell, claimed map[int]bool, options DetectionOptions) []DetectedTable {
	kept := make([]DetectedTable, 0, len(tables))
	for index, detected := range tables {
		if index < len(lineBuilt) && lineBuilt[index] && tableViolatesGutterPersistenceWithTokens(detected, tokenCells, options) {
			for _, cell := range detected.TextCells {
				delete(claimed, cell.Index)
			}
			continue
		}
		kept = append(kept, detected)
	}
	return kept
}

// tableViolatesGutterPersistenceWithTokens judges the table on its own source
// cells first; if those lines cannot express the columns (merged runs), it
// re-judges on the word tokens inside the table box.
func tableViolatesGutterPersistenceWithTokens(detected DetectedTable, tokenCells []page.TextCell, options DetectionOptions) bool {
	if tableViolatesGutterPersistence(detected, options.RowTolerance) {
		return true
	}
	if len(tokenCells) == 0 || tableHasLineColumnEvidence(detected, options.RowTolerance) {
		return false
	}
	tokens := containedTextCells(tokenCells, detected.Box, options.TextOverlapThreshold)
	if len(tokens) == 0 {
		return false
	}
	tokenView := detected
	tokenView.TextCells = tokens
	return tableViolatesGutterPersistence(tokenView, options.RowTolerance)
}

// tableHasLineColumnEvidence reports whether some source line carries one cell
// per detected column (the precondition under which the line-level judgment is
// meaningful).
func tableHasLineColumnEvidence(detected DetectedTable, rowTolerance float64) bool {
	for _, row := range groupTextRows(detected.TextCells, rowTolerance) {
		if len(row.Cells) >= detected.Data.NumCols {
			return true
		}
	}
	return false
}

// tableViolatesGutterPersistence tests the invariant on a detected table's
// SOURCE lines. Each column's core is its robust content extent: source cells
// are grouped per (line, nearest detected column), each group enclosed, and
// the core is the median left/right edge of those per-line groups (a median
// resists one wide merged-header or section line). Two failure shapes fire the
// gate:
//
//   - crossing runs: a run overlapping two (or more) cores covers the
//     whitespace the grid claims is a boundary — prose flowing straight across
//     the imaginary columns. Counted per line and per boundary.
//   - degenerate gutters: adjacent cores that (nearly) touch mean the columns'
//     content interpenetrates row to row — mathematics whose fragment gaps
//     drift so no corridor survives.
func tableViolatesGutterPersistence(detected DetectedTable, rowTolerance float64) bool {
	if detected.Data.NumCols < 2 {
		return false
	}
	rows := groupTextRows(detected.TextCells, rowTolerance)
	if len(rows) < 2 {
		return false
	}

	// The gate judges LINE-level gutters, so it needs line-level column
	// evidence: some source line must carry one cell per column (the line the
	// borderless detectors derived the columns from). A grid recovered from
	// word tokens inside merged line runs (every line a single run) offers no
	// such evidence — its full-width runs are legitimate, not prose flowing
	// across gutters — so it is not judged here.
	maxCellsPerRow := 0
	for _, row := range rows {
		if len(row.Cells) > maxCellsPerRow {
			maxCellsPerRow = len(row.Cells)
		}
	}
	if maxCellsPerRow < detected.Data.NumCols {
		return false
	}

	cores, coreBands := tableColumnContentCores(detected.Data, rows)
	if len(cores) < 2 {
		return false
	}

	modalHeight := modalTextCellHeight(detected.TextCells)
	tolerance := math.Max(gutterCrossMinTolerance, gutterCrossToleranceHeightRatio*modalHeight)

	// A corridor narrower than about half a character height cannot separate
	// columns visually; when at least two such corridors make up half the
	// gutters, the columns' content interpenetrates row to row (drifting
	// mathematics), not a grid. Real tables' median corridors measure well
	// above this even in tight layouts.
	degenerateWidth := math.Max(tolerance, math.Min(gutterDegenerateHeightRatio*modalHeight, gutterDegenerateMaxWidth))
	degenerate := 0
	for index := 0; index+1 < len(cores); index++ {
		if cores[index+1].L-cores[index].R < degenerateWidth {
			degenerate++
		}
	}
	if degenerate >= gutterMinDegenerateGutters && degenerate*2 >= len(cores)-1 {
		return true
	}

	crossingLines := 0
	perBoundary := make([]int, len(cores)-1)
	for _, row := range rows {
		lineCrosses := false
		for _, cell := range row.Cells {
			own := columnBandForCell(coreBands, cell.Box.CenterX())
			for boundary := 0; boundary+1 < len(cores); boundary++ {
				// The cell's ink covers the middle of the claimed gutter (with
				// a margin capped at half the corridor so narrow corridors are
				// still judged). A cell merely overhanging its own column stops
				// short of the corridor middle and does not count.
				middle := (cores[boundary].R + cores[boundary+1].L) / 2
				margin := math.Max(0, math.Min(tolerance, (cores[boundary+1].L-cores[boundary].R)/2))
				if cell.Box.L >= middle-margin || cell.Box.R <= middle+margin {
					continue
				}
				// The run must also overlap ANOTHER column's content, not just
				// the gutter: a wide entry in a ragged right- (or left-)
				// aligned column reaches across the corridor middle but sits
				// alone in its own column, whereas a genuine spanning prose
				// line covers the neighbouring column's content as well.
				if !cellOverlapsForeignCore(cell.Box, own, boundary, cores, tolerance) {
					continue
				}
				perBoundary[boundary]++
				lineCrosses = true
			}
		}
		if lineCrosses {
			crossingLines++
		}
	}

	lineCount := float64(len(rows))
	if crossingLines >= gutterMinCrossingLines && float64(crossingLines) >= gutterCrossingLineFraction*lineCount {
		return true
	}
	for _, count := range perBoundary {
		if count >= gutterMinCrossingLines && float64(count) >= gutterBoundaryCrossFraction*lineCount {
			return true
		}
	}
	return false
}

// tableColumnContentCores computes each detected column's robust content
// extent from the source lines. Cells are assigned to the nearest column
// centre (recovered from the grid's single-span cell boxes), grouped per line,
// and each column's core is the median left/right edge of its per-line group
// boxes. Columns with no assigned content are skipped; the returned cores are
// ordered left to right.
func tableColumnContentCores(data Data, rows []textline.ParagraphTextLine) ([]geom.Box, []geom.Box) {
	bands := tableColumnBands(data)
	if len(bands) < 2 {
		return nil, nil
	}

	type group struct {
		box geom.Box
		set bool
	}
	lefts := make([][]float64, len(bands))
	rights := make([][]float64, len(bands))
	for _, row := range rows {
		groups := make([]group, len(bands))
		for _, cell := range row.Cells {
			column := columnBandForCell(bands, cell.Box.CenterX())
			if column < 0 {
				continue
			}
			if !groups[column].set {
				groups[column] = group{box: cell.Box, set: true}
				continue
			}
			groups[column].box = geom.EnclosingBox(groups[column].box, cell.Box)
		}
		for column, g := range groups {
			if !g.set {
				continue
			}
			lefts[column] = append(lefts[column], g.box.L)
			rights[column] = append(rights[column], g.box.R)
		}
	}

	cores := make([]geom.Box, 0, len(bands))
	coreBands := make([]geom.Box, 0, len(bands))
	for column := range bands {
		if len(lefts[column]) == 0 {
			continue
		}
		cores = append(cores, geom.Box{L: medianFloat64(lefts[column]), R: medianFloat64(rights[column])})
		coreBands = append(coreBands, bands[column])
	}
	return cores, coreBands
}

// cellOverlapsForeignCore reports whether the cell's ink overlaps, by more
// than tolerance, the content core of a column adjacent to the given boundary
// that is NOT the cell's own column.
func cellOverlapsForeignCore(cell geom.Box, own, boundary int, cores []geom.Box, tolerance float64) bool {
	for _, coreIndex := range [2]int{boundary, boundary + 1} {
		if coreIndex == own || coreIndex < 0 || coreIndex >= len(cores) {
			continue
		}
		overlap := math.Min(cell.R, cores[coreIndex].R) - math.Max(cell.L, cores[coreIndex].L)
		if overlap > tolerance {
			return true
		}
	}
	return false
}

// tableColumnBands recovers each column's horizontal band from the grid's
// single-span cell boxes (span cells straddle columns by design and are
// excluded). Bands are returned left to right.
func tableColumnBands(data Data) []geom.Box {
	left := make(map[int]float64, data.NumCols)
	right := make(map[int]float64, data.NumCols)
	for _, cell := range data.Cells {
		if cell.Box == nil || cell.EndCol-cell.StartCol != 1 {
			continue
		}
		if existing, ok := left[cell.StartCol]; !ok || cell.Box.L < existing {
			left[cell.StartCol] = cell.Box.L
		}
		if existing, ok := right[cell.StartCol]; !ok || cell.Box.R > existing {
			right[cell.StartCol] = cell.Box.R
		}
	}
	bands := make([]geom.Box, 0, data.NumCols)
	for col := 0; col < data.NumCols; col++ {
		l, okLeft := left[col]
		r, okRight := right[col]
		if !okLeft || !okRight {
			continue
		}
		bands = append(bands, geom.Box{L: l, R: r})
	}
	return bands
}

// columnBandForCell assigns a cell to the band containing its centre; when no
// band contains it (or several overlapping bands do), the band with the
// nearest centre (among the containing ones, if any) wins.
func columnBandForCell(bands []geom.Box, x float64) int {
	best, bestDistance := -1, math.Inf(1)
	containingBest, containingDistance := -1, math.Inf(1)
	for index, band := range bands {
		distance := math.Abs(x - (band.L+band.R)/2)
		if distance < bestDistance {
			best, bestDistance = index, distance
		}
		if x >= band.L && x <= band.R && distance < containingDistance {
			containingBest, containingDistance = index, distance
		}
	}
	if containingBest >= 0 {
		return containingBest
	}
	return best
}

// modalTextCellHeight returns the most common rounded cell height, preferring
// the larger height on ties. Used to scale the crossing tolerance with type
// size.
func modalTextCellHeight(cells []page.TextCell) float64 {
	counts := make(map[int]int, len(cells))
	for _, cell := range cells {
		height := int(cell.Box.Height() + 0.5)
		if height > 0 {
			counts[height]++
		}
	}
	best, bestCount := 0, 0
	for height, count := range counts {
		if count > bestCount || (count == bestCount && height > best) {
			best, bestCount = height, count
		}
	}
	return float64(best)
}

// dropProseSlabTables is the real-table-sparing variant of
// dropMultiColumnProseTables used by the anchored detector. It suppresses a
// multi-column-prose region ONLY when no "Table N" caption sits next to it. A
// genuine description-heavy table (two long description columns, common in real
// documents) is announced by a caption — "[Table 2.2.4.A] Evaluations …" — and
// is preserved; a two-column bibliography or body-text slab has no caption and
// is dropped, releasing its cells back to TextCells.
//
// The caption is the only RELIABLE discriminator: short-key-column and
// distinct-header tests were measured to be tripped by prose slabs too (a slab's
// arbitrary splits produce short fragments and a shorter first row), so they
// spare the very false positives this guard exists to drop. A caption ("Table"/
// "Tab" leading token) does not occur incidentally beside a prose slab.
func dropProseSlabTables(tables []DetectedTable, pageCells []page.TextCell, claimed map[int]bool) []DetectedTable {
	kept := make([]DetectedTable, 0, len(tables))
	for _, detected := range tables {
		if tableHasMultiColumnProse(detected.Data) && !tableHasNearbyTableCaption(detected.Box, pageCells) {
			for _, cell := range detected.TextCells {
				delete(claimed, cell.Index)
			}
			continue
		}
		kept = append(kept, detected)
	}
	return kept
}

// tableHasNearbyTableCaption reports whether a "Table N"/"Tab." caption cell sits
// directly above or below the table box (within captionGap) and horizontally
// near it. Used to spare a real captioned table from the prose-slab guard.
func tableHasNearbyTableCaption(box geom.Box, cells []page.TextCell) bool {
	const (
		captionGap    = 120.0
		captionXSlack = 48.0
	)
	top := math.Min(box.T, box.B)
	bottom := math.Max(box.T, box.B)
	for _, cell := range cells {
		if !cellStartsTableCaption(cell.Text) {
			continue
		}
		if cell.Box.R < box.L-captionXSlack || cell.Box.L > box.R+captionXSlack {
			continue
		}
		cellTop := math.Min(cell.Box.T, cell.Box.B)
		cellBottom := math.Max(cell.Box.T, cell.Box.B)
		gap := 0.0
		switch {
		case cellBottom <= top:
			gap = top - cellBottom
		case cellTop >= bottom:
			gap = cellTop - bottom
		}
		if gap <= captionGap {
			return true
		}
	}
	return false
}

// cellStartsTableCaption reports whether a cell's text begins with a table
// caption cue ("table"/"tab" as its first alphanumeric token, e.g. "Table 3:",
// "[Table 2.2.4.A]").
func cellStartsTableCaption(text string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return len(tokens) > 0 && (tokens[0] == "table" || tokens[0] == "tab")
}

// ValidityScore is a positive table-validity score (0 = not a table, higher
// = more table-like) — a geometric table-DTO score that rewards table-ness
// positively rather than suppressing prose negatively.
//
// It combines three positive credits — standard (regular) structure, a distinct
// header row, and a key/value column shape — and applies the dominant non-table
// signal as a hard floor: a region whose every content column is prose (a slab
// of body paragraphs gridded into a fake table) scores 0. On a no-render
// geometric path the structural credits cannot separate the residual borderless
// false positives from genuine description-column tables (empirically a real
// "Stage|Function|Explanation|Benefit" table is structurally identical to a
// prose slab), so the all-columns-prose floor is the only signal that gates
// without regressing real tables; the credits are computed for document-general
// ranking and to make the model explicit. The pdf backend rejects a survived
// borderless table when its score is 0.
func ValidityScore(data Data) float64 {
	if data.NumCols < 2 || data.NumRows < 1 {
		return 1
	}
	if tableProseColumnFraction(data) >= 1.0 {
		return 0
	}
	return 0.34*tableStdStructureScore(data) +
		0.33*tableHeaderDistinctScore(data) +
		0.33*tableKeyValueScore(data)
}

// tableProseColumnFraction is the fraction of CONTENT columns (columns with any
// non-empty cell) that are prose — contain a cell with a long (> 20-word) run.
// 1.0 means every content column is prose (the all-columns-prose non-table
// floor). Returns 0 when there are fewer than two content columns.
func tableProseColumnFraction(data Data) float64 {
	const proseColumnWordCount = 20
	grid := data.Grid()
	content, prose := 0, 0
	for col := 0; col < data.NumCols; col++ {
		hasContent, isProse := false, false
		for row := 0; row < data.NumRows; row++ {
			n := len(strings.Fields(grid[row][col].Text))
			if n > 0 {
				hasContent = true
			}
			if n > proseColumnWordCount {
				isProse = true
			}
		}
		if hasContent {
			content++
		}
		if isProse {
			prose++
		}
	}
	if content < 2 {
		return 0
	}
	return float64(prose) / float64(content)
}

// tableStdStructureScore credits regular structure: the fraction of rows whose
// non-empty cell count equals the modal count.
func tableStdStructureScore(data Data) float64 {
	grid := data.Grid()
	if data.NumRows == 0 {
		return 0
	}
	counts := make(map[int]int, data.NumRows)
	for row := 0; row < data.NumRows; row++ {
		n := 0
		for col := 0; col < data.NumCols; col++ {
			if strings.TrimSpace(grid[row][col].Text) != "" {
				n++
			}
		}
		counts[n]++
	}
	modal := 0
	for _, c := range counts {
		if c > modal {
			modal = c
		}
	}
	return float64(modal) / float64(data.NumRows)
}

// tableHeaderDistinctScore credits a header row distinct from the body: row 0's
// mean words-per-cell being shorter than the data rows' (a label header above
// longer data), normalised to 0..1.
func tableHeaderDistinctScore(data Data) float64 {
	if data.NumRows < 2 {
		return 0
	}
	grid := data.Grid()
	meanWords := func(row int) float64 {
		total, n := 0, 0
		for col := 0; col < data.NumCols; col++ {
			if w := len(strings.Fields(grid[row][col].Text)); w > 0 {
				total += w
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return float64(total) / float64(n)
	}
	header := meanWords(0)
	body, rows := 0.0, 0
	for row := 1; row < data.NumRows; row++ {
		body += meanWords(row)
		rows++
	}
	if rows == 0 || body <= 0 {
		return 0
	}
	body /= float64(rows)
	if header >= body {
		return 0
	}
	return (body - header) / body
}

// tableKeyValueScore credits a key/value shape: the fraction of first-column
// data cells that are short keys (<= 4 words).
func tableKeyValueScore(data Data) float64 {
	if data.NumRows < 2 || data.NumCols < 2 {
		return 0
	}
	grid := data.Grid()
	short, total := 0, 0
	for row := 1; row < data.NumRows; row++ {
		text := strings.TrimSpace(grid[row][0].Text)
		if text == "" {
			continue
		}
		total++
		if len(strings.Fields(text)) <= 4 {
			short++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(short) / float64(total)
}

// tableHasMultiColumnProse reports whether a detected borderless table is more
// likely multi-column page prose than a real table: at least two columns are
// "prose" (contain a cell with a long, > proseColumnWordCount, run of words) AND
// prose columns make up at least two-thirds of all columns. A genuine table
// confines long prose to a minority of columns (e.g. one or two description
// columns beside short key/label/numeric columns), whereas body text wrongly
// gridded into a fake table is long prose in (nearly) every column. Both the
// count and the fraction are required so a wide table with a couple of
// description columns (e.g. "Stage | Name | Explanation | Benefit") is kept
// while a 3-column slab of paragraphs is rejected. Document-general
// (geometry/structure; no literal-text or corpus-specific signal) guard against
// the dominant borderless false positive.
func tableHasMultiColumnProse(data Data) bool {
	const proseColumnWordCount = 20
	if data.NumCols < 2 || data.NumRows < 1 {
		return false
	}
	grid := data.Grid()
	proseColumns := 0
	for col := 0; col < data.NumCols; col++ {
		for row := 0; row < data.NumRows; row++ {
			if len(strings.Fields(grid[row][col].Text)) > proseColumnWordCount {
				proseColumns++
				break
			}
		}
	}
	return proseColumns >= 2 && proseColumns*3 >= data.NumCols*2
}

func DetectAnchoredTextTables(lineCells, tokenCells []page.TextCell, options DetectionOptions) DetectionResult {
	options = normaliseDetectionOptions(options)
	if len(lineCells) == 0 || len(tokenCells) == 0 {
		return DetectionResult{TextCells: append([]page.TextCell(nil), lineCells...)}
	}

	rows := groupTextRows(lineCells, options.RowTolerance)
	tables := make([]DetectedTable, 0)
	// lineBuilt marks, per detected table, whether its grid was derived from
	// LINE-cell structure (the gutter-persistence gate only judges those; a
	// word-token grid's merged line runs are legitimate, not prose crossing
	// gutters).
	lineBuilt := make([]bool, 0)
	tableCellIndexes := make(map[int]bool)
	minAnchorCols := anchoredMinCols(options)

	appendTables := func(detected []DetectedTable, isLineBuilt bool) {
		for _, table := range detected {
			tables = append(tables, table)
			lineBuilt = append(lineBuilt, isLineBuilt)
			for _, cell := range table.TextCells {
				tableCellIndexes[cell.Index] = true
			}
		}
	}

	for index := 0; index < len(rows); {
		if len(rows[index].Cells) < minAnchorCols {
			index++
			continue
		}

		start, end := anchoredRowBand(rows, index)
		if end-start < options.MinRows {
			index++
			continue
		}
		if !hasNearbyTableCaption(rows, end) {
			index++
			continue
		}

		if detected, ok := buildAnchoredDetectedTable(rows[start:end], tokenCells, minAnchorCols, options); ok {
			tables = append(tables, detected)
			lineBuilt = append(lineBuilt, false)
			for _, row := range rows[start:end] {
				for _, cell := range row.Cells {
					tableCellIndexes[cell.Index] = true
				}
			}
			index = end
			continue
		}
		index++
	}

	if len(tables) == 0 {
		appendTables(detectMultilineNumericContinuationTables(rows, options, tableCellIndexes), true)
	}

	if len(tables) == 0 {
		appendTables(detectWideMultilineTextTables(rows, options, tableCellIndexes), true)
	}

	if len(tables) == 0 {
		appendTables(detectCaptionlessThreeColumnMultilineTextTables(rows, options, tableCellIndexes), true)
	}

	if len(tables) == 0 {
		appendTables(detectMergedHeaderTextTables(rows, tokenCells, options, tableCellIndexes), true)
	}

	appendTables(detectCompactWordGridTables(rows, tokenCells, options, tableCellIndexes), false)

	appendTables(detectShortCaptionedWordGridTables(rows, tokenCells, options, tableCellIndexes), false)

	appendTables(detectCaptionBeforeMultilineTables(rows, options, tableCellIndexes), true)

	appendTables(detectLabelValueTextTables(rows, options, tableCellIndexes), true)

	// Suppress equation/prose grids whose column boundaries are not persistent
	// whitespace gutters (see the gutter-persistence invariant above). Runs
	// before dropProseSlabTables so the lineBuilt flags stay aligned with tables.
	tables = dropGutterCrossingTablesWhere(tables, lineBuilt, tokenCells, tableCellIndexes, options)

	// Suppress the dominant borderless false positive — multi-column page prose
	// (a two-column bibliography or body text split into a fake grid) — on the
	// anchored path. But spare a genuine description-heavy table announced by a
	// "Table N" caption (e.g. "[Table 2.2.4.A] Evaluations …"): dropProseSlabTables
	// drops only captionless prose slabs, releasing their cells back to TextCells.
	// The cruder dropMultiColumnProseTables (used by DetectTextTables) would also
	// nuke the real captioned description tables that real-world documents contain.
	tables = dropProseSlabTables(tables, lineCells, tableCellIndexes)

	return DetectionResult{
		Tables:    tables,
		TextCells: unclaimedTextCells(lineCells, tableCellIndexes),
	}
}

func detectRuledTables(cells []page.TextCell, rulings []page.RulingSegment, options DetectionOptions) DetectionResult {
	horizontal, vertical := axisAlignedRulings(rulings)
	if len(horizontal) < 2 || len(vertical) < 2 {
		return DetectionResult{}
	}

	xCoords := snappedPositions(vertical, options.ColumnTolerance)
	yCoords := snappedPositions(horizontal, options.RowTolerance)
	if len(xCoords) < 2 || len(yCoords) < 2 {
		return DetectionResult{}
	}

	left, right := xCoords[0], xCoords[len(xCoords)-1]
	top, bottom := yCoords[0], yCoords[len(yCoords)-1]
	if !spansGrid(horizontal, yCoords, left, right, options.ColumnTolerance) ||
		!spansGrid(vertical, xCoords, top, bottom, options.RowTolerance) {
		return DetectionResult{}
	}

	tableBox := geom.Box{L: left, T: top, R: right, B: bottom, Origin: geom.TopLeft}
	sourceCells := containedTextCells(cells, tableBox, options.TextOverlapThreshold)
	if len(sourceCells) == 0 {
		return DetectionResult{}
	}

	rowBoxes := make([]geom.Box, 0, len(yCoords)-1)
	for i := 0; i+1 < len(yCoords); i++ {
		rowBoxes = append(rowBoxes, geom.Box{L: left, T: yCoords[i], R: right, B: yCoords[i+1], Origin: geom.TopLeft})
	}
	colBoxes := make([]geom.Box, 0, len(xCoords)-1)
	for i := 0; i+1 < len(xCoords); i++ {
		colBoxes = append(colBoxes, geom.Box{L: xCoords[i], T: top, R: xCoords[i+1], B: bottom, Origin: geom.TopLeft})
	}
	if len(rowBoxes) < 2 || len(colBoxes) < 2 {
		return DetectionResult{}
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	}).WithAssignedText(sourceCells, options.TextOverlapThreshold)

	tableCellIndexes := make(map[int]bool)
	for _, cell := range sourceCells {
		tableCellIndexes[cell.Index] = true
	}
	return DetectionResult{
		Tables: []DetectedTable{{
			Data:      data,
			Box:       tableBox,
			TextCells: sourceCells,
		}},
		TextCells: unclaimedTextCells(cells, tableCellIndexes),
	}
}

func axisAlignedRulings(rulings []page.RulingSegment) ([]rulingLine, []rulingLine) {
	const axisTolerance = 1.5
	const minLength = 8.0

	horizontal := make([]rulingLine, 0)
	vertical := make([]rulingLine, 0)
	for _, segment := range rulings {
		if segment.Origin != "" && segment.Origin != geom.TopLeft {
			continue
		}
		dx := math.Abs(segment.ToX - segment.FromX)
		dy := math.Abs(segment.ToY - segment.FromY)
		switch {
		case dx >= minLength && dy <= axisTolerance:
			start, end := ordered(segment.FromX, segment.ToX)
			horizontal = append(horizontal, rulingLine{Position: (segment.FromY + segment.ToY) / 2, Start: start, End: end})
		case dy >= minLength && dx <= axisTolerance:
			start, end := ordered(segment.FromY, segment.ToY)
			vertical = append(vertical, rulingLine{Position: (segment.FromX + segment.ToX) / 2, Start: start, End: end})
		}
	}
	return horizontal, vertical
}

func snappedPositions(lines []rulingLine, tolerance float64) []float64 {
	positions := make([]float64, 0, len(lines))
	for _, line := range lines {
		positions = append(positions, line.Position)
	}
	sort.Float64s(positions)
	if len(positions) == 0 {
		return nil
	}
	if tolerance <= 0 {
		tolerance = 2
	}

	out := make([]float64, 0, len(positions))
	clusterStart := 0
	for i := 1; i <= len(positions); i++ {
		if i < len(positions) && math.Abs(positions[i]-positions[i-1]) <= tolerance {
			continue
		}
		total := 0.0
		for _, position := range positions[clusterStart:i] {
			total += position
		}
		out = append(out, total/float64(i-clusterStart))
		clusterStart = i
	}
	return out
}

func spansGrid(lines []rulingLine, positions []float64, start, end, tolerance float64) bool {
	for _, position := range positions {
		found := false
		for _, line := range lines {
			if math.Abs(line.Position-position) <= tolerance && line.Start <= start+tolerance && line.End >= end-tolerance {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containedTextCells(cells []page.TextCell, tableBox geom.Box, threshold float64) []page.TextCell {
	out := make([]page.TextCell, 0)
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		if cell.Box.IntersectionOverSelf(tableBox) > threshold {
			out = append(out, cell)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out
}

func unclaimedTextCells(cells []page.TextCell, claimed map[int]bool) []page.TextCell {
	out := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if !claimed[cell.Index] {
			out = append(out, cell)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out
}

func ordered(a, b float64) (float64, float64) {
	if a <= b {
		return a, b
	}
	return b, a
}

func detectContinuationTextTables(cells []page.TextCell, options DetectionOptions) []DetectedTable {
	rows := groupTextRows(cells, options.RowTolerance)
	tables := make([]DetectedTable, 0)
	claimed := make(map[int]bool)

	for start := 0; start < len(rows); start++ {
		if rowHasClaimedCell(rows[start], claimed) || !isContinuationHeaderRow(rows[start]) {
			continue
		}

		detected, end, ok := buildContinuationTextTable(rows, start, options, claimed)
		if !ok {
			continue
		}

		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		if end > start {
			start = end - 1
		}
	}

	return tables
}

func rowHasClaimedCell(row textline.ParagraphTextLine, claimed map[int]bool) bool {
	for _, cell := range row.Cells {
		if claimed[cell.Index] {
			return true
		}
	}
	return false
}

func isContinuationHeaderRow(row textline.ParagraphTextLine) bool {
	if len(row.Cells) != 2 {
		return false
	}
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || strings.HasSuffix(text, ".") || len(strings.Fields(text)) > 4 || isStandaloneListMarker(text) {
			return false
		}
	}
	return true
}

func buildContinuationTextTable(rows []textline.ParagraphTextLine, start int, options DetectionOptions, claimed map[int]bool) (DetectedTable, int, bool) {
	headerCells := append([]page.TextCell(nil), rows[start].Cells...)
	sort.SliceStable(headerCells, func(i, j int) bool {
		return headerCells[i].Box.L < headerCells[j].Box.L
	})
	boundary := (headerCells[0].Box.R + headerCells[1].Box.L) / 2
	gapLimit := continuationRowGapLimit(rows[start])

	logicalRows := make([]continuationLogicalRow, 0)
	sourceCells := make([]page.TextCell, 0)
	dataRowsWithBoth := 0
	dataRowsWithRight := 0
	numberedRightRows := 0
	rightOnlyRows := 0
	end := start

	for end < len(rows) {
		row := rows[end]
		if rowHasClaimedCell(row, claimed) {
			break
		}
		if end > start && math.Abs(row.Center-rows[end-1].Center) > gapLimit {
			break
		}

		leftCells, rightCells, ok := classifyContinuationRow(row, boundary)
		if !ok {
			if validContinuationShape(logicalRows, dataRowsWithBoth, dataRowsWithRight, numberedRightRows, rightOnlyRows, options) {
				break
			}
			return DetectedTable{}, start, false
		}

		switch {
		case len(leftCells) == 1 && len(rightCells) == 1:
			logicalRows = append(logicalRows, continuationLogicalRow{
				Box:      enclosingTextCells(row.Cells),
				Cells:    append([]page.TextCell(nil), row.Cells...),
				HasLeft:  true,
				HasRight: true,
			})
			if end > start {
				dataRowsWithBoth++
				dataRowsWithRight++
				if startsNumberedOutline(rightCells[0].Text) {
					numberedRightRows++
				}
			}
			sourceCells = append(sourceCells, row.Cells...)
		case len(leftCells) == 0 && len(rightCells) == 1:
			logicalRows = append(logicalRows, continuationLogicalRow{
				Box:      enclosingTextCells(rightCells),
				Cells:    append([]page.TextCell(nil), rightCells...),
				HasRight: true,
			})
			dataRowsWithRight++
			if startsNumberedOutline(rightCells[0].Text) {
				numberedRightRows++
			}
			rightOnlyRows++
			sourceCells = append(sourceCells, rightCells...)
		case len(leftCells) == 1 && len(rightCells) == 0:
			if len(logicalRows) <= 1 || !logicalRows[len(logicalRows)-1].HasLeft {
				if validContinuationShape(logicalRows, dataRowsWithBoth, dataRowsWithRight, numberedRightRows, rightOnlyRows, options) {
					break
				}
				return DetectedTable{}, start, false
			}
			current := &logicalRows[len(logicalRows)-1]
			current.Box = unionBoxes(current.Box, enclosingTextCells(leftCells))
			current.Cells = append(current.Cells, leftCells...)
			sourceCells = append(sourceCells, leftCells...)
		default:
			if validContinuationShape(logicalRows, dataRowsWithBoth, dataRowsWithRight, numberedRightRows, rightOnlyRows, options) {
				break
			}
			return DetectedTable{}, start, false
		}

		end++
	}

	if !validContinuationShape(logicalRows, dataRowsWithBoth, dataRowsWithRight, numberedRightRows, rightOnlyRows, options) {
		return DetectedTable{}, start, false
	}

	sort.SliceStable(sourceCells, func(i, j int) bool {
		return sourceCells[i].Index < sourceCells[j].Index
	})
	tableBox := enclosingTextCells(sourceCells)
	if !(tableBox.L < boundary && boundary < tableBox.R) {
		return DetectedTable{}, start, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}
	colBoxes := []geom.Box{
		{L: tableBox.L, T: tableBox.T, R: boundary, B: tableBox.B, Origin: geom.TopLeft},
		{L: boundary, T: tableBox.T, R: tableBox.R, B: tableBox.B, Origin: geom.TopLeft},
	}

	data := assignContinuationText(FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	}), logicalRows, boundary)

	return DetectedTable{
		Data:      data,
		Box:       tableBox,
		TextCells: sourceCells,
	}, end, true
}

func validContinuationShape(rows []continuationLogicalRow, dataRowsWithBoth, dataRowsWithRight, numberedRightRows, rightOnlyRows int, options DetectionOptions) bool {
	return len(rows) >= options.MinRows &&
		dataRowsWithBoth >= 2 &&
		rightOnlyRows > 0 &&
		numberedRightRows >= 2 &&
		numberedRightRows*2 >= dataRowsWithRight
}

func detectLabelValueTextTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for start := 0; start < len(rows); start++ {
		if rowHasClaimedCell(rows[start], claimed) || !isLabelValueAnchorRow(rows[start]) {
			continue
		}

		detected, end, ok := buildLabelValueTextTable(rows, start, options, claimed)
		if !ok {
			continue
		}

		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		if end > start {
			start = end - 1
		}
	}
	return tables
}

func isLabelValueAnchorRow(row textline.ParagraphTextLine) bool {
	boundary, ok := labelValueBoundary(row)
	if !ok {
		return false
	}
	leftCells, rightCells, ok := classifyLabelValueRow(row, boundary)
	return ok && len(leftCells) > 0 && len(rightCells) > 0 && labelValueCellsAreLabel(leftCells)
}

func buildLabelValueTextTable(rows []textline.ParagraphTextLine, start int, options DetectionOptions, claimed map[int]bool) (DetectedTable, int, bool) {
	boundary, ok := labelValueBoundary(rows[start])
	if !ok {
		return DetectedTable{}, start, false
	}

	logicalRows := make([]continuationLogicalRow, 0)
	sourceCells := make([]page.TextCell, 0)
	rowsWithBoth := 0
	leftOnlyRows := 0
	rightOnlyRows := 0
	numberedRightRows := 0
	multiNumericRightRows := 0
	end := start

scanRows:
	for end < len(rows) {
		row := rows[end]
		if rowHasClaimedCell(row, claimed) {
			break
		}
		if end > start && math.Abs(row.Center-rows[end-1].Center) > labelValueRowGapLimit(rows[end-1]) {
			break
		}

		leftCells, rightCells, ok := classifyLabelValueRow(row, boundary)
		if !ok {
			if validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
				break scanRows
			}
			return DetectedTable{}, start, false
		}

		switch {
		case len(leftCells) > 0 && len(rightCells) > 0:
			if !labelValueCellsAreLabel(leftCells) {
				if validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
					break scanRows
				}
				return DetectedTable{}, start, false
			}
			logicalRows = append(logicalRows, continuationLogicalRow{
				Box:      enclosingTextCells(row.Cells),
				Cells:    append([]page.TextCell(nil), row.Cells...),
				HasLeft:  true,
				HasRight: true,
			})
			rowsWithBoth++
			if labelValueRightStartsNumberedOutline(rightCells) {
				numberedRightRows++
			}
			if labelValueRightNumericTokenCount(rightCells) >= 3 {
				multiNumericRightRows++
			}
			sourceCells = append(sourceCells, row.Cells...)
		case len(leftCells) > 0 && len(rightCells) == 0:
			if validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
				break scanRows
			}
			if !labelValueCellsAreLabel(leftCells) {
				if validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
					break scanRows
				}
				return DetectedTable{}, start, false
			}
			logicalRows = append(logicalRows, continuationLogicalRow{
				Box:     enclosingTextCells(leftCells),
				Cells:   append([]page.TextCell(nil), leftCells...),
				HasLeft: true,
			})
			leftOnlyRows++
			sourceCells = append(sourceCells, leftCells...)
		case len(leftCells) == 0 && len(rightCells) > 0:
			if len(logicalRows) == 0 {
				return DetectedTable{}, start, false
			}
			current := &logicalRows[len(logicalRows)-1]
			current.Box = unionBoxes(current.Box, enclosingTextCells(rightCells))
			current.Cells = append(current.Cells, rightCells...)
			current.HasRight = true
			rightOnlyRows++
			if labelValueRightStartsNumberedOutline(rightCells) {
				numberedRightRows++
			}
			if labelValueRightNumericTokenCount(rightCells) >= 3 {
				multiNumericRightRows++
			}
			sourceCells = append(sourceCells, rightCells...)
		default:
			if validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
				break scanRows
			}
			return DetectedTable{}, start, false
		}

		end++
	}

	if !validLabelValueShape(logicalRows, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows, options) {
		return DetectedTable{}, start, false
	}

	sort.SliceStable(sourceCells, func(i, j int) bool {
		return sourceCells[i].Index < sourceCells[j].Index
	})
	tableBox := enclosingTextCells(sourceCells)
	if !(tableBox.L < boundary && boundary < tableBox.R) {
		return DetectedTable{}, start, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}
	colBoxes := []geom.Box{
		{L: tableBox.L, T: tableBox.T, R: boundary, B: tableBox.B, Origin: geom.TopLeft},
		{L: boundary, T: tableBox.T, R: tableBox.R, B: tableBox.B, Origin: geom.TopLeft},
	}

	data := assignLabelValueText(FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	}), logicalRows, boundary)

	return DetectedTable{
		Data:      data,
		Box:       tableBox,
		TextCells: sourceCells,
	}, end, true
}

func labelValueBoundary(row textline.ParagraphTextLine) (float64, bool) {
	cells := append([]page.TextCell(nil), row.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Box.L < cells[j].Box.L
	})
	if len(cells) < 2 {
		return 0, false
	}

	bestIndex := -1
	bestGap := 0.0
	for index := 0; index < len(cells)-1; index++ {
		gap := cells[index+1].Box.L - cells[index].Box.R
		if gap > bestGap {
			bestGap = gap
			bestIndex = index
		}
	}
	if bestIndex < 0 || bestGap < 16 {
		return 0, false
	}
	return (cells[bestIndex].Box.R + cells[bestIndex+1].Box.L) / 2, true
}

func classifyLabelValueRow(row textline.ParagraphTextLine, boundary float64) ([]page.TextCell, []page.TextCell, bool) {
	leftCells := make([]page.TextCell, 0)
	rightCells := make([]page.TextCell, 0)
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if cell.Box.CenterX() < boundary {
			leftCells = append(leftCells, cell)
			continue
		}
		rightCells = append(rightCells, cell)
	}
	return leftCells, rightCells, len(leftCells) > 0 || len(rightCells) > 0
}

func labelValueCellsAreLabel(cells []page.TextCell) bool {
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if text := strings.TrimSpace(cell.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return isLabelValueLabelText(strings.Join(parts, " "))
}

func isLabelValueLabelText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasSuffix(text, ".") || isStandaloneListMarker(text) || isNumericTableCell(text) {
		return false
	}
	if strings.ContainsAny(text, "=()[]{}∂◦") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > 5 {
		return false
	}
	meaningfulWords := 0
	for _, field := range fields {
		word := strings.Trim(field, ":,;")
		if word == "" || isLabelValueConnector(word) {
			continue
		}
		if !startsWithUppercaseLetter(word) {
			return false
		}
		meaningfulWords++
	}
	if meaningfulWords == 0 {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isLabelValueConnector(word string) bool {
	switch strings.ToLower(strings.Trim(word, ":,;")) {
	case "and", "or", "of", "the", "to", "in", "for":
		return true
	default:
		return false
	}
}

func startsWithUppercaseLetter(word string) bool {
	for _, r := range word {
		if !unicode.IsLetter(r) {
			if unicode.IsDigit(r) {
				return false
			}
			continue
		}
		return unicode.IsUpper(r)
	}
	return false
}

func containsDigit(text string) bool {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func labelValueRowGapLimit(row textline.ParagraphTextLine) float64 {
	limit := continuationRowGapLimit(row)
	if limit < 84 {
		return 84
	}
	return limit
}

func labelValueRightStartsNumberedOutline(cells []page.TextCell) bool {
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if text := strings.TrimSpace(cell.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return startsNumberedOutline(strings.Join(parts, " "))
}

func labelValueRightNumericTokenCount(cells []page.TextCell) int {
	count := 0
	for _, cell := range cells {
		for field := range strings.FieldsSeq(cell.Text) {
			token := strings.Trim(field, ".,;:%()")
			if isNumericTableCell(token) {
				count++
			}
		}
	}
	return count
}

func validLabelValueShape(rows []continuationLogicalRow, rowsWithBoth, leftOnlyRows, rightOnlyRows, numberedRightRows, multiNumericRightRows int, options DetectionOptions) bool {
	if len(rows) < options.MinRows || rowsWithBoth < 3 || leftOnlyRows == 0 || rightOnlyRows < 3 || numberedRightRows > 0 || multiNumericRightRows > 0 {
		return false
	}
	return true
}

func assignLabelValueText(data Data, rows []continuationLogicalRow, boundary float64) Data {
	texts := make([][2]string, len(rows))
	for rowIndex, row := range rows {
		leftParts, rightParts := labelValueTextParts(row.Cells, boundary)
		texts[rowIndex][0] = strings.Join(leftParts, " ")
		texts[rowIndex][1] = strings.Join(rightParts, " ")
	}

	for index := range data.Cells {
		cell := &data.Cells[index]
		if cell.StartRow < 0 || cell.StartRow >= len(texts) || cell.StartCol < 0 || cell.StartCol > 1 {
			continue
		}
		cell.Text = texts[cell.StartRow][cell.StartCol]
	}
	return data
}

func labelValueTextParts(cells []page.TextCell, boundary float64) ([]string, []string) {
	leftParts := make([]string, 0)
	rightParts := make([]string, 0)
	for _, row := range groupTextRows(cells, 4) {
		for _, cell := range row.Cells {
			text := strings.TrimSpace(cell.Text)
			if text == "" {
				continue
			}
			if cell.Box.CenterX() < boundary {
				leftParts = append(leftParts, text)
				continue
			}
			rightParts = append(rightParts, text)
		}
	}
	return leftParts, rightParts
}

func assignContinuationText(data Data, rows []continuationLogicalRow, boundary float64) Data {
	texts := make([][2]string, len(rows))
	for rowIndex, row := range rows {
		leftParts, rightParts := continuationTextParts(row.Cells, boundary)
		texts[rowIndex][0] = strings.Join(leftParts, " ")
		texts[rowIndex][1] = strings.Join(rightParts, " ")
	}

	for index := range data.Cells {
		cell := &data.Cells[index]
		if cell.StartRow < 0 || cell.StartRow >= len(texts) || cell.StartCol < 0 || cell.StartCol > 1 {
			continue
		}
		cell.Text = texts[cell.StartRow][cell.StartCol]
	}
	return data
}

func continuationTextParts(cells []page.TextCell, boundary float64) ([]string, []string) {
	orderedCells := append([]page.TextCell(nil), cells...)
	sort.SliceStable(orderedCells, func(i, j int) bool {
		return orderedCells[i].Index < orderedCells[j].Index
	})

	leftParts := make([]string, 0)
	rightParts := make([]string, 0)
	for _, cell := range orderedCells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if cell.Box.CenterX() < boundary {
			leftParts = append(leftParts, text)
			continue
		}
		rightParts = append(rightParts, text)
	}
	return leftParts, rightParts
}

func continuationRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 4
	if limit < 36 {
		return 36
	}
	return limit
}

func classifyContinuationRow(row textline.ParagraphTextLine, boundary float64) ([]page.TextCell, []page.TextCell, bool) {
	if len(row.Cells) == 0 || len(row.Cells) > 2 {
		return nil, nil, false
	}

	leftCells := make([]page.TextCell, 0, 1)
	rightCells := make([]page.TextCell, 0, 1)
	for _, cell := range row.Cells {
		if cell.Box.CenterX() < boundary {
			leftCells = append(leftCells, cell)
			continue
		}
		rightCells = append(rightCells, cell)
	}
	if len(leftCells) > 1 || len(rightCells) > 1 {
		return nil, nil, false
	}
	return leftCells, rightCells, true
}

func unionBoxes(left, right geom.Box) geom.Box {
	return geom.EnclosingBox(left, right)
}

func isStandaloneListMarker(text string) bool {
	text = strings.TrimSpace(text)
	switch text {
	case "•", "-", "*", "·", "‣", "▪", "–":
		return true
	}
	return startsNumberedOutline(text) && len(strings.Fields(text)) == 1
}

func startsNumberedOutline(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}

	token := strings.TrimRight(fields[0], ":")
	hasMarker := strings.Contains(token, ".")
	if strings.HasSuffix(token, ")") {
		hasMarker = true
		token = strings.TrimSuffix(token, ")")
	}
	token = strings.TrimSuffix(token, ".")
	if !hasMarker || token == "" {
		return false
	}

	for part := range strings.SplitSeq(token, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !unicode.IsDigit(r) {
				return false
			}
		}
	}
	return true
}

func normaliseDetectionOptions(options DetectionOptions) DetectionOptions {
	if options.MinRows <= 0 {
		options.MinRows = 4
	}
	if options.MinCols <= 0 {
		options.MinCols = 2
	}
	if options.RowTolerance <= 0 {
		options.RowTolerance = 4
	}
	if options.ColumnTolerance <= 0 {
		options.ColumnTolerance = 8
	}
	if options.TextOverlapThreshold <= 0 {
		options.TextOverlapThreshold = 0.3
	}
	if options.MaxRowFillRatio <= 0 {
		options.MaxRowFillRatio = 0.75
	}
	return options
}

func anchoredMinCols(options DetectionOptions) int {
	if options.MinCols > 2 {
		return options.MinCols
	}
	return 3
}

func hasNearbyTableCaption(rows []textline.ParagraphTextLine, end int) bool {
	return slices.ContainsFunc(rows[end:min(len(rows), end+2)], rowHasTableCaptionCue)
}

func rowHasTableCaptionCue(row textline.ParagraphTextLine) bool {
	parts := make([]string, 0, len(row.Cells))
	for _, cell := range row.Cells {
		if text := strings.TrimSpace(cell.Text); text != "" {
			parts = append(parts, text)
		}
	}
	text := strings.ToLower(strings.Join(parts, " "))
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(tokens) == 0 {
		return false
	}
	return tokens[0] == "table" || tokens[0] == "tab"
}

func anchoredRowBand(rows []textline.ParagraphTextLine, anchorIndex int) (int, int) {
	gapLimit := anchoredRowGapLimit(rows[anchorIndex])
	start := anchorIndex
	for start > 0 && math.Abs(rows[start].Center-rows[start-1].Center) <= gapLimit {
		start--
	}
	end := anchorIndex + 1
	for end < len(rows) && math.Abs(rows[end].Center-rows[end-1].Center) <= gapLimit {
		end++
	}
	return start, end
}

func anchoredRowGapLimit(anchor textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range anchor.Cells {
		height += cell.Box.Height()
	}
	if len(anchor.Cells) > 0 {
		height /= float64(len(anchor.Cells))
	}
	limit := height * 2.5
	if limit < 12 {
		return 12
	}
	return limit
}

func buildAnchoredDetectedTable(rows []textline.ParagraphTextLine, tokenCells []page.TextCell, minAnchorCols int, options DetectionOptions) (DetectedTable, bool) {
	anchor, ok := strongestAnchorRow(rows, minAnchorCols)
	if !ok {
		return DetectedTable{}, false
	}
	if supportedAnchorRows(rows, minAnchorCols) < 2 {
		return DetectedTable{}, false
	}

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	rowBoxes := make([]geom.Box, 0, len(rows))
	for _, row := range rows {
		rowBox := enclosingTextCells(row.Cells)
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}
	colBoxes := anchoredColumnBoxes(anchor.Cells, tableBox)
	if len(rowBoxes) < options.MinRows || len(colBoxes) < options.MinCols {
		return DetectedTable{}, false
	}

	sourceCells := containedTextCells(tokenCells, tableBox, options.TextOverlapThreshold)
	if len(sourceCells) == 0 {
		return DetectedTable{}, false
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	}).WithAssignedText(sourceCells, options.TextOverlapThreshold)

	return DetectedTable{
		Data:      data,
		Box:       tableBox,
		TextCells: sourceCells,
	}, true
}

func detectMergedHeaderTextTables(rows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for headerIndex := 0; headerIndex+1 < len(rows); headerIndex++ {
		header := rows[headerIndex]
		if rowHasClaimedCell(header, claimed) || len(header.Cells) != 1 {
			continue
		}

		dataStart := headerIndex + 1
		if len(rows[dataStart].Cells) < options.MinCols {
			continue
		}

		gapLimit := mergedHeaderRowGapLimit(rows[dataStart])
		dataEnd := dataStart
		for dataEnd < len(rows) {
			if rowHasClaimedCell(rows[dataEnd], claimed) || len(rows[dataEnd].Cells) < options.MinCols {
				break
			}
			if dataEnd > dataStart && math.Abs(rows[dataEnd].Center-rows[dataEnd-1].Center) > gapLimit {
				break
			}
			dataEnd++
		}
		if dataEnd-dataStart < mergedHeaderMinDataRows(options) {
			continue
		}
		if math.Abs(rows[dataStart].Center-header.Center) > gapLimit {
			continue
		}

		detected, ok := buildMergedHeaderTextTable(header, rows[dataStart:dataEnd], tokenCells, options)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		headerIndex = dataEnd - 1
	}
	return tables
}

func mergedHeaderRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 3
	if limit < 24 {
		return 24
	}
	return limit
}

func mergedHeaderMinDataRows(options DetectionOptions) int {
	minRows := options.MinRows - 1
	if minRows < 3 {
		return 3
	}
	return minRows
}

func buildMergedHeaderTextTable(header textline.ParagraphTextLine, dataRows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions) (DetectedTable, bool) {
	if len(dataRows) < mergedHeaderMinDataRows(options) {
		return DetectedTable{}, false
	}
	clusters := clusterColumns(dataRows, options.ColumnTolerance)
	if len(clusters) < options.MinCols {
		return DetectedTable{}, false
	}
	if !rowsFitClusters(dataRows, clusters, options) {
		return DetectedTable{}, false
	}

	sourceCells := append([]page.TextCell(nil), header.Cells...)
	sourceCells = append(sourceCells, flattenRows(dataRows)...)
	tableBox := enclosingTextCells(sourceCells)
	if !rowsHaveTableWhitespace(dataRows, tableBox, options.MaxRowFillRatio) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(dataRows)+1)
	headerBox := enclosingTextCells(header.Cells)
	headerBox.L = tableBox.L
	headerBox.R = tableBox.R
	rowBoxes = append(rowBoxes, headerBox)
	for _, row := range dataRows {
		rowBox := enclosingTextCells(row.Cells)
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	colBoxes := columnBoxesFromClusters(clusters, tableBox)
	if len(colBoxes) < options.MinCols {
		return DetectedTable{}, false
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned, ok := assignMergedHeaderText(data, header, dataRows, tokenCells, clusters, options.TextOverlapThreshold)
	if !ok {
		return DetectedTable{}, false
	}

	sort.SliceStable(sourceCells, func(i, j int) bool {
		return sourceCells[i].Index < sourceCells[j].Index
	})
	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: sourceCells,
	}, true
}

func columnBoxesFromClusters(clusters []columnCluster, tableBox geom.Box) []geom.Box {
	sortedClusters := append([]columnCluster(nil), clusters...)
	sort.SliceStable(sortedClusters, func(i, j int) bool {
		return sortedClusters[i].Center < sortedClusters[j].Center
	})

	colBoxes := make([]geom.Box, 0, len(sortedClusters))
	for index, cluster := range sortedClusters {
		left := tableBox.L
		if index > 0 {
			left = (sortedClusters[index-1].Center + cluster.Center) / 2
		}
		right := tableBox.R
		if index+1 < len(sortedClusters) {
			right = (cluster.Center + sortedClusters[index+1].Center) / 2
		}
		colBoxes = append(colBoxes, geom.Box{L: left, T: tableBox.T, R: right, B: tableBox.B, Origin: tableBox.Origin})
	}
	return colBoxes
}

func assignMergedHeaderText(data Data, header textline.ParagraphTextLine, dataRows []textline.ParagraphTextLine, tokenCells []page.TextCell, clusters []columnCluster, threshold float64) (Data, bool) {
	texts := make([][]string, len(dataRows)+1)
	for index := range texts {
		texts[index] = make([]string, len(clusters))
	}

	headerTokens := containedTextCells(tokenCells, enclosingTextCells(header.Cells), threshold)
	if len(headerTokens) < len(clusters) {
		return Data{}, false
	}
	assignCellsByColumnLeft(texts[0], headerTokens, clusters)
	if slices.Contains(texts[0], "") {
		return Data{}, false
	}
	for rowIndex, row := range dataRows {
		assignCellsByColumnLeft(texts[rowIndex+1], row.Cells, clusters)
	}

	for index := range data.Cells {
		cell := &data.Cells[index]
		if cell.StartRow < 0 || cell.StartRow >= len(texts) || cell.StartCol < 0 || cell.StartCol >= len(clusters) {
			continue
		}
		cell.Text = texts[cell.StartRow][cell.StartCol]
	}
	return data, true
}

func assignCellsByColumnLeft(out []string, cells []page.TextCell, clusters []columnCluster) {
	orderedCells := append([]page.TextCell(nil), cells...)
	sort.SliceStable(orderedCells, func(i, j int) bool {
		return orderedCells[i].Index < orderedCells[j].Index
	})

	parts := make([][]string, len(clusters))
	for _, cell := range orderedCells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		column := nearestClusterByLeft(cell, clusters)
		if column < 0 || column >= len(parts) {
			continue
		}
		parts[column] = append(parts[column], text)
	}
	for index := range parts {
		out[index] = strings.Join(parts[index], " ")
	}
}

func nearestClusterByLeft(cell page.TextCell, clusters []columnCluster) int {
	if len(clusters) == 0 {
		return -1
	}
	bestIndex := 0
	bestDistance := math.Abs(cell.Box.L - clusters[0].Center)
	for index := 1; index < len(clusters); index++ {
		distance := math.Abs(cell.Box.L - clusters[index].Center)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func detectCompactWordGridTables(rows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for captionIndex, row := range rows {
		if !rowHasTableCaptionCue(row) || captionIndex == 0 {
			continue
		}

		var best DetectedTable
		bestScore := 0
		found := false
		for _, candidate := range compactWordGridCandidateBands(rows, captionIndex, compactWordGridMinRows) {
			if rowsContainClaimedCell(candidate.Rows, claimed) {
				continue
			}

			detected, ok := buildCompactWordGridTable(candidate.Rows, tokenCells, options)
			if !ok {
				continue
			}
			detected = promotePrecedingWordGridHeaderLabel(detected, rows, candidate.Start)
			score := compactWordGridCandidateScore(detected)
			if !found || score > bestScore {
				best = detected
				bestScore = score
				found = true
			}
		}
		if !found {
			continue
		}
		tables = append(tables, best)
		for _, cell := range best.TextCells {
			claimed[cell.Index] = true
		}
	}
	return tables
}

func detectShortCaptionedWordGridTables(rows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for captionIndex, row := range rows {
		if !rowHasTableCaptionCue(row) || captionIndex < shortCaptionedWordGridRows {
			continue
		}

		start := compactWordGridBandStart(rows, captionIndex)
		if captionIndex-start != shortCaptionedWordGridRows {
			continue
		}

		candidateRows := rows[start:captionIndex]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildCompactWordGridTableWithMinRows(candidateRows, tokenCells, options, shortCaptionedWordGridRows)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
	}
	return tables
}

func compactWordGridBandStart(rows []textline.ParagraphTextLine, captionIndex int) int {
	start := captionIndex - 1
	for start > 0 && math.Abs(rows[start].Center-rows[start-1].Center) <= compactWordGridRowGapLimit(rows[start]) {
		start--
	}
	return start
}

func compactWordGridCandidateBands(rows []textline.ParagraphTextLine, captionIndex, minRows int) []compactWordGridCandidateBand {
	if captionIndex <= 0 {
		return nil
	}

	bands := make([]compactWordGridCandidateBand, 0)
	seenStarts := make(map[int]bool)
	add := func(start int) {
		if start < 0 || captionIndex-start < minRows || seenStarts[start] {
			return
		}
		seenStarts[start] = true
		bands = append(bands, compactWordGridCandidateBand{
			Start: start,
			Rows:  rows[start:captionIndex],
		})
	}

	add(compactWordGridBandStart(rows, captionIndex))

	limit := max(captionIndex-compactWordGridMaxLookback, 0)
	for index := captionIndex - 1; index >= limit; index-- {
		if rowHasTableCaptionCue(rows[index]) {
			limit = index + 1
			break
		}
	}
	for start := limit; start <= captionIndex-minRows; start++ {
		add(start)
	}
	return bands
}

func promotePrecedingWordGridHeaderLabel(detected DetectedTable, rows []textline.ParagraphTextLine, start int) DetectedTable {
	if start <= 0 || start >= len(rows) || detected.Data.NumRows == 0 || detected.Data.NumCols == 0 {
		return detected
	}
	grid := detected.Data.Grid()
	if strings.TrimSpace(grid[0][0].Text) != "" {
		return detected
	}
	previous := rows[start-1]
	if len(previous.Cells) == 0 || math.Abs(rows[start].Center-previous.Center) > compactWordGridPromotedHeaderGap(rows[start]) {
		return detected
	}

	cells := append([]page.TextCell(nil), previous.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Box.L < cells[j].Box.L
	})
	firstHeaderCell := grid[0][0]
	var label string
	var labelCell page.TextCell
	for _, cell := range cells {
		if !wordGridHeaderLabelCellAlignsWithFirstColumn(cell, firstHeaderCell) {
			continue
		}
		label = wordGridPromotableHeaderLabel(cell.Text)
		if label != "" {
			labelCell = cell
			break
		}
	}
	if label == "" {
		return detected
	}

	for index := range detected.Data.Cells {
		cell := &detected.Data.Cells[index]
		if cell.StartRow == 0 && cell.StartCol == 0 {
			cell.Text = label
			if cell.Box != nil {
				expanded := geom.EnclosingBox(*cell.Box, labelCell.Box)
				cell.Box = &expanded
			}
			break
		}
	}
	detected.Box = geom.EnclosingBox(detected.Box, enclosingTextCells(previous.Cells))
	detected.TextCells = append(append([]page.TextCell(nil), previous.Cells...), detected.TextCells...)
	return detected
}

func wordGridHeaderLabelCellAlignsWithFirstColumn(label page.TextCell, firstHeader Cell) bool {
	if firstHeader.Box == nil {
		return true
	}
	tolerance := firstHeader.Box.Width() * 0.25
	if tolerance < 6 {
		tolerance = 6
	}
	if math.Abs(label.Box.L-firstHeader.Box.L) <= tolerance {
		return true
	}
	return label.Box.IntersectionOverSelf(*firstHeader.Box) > 0.3
}

func compactWordGridPromotedHeaderGap(row textline.ParagraphTextLine) float64 {
	limit := compactWordGridRowGapLimit(row)
	if limit < 40 {
		return 40
	}
	return limit
}

func wordGridPromotableHeaderLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasSuffix(text, ".") || isStandaloneListMarker(text) {
		return ""
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	label := strings.Trim(fields[0], ":,;")
	if label == "" || isNumericTableCell(label) {
		return ""
	}
	return label
}

func compactWordGridCandidateScore(detected DetectedTable) int {
	grid := detected.Data.Grid()
	if len(grid) == 0 || len(grid[0]) == 0 {
		return 0
	}
	score := detected.Data.NumRows*100 + detected.Data.NumCols*10
	nonEmpty := 0
	numeric := 0
	empty := 0
	for _, row := range grid {
		for _, cell := range row {
			text := strings.TrimSpace(cell.Text)
			if text == "" {
				empty++
				continue
			}
			nonEmpty++
			if isNumericTableCell(text) {
				numeric++
			}
		}
	}
	score += nonEmpty * 2
	score += numeric * 3
	score -= empty

	for _, cell := range grid[0] {
		if strings.TrimSpace(cell.Text) != "" {
			score += 2
		}
	}
	return score
}

func compactWordGridRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 3
	if limit < 14 {
		return 14
	}
	return limit
}

func rowsContainClaimedCell(rows []textline.ParagraphTextLine, claimed map[int]bool) bool {
	for _, row := range rows {
		if rowHasClaimedCell(row, claimed) {
			return true
		}
	}
	return false
}

func buildCompactWordGridTable(lineRows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions) (DetectedTable, bool) {
	return buildCompactWordGridTableWithMinRows(lineRows, tokenCells, options, compactWordGridMinRows)
}

func buildCompactWordGridTableWithMinRows(lineRows []textline.ParagraphTextLine, tokenCells []page.TextCell, options DetectionOptions, minRows int) (DetectedTable, bool) {
	if len(lineRows) < minRows {
		return DetectedTable{}, false
	}

	lineCells := flattenRows(lineRows)
	tableBox := enclosingTextCells(lineCells)
	sourceTokens := containedTextCells(tokenCells, tableBox, options.TextOverlapThreshold)
	if len(sourceTokens) == 0 {
		return DetectedTable{}, false
	}

	tokenRows := groupTextRows(sourceTokens, compactWordGridTokenRowTolerance(options))
	if len(tokenRows) < minRows {
		return DetectedTable{}, false
	}

	gridRows := make([]textline.ParagraphTextLine, 0, len(tokenRows))
	for _, row := range tokenRows {
		merged := mergeCompactWordGridRow(row)
		if len(merged.Cells) == 0 {
			continue
		}
		gridRows = append(gridRows, merged)
	}
	gridRows = mergeWordGridContinuationRows(gridRows)
	if len(gridRows) < minRows {
		return DetectedTable{}, false
	}

	tolerance := compactWordGridColumnTolerance(options)
	rowBoxes := make([]geom.Box, 0, len(gridRows))
	for _, row := range gridRows {
		rowBox := enclosingTextCells(row.Cells)
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	for _, anchorMode := range wordGridAnchorModesForRows(gridRows) {
		clusters := clusterWordGridColumns(gridRows, tolerance, anchorMode)
		if len(clusters) < compactWordGridMinColumnCount(options) {
			continue
		}
		if !wordGridRowsFitClusters(gridRows, clusters, tolerance, anchorMode) {
			continue
		}
		if !wordGridColumnsHaveSupport(gridRows, clusters, tolerance, anchorMode) {
			continue
		}
		if !wordGridHasNumericEvidence(gridRows, clusters, tolerance, anchorMode) {
			continue
		}

		colBoxes := columnBoxesFromWordGridClusters(clusters, tableBox)
		if len(rowBoxes) < minRows || len(colBoxes) < compactWordGridMinColumnCount(options) {
			continue
		}

		data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
			ColumnHeaders: []geom.Box{rowBoxes[0]},
		})
		assigned, ok := assignWordGridText(data, gridRows, clusters, tolerance, anchorMode)
		if !ok {
			continue
		}

		return DetectedTable{
			Data:      assigned,
			Box:       tableBox,
			TextCells: lineCells,
		}, true
	}

	return DetectedTable{}, false
}

func mergeCompactWordGridRow(row textline.ParagraphTextLine) textline.ParagraphTextLine {
	if len(row.Cells) == 0 {
		return row
	}
	cells := append([]page.TextCell(nil), row.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Box.L < cells[j].Box.L
	})

	merged := make([]page.TextCell, 0, len(cells))
	current := cells[0]
	for _, next := range cells[1:] {
		if next.Box.L-current.Box.R <= compactWordGridMergeGap {
			current.Text = strings.TrimSpace(current.Text + " " + next.Text)
			current.Box = geom.EnclosingBox(current.Box, next.Box)
			current.Index = min(current.Index, next.Index)
			if next.FontSize > current.FontSize {
				current.FontSize = next.FontSize
			}
			continue
		}
		merged = append(merged, current)
		current = next
	}
	merged = append(merged, current)
	return textline.ParagraphTextLine{Cells: merged, Center: averageRowCenter(merged)}
}

func mergeWordGridContinuationRows(rows []textline.ParagraphTextLine) []textline.ParagraphTextLine {
	merged := make([]textline.ParagraphTextLine, 0, len(rows))
	for _, row := range rows {
		if len(merged) > 0 && isWordGridFirstColumnContinuation(row, merged[len(merged)-1]) {
			previous := &merged[len(merged)-1]
			continuation := row.Cells[0]
			previous.Cells[0].Text = strings.TrimSpace(previous.Cells[0].Text + " " + continuation.Text)
			previous.Cells[0].Box = geom.EnclosingBox(previous.Cells[0].Box, continuation.Box)
			previous.Cells[0].Index = min(previous.Cells[0].Index, continuation.Index)
			if continuation.FontSize > previous.Cells[0].FontSize {
				previous.Cells[0].FontSize = continuation.FontSize
			}
			previous.Center = averageRowCenter(previous.Cells)
			continue
		}
		merged = append(merged, row)
	}
	return merged
}

func isWordGridFirstColumnContinuation(row, previous textline.ParagraphTextLine) bool {
	if len(row.Cells) != 1 || len(previous.Cells) < 2 {
		return false
	}
	cell := row.Cells[0]
	text := strings.TrimSpace(cell.Text)
	if text == "" || isNumericTableCell(text) || rowHasTableCaptionCue(row) {
		return false
	}
	if math.Abs(row.Center-previous.Center) > compactWordGridContinuationRowGap(previous) {
		return false
	}
	first := previous.Cells[0]
	return math.Abs(cell.Box.L-first.Box.L) <= compactWordGridColumnTolerance(DetectionOptions{})
}

func compactWordGridContinuationRowGap(row textline.ParagraphTextLine) float64 {
	limit := compactWordGridRowGapLimit(row)
	if limit < 18 {
		return 18
	}
	return limit
}

func wordGridAnchorModesForRows(rows []textline.ParagraphTextLine) []wordGridAnchorMode {
	for _, row := range rows {
		for _, cell := range row.Cells {
			if hasSplitDecimalText(cell.Text) {
				return []wordGridAnchorMode{wordGridAnchorLeft, wordGridAnchorCenter}
			}
		}
	}
	return []wordGridAnchorMode{wordGridAnchorCenter, wordGridAnchorLeft}
}

func hasSplitDecimalText(text string) bool {
	runes := []rune(strings.TrimSpace(text))
	for index, r := range runes {
		if r != '.' {
			continue
		}
		prev := index - 1
		prevSpace := false
		for prev >= 0 && unicode.IsSpace(runes[prev]) {
			prevSpace = true
			prev--
		}
		next := index + 1
		nextSpace := false
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			nextSpace = true
			next++
		}
		if prev >= 0 && next < len(runes) && unicode.IsDigit(runes[prev]) && unicode.IsDigit(runes[next]) && (prevSpace || nextSpace) {
			return true
		}
	}
	return false
}

func compactWordGridColumnTolerance(options DetectionOptions) float64 {
	if options.ColumnTolerance < 12 {
		return 12
	}
	return options.ColumnTolerance
}

func compactWordGridTokenRowTolerance(options DetectionOptions) float64 {
	if options.RowTolerance < 8 {
		return 8
	}
	return options.RowTolerance
}

func compactWordGridMinColumnCount(options DetectionOptions) int {
	if options.MinCols > compactWordGridMinCols {
		return options.MinCols
	}
	return compactWordGridMinCols
}

func clusterWordGridColumns(rows []textline.ParagraphTextLine, tolerance float64, anchorMode wordGridAnchorMode) []columnCluster {
	cells := flattenRows(rows)
	sort.SliceStable(cells, func(i, j int) bool {
		return wordGridColumnAnchor(cells[i], anchorMode) < wordGridColumnAnchor(cells[j], anchorMode)
	})

	clusters := make([]columnCluster, 0)
	for _, cell := range cells {
		center := wordGridColumnAnchor(cell, anchorMode)
		if len(clusters) == 0 || math.Abs(center-clusters[len(clusters)-1].Center) > tolerance {
			clusters = append(clusters, columnCluster{Cells: []page.TextCell{cell}, Center: center})
			continue
		}

		cluster := &clusters[len(clusters)-1]
		cluster.Cells = append(cluster.Cells, cell)
		cluster.Center = averageWordGridColumnCenter(cluster.Cells, anchorMode)
	}
	return clusters
}

func averageWordGridColumnCenter(cells []page.TextCell, anchorMode wordGridAnchorMode) float64 {
	total := 0.0
	for _, cell := range cells {
		total += wordGridColumnAnchor(cell, anchorMode)
	}
	return total / float64(len(cells))
}

func wordGridRowsFitClusters(rows []textline.ParagraphTextLine, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) bool {
	minSeen := int(math.Ceil(float64(len(clusters)) * 0.75))
	if minSeen < compactWordGridMinColumnCount(DetectionOptions{}) {
		minSeen = compactWordGridMinColumnCount(DetectionOptions{})
	}
	denseRows := 0
	sparseHeaderRows := 0
	for _, row := range rows {
		seen := wordGridSeenColumns(row.Cells, clusters, tolerance, anchorMode)
		if len(seen) >= minSeen {
			denseRows++
			continue
		}
		if denseRows > 0 || !sparseWordGridHeaderRow(row, seen, clusters, tolerance, anchorMode) {
			return false
		}
		sparseHeaderRows++
	}
	if sparseHeaderRows == 0 {
		return true
	}
	return denseRows >= compactWordGridMinRows
}

func sparseWordGridHeaderRow(row textline.ParagraphTextLine, seen map[int]bool, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) bool {
	if len(row.Cells) == 0 || len(seen) == 0 || len(row.Cells) > 3 {
		return false
	}
	for _, cell := range row.Cells {
		column := nearestWordGridClusterIndex(cell, clusters, anchorMode)
		if column < 0 || math.Abs(wordGridColumnAnchor(cell, anchorMode)-clusters[column].Center) > tolerance {
			return false
		}
	}
	return true
}

func wordGridColumnsHaveSupport(rows []textline.ParagraphTextLine, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) bool {
	support := make([]int, len(clusters))
	for _, row := range rows {
		for index := range wordGridSeenColumns(row.Cells, clusters, tolerance, anchorMode) {
			support[index]++
		}
	}
	for _, count := range support {
		if count < 2 {
			return false
		}
	}
	return true
}

func wordGridSeenColumns(cells []page.TextCell, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) map[int]bool {
	seen := make(map[int]bool)
	for _, cell := range cells {
		clusterIndex := nearestWordGridClusterIndex(cell, clusters, anchorMode)
		if clusterIndex >= 0 && math.Abs(wordGridColumnAnchor(cell, anchorMode)-clusters[clusterIndex].Center) <= tolerance {
			seen[clusterIndex] = true
		}
	}
	return seen
}

func wordGridHasNumericEvidence(rows []textline.ParagraphTextLine, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) bool {
	numericCells := 0
	numericColumns := make(map[int]bool)
	for rowIndex, row := range rows {
		if rowIndex == 0 {
			continue
		}
		for _, cell := range row.Cells {
			if !isNumericTableCell(cell.Text) {
				continue
			}
			column := nearestWordGridClusterIndex(cell, clusters, anchorMode)
			if column < 0 || math.Abs(wordGridColumnAnchor(cell, anchorMode)-clusters[column].Center) > tolerance {
				continue
			}
			numericCells++
			numericColumns[column] = true
		}
	}
	return numericCells >= compactWordGridMinNumerics && len(numericColumns) >= 2
}

func isNumericTableCell(text string) bool {
	text = normaliseNumericTableCell(text)
	if text == "" {
		return false
	}
	hasDigit := false
	runes := []rune(text)
	for index, r := range runes {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		if hasDigit && index == len(runes)-1 && isNumericMagnitudeSuffix(r) {
			continue
		}
		switch r {
		case '.', ',', '-', '+', '%', '(', ')':
		default:
			return false
		}
	}
	return hasDigit
}

func normaliseNumericTableCell(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	text = strings.ReplaceAll(text, " . ", ".")
	text = strings.ReplaceAll(text, " .", ".")
	text = strings.ReplaceAll(text, ". ", ".")
	return text
}

func isNumericMagnitudeSuffix(r rune) bool {
	switch r {
	case 'K', 'M', 'B', 'T':
		return true
	default:
		return false
	}
}

func columnBoxesFromWordGridClusters(clusters []columnCluster, tableBox geom.Box) []geom.Box {
	sortedClusters := append([]columnCluster(nil), clusters...)
	sort.SliceStable(sortedClusters, func(i, j int) bool {
		return sortedClusters[i].Center < sortedClusters[j].Center
	})

	colBoxes := make([]geom.Box, 0, len(sortedClusters))
	for index, cluster := range sortedClusters {
		left := tableBox.L
		if index > 0 {
			left = (sortedClusters[index-1].Center + cluster.Center) / 2
		}
		right := tableBox.R
		if index+1 < len(sortedClusters) {
			right = (cluster.Center + sortedClusters[index+1].Center) / 2
		}
		colBoxes = append(colBoxes, geom.Box{L: left, T: tableBox.T, R: right, B: tableBox.B, Origin: tableBox.Origin})
	}
	return colBoxes
}

func assignWordGridText(data Data, rows []textline.ParagraphTextLine, clusters []columnCluster, tolerance float64, anchorMode wordGridAnchorMode) (Data, bool) {
	texts := make([][]string, len(rows))
	for index := range texts {
		texts[index] = make([]string, len(clusters))
	}

	for rowIndex, row := range rows {
		parts := make([][]string, len(clusters))
		orderedCells := append([]page.TextCell(nil), row.Cells...)
		sort.SliceStable(orderedCells, func(i, j int) bool {
			return orderedCells[i].Index < orderedCells[j].Index
		})
		for _, cell := range orderedCells {
			column := nearestWordGridClusterIndex(cell, clusters, anchorMode)
			if column < 0 || math.Abs(wordGridColumnAnchor(cell, anchorMode)-clusters[column].Center) > tolerance {
				return Data{}, false
			}
			text := strings.TrimSpace(cell.Text)
			if text != "" {
				parts[column] = append(parts[column], text)
			}
		}
		for column := range parts {
			texts[rowIndex][column] = strings.Join(parts[column], " ")
		}
	}

	for index := range data.Cells {
		cell := &data.Cells[index]
		if cell.StartRow < 0 || cell.StartRow >= len(texts) || cell.StartCol < 0 || cell.StartCol >= len(clusters) {
			continue
		}
		cell.Text = texts[cell.StartRow][cell.StartCol]
	}
	return data, true
}

func nearestWordGridClusterIndex(cell page.TextCell, clusters []columnCluster, anchorMode wordGridAnchorMode) int {
	if len(clusters) == 0 {
		return -1
	}

	center := wordGridColumnAnchor(cell, anchorMode)
	bestIndex := 0
	bestDistance := math.Abs(center - clusters[0].Center)
	for index := 1; index < len(clusters); index++ {
		distance := math.Abs(center - clusters[index].Center)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func wordGridColumnAnchor(cell page.TextCell, anchorMode wordGridAnchorMode) float64 {
	if anchorMode == wordGridAnchorLeft {
		return cell.Box.L
	}
	return cell.Box.CenterX()
}

func detectCaptionBeforeMultilineTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for captionIndex, row := range rows {
		if !rowHasTableCaptionCue(row) {
			continue
		}
		start, ok := captionBeforeTableStart(rows, captionIndex, options)
		if !ok {
			continue
		}
		end := captionBeforeTableEnd(rows, start)
		if end-start < compactWordGridMinRows {
			continue
		}

		candidateRows := rows[start:end]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildCaptionBeforeMultilineTable(candidateRows, options)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
	}
	return tables
}

func captionBeforeTableStart(rows []textline.ParagraphTextLine, captionIndex int, options DetectionOptions) (int, bool) {
	minCols := captionBeforeMinCols(options)
	limit := min(captionIndex+5, len(rows))
	for index := captionIndex + 1; index < limit; index++ {
		if rowHasTableCaptionCue(rows[index]) {
			return 0, false
		}
		if len(rows[index].Cells) >= minCols {
			return index, true
		}
	}
	return 0, false
}

func captionBeforeTableEnd(rows []textline.ParagraphTextLine, start int) int {
	end := start + 1
	for end < len(rows) {
		if rowHasTableCaptionCue(rows[end]) || rowHasTableFootnoteCue(rows[end]) {
			break
		}
		if math.Abs(rows[end].Center-rows[end-1].Center) > captionBeforeRowGapLimit(rows[end-1]) {
			break
		}
		end++
	}
	return end
}

func captionBeforeRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 4
	if limit < 28 {
		return 28
	}
	return limit
}

func captionBeforeMinCols(options DetectionOptions) int {
	if options.MinCols > 3 {
		return options.MinCols
	}
	return 3
}

func buildCaptionBeforeMultilineTable(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, bool) {
	if len(rows) < compactWordGridMinRows {
		return DetectedTable{}, false
	}
	anchor, ok := captionBeforeAnchorRow(rows, options)
	if !ok {
		return DetectedTable{}, false
	}

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	colBoxes := anchoredColumnBoxes(anchor.Cells, tableBox)
	if len(colBoxes) < captionBeforeMinCols(options) {
		return DetectedTable{}, false
	}

	logicalRows, ok := buildCaptionBeforeLogicalRows(rows, colBoxes)
	if !ok || !validCaptionBeforeLogicalRows(logicalRows, len(colBoxes), options) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned := assignCaptionBeforeText(data, logicalRows)

	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: lineCells,
	}, true
}

func captionBeforeAnchorRow(rows []textline.ParagraphTextLine, options DetectionOptions) (textline.ParagraphTextLine, bool) {
	minCols := captionBeforeMinCols(options)
	for _, row := range rows {
		if len(row.Cells) >= minCols {
			cells := append([]page.TextCell(nil), row.Cells...)
			sort.SliceStable(cells, func(i, j int) bool {
				return cells[i].Box.L < cells[j].Box.L
			})
			return textline.ParagraphTextLine{Cells: cells, Center: row.Center}, true
		}
	}
	return textline.ParagraphTextLine{}, false
}

func buildCaptionBeforeLogicalRows(rows []textline.ParagraphTextLine, colBoxes []geom.Box) ([]multilineLogicalRow, bool) {
	logicalRows := make([]multilineLogicalRow, 0, len(rows))
	for _, row := range rows {
		parts, seen, ok := captionBeforeRowParts(row, colBoxes)
		if !ok || len(seen) == 0 {
			return nil, false
		}
		startsNew := len(logicalRows) == 0 || startsCaptionBeforeLogicalRow(seen, len(colBoxes))
		if startsNew {
			logicalRows = append(logicalRows, multilineLogicalRow{
				Parts: make([][]string, len(colBoxes)),
			})
		}
		current := &logicalRows[len(logicalRows)-1]
		appendCaptionBeforeParts(current, row.Cells, parts)
	}
	return logicalRows, true
}

func captionBeforeRowParts(row textline.ParagraphTextLine, colBoxes []geom.Box) ([][]string, map[int]bool, bool) {
	parts := make([][]string, len(colBoxes))
	seen := make(map[int]bool)
	cells := append([]page.TextCell(nil), row.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Box.L < cells[j].Box.L
	})
	for _, cell := range cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		column := captionBeforeColumnIndex(cell, colBoxes)
		if column < 0 || column >= len(colBoxes) {
			return nil, nil, false
		}
		parts[column] = append(parts[column], text)
		seen[column] = true
	}
	return parts, seen, true
}

func captionBeforeColumnIndex(cell page.TextCell, colBoxes []geom.Box) int {
	center := cell.Box.CenterX()
	for index, box := range colBoxes {
		if center >= box.L && center <= box.R {
			return index
		}
	}
	if len(colBoxes) == 0 {
		return -1
	}
	bestIndex := 0
	bestDistance := math.Abs(center - colBoxes[0].CenterX())
	for index := 1; index < len(colBoxes); index++ {
		distance := math.Abs(center - colBoxes[index].CenterX())
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func startsCaptionBeforeLogicalRow(seen map[int]bool, columnCount int) bool {
	if seen[0] {
		return true
	}
	lastColumn := columnCount - 1
	return columnCount >= 3 && seen[1] && seen[lastColumn] && len(seen) >= 2
}

func appendCaptionBeforeParts(row *multilineLogicalRow, cells []page.TextCell, parts [][]string) {
	for column := range parts {
		row.Parts[column] = append(row.Parts[column], parts[column]...)
	}
	if len(row.Cells) == 0 {
		row.Box = enclosingTextCells(cells)
	} else {
		row.Box = unionBoxes(row.Box, enclosingTextCells(cells))
	}
	row.Cells = append(row.Cells, cells...)
}

func validCaptionBeforeLogicalRows(rows []multilineLogicalRow, columnCount int, options DetectionOptions) bool {
	if len(rows) < compactWordGridMinRows || columnCount < captionBeforeMinCols(options) {
		return false
	}
	if countCaptionBeforeNonEmptyColumns(rows[0]) < captionBeforeMinCols(options) {
		return false
	}
	return validCaptionBeforeNumberedLogicalRows(rows, columnCount) || validCaptionBeforeAlignedNumericRows(rows, columnCount)
}

func validCaptionBeforeNumberedLogicalRows(rows []multilineLogicalRow, columnCount int) bool {
	numberedRows := 0
	numericValueRows := 0
	lastColumn := columnCount - 1
	for _, row := range rows[1:] {
		if len(row.Parts) != columnCount {
			return false
		}
		if isIntegerTableCell(strings.Join(row.Parts[0], " ")) {
			numberedRows++
		}
		if isNumericTableCell(strings.Join(row.Parts[lastColumn], " ")) {
			numericValueRows++
		}
	}
	return numberedRows >= 2 && numericValueRows >= 2
}

func validCaptionBeforeAlignedNumericRows(rows []multilineLogicalRow, columnCount int) bool {
	numericRows := 0
	numericCells := 0
	numericColumns := make(map[int]bool)
	for _, row := range rows[1:] {
		if len(row.Parts) != columnCount {
			return false
		}
		if strings.TrimSpace(strings.Join(row.Parts[0], " ")) == "" {
			return false
		}
		rowNumericColumns := 0
		for column := 1; column < columnCount; column++ {
			text := strings.TrimSpace(strings.Join(row.Parts[column], " "))
			if text == "" {
				continue
			}
			if !isNumericTableCell(text) {
				return false
			}
			rowNumericColumns++
			numericCells++
			numericColumns[column] = true
		}
		if rowNumericColumns >= 2 {
			numericRows++
		}
	}
	return numericRows >= 2 && numericCells >= compactWordGridMinNumerics && len(numericColumns) >= 2
}

func countCaptionBeforeNonEmptyColumns(row multilineLogicalRow) int {
	count := 0
	for _, parts := range row.Parts {
		if strings.TrimSpace(strings.Join(parts, " ")) != "" {
			count++
		}
	}
	return count
}

func isIntegerTableCell(text string) bool {
	text = strings.TrimSpace(strings.ReplaceAll(text, ",", ""))
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func detectMultilineNumericContinuationTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for index := 0; index < len(rows); index++ {
		if !isMultilineNumericAnchorRow(rows[index], options) {
			continue
		}
		end := multilineNumericTableEnd(rows, index)
		if end-index < compactWordGridMinRows {
			continue
		}

		candidateRows := rows[index:end]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildMultilineNumericContinuationTable(candidateRows, options)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		index = end - 1
	}
	return tables
}

func isMultilineNumericAnchorRow(row textline.ParagraphTextLine, options DetectionOptions) bool {
	if len(row.Cells) < multilineNumericMinCols(options) {
		return false
	}
	numericCells := 0
	textCells := 0
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if isNumericTableCell(text) {
			numericCells++
		} else {
			textCells++
		}
	}
	return textCells > 0 && numericCells >= 2
}

func multilineNumericTableEnd(rows []textline.ParagraphTextLine, start int) int {
	end := start + 1
	for end < len(rows) {
		if rowHasTableCaptionCue(rows[end]) || rowHasTableFootnoteCue(rows[end]) {
			break
		}
		if math.Abs(rows[end].Center-rows[end-1].Center) > captionBeforeRowGapLimit(rows[end-1]) {
			break
		}
		end++
	}
	return end
}

func rowHasTableFootnoteCue(row textline.ParagraphTextLine) bool {
	parts := make([]string, 0, len(row.Cells))
	for _, cell := range row.Cells {
		if text := strings.TrimSpace(cell.Text); text != "" {
			parts = append(parts, text)
		}
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join(parts, " ")))
	return strings.HasPrefix(text, "source:") || strings.HasPrefix(text, "note:")
}

func buildMultilineNumericContinuationTable(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, bool) {
	if len(rows) < compactWordGridMinRows {
		return DetectedTable{}, false
	}
	anchor, ok := multilineNumericAnchorRow(rows, options)
	if !ok {
		return DetectedTable{}, false
	}

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	colBoxes := anchoredColumnBoxes(anchor.Cells, tableBox)
	if len(colBoxes) < multilineNumericMinCols(options) {
		return DetectedTable{}, false
	}

	logicalRows, ok := buildMultilineNumericLogicalRows(rows, colBoxes)
	if !ok || !validMultilineNumericLogicalRows(logicalRows, len(colBoxes), options) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned := assignCaptionBeforeText(data, logicalRows)

	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: lineCells,
	}, true
}

func multilineNumericAnchorRow(rows []textline.ParagraphTextLine, options DetectionOptions) (textline.ParagraphTextLine, bool) {
	minCols := multilineNumericMinCols(options)
	for _, row := range rows {
		if !isMultilineNumericAnchorRow(row, options) {
			continue
		}
		cells := append([]page.TextCell(nil), row.Cells...)
		sort.SliceStable(cells, func(i, j int) bool {
			return cells[i].Box.L < cells[j].Box.L
		})
		if len(cells) >= minCols {
			return textline.ParagraphTextLine{Cells: cells, Center: row.Center}, true
		}
	}
	return textline.ParagraphTextLine{}, false
}

func multilineNumericMinCols(options DetectionOptions) int {
	if options.MinCols > compactWordGridMinCols {
		return options.MinCols
	}
	return compactWordGridMinCols
}

func buildMultilineNumericLogicalRows(rows []textline.ParagraphTextLine, colBoxes []geom.Box) ([]multilineLogicalRow, bool) {
	logicalRows := make([]multilineLogicalRow, 0, len(rows))
	for _, row := range rows {
		parts, seen, ok := captionBeforeRowParts(row, colBoxes)
		if !ok || len(seen) == 0 {
			return nil, false
		}
		if startsMultilineNumericLogicalRow(parts, seen) {
			logicalRows = append(logicalRows, multilineLogicalRow{
				Parts: make([][]string, len(colBoxes)),
			})
			current := &logicalRows[len(logicalRows)-1]
			appendCaptionBeforeParts(current, row.Cells, parts)
			continue
		}
		if len(logicalRows) == 0 || !isMultilineNumericContinuationRow(parts, seen) {
			return nil, false
		}
		current := &logicalRows[len(logicalRows)-1]
		appendCaptionBeforeParts(current, row.Cells, parts)
	}
	return logicalRows, true
}

func startsMultilineNumericLogicalRow(parts [][]string, seen map[int]bool) bool {
	return seen[0] && multilineNumericValueColumns(parts) >= 2
}

func isMultilineNumericContinuationRow(parts [][]string, seen map[int]bool) bool {
	if len(seen) != 1 || !seen[0] {
		return false
	}
	return strings.TrimSpace(strings.Join(parts[0], " ")) != ""
}

func validMultilineNumericLogicalRows(rows []multilineLogicalRow, columnCount int, options DetectionOptions) bool {
	if len(rows) < compactWordGridMinRows || columnCount < multilineNumericMinCols(options) {
		return false
	}
	if hasInconsistentKeyColumnNumericContamination(rows) {
		return false
	}
	numericRows := 0
	numericCells := 0
	numericColumns := make(map[int]bool)
	for _, row := range rows {
		if len(row.Parts) != columnCount {
			return false
		}
		if strings.TrimSpace(strings.Join(row.Parts[0], " ")) == "" {
			return false
		}
		rowNumericColumns := 0
		for column := 1; column < columnCount; column++ {
			text := strings.TrimSpace(strings.Join(row.Parts[column], " "))
			if text == "" {
				continue
			}
			if !isNumericTableCell(text) {
				return false
			}
			rowNumericColumns++
			numericCells++
			numericColumns[column] = true
		}
		if rowNumericColumns >= 2 {
			numericRows++
		}
	}
	return numericRows >= compactWordGridMinRows && numericCells >= compactWordGridMinNumerics && len(numericColumns) >= 2
}

// hasInconsistentKeyColumnNumericContamination reports whether the key (left-most)
// column of the logical rows is structurally incoherent because standalone numeric
// tokens have collapsed into the text label.
//
// A coherent multi-line numeric table has a stable key column: every label is either
// a text phrase (it may embed numbers, e.g. "Less than 3 months", which still ends in
// a word) or a wholly numeric row index. When extra, sparsely-populated numeric
// columns are mis-merged into the key column, labels become a text token followed by
// trailing standalone numbers (e.g. "OTSL 6 6" or "HTML 4 4"). Such a label never
// occurs in a coherent label-value table, so its presence is a reliable signal that
// the detected column geometry does not span the rows: the band should fall back to
// prose instead of being rendered as a garbled table.
func hasInconsistentKeyColumnNumericContamination(rows []multilineLogicalRow) bool {
	for _, row := range rows {
		if len(row.Parts) == 0 {
			continue
		}
		if keyLabelHasTrailingStandaloneNumber(row.Parts[0]) {
			return true
		}
	}
	return false
}

// keyLabelHasTrailingStandaloneNumber reports whether a key-column label is a text
// label with one or more standalone numeric tokens appended to it, e.g. "OTSL 6 6".
// A label that is wholly numeric (a row number such as "1") or wholly textual is not
// considered contaminated; only the text+trailing-number mix indicates a numeric
// column that has bled into the label.
func keyLabelHasTrailingStandaloneNumber(parts []string) bool {
	tokens := strings.Fields(strings.Join(parts, " "))
	if len(tokens) < 2 {
		return false
	}
	if !isNumericTableCell(tokens[len(tokens)-1]) {
		return false
	}
	for _, token := range tokens {
		if !isNumericTableCell(token) {
			return true
		}
	}
	return false
}

func detectCaptionlessMultilineTextTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for index := 0; index < len(rows); index++ {
		if rowHasClaimedCell(rows[index], claimed) || !isCaptionlessMultilineHeaderAnchor(rows[index], options) {
			continue
		}
		end := wideMultilineTableEnd(rows, index)
		if end-index < compactWordGridMinRows {
			continue
		}
		if hasNearbyTableCaption(rows, end) {
			continue
		}

		candidateRows := rows[index:end]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildCaptionlessMultilineTextTable(candidateRows, options)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		index = end - 1
	}
	return tables
}

func isCaptionlessMultilineHeaderAnchor(row textline.ParagraphTextLine, options DetectionOptions) bool {
	if rowHasTableCaptionCue(row) || len(row.Cells) < captionlessMultilineMinColumnCount(options) {
		return false
	}
	textCells := 0
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if isNumericTableCell(text) || !isSubstantialWideHeaderCell(cell) {
			return false
		}
		textCells++
	}
	return textCells == captionlessMultilineMinColumnCount(options)
}

func captionlessMultilineMinColumnCount(options DetectionOptions) int {
	if options.MinCols > captionlessMultilineMinCols {
		return options.MinCols
	}
	return captionlessMultilineMinCols
}

func buildCaptionlessMultilineTextTable(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, bool) {
	if len(rows) < compactWordGridMinRows || !isCaptionlessMultilineHeaderAnchor(rows[0], options) {
		return DetectedTable{}, false
	}

	anchorCells := append([]page.TextCell(nil), rows[0].Cells...)
	sort.SliceStable(anchorCells, func(i, j int) bool {
		return anchorCells[i].Box.L < anchorCells[j].Box.L
	})

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	colBoxes := anchoredColumnBoxes(anchorCells, tableBox)
	if len(colBoxes) != captionlessMultilineMinColumnCount(options) {
		return DetectedTable{}, false
	}

	logicalRows, ok := buildCaptionlessMultilineLogicalRows(rows, colBoxes, options)
	if !ok || !validCaptionlessMultilineLogicalRows(logicalRows, len(colBoxes), options) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned := assignCaptionBeforeText(data, logicalRows)

	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: lineCells,
	}, true
}

func buildCaptionlessMultilineLogicalRows(rows []textline.ParagraphTextLine, colBoxes []geom.Box, options DetectionOptions) ([]multilineLogicalRow, bool) {
	parsed := make([]captionlessRowParts, 0, len(rows))
	for _, row := range rows {
		parts, seen, ok := captionBeforeRowParts(row, colBoxes)
		if !ok || len(seen) == 0 {
			return nil, false
		}
		parsed = append(parsed, captionlessRowParts{cells: row.Cells, parts: parts, seen: seen, center: row.Center})
	}
	if len(parsed) < compactWordGridMinRows {
		return nil, false
	}

	bodyAnchors := captionlessBodyAnchorIndexes(parsed, options)
	if len(bodyAnchors) < 2 {
		return nil, false
	}

	logicalRows := make([]multilineLogicalRow, 0, len(bodyAnchors)+1)
	header := multilineLogicalRow{Parts: make([][]string, len(colBoxes))}
	appendCaptionBeforeParts(&header, parsed[0].cells, parsed[0].parts)
	logicalRows = append(logicalRows, header)
	for range bodyAnchors {
		logicalRows = append(logicalRows, multilineLogicalRow{Parts: make([][]string, len(colBoxes))})
	}

	for index := 1; index < len(parsed); index++ {
		bodyIndex := captionlessBodyAnchorForParsedRow(index, parsed, bodyAnchors)
		if bodyIndex < 0 || bodyIndex+1 >= len(logicalRows) {
			return nil, false
		}
		appendCaptionBeforeParts(&logicalRows[bodyIndex+1], parsed[index].cells, parsed[index].parts)
	}
	return logicalRows, true
}

func captionlessBodyAnchorIndexes(rows []captionlessRowParts, options DetectionOptions) []int {
	anchors := make([]int, 0)
	for index := 1; index < len(rows); index++ {
		row := rows[index]
		if isCaptionlessMultilineBodyAnchor(row, options) {
			anchors = append(anchors, index)
		}
	}
	return anchors
}

func isCaptionlessMultilineBodyAnchor(row captionlessRowParts, options DetectionOptions) bool {
	if row.seen[0] && isCaptionlessMultilineBodyKey(strings.Join(row.parts[0], " ")) {
		return true
	}
	return isCaptionlessSecondaryBodyAnchor(row, captionlessMultilineMinColumnCount(options))
}

func captionlessBodyAnchorHasCompanionColumn(row captionlessRowParts) bool {
	for column := 1; column < len(row.parts); column++ {
		if strings.TrimSpace(strings.Join(row.parts[column], " ")) != "" {
			return true
		}
	}
	return false
}

func isCaptionlessSecondaryBodyAnchor(row captionlessRowParts, columnCount int) bool {
	if columnCount < 4 || row.seen[0] || !row.seen[1] || (!row.seen[2] && !row.seen[3]) {
		return false
	}
	label := strings.TrimSpace(strings.Join(row.parts[1], " "))
	fields := strings.Fields(label)
	if len(fields) < 2 || len(fields) > 6 {
		return false
	}
	if containsDigit(label) {
		return false
	}
	return startsWithUppercaseLetter(label)
}

func captionlessBodyAnchorForParsedRow(rowIndex int, rows []captionlessRowParts, anchors []int) int {
	if rowIndex >= 0 && rowIndex < len(rows) && captionlessContinuationPrefersPreviousAnchor(rows[rowIndex]) {
		if previous := previousCaptionlessBodyAnchor(rowIndex, anchors); previous >= 0 {
			return previous
		}
	}
	return nearestCaptionlessBodyAnchor(rows[rowIndex].center, rows, anchors)
}

func captionlessContinuationPrefersPreviousAnchor(row captionlessRowParts) bool {
	if row.seen[0] || row.seen[1] || len(row.parts) == 0 {
		return false
	}
	return row.seen[len(row.parts)-1]
}

func previousCaptionlessBodyAnchor(rowIndex int, anchors []int) int {
	best := -1
	for anchorIndex, anchorRow := range anchors {
		if anchorRow > rowIndex {
			break
		}
		best = anchorIndex
	}
	return best
}

func nearestCaptionlessBodyAnchor(center float64, rows []captionlessRowParts, anchors []int) int {
	if len(anchors) == 0 {
		return -1
	}
	best := 0
	bestDistance := math.Abs(center - rows[anchors[0]].center)
	for index := 1; index < len(anchors); index++ {
		distance := math.Abs(center - rows[anchors[index]].center)
		if distance < bestDistance {
			best = index
			bestDistance = distance
		}
	}
	return best
}

func validCaptionlessMultilineLogicalRows(rows []multilineLogicalRow, columnCount int, options DetectionOptions) bool {
	if len(rows) < compactWordGridMinRows || columnCount != captionlessMultilineMinColumnCount(options) {
		return false
	}
	if countCaptionBeforeNonEmptyColumns(rows[0]) < captionlessMultilineMinColumnCount(options) {
		return false
	}

	bodyRows := 0
	for _, row := range rows[1:] {
		if len(row.Parts) != columnCount {
			return false
		}
		key := strings.TrimSpace(strings.Join(row.Parts[0], " "))
		if key == "" {
			if !isCaptionlessSecondaryLogicalRow(row) {
				return false
			}
		} else if !isCaptionlessMultilineBodyKey(key) {
			return false
		}
		if countCaptionBeforeNonEmptyColumns(row) < 2 {
			return false
		}
		bodyRows++
	}
	return bodyRows >= 2
}

func isCaptionlessSecondaryLogicalRow(row multilineLogicalRow) bool {
	if len(row.Parts) < 2 {
		return false
	}
	label := strings.TrimSpace(strings.Join(row.Parts[1], " "))
	return label != "" && startsWithUppercaseLetter(label)
}

func isCaptionlessMultilineBodyKey(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 48 {
		return false
	}
	if len(strings.Fields(text)) > 6 {
		return false
	}
	hasDigit := false
	for _, r := range text {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		if unicode.IsLetter(r) || unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '.', '-', '/', '(', ')':
		default:
			return false
		}
	}
	return hasDigit
}

func detectCaptionlessThreeColumnMultilineTextTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for index := 0; index < len(rows); index++ {
		if rowHasClaimedCell(rows[index], claimed) || !isCaptionlessThreeColumnHeaderAnchor(rows[index]) {
			continue
		}
		end := captionlessThreeColumnTableEnd(rows, index)
		if end-index < compactWordGridMinRows {
			continue
		}
		candidateRows := rows[index:end]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildCaptionlessThreeColumnMultilineTextTable(candidateRows)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		index = end - 1
	}
	return tables
}

func captionlessThreeColumnTableEnd(rows []textline.ParagraphTextLine, start int) int {
	end := start + 1
	for end < len(rows) {
		if rowHasTableCaptionCue(rows[end]) || rowHasTableFootnoteCue(rows[end]) {
			break
		}
		if math.Abs(rows[end].Center-rows[end-1].Center) > captionlessThreeColumnRowGapLimit(rows[end-1]) {
			break
		}
		end++
	}
	return end
}

func captionlessThreeColumnRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 3
	if limit < 36 {
		return 36
	}
	return limit
}

func isCaptionlessThreeColumnHeaderAnchor(row textline.ParagraphTextLine) bool {
	if rowHasTableCaptionCue(row) || len(row.Cells) != 3 {
		return false
	}
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || isNumericTableCell(text) || !isSubstantialWideHeaderCell(cell) {
			return false
		}
	}
	return true
}

func buildCaptionlessThreeColumnMultilineTextTable(rows []textline.ParagraphTextLine) (DetectedTable, bool) {
	if len(rows) < compactWordGridMinRows || !isCaptionlessThreeColumnHeaderAnchor(rows[0]) {
		return DetectedTable{}, false
	}

	anchorCells := append([]page.TextCell(nil), rows[0].Cells...)
	sort.SliceStable(anchorCells, func(i, j int) bool {
		return anchorCells[i].Box.L < anchorCells[j].Box.L
	})

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	colBoxes := anchoredColumnBoxes(anchorCells, tableBox)
	if len(colBoxes) != 3 {
		return DetectedTable{}, false
	}

	logicalRows, ok := buildCaptionlessThreeColumnLogicalRows(rows, colBoxes)
	if !ok || !validCaptionlessThreeColumnLogicalRows(logicalRows) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned := assignCaptionBeforeText(data, logicalRows)

	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: lineCells,
	}, true
}

func buildCaptionlessThreeColumnLogicalRows(rows []textline.ParagraphTextLine, colBoxes []geom.Box) ([]multilineLogicalRow, bool) {
	parsed := make([]captionlessRowParts, 0, len(rows))
	for _, row := range rows {
		parts, seen, ok := captionBeforeRowParts(row, colBoxes)
		if !ok || len(seen) == 0 {
			return nil, false
		}
		parsed = append(parsed, captionlessRowParts{cells: row.Cells, parts: parts, seen: seen, center: row.Center})
	}
	if len(parsed) < compactWordGridMinRows {
		return nil, false
	}

	bodyAnchors := captionlessThreeColumnBodyAnchorIndexes(parsed)
	if len(bodyAnchors) < 2 {
		return nil, false
	}

	logicalRows := make([]multilineLogicalRow, 0, len(bodyAnchors)+1)
	header := multilineLogicalRow{Parts: make([][]string, len(colBoxes))}
	appendCaptionBeforeParts(&header, parsed[0].cells, parsed[0].parts)
	logicalRows = append(logicalRows, header)
	for range bodyAnchors {
		logicalRows = append(logicalRows, multilineLogicalRow{Parts: make([][]string, len(colBoxes))})
	}

	for index := 1; index < len(parsed); index++ {
		bodyIndex := captionlessThreeColumnBodyAnchorForParsedRow(index, parsed, bodyAnchors)
		if bodyIndex < 0 || bodyIndex+1 >= len(logicalRows) {
			return nil, false
		}
		appendCaptionBeforeParts(&logicalRows[bodyIndex+1], parsed[index].cells, parsed[index].parts)
	}
	return logicalRows, true
}

func captionlessThreeColumnBodyAnchorIndexes(rows []captionlessRowParts) []int {
	anchors := make([]int, 0)
	for index := 1; index < len(rows); index++ {
		row := rows[index]
		if isCaptionlessThreeColumnBodyAnchor(row) && captionlessThreeColumnBodyAnchorStartsRow(index, rows, anchors) {
			anchors = append(anchors, index)
		}
	}
	return anchors
}

func isCaptionlessThreeColumnBodyAnchor(row captionlessRowParts) bool {
	return row.seen[0] &&
		captionlessBodyAnchorHasCompanionColumn(row) &&
		isCaptionlessThreeColumnBodyKey(strings.Join(row.parts[0], " "))
}

func captionlessThreeColumnBodyAnchorStartsRow(index int, rows []captionlessRowParts, anchors []int) bool {
	if len(anchors) == 0 || index <= 1 || index >= len(rows) {
		return true
	}
	if !rows[index-1].seen[0] {
		return true
	}
	gap := math.Abs(rows[index].center - rows[index-1].center)
	return gap > captionlessThreeColumnContinuationGapLimit(rows[index-1])
}

func captionlessThreeColumnContinuationGapLimit(row captionlessRowParts) float64 {
	height := 0.0
	for _, cell := range row.cells {
		height += cell.Box.Height()
	}
	if len(row.cells) > 0 {
		height /= float64(len(row.cells))
	}
	limit := height * 1.8
	if limit < 18 {
		return 18
	}
	return limit
}

func captionlessThreeColumnBodyAnchorForParsedRow(rowIndex int, rows []captionlessRowParts, anchors []int) int {
	for anchorIndex, anchorRow := range anchors {
		if anchorRow == rowIndex {
			return anchorIndex
		}
	}
	if previous := previousCaptionlessBodyAnchor(rowIndex, anchors); previous >= 0 {
		return previous
	}
	return nearestCaptionlessBodyAnchor(rows[rowIndex].center, rows, anchors)
}

func validCaptionlessThreeColumnLogicalRows(rows []multilineLogicalRow) bool {
	if len(rows) < compactWordGridMinRows {
		return false
	}
	if countCaptionBeforeNonEmptyColumns(rows[0]) != 3 {
		return false
	}

	bodyRows := 0
	for _, row := range rows[1:] {
		if len(row.Parts) != 3 {
			return false
		}
		key := strings.TrimSpace(strings.Join(row.Parts[0], " "))
		if !isCaptionlessThreeColumnBodyKey(key) {
			return false
		}
		if countCaptionBeforeNonEmptyColumns(row) < 2 {
			return false
		}
		bodyRows++
	}
	return bodyRows >= 2
}

func isCaptionlessThreeColumnBodyKey(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !isNumericTableCell(text)
}

func multilineNumericValueColumns(parts [][]string) int {
	count := 0
	for column := 1; column < len(parts); column++ {
		if isNumericTableCell(strings.Join(parts[column], " ")) {
			count++
		}
	}
	return count
}

func detectWideMultilineTextTables(rows []textline.ParagraphTextLine, options DetectionOptions, claimed map[int]bool) []DetectedTable {
	tables := make([]DetectedTable, 0)
	for index := 0; index < len(rows); index++ {
		if rowHasClaimedCell(rows[index], claimed) || !isWideMultilineHeaderAnchor(rows[index], options) {
			continue
		}
		end := wideMultilineTableEnd(rows, index)
		if end-index < options.MinRows {
			continue
		}

		candidateRows := rows[index:end]
		if rowsContainClaimedCell(candidateRows, claimed) {
			continue
		}

		detected, ok := buildWideMultilineTextTable(candidateRows, options)
		if !ok {
			continue
		}
		tables = append(tables, detected)
		for _, cell := range detected.TextCells {
			claimed[cell.Index] = true
		}
		index = end - 1
	}
	return tables
}

func wideMultilineTableEnd(rows []textline.ParagraphTextLine, start int) int {
	end := start + 1
	for end < len(rows) {
		if rowHasTableCaptionCue(rows[end]) || rowHasTableFootnoteCue(rows[end]) {
			break
		}
		if math.Abs(rows[end].Center-rows[end-1].Center) > wideMultilineRowGapLimit(rows[end-1]) {
			break
		}
		end++
	}
	return end
}

func wideMultilineRowGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 3
	if limit < 24 {
		return 24
	}
	return limit
}

func buildWideMultilineTextTable(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, bool) {
	if len(rows) < options.MinRows || !isWideMultilineHeaderAnchor(rows[0], options) {
		return DetectedTable{}, false
	}

	anchorCells := append([]page.TextCell(nil), rows[0].Cells...)
	sort.SliceStable(anchorCells, func(i, j int) bool {
		return anchorCells[i].Box.L < anchorCells[j].Box.L
	})

	lineCells := flattenRows(rows)
	tableBox := enclosingTextCells(lineCells)
	colBoxes := anchoredColumnBoxes(anchorCells, tableBox)
	if len(colBoxes) < wideMultilineMinCols(options) {
		return DetectedTable{}, false
	}

	logicalRows, ok := buildWideMultilineLogicalRows(rows, colBoxes)
	if !ok || !validWideMultilineLogicalRows(logicalRows, len(colBoxes), options) {
		return DetectedTable{}, false
	}

	rowBoxes := make([]geom.Box, 0, len(logicalRows))
	for _, row := range logicalRows {
		rowBox := row.Box
		rowBox.L = tableBox.L
		rowBox.R = tableBox.R
		rowBoxes = append(rowBoxes, rowBox)
	}

	data := FromRegions(tableBox, rowBoxes, colBoxes, nil, RegionSemantics{
		ColumnHeaders: []geom.Box{rowBoxes[0]},
	})
	assigned := assignCaptionBeforeText(data, logicalRows)

	return DetectedTable{
		Data:      assigned,
		Box:       tableBox,
		TextCells: lineCells,
	}, true
}

func wideMultilineMinCols(options DetectionOptions) int {
	minCols := compactWordGridMinCols + 1
	if options.MinCols > minCols {
		return options.MinCols
	}
	return minCols
}

func isWideMultilineHeaderAnchor(row textline.ParagraphTextLine, options DetectionOptions) bool {
	if len(row.Cells) < wideMultilineMinCols(options) {
		return false
	}
	textCells := 0
	for _, cell := range row.Cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if isNumericTableCell(text) || !isSubstantialWideHeaderCell(cell) {
			return false
		}
		textCells++
	}
	return textCells >= wideMultilineMinCols(options)
}

func isSubstantialWideHeaderCell(cell page.TextCell) bool {
	text := strings.TrimSpace(cell.Text)
	if cell.Box.Width() < 12 {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func buildWideMultilineLogicalRows(rows []textline.ParagraphTextLine, colBoxes []geom.Box) ([]multilineLogicalRow, bool) {
	type rowParts struct {
		cells []page.TextCell
		parts [][]string
		seen  map[int]bool
	}

	parsed := make([]rowParts, 0, len(rows))
	for _, row := range rows {
		parts, seen, ok := captionBeforeRowParts(row, colBoxes)
		if !ok || len(seen) == 0 {
			return nil, false
		}
		parsed = append(parsed, rowParts{cells: row.Cells, parts: parts, seen: seen})
	}

	firstBody := -1
	for index := 1; index < len(parsed); index++ {
		if startsWideBodyRow(parsed[index].seen) {
			firstBody = index
			break
		}
	}
	if firstBody < 0 {
		return nil, false
	}

	columnCount := len(colBoxes)
	headerEnd := firstBody
	covered := make(map[int]bool, columnCount)
	for index := 0; index < firstBody; index++ {
		for column := range parsed[index].seen {
			covered[column] = true
		}
		if index > 0 && len(covered) == columnCount && isWidePreBodyRow(parsed[index].seen, columnCount) {
			headerEnd = index
			break
		}
	}
	if headerEnd <= 0 {
		return nil, false
	}

	logicalRows := make([]multilineLogicalRow, 0, len(rows))
	header := multilineLogicalRow{Parts: make([][]string, columnCount)}
	for index := 0; index < headerEnd; index++ {
		appendCaptionBeforeParts(&header, parsed[index].cells, parsed[index].parts)
	}
	logicalRows = append(logicalRows, header)

	if headerEnd < firstBody {
		blankBody := multilineLogicalRow{Parts: make([][]string, columnCount)}
		for index := headerEnd; index < firstBody; index++ {
			appendCaptionBeforeParts(&blankBody, parsed[index].cells, parsed[index].parts)
		}
		logicalRows = append(logicalRows, blankBody)
	}

	for index := firstBody; index < len(parsed); index++ {
		if startsWideBodyRow(parsed[index].seen) {
			logicalRows = append(logicalRows, multilineLogicalRow{
				Parts: make([][]string, columnCount),
			})
		} else if len(logicalRows) <= 1 {
			return nil, false
		}
		current := &logicalRows[len(logicalRows)-1]
		appendCaptionBeforeParts(current, parsed[index].cells, parsed[index].parts)
	}

	return logicalRows, true
}

func startsWideBodyRow(seen map[int]bool) bool {
	return seen[0]
}

func isWidePreBodyRow(seen map[int]bool, columnCount int) bool {
	if len(seen) != 1 || seen[0] || seen[columnCount-1] {
		return false
	}
	for column := range seen {
		return column > 0 && column < columnCount-1
	}
	return false
}

func validWideMultilineLogicalRows(rows []multilineLogicalRow, columnCount int, options DetectionOptions) bool {
	if len(rows) < options.MinRows || columnCount < wideMultilineMinCols(options) {
		return false
	}
	if countCaptionBeforeNonEmptyColumns(rows[0]) < wideMultilineMinCols(options) {
		return false
	}

	bodyRowsWithFirstColumn := 0
	nonEmptyBodyRows := 0
	for _, row := range rows[1:] {
		if len(row.Parts) != columnCount {
			return false
		}
		nonEmptyColumns := countCaptionBeforeNonEmptyColumns(row)
		if nonEmptyColumns == 0 {
			return false
		}
		nonEmptyBodyRows++
		if strings.TrimSpace(strings.Join(row.Parts[0], " ")) != "" {
			bodyRowsWithFirstColumn++
		}
	}
	return bodyRowsWithFirstColumn >= 2 && nonEmptyBodyRows >= compactWordGridMinRows
}

func assignCaptionBeforeText(data Data, rows []multilineLogicalRow) Data {
	for index := range data.Cells {
		cell := &data.Cells[index]
		if cell.StartRow < 0 || cell.StartRow >= len(rows) || cell.StartCol < 0 || cell.StartCol >= len(rows[cell.StartRow].Parts) {
			continue
		}
		cell.Text = strings.Join(rows[cell.StartRow].Parts[cell.StartCol], " ")
	}
	return data
}

func strongestAnchorRow(rows []textline.ParagraphTextLine, minAnchorCols int) (textline.ParagraphTextLine, bool) {
	var best textline.ParagraphTextLine
	found := false
	for _, row := range rows {
		if len(row.Cells) < minAnchorCols {
			continue
		}
		if !found || len(row.Cells) > len(best.Cells) {
			best = row
			found = true
		}
	}
	return best, found
}

func supportedAnchorRows(rows []textline.ParagraphTextLine, minAnchorCols int) int {
	count := 0
	for _, row := range rows {
		if len(row.Cells) >= minAnchorCols {
			count++
		}
	}
	return count
}

func anchoredColumnBoxes(anchorCells []page.TextCell, tableBox geom.Box) []geom.Box {
	cells := append([]page.TextCell(nil), anchorCells...)
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Box.L < cells[j].Box.L
	})

	cols := make([]geom.Box, 0, len(cells))
	for index, cell := range cells {
		left := tableBox.L
		if index > 0 {
			left = (cells[index-1].Box.R + cell.Box.L) / 2
		}
		right := tableBox.R
		if index+1 < len(cells) {
			right = (cell.Box.R + cells[index+1].Box.L) / 2
		}
		cols = append(cols, geom.Box{L: left, T: tableBox.T, R: right, B: tableBox.B, Origin: geom.TopLeft})
	}
	return cols
}

func groupTextRows(cells []page.TextCell, rowTolerance float64) []textline.ParagraphTextLine {
	ordered := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) != "" {
			ordered = append(ordered, cell)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		leftCenter := ordered[i].Box.CenterY()
		rightCenter := ordered[j].Box.CenterY()
		if leftCenter == rightCenter {
			return ordered[i].Box.L < ordered[j].Box.L
		}
		return leftCenter < rightCenter
	})

	rows := make([]textline.ParagraphTextLine, 0)
	for _, cell := range ordered {
		center := cell.Box.CenterY()
		if len(rows) == 0 || math.Abs(center-rows[len(rows)-1].Center) > rowTolerance {
			rows = append(rows, textline.ParagraphTextLine{Cells: []page.TextCell{cell}, Center: center})
			continue
		}

		row := &rows[len(rows)-1]
		row.Cells = append(row.Cells, cell)
		row.Center = averageRowCenter(row.Cells)
	}

	for index := range rows {
		sort.SliceStable(rows[index].Cells, func(i, j int) bool {
			return rows[index].Cells[i].Box.L < rows[index].Cells[j].Box.L
		})
	}
	return rows
}

func buildDetectedTable(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, bool) {
	logicalRows := mergeRowsNW(rows, options)
	rowsForGeometry := logicalTableRows(logicalRows)
	layoutRows := mergeTightRowFragments(rowsForGeometry, options)
	clusters := clusterColumns(layoutRows, options.ColumnTolerance)
	if len(clusters) < options.MinCols {
		return DetectedTable{}, false
	}
	trimCount := sparseLeadingTrimCount(layoutRows, clusters, options)
	if trimCount > 0 {
		logicalRows = logicalRows[trimCount:]
		rowsForGeometry = rowsForGeometry[trimCount:]
		layoutRows = layoutRows[trimCount:]
	}
	logicalRows = trimUnstableTrailingRows(logicalRows, options)
	rowsForGeometry = logicalTableRows(logicalRows)
	layoutRows = mergeTightRowFragments(rowsForGeometry, options)
	if len(rowsForGeometry) < options.MinRows {
		return DetectedTable{}, false
	}

	sourceCells := flattenLogicalTableRowSources(logicalRows)
	tableBox := enclosingTextCells(sourceCells)
	if !rowsHaveTableWhitespace(rowsForGeometry, tableBox, options.MaxRowFillRatio) {
		return DetectedTable{}, false
	}

	// Reconstruct the grid from the word-level source cells, keeping the
	// logical rows that mergeFirstColumnContinuationRows already merged
	// (passed as rowBoxes so the grid does not re-merge and over-collapse).
	// The densest layout row supplies the column anchor. The grid derives
	// columns from the anchor and assigns cells by area overlap, replacing
	// the centre-cluster colBoxes + rowsFitClusters + WithAssignedText chain.
	// rowsFitClusters is dropped as a gate: it over-segments multi-line
	// tables (centre clustering produces spurious columns from wrapped-cell
	// centres) and rejects valid tables like the welfare-interview grids;
	// the grid's anchor-derived columns + area-overlap fit is the validity
	// check instead.
	anchor := densestLayoutRow(layoutRows)
	gridRowBoxes := make([]geom.Box, len(rowsForGeometry))
	for i, row := range rowsForGeometry {
		gridRowBoxes[i] = enclosingTextCells(row.Cells)
	}
	grid, err := ReconstructGridWithRows(sourceCells, tableBox, gridRowBoxes, anchor)
	if err != nil || len(grid.RowBoxes) < options.MinRows || len(grid.ColBoxes) < options.MinCols {
		return DetectedTable{}, false
	}

	// Shape gate (replaces the centre-cluster rowsFitClusters that
	// over-segmented multi-line tables): require that the header row (row 0) populates >= MinCols
	// distinct columns and at least one data row populates >= 2. This rejects
	// label-value prose (1-2 columns per row) while accepting genuine
	// multi-column tables whose rows have a blank first column.
	if !gridHasColumnSupport(grid, options.MinCols) {
		return DetectedTable{}, false
	}

	detected, ok := buildTableFromGrid(grid, sourceCells)
	if !ok {
		return DetectedTable{}, false
	}
	return detected, true
}

func buildDetectedTablePrefix(rows []textline.ParagraphTextLine, options DetectionOptions) (DetectedTable, int, bool) {
	for end := len(rows); end >= options.MinRows; end-- {
		detected, ok := buildDetectedTable(rows[:end], options)
		if ok {
			return detected, end, true
		}
	}
	return DetectedTable{}, 0, false
}

func newLogicalTableRow(row textline.ParagraphTextLine) logicalTableRow {
	return logicalTableRow{
		Row:    row,
		Source: append([]page.TextCell(nil), row.Cells...),
	}
}

func mergeFirstColumnContinuationRows(rows []textline.ParagraphTextLine, options DetectionOptions) []logicalTableRow {
	merged := make([]logicalTableRow, 0, len(rows))
	for _, row := range rows {
		if len(merged) > 0 && isFirstColumnContinuationRow(row, merged[len(merged)-1].Row, options) {
			merged[len(merged)-1] = mergeLogicalTableRowContinuation(merged[len(merged)-1], row)
			continue
		}
		merged = append(merged, newLogicalTableRow(row))
	}
	return merged
}

func mergeLogicalTableRowContinuation(previous logicalTableRow, continuation textline.ParagraphTextLine) logicalTableRow {
	previous.Row = mergeFirstColumnContinuationIntoRow(previous.Row, continuation)
	previous.Source = append(previous.Source, continuation.Cells...)
	return previous
}

func mergeFirstColumnContinuationIntoRow(previous, continuation textline.ParagraphTextLine) textline.ParagraphTextLine {
	if len(previous.Cells) == 0 || len(continuation.Cells) != 1 {
		return previous
	}

	cells := append([]page.TextCell(nil), previous.Cells...)
	firstIndex := 0
	for index := range cells[1:] {
		candidate := index + 1
		if cells[candidate].Box.L < cells[firstIndex].Box.L {
			firstIndex = candidate
		}
	}

	next := continuation.Cells[0]
	cells[firstIndex].Text = strings.TrimSpace(cells[firstIndex].Text + " " + next.Text)
	cells[firstIndex].Box = geom.EnclosingBox(cells[firstIndex].Box, next.Box)
	cells[firstIndex].Index = min(cells[firstIndex].Index, next.Index)
	if next.FontSize > cells[firstIndex].FontSize {
		cells[firstIndex].FontSize = next.FontSize
	}

	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Index < cells[j].Index
		}
		return cells[i].Box.L < cells[j].Box.L
	})
	return textline.ParagraphTextLine{Cells: cells, Center: averageRowCenter(cells)}
}

func isFirstColumnContinuationRow(row, previous textline.ParagraphTextLine, options DetectionOptions) bool {
	if len(row.Cells) != 1 || len(previous.Cells) < options.MinCols || len(previous.Cells) < 2 {
		return false
	}
	if math.Abs(row.Center-previous.Center) > firstColumnContinuationGapLimit(previous) {
		return false
	}

	cells := append([]page.TextCell(nil), previous.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Index < cells[j].Index
		}
		return cells[i].Box.L < cells[j].Box.L
	})

	first := cells[0]
	second := cells[1]
	boundary := (first.Box.R + second.Box.L) * 0.5
	continuation := row.Cells[0]
	tolerance := firstColumnContinuationTolerance(options)
	if continuation.Box.R > boundary+tolerance {
		return false
	}
	if math.Abs(continuation.Box.L-first.Box.L) <= tolerance {
		return true
	}
	return horizontalOverlapRatio(continuation.Box, first.Box) >= 0.5
}

func firstColumnContinuationGapLimit(row textline.ParagraphTextLine) float64 {
	height := 0.0
	for _, cell := range row.Cells {
		height += cell.Box.Height()
	}
	if len(row.Cells) > 0 {
		height /= float64(len(row.Cells))
	}
	limit := height * 2
	if limit < 18 {
		return 18
	}
	return limit
}

func firstColumnContinuationTolerance(options DetectionOptions) float64 {
	tolerance := options.ColumnTolerance
	if tolerance < 8 {
		return 8
	}
	return tolerance
}

func horizontalOverlapRatio(left, right geom.Box) float64 {
	width := left.Width()
	if width <= 0 {
		return 0
	}
	overlap := math.Min(left.R, right.R) - math.Max(left.L, right.L)
	if overlap <= 0 {
		return 0
	}
	return overlap / width
}

func logicalTableRows(rows []logicalTableRow) []textline.ParagraphTextLine {
	out := make([]textline.ParagraphTextLine, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Row)
	}
	return out
}

func flattenLogicalTableRowSources(rows []logicalTableRow) []page.TextCell {
	cells := make([]page.TextCell, 0)
	for _, row := range rows {
		cells = append(cells, row.Source...)
	}
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Index < cells[j].Index
	})
	return cells
}

func mergeTightRowFragments(rows []textline.ParagraphTextLine, options DetectionOptions) []textline.ParagraphTextLine {
	merged := make([]textline.ParagraphTextLine, 0, len(rows))
	for _, row := range rows {
		merged = append(merged, mergeTightRowFragment(row, tightRowFragmentGapLimit(options)))
	}
	return merged
}

func mergeTightRowFragment(row textline.ParagraphTextLine, gapLimit float64) textline.ParagraphTextLine {
	if len(row.Cells) < 2 {
		return row
	}
	cells := append([]page.TextCell(nil), row.Cells...)
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Box.L == cells[j].Box.L {
			return cells[i].Index < cells[j].Index
		}
		return cells[i].Box.L < cells[j].Box.L
	})

	merged := make([]page.TextCell, 0, len(cells))
	current := cells[0]
	for _, next := range cells[1:] {
		if next.Box.L-current.Box.R <= gapLimit {
			current.Text = strings.TrimSpace(current.Text + " " + next.Text)
			current.Box = geom.EnclosingBox(current.Box, next.Box)
			current.Index = min(current.Index, next.Index)
			if next.FontSize > current.FontSize {
				current.FontSize = next.FontSize
			}
			continue
		}
		merged = append(merged, current)
		current = next
	}
	merged = append(merged, current)
	return textline.ParagraphTextLine{Cells: merged, Center: averageRowCenter(merged)}
}

func tightRowFragmentGapLimit(options DetectionOptions) float64 {
	limit := options.ColumnTolerance * 0.5
	if limit < 4 {
		return 4
	}
	return limit
}

func sparseLeadingTrimCount(rows []textline.ParagraphTextLine, clusters []columnCluster, options DetectionOptions) int {
	trimmed := 0
	for len(rows) > options.MinRows {
		tableBox := enclosingTextCells(flattenRows(rows))
		if !isSparseLeadingGridRow(rows[0], rows[1:], clusters, tableBox, options) {
			break
		}
		rows = rows[1:]
		clusters = clusterColumns(rows, options.ColumnTolerance)
		if len(clusters) < options.MinCols {
			break
		}
		trimmed++
	}
	return trimmed
}

func trimUnstableTrailingRows(rows []logicalTableRow, options DetectionOptions) []logicalTableRow {
	trimmed := append([]logicalTableRow(nil), rows...)
	for len(trimmed) > options.MinRows {
		geometryRows := logicalTableRows(trimmed)
		prefixRows := geometryRows[:len(geometryRows)-1]
		prefixLayoutRows := mergeTightRowFragments(prefixRows, options)
		prefixClusters := clusterColumns(prefixLayoutRows, options.ColumnTolerance)
		if len(prefixClusters) < options.MinCols || !rowsFitClusters(prefixLayoutRows, prefixClusters, options) {
			break
		}

		lastLayoutRow := mergeTightRowFragment(geometryRows[len(geometryRows)-1], tightRowFragmentGapLimit(options))
		if rowFitsClusters(lastLayoutRow, prefixClusters, options) {
			break
		}
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}

func isSparseLeadingGridRow(row textline.ParagraphTextLine, following []textline.ParagraphTextLine, clusters []columnCluster, tableBox geom.Box, options DetectionOptions) bool {
	if len(clusters) < 4 || tableBox.Width() <= 0 {
		return false
	}

	firstSpan, firstOK := rowClusterSpan(row, clusters, options.ColumnTolerance)
	if !firstOK {
		return false
	}
	maxFollowingSpan := 0
	for _, next := range following {
		span, ok := rowClusterSpan(next, clusters, options.ColumnTolerance)
		if ok && span > maxFollowingSpan {
			maxFollowingSpan = span
		}
	}
	if maxFollowingSpan < int(math.Ceil(float64(len(clusters))*0.75)) {
		return false
	}
	if firstSpan*2 >= maxFollowingSpan {
		return false
	}
	return rowTextWidth(row)/tableBox.Width() <= 0.35
}

func rowClusterSpan(row textline.ParagraphTextLine, clusters []columnCluster, tolerance float64) (int, bool) {
	minColumn, maxColumn := len(clusters), -1
	for _, cell := range row.Cells {
		clusterIndex := nearestClusterIndex(cell, clusters)
		if clusterIndex < 0 || math.Abs(columnAnchor(cell)-clusters[clusterIndex].Center) > tolerance {
			continue
		}
		if clusterIndex < minColumn {
			minColumn = clusterIndex
		}
		if clusterIndex > maxColumn {
			maxColumn = clusterIndex
		}
	}
	if maxColumn < minColumn {
		return 0, false
	}
	return maxColumn - minColumn + 1, true
}

func rowTextWidth(row textline.ParagraphTextLine) float64 {
	width := 0.0
	for _, cell := range row.Cells {
		width += cell.Box.Width()
	}
	return width
}

func clusterColumns(rows []textline.ParagraphTextLine, columnTolerance float64) []columnCluster {
	cells := flattenRows(rows)
	sort.SliceStable(cells, func(i, j int) bool {
		return columnAnchor(cells[i]) < columnAnchor(cells[j])
	})

	clusters := make([]columnCluster, 0)
	for _, cell := range cells {
		center := columnAnchor(cell)
		if len(clusters) == 0 || math.Abs(center-clusters[len(clusters)-1].Center) > columnTolerance {
			clusters = append(clusters, columnCluster{Cells: []page.TextCell{cell}, Center: center})
			continue
		}

		cluster := &clusters[len(clusters)-1]
		cluster.Cells = append(cluster.Cells, cell)
		cluster.Center = averageColumnCenter(cluster.Cells)
	}
	return clusters
}

func rowsFitClusters(rows []textline.ParagraphTextLine, clusters []columnCluster, options DetectionOptions) bool {
	for _, row := range rows {
		if !rowFitsClusters(row, clusters, options) {
			return false
		}
	}
	return true
}

func rowFitsClusters(row textline.ParagraphTextLine, clusters []columnCluster, options DetectionOptions) bool {
	seen := make(map[int]bool)
	for _, cell := range row.Cells {
		clusterIndex := nearestClusterIndex(cell, clusters)
		if clusterIndex >= 0 && math.Abs(columnAnchor(cell)-clusters[clusterIndex].Center) <= options.ColumnTolerance {
			seen[clusterIndex] = true
		}
	}
	return len(seen) >= options.MinCols
}

func rowsHaveTableWhitespace(rows []textline.ParagraphTextLine, tableBox geom.Box, maxRowFillRatio float64) bool {
	if tableBox.Width() <= 0 {
		return false
	}
	for _, row := range rows {
		rowWidth := 0.0
		for _, cell := range row.Cells {
			rowWidth += cell.Box.Width()
		}
		if rowWidth/tableBox.Width() > maxRowFillRatio {
			return false
		}
	}
	return true
}

func nearestClusterIndex(cell page.TextCell, clusters []columnCluster) int {
	if len(clusters) == 0 {
		return -1
	}

	center := columnAnchor(cell)
	bestIndex := 0
	bestDistance := math.Abs(center - clusters[0].Center)
	for index := 1; index < len(clusters); index++ {
		distance := math.Abs(center - clusters[index].Center)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func flattenRows(rows []textline.ParagraphTextLine) []page.TextCell {
	cells := make([]page.TextCell, 0)
	for _, row := range rows {
		cells = append(cells, row.Cells...)
	}
	sort.SliceStable(cells, func(i, j int) bool {
		return cells[i].Index < cells[j].Index
	})
	return cells
}

func enclosingTextCells(cells []page.TextCell) geom.Box {
	boxes := make([]geom.Box, 0, len(cells))
	for _, cell := range cells {
		boxes = append(boxes, cell.Box)
	}
	return geom.EnclosingBox(boxes...)
}

func averageRowCenter(cells []page.TextCell) float64 {
	total := 0.0
	for _, cell := range cells {
		total += cell.Box.CenterY()
	}
	return total / float64(len(cells))
}

func averageColumnCenter(cells []page.TextCell) float64 {
	total := 0.0
	for _, cell := range cells {
		total += columnAnchor(cell)
	}
	return total / float64(len(cells))
}

func columnAnchor(cell page.TextCell) float64 {
	return cell.Box.L
}
