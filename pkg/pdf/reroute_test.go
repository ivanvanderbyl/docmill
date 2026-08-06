package pdf_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	pdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// The Task 5 neutrality gate, as a unit test.
//
// The full gate runs both paths over the 200-document DPBench corpus
// (benchmarks/layout/spike/cmd/reroutecheck) and requires byte-identical
// Markdown. These cases pin the same property on the shapes that exercise each
// routing destination, so a regression is caught by `go test` rather than only
// by a corpus run someone has to remember to do.

func routeNeutralityOptions(routed bool) pdf.ExtractionOptions {
	return pdf.ExtractionOptions{
		DetectTables:      true,
		ReadingOrder:      true,
		DetectStructure:   true,
		DetectHeadings:    true,
		ClassifyThenRoute: routed,
	}
}

func requireRouteNeutral(t *testing.T, doc pdf.Document) {
	t.Helper()
	ctx := context.Background()

	base, err := pdf.ExtractMarkdownWithOptions(ctx, doc, routeNeutralityOptions(false))
	require.NoError(t, err)
	routed, err := pdf.ExtractMarkdownWithOptions(ctx, doc, routeNeutralityOptions(true))
	require.NoError(t, err)

	require.Equal(t, base, routed, "classify-then-route must be byte-identical to the default path")
}

func TestClassifyThenRouteMatchesDefaultPathOnProse(t *testing.T) {
	t.Parallel()
	requireRouteNeutral(t, fakeDocument{pages: []fakePage{{
		cells: []page.TextCell{
			{Index: 1, Text: "A Heading", FontSize: 18, Box: geom.Box{L: 40, T: 40, R: 300, B: 60, Origin: geom.TopLeft}},
			{Index: 2, Text: "Body text that runs on for a while and wraps.", FontSize: 10, Box: geom.Box{L: 40, T: 80, R: 400, B: 92, Origin: geom.TopLeft}},
			{Index: 3, Text: "A second line of the same paragraph.", FontSize: 10, Box: geom.Box{L: 40, T: 94, R: 380, B: 106, Origin: geom.TopLeft}},
		},
	}}})
}

func TestClassifyThenRouteMatchesDefaultPathOnLists(t *testing.T) {
	t.Parallel()
	requireRouteNeutral(t, fakeDocument{pages: []fakePage{{
		cells: []page.TextCell{
			{Index: 1, Text: "Intro paragraph before the list.", FontSize: 10, Box: geom.Box{L: 40, T: 40, R: 400, B: 52, Origin: geom.TopLeft}},
			{Index: 2, Text: "• first item", FontSize: 10, Box: geom.Box{L: 60, T: 60, R: 300, B: 72, Origin: geom.TopLeft}},
			{Index: 3, Text: "• second item", FontSize: 10, Box: geom.Box{L: 60, T: 74, R: 300, B: 86, Origin: geom.TopLeft}},
			{Index: 4, Text: "• third item", FontSize: 10, Box: geom.Box{L: 60, T: 88, R: 300, B: 100, Origin: geom.TopLeft}},
		},
	}}})
}

// A grid is the case that matters most: table cells never reach the line
// assembler on the default path, and they do on the rerouted one.
func TestClassifyThenRouteMatchesDefaultPathOnTables(t *testing.T) {
	t.Parallel()
	var cells []page.TextCell
	index := 1
	headers := []string{"Region", "Revenue", "Growth"}
	for column, header := range headers {
		cells = append(cells, page.TextCell{
			Index: index, Text: header, FontSize: 10,
			Box: geom.Box{L: float64(40 + column*120), T: 40, R: float64(140 + column*120), B: 52, Origin: geom.TopLeft},
		})
		index++
	}
	rows := [][]string{
		{"North", "1200", "4.1"},
		{"South", "980", "2.7"},
		{"East", "1430", "6.9"},
	}
	for row, values := range rows {
		for column, value := range values {
			cells = append(cells, page.TextCell{
				Index: index, Text: value, FontSize: 10,
				Box: geom.Box{
					L: float64(40 + column*120), T: float64(60 + row*20),
					R: float64(140 + column*120), B: float64(72 + row*20), Origin: geom.TopLeft,
				},
			})
			index++
		}
	}
	requireRouteNeutral(t, fakeDocument{pages: []fakePage{{cells: cells}}})
}

func TestClassifyThenRouteMatchesDefaultPathAcrossPages(t *testing.T) {
	t.Parallel()
	requireRouteNeutral(t, fakeDocument{pages: []fakePage{
		{cells: []page.TextCell{
			{Index: 1, Text: "Page one heading", FontSize: 18, Box: geom.Box{L: 40, T: 40, R: 300, B: 60, Origin: geom.TopLeft}},
			{Index: 2, Text: "Page one body.", FontSize: 10, Box: geom.Box{L: 40, T: 80, R: 300, B: 92, Origin: geom.TopLeft}},
		}},
		{cells: []page.TextCell{
			{Index: 1, Text: "Page two body continues.", FontSize: 10, Box: geom.Box{L: 40, T: 40, R: 300, B: 52, Origin: geom.TopLeft}},
		}},
	}})
}

func TestClassifyThenRouteMatchesDefaultPathOnEmptyPage(t *testing.T) {
	t.Parallel()
	requireRouteNeutral(t, fakeDocument{pages: []fakePage{{cells: nil}}})
}
