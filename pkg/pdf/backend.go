// Package pdf turns a parsed PDF into Markdown. It defines the Backend/Document/
// Page interfaces that a parsing engine implements, then runs the extraction
// pipeline over the cells those return: reading order, paragraph/heading/list
// assembly, table detection, and Markdown serialisation.
package pdf

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/render"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

type Backend interface {
	OpenBytes(ctx context.Context, data []byte) (Document, error)
	Close() error
}

type Document interface {
	PageCount(ctx context.Context) (int, error)
	Page(ctx context.Context, index int) (Page, error)
	Close() error
}

type Page interface {
	Size(ctx context.Context) (geom.Size, error)
	TextCells(ctx context.Context) ([]page.TextCell, error)
	TextInRect(ctx context.Context, box geom.Box) (string, error)
}

type rulingSegmentProvider interface {
	RulingSegments(ctx context.Context) ([]page.RulingSegment, error)
}

type wordTextCellProvider interface {
	WordTextCells(ctx context.Context) ([]page.TextCell, error)
}

type formFieldProvider interface {
	FormFields(ctx context.Context) ([]page.FormField, error)
}

// drawnObjectProvider reports everything the page draws, each clipped to what
// is visible. It is optional in the same way rulings are: a backend that cannot
// supply it still works, and the ink-based proposals simply do not exist.
type drawnObjectProvider interface {
	DrawnObjects(ctx context.Context) ([]page.DrawnObject, error)
}

type ExtractionOptions struct {
	DetectTables bool
	ReadingOrder bool
	// MaxParallelPages controls page-level parallelism. Values <=0 use
	// a bounded GOMAXPROCS default; 1 forces serial extraction.
	MaxParallelPages int
	// DetectStructure reclassifies assembled paragraph blocks that begin with a
	// list marker into Markdown list items (see structure.go). ExtractMarkdown
	// enables this by default; callers using ExtractMarkdownWithOptions can
	// leave it false for legacy plain-paragraph output.
	DetectStructure bool
	// DetectHeadings emits Markdown ATX headings for font-size-prominent lines.
	// ExtractMarkdown enables this by default because DPBench MHS depends on
	// heading signatures; callers using ExtractMarkdownWithOptions can leave it
	// false for legacy plain-paragraph output.
	DetectHeadings bool
	// EnableInlineFormatting opts paragraph rendering into inline bold/italic/code
	// emission driven by the LineElement runs produced by AssembleLineElements.
	// It is OFF by default; with it off, paragraph text is byte-identical to the
	// legacy line.Text path. ExtractMarkdown leaves it false.
	EnableInlineFormatting bool
	TableDetection         doctable.DetectionOptions
	// ClassifyThenRoute selects the alternate classify-then-route pipeline
	// (Task 5 of docs/plans/2026-08-06-learned-layout-classifier.md): assemble
	// every cell into lines first, then route each line to its destination,
	// instead of carving figures and tables out before lines exist. Every
	// routing decision is still the existing heuristics' — the flag isolates
	// refactor risk from model risk so the reroute can be proven neutral on
	// DPBench before anything learned enters the loop. OFF by default; the
	// default path is untouched.
	ClassifyThenRoute bool
	// LearnedFormulaRouting migrates the Formula class from the heuristics to
	// the model within the rerouted path (Task 6). A candidate table whose
	// lines are predominantly Formula is rejected and its cells return to the
	// prose path. Requires ClassifyThenRoute; OFF by default.
	LearnedFormulaRouting bool
	// LearnedRouting hands every line-class decision to the model: headings,
	// list items and figure innards, on top of the Formula rule. The hand-tuned
	// detectors for those classes are bypassed, not deleted — deleting them is
	// the final step, once this measures well on every class. Requires
	// ClassifyThenRoute; OFF by default.
	LearnedRouting bool
	// LearnedColumns derives table column boundaries with the FinTabNet-trained
	// model instead of the densest-row heuristic. Independent of the line
	// model: it changes table STRUCTURE, which is what TEDS scores, rather than
	// which regions are tables. OFF by default.
	LearnedColumns bool
	// LearnedRegions gates the structural classes with the REGION model: a run
	// of same-label lines only stands if the second stage accepts it. Requires
	// LearnedRouting; OFF by default.
	LearnedRegions bool
	// InkProposals asks the backend for everything the page draws, so region
	// candidates can be built from ink as well as from assembled text lines.
	// It costs one extra content-stream walk per page and nothing else: no
	// decision reads it unless a learned stage is also on.
	InkProposals bool
	// SplitColumnLines cuts assembled lines at horizontal gaps that persist
	// across their neighbours. It changes the LINE SET every later stage sees,
	// so it is the one option here that is not purely additive.
	SplitColumnLines bool
	// LearnedProposals runs the full region stage — propose, classify,
	// suppress — and exposes the result. It implies InkProposals, because half
	// the proposal sources are ink.
	LearnedProposals bool

	// drawn is the per-page result of that walk. It is unexported because it is
	// not a caller's choice — the page stage fills it in on the way through,
	// and a caller setting it by hand would be describing a different page.
	drawn []page.DrawnObject
}

