package pdf

// Zone-level reading order: a directional-neighbour spatial graph +
// threshold-gated X-extent refinement + an in-degree-gated topological DFS. It
// is a graph traversal (not a detector with a confidence gate), so it follows
// the actual reading flow across columns, floating figures, and ragged layouts.
// orderCells applies it on confidently multi-column pages (see the orderCells
// doc comment for the measured reason it is gated rather than universal); the
// graph machinery itself always assigns a complete reading order to whatever
// cells it is given.
//
// Granularity note: the classic algorithm orders layout ZONES (one
// paragraph-block per zone); docmill's unit is the page.TextCell (one PDFium
// text rect), which is denser. The algorithm is identical at either
// granularity — a directional-neighbour graph over bounding boxes — with one
// cell-granularity adaptation in the DFS (see groupAndDFS): the walk is gated by
// in-degree so a full-width cell that sits below MANY cells is emitted only
// after all of them, which a plain DFS would not guarantee. orderCells'
// reassigned Index drives BLOCK ordering (via each block's MinIndex);
// within-block text is re-sorted geometrically by AssembleLineElements, so the
// cell-level order never corrupts paragraph text.
//
// Validation: on the 200-doc DPBench corpus this (gated to multi-column) is
// regression-free across all scored metrics and faster per page than the
// previous detector. The corpus is single-page with no gutter-confident
// multi-column reading-order cases, so the graph's multi-column and
// floating-figure handling — exercised by the package unit tests — is dormant on
// it. Measured along the way: an un-gated graph regressed reading-order NID by
// 0.0085 with a plain DFS and by 0.0008 with the in-degree-gated DFS (a few docs
// each way, on single-column-with-annotation pages the corpus over-represents),
// which is why orderCells gates rather than applying the graph universally.
//
// Labelled approximations:
//   - The dispatcher's 3-group directional split is NOT implemented: it is a
//     no-op for single-column body pages, and the cell granularity has no page
//     reference line to partition against. The multi-pass sorter runs over the
//     full cell set as one group.
//   - The neighbour pass records only DIRECT successor edges (the covering
//     relation) rather than the full transitive closure; recording only direct
//     neighbours yields the same DFS linearisation independent of input order
//     (above[A] = [B], not [B, C]).
//   - The refinement threshold is 0.15 in box coordinate units, applied
//     directly as the conservative default so extent-merge stays conservative
//     on PDF-point coordinates.
//   - A separate paragraph-level XY-cut is out of scope: docmill's per-column
//     AssembleLineElements already does the paragraph-within-column job.

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Geometry literals shared by the directional predicates and the search-rect
// build.
const (
	// roEpsilon is the 0.001 tolerance shared by every directional predicate.
	roEpsilon = 0.001
	// roMarginXY (0.1) is the search-rect edge inset.
	roMarginXY = 0.1
	// roExtent (0.2) is the search-rect extent widening (0.1 on each side).
	roExtent = 0.2
	// roSentinelFar (10000.0) is the open-side "to infinity" sentinel.
	roSentinelFar = 10000.0
	// roDefaultThreshold (0.15) is the column-merge tolerance, used when no
	// document settings supply a threshold.
	roDefaultThreshold = 0.15
)

// readingDirection is the text-direction enum: lrtb→0, rltb→1, tbrl→2, tblr→3,
// default lrtb. Only dirLRTB is reachable in production (the Latin default); the
// other three are implemented for completeness and covered by unit tests so the
// code is ready once a TextDirection setting is threaded through
// ExtractionOptions.
type readingDirection int

const (
	dirLRTB readingDirection = iota // 0 — left-to-right, top-to-bottom (default)
	dirRLTB                         // 1 — right-to-left, top-to-bottom
	dirTBRL                         // 2 — top-to-bottom, right-to-left
	dirTBLR                         // 3 — top-to-bottom, left-to-right
)

