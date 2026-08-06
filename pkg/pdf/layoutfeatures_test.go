package pdf

import (
	"math"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
)

// featureIndex looks a feature up by name so the tests below assert on meaning
// rather than on position. A test that hard-coded indexes would keep passing
// through exactly the reordering the contract exists to catch.
func featureIndex(t *testing.T, name string) int {
	t.Helper()
	for i, n := range LayoutFeatureNames {
		if n == name {
			return i
		}
	}
	t.Fatalf("unknown feature %q", name)
	return -1
}

func testLine(l, top, r, bottom float64, text string, cells ...page.TextCell) ParagraphTextLine {
	line := ParagraphTextLine{
		BBox:     geom.Box{L: l, T: top, R: r, B: bottom, Origin: geom.TopLeft},
		Text:     text,
		FontSize: 10,
		Cells:    cells,
	}
	line.Words = []textline.Word{{Value: text, FontSize: 10}}
	return line
}

func testCell(l, top, r, bottom, size float64, text string) page.TextCell {
	return page.TextCell{
		Text:     text,
		FontSize: size,
		Box:      geom.Box{L: l, T: top, R: r, B: bottom, Origin: geom.TopLeft},
	}
}

func testContext(lines []ParagraphTextLine, cells []page.TextCell) PageLayoutContext {
	size := geom.Size{Width: 600, Height: 800}
	return NewPageLayoutContext(size, cells, lines, nil, 0, 1)
}

// TestLayoutFeatureVectorMatchesContract is the guard that matters most: the
// vector and its names must stay the same length, because the Python trainer
// and the Go predictor both index by position.
func TestLayoutFeatureVectorMatchesContract(t *testing.T) {
	line := testLine(100, 100, 300, 112, "hello world")
	got := LineLayoutFeatures(line, nil, nil, testContext([]ParagraphTextLine{line}, nil))
	if len(got) != len(LayoutFeatureNames) {
		t.Fatalf("vector has %d values, contract names %d", len(got), len(LayoutFeatureNames))
	}
	for i, v := range got {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("feature %d (%s) is not finite: %v", i, LayoutFeatureNames[i], v)
		}
	}
	seen := map[string]bool{}
	for _, name := range LayoutFeatureNames {
		if seen[name] {
			t.Errorf("duplicate feature name %q", name)
		}
		seen[name] = true
	}
}

// TestLayoutFeaturesDegradeGracefully covers the inputs that occur in real
// corpora and would otherwise divide by zero: an empty line, a zero-area box,
// and a page with no measurable font size.
func TestLayoutFeaturesDegradeGracefully(t *testing.T) {
	for name, line := range map[string]ParagraphTextLine{
		"empty text":  testLine(0, 0, 100, 10, ""),
		"zero width":  testLine(50, 100, 50, 110, "x"),
		"zero height": testLine(50, 100, 200, 100, "x"),
		"degenerate":  testLine(0, 0, 0, 0, ""),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := PageLayoutContext{Size: geom.Size{}, PageCount: 0}
			got := LineLayoutFeatures(line, nil, nil, ctx)
			for i, v := range got {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("feature %d (%s) not finite: %v", i, LayoutFeatureNames[i], v)
				}
			}
		})
	}
}

func TestNumberingDepthAndListMarker(t *testing.T) {
	depth := featureIndex(t, "numbering_depth")
	marker := featureIndex(t, "has_list_marker")

	for _, tc := range []struct {
		text      string
		wantDepth float64
		wantMark  float64
	}{
		{"4.3.1 Deep subsection", 3, 1},
		{"4.3 Subsection", 2, 1},
		{"7. Section", 1, 1},
		{"• bullet item", 0, 1},
		{"- dashed item", 0, 1},
		{"Ordinary prose line", 0, 0},
		{"", 0, 0},
		// A leading token that merely contains digits is not a marker: this is
		// the case that separates numbering from an equation opening.
		{"3x + 2y = 7", 0, 0},
	} {
		line := testLine(100, 100, 300, 112, tc.text)
		got := LineLayoutFeatures(line, nil, nil, testContext([]ParagraphTextLine{line}, nil))
		if got[depth] != tc.wantDepth {
			t.Errorf("%q: numbering_depth = %v, want %v", tc.text, got[depth], tc.wantDepth)
		}
		if got[marker] != tc.wantMark {
			t.Errorf("%q: has_list_marker = %v, want %v", tc.text, got[marker], tc.wantMark)
		}
	}
}

func TestCaptionMarkerShapeIsLanguageAgnostic(t *testing.T) {
	idx := featureIndex(t, "caption_marker")
	for _, tc := range []struct {
		text string
		want float64
	}{
		{"Figure 3 shows the result", 1},
		{"Table 2: results", 1},
		{"Tabelle 2 zeigt", 1}, // German — no keyword list involved
		{"Abbildung 4.1", 1},
		{"The figure shows results", 0},
		{"3 Figure", 0},
		{"Figure", 0},
	} {
		line := testLine(100, 100, 300, 112, tc.text)
		got := LineLayoutFeatures(line, nil, nil, testContext([]ParagraphTextLine{line}, nil))
		if got[idx] != tc.want {
			t.Errorf("%q: caption_marker = %v, want %v", tc.text, got[idx], tc.want)
		}
	}
}

