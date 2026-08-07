package pdf

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/render"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

// The classify-then-route pipeline (Task 5 of the learned layout classifier
// plan). It is an ALTERNATE path, selected by ExtractionOptions.ClassifyThenRoute;
// the default path in backend.go is untouched.
//
// Why the pipeline has to be restructured at all: today's order decides tables
// BEFORE lines exist. DetectTables consumes cells directly and produces table
// blocks whose cells never reach the line assembler, and the heading detector
// splits its cells out earlier still. A line classifier bolted onto today's
// assembly therefore cannot take over those decisions — they are already baked
// into what got assembled. So the order becomes:
//
//	1. assemble ALL cells into lines, class-agnostically
//	2. route each line to a destination
//	3. hand each destination's cells to the existing builder
//
// In THIS task every routing decision is still made by the existing heuristics.
// Nothing learned is in the loop yet. That is deliberate: it isolates REFACTOR
// risk from MODEL risk, so the reroute can be proven neutral on DPBench before
// any model decision goes live.

// lineRoute is where a class-agnostically assembled line is sent.
type lineRoute int

const (
	routeParagraph lineRoute = iota
	routeHeading
	routeTable
)

// pageMarkdownBlocksRouted is the classify-then-route equivalent of
// pageMarkdownBlocks.
func pageMarkdownBlocksRouted(ctx context.Context, cells []page.TextCell, wordCells []page.TextCell, rulings []page.RulingSegment, formFields []page.FormField, size geom.Size, options ExtractionOptions) ([]markdownBlock, error) {
	if options.ReadingOrder {
		start := time.Now()
		_, span := tracer.Start(ctx, "pipeline.reading_order")
		cells = orderCells(cells, size)
		if len(wordCells) > 0 {
			wordCells = orderCells(wordCells, size)
		}
		span.End()
		recordStage(ctx, "reading_order", start)
	}

	var formBlocks []markdownBlock
	if len(formFields) > 0 {
		cells = reserveCellIndexGaps(cells)
		if len(wordCells) > 0 {
			wordCells = reserveCellIndexGaps(wordCells)
		}
		formBlocks = formFieldMarkdownBlocks(formFields, cells, size)
	}

	// STEP 1 — class-agnostic assembly. Every cell goes in: no heading split,
	// no table carve-out. This line set is what the classifier will see at
	// inference, and it is the set the Task 2 emitter dumps for training, which
	// is what keeps the two free of skew.
	allLines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)

	// The model runs once per page over the class-agnostic lines; every routing
	// decision below consults the same labels.
	var labeller *lineLabeller
	if options.LearnedRouting {
		_, finishStage := startStage(ctx, "layout_classify")
		labeller = newLineLabeller(allLines, cells, size, rulings)
		if options.LearnedRegions && labeller.ok {
			gapCells := wordCells
			if len(gapCells) == 0 {
				gapCells = cells
			}
			labeller.gateRegions(gapCells, rulings, size, layoutClassPicture)
		}
		finishStage(nil)
	}

	// STEP 2 — routing. Headings claim first, then tables, and whatever is
	// unclaimed flows as prose. With LearnedRouting the ORDER is unchanged and
	// only the DECIDER moves.
	var headingBlocks []markdownBlock
	remaining := cells
	if options.DetectHeadings {
		_, finishStage := startStage(ctx, "heading_detect")
		protected := protectedHeadingCellIndexes(cells, rulings, options)
		if options.DetectStructure {
			protected = mergeProtectedCellIndexes(protected, protectedListLineCellIndexes(cells))
		}
		protected = mergeProtectedCellIndexes(protected, denseIndexLineCellIndexes(cells, size))
		var decide headingDecider
		if labeller != nil && labeller.ok {
			decide = labeller.isHeading
		}
		headingBlocks, remaining = splitHeadingCellsWith(cells, size, protected, decide)
		finishStage(nil)
	}

	var detected doctable.DetectionResult
	if options.DetectTables {
		detectStart := time.Now()
		_, detectSpan := tracer.Start(ctx, "pipeline.table_detect")
		detected, remaining = detectPageTables(remaining, wordCells, rulings, size, options)

		// Task 6, first migrated class: Formula. The label-set mapping is
		// "display-equation handling; never table cells", and the measured
		// defect is that 31% of DocLayNet's Formula lines are currently emitted
		// as table cells. This is the fix.
		if options.LearnedFormulaRouting {
			detected, remaining = rejectFormulaTables(allLines, cells, size, rulings, detected, remaining)
		}

		detectSpan.SetAttributes(attribute.Int("tables", len(detected.Tables)))
		detectSpan.End()
		recordStage(ctx, "table_detect", detectStart)
		tablesDetected.Add(ctx, int64(len(detected.Tables)))
	}

	// STEP 3 — assemble the prose route. The cells that survived routing are
	// re-clustered exactly as the default path clusters them.
	//
	// The class-agnostic line set above is NOT reused to build these blocks, and
	// that is the neutrality decision this task turns on. A line assembled from
	// every cell on the page can straddle a table gutter or merge a heading with
	// the body text beside it; rebuilding from the routed cells reproduces the
	// default path's grouping exactly. Routing lines directly is the next step,
	// and it must not be taken until the shadow-mode confusion matrix says what
	// it would change.
	blocks := append([]markdownBlock{}, headingBlocks...)
	blocks = append(blocks, assembleRoutedProse(ctx, remaining, size, options)...)

	if options.DetectStructure {
		_, finishStage := startStage(ctx, "structure")
		if labeller != nil && labeller.ok {
			blocks = applyLearnedListItems(blocks, labeller)
		} else {
			blocks = detectStructure(blocks)
		}
		finishStage(nil)
	}

	_, finishStage := startStage(ctx, "postprocess")
	if labeller != nil && labeller.ok {
		blocks = dropPictureBlocks(blocks, labeller)
	} else {
		blocks = filterFigureInternalLabelBlocks(blocks, size)
	}
	blocks = splitMarginalPageNumberBlocks(blocks, size)
	blocks = append(blocks, formBlocks...)

	for _, detectedTable := range detected.Tables {
		renderStart := time.Now()
		_, renderSpan := tracer.Start(ctx, "render.table")
		tableMarkdown, err := render.Table(detectedTable.Data)
		renderSpan.End()
		recordStage(ctx, "render", renderStart)
		if err != nil {
			return nil, err
		}
		tableData := detectedTable.Data
		blocks = append(blocks, markdownBlock{
			Index:     minTextCellIndex(detectedTable.TextCells),
			Text:      strings.TrimSpace(tableMarkdown),
			tableData: &tableData,
			tableBox:  detectedTable.Box,
		})
	}

	sortMarkdownBlocks(blocks, size, !options.ReadingOrder)
	finishStage(nil)

	// STEP 4 — shadow mode. Route every class-agnostic line and record what the
	// heuristics decided, changing nothing. Once the model is wired in, the same
	// hook records what it WOULD have decided, which is the live confusion
	// matrix that cross-checks the offline one.
	recordShadowRoutes(ctx, allLines, headingBlocks, detected.Tables)

	return blocks, nil
}

