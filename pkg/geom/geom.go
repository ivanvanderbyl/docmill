// Package geom provides docling-compatible 2-D geometry: bounding boxes with an
// explicit coordinate origin (top-left or bottom-left) plus the intersection,
// area, and overlap helpers the extraction pipeline relies on.
package geom

import "math"

// CoordOrigin identifies which corner a Box's coordinates are measured from.
type CoordOrigin string

const (
	// TopLeft is the y-down origin: the top edge has the smaller y.
	TopLeft CoordOrigin = "TOPLEFT"
	// BottomLeft is the PDF y-up origin: the top edge has the larger y.
	BottomLeft CoordOrigin = "BOTTOMLEFT"
)

// Size is a width/height pair in points.
type Size struct {
	Width  float64
	Height float64
}

// Box is an axis-aligned rectangle: left/top/right/bottom edges expressed in the
// coordinate origin recorded in Origin.
type Box struct {
	L      float64
	T      float64
	R      float64
	B      float64
	Origin CoordOrigin
}

// BoxFromTuple, AsTuple, and Scaled mirror docling-core BoundingBox helpers.
// They are part of the geometry API surface and are currently exercised only
// by tests; keep them in sync with docling-core base.py.

// BoxFromTuple builds a Box from an (l, t, r, b) tuple in the given origin,
// normalising a BottomLeft tuple so T/B stay edge-consistent with the type.
func BoxFromTuple(l, t, r, b float64, origin CoordOrigin) Box {
	if origin == BottomLeft {
		return Box{L: l, T: b, R: r, B: t, Origin: origin}
	}
	return Box{L: l, T: t, R: r, B: b, Origin: origin}
}

// AsTuple returns the box edges as [l, t, r, b] in its own origin.
func (b Box) AsTuple() []float64 {
	if b.Origin == BottomLeft {
		return []float64{b.L, b.B, b.R, b.T}
	}
	return []float64{b.L, b.T, b.R, b.B}
}

// Width returns the box width (|R-L|).
func (b Box) Width() float64 {
	return math.Abs(b.R - b.L)
}

// Height returns the box height (|T-B|).
func (b Box) Height() float64 {
	return math.Abs(b.T - b.B)
}

// Area returns the box area (Width * Height).
func (b Box) Area() float64 {
	return b.Width() * b.Height()
}

// CenterX returns the horizontal midpoint of the box.
func (b Box) CenterX() float64 {
	return (b.L + b.R) / 2
}

// CenterY returns the vertical midpoint of the box. It is origin-independent:
// for both TopLeft and BottomLeft boxes the midpoint is (T+B)/2.
func (b Box) CenterY() float64 {
	return (b.T + b.B) / 2
}

// WithOrigin returns the box converted to the given origin by flipping T and B
// about pageHeight. It is a no-op when the box is already in that origin.
func (b Box) WithOrigin(origin CoordOrigin, pageHeight float64) Box {
	if b.Origin == origin {
		return b
	}
	return Box{
		L:      b.L,
		T:      pageHeight - b.T,
		R:      b.R,
		B:      pageHeight - b.B,
		Origin: origin,
	}
}

// Scaled returns the box with each edge multiplied by the x/y scale factors.
func (b Box) Scaled(x, y float64) Box {
	return Box{
		L:      b.L * x,
		T:      b.T * y,
		R:      b.R * x,
		B:      b.B * y,
		Origin: b.Origin,
	}
}

// IntersectionArea returns the area of the overlap between b and other, or 0 if
// they do not overlap. It is origin-agnostic (it normalises top/bottom).
func (b Box) IntersectionArea(other Box) float64 {
	left := math.Max(b.L, other.L)
	right := math.Min(b.R, other.R)
	if right <= left {
		return 0
	}

	top := math.Max(math.Min(b.T, b.B), math.Min(other.T, other.B))
	bottom := math.Min(math.Max(b.T, b.B), math.Max(other.T, other.B))
	if bottom <= top {
		return 0
	}

	return (right - left) * (bottom - top)
}

// IoU returns the intersection-over-union of b and other (0 when disjoint).
func (b Box) IoU(other Box) float64 {
	intersection := b.IntersectionArea(other)
	if intersection == 0 {
		return 0
	}
	union := b.Area() + other.Area() - intersection
	if union <= 0 {
		return 0
	}
	return intersection / union
}

// IntersectionOverSelf returns the fraction of b's own area covered by other.
func (b Box) IntersectionOverSelf(other Box) float64 {
	area := b.Area()
	if area <= 0 {
		return 0
	}
	return b.IntersectionArea(other) / area
}

// EnclosingBox returns the smallest box containing every input box. The result
// takes the origin of the first box; an empty input yields the zero Box.
func EnclosingBox(boxes ...Box) Box {
	if len(boxes) == 0 {
		return Box{}
	}

	result := boxes[0]
	for _, box := range boxes[1:] {
		result.L = math.Min(result.L, box.L)
		result.R = math.Max(result.R, box.R)
		switch result.Origin {
		case BottomLeft:
			result.T = math.Max(result.T, box.T)
			result.B = math.Min(result.B, box.B)
		default:
			result.T = math.Min(result.T, box.T)
			result.B = math.Max(result.B, box.B)
		}
	}
	return result
}
