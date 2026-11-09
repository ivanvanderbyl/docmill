package image

import (
	"encoding/binary"
	"image/color"
	"math"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestICCMatrixProfile(t *testing.T) {
	// A D50-adapted sRGB matrix; gamma affects interiors, not these primaries.
	matrix := [][3]float64{{.4360747, .2225045, .0139322}, {.3850649, .7168786, .0971045}, {.1430804, .0606169, .7141733}}
	profile := make([]byte, 132+6*12)
	copy(profile[16:], "RGB ")
	copy(profile[20:], "XYZ ")
	copy(profile[36:], "acsp")
	binary.BigEndian.PutUint32(profile[128:], 6)
	put := func(i int, name string, data []byte) {
		p := 132 + i*12
		copy(profile[p:], name)
		binary.BigEndian.PutUint32(profile[p+4:], uint32(len(profile)))
		binary.BigEndian.PutUint32(profile[p+8:], uint32(len(data)))
		profile = append(profile, data...)
	}
	for i, name := range []string{"r", "g", "b"} {
		xyz := make([]byte, 20)
		copy(xyz, "XYZ ")
		for j, v := range matrix[i] {
			binary.BigEndian.PutUint32(xyz[8+j*4:], uint32(math.Round(v*65536)))
		}
		put(i*2, name+"XYZ", xyz)
		curve := make([]byte, 14)
		copy(curve, "curv")
		binary.BigEndian.PutUint32(curve[8:], 1)
		binary.BigEndian.PutUint16(curve[12:], 563)
		put(i*2+1, name+"TRC", curve)
	}
	binary.BigEndian.PutUint32(profile, uint32(len(profile)))
	cs, err := iccSpace(objects.NewStreamFromSpan(profile))
	require.NoError(t, err)
	for _, tc := range []struct {
		v    []float64
		want color.NRGBA
	}{{[]float64{1, 0, 0}, color.NRGBA{255, 0, 0, 255}}, {[]float64{0, 1, 0}, color.NRGBA{0, 255, 0, 255}}, {[]float64{0, 0, 1}, color.NRGBA{0, 0, 255, 255}}, {[]float64{1, 1, 1}, color.NRGBA{255, 255, 255, 255}}} {
		got := cs.rgb(tc.v)
		require.InDelta(t, tc.want.R, got.R, 1)
		require.InDelta(t, tc.want.G, got.G, 1)
		require.InDelta(t, tc.want.B, got.B, 1)
	}
	_, err = iccSpace(objects.NewStreamFromSpan(profile[:134]))
	require.Error(t, err)
}
func TestICCParametricCurve(t *testing.T) {
	b := make([]byte, 16)
	copy(b, "para")
	binary.BigEndian.PutUint32(b[12:], 2*65536)
	curve, err := iccCurve(b)
	require.NoError(t, err)
	require.InDelta(t, .25, curve(.5), .000001)
	_, err = iccCurve([]byte("bad"))
	require.Error(t, err)
}
