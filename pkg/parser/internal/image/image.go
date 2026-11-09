// Package image decodes image XObjects to portable pixels. Sample extraction
// follows CPDF_DIB::GetBits8 and LoadColorInfo (PDFium cpdf_dib.cpp,
// blob 792862dec45b0b8f39fb9e7954dcdc1282918653).
package image

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// Decode returns unassociated RGBA pixels; JPEG is also returned unchanged
// when no PDF decode array or transparency changes its interpretation.
func Decode(stream *objects.Stream, resources *objects.Dictionary) (*image.NRGBA, []byte, error) {
	return decode(stream, resources, make(map[*objects.Stream]bool))
}

func decode(stream *objects.Stream, resources *objects.Dictionary, seen map[*objects.Stream]bool) (*image.NRGBA, []byte, error) {
	if stream == nil || seen[stream] || len(seen) >= 64 {
		return nil, nil, fmt.Errorf("image: missing or cyclic image stream")
	}
	seen[stream] = true
	defer delete(seen, stream)
	d := stream.Dict()
	w, h := d.GetIntegerFor("Width"), d.GetIntegerFor("Height")
	// Limit allocation before decoding data controlled by the PDF.
	if w <= 0 || h <= 0 || w > 131071 || h > 131071 || uint64(w)*uint64(h) > 25000000 {
		return nil, nil, fmt.Errorf("image: invalid or excessive dimensions %dx%d", w, h)
	}
	data, terminal, err := objects.DecodeImageData(stream)
	if err != nil {
		return nil, nil, err
	}
	pixels, original, err := decodeImageData(data, terminal, d, resources, w, h)
	if err != nil {
		return nil, nil, err
	}

	if mask := d.GetStreamFor("SMask"); mask != nil {
		if err := applySoftMask(pixels, mask, d, resources, seen); err != nil {
			return nil, nil, err
		}
		original = nil
	} else if mask := d.GetStreamFor("Mask"); mask != nil {
		if err := applyStencilMask(pixels, mask, resources, seen); err != nil {
			return nil, nil, err
		}
		original = nil
	}
	return pixels, original, nil
}

func decodeImageData(data []byte, terminal string, d, resources *objects.Dictionary, w, h int) (*image.NRGBA, []byte, error) {
	var pixels *image.NRGBA
	var original []byte
	var err error
	if terminal == "CCITTFaxDecode" || terminal == "CCF" {
		data, err = decodeFax(data, d, w, h)
		if err != nil {
			return nil, nil, err
		}
		copyDict := objects.ToDictionary(d.Clone())
		copyDict.SetNewNameFor("ColorSpace", "DeviceGray")
		copyDict.SetFor("BitsPerComponent", objects.NewNumberFromInt(1))
		pixels, err = decodeRaw(data, copyDict, resources, w, h)
		if err != nil {
			return nil, nil, err
		}
	} else if terminal != "" {
		if terminal != "DCTDecode" && terminal != "DCT" {
			return nil, nil, fmt.Errorf("image: unsupported terminal codec %s", terminal)
		}
		pixels, err = decodeJPEG(data, d, resources, w, h)
		if err != nil {
			return nil, nil, err
		}
		if !d.KeyExist("Decode") && !d.KeyExist("Mask") {
			cs, e := colorSpace(d.GetObjectFor("ColorSpace"), resources, make(map[objects.Object]bool))
			if e == nil && cs.name != "icc" {
				original = data
			}
		}
	} else {
		pixels, err = decodeRaw(data, d, resources, w, h)
		if err != nil {
			return nil, nil, err
		}
	}
	return pixels, original, nil
}

func applySoftMask(pixels *image.NRGBA, mask *objects.Stream, d, resources *objects.Dictionary, seen map[*objects.Stream]bool) error {
	alpha, _, err := decode(mask, resources, seen)
	if err != nil {
		return err
	}
	if alpha.Bounds() != pixels.Bounds() {
		return fmt.Errorf("image: soft-mask dimensions differ")
	}
	for y := range pixels.Bounds().Dy() {
		for x := range pixels.Bounds().Dx() {
			i := pixels.PixOffset(x, y)
			pixels.Pix[i+3] = color.GrayModel.Convert(alpha.At(x, y)).(color.Gray).Y
		}
	}
	if matte := mask.Dict().GetArrayFor("Matte"); matte != nil {
		return removeMatte(pixels, matte, d, resources)
	}
	return nil
}

