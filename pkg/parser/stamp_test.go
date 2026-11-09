package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func stampFixture(t *testing.T, rotation int, crop bool, signed bool) []byte {
	t.Helper()
	box := ""
	if crop {
		box = "/CropBox [20 30 180 280]"
	}
	signature := ""
	field := "<< /Type /Annot /Subtype /Widget /FT /Tx /T (full-name) /V (Ada) /Rect [170 30 200 50] /P 3 0 R >>"
	if signed {
		signature = "/Perms << /DocMDP 14 0 R >>"
		field = "<< /Type /Annot /Subtype /Widget /FT /Sig /T (signature) /V 14 0 R /Rect [170 30 200 50] /P 3 0 R >>"
	}
	text := "BT /F1 12 Tf 30 260 Td (Existing text) Tj ET\n2 0 0 2 50 60 cm % trailing comment"
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R] >> /Names << /EmbeddedFiles << /Names [(note) 8 0 R] >> >> " + signature + " >>",
		fmt.Sprintf("<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /MediaBox [10 20 210 320] /Resources 5 0 R /Rotate %d %s >>", rotation, box),
		"<< /Type /Page /Parent 2 0 R /Contents 6 0 R /Annots [7 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /Contents 9 0 R >>",
		"<< /Font << /F1 10 0 R >> /XObject << /DMStamp1 11 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
		field,
		"<< /Type /Filespec /F (note.txt) /EF << /F 12 0 R >> >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 3 >>\nstream\n\xff\x00\x00\nendstream",
		"<< /Type /EmbeddedFile /Length 10 >>\nstream\nattachment\nendstream",
	}
	bodies = append(bodies, "<< /Title (Kept document title) >>")
	if signed {
		bodies = append(bodies, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 10 20 30] /Contents <01020304> >>")
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(bodies))
	for i, body := range bodies {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(bodies)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer << /Size %d /Root 1 0 R /Info 13 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, xref)
	return out.Bytes()
}

func stampPixels() *image.NRGBA {
	im := image.NewNRGBA(image.Rect(5, 7, 7, 9))
	im.SetNRGBA(5, 7, color.NRGBA{R: 255, A: 255})
	im.SetNRGBA(6, 7, color.NRGBA{G: 255, A: 255})
	im.SetNRGBA(5, 8, color.NRGBA{B: 255, A: 255})
	im.SetNRGBA(6, 8, color.NRGBA{R: 255, G: 255, A: 128})
	return im
}

func TestImageStampPreservesDocument(t *testing.T) {
	ctx := context.Background()
	source := openIncidentPDF(t, stampFixture(t, 0, false, false))
	var before bytes.Buffer
	require.NoError(t, source.WritePDF(ctx, &before))
	page, err := source.Page(ctx, 0)
	require.NoError(t, err)
	text, err := page.TextCells(ctx)
	require.NoError(t, err)
	stamps := []parser.ImageStamp{{PageIndex: 0, Rect: geom.Box{L: 20, T: 30, R: 60, B: 70}, Image: stampPixels()}, {PageIndex: 0, Rect: geom.Box{L: 80, T: 100, R: 100, B: 120}, Image: stampPixels()}}
	var out bytes.Buffer
	require.NoError(t, source.WriteImageStamps(ctx, &out, stamps))
	require.Contains(t, out.String(), "/Title (Kept document title)")
	saved := openIncidentPDF(t, out.Bytes())
	count, err := saved.PageCount(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	values, err := saved.AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, "Ada", values["full-name"])
	attachments, err := saved.Attachments(ctx)
	require.NoError(t, err)
	require.Equal(t, []parser.Attachment{{Name: "note.txt"}}, attachments)
	savedPage, err := saved.Page(ctx, 0)
	require.NoError(t, err)
	savedText, err := savedPage.TextCells(ctx)
	require.NoError(t, err)
	require.Equal(t, text, savedText)
	images, err := saved.ExtractImages(ctx, []int{0})
	require.NoError(t, err)
	require.Len(t, images, 3)
	other, err := saved.ExtractImages(ctx, []int{1})
	require.NoError(t, err)
	require.Len(t, other, 1)
	var after bytes.Buffer
	require.NoError(t, source.WritePDF(ctx, &after))
	require.Equal(t, before.Bytes(), after.Bytes())
}

