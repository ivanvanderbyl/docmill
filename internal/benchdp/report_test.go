package benchdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderMarkdownReportMatchesBenchmarkShape(t *testing.T) {
	t.Parallel()

	report := RenderMarkdownReport(BenchmarkResult{
		CorpusName:   "dpbench",
		CorpusSize:   200,
		TableCases:   42,
		HeadingCases: 107,
		Tools: []ToolBenchmarkResult{
			{
				Name:                "docmill",
				Version:             "dev",
				Cases:               200,
				Scores:              Scores{ExtractionAccuracy: 0.91, ReadingOrderNID: 0.92, TableStructureTEDS: 0.73, HeadingLevelMHS: 0.83},
				MillisecondsPerPage: 10.0,
			},
			{
				Name:                "docling",
				Version:             "2.91.0",
				Cases:               200,
				Scores:              Scores{ExtractionAccuracy: 0.88, ReadingOrderNID: 0.90, TableStructureTEDS: 0.89, HeadingLevelMHS: 0.82},
				MillisecondsPerPage: 527.0,
			},
		},
	}, ReportOptions{
		BenchmarkDate: "2026-06-21",
		Hardware:      "Apple M4 Max",
	})

	require.Contains(t, report, "# Benchmarks")
	require.Contains(t, report, "Evaluated on 200 born-native PDF documents")
	require.Contains(t, report, "- Benchmark date: `2026-06-21`")
	require.Contains(t, report, "- Corpus: 200 documents with ground-truth Markdown annotations (42 with tables, 107 with headings)")
	require.Contains(t, report, "- Extraction accuracy and NID are averaged over successful documents; TEDS is averaged over documents with tables; MHS is averaged over documents with headings")
	require.Contains(t, report, "## Accuracy Metrics")
	require.Contains(t, report, "| Solution | Version | Extraction accuracy | Reading order (NID) | Table structure (TEDS) | Heading level (MHS) |")
	require.Contains(t, report, "| docmill | dev | **0.91** | **0.92** | 0.73 | **0.83** |")
	require.Contains(t, report, "## Speed")
	require.Contains(t, report, "| Solution | Milliseconds per page |")
	require.True(t, strings.Index(report, "| docmill | **10.0** |") < strings.Index(report, "| docling | 527.0 |"))
	require.Contains(t, report, "## Relative Speed Callouts")
	require.Contains(t, report, "- docmill is `53x` faster than `docling`")
}
