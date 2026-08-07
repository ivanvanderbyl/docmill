package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
)

// The region proposer.
//
// `GroupLineRegions` emitted one candidate per MAXIMAL run of lines sharing a
// predicted label. Measured on DocLayNet val that reaches 42.8% of real tables,
// against 78.4% reachable from contiguous line runs — 35.6 points lost by the
// proposer alone, before any model sees anything.
//
// The cause is the same-label requirement. Re-running the ceiling with the
// TEACHER's labels instead of the model's gave 85.8%, identical to the oracle
// to the decimal: when labels are right, a region's lines do share one. So every
// point of the gap is line-label noise. One line called Text inside a table cuts
// the candidate in two, and because only maximal runs were emitted, neither
// piece nor their union was ever offered.
//
// The answer is not better labels. It is to stop depending on them:
//
//  1. Split lines into ATOMIC groups on geometry alone — vertical gaps and
//     horizontal disjointness. No label is consulted, so no label can be wrong.
//  2. Propose every contiguous MERGE of those atomic groups. The correct extent
//     only has to be among the proposals; the region model picks it out.
//  3. Add ink clusters, which see what text cannot.
//
// Merging adjacent candidates alone recovers 24.3% of the tables the old
// proposer missed, which is what step 2 is for.
//
// Enumerating merges is quadratic in the number of atomic groups, and that is
// affordable precisely because the split is atomic-group-level rather than
// line-level: a page holds ~5-15 atomic groups but ~50-100 lines, so this is
// ~100 candidates per page rather than ~4,000.

// The split is done at TWO granularities, and the reason is measured rather
// than assumed. A single fine split (0.25) lifts List-item from 50.3% to 66.8%
// and Section-header from 64.5% to 74.0%, because DocLayNet annotates each list
// item and each heading separately and a coarse split swallows a whole list into
// one group. But the same change costs Table 4.5 points: a table cut into thirty
// fine groups needs thirty merges to rebuild, and bounding the merge span is
// what keeps the enumeration affordable.
//
// So neither granularity is right on its own, and picking a compromise gives up
// on both ends. Running both levels and letting the model choose costs one more
// pass and recovers each.
const (
	// fineGapRatio separates list items and consecutive paragraphs: units an
	// annotator marks separately even though they sit close together.
	fineGapRatio = 0.25
	// coarseGapRatio separates blocks — a table from the prose beneath it.
	coarseGapRatio = 0.6

	// Merge spans, per level. Fine groups are numerous, so their span is
	// tighter; coarse groups are few and are what large regions are built from.
	maxFineSpan   = 8
	maxCoarseSpan = 14
)

// RegionProposal is one candidate region offered to the region model.
type RegionProposal struct {
	Box   geom.Box
	Lines []int // indices into the page's line slice; nil for a pure ink proposal
	// Source records where the candidate came from, so the region model can
	// learn that ink candidates and text candidates fail differently.
	Source ProposalSource
	// Ink is the ink cluster behind an ink proposal, zero-valued otherwise.
	Ink InkCluster
	// AtomicSpan is how many atomic groups a text proposal merges. 1 is a
	// paragraph-sized unit; larger values are merges.
	AtomicSpan int
}

// ProposalSource is where a candidate came from.
type ProposalSource string

const (
	// ProposalText is a contiguous run of assembled lines.
	ProposalText ProposalSource = "text"
	// ProposalInk is a cluster of drawn objects.
	ProposalInk ProposalSource = "ink"
	// ProposalInkText is an ink cluster extended to the lines it overlaps —
	// a chart plus its axis labels, read as one region.
	ProposalInkText ProposalSource = "ink+text"
)

