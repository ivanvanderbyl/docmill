// Ported (in bounding-box form) from core/fpdfapi/page/cpdf_clippath.{h,cpp} @
// pdfium 0db284a42.
//
// PDFium keeps the full clip path — every path and its fill rule — because it
// has to rasterise. We only ever need to answer "how much of this object is
// visible", so this keeps the INTERSECTION OF BOUNDING BOXES instead.
//
// That approximation has a direction, and it is the safe one. The true clip
// region is always a subset of its own bounding box, so the visible area we
// compute is always a superset of the real one. We may therefore report a
// little ink that a non-rectangular clip actually hides; we can never hide ink
// that is really drawn. For layout analysis, wrongly keeping content is
// recoverable and wrongly dropping it is not.
//
// In practice the approximation is nearly exact: the overwhelmingly common clip
// in real PDFs is `re W n`, a single rectangle, for which the bounding box IS
// the clip.
package page

import "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"

// ClipPath is the accumulated clip region as a bounding box.
//
// The zero value means "no clip has been set", which is unbounded — NOT an
// empty region. That distinction matters because the zero FloatRect is empty,
// so an unset clip must be tracked by the flag rather than inferred from the
// box.
//
// It is a value type on purpose. GraphicStates is saved by q and restored by Q
// through a whole-struct copy, so value semantics give correct save/restore for
// free; a pointer would alias the saved state and let a clip set inside q/Q
// escape it.
type ClipPath struct {
	box     crt.FloatRect
	bounded bool
}

// Bounded reports whether any clip has been applied.
func (c ClipPath) Bounded() bool { return c.bounded }

// Box returns the clip bounding box and whether it is bounded at all.
func (c ClipPath) Box() (crt.FloatRect, bool) { return c.box, c.bounded }

// IntersectRect narrows the clip by rect, which is the W/W* semantic: each new
// clip path intersects with the one already in force, never replaces it.
func (c *ClipPath) IntersectRect(rect crt.FloatRect) {
	rect.Normalize()
	if !c.bounded {
		c.box = rect
		c.bounded = true
		return
	}
	c.box.Intersect(rect)
}

// Clip returns rect restricted to the visible region, and whether anything of
// it survives. An unbounded clip returns rect unchanged.
//
// A zero-width or zero-height rect is passed through rather than rejected: a
// horizontal rule is a legitimately flat object, and FloatRect.IsEmpty treats
// flat as empty. Only a genuine miss — no overlap on an axis — is invisible.
func (c ClipPath) Clip(rect crt.FloatRect) (crt.FloatRect, bool) {
	rect.Normalize()
	if !c.bounded {
		return rect, true
	}
	clip := c.box
	out := crt.NewFloatRect(
		maxFloat(rect.Left, clip.Left),
		maxFloat(rect.Bottom, clip.Bottom),
		minFloat(rect.Right, clip.Right),
		minFloat(rect.Top, clip.Top),
	)
	if out.Left > out.Right || out.Bottom > out.Top {
		return crt.FloatRect{}, false
	}
	return out, true
}

func minFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
