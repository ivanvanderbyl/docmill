package pdf_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
	"github.com/stretchr/testify/require"
)

// readingOrderDoc wraps a single page of cells; the page reports the standard
// 100x200 fake size so the detector has a real width to work with.
func readingOrderDoc(cells []page.TextCell) fakeDocument {
	return fakeDocument{pages: []fakePage{{cells: cells}}}
}

func extractWithReadingOrder(t *testing.T, cells []page.TextCell) string {
	t.Helper()
	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), readingOrderDoc(cells), pdf.ExtractionOptions{
		ReadingOrder: true,
	})
	require.NoError(t, err)
	return got
}

func extractIdentity(t *testing.T, cells []page.TextCell) string {
	t.Helper()
	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), readingOrderDoc(cells), pdf.ExtractionOptions{
		ReadingOrder: false,
	})
	require.NoError(t, err)
	return got
}

// twoColumnCells builds a dense two-column page: rows rows of a left cell
// (x in [0,40]) and a right cell (x in [60,100]), with the two columns offset
// vertically by 6pt so the assembler keeps them on separate lines (real columns
// rarely share an exact baseline). Cells are appended interleaved
// (L0,R0,L1,R1,...) and given a large per-row vertical gap so each becomes its
// own paragraph. The cell count clears the detector's confidence gates.
func twoColumnCells(rows int) []page.TextCell {
	cells := make([]page.TextCell, 0, rows*2)
	idx := 1
	for r := range rows {
		top := float64(r * 6)
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("L%02d", r), 0, top, 40, top+4))
		idx++
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("R%02d", r), 60, top+1, 100, top+5))
		idx++
	}
	return cells
}

// orderOfFirst returns the index of the first block (paragraph) in out whose
// text contains marker, or -1.
func indexOfToken(out, token string) int {
	return strings.Index(out, token)
}

// TestOrderCellsReordersTwoColumns: with a dense, vertically-interleaved
// two-column page, every left-column row must appear before every right-column
// row, and rows within each column must stay top-to-bottom.
func TestOrderCellsReordersTwoColumns(t *testing.T) {
	t.Parallel()

	rows := 32 // 64 cells total, clears the cell-count gate.
	cells := twoColumnCells(rows)

	got := extractWithReadingOrder(t, cells)

	// All left tokens precede all right tokens.
	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", rows-1))
	firstRight := indexOfToken(got, "R00")
	require.GreaterOrEqual(t, lastLeft, 0, "missing last left token in %q", got)
	require.GreaterOrEqual(t, firstRight, 0, "missing first right token in %q", got)
	require.Less(t, lastLeft, firstRight, "left column must fully precede right column:\n%s", got)

	// Within each column, rows stay in top-to-bottom order.
	requireAscendingTokenPositions(t, got, "L", rows)
	requireAscendingTokenPositions(t, got, "R", rows)
}

// TestOrderCellsReordersRaggedTwoColumnBibliography covers reference pages where
// the right column ends earlier than the left column. Competitors that score the
// DPBench bibliography case well still read these pages column-by-column.
func TestOrderCellsReordersRaggedTwoColumnBibliography(t *testing.T) {
	t.Parallel()

	leftRows := 70
	rightRows := 32
	cells := make([]page.TextCell, 0, leftRows+rightRows)
	idx := 1
	for r := range leftRows {
		top := float64(r * 12)
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("L%02d", r), 0, top, 40, top+6))
		idx++
	}
	for r := range rightRows {
		top := float64(r * 12)
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("R%02d", r), 60, top, 100, top+6))
		idx++
	}

	got := extractWithReadingOrder(t, cells)

	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", leftRows-1))
	firstRight := indexOfToken(got, "R00")
	require.GreaterOrEqual(t, lastLeft, 0, "missing last left token in %q", got)
	require.GreaterOrEqual(t, firstRight, 0, "missing first right token in %q", got)
	require.Less(t, lastLeft, firstRight, "ragged columns must still read left column before right column:\n%s", got)
	requireAscendingTokenPositions(t, got, "L", leftRows)
	requireAscendingTokenPositions(t, got, "R", rightRows)
}