// boxTopV / boxBottomV return the box's top and bottom edges as absolute
// top-left-space values (top < bottom). Boxes reaching the graph are normalised
// to TopLeft, so T <= B, but min/max keeps the helpers origin-robust.
func boxTopV(b geom.Box) float64    { return math.Min(b.T, b.B) }
func boxBottomV(b geom.Box) float64 { return math.Max(b.T, b.B) }

// below reports whether c lies below z: z.Y2 + 0.001 <= c.Y1.
func below(z, c geom.Box) bool { return boxBottomV(z)+roEpsilon <= boxTopV(c) }

// horizontallyOverlaps reports max(z.X1,c.X1) < min(z.X2,c.X2) - 0.001.
func horizontallyOverlaps(z, c geom.Box) bool {
	return math.Max(z.L, c.L) < math.Min(z.R, c.R)-roEpsilon
}

// isRightOf reports whether c is to the right of z: z.X2 + 0.001 <= c.X1.
func isRightOf(z, c geom.Box) bool { return z.R+roEpsilon <= c.L }

// isLeftOf reports whether c is to the left of z: c.X2 <= z.X1 + 0.001.
func isLeftOf(c, z geom.Box) bool { return c.R <= z.L+roEpsilon }

// verticallyOverlaps reports max(z.Y1,c.Y1) + 0.001 < min(z.Y2,c.Y2).
func verticallyOverlaps(z, c geom.Box) bool {
	return math.Max(boxTopV(z), boxTopV(c))+roEpsilon < math.Min(boxBottomV(z), boxBottomV(c))
}

// rectsIntersect reports rect–rect intersection, used by the refinement's
// sibling-overlap cancel.
func rectsIntersect(a, b geom.Box) bool {
	return math.Max(a.L, b.L) < math.Min(a.R, b.R) &&
		math.Max(boxTopV(a), boxTopV(b)) < math.Min(boxBottomV(a), boxBottomV(b))
}

// searchRect builds the per-direction search rectangle for zone z. The rect is
// expressed as (x, y, width, height) and returned as a geom.Box (L,T,R,B),
// converting R = x + width and B = y + height. The returned box is in top-left
// space (T = top edge, B = bottom edge).
func searchRect(z geom.Box, dir readingDirection) geom.Box {
	x1, x2 := z.L, z.R
	y1, y2 := boxTopV(z), boxBottomV(z)
	switch dir {
	case dirTBRL: // look RIGHT: x=X2-0.1, y=Y1-0.1, w=10000, h=(Y2-Y1)+0.2
		left := x2 - roMarginXY
		top := y1 - roMarginXY
		return geom.Box{
			L:      left,
			T:      top,
			R:      left + roSentinelFar,
			B:      top + (y2 - y1) + roExtent,
			Origin: geom.TopLeft,
		}
	case dirTBLR: // look LEFT: x=0, y=Y1-0.1, w=X1+0.1, h=(Y2-Y1)+0.2
		top := y1 - roMarginXY
		return geom.Box{
			L:      0,
			T:      top,
			R:      x1 + roMarginXY,
			B:      top + (y2 - y1) + roExtent,
			Origin: geom.TopLeft,
		}
	default: // dirLRTB / dirRLTB — look DOWN: x=X1-0.1, y=Y2, w=(X2-X1)+0.2, h=10000
		left := x1 - roMarginXY
		return geom.Box{
			L:      left,
			T:      y2,
			R:      left + (x2 - x1) + roExtent, // == X2 + 0.1
			B:      y2 + roSentinelFar,
			Origin: geom.TopLeft,
		}
	}
}

// directionalOK applies the per-direction acceptance predicate for candidate c
// relative to current zone z:
//   - lrtb/rltb: c below z AND horizontal overlap.
//   - tbrl:      c right of z AND vertical overlap.
//   - tblr:      c left of z AND vertical overlap.
func directionalOK(z, c geom.Box, dir readingDirection) bool {
	switch dir {
	case dirTBRL:
		return isRightOf(z, c) && verticallyOverlaps(z, c)
	case dirTBLR:
		return isLeftOf(c, z) && verticallyOverlaps(z, c)
	default: // dirLRTB / dirRLTB
		return below(z, c) && horizontallyOverlaps(z, c)
	}
}

