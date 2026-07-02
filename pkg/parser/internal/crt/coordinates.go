// Package crt holds the idiomatic-Go foundations ported from PDFium's
// core/fxcrt layer: the affine matrix, float rectangle, point, and number
// value types, plus PDF text-string decoding helpers.
//
// Ported from core/fxcrt/fx_coordinates.{h,cpp} @ pdfium 0db284a42.
// Algorithmic 1:1 port. PDFium's C++ memory machinery (RetainPtr, span,
// DataVector) is not reproduced; we use Go values and slices. The geometry is
// computed in float32 to match PDFium's `float` arithmetic exactly,
// reproducing its rounding bit-for-bit. See plan 009 Phase A.
package crt

import "math"

// PointF mirrors CFX_PointF: a 2D point in float32.
type PointF struct {
	X float32
	Y float32
}

// FloatRect mirrors CFX_FloatRect. Note the PDFium field order and semantics:
// the constructor is (left, bottom, right, top) and Y grows upward, so a
// "normalized" rect has left<=right and bottom<=top.
type FloatRect struct {
	Left   float32
	Bottom float32
	Right  float32
	Top    float32
}

// NewFloatRect constructs a rect in PDFium's (left, bottom, right, top) order.
func NewFloatRect(left, bottom, right, top float32) FloatRect {
	return FloatRect{Left: left, Bottom: bottom, Right: right, Top: top}
}

// Width returns right-left (may be negative on an un-normalized rect).
func (r FloatRect) Width() float32 { return r.Right - r.Left }

// Height returns top-bottom (may be negative on an un-normalized rect).
func (r FloatRect) Height() float32 { return r.Top - r.Bottom }

// IsEmpty reports whether the rect has no positive area.
func (r FloatRect) IsEmpty() bool { return r.Left >= r.Right || r.Bottom >= r.Top }

// Normalize swaps inverted edges so left<=right and bottom<=top.
//
// Carry-over fix (plan 009 Phase A): downstream rect-union in the textpage
// depends on this swap-on-inverted behaviour.
func (r *FloatRect) Normalize() {
	if r.Left > r.Right {
		r.Left, r.Right = r.Right, r.Left
	}
	if r.Bottom > r.Top {
		r.Top, r.Bottom = r.Bottom, r.Top
	}
}

// Intersect replaces r with its intersection with other; empties on no overlap.
func (r *FloatRect) Intersect(other FloatRect) {
	r.Normalize()
	other.Normalize()
	r.Left = maxf(r.Left, other.Left)
	r.Bottom = maxf(r.Bottom, other.Bottom)
	r.Right = minf(r.Right, other.Right)
	r.Top = minf(r.Top, other.Top)
	if r.Left > r.Right || r.Bottom > r.Top {
		*r = FloatRect{}
	}
}

// Union grows r to also contain other.
func (r *FloatRect) Union(other FloatRect) {
	r.Normalize()
	other.Normalize()
	r.Left = minf(r.Left, other.Left)
	r.Bottom = minf(r.Bottom, other.Bottom)
	r.Right = maxf(r.Right, other.Right)
	r.Top = maxf(r.Top, other.Top)
}

// ContainsPoint reports whether point lies within the normalized rect.
func (r FloatRect) ContainsPoint(point PointF) bool {
	n1 := r
	n1.Normalize()
	return point.X <= n1.Right && point.X >= n1.Left && point.Y <= n1.Top && point.Y >= n1.Bottom
}

// Inflate expands the (normalized) rect outward by the given per-edge amounts.
func (r *FloatRect) Inflate(left, bottom, right, top float32) {
	r.Normalize()
	r.Left -= left
	r.Bottom -= bottom
	r.Right += right
	r.Top += top
}

// Deflate shrinks the (normalized) rect inward by the given per-edge amounts.
func (r *FloatRect) Deflate(left, bottom, right, top float32) {
	r.Inflate(-left, -bottom, -right, -top)
}

