package parser_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func encodedTestImage(t *testing.T) parser.ExtractedImage {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128})
		}
	}
	source.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 0})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, source))
	return parser.ExtractedImage{Data: b.Bytes(), Format: "png", Width: 16, Height: 16}
}

func TestExtractedImagePNGPreservesPixelsAndAlpha(t *testing.T) {
	extracted := encodedTestImage(t)
	decoded, err := extracted.Decode()
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{R: 255, A: 128}, color.NRGBAModel.Convert(decoded.At(8, 8)))
	data, err := extracted.PNG()
	require.NoError(t, err)
	output, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 16, 16), output.Bounds())
	require.Equal(t, color.NRGBA{R: 255, A: 128}, color.NRGBAModel.Convert(output.At(8, 8)))
	_, _, _, a := output.At(0, 0).RGBA()
	require.Zero(t, a)
}

func TestExtractedImageJPEGCompositesTransparencyOnWhite(t *testing.T) {
	extracted := encodedTestImage(t)
	data, err := extracted.JPEG(&jpeg.Options{Quality: 100})
	require.NoError(t, err)
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	pixel := color.NRGBAModel.Convert(decoded.At(8, 8)).(color.NRGBA)
	require.InDelta(t, 255, pixel.R, 3)
	require.InDelta(t, 127, pixel.G, 3)
	require.InDelta(t, 127, pixel.B, 3)
	require.Equal(t, uint8(255), pixel.A)
	// A JPEG output remains valid input to the PNG conversion API.
	converted, err := (parser.ExtractedImage{Data: data}).PNG()
	require.NoError(t, err)
	_, err = png.Decode(bytes.NewReader(converted))
	require.NoError(t, err)
	_, err = extracted.JPEG(nil)
	require.NoError(t, err)
}

func TestExtractedImageEncodingRejectsCorruptInput(t *testing.T) {
	extracted := parser.ExtractedImage{Data: []byte("invalid"), Format: "png"}
	_, err := extracted.Decode()
	require.Error(t, err)
	data, err := extracted.PNG()
	require.Error(t, err)
	require.Nil(t, data)
	data, err = extracted.JPEG(nil)
	require.Error(t, err)
	require.Nil(t, data)
}