func TestImageStampRenderedCoordinatesAndAlpha(t *testing.T) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("pdftoppm required for independent pixel checks")
	}
	for _, rotation := range []int{0, 90, 180, 270} {
		for _, crop := range []bool{false, true} {
			for _, origin := range []geom.CoordOrigin{geom.TopLeft, geom.BottomLeft} {
				t.Run(fmt.Sprintf("rotation%d/crop%t/%s", rotation, crop, origin), func(t *testing.T) {
					source := openIncidentPDF(t, stampFixture(t, rotation, crop, false))
					page, err := source.Page(context.Background(), 0)
					require.NoError(t, err)
					size, err := page.Size(context.Background())
					require.NoError(t, err)
					rect := geom.Box{L: 20, T: 30, R: 60, B: 70, Origin: geom.TopLeft}.WithOrigin(origin, size.Height)
					var out bytes.Buffer
					require.NoError(t, source.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{{PageIndex: 0, Rect: rect, Image: stampPixels()}}))
					dir := t.TempDir()
					input := filepath.Join(dir, "stamped.pdf")
					require.NoError(t, os.WriteFile(input, out.Bytes(), 0600))
					if qpdf, err := exec.LookPath("qpdf"); err == nil {
						log, err := exec.Command(qpdf, "--check", input).CombinedOutput()
						require.NoError(t, err, string(log))
					}
					output := filepath.Join(dir, "page")
					log, err := exec.Command(tool, "-f", "1", "-singlefile", "-cropbox", "-r", "72", "-png", input, output).CombinedOutput()
					require.NoError(t, err, string(log))
					file, err := os.Open(output + ".png")
					require.NoError(t, err)
					defer file.Close()
					pixels, err := png.Decode(file)
					require.NoError(t, err)
					for _, point := range []struct {
						x, y int
						want color.NRGBA
					}{{30, 40, color.NRGBA{R: 255, A: 255}}, {50, 40, color.NRGBA{G: 255, A: 255}}, {30, 60, color.NRGBA{B: 255, A: 255}}, {50, 60, color.NRGBA{R: 255, G: 255, B: 127, A: 255}}} {
						got := color.NRGBAModel.Convert(pixels.At(point.x, point.y)).(color.NRGBA)
						require.InDelta(t, point.want.R, got.R, 2)
						require.InDelta(t, point.want.G, got.G, 2)
						require.InDelta(t, point.want.B, got.B, 2)
					}
				})
			}
		}
	}
}

func TestImageStampRejectsInvalidAndSignedInputs(t *testing.T) {
	source := openIncidentPDF(t, stampFixture(t, 0, false, false))
	base := parser.ImageStamp{PageIndex: 0, Rect: geom.Box{L: 20, T: 30, R: 60, B: 70}, Image: stampPixels()}
	tests := []struct {
		name   string
		change func(*parser.ImageStamp)
	}{
		{"negative page", func(s *parser.ImageStamp) { s.PageIndex = -1 }},
		{"past last page", func(s *parser.ImageStamp) { s.PageIndex = 2 }},
		{"nil image", func(s *parser.ImageStamp) { s.Image = nil }},
		{"empty rect", func(s *parser.ImageStamp) { s.Rect.R = s.Rect.L }},
		{"subprecision rect", func(s *parser.ImageStamp) { s.Rect.R = s.Rect.L + 1e-12 }},
		{"outside page", func(s *parser.ImageStamp) { s.Rect.L = -1 }},
		{"invalid origin", func(s *parser.ImageStamp) { s.Rect.Origin = "invalid" }},
		{"nonfinite", func(s *parser.ImageStamp) { s.Rect.T = math.NaN() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stamp := base
			tc.change(&stamp)
			var out bytes.Buffer
			require.Error(t, source.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{stamp}))
			require.Empty(t, out.Bytes())
		})
	}
	var out bytes.Buffer
	require.Error(t, source.WriteImageStamps(context.Background(), &out, nil))
	require.Empty(t, out.Bytes())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, source.WriteImageStamps(canceled, &out, []parser.ImageStamp{base}), context.Canceled)
	signed := openIncidentPDF(t, stampFixture(t, 0, false, true))
	require.ErrorIs(t, signed.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{base}), parser.ErrSignedDocument)
	require.Empty(t, out.Bytes())
}

