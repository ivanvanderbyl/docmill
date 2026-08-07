package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
)

func scored(class string, score float64, l, t, r, b float64) ScoredProposal {
	return ScoredProposal{
		Proposal: RegionProposal{Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}},
		Class:    class,
		Score:    score,
	}
}

func TestSelectRegionsDropsBackground(t *testing.T) {
	// A candidate the model calls Background must not suppress a real region,
	// however confident the model is that it is nothing.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassBackground, 0.99, 50, 100, 500, 400),
		scored(layoutClassTable, 0.60, 50, 100, 500, 400),
	})
	if len(got) != 1 || got[0].Class != layoutClassTable {
		t.Fatalf("got %+v, want the Table alone", got)
	}
}

func TestSelectRegionsKeepsTheBestOfNearMisses(t *testing.T) {
	// This is the case the old gate could never handle. A table proposed one
	// line short and the same table proposed correctly are each plausible in
	// isolation; only comparing them settles it.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassTable, 0.62, 50, 100, 500, 380), // one line short
		scored(layoutClassTable, 0.91, 50, 100, 500, 400), // correct
		scored(layoutClassTable, 0.55, 50, 100, 500, 420), // one line long
	})
	if len(got) != 1 {
		t.Fatalf("got %d regions, want 1 — the three overlap almost exactly", len(got))
	}
	if bottomEdgeOf(got[0].Proposal.Box) != 400 {
		t.Errorf("kept the box ending at %v, want the highest-scoring one at 400",
			bottomEdgeOf(got[0].Proposal.Box))
	}
}

func TestSelectRegionsKeepsAbuttingRegions(t *testing.T) {
	// A caption sits directly under a figure. They touch and must both survive,
	// which is why the IoU threshold is permissive about touching.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassPicture, 0.90, 50, 100, 500, 400),
		scored("Caption", 0.80, 50, 402, 500, 420),
	})
	if len(got) != 2 {
		t.Fatalf("got %d regions, want 2 — a figure and its caption abut but do not overlap", len(got))
	}
}

func TestSelectRegionsSuppressesContainedCandidates(t *testing.T) {
	// One line inside a large accepted table has tiny IoU with it — the areas
	// are wildly different — but it is not a separate region. IoU alone will
	// never say so, which is why containment is checked as well.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassTable, 0.90, 50, 100, 500, 400),
		scored("Text", 0.70, 60, 200, 480, 212),
	})
	if len(got) != 1 || got[0].Class != layoutClassTable {
		t.Fatalf("got %+v, want the table alone", got)
	}
}

func TestSelectRegionsSuppressesAnEnclosingLowScorer(t *testing.T) {
	// The reverse containment: a confident heading inside a sprawling,
	// low-confidence whole-page merge. Letting the merge stand would
	// double-count every line beneath it.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassSectionHeader, 0.95, 50, 100, 300, 120),
		scored("Text", 0.40, 50, 95, 500, 700),
	})
	if len(got) != 1 || got[0].Class != layoutClassSectionHeader {
		t.Fatalf("got %+v, want the heading alone", got)
	}
}

func TestSelectRegionsTiesGoToTheLargerBox(t *testing.T) {
	// Equal confidence means the model cannot tell them apart. The larger box
	// keeps more content inside a region rather than stranding it as loose
	// lines, which is the recoverable direction.
	got := SelectRegions([]ScoredProposal{
		scored(layoutClassTable, 0.70, 50, 100, 500, 300),
		scored(layoutClassTable, 0.70, 50, 100, 500, 400),
	})
	if len(got) != 1 {
		t.Fatalf("got %d regions, want 1", len(got))
	}
	if bottomEdgeOf(got[0].Proposal.Box) != 400 {
		t.Errorf("kept the smaller box on a tie; want the larger one")
	}
}
