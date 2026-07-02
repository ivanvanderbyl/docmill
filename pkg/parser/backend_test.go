package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	docpage "github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
	"github.com/stretchr/testify/require"
)

func TestPageRulingSegmentsReturnsNativePathLines(t *testing.T) {
	t.Parallel()

	backend := parser.NewBackend()
	doc, err := backend.OpenBytes(context.Background(), singlePagePDF(t, "2 w 10 20 m 110 20 l S"))
	require.NoError(t, err)
	defer doc.Close()

	pdfPage, err := doc.Page(context.Background(), 0)
	require.NoError(t, err)
	provider, ok := pdfPage.(interface {
		RulingSegments(context.Context) ([]docpage.RulingSegment, error)
	})
	require.True(t, ok, "native page should expose ruling segments")

	segments, err := provider.RulingSegments(context.Background())
	require.NoError(t, err)
	require.Equal(t, []docpage.RulingSegment{{
		FromX:  10,
		FromY:  80,
		ToX:    110,
		ToY:    80,
		Width:  2,
		Origin: geom.TopLeft,
	}}, segments)
}

func TestBackendExtractsFilledAcroFormFieldsAsMarkdown(t *testing.T) {
	t.Parallel()

	backend := parser.NewBackend()
	doc, err := backend.OpenBytes(context.Background(), singlePageAcroFormPDF(t))
	require.NoError(t, err)
	defer doc.Close()

	got, err := docpdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Applicant name: Ada Lovelace", got)
}

func singlePagePDF(t *testing.T, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 5)
	writeObj := func(id int, body string) {
		offsets[id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", id, body)
	}

	buf.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>")
	offsets[4] = buf.Len()
	fmt.Fprintf(&buf, "4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(content)+1, content)

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 5\n0000000000 65535 f \n")
	for id := 1; id <= 4; id++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return buf.Bytes()
}

func singlePageAcroFormPDF(t *testing.T) []byte {
	t.Helper()

	return buildPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Contents 4 0 R /Annots [7 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [6 0 R] >>",
		"<< /FT /Tx /T (Applicant name) /V (Ada Lovelace) /Kids [7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [120 110 260 130] >>",
	}, "1 0 R")
}

func buildPDFObjects(t *testing.T, objects []string, root string) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, len(objects))
	buf.WriteString("%PDF-1.4\n")
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, root, xrefOffset)
	return buf.Bytes()
}
