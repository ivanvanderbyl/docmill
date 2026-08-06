package pdf

import (
	"context"
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

// The debug emitter behind Task 1 and Task 2 of the learned layout classifier
// plan. It answers two questions for one corpus in one pass:
//
//   - Task 2: what does the feature vector look like for every assembled line?
//   - Task 1: what does the CURRENT heuristic pipeline call that same line?
//
// Both must be measured on the SAME lines or the comparison is meaningless, so
// the unit is the class-agnostically assembled line: every cell goes into the
// assembler with no table carve-outs and no figure drops. That is also the
// assembly the classifier will see at inference (Task 5), which is what keeps
// this free of training/serving skew.
//
// Nothing here changes extraction. It is a read-only view of the pipeline.

// Current-class labels reported by LayoutDebugRows. They are docmill's classes,
// not the teacher's — the whole point of Task 1 is to score the heuristics on
// their own terms and then map onto the label set for comparison.
const (
	CurrentClassParagraph   = "paragraph"
	CurrentClassHeading     = "heading"
	CurrentClassTable       = "table"
	CurrentClassListItem    = "list-item"
	CurrentClassFigureLabel = "figure-label"
)

// LayoutDebugRow is one assembled line: its identity, its box in top-left page
// points, its feature vector, and the class today's heuristics give it.
type LayoutDebugRow struct {
	Doc          string    `json:"doc"`
	Page         int       `json:"page"` // 1-based
	Line         int       `json:"line"`
	PageW        float64   `json:"page_w"`
	PageH        float64   `json:"page_h"`
	L            float64   `json:"l"`
	T            float64   `json:"t"`
	R            float64   `json:"r"`
	B            float64   `json:"b"`
	Text         string    `json:"text"`
	Features     []float64 `json:"f"`
	CurrentClass string    `json:"current"`
}

// pageDebugState is the first-pass capture for one page. Two passes are needed
// because repeat_frac is a document-scoped feature: whether a box recurs in the
// same slot on other pages cannot be known until every page has been assembled.
type pageDebugState struct {
	size    geom.Size
	cells   []page.TextCell
	words   []page.TextCell
	rulings []page.RulingSegment
	lines   []ParagraphTextLine
}

// LayoutDebugRows extracts feature rows plus current-heuristic classes for a
// whole document.
func LayoutDebugRows(ctx context.Context, doc Document, name string, options ExtractionOptions) ([]LayoutDebugRow, error) {
	pageCount, err := doc.PageCount(ctx)
	if err != nil {
		return nil, err
	}

	states := make([]pageDebugState, 0, pageCount)
	for index := 0; index < pageCount; index++ {
		state, err := capturePageDebugState(ctx, doc, index, options)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}

	pageLines := make([][]ParagraphTextLine, len(states))
	sizes := make([]geom.Size, len(states))
	for i, state := range states {
		pageLines[i] = state.lines
		sizes[i] = state.size
	}
	document := NewDocumentLayoutContext(pageLines, sizes)

	var rows []LayoutDebugRow
	for index, state := range states {
		pageCtx := NewPageLayoutContext(state.size, state.cells, state.lines, state.rulings, index, pageCount)
		pageCtx.Repeat = document
		classes := currentLineClasses(state, options)

		for i := range state.lines {
			var prev, next *ParagraphTextLine
			if i > 0 {
				prev = &state.lines[i-1]
			}
			if i+1 < len(state.lines) {
				next = &state.lines[i+1]
			}
			box := state.lines[i].BBox
			rows = append(rows, LayoutDebugRow{
				Doc:          name,
				Page:         index + 1,
				Line:         i,
				PageW:        state.size.Width,
				PageH:        state.size.Height,
				L:            box.L,
				T:            topEdgeOf(box),
				R:            box.R,
				B:            bottomEdgeOf(box),
				Text:         state.lines[i].Text,
				Features:     LineLayoutFeatures(state.lines[i], prev, next, pageCtx),
				CurrentClass: classes[i],
			})
		}
	}
	return rows, nil
}

func capturePageDebugState(ctx context.Context, doc Document, index int, options ExtractionOptions) (pageDebugState, error) {
	pdfPage, err := doc.Page(ctx, index)
	if err != nil {
		return pageDebugState{}, err
	}
	size, err := pdfPage.Size(ctx)
	if err != nil {
		return pageDebugState{}, err
	}
	cells, err := pdfPage.TextCells(ctx)
	if err != nil {
		return pageDebugState{}, err
	}

	state := pageDebugState{size: size, cells: cells}
	if provider, ok := pdfPage.(rulingSegmentProvider); ok {
		if state.rulings, err = provider.RulingSegments(ctx); err != nil {
			return pageDebugState{}, err
		}
	}
	if provider, ok := pdfPage.(wordTextCellProvider); ok {
		if state.words, err = provider.WordTextCells(ctx); err != nil {
			return pageDebugState{}, err
		}
	}

	// The class-agnostic assembly: every cell, no exclusions, ordered
	// top-to-bottom so the gap features see true vertical neighbours.
	lines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].BBox.CenterY() < lines[j].BBox.CenterY() })
	state.lines = lines
	return state, nil
}

