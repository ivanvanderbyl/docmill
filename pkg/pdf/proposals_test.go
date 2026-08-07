package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

func proposalLine(l, t, r, b float64) ParagraphTextLine {
	return ParagraphTextLine{
		BBox:     geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
		FontSize: 10,
		Cells: []page.TextCell{
			{Text: "x", Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}},
		},
	}
}

func drawn(kind page.DrawnKind, l, t, r, b float64) page.DrawnObject {
	return page.DrawnObject{
		Kind: kind,
		Box:  geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}

var testPageSize = geom.Size{Width: 612, Height: 792}

func TestAtomicGroupsSplitOnVerticalGap(t *testing.T) {
	// Three tight lines, a wide gap, then two more. Geometry alone must find
	// the boundary — no label is consulted anywhere in this path.
	lines := []ParagraphTextLine{
		proposalLine(50, 100, 300, 110),
		proposalLine(50, 112, 300, 122),
		proposalLine(50, 124, 300, 134),
		proposalLine(50, 200, 300, 210),
		proposalLine(50, 212, 300, 222),
	}
	groups := atomicLineGroups(lines, fineGapRatio)
	if len(groups) != 2 {
		t.Fatalf("got %d atomic groups, want 2", len(groups))
	}
	if len(groups[0].lines) != 3 || len(groups[1].lines) != 2 {
		t.Errorf("group sizes = %d,%d, want 3,2", len(groups[0].lines), len(groups[1].lines))
	}
}

func TestAtomicGroupsSplitOnDisjointColumns(t *testing.T) {
	// Two columns at the same vertical position. Without the horizontal test
	// they would join into one group spanning the whole page.
	lines := []ParagraphTextLine{
		proposalLine(50, 100, 250, 110),
		proposalLine(350, 112, 550, 122),
	}
	if groups := atomicLineGroups(lines, fineGapRatio); len(groups) != 2 {
		t.Fatalf("got %d atomic groups, want 2 for disjoint columns", len(groups))
	}
}

func TestProposalsOfferEveryContiguousMerge(t *testing.T) {
	// Three atomic groups must yield all six contiguous runs: {1},{2},{3},
	// {1,2},{2,3},{1,2,3}. This is the property that makes the proposer robust
	// to a wrong line label — the correct extent only has to be among them.
	lines := []ParagraphTextLine{
		proposalLine(50, 100, 300, 110),
		proposalLine(50, 200, 300, 210),
		proposalLine(50, 300, 300, 310),
	}
	proposals := ProposeRegions(lines, nil, testPageSize)

	spans := map[int]int{}
	for _, proposal := range proposals {
		if proposal.Source != ProposalText {
			t.Fatalf("unexpected source %q with no ink supplied", proposal.Source)
		}
		spans[proposal.AtomicSpan]++
	}
	if spans[1] != 3 || spans[2] != 2 || spans[3] != 1 {
		t.Errorf("span counts = %v, want 3 of span 1, 2 of span 2, 1 of span 3", spans)
	}
}

func TestProposalLinesAreNotAliased(t *testing.T) {
	// Every proposal in a merge chain is built by appending to one slice. If
	// the slice is shared, each proposal ends up describing the longest run.
	lines := []ParagraphTextLine{
		proposalLine(50, 100, 300, 110),
		proposalLine(50, 200, 300, 210),
		proposalLine(50, 300, 300, 310),
	}
	proposals := ProposeRegions(lines, nil, testPageSize)
	for _, proposal := range proposals {
		if len(proposal.Lines) != proposal.AtomicSpan {
			t.Fatalf("span %d proposal carries %d lines; the slice is aliased",
				proposal.AtomicSpan, len(proposal.Lines))
		}
	}
}

func TestInkClusterFromASingleImage(t *testing.T) {
	// The 58.3% case: a lone image IS the region, and needs no clustering.
	objects := []page.DrawnObject{drawn(page.DrawnImage, 100, 100, 400, 400)}
	clusters := GroupInkClusters(objects, testPageSize)
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(clusters))
	}
	if !clusters[0].Single || clusters[0].Images != 1 {
		t.Errorf("cluster = %+v, want Single with one image", clusters[0])
	}
}

