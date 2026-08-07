package pdf

import (
	"math"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Ink clustering: region candidates built from what the page DRAWS, not from
// what it says.
//
// Every proposal source before this one was made of assembled text lines, and
// measured on DocLayNet val that caps Picture at 22.1% — 39.8% of annotated
// picture regions contain no text at all, so no amount of grouping over lines
// can reach them. Adding ink takes Picture to 63.1% on its own and 69.6%
// combined with text runs.
//
// Two facts from that measurement shape this file.
//
// First, 58.3% of picture regions are matched by a SINGLE drawn object at
// IoU >= 0.5. More than half the problem is not a clustering problem: the box is
// read straight off the content stream. So single objects are emitted as
// candidates in their own right and never only as cluster members.
//
// Second, clustering has to be done by rasterising. The obvious implementation —
// union-find over pairwise box proximity — does not finish on real input: a
// chart is thousands of path operations packed into a small area, which is
// simultaneously the quadratic worst case and the exact input this exists to
// handle. Painting boxes into a coarse grid and flood-filling is linear in page
// area and expresses "merge what is within gap of each other" directly.

const (
	// inkCellSize is the raster resolution in points. Small enough that a
	// hairline rule still occupies a cell, coarse enough that a page is a few
	// hundred cells on a side.
	inkCellSize = 2.0

	// inkClusterGap is how far apart two pieces of ink may sit and still join
	// one cluster. A chart's axis labels sit a few points off its axes; its
	// caption sits much further, and should not be swallowed.
	inkClusterGap = 6.0

	// inkMinArea is the smallest candidate worth proposing, in square points.
	// Below this a "cluster" is a bullet glyph or a stray rule, and proposing
	// thousands of them per page buries the real candidates.
	inkMinArea = 144.0 // 12pt x 12pt
)

// InkCluster is a candidate region built from drawn objects.
type InkCluster struct {
	Box geom.Box
	// Objects counts every drawn object in the cluster; Ink counts only the
	// non-text ones. A cluster that is all text is not an ink candidate — that
	// is what the line-based proposer already covers — so Ink is what
	// distinguishes a chart from a paragraph.
	Objects int
	Ink     int
	// Images, Paths and Shadings break Ink down, because "one image" and "four
	// hundred paths" are different kinds of region and the model should be able
	// to tell them apart.
	Images   int
	Paths    int
	Shadings int
	// Single marks a cluster that is exactly one drawn object, the 58.3% case.
	Single bool
}

// GroupInkClusters proposes region candidates from the objects a page draws.
//
// Text objects take part in clustering — a chart's axis labels belong to the
// chart — but cannot form a candidate alone, since a run of text with no ink in
// it is precisely what the line proposer already handles.
func GroupInkClusters(objects []page.DrawnObject, size geom.Size) []InkCluster {
	if len(objects) == 0 || size.Width <= 0 || size.Height <= 0 {
		return nil
	}

	// A form XObject's box is the union of its children, which are reported
	// alongside it. Clustering both would merge everything the form contains
	// into one blob regardless of how far apart it sits.
	usable := make([]page.DrawnObject, 0, len(objects))
	for _, obj := range objects {
		if obj.Kind == page.DrawnForm {
			continue
		}
		if obj.Box.Width() <= 0 && obj.Box.Height() <= 0 {
			continue // a point is not ink
		}
		usable = append(usable, obj)
	}
	if len(usable) == 0 {
		return nil
	}

	clusters := floodFillClusters(usable, size)

	// Single objects, as candidates in their own right. A photograph inside a
	// framed figure clusters together with its frame and caption, and the
	// cluster is a fine candidate for the figure — but the photograph alone is
	// the better candidate for the picture, and only one of the two can win.
	// Proposing both costs nothing and lets the model choose.
	for _, obj := range usable {
		if obj.Kind == page.DrawnText {
			continue
		}
		if boxArea(obj.Box) < inkMinArea {
			continue
		}
		cluster := InkCluster{Box: obj.Box, Objects: 1, Ink: 1, Single: true}
		countKind(&cluster, obj.Kind)
		clusters = append(clusters, cluster)
	}

	return dedupeInkClusters(clusters)
}

// floodFillClusters rasterises the boxes and takes connected components.
func floodFillClusters(objects []page.DrawnObject, size geom.Size) []InkCluster {
	cols := int(size.Width/inkCellSize) + 2
	rows := int(size.Height/inkCellSize) + 2
	if cols < 1 || rows < 1 {
		return nil
	}

	occupied := make([]bool, cols*rows)
	type span struct{ x0, y0, x1, y1 int }
	spans := make([]span, len(objects))

	pad := inkClusterGap / 2
	for i, obj := range objects {
		// Both ends of every range are clamped, not just the near one. Content
		// is routinely drawn off the page — bleed, oversized backgrounds — and
		// clamping one side turns those into an out-of-range index.
		s := span{
			x0: clampInt(int((obj.Box.L-pad)/inkCellSize), 0, cols-1),
			x1: clampInt(int((obj.Box.R+pad)/inkCellSize), 0, cols-1),
			y0: clampInt(int((topEdgeOf(obj.Box)-pad)/inkCellSize), 0, rows-1),
			y1: clampInt(int((bottomEdgeOf(obj.Box)+pad)/inkCellSize), 0, rows-1),
		}
		spans[i] = s
		for y := s.y0; y <= s.y1; y++ {
			row := y * cols
			for x := s.x0; x <= s.x1; x++ {
				occupied[row+x] = true
			}
		}
	}

	labels := make([]int32, cols*rows)
	next := int32(0)
	stack := make([]int, 0, 256)
	for start := range occupied {
		if !occupied[start] || labels[start] != 0 {
			continue
		}
		next++
		labels[start] = next
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			cell := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cx, cy := cell%cols, cell/cols
			// Eight-connectivity: diagonal touching counts, so a dashed
			// diagonal rule does not fragment into one cluster per dash.
			for dy := -1; dy <= 1; dy++ {
				ny := cy + dy
				if ny < 0 || ny >= rows {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					nx := cx + dx
					if nx < 0 || nx >= cols {
						continue
					}
					neighbour := ny*cols + nx
					if occupied[neighbour] && labels[neighbour] == 0 {
						labels[neighbour] = next
						stack = append(stack, neighbour)
					}
				}
			}
		}
	}
	if next == 0 {
		return nil
	}

	// Attribute each object to the component its own cells landed in, and take
	// the union of the ORIGINAL boxes. Using the raster extent instead would
	// inflate every candidate by the padding and depress its IoU.
	byLabel := make(map[int32]*InkCluster, next)
	for i, obj := range objects {
		label := labels[spans[i].y0*cols+spans[i].x0]
		if label == 0 {
			continue
		}
		cluster, ok := byLabel[label]
		if !ok {
			cluster = &InkCluster{Box: obj.Box}
			byLabel[label] = cluster
		} else {
			cluster.Box = unionBoxes(cluster.Box, obj.Box)
		}
		cluster.Objects++
		if obj.Kind != page.DrawnText {
			cluster.Ink++
			countKind(cluster, obj.Kind)
		}
	}

	out := make([]InkCluster, 0, len(byLabel))
	for _, cluster := range byLabel {
		if cluster.Ink == 0 || boxArea(cluster.Box) < inkMinArea {
			continue
		}
		cluster.Single = cluster.Objects == 1
		out = append(out, *cluster)
	}
	return out
}

