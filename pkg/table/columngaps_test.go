package table

import (
	"math"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

func gapCell(text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{Text: text, Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}}
}

func gapFeature(t *testing.T, name string) int {
	t.Helper()
	for i, n := range ColumnGapFeatureNames {
		if n == name {
			return i
		}
	}
	t.Fatalf("unknown feature %q", name)
	return -1
}

// A three-column grid: the two inter-column gaps must be found, and they must
// be clear in every row — that persistence is the signal separating a column
// boundary from a chance space inside a sentence.
func threeColumnTable() ([]page.TextCell, geom.Box) {
	var cells []page.TextCell
	columns := []float64{50, 200, 350}
	for row := 0; row < 5; row++ {
		top := 100 + float64(row)*20
		for _, x := range columns {
			cells = append(cells, gapCell("value", x, top, x+60, top+12))
		}
	}
	return cells, geom.Box{L: 40, T: 90, R: 430, B: 210, Origin: geom.TopLeft}
}

func TestColumnGapCandidatesFindsGridBoundaries(t *testing.T) {
	cells, box := threeColumnTable()
	gaps := ColumnGapCandidates(cells, nil, box)
	if len(gaps) != 2 {
		t.Fatalf("expected 2 gaps between 3 columns, got %d", len(gaps))
	}
	persistence := gapFeature(t, "persistence")
	crossing := gapFeature(t, "crossing_rows_frac")
	for i, gap := range gaps {
		if gap.Features[persistence] != 1 {
			t.Errorf("gap %d persistence = %v, want 1 (clear in every row)", i, gap.Features[persistence])
		}
		if gap.Features[crossing] != 0 {
			t.Errorf("gap %d crossing_rows_frac = %v, want 0", i, gap.Features[crossing])
		}
	}
	if c := gaps[0].Center(); math.Abs(c-155) > 5 {
		t.Errorf("first gap centre = %v, want ~155", c)
	}
}

// A cell spanning the gap in one row must lower persistence. This is the merged
// header case that the hand-tuned gutter rule handles by exception.
func TestColumnGapPersistenceFallsWhenARowSpansIt(t *testing.T) {
	cells, box := threeColumnTable()
	cells = append(cells, gapCell("merged header across two columns", 50, 80, 310, 92))
	box.T = 70

	gaps := ColumnGapCandidates(cells, nil, box)
	if len(gaps) == 0 {
		t.Fatal("expected at least one gap")
	}
	persistence := gapFeature(t, "persistence")
	for _, gap := range gaps {
		if gap.Center() < 300 && gap.Features[persistence] >= 1 {
			t.Errorf("gap at %v still reports full persistence despite a spanning row", gap.Center())
		}
	}
}

func TestColumnGapFeatureVectorMatchesContract(t *testing.T) {
	cells, box := threeColumnTable()
	for _, gap := range ColumnGapCandidates(cells, nil, box) {
		if len(gap.Features) != len(ColumnGapFeatureNames) {
			t.Fatalf("vector has %d values, contract names %d", len(gap.Features), len(ColumnGapFeatureNames))
		}
		for i, v := range gap.Features {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("feature %d (%s) not finite: %v", i, ColumnGapFeatureNames[i], v)
			}
		}
	}
}

func TestColumnGapCandidatesHandlesDegenerateInput(t *testing.T) {
	box := geom.Box{L: 0, T: 0, R: 100, B: 100, Origin: geom.TopLeft}
	for name, cells := range map[string][]page.TextCell{
		"nil":        nil,
		"one cell":   {gapCell("solo", 10, 10, 40, 22)},
		"all blank":  {gapCell("   ", 10, 10, 40, 22), gapCell("", 60, 10, 90, 22)},
		"outside":    {gapCell("far", 500, 500, 560, 512), gapCell("away", 600, 500, 660, 512)},
		"zero width": {gapCell("a", 10, 10, 10, 22), gapCell("b", 60, 10, 60, 22)},
	} {
		t.Run(name, func(t *testing.T) {
			for _, gap := range ColumnGapCandidates(cells, nil, box) {
				for i, v := range gap.Features {
					if math.IsNaN(v) || math.IsInf(v, 0) {
						t.Errorf("feature %d (%s) not finite: %v", i, ColumnGapFeatureNames[i], v)
					}
				}
			}
		})
	}
}

// Vertical rules inside a gap are evidence for a boundary; horizontal ones are
// not, and must not be counted.
func TestColumnGapRulingCoverageIgnoresHorizontalRules(t *testing.T) {
	cells, box := threeColumnTable()
	coverage := gapFeature(t, "ruling_coverage")

	horizontal := ColumnGapCandidates(cells, []page.RulingSegment{
		{FromX: 40, FromY: 150, ToX: 430, ToY: 150, Origin: geom.TopLeft},
	}, box)
	for _, gap := range horizontal {
		if gap.Features[coverage] != 0 {
			t.Errorf("horizontal rule counted as column evidence: %v", gap.Features[coverage])
		}
	}

	vertical := ColumnGapCandidates(cells, []page.RulingSegment{
		{FromX: 155, FromY: 95, ToX: 155, ToY: 205, Origin: geom.TopLeft},
	}, box)
	found := false
	for _, gap := range vertical {
		if gap.Features[coverage] > 0 {
			found = true
		}
	}
	if !found {
		t.Error("vertical rule inside a gap produced no ruling coverage")
	}
}
