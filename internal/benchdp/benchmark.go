package benchdp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type ToolRunner func(context.Context, ToolConfig, string, string) (ToolRunResult, error)
type ToolVersioner func(context.Context, ToolConfig) (string, error)

type BenchmarkOptions struct {
	OutputDir string
	Runner    ToolRunner
	Versioner ToolVersioner
}

type BenchmarkResult struct {
	CorpusName            string                `json:"corpus_name"`
	CorpusSource          string                `json:"corpus_source,omitempty"`
	CorpusSize            int                   `json:"corpus_size"`
	SkippedImageOnlyCases int                   `json:"skipped_image_only_cases,omitempty"`
	TableCases            int                   `json:"table_cases"`
	HeadingCases          int                   `json:"heading_cases"`
	Tools                 []ToolBenchmarkResult `json:"tools"`
}

type ToolBenchmarkResult struct {
	Name                string                `json:"name"`
	Version             string                `json:"version,omitempty"`
	Cases               int                   `json:"cases"`
	Errors              int                   `json:"errors"`
	Scores              Scores                `json:"scores"`
	MillisecondsPerPage float64               `json:"milliseconds_per_page"`
	TotalDuration       time.Duration         `json:"total_duration"`
	CaseResults         []CaseBenchmarkResult `json:"case_results"`
}

type CaseBenchmarkResult struct {
	ID       string        `json:"id"`
	Pages    int           `json:"pages"`
	Scores   Scores        `json:"scores"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

func RunBenchmark(ctx context.Context, corpus Corpus, config ToolConfigFile, options BenchmarkOptions) (BenchmarkResult, error) {
	runner := options.Runner
	if runner == nil {
		runner = RunTool
	}
	versioner := options.Versioner
	if versioner == nil {
		versioner = ToolVersion
	}
	outputDir := options.OutputDir
	if outputDir == "" {
		var err error
		outputDir, err = os.MkdirTemp("", "docmill-benchmark-*")
		if err != nil {
			return BenchmarkResult{}, err
		}
	}

	result := BenchmarkResult{
		CorpusName:            corpus.Name,
		CorpusSource:          corpus.Source,
		CorpusSize:            len(corpus.Cases),
		SkippedImageOnlyCases: corpus.SkippedImageOnlyCases,
	}
	groundTruth := make(map[string]string, len(corpus.Cases))
	for _, c := range corpus.Cases {
		data, err := os.ReadFile(c.GroundTruthPath)
		if err != nil {
			return BenchmarkResult{}, fmt.Errorf("read ground truth for %s: %w", c.ID, err)
		}
		text := string(data)
		groundTruth[c.ID] = text
		if len(tableSignatures(text)) > 0 {
			result.TableCases++
		}
		if len(headingSignatures(text)) > 0 {
			result.HeadingCases++
		}
	}

	for _, tool := range config.Tools {
		toolResult := ToolBenchmarkResult{Name: tool.Name}
		version, err := versioner(ctx, tool)
		if err != nil {
			toolResult.Errors++
			toolResult.CaseResults = append(toolResult.CaseResults, CaseBenchmarkResult{Error: err.Error()})
		} else {
			toolResult.Version = version
		}

		var total Scores
		extractionCases := 0
		readingOrderCases := 0
		tableCases := 0
		headingCases := 0
		totalPages := 0
		for _, c := range corpus.Cases {
			pages := c.Pages
			if pages <= 0 {
				pages = 1
			}
			outputPath := filepath.Join(outputDir, safePathName(tool.Name), safePathName(c.ID)+".md")
			if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
				return BenchmarkResult{}, err
			}
			run, err := runner(ctx, tool, c.PDFPath, outputPath)
			caseResult := CaseBenchmarkResult{ID: c.ID, Pages: pages, Duration: run.Duration}
			if err != nil {
				toolResult.Errors++
				caseResult.Error = err.Error()
				toolResult.CaseResults = append(toolResult.CaseResults, caseResult)
				continue
			}
			caseResult.Scores = EvaluateMarkdown(groundTruth[c.ID], run.Markdown)
			toolResult.CaseResults = append(toolResult.CaseResults, caseResult)
			toolResult.Cases++
			toolResult.TotalDuration += run.Duration
			totalPages += pages
			total.ExtractionAccuracy += caseResult.Scores.ExtractionAccuracy
			extractionCases++
			total.ReadingOrderNID += caseResult.Scores.ReadingOrderNID
			readingOrderCases++
			if len(tableSignatures(groundTruth[c.ID])) > 0 {
				total.TableStructureTEDS += caseResult.Scores.TableStructureTEDS
				tableCases++
			}
			if len(headingSignatures(groundTruth[c.ID])) > 0 {
				total.HeadingLevelMHS += caseResult.Scores.HeadingLevelMHS
				headingCases++
			}
		}
		toolResult.Scores = averageScores(total, extractionCases, readingOrderCases, tableCases, headingCases)
		if totalPages > 0 {
			toolResult.MillisecondsPerPage = float64(toolResult.TotalDuration) / float64(time.Millisecond) / float64(totalPages)
		}
		result.Tools = append(result.Tools, toolResult)
	}

	return result, nil
}

func averageScores(s Scores, extractionCases, readingOrderCases, tableCases, headingCases int) Scores {
	return Scores{
		ExtractionAccuracy: divideMetric(s.ExtractionAccuracy, extractionCases),
		ReadingOrderNID:    divideMetric(s.ReadingOrderNID, readingOrderCases),
		TableStructureTEDS: divideMetric(s.TableStructureTEDS, tableCases),
		HeadingLevelMHS:    divideMetric(s.HeadingLevelMHS, headingCases),
	}
}

func divideMetric(value float64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return value / float64(count)
}

func safePathName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}