func TestInkClusterMergesScatteredPaths(t *testing.T) {
	// A chart: many small paths within the merge gap of each other. They must
	// become ONE candidate covering the chart, not 200 candidates.
	var objects []page.DrawnObject
	for i := range 200 {
		x := 100 + float64(i%20)*5
		y := 100 + float64(i/20)*5
		objects = append(objects, drawn(page.DrawnPath, x, y, x+4, y+4))
	}
	clusters := GroupInkClusters(objects, testPageSize)

	var merged *InkCluster
	for i := range clusters {
		if clusters[i].Ink > 100 {
			merged = &clusters[i]
		}
	}
	if merged == nil {
		t.Fatalf("no cluster merged the 200 paths; got %d clusters", len(clusters))
	}
	if merged.Box.L > 100 || merged.Box.R < 199 {
		t.Errorf("merged box = %+v, does not span the paths", merged.Box)
	}
}

func TestInkClusterIgnoresTextOnlyGroups(t *testing.T) {
	// Text takes part in clustering but cannot form a candidate alone: a run of
	// prose is the line proposer's job, and proposing it here would duplicate
	// every paragraph on the page.
	objects := []page.DrawnObject{
		drawn(page.DrawnText, 100, 100, 400, 110),
		drawn(page.DrawnText, 100, 112, 400, 122),
	}
	if clusters := GroupInkClusters(objects, testPageSize); len(clusters) != 0 {
		t.Fatalf("got %d clusters from text alone, want 0", len(clusters))
	}
}

func TestInkClusterIgnoresFormBoxes(t *testing.T) {
	// A form's box is the union of its children, which are reported too.
	// Clustering both would merge everything the form holds into one blob.
	objects := []page.DrawnObject{
		drawn(page.DrawnForm, 50, 50, 550, 700),
		drawn(page.DrawnImage, 60, 60, 200, 200),
		drawn(page.DrawnImage, 400, 600, 540, 690),
	}
	clusters := GroupInkClusters(objects, testPageSize)
	for _, cluster := range clusters {
		if cluster.Box.Width() > 400 {
			t.Fatalf("cluster %+v spans the form box; the two images should stay apart", cluster.Box)
		}
	}
	if len(clusters) != 2 {
		t.Fatalf("got %d clusters, want 2 (one per image)", len(clusters))
	}
}

func TestInkProposalExtendsToTouchedLines(t *testing.T) {
	// A chart with an axis label overlapping its box yields both the tight ink
	// box and the extended one, so the model can choose.
	objects := []page.DrawnObject{drawn(page.DrawnImage, 100, 100, 300, 300)}
	lines := []ParagraphTextLine{proposalLine(90, 290, 320, 305)}

	proposals := ProposeRegions(lines, GroupInkClusters(objects, testPageSize), testPageSize)
	var sawInk, sawExtended bool
	for _, proposal := range proposals {
		switch proposal.Source {
		case ProposalInk:
			sawInk = true
		case ProposalInkText:
			sawExtended = true
			if proposal.Box.R < 320 {
				t.Errorf("extended box %+v does not reach the label at x=320", proposal.Box)
			}
		}
	}
	if !sawInk || !sawExtended {
		t.Errorf("ink=%v extended=%v, want both", sawInk, sawExtended)
	}
}

func TestColumnSplitNeedsPersistence(t *testing.T) {
	// A moderate gap — wide enough to consider, well below decisive — with no
	// corroboration from its neighbours is a sentence break or display maths,
	// not a column. It must NOT split.
	//
	// Font size 10 makes the pre-filter 15pt and the decisive threshold 60pt,
	// so the 30pt gap here sits squarely between them, where persistence is
	// what decides.
	lines := []ParagraphTextLine{
		{
			BBox:     geom.Box{L: 50, T: 100, R: 500, B: 110, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "a", Box: geom.Box{L: 50, T: 100, R: 200, B: 110, Origin: geom.TopLeft}},
				{Text: "b", Box: geom.Box{L: 230, T: 100, R: 500, B: 110, Origin: geom.TopLeft}},
			},
		},
		{
			BBox:     geom.Box{L: 50, T: 112, R: 500, B: 122, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "c", Box: geom.Box{L: 50, T: 112, R: 500, B: 122, Origin: geom.TopLeft}},
			},
		},
	}
	if got := SplitLinesAtColumnGaps(lines, testPageSize); len(got) != 2 {
		t.Fatalf("got %d lines, want 2 — the gap does not persist so nothing should split", len(got))
	}
}

