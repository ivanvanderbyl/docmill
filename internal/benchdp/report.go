package benchdp

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type ReportOptions struct {
	BenchmarkDate string
	Hardware      string
}

func RenderMarkdownReport(result BenchmarkResult, options ReportOptions) string {
	date := options.BenchmarkDate
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	hardware := options.Hardware
	if hardware == "" {
		hardware = "unknown"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Benchmarks\n\n")
	fmt.Fprintf(&b, "Evaluated on %d born-native PDF documents with ground-truth Markdown annotations from the %s corpus.\n\n", result.CorpusSize, result.CorpusName)
	fmt.Fprintf(&b, "- Benchmark date: `%s`\n", date)
	fmt.Fprintf(&b, "- Corpus: %d documents with ground-truth Markdown annotations (%d with tables, %d with headings)\n", result.CorpusSize, result.TableCases, result.HeadingCases)
	if result.SkippedImageOnlyCases > 0 {
		fmt.Fprintf(&b, "- Scope: born-native PDFs only (%d image-only PDFs excluded)\n", result.SkippedImageOnlyCases)
	} else {
		fmt.Fprintf(&b, "- Scope: born-native PDFs only\n")
	}
	fmt.Fprintf(&b, "- Hardware: %s\n", hardware)
	fmt.Fprintf(&b, "- Metrics: NID (reading order), TEDS (table structure), MHS (heading hierarchy)\n")
	fmt.Fprintf(&b, "- All scores normalised to [0, 1] - higher is better\n")
	fmt.Fprintf(&b, "- Extraction accuracy and NID are averaged over successful documents; TEDS is averaged over documents with tables; MHS is averaged over documents with headings\n")
	fmt.Fprintf(&b, "- Competitor commands and versions are loaded from the benchmark tool config\n\n")

	renderAccuracyTable(&b, result.Tools)
	renderSpeedTable(&b, result.Tools)
	renderSpeedCallouts(&b, result.Tools)

	return b.String()
}

func renderAccuracyTable(b *strings.Builder, tools []ToolBenchmarkResult) {
	bestExtraction := bestScore(tools, func(t ToolBenchmarkResult) float64 { return t.Scores.ExtractionAccuracy })
	bestOrder := bestScore(tools, func(t ToolBenchmarkResult) float64 { return t.Scores.ReadingOrderNID })
	bestTables := bestScore(tools, func(t ToolBenchmarkResult) float64 { return t.Scores.TableStructureTEDS })
	bestHeadings := bestScore(tools, func(t ToolBenchmarkResult) float64 { return t.Scores.HeadingLevelMHS })

	fmt.Fprintf(b, "## Accuracy Metrics\n\n")
	fmt.Fprintf(b, "| Solution | Version | Extraction accuracy | Reading order (NID) | Table structure (TEDS) | Heading level (MHS) |\n")
	fmt.Fprintf(b, "| --- | --- | ---: | ---: | ---: | ---: |\n")
	for _, tool := range tools {
		fmt.Fprintf(
			b,
			"| %s | %s | %s | %s | %s | %s |\n",
			tool.Name,
			displayVersion(tool.Version),
			formatMetric(tool.Scores.ExtractionAccuracy, bestExtraction),
			formatMetric(tool.Scores.ReadingOrderNID, bestOrder),
			formatMetric(tool.Scores.TableStructureTEDS, bestTables),
			formatMetric(tool.Scores.HeadingLevelMHS, bestHeadings),
		)
	}
	fmt.Fprintf(b, "\n")
}

func renderSpeedTable(b *strings.Builder, tools []ToolBenchmarkResult) {
	sorted := append([]ToolBenchmarkResult(nil), tools...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].MillisecondsPerPage < sorted[j].MillisecondsPerPage
	})
	fastest := 0.0
	for _, tool := range sorted {
		if tool.MillisecondsPerPage > 0 {
			fastest = tool.MillisecondsPerPage
			break
		}
	}

	fmt.Fprintf(b, "## Speed\n\n")
	fmt.Fprintf(b, "| Solution | Milliseconds per page |\n")
	fmt.Fprintf(b, "| --- | ---: |\n")
	for _, tool := range sorted {
		speed := fmt.Sprintf("%.1f", tool.MillisecondsPerPage)
		if fastest > 0 && nearlyEqual(tool.MillisecondsPerPage, fastest) {
			speed = "**" + speed + "**"
		}
		fmt.Fprintf(b, "| %s | %s |\n", tool.Name, speed)
	}
	fmt.Fprintf(b, "\n")
}

func renderSpeedCallouts(b *strings.Builder, tools []ToolBenchmarkResult) {
	sorted := append([]ToolBenchmarkResult(nil), tools...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].MillisecondsPerPage < sorted[j].MillisecondsPerPage
	})
	var fastest *ToolBenchmarkResult
	for i := range sorted {
		if sorted[i].MillisecondsPerPage > 0 {
			fastest = &sorted[i]
			break
		}
	}

	fmt.Fprintf(b, "## Relative Speed Callouts\n\n")
	if fastest == nil {
		fmt.Fprintf(b, "- No successful speed measurements\n")
		return
	}
	for _, tool := range sorted {
		if tool.Name == fastest.Name || tool.MillisecondsPerPage <= 0 {
			continue
		}
		ratio := int(math.Round(tool.MillisecondsPerPage / fastest.MillisecondsPerPage))
		if ratio < 2 {
			continue
		}
		fmt.Fprintf(b, "- %s is `%dx` faster than `%s`\n", fastest.Name, ratio, tool.Name)
	}
}

func bestScore(tools []ToolBenchmarkResult, value func(ToolBenchmarkResult) float64) float64 {
	best := 0.0
	for _, tool := range tools {
		v := value(tool)
		if v > best {
			best = v
		}
	}
	return best
}

func formatMetric(value, best float64) string {
	out := fmt.Sprintf("%.2f", value)
	if best > 0 && nearlyEqual(value, best) {
		return "**" + out + "**"
	}
	return out
}

func displayVersion(version string) string {
	if version == "" {
		return "-"
	}
	return version
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.000000001
}
