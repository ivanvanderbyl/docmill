package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ivanvanderbyl/docmill/internal/benchdp"
)

func runBenchmark(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("docmill benchmark", flag.ContinueOnError)
	flags.SetOutput(stderr)
	corpusPath := flags.String("corpus", "", "benchmark corpus directory containing manifest.json")
	toolsPath := flags.String("tools", "", "tool config JSON path")
	reportPath := flags.String("out", "", "write Markdown report to this path instead of stdout")
	jsonPath := flags.String("json", "", "write machine-readable JSON report to this path")
	outputDir := flags.String("outputs", "", "directory for converter output files")
	date := flags.String("date", "", "benchmark date for the Markdown report")
	hardware := flags.String("hardware", "", "hardware label for the Markdown report")
	allowMissing := flags.Bool("allow-missing", false, "allow tool configs that omit requested benchmark competitors")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *corpusPath == "" || *toolsPath == "" {
		_, _ = fmt.Fprintln(stderr, "-corpus and -tools are required")
		return 1
	}

	corpus, err := benchdp.LoadCorpus(*corpusPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "load corpus: %v\n", err)
		return 1
	}

	configData, err := os.ReadFile(*toolsPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read tools config: %v\n", err)
		return 1
	}
	config, err := benchdp.LoadToolConfig(configData)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "parse tools config: %v\n", err)
		return 1
	}
	if !*allowMissing {
		if err := benchdp.ValidateRequiredTools(config, benchdp.RequiredToolNames); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
	}

	result, err := benchdp.RunBenchmark(ctx, corpus, config, benchdp.BenchmarkOptions{OutputDir: *outputDir})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "run benchmark: %v\n", err)
		return 1
	}

	report := benchdp.RenderMarkdownReport(result, benchdp.ReportOptions{
		BenchmarkDate: *date,
		Hardware:      *hardware,
	})
	if *reportPath == "" {
		if _, err := fmt.Fprint(stdout, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "write report: %v\n", err)
			return 1
		}
	} else if err := writeBenchmarkFile(*reportPath, []byte(report)); err != nil {
		_, _ = fmt.Fprintf(stderr, "write Markdown report: %v\n", err)
		return 1
	}

	if *jsonPath != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "encode JSON report: %v\n", err)
			return 1
		}
		data = append(data, '\n')
		if err := writeBenchmarkFile(*jsonPath, data); err != nil {
			_, _ = fmt.Fprintf(stderr, "write JSON report: %v\n", err)
			return 1
		}
	}

	return 0
}

func writeBenchmarkFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
