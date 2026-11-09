package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

func decodeJPEG(data []byte, d, resources *objects.Dictionary, w, h int) (*image.NRGBA, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: invalid JPEG: %w", err)
	}
	if cfg.Width != w || cfg.Height != h {
		return nil, fmt.Errorf("image: JPEG dimensions differ from dictionary")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !d.KeyExist("Decode") && d.GetArrayFor("Mask") == nil {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(x, y, decoded.At(x, y))
			}
		}
		return out, nil
	}
	cs, err := colorSpace(d.GetObjectFor("ColorSpace"), resources, make(map[objects.Object]bool))
	if err != nil {
		return nil, err
	}
	if cs.name == "indexed" {
		return nil, fmt.Errorf("image: indexed JPEG is unsupported")
	}
	samples := make([]byte, w*h*cs.components)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			offset := (y*w + x) * cs.components
			c := decoded.At(x, y)
			switch cs.name {
			case "gray":
				samples[offset] = color.GrayModel.Convert(c).(color.Gray).Y
			case "rgb":
				rgb := color.NRGBAModel.Convert(c).(color.NRGBA)
				copy(samples[offset:], []byte{rgb.R, rgb.G, rgb.B})
			case "cmyk":
				cmyk := color.CMYKModel.Convert(c).(color.CMYK)
				copy(samples[offset:], []byte{cmyk.C, cmyk.M, cmyk.Y, cmyk.K})
			}
		}
	}
	copyDict := objects.ToDictionary(d.Clone())
	copyDict.SetFor("BitsPerComponent", objects.NewNumberFromInt(8))
	return decodeSamples(samples, copyDict, resources, w, h)
}

// Standalone stencils have no intrinsic color. Export them with black paint,
// retaining the mask alpha so callers can apply another color if required.
func decodeRaw(data []byte, d, resources *objects.Dictionary, w, h int) (*image.NRGBA, error) {
	if !d.GetBooleanFor("ImageMask", false) {
		return decodeSamples(data, d, resources, w, h)
	}
	if bpc := d.GetIntegerWithDefaultFor("BitsPerComponent", 1); bpc != 1 {
		return nil, fmt.Errorf("image: stencil depth must be one")
	}
	copyDict := objects.ToDictionary(d.Clone())
	copyDict.SetNewNameFor("ColorSpace", "DeviceGray")
	copyDict.SetFor("BitsPerComponent", objects.NewNumberFromInt(1))
	im, err := decodeSamples(data, copyDict, resources, w, h)
	if err != nil {
		return nil, err
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := im.PixOffset(x, y)
			alpha := 255 - im.Pix[i]
			im.Pix[i] = 0
			im.Pix[i+1] = 0
			im.Pix[i+2] = 0
			im.Pix[i+3] = alpha
		}
	}
	return im, nil
}