type oversizedStampImage struct{}

func (oversizedStampImage) ColorModel() color.Model { return color.NRGBAModel }
func (oversizedStampImage) Bounds() image.Rectangle {
	maxInt := int(^uint(0) >> 1)
	return image.Rectangle{Min: image.Pt(-maxInt, 0), Max: image.Pt(maxInt, 1)}
}
func (oversizedStampImage) At(int, int) color.Color { panic("oversized image must not be sampled") }

func TestImageStampSizeGuardAndOpaqueBottomLeft(t *testing.T) {
	source := openIncidentPDF(t, stampFixture(t, 90, true, false))
	stamp := parser.ImageStamp{PageIndex: 0, Rect: geom.Box{L: 20, T: 130, R: 60, B: 90, Origin: geom.BottomLeft}, Image: oversizedStampImage{}}
	var out bytes.Buffer
	require.ErrorContains(t, source.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{stamp}), "16 megapixels")
	require.Empty(t, out.Bytes())
	opaque := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	opaque.SetNRGBA(0, 0, color.NRGBA{R: 27, G: 51, B: 89, A: 255})
	stamp.Image = opaque
	require.NoError(t, source.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{stamp}))
	saved := openIncidentPDF(t, out.Bytes())
	images, err := saved.ExtractImages(context.Background(), []int{0})
	require.NoError(t, err)
	require.Len(t, images, 2)
	require.NotContains(t, out.String(), "/SMask")
	last, err := png.Decode(bytes.NewReader(images[1].Data))
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{R: 27, G: 51, B: 89, A: 255}, color.NRGBAModel.Convert(last.At(0, 0)))
}

func TestImageStampEncryptedDocument(t *testing.T) {
	ctx := context.Background()
	input, err := os.ReadFile("testdata/security/r6.pdf")
	require.NoError(t, err)
	opened, err := (&parser.Backend{}).OpenBytesWithPassword(ctx, input, "user")
	require.NoError(t, err)
	source := opened.(*parser.Document)
	before, err := source.AcroFormValues(ctx)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, source.WriteImageStamps(ctx, &out, []parser.ImageStamp{{PageIndex: 0, Rect: geom.Box{L: 20, T: 30, R: 60, B: 70}, Image: stampPixels()}}))
	saved := openIncidentPDF(t, out.Bytes())
	encrypted, err := saved.IsEncrypted(ctx)
	require.NoError(t, err)
	require.False(t, encrypted)
	after, err := saved.AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	stillEncrypted, err := source.IsEncrypted(ctx)
	require.NoError(t, err)
	require.True(t, stillEncrypted)
}

func TestImageStampUpgradesVersionForTransparency(t *testing.T) {
	input := bytes.Replace(singlePagePDF(t, ""), []byte("%PDF-1.4"), []byte("%PDF-1.3"), 1)
	source := openIncidentPDF(t, input)
	var out bytes.Buffer
	require.NoError(t, source.WriteImageStamps(context.Background(), &out, []parser.ImageStamp{{PageIndex: 0, Rect: geom.Box{L: 20, T: 30, R: 60, B: 70}, Image: stampPixels()}}))
	require.True(t, bytes.HasPrefix(out.Bytes(), []byte("%PDF-1.4")))
}