// detectPageTables is the default path's table-detection block, lifted verbatim
// so both paths share one definition. Extracting it is what makes "the reroute
// changes the ORDER, not the decisions" checkable rather than merely claimed.
func detectPageTables(cells []page.TextCell, wordCells []page.TextCell, rulings []page.RulingSegment, size geom.Size, options ExtractionOptions) (doctable.DetectionResult, []page.TextCell) {
	if options.LearnedColumns {
		defer doctable.SetColumnDerivation(true, rulings)()
	}
	tableProtected := denseIndexLineCellIndexes(cells, size)
	tableCells, protectedTableCells := splitCellsByIndexSet(cells, tableProtected)
	detected := doctable.DetectTables(tableCells, rulings, options.TableDetection)
	remaining := detected.TextCells
	threshold := normalisedTableOverlapThreshold(options.TableDetection)

	if len(wordCells) > 0 {
		anchored := doctable.DetectAnchoredTextTables(tableCells, wordCells, options.TableDetection)
		if len(anchored.Tables) > 0 {
			detected.Tables = mergePreferredTables(detected.Tables, anchored.Tables, tableCells)
			remaining = textCellsOutsideTables(tableCells, detected.Tables, threshold)
		} else if len(detected.Tables) == 0 {
			remaining = tableCells
		}
		detected.Tables = reassignDetectedTableTextFromWords(detected.Tables, wordCells, threshold)
	}

	// Drop zero-validity false positives: tables whose every content column is
	// prose. Their cells return to the paragraph path.
	kept := detected.Tables[:0]
	for _, detectedTable := range detected.Tables {
		if doctable.ValidityScore(detectedTable.Data) <= 0 {
			remaining = append(remaining, detectedTable.TextCells...)
			continue
		}
		kept = append(kept, detectedTable)
	}
	detected.Tables = kept
	remaining = append(remaining, protectedTableCells...)
	return detected, remaining
}