func applyStencilMask(pixels *image.NRGBA, mask *objects.Stream, resources *objects.Dictionary, seen map[*objects.Stream]bool) error {
	if !mask.Dict().GetBooleanFor("ImageMask", false) {
		return fmt.Errorf("image: explicit mask is not a stencil")
	}
	alpha, _, err := decode(mask, resources, seen)
	if err != nil {
		return err
	}
	if alpha.Bounds() != pixels.Bounds() {
		return fmt.Errorf("image: mask dimensions differ")
	}
	for y := range pixels.Bounds().Dy() {
		for x := range pixels.Bounds().Dx() {
			pixels.Pix[pixels.PixOffset(x, y)+3] = alpha.NRGBAAt(x, y).A
		}
	}
	return nil
}

type space struct {
	name       string
	components int
	palette    []byte
	base       *space
	high       int
	transform  func([]float64) color.NRGBA
}

func colorSpace(obj objects.Object, resources *objects.Dictionary, seen map[objects.Object]bool) (*space, error) {
	obj = objects.Direct(obj)
	if obj == nil || seen[obj] || len(seen) >= 64 {
		return nil, fmt.Errorf("image: missing or cyclic color space")
	}
	seen[obj] = true
	defer delete(seen, obj)
	if n := objects.ToName(obj); n != nil {
		if cs := deviceSpace(n.GetString()); cs != nil {
			return cs, nil
		}
		if resources != nil {
			if cs := resources.GetDictFor("ColorSpace"); cs != nil {
				return colorSpace(cs.GetObjectFor(n.GetString()), resources, seen)
			}
		}
	}
	if a := objects.ToArray(obj); a != nil && a.Len() == 2 && a.GetByteStringAt(0) == "ICCBased" {
		profile := objects.ToStream(a.GetDirectObjectAt(1))
		if profile == nil {
			return nil, fmt.Errorf("image: invalid ICCBased profile")
		}
		return iccSpace(profile)
	}
	if a := objects.ToArray(obj); a != nil && a.Len() >= 4 && a.GetByteStringAt(0) == "Indexed" {
		return indexedSpace(a, resources, seen)
	}
	return nil, fmt.Errorf("image: unsupported color space")
}

func deviceSpace(name string) *space {
	switch name {
	case "DeviceGray", "G":
		return &space{name: "gray", components: 1}
	case "DeviceRGB", "RGB":
		return &space{name: "rgb", components: 3}
	case "DeviceCMYK", "CMYK":
		return &space{name: "cmyk", components: 4}
	}
	return nil
}

func indexedSpace(a *objects.Array, resources *objects.Dictionary, seen map[objects.Object]bool) (*space, error) {
	base, e := colorSpace(a.GetObjectAt(1), resources, seen)
	if e != nil {
		return nil, e
	}
	high := a.GetIntegerAt(2)
	if high < 0 || high > 255 || base.name == "indexed" {
		return nil, fmt.Errorf("image: invalid indexed color space")
	}
	lookup := a.GetDirectObjectAt(3)
	var palette []byte
	if s := objects.ToString(lookup); s != nil {
		palette = []byte(s.GetString())
	} else if s := objects.ToStream(lookup); s != nil {
		var terminal string
		palette, terminal, e = objects.DecodeImageData(s)
		if e != nil || terminal != "" {
			return nil, fmt.Errorf("image: invalid palette stream")
		}
	}
	if len(palette) < (high+1)*base.components {
		return nil, fmt.Errorf("image: truncated palette")
	}
	return &space{name: "indexed", components: 1, palette: palette, base: base, high: high}, nil
}