func countKind(cluster *InkCluster, kind page.DrawnKind) {
	switch kind {
	case page.DrawnImage:
		cluster.Images++
	case page.DrawnPath:
		cluster.Paths++
	case page.DrawnShading:
		cluster.Shadings++
	}
}

// dedupeInkClusters drops candidates whose box duplicates one already kept. A
// single-object candidate and the cluster it belongs to are frequently the same
// box — a lone photograph with nothing near it — and proposing it twice would
// let the same region compete against itself.
func dedupeInkClusters(clusters []InkCluster) []InkCluster {
	if len(clusters) < 2 {
		return clusters
	}
	sort.SliceStable(clusters, func(a, b int) bool {
		return boxArea(clusters[a].Box) > boxArea(clusters[b].Box)
	})
	out := clusters[:0]
	for _, candidate := range clusters {
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

// boxesNearlyEqual treats boxes agreeing to within a raster cell as the same
// region, since the proposer cannot resolve finer than that anyway.
func boxesNearlyEqual(a, b geom.Box) bool {
	return math.Abs(a.L-b.L) <= inkCellSize &&
		math.Abs(a.R-b.R) <= inkCellSize &&
		math.Abs(topEdgeOf(a)-topEdgeOf(b)) <= inkCellSize &&
		math.Abs(bottomEdgeOf(a)-bottomEdgeOf(b)) <= inkCellSize
}

func boxArea(box geom.Box) float64 {
	width := box.Width()
	height := math.Abs(bottomEdgeOf(box) - topEdgeOf(box))
	return width * height
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