// currentLineClasses replays today's decisions over the page and reports what
// each class-agnostic line would have been called.
//
// The replay is by BOX, not by cell index, deliberately: orderCells reassigns
// every cell's Index, so an index captured before reading order no longer
// identifies the same cell afterwards. Geometry survives that renumbering.
//
// Precedence follows the pipeline's existing structural order, which is also
// the transitional rule the plan fixes for migration: tables claim their region
// first, then headings, then figure-internal labels, then list items, and
// anything unclaimed is paragraph text.
func currentLineClasses(state pageDebugState, options ExtractionOptions) []string {
	classes := make([]string, len(state.lines))
	for i := range classes {
		classes[i] = CurrentClassParagraph
	}

	cells := state.cells
	words := state.words
	if options.ReadingOrder {
		cells = orderCells(cells, state.size)
		if len(words) > 0 {
			words = orderCells(words, state.size)
		}
	}

	var headingBoxes, tableBoxes, listBoxes, figureBoxes []geom.Box

	if options.DetectHeadings {
		protected := protectedHeadingCellIndexes(cells, state.rulings, options)
		if options.DetectStructure {
			protected = mergeProtectedCellIndexes(protected, protectedListLineCellIndexes(cells))
		}
		protected = mergeProtectedCellIndexes(protected, denseIndexLineCellIndexes(cells, state.size))
		var headingBlocks []markdownBlock
		headingBlocks, cells = splitHeadingCellsProtecting(cells, state.size, protected)
		for _, block := range headingBlocks {
			headingBoxes = append(headingBoxes, block.Box)
		}
	}

	remaining := cells
	if options.DetectTables {
		tableProtected := denseIndexLineCellIndexes(cells, state.size)
		tableCells, protectedTableCells := splitCellsByIndexSet(cells, tableProtected)
		detected := doctable.DetectTables(tableCells, state.rulings, options.TableDetection)
		remaining = detected.TextCells
		threshold := normalisedTableOverlapThreshold(options.TableDetection)
		if len(words) > 0 {
			anchored := doctable.DetectAnchoredTextTables(tableCells, words, options.TableDetection)
			if len(anchored.Tables) > 0 {
				detected.Tables = mergePreferredTables(detected.Tables, anchored.Tables, tableCells)
				remaining = textCellsOutsideTables(tableCells, detected.Tables, threshold)
			} else if len(detected.Tables) == 0 {
				remaining = tableCells
			}
		}
		// Mirror the zero-validity drop: those cells return to the paragraph
		// path in production, so they must not count as tables here either.
		kept := detected.Tables[:0]
		for _, detectedTable := range detected.Tables {
			if doctable.ValidityScore(detectedTable.Data) <= 0 {
				remaining = append(remaining, detectedTable.TextCells...)
				continue
			}
			kept = append(kept, detectedTable)
		}
		detected.Tables = kept
		// With the Formula class migrated, the replay must apply the same veto,
		// so `current` describes the pipeline being measured rather than the
		// one it replaced.
		if options.LearnedFormulaRouting {
			detected, remaining = rejectFormulaTables(state.lines, state.cells, state.size, state.rulings, detected, remaining)
		}
		for _, detectedTable := range detected.Tables {
			tableBoxes = append(tableBoxes, detectedTable.Box)
		}
		remaining = append(remaining, protectedTableCells...)
	}

	// Assemble what survived, exactly as production does, then observe which
	// blocks the list detector rewrote and which the figure filter discarded.
	blocks := assembleDebugBlocks(remaining, state.size, options)
	if options.DetectStructure {
		structured := detectStructure(blocks)
		for i := range structured {
			if i < len(blocks) && structured[i].Text != blocks[i].Text {
				listBoxes = append(listBoxes, structured[i].Box)
			}
		}
		blocks = structured
	}
	kept := filterFigureInternalLabelBlocks(blocks, state.size)
	keptBoxes := make(map[geom.Box]bool, len(kept))
	for _, block := range kept {
		keptBoxes[block.Box] = true
	}
	for _, block := range blocks {
		if !keptBoxes[block.Box] {
			figureBoxes = append(figureBoxes, block.Box)
		}
	}

	// Assign in reverse precedence so the strongest claim wins the final write.
	for _, group := range []struct {
		boxes []geom.Box
		class string
	}{
		{listBoxes, CurrentClassListItem},
		{figureBoxes, CurrentClassFigureLabel},
		{headingBoxes, CurrentClassHeading},
		{tableBoxes, CurrentClassTable},
	} {
		for i, line := range state.lines {
			for _, box := range group.boxes {
				if lineContainment(line.BBox, box) >= 0.5 {
					classes[i] = group.class
					break
				}
			}
		}
	}
	return classes
}

