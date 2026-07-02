package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func TestAttachLeadingHeadingMarkers(t *testing.T) {
	t.Parallel()

	marker := page.TextCell{
		Index:    1,
		Text:     "3.",
		FontSize: 14,
		Box:      geom.Box{L: 120, T: 90, R: 130, B: 100, Origin: geom.TopLeft},
	}
	title := page.TextCell{
		Index:    2,
		Text:     "RECOLLECTION OF NATIONAL INITIATIVES",
		FontSize: 14,
		Box:      geom.Box{L: 158, T: 90, R: 345, B: 100, Origin: geom.TopLeft},
	}
	headings := []headingLine{{
		line: ParagraphTextLine{
			Text:     title.Text,
			BBox:     title.Box,
			MinIndex: title.Index,
			FontSize: title.FontSize,
			Cells:    []page.TextCell{title},
		},
		metric: 14,
		level:  1,
	}}

	got := attachLeadingHeadingMarkers(headings, []page.TextCell{marker, title})

	require.Len(t, got, 1)
	require.Equal(t, "3. RECOLLECTION OF NATIONAL INITIATIVES", got[0].line.Text)
	require.Equal(t, 1, got[0].line.MinIndex)
	require.Len(t, got[0].line.Cells, 2)
}

func TestAttachTrailingHeadingContinuations(t *testing.T) {
	t.Parallel()

	title := page.TextCell{
		Index:    1,
		Text:     "Author's Note to the",
		FontSize: 24,
		Box:      geom.Box{L: 111, T: 139, R: 316, B: 157, Origin: geom.TopLeft},
	}
	continuation := page.TextCell{
		Index:    2,
		Text:     "2021 Edition",
		FontSize: 24,
		Box:      geom.Box{L: 147, T: 165, R: 275, B: 183, Origin: geom.TopLeft},
	}
	headings := []headingLine{{
		line: ParagraphTextLine{
			Text:     title.Text,
			BBox:     title.Box,
			MinIndex: title.Index,
			FontSize: title.FontSize,
			Cells:    []page.TextCell{title},
		},
		metric: 24,
		level:  1,
	}}

	got := attachTrailingHeadingContinuations(headings, []page.TextCell{title, continuation})

	require.Len(t, got, 1)
	require.Equal(t, "Author's Note to the 2021 Edition", got[0].line.Text)
	require.Len(t, got[0].line.Cells, 2)
}

func TestAttachTrailingHeadingContinuationsIgnoresBodyText(t *testing.T) {
	t.Parallel()

	title := page.TextCell{
		Index:    1,
		Text:     "n. In-store Sorting and Recycling Bins.",
		FontSize: 12,
		Box:      geom.Box{L: 70, T: 90, R: 250, B: 102, Origin: geom.TopLeft},
	}
	body := page.TextCell{
		Index:    2,
		Text:     "McDonalds has installed sorting and recycling bins.",
		FontSize: 12,
		Box:      geom.Box{L: 75, T: 112, R: 285, B: 124, Origin: geom.TopLeft},
	}
	headings := []headingLine{{
		line: ParagraphTextLine{
			Text:     title.Text,
			BBox:     title.Box,
			MinIndex: title.Index,
			FontSize: title.FontSize,
			Cells:    []page.TextCell{title},
		},
		metric: 12,
		level:  1,
	}}

	got := attachTrailingHeadingContinuations(headings, []page.TextCell{title, body})

	require.Len(t, got, 1)
	require.Equal(t, "n. In-store Sorting and Recycling Bins.", got[0].line.Text)
	require.Len(t, got[0].line.Cells, 1)
}