func TestColumnSplitOnDecisiveGap(t *testing.T) {
	// A running header, `Chapter 3 ... Page 45`. Persistence cannot help here:
	// the line below is body text spanning the very corridor being tested, so
	// corroboration always fails. The gap alone has to carry it.
	//
	// This rule is worth 16.6 points of Page-header recall and 13.4 of Caption.
	lines := []ParagraphTextLine{
		{
			BBox:     geom.Box{L: 50, T: 40, R: 500, B: 50, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "Chapter 3", Box: geom.Box{L: 50, T: 40, R: 120, B: 50, Origin: geom.TopLeft}},
				{Text: "Page 45", Box: geom.Box{L: 430, T: 40, R: 500, B: 50, Origin: geom.TopLeft}},
			},
		},
		{
			BBox:     geom.Box{L: 50, T: 100, R: 500, B: 110, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "body", Box: geom.Box{L: 50, T: 100, R: 500, B: 110, Origin: geom.TopLeft}},
			},
		},
	}
	got := SplitLinesAtColumnGaps(lines, testPageSize)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3 — the header should split into two", len(got))
	}
	if got[0].BBox.R > 200 || got[1].BBox.L < 400 {
		t.Errorf("header pieces = %+v and %+v, want them either side of the gap", got[0].BBox, got[1].BBox)
	}
}

func TestProposalsCoverBothLineSets(t *testing.T) {
	// A table row is column-separated by construction, so the splitter cuts it
	// into cells and the full-width run a table needs disappears. Proposing
	// from the UNSPLIT set as well is what keeps it: measured, that is worth
	// 5.6 points of Table recall.
	var lines []ParagraphTextLine
	for i := range 4 {
		top := 100 + float64(i)*12
		lines = append(lines, ParagraphTextLine{
			BBox:     geom.Box{L: 50, T: top, R: 500, B: top + 10, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "l", Box: geom.Box{L: 50, T: top, R: 150, B: top + 10, Origin: geom.TopLeft}},
				{Text: "r", Box: geom.Box{L: 400, T: top, R: 500, B: top + 10, Origin: geom.TopLeft}},
			},
		})
	}
	proposals := ProposeRegions(lines, nil, testPageSize)

	var fullWidth bool
	for _, proposal := range proposals {
		if proposal.Box.L <= 50 && proposal.Box.R >= 500 &&
			topEdgeOf(proposal.Box) <= 100 && bottomEdgeOf(proposal.Box) >= 146 {
			fullWidth = true
		}
	}
	if !fullWidth {
		t.Fatal("no proposal spans the whole table; the unsplit line set is not being proposed from")
	}
}

func TestColumnSplitOnPersistentGutter(t *testing.T) {
	// Two side-by-side captions across three rows: the corridor is clear on
	// every line, so every line splits. This is the measured defect —
	// DocLayNet marks two caption regions and docmill assembled one line.
	var lines []ParagraphTextLine
	for i := range 3 {
		top := 100 + float64(i)*12
		lines = append(lines, ParagraphTextLine{
			BBox:     geom.Box{L: 50, T: top, R: 500, B: top + 10, Origin: geom.TopLeft},
			FontSize: 10,
			Cells: []page.TextCell{
				{Text: "l", Box: geom.Box{L: 50, T: top, R: 200, B: top + 10, Origin: geom.TopLeft}},
				{Text: "r", Box: geom.Box{L: 350, T: top, R: 500, B: top + 10, Origin: geom.TopLeft}},
			},
		})
	}
	got := SplitLinesAtColumnGaps(lines, testPageSize)
	if len(got) != 6 {
		t.Fatalf("got %d lines, want 6 (three rows x two columns)", len(got))
	}
	for _, line := range got {
		if line.BBox.Width() > 200 {
			t.Errorf("line %+v still spans the gutter", line.BBox)
		}
	}
	// Reading order must be renumbered, or every downstream stage that trusts
	// it starts seeing duplicates.
	for i, line := range got {
		if line.ReadingOrder != i {
			t.Errorf("line %d has ReadingOrder %d", i, line.ReadingOrder)
		}
	}
}

func TestColumnSplitKeepsSingleColumnProseIntact(t *testing.T) {
	// Ordinary body text: no gap wide enough to consider, so the line set must
	// come back completely unchanged.
	var lines []ParagraphTextLine
	for i := range 5 {
		top := 100 + float64(i)*12
		lines = append(lines, proposalLine(50, top, 500, top+10))
	}
	if got := SplitLinesAtColumnGaps(lines, testPageSize); len(got) != 5 {
		t.Fatalf("got %d lines, want 5 unchanged", len(got))
	}
}
