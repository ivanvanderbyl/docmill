package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func imagePDF(bodies ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(bodies))
	for i, body := range bodies {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(bodies)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer << /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, xref)
	return b.Bytes()
}
func imageStream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}
func TestExtractImagesInheritedNestedAndInline(t *testing.T) {
	pdf := imagePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /Direct 4 0 R /Again 4 0 R /Nested 5 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 6 0 R >>",
		imageStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB", string([]byte{255, 0, 0})),
		imageStream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Cycle 5 0 R /Second 7 0 R >> >>", ""),
		imageStream("", "BI /W 1 /H 1 /CS /RGB /BPC 8 ID "+string([]byte{0, 0, 255})+" EI"),
		imageStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", string([]byte{127})),
	)
	d, err := parser.NewBackend().OpenBytes(context.Background(), pdf)
	require.NoError(t, err)
	native := d.(*parser.Document)
	images, err := native.ExtractImages(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, images, 3)
	for i, want := range []color.NRGBA{{255, 0, 0, 255}, {127, 127, 127, 255}, {0, 0, 255, 255}} {
		im, format, err := image.Decode(bytes.NewReader(images[i].Data))
		require.NoError(t, err)
		require.Equal(t, "png", format)
		require.Equal(t, want, color.NRGBAModel.Convert(im.At(0, 0)))
		require.Equal(t, 0, images[i].PageIndex)
	}
	images, err = native.ExtractImages(context.Background(), []int{})
	require.NoError(t, err)
	require.Empty(t, images)
	_, err = native.ExtractImages(context.Background(), []int{-1})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = native.ExtractImages(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
}
func TestExtractImagesFailsOnUnsupportedImage(t *testing.T) {
	pdf := imagePDF("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /Resources << /XObject << /I 4 0 R >> >> >>", imageStream("/Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Filter /JBIG2Decode", "garbage"))
	d, err := parser.NewBackend().OpenBytes(context.Background(), pdf)
	require.NoError(t, err)
	_, err = d.(*parser.Document).ExtractImages(context.Background(), nil)
	require.ErrorContains(t, err, "unsupported terminal codec JBIG2Decode")
}

func TestExtractImagesCyclicContents(t *testing.T) {
	pdf := imagePDF("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>", "[4 0 R]")
	d, err := parser.NewBackend().OpenBytes(context.Background(), pdf)
	require.NoError(t, err)
	_, err = d.(*parser.Document).ExtractImages(context.Background(), nil)
	require.ErrorContains(t, err, "cyclic content")
	var empty parser.Document
	_, err = empty.ExtractImages(context.Background(), nil)
	require.ErrorContains(t, err, "nil document")
}