func TestIsHeadingLineRejectsStandaloneTableAcronymEntry(t *testing.T) {
	t.Parallel()

	line := ParagraphTextLine{
		Text: "COMFREL",
		BBox: geom.Box{L: 88, T: 308, R: 132, B: 315, Origin: geom.TopLeft},
	}
	prev := ParagraphTextLine{
		Text: "5 Our Friends Association 27",
		BBox: geom.Box{L: 66, T: 291, R: 328, B: 298, Origin: geom.TopLeft},
	}
	next := ParagraphTextLine{
		Text: "26",
		BBox: geom.Box{L: 318, T: 308, R: 328, B: 315, Origin: geom.TopLeft},
	}

	got := isHeadingLine(line, 9, 9, geom.Size{Width: 420, Height: 580}, &prev, &next)

	require.False(t, got)
}

func TestDenseIndexRegionRecognisesFragmentedOutlineRows(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		{Index: 1, Text: "Alpha", FontSize: 11, Box: geom.Box{L: 72.04, T: 77.76, R: 116.86, B: 86.23, Origin: geom.TopLeft}},
		{Index: 2, Text: "3", FontSize: 11, Box: geom.Box{L: 534.23, T: 78.19, R: 539.56, B: 86.23, Origin: geom.TopLeft}},
		{Index: 3, Text: "1 Introduction", FontSize: 11, Box: geom.Box{L: 72.07, T: 94.84, R: 146.56, B: 103.31, Origin: geom.TopLeft}},
		{Index: 4, Text: "10", FontSize: 11, Box: geom.Box{L: 528.61, T: 95.27, R: 539.52, B: 103.31, Origin: geom.TopLeft}},
		{Index: 5, Text: "1", FontSize: 11, Box: geom.Box{L: 90.20, T: 112.35, R: 93.62, B: 120.23, Origin: geom.TopLeft}},
		{Index: 6, Text: ".", FontSize: 11, Box: geom.Box{L: 94.71, T: 118.98, R: 95.99, B: 120.39, Origin: geom.TopLeft}},
		{Index: 7, Text: "1 Model training and characteristics", FontSize: 11, Box: geom.Box{L: 96.90, T: 111.92, R: 278.60, B: 123.20, Origin: geom.TopLeft}},
		{Index: 8, Text: "\u200b", FontSize: 11, Box: geom.Box{L: 279.04, T: 120.21, R: 281.79, B: 120.23, Origin: geom.TopLeft}},
		{Index: 9, Text: "11", FontSize: 11, Box: geom.Box{L: 532.20, T: 112.35, R: 539.62, B: 120.23, Origin: geom.TopLeft}},
		{Index: 10, Text: "1", FontSize: 11, Box: geom.Box{L: 108.20, T: 129.43, R: 111.62, B: 137.31, Origin: geom.TopLeft}},
		{Index: 11, Text: ".", FontSize: 11, Box: geom.Box{L: 112.71, T: 136.06, R: 113.99, B: 137.47, Origin: geom.TopLeft}},
		{Index: 12, Text: "1", FontSize: 11, Box: geom.Box{L: 114.90, T: 129.43, R: 118.32, B: 137.31, Origin: geom.TopLeft}},
		{Index: 13, Text: ".", FontSize: 11, Box: geom.Box{L: 119.41, T: 136.06, R: 120.70, B: 137.47, Origin: geom.TopLeft}},
		{Index: 14, Text: "1 Training data and process", FontSize: 11, Box: geom.Box{L: 121.61, T: 129.00, R: 259.97, B: 140.28, Origin: geom.TopLeft}},
		{Index: 15, Text: "\u200b", FontSize: 11, Box: geom.Box{L: 260.42, T: 137.29, R: 263.17, B: 137.31, Origin: geom.TopLeft}},
		{Index: 16, Text: "11", FontSize: 11, Box: geom.Box{L: 532.20, T: 129.43, R: 539.62, B: 137.31, Origin: geom.TopLeft}},
	}
	lines := AssembleLineElements(cells, 4)
	heading := headingLine{line: buildParagraphTextLine([]page.TextCell{cells[5], cells[6], cells[7]}), metric: 11, level: 1}

	sourceIndex := sourceLineIndexForHeading(heading, lines)

	require.GreaterOrEqual(t, sourceIndex, 0)
	require.True(t, belongsToDenseIndexRegion(lines, sourceIndex, geom.Size{Width: 612, Height: 792}))
}

