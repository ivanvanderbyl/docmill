package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
)

// Non-maximum suppression: turning 375 overlapping candidates into a page
// decomposition.
//
// The previous region model asked "should this candidate stand" about each
// candidate in isolation, and that framing is why it could never fix an extent.
// A table proposed one line too short and the same table proposed correctly are
// both plausible in isolation; only comparing them settles it. With the new
// proposer offering every contiguous merge, the correct extent is reliably
// present — but so are a dozen near-misses around it, and something has to
// choose.
//
// So the region stage classifies, and this picks the highest-confidence set of
// candidates that do not overlap. It is the standard detector trick, and it is
// what makes over-generating proposals a sound strategy rather than a way to
// flood the pipeline.

const (
	// nmsIoUThreshold is how much two kept regions may overlap. Layout regions
	// genuinely abut — a caption sits directly under a figure, table rows touch
	// the header — so this is deliberately permissive about touching and strict
	// about covering.
	nmsIoUThreshold = 0.3

	// nmsContainmentThreshold suppresses a candidate almost entirely inside one
	// already kept, even when their IoU is low. A single line inside a large
	// accepted table has tiny IoU with it but is not a separate region, and IoU
	// alone will never say so because the areas are so different.
	nmsContainmentThreshold = 0.8
)

// ScoredProposal is a candidate with the region model's verdict.
type ScoredProposal struct {
	Proposal RegionProposal
	// Class is the argmax over the model's classes. It may be "Background",
	// which means the candidate is not a region at all.
	Class string
	// Score is that class's probability.
	Score float64
	// Background is the probability the candidate is not a region, kept
	// separately because it is the useful thing to threshold on when a caller
	// wants to trade recall for precision.
	Background float64
	// Overlap is the IoU head's estimate of how well this extent matches the
	// region it is trying to be. Zero when the head is unavailable.
	Overlap float64
	// RealClass and RealScore are the best class EXCLUDING Background, with
	// its probability. When Class is Background these say what the candidate
	// would be if it is anything; when Class is real they equal Class/Score.
	RealClass string
	RealScore float64
}

// Rank is what suppression sorts by.
//
// Class probability alone cannot do this job, and the failure is measurable
// rather than theoretical. A table proposed one line short and the same table
// proposed correctly have nearly identical CONTENT features, so the classifier
// scores them nearly the same and suppression picks between them almost at
// random. Measured that way, recall halved on every structural class:
// Table 0.741 -> 0.281, Page-header 0.696 -> 0.350, Picture 0.582 -> 0.264.
//
// The IoU head answers the question the classifier cannot — how good is this
// EXTENT — and the product ranks a candidate that is both the right kind of
// thing and the right size above one that is merely the right kind of thing.
func (s ScoredProposal) Rank() float64 {
	if s.Overlap <= 0 {
		return s.Score
	}
	return s.Score * s.Overlap
}

// SelectRegions reduces scored candidates to a non-overlapping decomposition of
// the page, highest confidence first.
//
// Candidates the model calls Background are dropped before anything competes:
// a candidate that is not a region should not be able to suppress one that is,
// however confident the model is that it is nothing.
func SelectRegions(scored []ScoredProposal) []ScoredProposal {
	candidates := make([]ScoredProposal, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Class == "" || candidate.Class == layoutClassBackground {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) < 2 {
		return candidates
	}

	sort.SliceStable(candidates, func(a, b int) bool {
		rankA, rankB := candidates[a].Rank(), candidates[b].Rank()
		if rankA != rankB {
			return rankA > rankB
		}
		// Ties go to the larger candidate. A tie means the model cannot
		// distinguish them, and the larger box is the one that keeps more
		// content in a region rather than stranding it as loose lines.
		return boxArea(candidates[a].Proposal.Box) > boxArea(candidates[b].Proposal.Box)
	})

	kept := make([]ScoredProposal, 0, len(candidates))
	for _, candidate := range candidates {
		suppressed := false
		for _, winner := range kept {
			if suppresses(winner.Proposal.Box, candidate.Proposal.Box) {
				suppressed = true
				break
			}
		}
		if !suppressed {
			kept = append(kept, candidate)
		}
	}
	return kept
}

// suppresses reports whether a kept region rules out a later candidate.
func suppresses(winner, candidate geom.Box) bool {
	if boxIoU(winner, candidate) >= nmsIoUThreshold {
		return true
	}
	// Containment is checked BOTH ways. The candidate being swallowed by the
	// winner is the common case; the winner being a small high-confidence
	// region inside a large low-confidence one is rarer but real — a heading
	// scored confidently inside a whole-page text merge — and leaving that
	// merge to stand would double-count every line under it.
	return containedFractionOf(candidate, winner) >= nmsContainmentThreshold ||
		containedFractionOf(winner, candidate) >= nmsContainmentThreshold
}

func boxIoU(a, b geom.Box) float64 {
	width := math.Min(a.R, b.R) - math.Max(a.L, b.L)
	height := math.Min(bottomEdgeOf(a), bottomEdgeOf(b)) - math.Max(topEdgeOf(a), topEdgeOf(b))
	if width <= 0 || height <= 0 {
		return 0
	}
	overlap := width * height
	union := boxArea(a) + boxArea(b) - overlap
	if union <= 0 {
		return 0
	}
	return overlap / union
}

// containedFractionOf is the share of inner that lies inside outer.
func containedFractionOf(inner, outer geom.Box) float64 {
	width := math.Min(inner.R, outer.R) - math.Max(inner.L, outer.L)
	height := math.Min(bottomEdgeOf(inner), bottomEdgeOf(outer)) - math.Max(topEdgeOf(inner), topEdgeOf(outer))
	if width <= 0 || height <= 0 {
		return 0
	}
	area := boxArea(inner)
	if area <= 0 {
		return 0
	}
	return (width * height) / area
}
