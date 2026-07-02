package pdf_test

import (
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
	"github.com/stretchr/testify/require"
)

func extractBodyWithoutTables(t *testing.T, cells []page.TextCell) string {
	t.Helper()

	doc := fakeDocument{pages: []fakePage{{cells: cells}}}
	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{DetectTables: false})
	require.NoError(t, err)
	return got
}

func TestAssembleParagraphsDoesNotRepairBlocklistSentenceByLiteralText(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCell(1, "We normalize the URLs", 0, 0, 140, 10),
		pdfTextCell(2, "forward slashes “ ” them to and the blocklist patterns by removing / from them and setting lowercase", 0, 30, 520, 40),
	}

	got := extractBodyWithoutTables(t, cells)

	require.Equal(t, "We normalize the URLs\n\nforward slashes “ ” them to and the blocklist patterns by removing / from them and setting lowercase", got)
}