// TestBaselineDispersionSeparatesEquationFromProse is the signal Task 0 proved
// carries the Formula class: a prose line sits on one baseline, a display
// equation with sub/superscripts scatters across several.
func TestBaselineDispersionSeparatesEquationFromProse(t *testing.T) {
	dispersion := featureIndex(t, "baseline_dispersion")
	baselines := featureIndex(t, "baseline_count")

	prose := testLine(100, 100, 400, 112, "an ordinary sentence of prose",
		testCell(100, 100, 400, 112, 10, "an ordinary sentence of prose"))
	equation := testLine(200, 100, 320, 118, "x i = 2",
		testCell(200, 100, 220, 112, 10, "x"),
		testCell(220, 106, 230, 118, 7, "i"),
		testCell(240, 100, 320, 112, 10, "= 2"),
	)
	ctx := testContext([]ParagraphTextLine{prose, equation}, nil)

	proseFeatures := LineLayoutFeatures(prose, nil, nil, ctx)
	equationFeatures := LineLayoutFeatures(equation, nil, nil, ctx)

	if proseFeatures[baselines] != 1 {
		t.Errorf("prose baseline_count = %v, want 1", proseFeatures[baselines])
	}
	if equationFeatures[baselines] < 2 {
		t.Errorf("equation baseline_count = %v, want >= 2", equationFeatures[baselines])
	}
	if equationFeatures[dispersion] <= proseFeatures[dispersion] {
		t.Errorf("equation dispersion %v should exceed prose %v", equationFeatures[dispersion], proseFeatures[dispersion])
	}
}

func TestGapFeaturesUseLineHeightUnits(t *testing.T) {
	above := featureIndex(t, "gap_above_ratio")
	below := featureIndex(t, "gap_below_ratio")

	first := testLine(100, 100, 400, 110, "first")
	// 10pt gap below a 10pt line is exactly one line height.
	second := testLine(100, 120, 400, 130, "second")
	ctx := testContext([]ParagraphTextLine{first, second}, nil)

	got := LineLayoutFeatures(second, &first, nil, ctx)
	if math.Abs(got[above]-1) > 1e-9 {
		t.Errorf("gap_above_ratio = %v, want 1", got[above])
	}
	// A missing neighbour reports the sentinel, not zero: "nothing below me" is
	// not "touching what is below me".
	if got[below] != 10 {
		t.Errorf("gap_below_ratio with no neighbour = %v, want sentinel 10", got[below])
	}
}

// TestRepeatFractionFindsRunningHeaders is the cross-page signal: a header in
// the same slot on every page scores 1, a one-off heading scores near zero.
func TestRepeatFractionFindsRunningHeaders(t *testing.T) {
	idx := featureIndex(t, "repeat_frac")
	size := geom.Size{Width: 600, Height: 800}

	header := testLine(100, 40, 300, 52, "Running Header")
	var pages [][]ParagraphTextLine
	var sizes []geom.Size
	for i := 0; i < 10; i++ {
		body := testLine(100, float64(200+i*7), 400, float64(212+i*7), "body text")
		pages = append(pages, []ParagraphTextLine{header, body})
		sizes = append(sizes, size)
	}
	document := NewDocumentLayoutContext(pages, sizes)

	ctx := NewPageLayoutContext(size, nil, pages[0], nil, 0, 10)
	ctx.Repeat = document

	headerFeatures := LineLayoutFeatures(header, nil, nil, ctx)
	if headerFeatures[idx] < 0.9 {
		t.Errorf("running header repeat_frac = %v, want >= 0.9", headerFeatures[idx])
	}
	oneOff := testLine(100, 400, 400, 412, "A Section Heading")
	oneOffFeatures := LineLayoutFeatures(oneOff, nil, nil, ctx)
	if oneOffFeatures[idx] > 0.2 {
		t.Errorf("one-off line repeat_frac = %v, want <= 0.2", oneOffFeatures[idx])
	}
}

func TestStrokeDensityRespondsToRulings(t *testing.T) {
	idx := featureIndex(t, "stroke_density")
	line := testLine(100, 100, 400, 112, "cell a   cell b")
	size := geom.Size{Width: 600, Height: 800}

	bare := NewPageLayoutContext(size, nil, []ParagraphTextLine{line}, nil, 0, 1)
	ruled := NewPageLayoutContext(size, nil, []ParagraphTextLine{line}, []page.RulingSegment{
		{FromX: 100, FromY: 114, ToX: 400, ToY: 114, Origin: geom.TopLeft},
	}, 0, 1)

	if got := LineLayoutFeatures(line, nil, nil, bare)[idx]; got != 0 {
		t.Errorf("stroke_density with no rulings = %v, want 0", got)
	}
	if got := LineLayoutFeatures(line, nil, nil, ruled)[idx]; got <= 0 {
		t.Errorf("stroke_density under a rule = %v, want > 0", got)
	}
}

func TestFeaturesAreScaleInvariant(t *testing.T) {
	// The same layout on Letter and on A4 must produce the same normalised
	// vector for the position and size features; that is the whole reason they
	// are expressed as page fractions.
	build := func(w, h float64) []float64 {
		line := testLine(0.2*w, 0.3*h, 0.6*w, 0.3*h+0.02*h, "hello")
		line.FontSize = 0.012 * h
		cells := []page.TextCell{testCell(0.2*w, 0.3*h, 0.6*w, 0.3*h+0.02*h, 0.012*h, "hello")}
		line.Cells = cells
		ctx := NewPageLayoutContext(geom.Size{Width: w, Height: h}, cells, []ParagraphTextLine{line}, nil, 0, 1)
		return LineLayoutFeatures(line, nil, nil, ctx)
	}
	letter := build(612, 792)
	a4 := build(595, 842)

	for _, name := range []string{"width_frac", "left_frac", "y_center_frac", "center_offset_frac", "font_size_ratio"} {
		i := featureIndex(t, name)
		if math.Abs(letter[i]-a4[i]) > 1e-9 {
			t.Errorf("%s differs across page sizes: letter=%v a4=%v", name, letter[i], a4[i])
		}
	}
}