// Translate shifts the rect by (e, f).
func (r *FloatRect) Translate(e, f float32) {
	r.Left += e
	r.Right += e
	r.Top += f
	r.Bottom += f
}

// Scale multiplies every edge by fScale.
func (r *FloatRect) Scale(fScale float32) {
	r.Left *= fScale
	r.Bottom *= fScale
	r.Right *= fScale
	r.Top *= fScale
}

// ScaleFromCenterPoint scales the rect about its centre.
func (r *FloatRect) ScaleFromCenterPoint(fScale float32) {
	halfWidth := (r.Right - r.Left) / 2.0
	halfHeight := (r.Top - r.Bottom) / 2.0
	centerX := (r.Left + r.Right) / 2
	centerY := (r.Top + r.Bottom) / 2
	r.Left = centerX - halfWidth*fScale
	r.Bottom = centerY - halfHeight*fScale
	r.Right = centerX + halfWidth*fScale
	r.Top = centerY + halfHeight*fScale
}

// GetBBox returns the bounding box of the points, or the zero rect when empty.
// Ported from CFX_FloatRect::GetBBox.
func GetBBox(points []PointF) FloatRect {
	if len(points) == 0 {
		return FloatRect{}
	}
	minX, maxX := points[0].X, points[0].X
	minY, maxY := points[0].Y, points[0].Y
	for _, point := range points[1:] {
		minX = minf(minX, point.X)
		maxX = maxf(maxX, point.X)
		minY = minf(minY, point.Y)
		maxY = maxf(maxY, point.Y)
	}
	return NewFloatRect(minX, minY, maxX, maxY)
}

// Matrix mirrors CFX_Matrix: the 2x3 affine transform [a b c d e f].
// The zero value is NOT the identity; use NewMatrix or IdentityMatrix.
type Matrix struct {
	A float32
	B float32
	C float32
	D float32
	E float32
	F float32
}

// IdentityMatrix returns the identity transform (the default CFX_Matrix()).
func IdentityMatrix() Matrix {
	return Matrix{A: 1, B: 0, C: 0, D: 1, E: 0, F: 0}
}

// NewMatrix constructs a matrix from explicit coefficients.
func NewMatrix(a, b, c, d, e, f float32) Matrix {
	return Matrix{A: a, B: b, C: c, D: d, E: e, F: f}
}

// Multiply returns the product m*right, matching CFX_Matrix::operator*.
//
// Products are stored in float32 locals before being summed so each operation
// rounds to float32 independently. This deliberately defeats Go's FMA
// contraction (active on arm64): PDFium does not fuse multiply-add, and the
// port reproduces its bit-identical results. See plan 009 Phase A.
func (m Matrix) Multiply(right Matrix) Matrix {
	aA := m.A * right.A
	bC := m.B * right.C
	aB := m.A * right.B
	bD := m.B * right.D
	cA := m.C * right.A
	dC := m.D * right.C
	cB := m.C * right.B
	dD := m.D * right.D
	eA := m.E * right.A
	fC := m.F * right.C
	eB := m.E * right.B
	fD := m.F * right.D
	return Matrix{
		A: aA + bC,
		B: aB + bD,
		C: cA + dC,
		D: cB + dD,
		E: eA + fC + right.E,
		F: eB + fD + right.F,
	}
}

// IsIdentity reports whether m equals the identity transform.
func (m Matrix) IsIdentity() bool { return m == IdentityMatrix() }

// GetInverse returns the inverse transform, or the identity-ish zero result
// when the determinant is zero. Ported verbatim including the bug-for-bug
// behaviour on near-singular matrices (see fx_coordinates_unittest CR cases).
func (m Matrix) GetInverse() Matrix {
	inverse := IdentityMatrix()
	// Round each product to float32 before subtracting (defeat FMA): the
	// determinant of a near-singular matrix is dominated by cancellation, so a
	// fused a*d-b*c diverges sharply from PDFium. See CR702041/CR714187 cases.
	ad := m.A * m.D
	bc := m.B * m.C
	i := ad - bc
	if i == 0 {
		return inverse
	}
	j := -i
	inverse.A = m.D / i
	inverse.B = m.B / j
	inverse.C = m.C / j
	inverse.D = m.A / i
	cf := m.C * m.F
	de := m.D * m.E
	inverse.E = (cf - de) / i
	af := m.A * m.F
	be := m.B * m.E
	inverse.F = (af - be) / j
	return inverse
}