// rectQualifies reports whether candidate box c is returned by the search-rect
// query. The query returns boxes INTERSECTING the rect, so box-intersection is
// the faithful brute-force equivalent. (Centroid-containment breaks when a lower
// cell is wider than the current zone: its centroid falls outside the zone's
// narrow search rect and the vertical chain snaps, e.g. a long heading below a
// short one. Box-intersection keeps the chain.)
func rectQualifies(rect, c geom.Box) bool {
	return rectsIntersect(rect, c)
}

// neighbourPass builds the directional-neighbour edge maps. For each zone it
// builds a directional search rect, collects the candidates whose box intersects
// it (rectQualifies) and which pass the directional predicate, and records
// DIRECT edges into the two edge maps keyed by zone index:
//
//   - successors[i] = indices of cells immediately AFTER i in the reading flow.
//   - predecessors[j] = indices of cells immediately BEFORE j.
//
// A candidate j of z is DIRECT iff no other candidate k of z also has j as a
// directional neighbour (directionalOK(k, j)); that transitive-reduction keeps
// the maps a covering relation (Hasse diagram) so the DFS linearisation does
// not depend on input order. See the package-level approximation note.
func neighbourPass(boxes []geom.Box, dir readingDirection) (successors, predecessors map[int][]int) {
	successors = make(map[int][]int)
	predecessors = make(map[int][]int)
	candidates := make([]int, 0, 8)
	for i := range boxes {
		candidates = candidates[:0]
		for j := range boxes {
			if j == i {
				continue
			}
			if rectQualifies(searchRect(boxes[i], dir), boxes[j]) && directionalOK(boxes[i], boxes[j], dir) {
				candidates = append(candidates, j)
			}
		}
		for _, j := range candidates {
			direct := true
			for _, k := range candidates {
				if k == j {
					continue
				}
				// j is reachable via k (z -> k -> j), so the z -> j edge is
				// transitive; drop it and keep z -> k.
				if directionalOK(boxes[k], boxes[j], dir) {
					direct = false
					break
				}
			}
			if direct {
				successors[i] = append(successors[i], j)
				predecessors[j] = append(predecessors[j], i)
			}
		}
	}
	return successors, predecessors
}

// refineExtents performs the extent-merge refinement. For each zone it absorbs
// its predecessor's and successor's X-extent when the union widens each side by
// no more than threshold, cancelling the write-back if the widened rect would
// intersect any sibling. It returns a fresh box slice with the merged extents
// and the edge maps re-derived from the widened boxes. The input is not mutated.
//
// With the 0.15 threshold in PDF-point units this is conservative (only
// near-identical X-extents merge), so on real coordinates it is close to a
// no-op; it is gated on runRefinement (default true).
func refineExtents(boxes []geom.Box, successors, predecessors map[int][]int, threshold float64, dir readingDirection) ([]geom.Box, map[int][]int, map[int][]int) {
	widened := make([]geom.Box, len(boxes))
	copy(widened, boxes)

	absorb := func(x1, x2 float64, neighbours []int) (float64, float64) {
		for _, n := range neighbours {
			lo := math.Min(x1, boxes[n].L)
			hi := math.Max(x2, boxes[n].R)
			if x1-lo <= threshold && hi-x2 <= threshold {
				x1, x2 = lo, hi
			}
		}
		return x1, x2
	}

	for i := range boxes {
		x1, x2 := boxes[i].L, boxes[i].R
		x1, x2 = absorb(x1, x2, successors[i])
		x1, x2 = absorb(x1, x2, predecessors[i])
		if x1 == boxes[i].L && x2 == boxes[i].R {
			continue
		}
		candidate := geom.Box{L: x1, T: boxes[i].T, R: x2, B: boxes[i].B, Origin: boxes[i].Origin}
		// Sibling-overlap cancel: skip the write-back if widening would make i
		// intersect another zone it does not already overlap.
		cancel := false
		for j := range boxes {
			if j == i {
				continue
			}
			if rectsIntersect(candidate, boxes[j]) && !rectsIntersect(boxes[i], boxes[j]) {
				cancel = true
				break
			}
		}
		if !cancel {
			widened[i] = candidate
		}
	}

	newSucc, newPred := neighbourPass(widened, dir)
	return widened, newSucc, newPred
}