func TestDenseIndexLineCellIndexesIncludesStandaloneFolioContinuation(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		{Index: 1, Text: "1 Overview", FontSize: 11, Box: geom.Box{L: 72, T: 80, R: 140, B: 90, Origin: geom.TopLeft}},
		{Index: 2, Text: "10", FontSize: 11, Box: geom.Box{L: 530, T: 80, R: 540, B: 90, Origin: geom.TopLeft}},
		{Index: 3, Text: "1.1 A long entry that wraps before its folio", FontSize: 11, Box: geom.Box{L: 90, T: 98, R: 430, B: 108, Origin: geom.TopLeft}},
		{Index: 4, Text: "11", FontSize: 11, Box: geom.Box{L: 530, T: 110, R: 540, B: 118, Origin: geom.TopLeft}},
		{Index: 5, Text: "1.2 Next entry", FontSize: 11, Box: geom.Box{L: 90, T: 126, R: 190, B: 136, Origin: geom.TopLeft}},
		{Index: 6, Text: "12", FontSize: 11, Box: geom.Box{L: 530, T: 126, R: 540, B: 136, Origin: geom.TopLeft}},
	}

	indexes := denseIndexLineCellIndexes(cells, geom.Size{Width: 612, Height: 792})

	require.True(t, indexes[3])
	require.True(t, indexes[4])
}

func TestDenseIndexLineCellIndexesIncludesIndentedWrappedFolioContinuation(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		{Index: 1, Text: "Earlier dense index entry", FontSize: 11, Box: geom.Box{L: 126.13, T: 111.92, R: 362.75, B: 123.20, Origin: geom.TopLeft}},
		{Index: 2, Text: "\u200b", FontSize: 11, Box: geom.Box{L: 363.08, T: 120.22, R: 365.83, B: 120.23, Origin: geom.TopLeft}},
		{Index: 3, Text: "86", FontSize: 11, Box: geom.Box{L: 528.12, T: 112.35, R: 539.47, B: 120.39, Origin: geom.TopLeft}},
		{Index: 4, Text: "A long dense index entry that reaches the folio column before wrapping", FontSize: 11, Box: geom.Box{L: 126.13, T: 129.00, R: 530.95, B: 140.28, Origin: geom.TopLeft}},
		{Index: 5, Text: "\u200b", FontSize: 11, Box: geom.Box{L: 531.39, T: 137.29, R: 534.14, B: 137.31, Origin: geom.TopLeft}},
		{Index: 6, Text: "87", FontSize: 11, Box: geom.Box{L: 126.65, T: 143.51, R: 137.03, B: 151.56, Origin: geom.TopLeft}},
		{Index: 7, Text: "Following dense index entry", FontSize: 11, Box: geom.Box{L: 126.13, T: 160.11, R: 416.45, B: 171.44, Origin: geom.TopLeft}},
		{Index: 8, Text: "\u200b", FontSize: 11, Box: geom.Box{L: 416.49, T: 168.45, R: 419.24, B: 168.47, Origin: geom.TopLeft}},
		{Index: 9, Text: "88", FontSize: 11, Box: geom.Box{L: 528.29, T: 160.59, R: 539.45, B: 168.63, Origin: geom.TopLeft}},
	}

	indexes := denseIndexLineCellIndexes(cells, geom.Size{Width: 612, Height: 792})

	require.True(t, indexes[4])
	require.True(t, indexes[5])
	require.True(t, indexes[6])
}

// hl builds a headingLine for level-assignment tests.
func hl(index int, text string) headingLine {
	return headingLine{
		line: ParagraphTextLine{
			Text:     text,
			BBox:     geom.Box{L: 100, T: float64(index) * 20, R: 300, B: float64(index)*20 + 12, Origin: geom.TopLeft},
			MinIndex: index,
			FontSize: 14,
			Cells:    []page.TextCell{{Index: index, Text: text, FontSize: 14}},
		},
		metric: 14,
		level:  1,
	}
}