// Concat post-multiplies m by right in place (m = m * right).
func (m *Matrix) Concat(right Matrix) { *m = m.Multiply(right) }

// Translate adds (x, y) to the translation component.
func (m *Matrix) Translate(x, y float32) {
	m.E += x
	m.F += y
}

// Scale multiplies the matrix by a scaling transform.
func (m *Matrix) Scale(sx, sy float32) {
	m.A *= sx
	m.B *= sy
	m.C *= sx
	m.D *= sy
	m.E *= sx
	m.F *= sy
}

// Rotate post-concats a rotation by fRadian.
func (m *Matrix) Rotate(fRadian float32) {
	cosValue := float32(math.Cos(float64(fRadian)))
	sinValue := float32(math.Sin(float64(fRadian)))
	m.Concat(NewMatrix(cosValue, sinValue, -sinValue, cosValue, 0, 0))
}

// GetXUnit returns the length of the transformed unit x vector.
func (m Matrix) GetXUnit() float32 {
	if m.B == 0 {
		if m.A > 0 {
			return m.A
		}
		return -m.A
	}
	if m.A == 0 {
		if m.B > 0 {
			return m.B
		}
		return -m.B
	}
	return float32(math.Hypot(float64(m.A), float64(m.B)))
}

// GetYUnit returns the length of the transformed unit y vector.
func (m Matrix) GetYUnit() float32 {
	if m.C == 0 {
		if m.D > 0 {
			return m.D
		}
		return -m.D
	}
	if m.D == 0 {
		if m.C > 0 {
			return m.C
		}
		return -m.C
	}
	return float32(math.Hypot(float64(m.C), float64(m.D)))
}

// TransformXDistance returns the transformed length of an x-axis distance.
func (m Matrix) TransformXDistance(dx float32) float32 {
	fx := m.A * dx
	fy := m.B * dx
	return float32(math.Hypot(float64(fx), float64(fy)))
}

// TransformDistance returns the average transformed length of a distance.
func (m Matrix) TransformDistance(distance float32) float32 {
	return distance * (m.GetXUnit() + m.GetYUnit()) / 2
}

// Transform maps a point through the matrix.
func (m Matrix) Transform(point PointF) PointF {
	ax := m.A * point.X
	cy := m.C * point.Y
	bx := m.B * point.X
	dy := m.D * point.Y
	return PointF{
		X: ax + cy + m.E,
		Y: bx + dy + m.F,
	}
}

// TransformRect maps a rect through the matrix, returning the axis-aligned
// bounding box of the four transformed corners.
func (m Matrix) TransformRect(rect FloatRect) FloatRect {
	points := [4]PointF{
		{rect.Left, rect.Top},
		{rect.Left, rect.Bottom},
		{rect.Right, rect.Top},
		{rect.Right, rect.Bottom},
	}
	for i := range points {
		points[i] = m.Transform(points[i])
	}
	newRight := points[0].X
	newLeft := points[0].X
	newTop := points[0].Y
	newBottom := points[0].Y
	for i := 1; i < len(points); i++ {
		newRight = maxf(newRight, points[i].X)
		newLeft = minf(newLeft, points[i].X)
		newTop = maxf(newTop, points[i].Y)
		newBottom = minf(newBottom, points[i].Y)
	}
	return NewFloatRect(newLeft, newBottom, newRight, newTop)
}

// minf/maxf are float32 helpers matching std::min/std::max semantics (NaN
// handling differs but PDFium geometry never produces NaN on these paths).
func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
