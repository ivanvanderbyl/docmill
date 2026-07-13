package parser

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPageLoadReusesInterpretedPage(t *testing.T) {
	t.Parallel()

	backend := NewBackend()
	doc, err := backend.OpenBytes(context.Background(), cacheTestPDF(t))
	require.NoError(t, err)
	defer doc.Close()

	pdfPage, err := doc.Page(context.Background(), 0)
	require.NoError(t, err)
	p := pdfPage.(*Page)

	first, firstSize, err := p.load()
	require.NoError(t, err)
	second, secondSize, err := p.load()
	require.NoError(t, err)

	require.Same(t, first, second)
	require.Equal(t, firstSize, secondSize)
}

func TestPageTextPageReusesTextInterpretation(t *testing.T) {
	t.Parallel()

	backend := NewBackend()
	doc, err := backend.OpenBytes(context.Background(), cacheTestPDF(t))
	require.NoError(t, err)
	defer doc.Close()

	pdfPage, err := doc.Page(context.Background(), 0)
	require.NoError(t, err)
	p := pdfPage.(*Page)

	first, firstSize, err := p.textPage()
	require.NoError(t, err)
	second, secondSize, err := p.textPage()
	require.NoError(t, err)

	require.Same(t, first, second)
	require.Equal(t, firstSize, secondSize)
}

func cacheTestPDF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 5)
	writeObject := func(id int, body string) {
		offsets[id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", id, body)
	}

	buf.WriteString("%PDF-1.4\n")
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>")
	writeObject(4, "<< /Length 0 >>\nstream\n\nendstream")

	xrefOffset := buf.Len()
	fmt.Fprint(&buf, "xref\n0 5\n0000000000 65535 f \n")
	for id := 1; id <= 4; id++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return buf.Bytes()
}