func TestHeadingNumberDepth(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"Introduction":          0,
		"Abstract":              0,
		"1 Introduction":        1,
		"4. THEORY":             1,
		"4.1 Introduction":      2,
		"7.2. Determination":    2,
		"4.3.1 Instruction":     3,
		"2.10.3 Deep Section":   3,
		"Chapter 4":             0,
		"Reference frameworks:": 0,
	}
	for text, want := range cases {
		require.Equalf(t, want, headingNumberDepth(text), "depth of %q", text)
	}
}

func TestAssignHeadingLevelsNestsNumberedSubsections(t *testing.T) {
	t.Parallel()
	// An unnumbered chapter heading anchors level 1; its numbered subsections nest
	// one level deeper.
	headings := []headingLine{
		hl(1, "Chapter 4"),
		hl(2, "Nonlinear equations"),
		hl(3, "4.1 Introduction"),
		hl(4, "4.2 Definitions"),
	}
	assignHeadingLevels(headings)
	require.Equal(t, 1, headings[0].level)
	require.Equal(t, 1, headings[1].level)
	require.Equal(t, 2, headings[2].level)
	require.Equal(t, 2, headings[3].level)
}

func TestAssignHeadingLevelsNormalisesOrphanSubsection(t *testing.T) {
	t.Parallel()
	// A page whose only heading is a numbered subsection with no shallower parent
	// renders at level 1, not an orphan ##.
	headings := []headingLine{hl(1, "1.5. Migrant Workers More at Risk")}
	assignHeadingLevels(headings)
	require.Equal(t, 1, headings[0].level)

	// But relative depth is preserved: 4.3 and 4.3.1 with no "4" parent become
	// H1 and H2.
	deep := []headingLine{hl(1, "4.3 Ablation Studies"), hl(2, "4.3.1 Instruction Tuning")}
	assignHeadingLevels(deep)
	require.Equal(t, 1, deep[0].level)
	require.Equal(t, 2, deep[1].level)
}

func TestAssignHeadingLevelsLeavesUnnumberedFlat(t *testing.T) {
	t.Parallel()
	headings := []headingLine{hl(1, "Abstract"), hl(2, "Introduction"), hl(3, "Conclusion")}
	assignHeadingLevels(headings)
	for _, h := range headings {
		require.Equal(t, 1, h.level)
	}
}

func TestAssignDocumentHeadingLevelsNestsAcrossPages(t *testing.T) {
	t.Parallel()
	// Page 1 establishes 2 and 2.1; page 2 carries only the deep subsections,
	// which a per-page normaliser would collapse to # — document-level keeps them
	// nested beneath their cross-page parents.
	pageBlocks := [][]markdownBlock{
		{
			{Text: "# 2 RSP evaluations", HeadingLevel: 1},
			{Text: "## 2.1 Risk process", HeadingLevel: 2},
		},
		{
			{Text: "# 2.1.3 Summary", HeadingLevel: 1},
			{Text: "# 2.1.3.1 On autonomy", HeadingLevel: 1},
		},
	}
	assignDocumentHeadingLevels(pageBlocks)
	require.Equal(t, "# 2 RSP evaluations", pageBlocks[0][0].Text)
	require.Equal(t, "## 2.1 Risk process", pageBlocks[0][1].Text)
	require.Equal(t, 3, pageBlocks[1][0].HeadingLevel)
	require.Equal(t, "### 2.1.3 Summary", pageBlocks[1][0].Text)
	require.Equal(t, 4, pageBlocks[1][1].HeadingLevel)
	require.Equal(t, "#### 2.1.3.1 On autonomy", pageBlocks[1][1].Text)
}