// assembleRoutedProse clusters the prose-routed cells into blocks, per column.
func assembleRoutedProse(ctx context.Context, cells []page.TextCell, size geom.Size, options ExtractionOptions) []markdownBlock {
	start := time.Now()
	_, span := tracer.Start(ctx, "pipeline.assemble")
	defer func() {
		span.End()
		recordStage(ctx, "assemble", start)
	}()

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

// ShadowRoute is one class-agnostic line together with the destination the
// current heuristics sent it to. It is the observability surface for Task 5
// step 4 and the input to the live confusion matrix.
type ShadowRoute struct {
	Box   geom.Box
	Text  string
	Route lineRoute
}

// shadowSink receives shadow routes when a caller installs one. It is nil in
// production, so shadow mode costs a routing pass and nothing else.
var shadowSink func([]ShadowRoute)

// SetShadowRouteSink installs a collector for shadow-mode routing decisions and
// returns a function restoring the previous one. Intended for the benchmark
// harness and tests, not for production callers.
func SetShadowRouteSink(sink func([]ShadowRoute)) func() {
	previous := shadowSink
	shadowSink = sink
	return func() { shadowSink = previous }
}

// StraddleReport describes a class-agnostic line's relationship to the routing
// region it lands in. Straddles is true when the line is only PARTLY inside —
// neither cleanly in nor cleanly out.
//
// These are the lines whose output changes the moment Task 6 routes lines
// rather than cells, so counting them converts the plan's stated hazard
// ("class-agnostic assembly creates lines that never existed before") from an
// unknown into a measurement.
type StraddleReport struct {
	Text        string
	Destination string
	Containment float64
	Straddles   bool
}

var straddleSink func([]StraddleReport)

// SetStraddleSink installs a collector for straddle reports. For the benchmark
// harness and tests; nil in production.
func SetStraddleSink(sink func([]StraddleReport)) { straddleSink = sink }

// straddleBand is the range in which a line is neither cleanly inside a region
// nor cleanly outside it. Outside this band the line's destination is
// unambiguous and routing it directly cannot change the output.
const straddleLow, straddleHigh = 0.05, 0.95

func recordStraddles(lines []ParagraphTextLine, headings []markdownBlock, tables []doctable.DetectedTable) {
	if straddleSink == nil {
		return
	}
	reports := make([]StraddleReport, 0, len(lines))
	for _, line := range lines {
		best, destination := 0.0, "prose"
		for _, table := range tables {
			if c := lineContainment(line.BBox, table.Box); c > best {
				best, destination = c, "table"
			}
		}
		for _, heading := range headings {
			if c := lineContainment(line.BBox, heading.Box); c > best {
				best, destination = c, "heading"
			}
		}
		reports = append(reports, StraddleReport{
			Text:        line.Text,
			Destination: destination,
			Containment: best,
			Straddles:   best > straddleLow && best < straddleHigh,
		})
	}
	straddleSink(reports)
}

func recordShadowRoutes(ctx context.Context, lines []ParagraphTextLine, headings []markdownBlock, tables []doctable.DetectedTable) {
	recordStraddles(lines, headings, tables)
	if shadowSink == nil {
		return
	}
	_, finishStage := startStage(ctx, "shadow_route")
	defer finishStage(nil)

	routes := make([]ShadowRoute, 0, len(lines))
	for _, line := range lines {
		route := routeParagraph
		for _, table := range tables {
			if lineContainment(line.BBox, table.Box) >= 0.5 {
				route = routeTable
				break
			}
		}
		if route == routeParagraph {
			for _, heading := range headings {
				if lineContainment(line.BBox, heading.Box) >= 0.5 {
					route = routeHeading
					break
				}
			}
		}
		routes = append(routes, ShadowRoute{Box: line.BBox, Text: line.Text, Route: route})
	}
	shadowSink(routes)
}

// rejectFormulaTables is the Formula class's migration from heuristic to model.
//
// The rule is a plurality vote, not a tuned threshold: a candidate table is
// rejected when the most common label among the lines inside it is Formula.
// That is argmax over the region's line-label distribution — precisely the
// region feature the plan describes ("80% of these lines scored Formula is a
// region feature") — so no hand-picked constant enters, which is the property
// the whole project exists to establish.
//
// Rejected candidates return their cells to the prose path, exactly as the
// existing zero-validity drop does. Nothing is deleted here: the heuristic
// table detector still proposes every candidate, and the model only vetoes.
// Deleting the superseded guards is a later step, gated on this measuring well.
func rejectFormulaTables(lines []ParagraphTextLine, cells []page.TextCell, size geom.Size, rulings []page.RulingSegment, detected doctable.DetectionResult, remaining []page.TextCell) (doctable.DetectionResult, []page.TextCell) {
	model, err := layoutModel()
	if err != nil || model == nil || len(detected.Tables) == 0 || len(lines) == 0 {
		return detected, remaining
	}

	// Features are computed with a nil document context, so repeat_frac is 0 —
	// which is exactly the value the model was trained on, because DocLayNet is
	// 81k single-page PDFs. Consistency here is not an accident; it is why
	// DocumentLayoutContext.fraction reports 0 below two pages.
	pageCtx := NewPageLayoutContext(size, cells, lines, rulings, 0, 1)

	labels := make([]string, len(lines))
	for i := range lines {
		var prev, next *ParagraphTextLine
		if i > 0 {
			prev = &lines[i-1]
		}
		if i+1 < len(lines) {
			next = &lines[i+1]
		}
		labels[i], _ = model.PredictLineClass(LineLayoutFeatures(lines[i], prev, next, pageCtx))
	}

	kept := detected.Tables[:0]
	for _, candidate := range detected.Tables {
		votes := map[string]int{}
		_ = votes
		for i, line := range lines {
			if lineContainment(line.BBox, candidate.Box) >= 0.5 {
				votes[labels[i]]++
			}
		}
		winner, best := "", 0
		for label, count := range votes {
			if count > best || (count == best && label < winner) {
				winner, best = label, count
			}
		}
		if formulaVetoSink != nil {
			formulaVetoSink(winner, best, votes)
		}
		if best > 0 && winner == layoutClassFormula {
			remaining = append(remaining, candidate.TextCells...)
			continue
		}
		kept = append(kept, candidate)
	}
	detected.Tables = kept
	return detected, remaining
}

// layoutClassFormula is the model's label for a display equation. It matches
// DocLayNet's class name, which is the teacher's vocabulary rather than
// docmill's; the mapping onto docmill routing lives in the plan's label table.
const layoutClassFormula = "Formula"

// formulaVetoSink observes every candidate table the Formula veto considers:
// the plurality label among its lines, how many lines voted for it, and the
// full tally. Installed by the benchmark harness to answer "did the rule run
// and find nothing, or did it never run?" — a distinction a byte-identical
// result cannot make on its own. nil in production.
var formulaVetoSink func(winner string, votes int, tally map[string]int)

// SetFormulaVetoSink installs the observer above and returns a restore func.
func SetFormulaVetoSink(sink func(string, int, map[string]int)) func() {
	previous := formulaVetoSink
	formulaVetoSink = sink
	return func() { formulaVetoSink = previous }
}