// assembleDebugBlocks mirrors the production assembleByColumn closure.
func assembleDebugBlocks(cells []page.TextCell, size geom.Size, options ExtractionOptions) []markdownBlock {
	paragraphOptions := ParagraphOptions{EnableInlineFormatting: options.EnableInlineFormatting}
	tolerance := ParagraphOptions{}.withDefaults().LineTolerance
	if !options.ReadingOrder {
		return assembleWithToc(AssembleLineElements(cells, tolerance), paragraphOptions)
	}
	var blocks []markdownBlock
	for _, group := range partitionColumns(cells, size) {
		blocks = append(blocks, assembleWithToc(AssembleLineElements(group, tolerance), paragraphOptions)...)
	}
	return blocks
}

// lineContainment is the fraction of the line's own area inside box — the same
// measure the DocLayNet join uses, so the heuristic baseline and the model are
// scored by identical rules.
func lineContainment(line, box geom.Box) float64 {
	lineTop, lineBottom := topEdgeOf(line), bottomEdgeOf(line)
	boxTop, boxBottom := topEdgeOf(box), bottomEdgeOf(box)
	width := math.Min(line.R, box.R) - math.Max(line.L, box.L)
	height := math.Min(lineBottom, boxBottom) - math.Max(lineTop, boxTop)
	if width <= 0 || height <= 0 {
		return 0
	}
	area := line.Width() * math.Abs(lineBottom-lineTop)
	if area <= 0 {
		return 0
	}
	return (width * height) / area
}

// LayoutFeatureContract returns the feature names, for the trainer to assert
// against. Exposed so the Python side reads the contract out of the binary
// rather than restating it.
func LayoutFeatureContract() []string {
	return append([]string(nil), LayoutFeatureNames...)
}