const defaultMaxParallelPages = 12

func ExtractMarkdown(ctx context.Context, doc Document) (string, error) {
	return ExtractMarkdownWithOptions(ctx, doc, ExtractionOptions{DetectTables: true, ReadingOrder: true, DetectStructure: true, DetectHeadings: true})
}

func ExtractMarkdownWithOptions(ctx context.Context, doc Document, options ExtractionOptions) (string, error) {
	ctx, span := tracer.Start(ctx, "docmill.convert")
	defer span.End()
	convertStart := time.Now()

	pageCount, err := doc.PageCount(ctx)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	span.SetAttributes(attribute.Int("pages", pageCount))

	pageBlocks := make([][]markdownBlock, pageCount)
	pageSizes := make([]geom.Size, pageCount)
	if err := extractPages(ctx, doc, pageCount, options, pageBlocks, pageSizes); err != nil {
		span.RecordError(err)
		return "", err
	}

	// Re-level headings against the whole-document numbering hierarchy so deep
	// subsections nest coherently across pages (a "4.3.1" on a page lacking its
	// "4"/"4.3" parents renders ### rather than being collapsed to #). For a
	// single-page document this reproduces the per-page levels exactly.
	if options.DetectHeadings {
		assignDocumentHeadingLevels(pageBlocks)
	}

	// Stitch tables that continue from the bottom of one page to the top of the
	// next into a single table before assembling document text. This is a no-op
	// (leaving every page byte-identical) unless an adjacent page pair satisfies
	// the geometric continuation gate in connect.go.
	connectCrossPageTables(pageBlocks, pageSizes)

	parts := make([]string, 0, pageCount)
	for _, blocks := range pageBlocks {
		text := joinPageBlocks(blocks)
		if text != "" {
			parts = append(parts, text)
		}
	}

	out := strings.Join(parts, "\n\n")
	convertDuration.Record(ctx, time.Since(convertStart).Seconds())
	outputBytes.Record(ctx, int64(len(out)))
	span.SetAttributes(attribute.Int("output.bytes", len(out)))
	return out, nil
}

func protectedHeadingCellIndexes(cells []page.TextCell, rulings []page.RulingSegment, options ExtractionOptions) map[int]bool {
	if !options.DetectTables || len(cells) == 0 {
		return nil
	}
	detected := doctable.DetectTables(cells, rulings, options.TableDetection)
	if len(detected.Tables) == 0 {
		return nil
	}
	protected := make(map[int]bool)
	for _, detectedTable := range detected.Tables {
		if !protectsHeadingCaptionlessGrid(detectedTable) {
			continue
		}
		for _, cell := range detectedTable.TextCells {
			protected[cell.Index] = true
		}
	}
	if len(protected) == 0 {
		return nil
	}
	return protected
}

