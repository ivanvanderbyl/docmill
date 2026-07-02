package geom_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/stretchr/testify/require"
)

func TestBoxOriginConversionMatchesDoclingCore(t *testing.T) {
	t.Parallel()

	box := geom.Box{L: 10, T: 20, R: 60, B: 90, Origin: geom.TopLeft}

	converted := box.WithOrigin(geom.BottomLeft, 200)
	require.Equal(t, geom.Box{L: 10, T: 180, R: 60, B: 110, Origin: geom.BottomLeft}, converted)
	require.Equal(t, []float64{10, 110, 60, 180}, converted.AsTuple())

	roundTrip := converted.WithOrigin(geom.TopLeft, 200)
	require.Equal(t, box, roundTrip)
	require.Equal(t, []float64{10, 20, 60, 90}, roundTrip.AsTuple())
}

func TestBoxIntersectionMetricsMatchDoclingCore(t *testing.T) {
	t.Parallel()

	a := geom.Box{L: 0, T: 0, R: 100, B: 100, Origin: geom.TopLeft}
	b := geom.Box{L: 50, T: 25, R: 150, B: 75, Origin: geom.TopLeft}

	require.InDelta(t, 2500.0, a.IntersectionArea(b), 0.0001)
	require.InDelta(t, 0.2, a.IoU(b), 0.0001)
	require.InDelta(t, 0.25, a.IntersectionOverSelf(b), 0.0001)
	require.InDelta(t, 0.5, b.IntersectionOverSelf(a), 0.0001)
}

func TestBoxScaleAndEnclosurePreserveOrigin(t *testing.T) {
	t.Parallel()

	a := geom.Box{L: 10, T: 20, R: 30, B: 50, Origin: geom.TopLeft}
	b := geom.Box{L: 5, T: 25, R: 40, B: 60, Origin: geom.TopLeft}

	require.Equal(t, geom.Box{L: 20, T: 40, R: 60, B: 100, Origin: geom.TopLeft}, a.Scaled(2, 2))
	require.Equal(t, geom.Box{L: 5, T: 20, R: 40, B: 60, Origin: geom.TopLeft}, geom.EnclosingBox(a, b))
}

func TestBoxNormalisesTupleByOrigin(t *testing.T) {
	t.Parallel()

	topLeft := geom.BoxFromTuple(10, 20, 60, 90, geom.TopLeft)
	require.Equal(t, geom.Box{L: 10, T: 20, R: 60, B: 90, Origin: geom.TopLeft}, topLeft)

	bottomLeft := geom.BoxFromTuple(10, 20, 60, 90, geom.BottomLeft)
	require.Equal(t, geom.Box{L: 10, T: 90, R: 60, B: 20, Origin: geom.BottomLeft}, bottomLeft)
	require.Equal(t, []float64{10, 20, 60, 90}, bottomLeft.AsTuple())
}