// requireAscendingTokenPositions asserts that prefix00..prefix(rows-1) appear in
// strictly increasing positions in out.
func requireAscendingTokenPositions(t *testing.T, out, prefix string, rows int) {
	t.Helper()
	prev := -1
	for r := range rows {
		pos := indexOfToken(out, fmt.Sprintf("%s%02d", prefix, r))
		require.Greater(t, pos, prev, "%s%02d out of order in:\n%s", prefix, r, out)
		prev = pos
	}
}

// TestOrderCellsSingleColumnIdentity: cells filling the page width leave no
// interior gutter, so the detector falls back to identity order (byte-identical
// to ReadingOrder off).
func TestOrderCellsSingleColumnIdentity(t *testing.T) {
	t.Parallel()

	cells := make([]page.TextCell, 0, 12)
	for r := range 12 {
		// 20pt row pitch with 10pt cells => 10pt gap (> 0.6*height), so each row
		// is its own paragraph.
		top := float64(r * 20)
		cells = append(cells, pdfTextCell(r+1, fmt.Sprintf("P%02d", r), 0, top, 100, top+10))
	}

	got := extractWithReadingOrder(t, cells)
	identity := extractIdentity(t, cells)

	require.Equal(t, identity, got, "single-column reading order must equal identity")
	require.Equal(t, "P00\n\nP01\n\nP02\n\nP03\n\nP04\n\nP05\n\nP06\n\nP07\n\nP08\n\nP09\n\nP10\n\nP11", got)
}

func TestReadingOrderKeepsDenseSingleColumnListFragmentsTogether(t *testing.T) {
	t.Parallel()

	const rows = 12
	cells := make([]page.TextCell, 0, rows*8)
	idx := 1
	for row := range rows {
		top := 24.0 + float64(row)*18
		cells = append(cells,
			pdfTextCellWithFont(idx, "●", 90, top+3, 96, top+9, 11),
			pdfTextCellWithFont(idx+1, "Section 2", 108, top, 168, top+10, 11),
			pdfTextCellWithFont(idx+2, ".", 169, top+7, 171, top+9, 11),
			pdfTextCellWithFont(idx+3, "3", 172, top, 177, top+9, 11),
			pdfTextCellWithFont(idx+4, ".", 178, top+7, 180, top+9, 11),
			pdfTextCellWithFont(idx+5, "6 names Author", 181, top, 280, top+10, 11),
			pdfTextCellWithFont(idx+6, "’", 281, top, 283, top+4, 11),
			pdfTextCellWithFont(idx+7, "s internal fork", 284, top, 380, top+9, 11),
		)
		idx += 8
	}

	doc := fakeDocument{pages: []fakePage{{
		size:  geom.Size{Width: 420, Height: 260},
		cells: cells,
	}}}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder:     true,
		DetectStructure:  true,
		DetectTables:     false,
		MaxParallelPages: 1,
	})

	require.NoError(t, err)
	expected := strings.Repeat("- Section 2.3.6 names Author's internal fork\n\n", rows)
	require.Equal(t, strings.TrimSpace(expected), got)
}

func TestOrderCellsReordersSingleColumnOutOfStreamOrder(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "Body paragraph", 0, 80, 100, 90),
		pdfTextCell(2, "Figure caption", 0, 20, 100, 30),
	}

	got := extractWithReadingOrder(t, cells)

	require.Equal(t, "Figure caption\n\nBody paragraph", got)
}

