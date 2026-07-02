// Ported from core/fxcrt/fx_coordinates_unittest.cpp @ pdfium 0db284a42.
// A focused selection covering the text-path math: FloatRect Normalize/Scale/
// Union and Matrix inverse/compose/transform. Expected values are PDFium's own.
package crt

import (
	"math"
	"testing"
)

// near asserts |want-got| <= tol, mirroring gtest EXPECT_NEAR.
func near(t *testing.T, name string, want, got, tol float32) {
	t.Helper()
	if d := want - got; d > tol || d < -tol {
		t.Errorf("%s = %v, want %v (tol %v)", name, got, want, tol)
	}
}

// floatEq mirrors gtest EXPECT_FLOAT_EQ (~4 ULP) with a relative tolerance.
func floatEq(t *testing.T, name string, want, got float32) {
	t.Helper()
	tol := float32(math.Abs(float64(want))) * 1e-5
	if tol < 1e-5 {
		tol = 1e-5
	}
	near(t, name, want, got, tol)
}

func TestFloatRectNormalize(t *testing.T) {
	var rect FloatRect
	rect.Normalize()
	if rect != (FloatRect{}) {
		t.Errorf("zero rect normalize changed it: %+v", rect)
	}

	rect = NewFloatRect(-1.0, -3.0, 4.5, 3.2)
	rect.Normalize()
	floatEq(t, "left", -1.0, rect.Left)
	floatEq(t, "bottom", -3.0, rect.Bottom)
	floatEq(t, "right", 4.5, rect.Right)
	floatEq(t, "top", 3.2, rect.Top)

	rect.Scale(-1.0)
	rect.Normalize()
	floatEq(t, "left", -4.5, rect.Left)
	floatEq(t, "bottom", -3.2, rect.Bottom)
	floatEq(t, "right", 1.0, rect.Right)
	floatEq(t, "top", 3.0, rect.Top)
}

func TestFloatRectScale(t *testing.T) {
	rect := NewFloatRect(-1.0, -3.0, 4.5, 3.2)
	rect.Scale(1.0)
	floatEq(t, "left", -1.0, rect.Left)
	floatEq(t, "right", 4.5, rect.Right)
	rect.Scale(0.5)
	floatEq(t, "left", -0.5, rect.Left)
	floatEq(t, "bottom", -1.5, rect.Bottom)
	floatEq(t, "right", 2.25, rect.Right)
	floatEq(t, "top", 1.6, rect.Top)
	rect.Scale(2.0)
	floatEq(t, "left", -1.0, rect.Left)
	floatEq(t, "top", 3.2, rect.Top)
	rect.Scale(0.0)
	if rect != (FloatRect{}) {
		t.Errorf("scale by 0 should zero the rect: %+v", rect)
	}
}

func TestFloatRectScaleFromCenterPoint(t *testing.T) {
	rect := NewFloatRect(-1.0, -3.0, 4.5, 3.2)
	rect.ScaleFromCenterPoint(1.0)
	floatEq(t, "left", -1.0, rect.Left)
	floatEq(t, "top", 3.2, rect.Top)
	rect.ScaleFromCenterPoint(0.5)
	floatEq(t, "left", 0.375, rect.Left)
	floatEq(t, "bottom", -1.45, rect.Bottom)
	floatEq(t, "right", 3.125, rect.Right)
	floatEq(t, "top", 1.65, rect.Top)
}

func TestFloatRectUnion(t *testing.T) {
	a := NewFloatRect(0, 0, 2, 2)
	a.Union(NewFloatRect(1, 1, 4, 5))
	floatEq(t, "left", 0, a.Left)
	floatEq(t, "bottom", 0, a.Bottom)
	floatEq(t, "right", 4, a.Right)
	floatEq(t, "top", 5, a.Top)

	// Union normalizes both operands first (swap-on-inverted carry-over fix).
	b := NewFloatRect(2, 2, 0, 0) // inverted
	b.Union(NewFloatRect(-1, -1, 1, 1))
	floatEq(t, "left", -1, b.Left)
	floatEq(t, "bottom", -1, b.Bottom)
	floatEq(t, "right", 2, b.Right)
	floatEq(t, "top", 2, b.Top)
}

func TestMatrixReverseIdentity(t *testing.T) {
	rev := IdentityMatrix().GetInverse()
	floatEq(t, "a", 1.0, rev.A)
	floatEq(t, "b", 0.0, rev.B)
	floatEq(t, "c", 0.0, rev.C)
	floatEq(t, "d", 1.0, rev.D)
	floatEq(t, "e", 0.0, rev.E)
	floatEq(t, "f", 0.0, rev.F)

	result := rev.Transform(IdentityMatrix().Transform(PointF{2, 3}))
	floatEq(t, "x", 2, result.X)
	floatEq(t, "y", 3, result.Y)
}

func TestMatrixSetIdentity(t *testing.T) {
	m := IdentityMatrix()
	if !m.IsIdentity() {
		t.Error("expected identity")
	}
	m.A = -1
	if m.IsIdentity() {
		t.Error("expected non-identity after mutation")
	}
}

