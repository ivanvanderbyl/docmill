package parser_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

// Historical pdfcpu failures escaped from preview and form call sites. These
// synthetic inputs preserve the malformed structures without customer data.
func TestFormsIgnoreMalformedEntriesAndEmptyDestination(t *testing.T) {
	data := imagePDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /Dests << /Names [(broken) []] >> >> /AcroForm << /Fields [null 42 [] 4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Annots [null [] 42 4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (before) /Rect [10 10 50 30] >>",
	)
	ctx := context.Background()
	opened, err := parser.NewBackend().OpenBytes(ctx, data)
	require.NoError(t, err)
	d := opened.(*parser.Document)
	values, err := d.AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"name": "before"}, values)
	changed, err := d.SetAcroFormValues(ctx, map[string]string{"name": "after"})
	require.NoError(t, err)
	require.Equal(t, 1, changed)
	p, err := d.Page(ctx, 0)
	require.NoError(t, err)
	native := p.(*parser.Page)
	fields, err := native.FormFields(ctx)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, "after", fields[0].Value)
	boxes, err := native.FormFieldBoxes(ctx)
	require.NoError(t, err)
	require.Len(t, boxes, 1)
	require.Equal(t, "name", boxes[0].Name)
}

func TestFormAPIsRejectInvalidHandles(t *testing.T) {
	ctx := context.Background()
	for _, d := range []*parser.Document{nil, {}} {
		_, err := d.AcroFormValues(ctx)
		require.Error(t, err)
		_, err = d.SetAcroFormValues(ctx, map[string]string{"name": "value"})
		require.Error(t, err)
	}
	for _, p := range []*parser.Page{nil, {}} {
		_, err := p.FormFields(ctx)
		require.Error(t, err)
		_, err = p.FormFieldBoxes(ctx)
		require.Error(t, err)
	}
}

func TestImageAPIsRejectTruncatedData(t *testing.T) {
	for _, tc := range []struct{ name, dict, payload, want string }{
		{
			"short RGB samples",
			"/ColorSpace /DeviceRGB /BitsPerComponent 8",
			string([]byte{255, 0}),
			"truncated samples",
		},
		{
			"truncated Flate",
			"/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode",
			string([]byte{0x78, 0x9c}),
			"truncated samples",
		},
		{
			"truncated JPEG",
			"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode",
			string([]byte{0xff, 0xd8, 0xff}),
			"invalid JPEG",
		},
		{
			"short inline samples",
			"",
			"BI /W 2 /H 2 /BPC 8 /CS /RGB ID X EI",
			"truncated inline image",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /I 4 0 R >> >> >>"
			body := imageStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 "+tc.dict, tc.payload)
			if tc.dict == "" {
				page = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>"
				body = imageStream("", tc.payload)
			}
			data := imagePDF("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", page, body)
			d, err := parser.NewBackend().OpenBytes(context.Background(), data)
			require.NoError(t, err)
			images, err := d.(*parser.Document).ExtractImages(context.Background(), nil)
			require.ErrorContains(t, err, tc.want)
			require.Empty(t, images)
		})
	}
}

func TestTruncatedFilesRejectedBeforePreviewAndForms(t *testing.T) {
	original := imagePDF("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /Resources << /XObject << /I 4 0 R >> >> >>", imageStream("/Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", string([]byte{255, 0, 0})))
	cuts := []struct {
		name   string
		offset int
	}{
		{"empty", 0}, {"one byte", 1}, {"header only", 7}, {"before page tree", bytes.Index(original, []byte("2 0 obj"))}, {"before image", bytes.Index(original, []byte("4 0 obj"))}, {"within image", bytes.Index(original, []byte("stream\n")) + len("stream\n") + 1},
	}
	for _, cut := range cuts {
		t.Run(cut.name, func(t *testing.T) {
			d, err := parser.NewBackend().OpenBytes(context.Background(), original[:cut.offset])
			require.ErrorIs(t, err, parser.ErrInvalidPDF)
			require.Nil(t, d)
		})
	}
}

func TestFormValuesIgnoreMalformedNestedArrays(t *testing.T) {
	data := imagePDF("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /FT /Ch /T (selection) /V [[(invalid)] (valid)] >>")
	d, err := parser.NewBackend().OpenBytes(context.Background(), data)
	require.NoError(t, err)
	values, err := d.(*parser.Document).AcroFormValues(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"selection": "valid"}, values)
}

func TestFormValuesIgnoreCyclicArrays(t *testing.T) {
	data := imagePDF("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Annots [4 0 R] >>", "<< /Type /Annot /Subtype /Widget /FT /Ch /T (selection) /V 5 0 R /Rect [10 10 50 30] >>", "[5 0 R (valid)]")
	ctx := context.Background()
	d, err := parser.NewBackend().OpenBytes(ctx, data)
	require.NoError(t, err)
	values, err := d.(*parser.Document).AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"selection": "valid"}, values)
	page, err := d.Page(ctx, 0)
	require.NoError(t, err)
	fields, err := page.(*parser.Page).FormFields(ctx)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, "valid", fields[0].Value)
}

func TestHistoricalStructuresThroughPreviewAndFormAPIs(t *testing.T) {
	for _, destination := range []string{"[]", "<< /D [] >>"} {
		t.Run(destination, func(t *testing.T) {
			// The fixture combines an empty page-tree branch with the empty named
			// destination shape that previously panicked pdfcpu validation callers.
			d := openIncidentPDF(t, incidentPDF(t, 2, destination))
			ctx := context.Background()
			images, err := d.ExtractImages(ctx, nil)
			require.NoError(t, err)
			require.Empty(t, images)
			values, err := d.AcroFormValues(ctx)
			require.NoError(t, err)
			require.Empty(t, values)
			changed, err := d.SetAcroFormValues(ctx, map[string]string{})
			require.NoError(t, err)
			require.Zero(t, changed)
			page, err := d.Page(ctx, 0)
			require.NoError(t, err)
			native := page.(*parser.Page)
			fields, err := native.FormFields(ctx)
			require.NoError(t, err)
			require.Empty(t, fields)
			boxes, err := native.FormFieldBoxes(ctx)
			require.NoError(t, err)
			require.Empty(t, boxes)
		})
	}
}
