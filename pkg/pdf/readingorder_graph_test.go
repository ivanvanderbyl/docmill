package pdf

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

// tlBox builds a top-left-origin box.
func tlBox(l, t, r, b float64) geom.Box {
	return geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}
}

// TestNeighbourPassBuildsEdgeMaps covers the directional-neighbour pass: a
// 3-cell vertical column A above B above C (lrtb) must build the direct-edge
// maps, not a transitive closure. successors[i] = cells after i;
// predecessors[i] = cells before i.
func TestNeighbourPassBuildsEdgeMaps(t *testing.T) {
	t.Parallel()

	// A above B above C, single column.
	const (
		a = 0
		b = 1
		c = 2
	)
	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),  // A
		tlBox(0, 20, 10, 30), // B
		tlBox(0, 40, 10, 50), // C
	}

	successors, predecessors := neighbourPass(boxes, dirLRTB)

	require.Equal(t, []int{b}, successors[a], "A's successor is B (direct, not B+C)")
	require.Equal(t, []int{c}, successors[b], "B's successor is C")
	require.Empty(t, successors[c], "C has no successor")

	require.Empty(t, predecessors[a], "A has no predecessor")
	require.Equal(t, []int{a}, predecessors[b], "B's predecessor is A")
	require.Equal(t, []int{b}, predecessors[c], "C's predecessor is B (not A+B)")
}

// TestNeighbourPassChainsWiderLowerCell guards the box-intersection spatial
// filter: a wide lower cell (a long heading) below a short upper cell must
// still chain, even though its centroid lies outside the upper cell's narrow
// search rect (centroid-containment would snap the chain).
func TestNeighbourPassChainsWiderLowerCell(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 80, 18),   // short heading "Chapter 2"
		tlBox(0, 30, 180, 48), // long heading "Narratives in Chuj" (centroid x=90)
		tlBox(0, 90, 80, 100), // body
	}

	successors, predecessors := neighbourPass(boxes, dirLRTB)

	require.Equal(t, []int{1}, successors[0], "short heading chains to the wider heading below it")
	require.Equal(t, []int{2}, successors[1])
	require.Empty(t, predecessors[0], "the top heading is the only head")
	require.Equal(t, []int{0}, predecessors[1])
}

// TestRefineExtentsAbsorbsSameColumnNeighbour covers the extent-merge
// refinement: two same-column cells with a small horizontal offset have their
// X-extents unioned when within threshold on both sides.
func TestRefineExtentsAbsorbsSameColumnNeighbour(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),      // A
		tlBox(0.5, 20, 10.5, 30), // B, offset right by 0.5
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)

	widened, _, _ := refineExtents(boxes, successors, predecessors, 1.0, dirLRTB)

	require.InDelta(t, 0.0, widened[0].L, 1e-9, "A absorbs B's left extent")
	require.InDelta(t, 10.5, widened[0].R, 1e-9, "A absorbs B's right extent")
	require.InDelta(t, 0.0, widened[1].L, 1e-9, "B absorbs A's left extent")
	require.InDelta(t, 10.5, widened[1].R, 1e-9)
}

// TestRefineExtentsCancelsOnSiblingOverlap covers the sibling-overlap cancel:
// widening A to absorb its successor would make A intersect sibling C, so the
// write-back is skipped and A keeps its extent.
func TestRefineExtentsCancelsOnSiblingOverlap(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),  // A
		tlBox(0, 20, 14, 30), // B (successor below, 4 wider on the right)
		tlBox(12, 2, 20, 8),  // C (sibling to the right of A, overlapping A's y-band)
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)

	widened, _, _ := refineExtents(boxes, successors, predecessors, 5.0, dirLRTB)

	require.InDelta(t, 10.0, widened[0].R, 1e-9, "A's widening is cancelled by sibling overlap")
	require.InDelta(t, 0.0, widened[0].L, 1e-9)
}

// TestDFSFollowsBelowEdges covers the DFS: an A->B->C chain linearises to
// [A,B,C].
func TestDFSFollowsBelowEdges(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),  // A
		tlBox(0, 20, 10, 30), // B
		tlBox(0, 40, 10, 50), // C
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)

	order := groupAndDFS(boxes, successors, predecessors, dirLRTB)

	require.Equal(t, []int{0, 1, 2}, order)
}

// TestDFSMultiHead covers two disjoint chains: heads in reading order, each
// followed by its chain.
func TestDFSMultiHead(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),   // A0 (left column)
		tlBox(0, 20, 10, 30),  // A1
		tlBox(50, 0, 60, 10),  // B0 (right column)
		tlBox(50, 20, 60, 30), // B1
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)

	order := groupAndDFS(boxes, successors, predecessors, dirLRTB)

	// Heads A0(left), B0(right) in reading order; each chain followed depth-first.
	require.Equal(t, []int{0, 1, 2, 3}, order)
}