func TestAssignDocumentHeadingLevelsSinglePageOrphanStaysFlat(t *testing.T) {
	t.Parallel()
	// A single-page document whose only heading is a numbered subsection still
	// normalises to level 1 (no shallower context anywhere), matching per-page.
	pageBlocks := [][]markdownBlock{{{Text: "# 1.5 Migrant Workers", HeadingLevel: 1}}}
	assignDocumentHeadingLevels(pageBlocks)
	require.Equal(t, "# 1.5 Migrant Workers", pageBlocks[0][0].Text)
	require.Equal(t, 1, pageBlocks[0][0].HeadingLevel)
}

func TestDecimalHeadingLooksLikeFootnote(t *testing.T) {
	t.Parallel()
	mk := func(text string, fs float64) ParagraphTextLine {
		return ParagraphTextLine{Text: text, FontSize: fs, BBox: geom.Box{L: 100, T: 600, R: 320, B: 600 + fs, Origin: geom.TopLeft}}
	}
	size := geom.Size{Width: 612, Height: 792}

	// Footnote: a single-integer marker set below the body font.
	require.True(t, decimalHeadingLooksLikeFootnote(mk("4 Note that:", 10), 10, 11, size))
	// Numeric-scale / rubric row: body font, comma + lowercase prose continuation.
	require.True(t, decimalHeadingLooksLikeFootnote(mk("4 Rare, crucial insights comparable to world", 11), 11, 11, size))
	// Real single-integer section headings: body font, title-case, no prose comma.
	require.False(t, decimalHeadingLooksLikeFootnote(mk("4 Results", 11), 11, 11, size))
	require.False(t, decimalHeadingLooksLikeFootnote(mk("4 Limitations and Future Work", 11), 11, 11, size))
	// Oxford comma + conjunction stays a heading.
	require.False(t, decimalHeadingLooksLikeFootnote(mk("4 Methods, Results, and Discussion", 11), 11, 11, size))
	// A dotted subsection is unambiguous and never footnote-suppressed.
	require.False(t, decimalHeadingLooksLikeFootnote(mk("2.1.3 Summary, with caveats", 11), 11, 11, size))
}

func TestIsSingleIntegerDecimalHeading(t *testing.T) {
	t.Parallel()
	require.True(t, isSingleIntegerDecimalHeading("4 Results"))
	require.True(t, isSingleIntegerDecimalHeading("12 Conclusion"))
	require.False(t, isSingleIntegerDecimalHeading("2.1.3 Summary"))
	require.False(t, isSingleIntegerDecimalHeading("Abstract"))
	require.False(t, isSingleIntegerDecimalHeading("Introduction and overview"))
}

// centredBoldLine builds a single-cell bold visual line at the given horizontal
// span and font size, mirroring parliamentary-Hansard heading geometry (bold,
// centred on the page, inset from both margins, at or near the body point size).
func centredBoldLine(text string, fs, l, r float64, bold bool) ParagraphTextLine {
	weight := 400
	if bold {
		weight = 700
	}
	cell := page.TextCell{
		Index:      1,
		Text:       text,
		FontSize:   fs,
		FontWeight: weight,
		Box:        geom.Box{L: l, T: 100, R: r, B: 100 + fs, Origin: geom.TopLeft},
	}
	return ParagraphTextLine{
		Text:     text,
		BBox:     cell.Box,
		MinIndex: 1,
		FontSize: fs,
		Cells:    []page.TextCell{cell},
	}
}

