package image

import (
	"encoding/binary"
	"fmt"
	"image/color"
	"math"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// ICC matrix/TRC profiles transform through their D50 profile connection space.
// LUT-based profiles require a color-management engine and fail explicitly.
func iccSpace(profile *objects.Stream) (*space, error) {
	data, terminal, err := objects.DecodeImageData(profile)
	if err != nil || terminal != "" {
		return nil, fmt.Errorf("image: invalid ICC stream")
	}
	if len(data) < 132 || string(data[36:40]) != "acsp" || string(data[16:20]) != "RGB " || string(data[20:24]) != "XYZ " {
		return nil, fmt.Errorf("image: unsupported ICC profile")
	}
	count := int(binary.BigEndian.Uint32(data[128:132]))
	if count > (len(data)-132)/12 {
		return nil, fmt.Errorf("image: truncated ICC table")
	}
	tags := map[string][]byte{}
	for i := range count {
		p := 132 + i*12
		off := uint64(binary.BigEndian.Uint32(data[p+4:]))
		size := uint64(binary.BigEndian.Uint32(data[p+8:]))
		if off+size > uint64(len(data)) {
			return nil, fmt.Errorf("image: truncated ICC tag")
		}
		tags[string(data[p:p+4])] = data[off : off+size]
	}
	var matrix [3][3]float64
	var curves [3]func(float64) float64
	for c, name := range []string{"r", "g", "b"} {
		xyz := tags[name+"XYZ"]
		if len(xyz) < 20 || string(xyz[:4]) != "XYZ " {
			return nil, fmt.Errorf("image: unsupported ICC LUT profile")
		}
		for row := range 3 {
			matrix[row][c] = fixed(xyz[8+4*row:])
		}
		curves[c], err = iccCurve(tags[name+"TRC"])
		if err != nil {
			return nil, err
		}
	}
	return &space{name: "icc", components: 3, transform: func(v []float64) color.NRGBA {
		var xyz [3]float64
		for c := range 3 {
			linear := curves[c](math.Max(0, math.Min(1, v[c])))
			for row := range 3 {
				xyz[row] += matrix[row][c] * linear
			}
		}
		// Bradford-adapt D50 XYZ to D65, then convert to linear sRGB.
		x := 0.9555766*xyz[0] - 0.0230393*xyz[1] + 0.0631636*xyz[2]
		y := -0.0282895*xyz[0] + 1.0099416*xyz[1] + 0.0210077*xyz[2]
		z := 0.0122982*xyz[0] - 0.0204830*xyz[1] + 1.3299098*xyz[2]
		return color.NRGBA{unit(srgbGamma(3.2404542*x - 1.5371385*y - 0.4985314*z)), unit(srgbGamma(-0.969266*x + 1.8760108*y + 0.041556*z)), unit(srgbGamma(0.0556434*x - 0.2040259*y + 1.0572252*z)), 255}
	}}, nil
}
func fixed(b []byte) float64 { return float64(int32(binary.BigEndian.Uint32(b))) / 65536 }
func srgbGamma(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}
func iccCurve(b []byte) (func(float64) float64, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("image: missing ICC tone curve")
	}
	switch string(b[:4]) {
	case "curv":
		n := int(binary.BigEndian.Uint32(b[8:]))
		if n > (len(b)-12)/2 {
			return nil, fmt.Errorf("image: truncated ICC curve")
		}
		if n == 0 {
			return func(v float64) float64 { return v }, nil
		}
		if n == 1 {
			g := float64(binary.BigEndian.Uint16(b[12:])) / 256
			return func(v float64) float64 { return math.Pow(v, g) }, nil
		}
		return func(v float64) float64 {
			p := v * float64(n-1)
			lo := min(int(p), n-1)
			hi := min(lo+1, n-1)
			a := float64(binary.BigEndian.Uint16(b[12+2*lo:])) / 65535
			c := float64(binary.BigEndian.Uint16(b[12+2*hi:])) / 65535
			return a + (c-a)*(p-float64(lo))
		}, nil
	case "para":
		kind := int(binary.BigEndian.Uint16(b[8:]))
		sizes := []int{1, 3, 4, 5, 7}
		if kind >= len(sizes) || len(b) < 12+4*sizes[kind] {
			return nil, fmt.Errorf("image: invalid ICC parametric curve")
		}
		p := make([]float64, 7)
		for i := 0; i < sizes[kind]; i++ {
			p[i] = fixed(b[12+4*i:])
		}
		if kind > 0 && p[1] == 0 {
			return nil, fmt.Errorf("image: invalid ICC curve slope")
		}
		return func(v float64) float64 {
			switch kind {
			case 0:
				return math.Pow(v, p[0])
			case 1:
				if v < -p[2]/p[1] {
					return 0
				}
				return math.Pow(p[1]*v+p[2], p[0])
			case 2:
				if v < -p[2]/p[1] {
					return p[3]
				}
				return math.Pow(p[1]*v+p[2], p[0]) + p[3]
			case 3:
				if v < p[4] {
					return p[3] * v
				}
				return math.Pow(p[1]*v+p[2], p[0])
			default:
				if v < p[4] {
					return p[3]*v + p[6]
				}
				return math.Pow(p[1]*v+p[2], p[0]) + p[5]
			}
		}, nil
	}
	return nil, fmt.Errorf("image: unsupported ICC tone curve")
}