func decodeSamples(data []byte, d, resources *objects.Dictionary, w, h int) (*image.NRGBA, error) {
	cs, err := colorSpace(d.GetObjectFor("ColorSpace"), resources, make(map[objects.Object]bool))
	if err != nil {
		return nil, err
	}
	bpc := d.GetIntegerFor("BitsPerComponent")
	if bpc != 1 && bpc != 2 && bpc != 4 && bpc != 8 && bpc != 16 {
		return nil, fmt.Errorf("image: invalid bits per component %d", bpc)
	}
	row := (w*cs.components*bpc + 7) / 8
	if len(data) < row*h {
		return nil, fmt.Errorf("image: truncated samples: %d bytes, need %d", len(data), row*h)
	}
	maximum := float64((uint32(1) << bpc) - 1)
	ranges, err := sampleRanges(d, cs, maximum)
	if err != nil {
		return nil, err
	}
	mask := d.GetArrayFor("Mask")
	if mask != nil && mask.Len() != cs.components*2 {
		return nil, fmt.Errorf("image: invalid color-key mask")
	}
	return renderSamples(data, cs, ranges, mask, w, h, row, bpc, maximum), nil
}

func sampleRanges(d *objects.Dictionary, cs *space, maximum float64) ([]float64, error) {
	ranges := make([]float64, cs.components*2)
	for i := 0; i < cs.components; i++ {
		ranges[2*i+1] = 1
		if cs.name == "indexed" {
			ranges[2*i+1] = maximum
		}
	}
	if a := d.GetArrayFor("Decode"); a != nil {
		if a.Len() != len(ranges) {
			return nil, fmt.Errorf("image: invalid decode array")
		}
		for i := range ranges {
			v := objects.ToNumber(a.GetDirectObjectAt(i))
			if v == nil {
				return nil, fmt.Errorf("image: nonnumeric decode array")
			}
			ranges[i] = float64(v.GetNumber())
			if math.IsNaN(ranges[i]) || math.IsInf(ranges[i], 0) {
				return nil, fmt.Errorf("image: nonfinite decode range")
			}
		}
	}
	return ranges, nil
}

func renderSamples(data []byte, cs *space, ranges []float64, mask *objects.Array, w, h, row, bpc int, maximum float64) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	values := make([]float64, cs.components)
	for y := range h {
		for x := range w {
			transparent := mask != nil
			for c := 0; c < cs.components; c++ {
				sample := bits(data[y*row:], (x*cs.components+c)*bpc, bpc)
				values[c] = ranges[2*c] + float64(sample)/maximum*(ranges[2*c+1]-ranges[2*c])
				if mask != nil && (sample < mask.GetIntegerAt(2*c) || sample > mask.GetIntegerAt(2*c+1)) {
					transparent = false
				}
			}
			pixel := cs.rgb(values)
			if transparent {
				pixel.A = 0
			}
			out.SetNRGBA(x, y, pixel)
		}
	}
	return out
}

func bits(data []byte, pos, n int) int {
	if n == 16 {
		return int(data[pos/8])<<8 | int(data[pos/8+1])
	}
	return int(data[pos/8]>>uint(8-n-pos%8)) & ((1 << n) - 1)
}

func unit(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

func (s *space) rgb(v []float64) color.NRGBA {
	if s.transform != nil {
		return s.transform(v)
	}
	switch s.name {
	case "gray":
		g := unit(v[0])
		return color.NRGBA{g, g, g, 255}
	case "rgb":
		return color.NRGBA{unit(v[0]), unit(v[1]), unit(v[2]), 255}
	case "cmyk":
		r, g, b := color.CMYKToRGB(unit(v[0]), unit(v[1]), unit(v[2]), unit(v[3]))
		return color.NRGBA{r, g, b, 255}
	case "indexed":
		index := int(math.Round(v[0]))
		index = max(0, min(s.high, index))
		base := make([]float64, s.base.components)
		for i := range base {
			base[i] = float64(s.palette[index*s.base.components+i]) / 255
		}
		return s.base.rgb(base)
	}
	return color.NRGBA{}
}