// groupAndDFS performs the graph grouping + DFS. It seeds from the zone heads —
// cells with no predecessor (nothing before them) — in reading order, then
// performs a depth-first walk following successor edges. The result is a
// topological linearisation of the directional DAG (a graph traversal, not a
// centroid sort). Heads and successors are visited in reading order so the
// linearisation is deterministic.
//
// The walk is gated by in-degree: a cell is emitted only once ALL of its
// predecessors are emitted (it is reached when its last predecessor's edge is
// followed). This is a cell-granularity adaptation of the plain visited-bit DFS.
// At zone granularity a body paragraph below a table is one zone with a single
// predecessor; at docmill's per-cell granularity a full-width cell sits below
// MANY cells (e.g. a body line under both a table and a figure caption), so a
// plain DFS would emit it as soon as the first predecessor is reached — pulling
// the body (and the footer below it) ahead of a mid-page caption that should
// precede it (a measured DPBench regression). Gating on in-degree makes the cell
// wait for every predecessor while still preferring depth-first column
// continuation, so it is correct for single columns with floating furniture,
// multi-column pages (each column reads fully before the next), and full-width
// spanning elements (a footer below two columns is emitted after both).
//
// Returns a permutation of the input indices in reading order.
func groupAndDFS(boxes []geom.Box, successors, predecessors map[int][]int, dir readingDirection) []int {
	n := len(boxes)
	sortByReading := func(idxs []int) []int {
		out := append([]int(nil), idxs...)
		sort.SliceStable(out, func(a, b int) bool {
			return readingLess(boxes[out[a]], boxes[out[b]], dir)
		})
		return out
	}

	indeg := make([]int, n)
	succSorted := make([][]int, n)
	heads := make([]int, 0, n)
	for i := range boxes {
		indeg[i] = len(predecessors[i])
		succSorted[i] = sortByReading(successors[i])
		if indeg[i] == 0 {
			heads = append(heads, i)
		}
	}
	heads = sortByReading(heads)

	visited := make([]bool, n)
	order := make([]int, 0, n)
	var visit func(i int)
	visit = func(i int) {
		visited[i] = true
		order = append(order, i)
		for _, j := range succSorted[i] {
			if visited[j] {
				continue
			}
			if indeg[j]--; indeg[j] <= 0 {
				visit(j)
			}
		}
	}
	for _, h := range heads {
		if !visited[h] {
			visit(h)
		}
	}
	// Defensive: the "below" relation is a strict partial order, so the graph is
	// a DAG and every cell is reachable from a head once all predecessors are
	// emitted. Any cell left unvisited would imply a cycle (impossible) or an
	// isolated remainder — force-emit it in reading order so the output is a
	// complete permutation.
	for _, i := range sortByReading(rangeIndices(n)) {
		if !visited[i] {
			visit(i)
		}
	}
	return order
}

func rangeIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// readingLess is the deterministic ordering key for seeds and successors per
// direction. Only the directional graph (the predicates) determines structure;
// this just linearises ties in reading order.
func readingLess(a, b geom.Box, dir readingDirection) bool {
	switch dir {
	case dirRLTB: // top-to-bottom, right-to-left
		if boxTopV(a) != boxTopV(b) {
			return boxTopV(a) < boxTopV(b)
		}
		return a.R > b.R
	case dirTBRL: // columns right-to-left, each top-to-bottom
		if a.R != b.R {
			return a.R > b.R
		}
		return boxTopV(a) < boxTopV(b)
	case dirTBLR: // columns left-to-right, each top-to-bottom
		if a.L != b.L {
			return a.L < b.L
		}
		return boxTopV(a) < boxTopV(b)
	default: // dirLRTB — top-to-bottom, left-to-right
		if boxTopV(a) != boxTopV(b) {
			return boxTopV(a) < boxTopV(b)
		}
		return a.L < b.L
	}
}

// orderCells returns the cells re-sorted into reading order, reassigning each
// cell's Index to the new 0..n-1 order. The input slice is not mutated; each
// returned cell keeps its original Box (and origin) — only Index and slice
// position change.
//
// It applies the directional-neighbour graph (orderCellsGraph) only when the
// page is confidently MULTI-COLUMN, and keeps the proven
// top-to-bottom-then-left single-column sort otherwise. This split is empirical,
// measured on the 200-doc DPBench corpus: the graph's depth-first column
// traversal is the right linearisation for genuine columns (each column reads
// fully before the next), but on a single column it orders a separate reading
// head's subtree (a right-side annotation, a floating caption) after the
// main-column subtree that reaches the footer — the column-vs-position tension.
// In-degree gating in groupAndDFS removes the multi-predecessor sub-case, but
// the multi-head sub-case is irreducible without breaking true columns. The
// always-graph variant regressed reading-order NID by ~0.0008 (a handful of
// docs each way); gating to the multi-column case is regression-free and keeps
// the graph active where it is correct. The column signal is the same gutter
// detector that drives partitionColumns, so ordering and column partitioning
// stay consistent. (A measured regression is not an acceptable trade-off.)
func orderCells(cells []page.TextCell, size geom.Size) []page.TextCell {
	if multiColumnLayout(cells, size) {
		// runRefinement defaults true.
		return orderCellsGraph(cells, size, dirLRTB, true)
	}
	out := sortReadingGroup(cells)
	for i := range out {
		out[i].Index = i
	}
	return out
}

// multiColumnLayout reports whether the gutter detector is confident the page
// has more than one text column (the same detection partitionColumns uses).
func multiColumnLayout(cells []page.TextCell, size geom.Size) bool {
	return len(columnGroups(cells, size, false)) > 1
}

// orderCellsGraph is the testable core of orderCells with the direction and
// refinement gate exposed. dir is hardcoded to dirLRTB by orderCells (the Latin
// default); the other directions are reachable only from tests until a
// TextDirection setting is threaded through ExtractionOptions.
func orderCellsGraph(cells []page.TextCell, size geom.Size, dir readingDirection, runRefinement bool) []page.TextCell {
	if len(cells) <= 1 {
		out := append([]page.TextCell(nil), cells...)
		for i := range out {
			out[i].Index = i
		}
		return out
	}

	// Normalise every box to TopLeft so the algorithm math (which assumes
	// top-left origin with Y1 < Y2) is correct regardless of the source origin.
	// The original cells (and their origins) are preserved in the output; only
	// Index and order change.
	boxes := make([]geom.Box, len(cells))
	for i := range cells {
		boxes[i] = cells[i].Box.WithOrigin(geom.TopLeft, size.Height)
	}

	successors, predecessors := neighbourPass(boxes, dir)
	if runRefinement {
		boxes, successors, predecessors = refineExtents(boxes, successors, predecessors, roDefaultThreshold, dir)
	}
	order := groupAndDFS(boxes, successors, predecessors, dir)

	out := make([]page.TextCell, len(order))
	for newIndex, oldIndex := range order {
		cell := cells[oldIndex]
		cell.Index = newIndex
		out[newIndex] = cell
	}
	return out
}