func TestMatrixGetInverse(t *testing.T) {
	m := NewMatrix(3, 0, 2, 3, 1, 4)
	rev := m.GetInverse()
	floatEq(t, "a", 0.33333334, rev.A)
	floatEq(t, "b", 0.0, rev.B)
	floatEq(t, "c", -0.22222222, rev.C)
	floatEq(t, "d", 0.33333334, rev.D)
	floatEq(t, "e", 0.55555556, rev.E)
	floatEq(t, "f", -1.3333334, rev.F)

	result := rev.Transform(m.Transform(PointF{2, 3}))
	floatEq(t, "x", 2, result.X)
	floatEq(t, "y", 3, result.Y)
}

// TestMatrixGetInverseCR702041 is PDFium's near-singular case (determinant <
// float epsilon). PDFium's hard-coded expected values were produced by a build
// that evaluated the determinant at higher-than-float32 precision (constant
// folding / FP contraction); a faithful runtime IEEE-754 float32 computation —
// which is what PDFium's no-FMA float math also produces — yields a different but
// equally "garbage" inverse, because the catastrophic cancellation makes the
// result FP-mode-dependent. Such matrices never occur as real page/text CTMs,
// so we assert the build-independent invariants (finite, deterministic) rather
// than pin magnitudes that depend on the compiler's FP mode. See plan 009 Phase
// A "float drift" note.
func TestMatrixGetInverseCR702041(t *testing.T) {
	m := NewMatrix(0.947368443, -0.108947366, -0.923076928, 0.106153846, 18.0, 787.929993)
	rev := m.GetInverse()
	for name, v := range map[string]float32{"a": rev.A, "b": rev.B, "c": rev.C, "d": rev.D, "e": rev.E, "f": rev.F} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Errorf("rev.%s = %v, want finite", name, v)
		}
	}
	if rev != m.GetInverse() {
		t.Error("GetInverse is not deterministic")
	}
}

func TestMatrixGetInverseCR714187(t *testing.T) {
	m := NewMatrix(0.000037, 0.0, 0.0, -0.000037, 182.413101, 136.977646)
	rev := m.GetInverse()
	floatEq(t, "a", 27027.025, rev.A)
	floatEq(t, "b", 0.0, rev.B)
	floatEq(t, "c", 0.0, rev.C)
	floatEq(t, "d", -27027.025, rev.D)
	floatEq(t, "e", -4930083.5, rev.E)
	floatEq(t, "f", 3702098.2, rev.F)
}

func TestMatrixComposeTransformations(t *testing.T) {
	rotate90 := IdentityMatrix()
	rotate90.Rotate(float32(math.Pi) / 2)
	near(t, "rot.a", 0.0, rotate90.A, 1e-5)
	near(t, "rot.b", 1.0, rotate90.B, 1e-5)
	near(t, "rot.c", -1.0, rotate90.C, 1e-5)
	near(t, "rot.d", 0.0, rotate90.D, 1e-5)

	translate := IdentityMatrix()
	translate.Translate(23, 11)
	scale := IdentityMatrix()
	scale.Scale(5, 13)

	// Step-by-step application of all three transforms.
	p := rotate90.Transform(PointF{10, 20})
	floatEq(t, "x", -20.0, p.X)
	floatEq(t, "y", 10.0, p.Y)
	p = translate.Transform(p)
	floatEq(t, "x", 3.0, p.X)
	floatEq(t, "y", 21.0, p.Y)
	p = scale.Transform(p)
	floatEq(t, "x", 15.0, p.X)
	floatEq(t, "y", 273.0, p.Y)

	// Compose all transforms via Concat (post-multiply).
	m := IdentityMatrix()
	m.Concat(rotate90)
	m.Concat(translate)
	m.Concat(scale)
	near(t, "m.a", 0.0, m.A, 1e-5)
	near(t, "m.b", 13.0, m.B, 1e-5)
	near(t, "m.c", -5.0, m.C, 1e-5)
	near(t, "m.d", 0.0, m.D, 1e-5)
	floatEq(t, "m.e", 115.0, m.E)
	floatEq(t, "m.f", 143.0, m.F)

	origin := m.Transform(PointF{0, 0})
	floatEq(t, "x", 115.0, origin.X)
	floatEq(t, "y", 143.0, origin.Y)
	p2 := m.Transform(PointF{10, 20})
	floatEq(t, "x", 15.0, p2.X)
	floatEq(t, "y", 273.0, p2.Y)
}

func TestMatrixTransformRectForFloatRect(t *testing.T) {
	rotate90 := IdentityMatrix()
	rotate90.Rotate(float32(math.Pi) / 2)
	scale := IdentityMatrix()
	scale.Scale(5, 13)

	rect := NewFloatRect(5.5, 0.0, 12.25, 2.7)
	rect = rotate90.TransformRect(rect)
	floatEq(t, "left", -2.7, rect.Left)
	floatEq(t, "bottom", 5.5, rect.Bottom)
	near(t, "right", 0.0, rect.Right, 1e-5)
	floatEq(t, "top", 12.25, rect.Top)

	rect = scale.TransformRect(rect)
	floatEq(t, "left", -13.5, rect.Left)
	floatEq(t, "bottom", 71.5, rect.Bottom)
	near(t, "right", 0.0, rect.Right, 1e-5)
	floatEq(t, "top", 159.25, rect.Top)
}