// ProposeRegions builds the candidate set for a page.
//
// lines must be in reading order. clusters may be nil, in which case only text
// candidates are produced and the picture classes stay unreachable — that is
// the pre-ink behaviour, and it is a supported configuration rather than a
// degraded one, because a page with no drawn objects genuinely has no ink.
func ProposeRegions(lines []ParagraphTextLine, clusters []InkCluster, size geom.Size) []RegionProposal {
	proposals := make([]RegionProposal, 0, 128)

	// Two line sets, for the same reason there are two granularities.
	//
	// Splitting lines at column gaps lifts almost everything — Page-header
	// +16.6, Caption +13.4, Section-header +11.5, Formula +13.3 — because those
	// regions are annotated per column and the assembler had joined them. It
	// costs Table 5.6 points, because a table row IS a set of column-separated
	// cells and splitting it destroys the full-width runs a table is built from.
	//
	// Neither line set is right for every class, so both are proposed from. The
	// split set supplies fine and coarse merges; the unsplit set supplies coarse
	// merges only, which is what large regions like tables need from it.
	split := SplitLinesAtColumnGaps(lines, size)
	proposals = appendMergeProposals(proposals, lines, atomicLineGroups(split, fineGapRatio), maxFineSpan, split)
	proposals = appendMergeProposals(proposals, lines, atomicLineGroups(split, coarseGapRatio), maxCoarseSpan, split)
	proposals = appendMergeProposals(proposals, lines, atomicLineGroups(lines, coarseGapRatio), maxCoarseSpan, nil)

	for _, cluster := range clusters {
		proposals = append(proposals, RegionProposal{
			Box:    cluster.Box,
			Lines:  linesInsideBox(lines, cluster.Box),
			Source: ProposalInk,
			Ink:    cluster,
		})

		// A chart's axis labels are assembled as ordinary lines and sit just
		// outside the ink. Offering the ink box extended to the lines it
		// touches gives the model the whole figure as one candidate, without
		// giving up the tighter ink-only box.
		if extended, ok := extendBoxToTouchedLines(cluster.Box, lines); ok {
			proposals = append(proposals, RegionProposal{
				Box:    extended,
				Lines:  linesInsideBox(lines, extended),
				Source: ProposalInkText,
				Ink:    cluster,
			})
		}
	}

	return dedupeProposals(proposals)
}

// appendMergeProposals emits every contiguous merge of up to maxSpan groups.
//
// This is the step that makes the proposer robust to a wrong line label: the
// correct extent only has to be somewhere in the set, and the region model picks
// it out. The old proposer offered one candidate per maximal same-label run, so
// a single misread line destroyed the only candidate there was.
//
// groups index into groupLines; when that is a SPLIT line set, the resulting
// Lines are resolved back against the primary set instead. Every proposal must
// index one line space or a consumer cannot use them together, and the primary
// set is the one the rest of the pipeline holds.
func appendMergeProposals(proposals []RegionProposal, lines []ParagraphTextLine, groups []atomicGroup, maxSpan int, groupLines []ParagraphTextLine) []RegionProposal {
	for start := range groups {
		box := geom.Box{}
		members := []int(nil)
		for span := 0; span < maxSpan && start+span < len(groups); span++ {
			group := groups[start+span]
			if span == 0 {
				box = group.box
			} else {
				box = unionBoxes(box, group.box)
			}
			members = append(members, group.lines...)

			var lineIndices []int
			if groupLines == nil {
				// members is appended to on every iteration, so each proposal
				// needs its own copy; sharing the backing array would leave
				// every earlier proposal pointing at the final, longest run.
				lineIndices = make([]int, len(members))
				copy(lineIndices, members)
			} else {
				lineIndices = linesInsideBox(lines, box)
			}
			proposals = append(proposals, RegionProposal{
				Box:        box,
				Lines:      lineIndices,
				Source:     ProposalText,
				AtomicSpan: span + 1,
			})
		}
	}
	return proposals
}

// atomicGroup is an indivisible run of lines: the smallest unit the proposer
// will ever offer, and the unit merges are built from.
type atomicGroup struct {
	box   geom.Box
	lines []int
}