func TestIsCentredBoldHeading(t *testing.T) {
	t.Parallel()
	size := geom.Size{Width: 595.3, Height: 841.9}
	const body = 10.5

	// Positive — real Hansard body-page geometry (from the line dump).
	// A larger-than-body bold section label.
	require.True(t, isCentredBoldHeading(centredBoldLine("BILLS", 12.0, 280.2, 314.8, true), 12.0, body, size))
	// A bold centred title carrying digits and parentheses.
	require.True(t, isCentredBoldHeading(centredBoldLine("Treasury Laws Amendment (Tax Reform No. 2) Bill 2026", 11.5, 156.7, 438.6, true), 11.5, body, size))
	// A bold centred title at the BODY point size — the case every font-size
	// path misses.
	require.True(t, isCentredBoldHeading(centredBoldLine("First Reading", 10.5, 267.1, 328.2, true), body, body, size))
	require.True(t, isCentredBoldHeading(centredBoldLine("Second Reading", 10.5, 262.2, 333.5, true), body, body, size))

	// Negative — a bold full-width paragraph lead (the speaker line). Its centre
	// is near the page centre but its left inset is tiny (spans the column), so
	// it must not be promoted.
	require.False(t, isCentredBoldHeading(centredBoldLine("The SPEAKER (Hon. Milton Dick) took the chair at 09:00", 10.5, 68.4, 538.6, true), body, body, size))
	// Negative — a left-aligned bold run-in (small left inset).
	require.False(t, isCentredBoldHeading(centredBoldLine("Dr CHALMERS (Rankin—Treasurer) (09:01): I move:", 10.5, 68.2, 307.5, true), body, body, size))
	// Negative — centred and narrow but NOT bold (a centred caption / date line).
	require.False(t, isCentredBoldHeading(centredBoldLine("25 July 2024 — 30 September 2025", 10.5, 262.0, 333.0, false), body, body, size))
	// Negative — bold, centred, narrow but set materially smaller than body
	// (a centred bold caption beneath a figure).
	require.False(t, isCentredBoldHeading(centredBoldLine("Figure caption text", 8.0, 262.0, 333.0, true), 8.0, body, size))
	// Negative — unknown page width never fires.
	require.False(t, isCentredBoldHeading(centredBoldLine("BILLS", 12.0, 280.2, 314.8, true), 12.0, body, geom.Size{}))

	// Negative — bold-centred NON-headings from the DPBench figure/title corpus
	// (text-cleanliness gate). All are geometrically centred+bold+inset but read
	// as prose/captions/authors, not headings.
	fp := func(text string) {
		t.Helper()
		require.Falsef(t, isCentredBoldHeading(centredBoldLine(text, 11.0, 200, 395, true), 11.0, body, size), "should reject: %q", text)
	}
	fp("and Wood Chips")                                    // lower-case sentence fragment
	fp("per Region, 2007-2019")                             // lower-case chart-title continuation
	fp("New Investment (a Challenger).")                    // trailing sentence period
	fp("Source: Department of Employment, Thailand (2022)") // chart source caption (interior colon)
	fp("Dahyun Kim∗, Chanjun Park∗†, Sanghoon Kim∗†")       // author line (footnote markers)

	// Positive — a title carrying the abbreviation "No. 2" must NOT be rejected
	// by the cleanliness gate (a naive ". " sentence-break test would kill it).
	require.True(t, isCleanHeadingText("Treasury Laws Amendment (Tax Reform No. 2) Bill 2026"))
}

func TestIsHeadingLinePromotesCentredBoldAcronym(t *testing.T) {
	t.Parallel()
	size := geom.Size{Width: 595.3, Height: 841.9}
	const body = 10.5

	// "BILLS" is a single all-caps word that looksLikeStandaloneAcronymEntry
	// would reject — but a bold, centred, inset line is a section heading, so the
	// centred-bold path must exempt it and promote it end-to-end.
	bills := centredBoldLine("BILLS", 12.0, 280.2, 314.8, true)
	// The line directly below is a bill title carrying a year ("... Bill 2026"),
	// which trips looksLikeAcronymTableEntry's numeric-neighbour cue — the
	// centred-bold exemption must still promote BILLS.
	next := centredBoldLine("Treasury Laws Amendment (Tax Reform No. 2) Bill 2026", 11.5, 156.7, 438.6, true)
	require.True(t, isHeadingLine(bills, 12.0, body, size, nil, &next))

	// A non-bold standalone all-caps acronym next to a numeric table row (a table
	// entry, not a heading) stays rejected.
	acronym := centredBoldLine("OTSL", 10.5, 280.2, 314.8, false)
	numericRow := centredBoldLine("0.965 0.934 0.955", 10.5, 60, 400, false)
	require.False(t, isHeadingLine(acronym, body, body, size, nil, &numericRow))
}