// TestDFSWaitsForAllPredecessors covers the in-degree gating: a full-width cell
// below two separate cells (e.g. a body line under both a left and a right
// element, or under a table and a figure caption) must be emitted only after
// BOTH predecessors, not as soon as the first is reached. A plain visited-bit
// DFS would emit Body right after P1 (pulling it and the Footer ahead of P2);
// in-degree gating holds Body until P2 is also emitted.
func TestDFSWaitsForAllPredecessors(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),  // P1 (top-left)
		tlBox(50, 0, 60, 10), // P2 (top-right, disjoint from P1)
		tlBox(0, 40, 60, 50), // Body (spans below both P1 and P2)
		tlBox(0, 80, 60, 90), // Footer (below Body)
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)
	require.ElementsMatch(t, []int{0, 1}, predecessors[2], "Body has both P1 and P2 as predecessors")

	order := groupAndDFS(boxes, successors, predecessors, dirLRTB)

	require.Equal(t, []int{0, 1, 2, 3}, order, "Body and Footer follow both top cells")
}

// TestDFSIsolatedCells covers a cell with no edges: it has no predecessor, so it
// is a head and still appears in the output.
func TestDFSIsolatedCells(t *testing.T) {
	t.Parallel()

	boxes := []geom.Box{
		tlBox(0, 0, 10, 10),       // A
		tlBox(0, 20, 10, 30),      // B (A->B)
		tlBox(100, 100, 110, 110), // C (isolated)
	}
	successors, predecessors := neighbourPass(boxes, dirLRTB)

	order := groupAndDFS(boxes, successors, predecessors, dirLRTB)

	require.Len(t, order, 3)
	require.Contains(t, order, 2, "isolated cell still appears")
	require.Equal(t, []int{0, 1, 2}, order, "A,B chain then isolated C")
}

// TestOrderCellsGraphMatchesAcrossOrigins covers the coordinate-origin
// normalisation: the same physical cells produce the same reading order whether
// expressed in TopLeft or BottomLeft.
func TestOrderCellsGraphMatchesAcrossOrigins(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 100, Height: 100}
	// Physical cells, top-to-bottom: A (top), B, C (bottom), supplied out of order.
	topLeft := []page.TextCell{
		{Index: 1, Text: "B", Box: tlBox(0, 40, 10, 50)},
		{Index: 2, Text: "C", Box: tlBox(0, 80, 10, 90)},
		{Index: 3, Text: "A", Box: tlBox(0, 0, 10, 10)},
	}
	bottomLeft := make([]page.TextCell, len(topLeft))
	for i, c := range topLeft {
		c.Box = c.Box.WithOrigin(geom.BottomLeft, size.Height)
		bottomLeft[i] = c
	}

	tlOrder := texts(orderCellsGraph(topLeft, size, dirLRTB, true))
	blOrder := texts(orderCellsGraph(bottomLeft, size, dirLRTB, true))

	require.Equal(t, []string{"A", "B", "C"}, tlOrder, "top-left reads top-to-bottom")
	require.Equal(t, tlOrder, blOrder, "bottom-left order matches top-left")
}

// TestOrderCellsGraphDirectionsCovered exercises all four text-direction code
// paths (only lrtb is reachable in production; the rest are ready for a future
// TextDirection setting). The 2x2 grid yields a distinct deterministic order
// per direction.
func TestOrderCellsGraphDirectionsCovered(t *testing.T) {
	t.Parallel()

	grid := func() []page.TextCell {
		return []page.TextCell{
			{Index: 0, Text: "A", Box: tlBox(0, 0, 10, 10)},   // top-left
			{Index: 1, Text: "B", Box: tlBox(20, 0, 30, 10)},  // top-right
			{Index: 2, Text: "C", Box: tlBox(0, 20, 10, 30)},  // bottom-left
			{Index: 3, Text: "D", Box: tlBox(20, 20, 30, 30)}, // bottom-right
		}
	}
	size := geom.Size{Width: 40, Height: 40}

	cases := []struct {
		dir  readingDirection
		want []string
	}{
		{dirLRTB, []string{"A", "C", "B", "D"}}, // left column then right column
		{dirRLTB, []string{"B", "D", "A", "C"}}, // right column then left column
		{dirTBRL, []string{"A", "B", "C", "D"}}, // rows, right-successor chain
		{dirTBLR, []string{"B", "A", "D", "C"}}, // rows, left-successor chain
	}
	for _, tc := range cases {
		got := texts(orderCellsGraph(grid(), size, tc.dir, true))
		require.Equalf(t, tc.want, got, "direction %d", tc.dir)
	}
}

// TestOrderCellsGraphReassignsIndexAndPreservesInput confirms the public
// contract: Index reassigned to 0..n-1 in reading order, input not mutated,
// boxes preserved.
func TestOrderCellsGraphReassignsIndexAndPreservesInput(t *testing.T) {
	t.Parallel()

	size := geom.Size{Width: 100, Height: 100}
	input := []page.TextCell{
		{Index: 7, Text: "second", Box: tlBox(0, 40, 10, 50)},
		{Index: 9, Text: "first", Box: tlBox(0, 0, 10, 10)},
	}
	snapshot := append([]page.TextCell(nil), input...)

	out := orderCells(input, size)

	require.Equal(t, snapshot, input, "input slice is not mutated")
	require.Equal(t, []string{"first", "second"}, texts(out))
	require.Equal(t, 0, out[0].Index)
	require.Equal(t, 1, out[1].Index)
	require.Equal(t, tlBox(0, 0, 10, 10), out[0].Box, "boxes preserved")
}

func texts(cells []page.TextCell) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = c.Text
	}
	return out
}