// TestOrderCellsBannerThenTwoColumns: a full-width banner (title) above two
// columns must come first, then the left column, then the right column.
func TestOrderCellsBannerThenTwoColumns(t *testing.T) {
	t.Parallel()

	rows := 32
	cells := twoColumnCells(rows)
	// Prepend a full-width banner (width 100 > 0.66*100) above the columns. Push
	// the column rows down so the banner is clearly first.
	for i := range cells {
		cells[i].Box.T += 20
		cells[i].Box.B += 20
		cells[i].Index += 1
	}
	banner := pdfTextCell(1, "TITLE", 0, 0, 100, 8)
	cells = append([]page.TextCell{banner}, cells...)

	got := extractWithReadingOrder(t, cells)

	titlePos := indexOfToken(got, "TITLE")
	firstLeft := indexOfToken(got, "L00")
	firstRight := indexOfToken(got, "R00")
	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", rows-1))

	require.GreaterOrEqual(t, titlePos, 0)
	require.Less(t, titlePos, firstLeft, "banner must precede the left column:\n%s", got)
	require.Less(t, lastLeft, firstRight, "left column must precede right column:\n%s", got)
}

// TestOrderCellsTooFewCellsIdentity: fewer than the detection floor means we
// never attempt reordering, even with a clear two-column geometry.
func TestOrderCellsTooFewCellsIdentity(t *testing.T) {
	t.Parallel()

	// Clear two-column geometry but only 4 cells (< the detection floor).
	cells := []page.TextCell{
		pdfTextCell(1, "L1", 0, 0, 40, 10),
		pdfTextCell(2, "R1", 60, 0, 100, 10),
		pdfTextCell(3, "L2", 0, 40, 40, 50),
		pdfTextCell(4, "R2", 60, 40, 100, 50),
	}

	got := extractWithReadingOrder(t, cells)
	identity := extractIdentity(t, cells)

	require.Equal(t, identity, got)
}

// TestOrderCellsSparseTwoColumnGeometryStaysIdentity: a clear gutter but too few
// cells (below the column confidence gate) must fall back to identity, so sparse
// figure-label pages are never reordered.
func TestOrderCellsSparseTwoColumnGeometryStaysIdentity(t *testing.T) {
	t.Parallel()

	// 10 cells (clears the >=8 floor) with a clean centre gutter, but well below
	// the column cell-count gate.
	cells := make([]page.TextCell, 0, 10)
	idx := 1
	for r := range 5 {
		top := float64(r * 30)
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("L%d", r), 0, top, 40, top+10))
		idx++
		cells = append(cells, pdfTextCell(idx, fmt.Sprintf("R%d", r), 60, top+15, 100, top+25))
		idx++
	}

	got := extractWithReadingOrder(t, cells)
	identity := extractIdentity(t, cells)

	require.Equal(t, identity, got, "sparse two-column geometry must stay in identity order")
}

// TestOrderCellsWithTableDetectionEnabled: with both ReadingOrder and
// DetectTables enabled (the production default), a dense two-column body that is
// not a table still reads column-by-column.
func TestOrderCellsWithTableDetectionEnabled(t *testing.T) {
	t.Parallel()

	rows := 32
	cells := twoColumnCells(rows)

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), readingOrderDoc(cells), pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: true,
	})
	require.NoError(t, err)

	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", rows-1))
	firstRight := indexOfToken(got, "R00")
	require.GreaterOrEqual(t, lastLeft, 0)
	require.GreaterOrEqual(t, firstRight, 0)
	require.Less(t, lastLeft, firstRight, "left column must precede right column:\n%s", got)
}

func TestReadingOrderPlacesBottomMarginFurnitureAfterBodyByGeometry(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCell(1, "Internal review footer", 40, 744, 210, 756),
				pdfTextCell(2, "Body content should be read first.", 72, 160, 500, 172),
				pdfTextCell(3, "The next paragraph stays in body flow.", 72, 196, 500, 208),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: false,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "Body content should be read first.\n\nThe next paragraph stays in body flow.\n\nInternal review footer", got)
}

func TestReadingOrderDoesNotMoveTopMarginTextByLiteralPhrase(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCell(1, "This project has been funded with the support of the European Commission.", 40, 24, 572, 36),
				pdfTextCell(2, "Project No: 2021-2-FR02-KA220-YOU-000048126", 40, 42, 360, 54),
				pdfTextCell(3, "As seen in this chart of responses, every age group was represented.", 72, 160, 500, 172),
				pdfTextCell(4, "For responders' profession, Youth Workers and Project Managers were common.", 72, 196, 500, 208),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "This project has been funded with the support of the European Commission. Project No: 2021-2-FR02-KA220-YOU-000048126\n\nAs seen in this chart of responses, every age group was represented.\n\nFor responders' profession, Youth Workers and Project Managers were common.", got)
}