func protectsHeadingCaptionlessGrid(detected doctable.DetectedTable) bool {
	data := detected.Data
	if data.NumRows != 3 || data.NumCols != 4 {
		return false
	}
	grid := data.Grid()
	for column := 0; column < data.NumCols; column++ {
		if strings.TrimSpace(grid[0][column].Text) == "" {
			return false
		}
	}
	bodyRows := 0
	for row := 1; row < data.NumRows; row++ {
		key := strings.TrimSpace(grid[row][0].Text)
		if !isProtectedCaptionlessGridRowKey(key) {
			return false
		}
		bodyRows++
	}
	return bodyRows >= 2
}

func isProtectedCaptionlessGridRowKey(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 24 || len(strings.Fields(text)) > 3 {
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

func mergeProtectedCellIndexes(left, right map[int]bool) map[int]bool {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	merged := make(map[int]bool, len(left)+len(right))
	for index := range left {
		merged[index] = true
	}
	for index := range right {
		merged[index] = true
	}
	return merged
}

func extractPages(ctx context.Context, doc Document, pageCount int, options ExtractionOptions, pageBlocks [][]markdownBlock, pageSizes []geom.Size) error {
	parallelism := options.MaxParallelPages
	if parallelism <= 0 {
		parallelism = min(runtime.GOMAXPROCS(0), defaultMaxParallelPages)
	}
	if parallelism < 1 {
		parallelism = 1
	}
	if parallelism > pageCount {
		parallelism = pageCount
	}
	if parallelism <= 1 {
		for index := range pageCount {
			blocks, size, err := extractPageBlocksSafe(ctx, doc, index, options)
			if err != nil {
				return err
			}
			pageBlocks[index] = blocks
			pageSizes[index] = size
		}
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for worker := 0; worker < parallelism; worker++ {
		wg.Go(func() {
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				blocks, size, err := extractPageBlocksSafe(ctx, doc, index, options)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				// Each worker writes its own page slot, so the parallel writes to
				// distinct indexes of pageBlocks/pageSizes do not race.
				pageBlocks[index] = blocks
				pageSizes[index] = size
			}
		})
	}

sendJobs:
	for index := range pageCount {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}
	return ctx.Err()
}

// extractPageBlocksSafe wraps extractPageBlocks with a per-page panic-recovery
// boundary. The parser reproduces PDFium's behaviour on malformed input and can
// panic on adversarial PDFs. A panic in a goroutine is unrecoverable by the
// caller, so during parallel extraction an unguarded panic in a worker would
// crash the whole process rather than surface to ExtractMarkdown's caller.
// Recovering in the goroutine that runs each page contains the failure to that
// page and converts it into an ordinary error: a single malformed page fails
// the conversion (like any other page error) instead of taking down the
// process, and callers never need their own recover() regardless of parallelism.
func extractPageBlocksSafe(ctx context.Context, doc Document, index int, options ExtractionOptions) (blocks []markdownBlock, size geom.Size, err error) {
	defer func() {
		if r := recover(); r != nil {
			blocks = nil
			size = geom.Size{}
			err = fmt.Errorf("docmill: recovered panic extracting page %d: %v\n%s", index, r, debug.Stack())
		}
	}()
	return extractPageBlocks(ctx, doc, index, options)
}

func extractPageBlocks(ctx context.Context, doc Document, index int, options ExtractionOptions) ([]markdownBlock, geom.Size, error) {
	pageCtx, pageSpan := tracer.Start(ctx, "docmill.page", trace.WithAttributes(attribute.Int("page.index", index)))
	pageStart := time.Now()
	defer pageSpan.End()

	blocks, size, err := extractPage(pageCtx, doc, index, options, pageSpan)
	if err != nil {
		pageSpan.RecordError(err)
		return nil, geom.Size{}, err
	}

	pagesProcessed.Add(pageCtx, 1)
	pageDuration.Record(pageCtx, time.Since(pageStart).Seconds())
	return blocks, size, nil
}

// joinPageBlocks reproduces the legacy per-page text assembly: drop blocks with
// empty Text and join the rest with a blank line. Keeping this identical to the
// old extractPageText join is what guarantees single-page byte-identity.
func joinPageBlocks(blocks []markdownBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func extractPage(ctx context.Context, doc Document, index int, options ExtractionOptions, span trace.Span) ([]markdownBlock, geom.Size, error) {
	pdfPage, err := runStage(ctx, "page_open", func(ctx context.Context) (Page, error) {
		return doc.Page(ctx, index)
	})
	if err != nil {
		return nil, geom.Size{}, err
	}
	size, err := runStage(ctx, "page_size", pdfPage.Size)
	if err != nil {
		return nil, geom.Size{}, err
	}
	cells, err := runStage(ctx, "text_cells", pdfPage.TextCells)
	if err != nil {
		return nil, geom.Size{}, err
	}
	var rulings []page.RulingSegment
	var wordCells []page.TextCell
	var formFields []page.FormField
	var drawn []page.DrawnObject
	if provider, ok := pdfPage.(formFieldProvider); ok {
		formFields, err = runStage(ctx, "form_fields", provider.FormFields)
		if err != nil {
			return nil, geom.Size{}, err
		}
	}
	if options.DetectTables {
		if provider, ok := pdfPage.(rulingSegmentProvider); ok {
			rulings, err = runStage(ctx, "ruling_segments", provider.RulingSegments)
			if err != nil {
				return nil, geom.Size{}, err
			}
		}
		if provider, ok := pdfPage.(wordTextCellProvider); ok {
			wordCells, err = runStage(ctx, "word_text_cells", provider.WordTextCells)
			if err != nil {
				return nil, geom.Size{}, err
			}
		}
	}
	if options.InkProposals || options.LearnedProposals {
		if provider, ok := pdfPage.(drawnObjectProvider); ok {
			drawn, err = runStage(ctx, "drawn_objects", provider.DrawnObjects)
			if err != nil {
				return nil, geom.Size{}, err
			}
		}
	}
	span.SetAttributes(attribute.Int("text_cells", len(cells)))
	textCellsPerPage.Record(ctx, int64(len(cells)))

	options.drawn = drawn
	blocks, err := pageMarkdownBlocks(ctx, cells, wordCells, rulings, formFields, size, options)
	if err != nil {
		return nil, geom.Size{}, err
	}
	return blocks, size, nil
}

type markdownBlock struct {
	Index         int
	Text          string
	Box           geom.Box
	FontSize      float64
	LineCount     int
	HeadingLevel  int
	ListCandidate bool
	ListContentL  float64
	// tableData carries the structured table for a rendered table block (nil for
	// every non-table block). tableBox is the table's bounding box on its page.
	// Both are populated only so the document-level pass can test cross-page
	// continuation (see connect.go); they never affect single-page rendering,
	// which still flows entirely through Text.
	tableData *doctable.Data
	tableBox  geom.Box
}

func pageMarkdownBlocks(ctx context.Context, cells []page.TextCell, wordCells []page.TextCell, rulings []page.RulingSegment, formFields []page.FormField, size geom.Size, options ExtractionOptions) ([]markdownBlock, error) {
	if options.ClassifyThenRoute {
		return pageMarkdownBlocksRouted(ctx, cells, wordCells, rulings, formFields, size, options)
	}
	// Reassign each cell a column-aware reading-order Index, then assemble text
	// per column so that lines are never merged across a column gutter. When the
	// detector is not confident the page is multi-column, orderCells is identity
	// and partitionColumns returns a single group, so single-column docs are
	// untouched.
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

	var headingBlocks []markdownBlock
	if options.DetectHeadings {
		_, finishStage := startStage(ctx, "heading_detect")
		protected := protectedHeadingCellIndexes(cells, rulings, options)
		if options.DetectStructure {
			protected = mergeProtectedCellIndexes(protected, protectedListLineCellIndexes(cells))
		}
		protected = mergeProtectedCellIndexes(protected, denseIndexLineCellIndexes(cells, size))
		headingBlocks, cells = splitHeadingCellsProtecting(cells, size, protected)
		finishStage(nil)
	}

	assembleByColumn := func(textCells []page.TextCell) []markdownBlock {
		start := time.Now()
		_, span := tracer.Start(ctx, "pipeline.assemble")
		defer func() {
			span.End()
			recordStage(ctx, "assemble", start)
		}()
		paragraphOptions := ParagraphOptions{EnableInlineFormatting: options.EnableInlineFormatting}
		if !options.ReadingOrder {
			return assembleWithToc(AssembleLineElements(textCells, ParagraphOptions{}.withDefaults().LineTolerance), paragraphOptions)
		}
		var blocks []markdownBlock
		for _, group := range partitionColumns(textCells, size) {
			blocks = append(blocks, assembleWithToc(AssembleLineElements(group, ParagraphOptions{}.withDefaults().LineTolerance), paragraphOptions)...)
		}
		return blocks
	}

	if !options.DetectTables {
		blocks := append([]markdownBlock{}, headingBlocks...)
		blocks = append(blocks, assembleByColumn(cells)...)
		if options.DetectStructure {
			_, finishStage := startStage(ctx, "structure")
			blocks = detectStructure(blocks)
			finishStage(nil)
		}
		_, finishStage := startStage(ctx, "postprocess")
		blocks = filterFigureInternalLabelBlocks(blocks, size)
		blocks = splitMarginalPageNumberBlocks(blocks, size)
		blocks = append(blocks, formBlocks...)
		sortMarkdownBlocks(blocks, size, !options.ReadingOrder)
		finishStage(nil)
		return blocks, nil
	}

	// Table detection is shared with the rerouted path (reroute.go) so that
	// "the reroute changes the ORDER, not the decisions" is enforced by there
	// being one definition, rather than merely asserted in a comment.
	detectStart := time.Now()
	_, detectSpan := tracer.Start(ctx, "pipeline.table_detect")
	detected, remainingCells := detectPageTables(cells, wordCells, rulings, size, options)
	detectSpan.SetAttributes(attribute.Int("tables", len(detected.Tables)))
	detectSpan.End()
	recordStage(ctx, "table_detect", detectStart)
	tablesDetected.Add(ctx, int64(len(detected.Tables)))

	blocks := append([]markdownBlock{}, headingBlocks...)
	blocks = append(blocks, assembleByColumn(remainingCells)...)
	if options.DetectStructure {
		_, finishStage := startStage(ctx, "structure")
		blocks = detectStructure(blocks)
		finishStage(nil)
	}
	_, finishStage := startStage(ctx, "postprocess")
	blocks = filterFigureInternalLabelBlocks(blocks, size)
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
	return blocks, nil
}

func splitCellsByIndexSet(cells []page.TextCell, indexes map[int]bool) ([]page.TextCell, []page.TextCell) {
	if len(indexes) == 0 {
		return cells, nil
	}
	unprotected := make([]page.TextCell, 0, len(cells)-len(indexes))
	protected := make([]page.TextCell, 0, len(indexes))
	for _, cell := range cells {
		if indexes[cell.Index] {
			protected = append(protected, cell)
			continue
		}
		unprotected = append(unprotected, cell)
	}
	return unprotected, protected
}

func mergePreferredTables(base, preferred []doctable.DetectedTable, cells []page.TextCell) []doctable.DetectedTable {
	if len(base) == 0 {
		return append([]doctable.DetectedTable(nil), preferred...)
	}
	if len(preferred) == 0 {
		return append([]doctable.DetectedTable(nil), base...)
	}

	merged := make([]doctable.DetectedTable, 0, len(base)+len(preferred))
	for _, table := range base {
		if overlapsAnyDetectedTable(table, preferred) {
			continue
		}
		merged = append(merged, table)
	}
	for _, table := range preferred {
		if overlapsAnyDetectedTable(table, base) || hasNearbyTableCaptionCell(table, cells) {
			merged = append(merged, table)
		}
	}
	return merged
}

func reassignDetectedTableTextFromWords(tables []doctable.DetectedTable, wordCells []page.TextCell, threshold float64) []doctable.DetectedTable {
	if len(tables) == 0 || len(wordCells) == 0 {
		return tables
	}

	out := append([]doctable.DetectedTable(nil), tables...)
	for index := range out {
		words := textCellsInsideBox(wordCells, out[index].Box, threshold)
		if len(words) == 0 {
			continue
		}
		out[index].Data = out[index].Data.WithAssignedText(words, threshold)
	}
	return out
}

func textCellsInsideBox(cells []page.TextCell, box geom.Box, threshold float64) []page.TextCell {
	out := make([]page.TextCell, 0)
	for _, cell := range cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		if cell.Box.IntersectionOverSelf(box) > threshold {
			out = append(out, cell)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out
}

func overlapsAnyDetectedTable(table doctable.DetectedTable, others []doctable.DetectedTable) bool {
	for _, other := range others {
		if detectedTablesOverlap(table, other) {
			return true
		}
	}
	return false
}

func detectedTablesOverlap(left, right doctable.DetectedTable) bool {
	const (
		minIoU              = 0.2
		minContainedOverlap = 0.6
	)
	return left.Box.IoU(right.Box) >= minIoU ||
		left.Box.IntersectionOverSelf(right.Box) >= minContainedOverlap ||
		right.Box.IntersectionOverSelf(left.Box) >= minContainedOverlap
}

func hasNearbyTableCaptionCell(table doctable.DetectedTable, cells []page.TextCell) bool {
	const (
		maxCaptionGap = 96.0
		xSlack        = 24.0
		insideOverlap = 0.3
	)
	tableTop, tableBottom := verticalBounds(table.Box)
	for _, cell := range cells {
		text := strings.TrimSpace(cell.Text)
		if text == "" || !startsTableCaptionCue(text) {
			continue
		}
		if cell.Box.IntersectionOverSelf(table.Box) > insideOverlap {
			continue
		}
		if cell.Box.R < table.Box.L-xSlack || cell.Box.L > table.Box.R+xSlack {
			continue
		}
		cellTop, cellBottom := verticalBounds(cell.Box)
		gap := 0.0
		switch {
		case cellBottom <= tableTop:
			gap = tableTop - cellBottom
		case cellTop >= tableBottom:
			gap = cellTop - tableBottom
		default:
			gap = 0
		}
		if gap <= maxCaptionGap {
			return true
		}
	}
	return false
}

func startsTableCaptionCue(text string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(tokens) == 0 {
		return false
	}
	return tokens[0] == "table" || tokens[0] == "tab"
}

func verticalBounds(box geom.Box) (float64, float64) {
	if box.T <= box.B {
		return box.T, box.B
	}
	return box.B, box.T
}

func reserveCellIndexGaps(cells []page.TextCell) []page.TextCell {
	out := append([]page.TextCell(nil), cells...)
	for i := range out {
		out[i].Index *= 2
	}
	return out
}

func formFieldMarkdownBlocks(fields []page.FormField, cells []page.TextCell, size geom.Size) []markdownBlock {
	if len(fields) == 0 {
		return nil
	}

	fields = append([]page.FormField(nil), fields...)
	for i := range fields {
		if fields[i].Box.Origin != geom.TopLeft {
			fields[i].Box = fields[i].Box.WithOrigin(geom.TopLeft, size.Height)
		}
	}
	sort.SliceStable(fields, func(i, j int) bool {
		if boxTop(fields[i].Box) != boxTop(fields[j].Box) {
			return boxTop(fields[i].Box) < boxTop(fields[j].Box)
		}
		return fields[i].Box.L < fields[j].Box.L
	})

	blocks := make([]markdownBlock, 0, len(fields))
	for _, field := range fields {
		text := formFieldMarkdown(field)
		if text == "" {
			continue
		}
		blocks = append(blocks, markdownBlock{
			Index: formFieldBlockIndex(field.Box, cells, len(blocks)),
			Text:  text,
			Box:   field.Box,
		})
	}
	return blocks
}

func formFieldMarkdown(field page.FormField) string {
	name := collapseSpaces(field.Name)
	value := collapseSpaces(field.Value)
	if name == "" || !formFieldHasFilledValue(value) {
		return ""
	}
	return name + ": " + value
}

func formFieldHasFilledValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.EqualFold(value, "Off")
}

func formFieldBlockIndex(box geom.Box, cells []page.TextCell, ordinal int) int {
	if len(cells) == 0 {
		return ordinal * 2
	}
	index := cells[len(cells)-1].Index + 1
	for _, cell := range cells {
		if boxComesBefore(box, cell.Box) {
			return cell.Index - 1
		}
		index = cell.Index + 1
	}
	return index
}

func boxComesBefore(left, right geom.Box) bool {
	leftTop := boxTop(left)
	rightTop := boxTop(right)
	if leftTop != rightTop {
		return leftTop < rightTop
	}
	return left.L < right.L
}

func boxTop(box geom.Box) float64 {
	if box.T < box.B {
		return box.T
	}
	return box.B
}

func textCellsOutsideTables(cells []page.TextCell, tables []doctable.DetectedTable, threshold float64) []page.TextCell {
	if len(tables) == 0 {
		return cells
	}
	out := make([]page.TextCell, 0, len(cells))
	for _, cell := range cells {
		insideTable := false
		for _, table := range tables {
			if cell.Box.IntersectionOverSelf(table.Box) > threshold {
				insideTable = true
				break
			}
		}
		if !insideTable {
			out = append(out, cell)
		}
	}
	return out
}

func normalisedTableOverlapThreshold(options doctable.DetectionOptions) float64 {
	if options.TextOverlapThreshold > 0 {
		return options.TextOverlapThreshold
	}
	return 0.3
}

// splitMarginalPageNumberCells removes standalone page-number cells sitting in
// the page's top/bottom margin from the table-detection input, returning them
// separately so they flow to the ordinary text path. A table, equation block,
// or figure reaching the page edge otherwise swallows the page number into a
// cell mid-content. Three co-signals gate the extraction, so a bare number
// that is table DATA (a year or count in a margin-band row) is never pulled
// out:
//   - position: the cell sits in the page's top/bottom margin band (the same
//     band splitMarginalPageNumberBlocks uses);
//   - alone on its line: a number inside a footer sentence is the
//     trailing-page-number split's job;
//   - vertically isolated: page furniture is separated from the nearest
//     content by well over a row pitch, whereas a table row has neighbouring
//     rows within roughly a line height.
func splitMarginalPageNumberCells(cells []page.TextCell, size geom.Size) ([]page.TextCell, []page.TextCell) {
	const lineTolerance = 4.0
	if size.Height <= 0 || len(cells) == 0 {
		return cells, nil
	}
	remaining := make([]page.TextCell, 0, len(cells))
	marginal := make([]page.TextCell, 0, 1)
	for index, cell := range cells {
		if !isStandalonePageNumber(strings.TrimSpace(cell.Text)) || !isMarginalBlock(markdownBlock{Box: cell.Box}, size) {
			remaining = append(remaining, cell)
			continue
		}
		isolation := math.Max(2.5*cell.Box.Height(), 18)
		aloneOnLine := true
		isolated := true
		for otherIndex, other := range cells {
			if otherIndex == index {
				continue
			}
			distance := math.Abs(other.Box.CenterY() - cell.Box.CenterY())
			if distance <= lineTolerance {
				aloneOnLine = false
				break
			}
			if distance < isolation {
				isolated = false
			}
		}
		if !aloneOnLine || !isolated {
			remaining = append(remaining, cell)
			continue
		}
		marginal = append(marginal, cell)
	}
	return remaining, marginal
}

func splitMarginalPageNumberBlocks(blocks []markdownBlock, size geom.Size) []markdownBlock {
	if len(blocks) == 0 || size.Height <= 0 {
		return blocks
	}
	out := make([]markdownBlock, 0, len(blocks))
	for _, block := range blocks {
		prefix, pageNumber, ok := splitTrailingPageNumber(block, size)
		if !ok {
			out = append(out, block)
			continue
		}
		footer := block
		footer.Text = prefix
		number := block
		number.Text = pageNumber
		out = append(out, footer, number)
	}
	return out
}

func splitTrailingPageNumber(block markdownBlock, size geom.Size) (string, string, bool) {
	if block.HeadingLevel > 0 || block.LineCount != 1 || !isMarginalBlock(block, size) {
		return "", "", false
	}
	text := collapseSpaces(block.Text)
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "", "", false
	}
	pageNumber := fields[len(fields)-1]
	if !isStandalonePageNumber(pageNumber) {
		return "", "", false
	}
	prefix := strings.TrimSpace(strings.TrimSuffix(text, pageNumber))
	if prefix == "" || !hasLetter(prefix) {
		return "", "", false
	}
	if strings.Contains(prefix, "|") {
		return "", "", false
	}
	if mostlyUppercase(prefix) {
		return "", "", false
	}
	return prefix, pageNumber, true
}

func isStandalonePageNumber(text string) bool {
	if len(text) == 0 || len(text) > 4 {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sortMarkdownBlocks(blocks []markdownBlock, size geom.Size, demoteBottomFurniture bool) {
	sort.SliceStable(blocks, func(i, j int) bool {
		leftTier := markdownBlockSortTier(blocks[i], size, demoteBottomFurniture)
		rightTier := markdownBlockSortTier(blocks[j], size, demoteBottomFurniture)
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		return blocks[i].Index < blocks[j].Index
	})
}

func markdownBlockSortTier(block markdownBlock, size geom.Size, demoteBottomFurniture bool) int {
	if demoteBottomFurniture && isBottomMarginFurnitureBlock(block, size) {
		return 1
	}
	return 0
}

func isBottomMarginFurnitureBlock(block markdownBlock, size geom.Size) bool {
	if block.HeadingLevel > 0 || block.LineCount > 2 || strings.TrimSpace(block.Text) == "" {
		return false
	}
	if size.Width > 0 && block.Box.Width() >= size.Width*0.85 {
		return false
	}
	return isBottomMarginalBlock(block, size)
}

func isMarginalBlock(block markdownBlock, size geom.Size) bool {
	if size.Height <= 0 || block.Box.Height() <= 0 {
		return false
	}
	top := block.Box.T
	bottom := block.Box.B
	if bottom < top {
		top, bottom = bottom, top
	}
	margin := size.Height * 0.1
	if margin < 72 {
		margin = 72
	}
	return bottom <= margin || top >= size.Height-margin
}

func isBottomMarginalBlock(block markdownBlock, size geom.Size) bool {
	if size.Height <= 0 || block.Box.Height() <= 0 {
		return false
	}
	top := block.Box.T
	bottom := block.Box.B
	if bottom < top {
		top, bottom = bottom, top
	}
	if top >= size.Height {
		return false
	}
	margin := size.Height * 0.1
	return top >= size.Height-margin
}

func minTextCellIndex(cells []page.TextCell) int {
	if len(cells) == 0 {
		return 0
	}
	minimum := cells[0].Index
	for _, cell := range cells[1:] {
		if cell.Index < minimum {
			minimum = cell.Index
		}
	}
	return minimum
}