// atomicLineGroups splits lines on geometry alone.
//
// Two rules, both label-free. A vertical gap larger than the local line height
// starts a new group, which is what separates a paragraph from the next
// heading. And a line that shares no horizontal extent with the run so far
// starts a new group, which is what stops a left column and a right column
// being read as one run when reading order interleaves them.
func atomicLineGroups(lines []ParagraphTextLine, gapRatio float64) []atomicGroup {
	if len(lines) == 0 {
		return nil
	}

	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return lines[order[a]].BBox.CenterY() < lines[order[b]].BBox.CenterY()
	})

	var groups []atomicGroup
	var current atomicGroup
	flush := func() {
		if len(current.lines) > 0 {
			groups = append(groups, current)
		}
		current = atomicGroup{}
	}

	for _, index := range order {
		line := lines[index]
		if len(current.lines) == 0 {
			current = atomicGroup{box: line.BBox, lines: []int{index}}
			continue
		}
		if continuesAtomicGroup(current.box, line.BBox, gapRatio) {
			current.box = unionBoxes(current.box, line.BBox)
			current.lines = append(current.lines, index)
			continue
		}
		flush()
		current = atomicGroup{box: line.BBox, lines: []int{index}}
	}
	flush()
	return groups
}

func continuesAtomicGroup(group, line geom.Box, gapRatio float64) bool {
	if horizontalOverlap(group, line) <= 0 {
		return false
	}
	gap := topEdgeOf(line) - bottomEdgeOf(group)
	if gap <= 0 {
		return true
	}
	height := math.Max(line.Height(), 1)
	return gap <= height*gapRatio
}

func horizontalOverlap(a, b geom.Box) float64 {
	return math.Min(a.R, b.R) - math.Max(a.L, b.L)
}

// linesInsideBox returns the lines at least half contained in box, which is how
// an ink candidate acquires the text it encloses.
func linesInsideBox(lines []ParagraphTextLine, box geom.Box) []int {
	var out []int
	for i, line := range lines {
		if containedFraction(line.BBox, box) >= 0.5 {
			out = append(out, i)
		}
	}
	return out
}

// extendBoxToTouchedLines grows box to cover any line it already overlaps,
// reporting whether that changed anything. It does not pull in lines merely
// NEAR the box: a caption sits below a figure without overlapping it, and
// swallowing captions into figures is the defect this project set out to fix.
func extendBoxToTouchedLines(box geom.Box, lines []ParagraphTextLine) (geom.Box, bool) {
	extended := box
	changed := false
	for _, line := range lines {
		if containedFraction(line.BBox, box) <= 0 {
			continue
		}
		grown := unionBoxes(extended, line.BBox)
		if !boxesNearlyEqual(grown, extended) {
			extended = grown
			changed = true
		}
	}
	return extended, changed
}

func containedFraction(inner, outer geom.Box) float64 {
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

// dedupeProposals collapses candidates with the same box. A one-group merge and
// an ink cluster covering the same figure are common, and a region competing
// against a duplicate of itself is noise in both training and inference.
//
// Ink wins ties, because an ink candidate carries counts a text candidate does
// not, and the extra features are strictly more information about the same box.
func dedupeProposals(proposals []RegionProposal) []RegionProposal {
	if len(proposals) < 2 {
		return proposals
	}
	sourceRank := map[ProposalSource]int{ProposalInk: 0, ProposalInkText: 1, ProposalText: 2}
	sort.SliceStable(proposals, func(a, b int) bool {
		if sourceRank[proposals[a].Source] != sourceRank[proposals[b].Source] {
			return sourceRank[proposals[a].Source] < sourceRank[proposals[b].Source]
		}
		return boxArea(proposals[a].Box) > boxArea(proposals[b].Box)
	})

	out := proposals[:0]
	for _, candidate := range proposals {
		duplicate := false
		for _, kept := range out {
			if boxesNearlyEqual(kept.Box, candidate.Box) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, candidate)
		}
	}
	return out
}
