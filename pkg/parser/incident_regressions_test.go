package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

// This synthetic PDF mirrors PR Alcova-AI/alcova-backend#2853: a /Pages
// /Count 0 /Kids [] node alongside four real pages. It contains no customer data.
func incidentPDF(t *testing.T, emptyBranchPosition int, destination string) []byte {
	t.Helper()
	kids := []int{4, 5, 6, 7}
	kids = append(kids, 0)
	copy(kids[emptyBranchPosition+1:], kids[emptyBranchPosition:])
	kids[emptyBranchPosition] = 3
	var kidRefs bytes.Buffer
	for _, id := range kids {
		fmt.Fprintf(&kidRefs, "%d 0 R ", id)
	}
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if destination != "" {
		catalog = "<< /Type /Catalog /Pages 2 0 R /Names << /Dests 13 0 R >> >>"
	}
	bodies := []string{
		catalog,
		fmt.Sprintf("<< /Type /Pages /Count 4 /Kids [%s] >>", kidRefs.String()),
		"<< /Type /Pages /Count 0 /Kids [] /Parent 2 0 R >>",
	}
	for i := range 4 {
		bodies = append(bodies, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d 700] /Resources << /Font << /F1 8 0 R >> >> /Contents %d 0 R >>", 501+i, 9+i))
	}
	bodies = append(bodies, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>")
	for i := range 4 {
		content := fmt.Sprintf("BT /F1 12 Tf 30 650 Td (Incident page %d) Tj ET", i+1)
		bodies = append(bodies, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	if destination != "" {
		bodies = append(bodies, "<< /Names [(empty) 14 0 R] >>", destination)
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
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, xref)
	return out.Bytes()
}

func openIncidentPDF(t *testing.T, data []byte) *parser.Document {
	t.Helper()
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	require.NoError(t, err)
	native, ok := doc.(*parser.Document)
	require.True(t, ok)
	return native
}

func assertIncidentPages(t *testing.T, doc *parser.Document, sourcePages []int) {
	t.Helper()
	ctx := context.Background()
	count, err := doc.PageCount(ctx)
	require.NoError(t, err)
	require.Equal(t, len(sourcePages), count)
	for index, sourcePage := range sourcePages {
		page, err := doc.Page(ctx, index)
		require.NoError(t, err)
		size, err := page.Size(ctx)
		require.NoError(t, err)
		require.Equal(t, float64(500+sourcePage), size.Width)
		require.Equal(t, float64(700), size.Height)
		cells, err := page.TextCells(ctx)
		require.NoError(t, err)
		var text bytes.Buffer
		for _, cell := range cells {
			text.WriteString(cell.Text)
		}
		require.Equal(t, fmt.Sprintf("Incident page %d", sourcePage), text.String())
	}
}

// ALCOVA-BACKEND-15R requested pages 1-4 from a readable PDF with an empty
// page-tree branch. Keep the range contracts identical to the backend's
// synthetic regression, adapting its inclusive one-based bounds at the call.
func TestIncident15REmptyPageTreeBranchRoundTrip(t *testing.T) {
	for _, position := range []int{0, 2, 4} {
		t.Run(fmt.Sprintf("empty branch at %d", position), func(t *testing.T) {
			source := openIncidentPDF(t, incidentPDF(t, position, ""))
			assertIncidentPages(t, source, []int{1, 2, 3, 4})
			for _, tc := range []struct {
				name       string
				start, end int
				want       []int
			}{
				{"full requested range", 0, 4, []int{1, 2, 3, 4}},
				{"truncated range", 1, 3, []int{2, 3}},
				{"single page", 3, 4, []int{4}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var out bytes.Buffer
					require.NoError(t, source.WritePageRange(context.Background(), &out, tc.start, tc.end))
					assertIncidentPages(t, openIncidentPDF(t, out.Bytes()), tc.want)
				})
			}
			var out bytes.Buffer
			require.NoError(t, source.WritePages(context.Background(), &out, []int{3, 0, 2, 0}))
			assertIncidentPages(t, openIncidentPDF(t, out.Bytes()), []int{4, 1, 3, 1})
			assertIncidentPages(t, source, []int{1, 2, 3, 4})
		})
	}
}

// AI-1654 failed in pdfcpu migrateNamedDests at arr[0] for an empty array.
// The customer PDF was not retained; these cases reproduce both indexing
// branches in that function: an array destination and a dictionary's /D array.
func TestIncident1654EmptyNamedDestinationsDoNotBreakTruncation(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"empty array", "[]"},
		{"empty dictionary destination", "<< /D [] >>"},
		{"missing dictionary destination", "<< >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := openIncidentPDF(t, incidentPDF(t, 0, tc.value))
			var out bytes.Buffer
			require.NoError(t, source.WritePageRange(context.Background(), &out, 0, 2))
			assertIncidentPages(t, openIncidentPDF(t, out.Bytes()), []int{1, 2})
			out.Reset()
			require.NoError(t, source.WritePages(context.Background(), &out, []int{3, 0, 3}))
			assertIncidentPages(t, openIncidentPDF(t, out.Bytes()), []int{4, 1, 4})
			// Full-document save retains the malformed optional metadata, but must
			// still leave page content accessible and return an ordinary result.
			out.Reset()
			require.NoError(t, source.WritePDF(context.Background(), &out))
			assertIncidentPages(t, openIncidentPDF(t, out.Bytes()), []int{1, 2, 3, 4})
		})
	}
}
