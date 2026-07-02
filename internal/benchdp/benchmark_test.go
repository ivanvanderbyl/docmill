package benchdp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunBenchmarkAggregatesToolScoresAndSpeed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeCorpusFile(t, root, "pdf/one.pdf", "%PDF-one")
	writeCorpusFile(t, root, "pdf/two.pdf", "%PDF-two")
	writeCorpusFile(t, root, "groundtruth/one.md", "# Title\n\nfirst")
	writeCorpusFile(t, root, "groundtruth/two.md", "second")
	corpus := Corpus{
		Name: "fixture",
		Cases: []DocumentCase{
			{ID: "one", PDFPath: filepath.Join(root, "pdf/one.pdf"), GroundTruthPath: filepath.Join(root, "groundtruth/one.md"), Pages: 2},
			{ID: "two", PDFPath: filepath.Join(root, "pdf/two.pdf"), GroundTruthPath: filepath.Join(root, "groundtruth/two.md"), Pages: 1},
		},
	}
	config := ToolConfigFile{
		Tools: []ToolConfig{
			{Name: "docmill", Command: []string{"native", "{{input}}"}, OutputMode: OutputStdout},
			{Name: "docling", Command: []string{"docling", "{{input}}"}, OutputMode: OutputStdout},
		},
	}

	result, err := RunBenchmark(context.Background(), corpus, config, BenchmarkOptions{
		OutputDir: t.TempDir(),
		Runner: func(_ context.Context, tool ToolConfig, inputPath, _ string) (ToolRunResult, error) {
			switch {
			case tool.Name == "docmill" && filepath.Base(inputPath) == "one.pdf":
				return ToolRunResult{Markdown: "# Title\n\nfirst", Duration: 2 * time.Second}, nil
			case tool.Name == "docmill" && filepath.Base(inputPath) == "two.pdf":
				return ToolRunResult{Markdown: "second", Duration: time.Second}, nil
			default:
				return ToolRunResult{Markdown: "wrong", Duration: 3 * time.Second}, nil
			}
		},
		Versioner: func(_ context.Context, tool ToolConfig) (string, error) {
			return tool.Name + " 1.0.0", nil
		},
	})

	require.NoError(t, err)
	require.Equal(t, "fixture", result.CorpusName)
	require.Len(t, result.Tools, 2)
	require.Equal(t, "docmill", result.Tools[0].Name)
	require.Equal(t, "docmill 1.0.0", result.Tools[0].Version)
	require.Equal(t, 2, result.Tools[0].Cases)
	require.Equal(t, 0, result.Tools[0].Errors)
	require.Equal(t, 1.0, result.Tools[0].Scores.ExtractionAccuracy)
	require.InEpsilon(t, 1000.0, result.Tools[0].MillisecondsPerPage, 0.001)
	require.Equal(t, "docling", result.Tools[1].Name)
	require.Less(t, result.Tools[1].Scores.ExtractionAccuracy, 1.0)
}

func TestRunBenchmarkRecordsToolErrorsAndContinues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pdfPath := filepath.Join(root, "sample.pdf")
	groundTruthPath := filepath.Join(root, "sample.md")
	require.NoError(t, os.WriteFile(pdfPath, []byte("%PDF"), 0o644))
	require.NoError(t, os.WriteFile(groundTruthPath, []byte("hello"), 0o644))

	result, err := RunBenchmark(context.Background(), Corpus{
		Name:  "fixture",
		Cases: []DocumentCase{{ID: "sample", PDFPath: pdfPath, GroundTruthPath: groundTruthPath, Pages: 1}},
	}, ToolConfigFile{
		Tools: []ToolConfig{{Name: "broken", Command: []string{"broken"}, OutputMode: OutputStdout}},
	}, BenchmarkOptions{
		OutputDir: t.TempDir(),
		Runner: func(context.Context, ToolConfig, string, string) (ToolRunResult, error) {
			return ToolRunResult{}, os.ErrNotExist
		},
	})

	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	require.Equal(t, 1, result.Tools[0].Errors)
	require.Equal(t, 0, result.Tools[0].Cases)
	require.Contains(t, result.Tools[0].CaseResults[0].Error, "file does not exist")
}

func TestRunBenchmarkAveragesTableAndHeadingScoresOnlyOnRelevantCases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeCorpusFile(t, root, "pdf/plain.pdf", "%PDF-plain")
	writeCorpusFile(t, root, "pdf/rich.pdf", "%PDF-rich")
	writeCorpusFile(t, root, "groundtruth/plain.md", "plain body")
	writeCorpusFile(t, root, "groundtruth/rich.md", "# Heading\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n")
	corpus := Corpus{
		Name: "fixture",
		Cases: []DocumentCase{
			{ID: "plain", PDFPath: filepath.Join(root, "pdf/plain.pdf"), GroundTruthPath: filepath.Join(root, "groundtruth/plain.md"), Pages: 1},
			{ID: "rich", PDFPath: filepath.Join(root, "pdf/rich.pdf"), GroundTruthPath: filepath.Join(root, "groundtruth/rich.md"), Pages: 1},
		},
	}

	result, err := RunBenchmark(context.Background(), corpus, ToolConfigFile{
		Tools: []ToolConfig{{Name: "tool", Command: []string{"tool", "{{input}}"}, OutputMode: OutputStdout}},
	}, BenchmarkOptions{
		OutputDir: t.TempDir(),
		Runner: func(_ context.Context, _ ToolConfig, inputPath, _ string) (ToolRunResult, error) {
			if filepath.Base(inputPath) == "plain.pdf" {
				return ToolRunResult{Markdown: "plain body", Duration: time.Second}, nil
			}
			return ToolRunResult{Markdown: "wrong body", Duration: time.Second}, nil
		},
	})

	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	require.Equal(t, 2, result.Tools[0].Cases)
	require.InEpsilon(t, 0.5, result.Tools[0].Scores.ExtractionAccuracy, 0.001)
	require.Equal(t, 0.0, result.Tools[0].Scores.TableStructureTEDS)
	require.Equal(t, 0.0, result.Tools[0].Scores.HeadingLevelMHS)
}