func TestReadingOrderSplitsMarginalFooterPageNumber(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 420, Height: 666},
			cells: []page.TextCell{
				pdfTextCell(1, "Body paragraph", 57, 440, 385, 450),
				pdfTextCell(2, "part iv: serious geographies of play", 57, 630, 205, 635),
				pdfTextCell(3, "115", 374, 630, 385, 637),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "Body paragraph\n\npart iv: serious geographies of play\n\n115", got)
}

func TestReadingOrderKeepsPipeFooterWithPageNumberTogether(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 420, Height: 666},
			cells: []page.TextCell{
				pdfTextCell(1, "Body paragraph", 57, 440, 385, 450),
				pdfTextCell(2, "Soil Formation |", 290, 630, 360, 635),
				pdfTextCell(3, "27", 374, 630, 385, 637),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "Body paragraph\n\nSoil Formation | 27", got)
}

func TestReadingOrderKeepsAllCapsFooterWithPageNumberTogether(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCell(1, "Body paragraph", 57, 645, 558, 657),
				pdfTextCell(2, "BEHAVIORAL ECONOMICS PRACTICUM 213", 392, 754, 555, 759),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "Body paragraph\n\nBEHAVIORAL ECONOMICS PRACTICUM 213", got)
}

// TestReadingOrderGraphSpanningTitleThenColumns: a full-width spanning title
// above two columns reads title first, then the whole left column, then the
// whole right column. The directional graph makes the title the sole head (it
// has no above-neighbour) and follows each column's chain depth-first.
func TestReadingOrderGraphSpanningTitleThenColumns(t *testing.T) {
	t.Parallel()

	rows := 32
	cells := twoColumnCells(rows)
	for i := range cells {
		cells[i].Box.T += 20
		cells[i].Box.B += 20
	}
	title := pdfTextCell(0, "SPANTITLE", 0, 0, 100, 8)
	cells = append([]page.TextCell{title}, cells...)

	got := extractWithReadingOrder(t, cells)

	titlePos := indexOfToken(got, "SPANTITLE")
	firstLeft := indexOfToken(got, "L00")
	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", rows-1))
	firstRight := indexOfToken(got, "R00")

	require.GreaterOrEqual(t, titlePos, 0)
	require.Less(t, titlePos, firstLeft, "spanning title precedes the left column:\n%s", got)
	require.Less(t, lastLeft, firstRight, "left column fully precedes the right column:\n%s", got)
	requireAscendingTokenPositions(t, got, "L", rows)
	requireAscendingTokenPositions(t, got, "R", rows)
}

// TestReadingOrderGraphFloatingBottomLineStaysInFlow: a full-width line BELOW
// both columns (a floating caption/footer band) is read in reading flow after
// the body it follows, not hoisted to the top. The old confidence-gated
// detector treated every full-width cell as a leading banner and emitted it
// first; the directional graph orders it by its actual position (its only
// neighbours are above it, so it is never a head).
func TestReadingOrderGraphFloatingBottomLineStaysInFlow(t *testing.T) {
	t.Parallel()

	rows := 32
	cells := twoColumnCells(rows)
	// Full-width caption strictly below every column row (rows reach y≈191).
	caption := pdfTextCell(0, "FLOATCAPTION", 0, 193, 100, 199)
	cells = append(cells, caption)

	got := extractWithReadingOrder(t, cells)

	captionPos := indexOfToken(got, "FLOATCAPTION")
	firstLeft := indexOfToken(got, "L00")
	lastLeft := indexOfToken(got, fmt.Sprintf("L%02d", rows-1))

	require.GreaterOrEqual(t, captionPos, 0)
	require.Greater(t, captionPos, firstLeft, "floating bottom line is not hoisted above the body:\n%s", got)
	require.Greater(t, captionPos, lastLeft, "floating bottom line follows the left column it sits under:\n%s", got)
}
