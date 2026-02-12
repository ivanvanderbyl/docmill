package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/urfave/cli/v3"

	"github.com/ivanvanderbyl/pdfmarkdown"
)

func main() {
	cmd := &cli.Command{
		Name:  "pdfmarkdown",
		Usage: "Convert PDF files to markdown",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "input",
				Aliases:  []string{"i"},
				Usage:    "Input PDF file path",
				Required: true,
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Output markdown file path (default: stdout)",
			},
			&cli.IntFlag{
				Name:  "start-page",
				Usage: "Start page number (0-indexed)",
				Value: -1,
			},
			&cli.IntFlag{
				Name:  "end-page",
				Usage: "End page number (0-indexed)",
				Value: -1,
			},
			&cli.BoolFlag{
				Name:    "metrics",
				Aliases: []string{"m"},
				Usage:   "Enable processing time and statistics logging",
				Value:   false,
			},
			&cli.BoolFlag{
				Name:  "chunk",
				Usage: "Output as JSON chunks instead of markdown",
				Value: false,
			},
			&cli.IntFlag{
				Name:  "chunk-max-tokens",
				Usage: "Maximum tokens per chunk",
				Value: 512,
			},
			&cli.IntFlag{
				Name:  "chunk-overlap",
				Usage: "Number of overlap tokens between chunks",
				Value: 0,
			},
			&cli.BoolFlag{
				Name:  "chunk-repeat-headings",
				Usage: "Repeat heading hierarchy at the start of each chunk",
				Value: false,
			},
		},
		Action: convertPDF,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func convertPDF(_ context.Context, cmd *cli.Command) error {
	inputPath := cmd.String("input")
	outputPath := cmd.String("output")
	startPage := cmd.Int("start-page")
	endPage := cmd.Int("end-page")
	enableMetrics := cmd.Bool("metrics")
	chunkMode := cmd.Bool("chunk")
	chunkMaxTokens := cmd.Int("chunk-max-tokens")
	chunkOverlap := cmd.Int("chunk-overlap")
	chunkRepeatHeadings := cmd.Bool("chunk-repeat-headings")

	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	if err != nil {
		return fmt.Errorf("failed to initialise pdfium: %w", err)
	}
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	if err != nil {
		return fmt.Errorf("failed to get pdfium instance: %w", err)
	}

	config := pdfmarkdown.DefaultConfig()
	config.EnableMetricsLogging = enableMetrics
	converter := pdfmarkdown.NewConverterWithConfig(instance, config)

	info, err := converter.GetDocumentInfo(inputPath)
	if err != nil {
		return fmt.Errorf("failed to get document info: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Processing PDF with %d pages...\n", info.PageCount)

	if chunkMode {
		cc := pdfmarkdown.ChunkConfig{
			MaxTokens:      int(chunkMaxTokens),
			OverlapTokens:  int(chunkOverlap),
			RepeatHeadings: chunkRepeatHeadings,
		}

		chunks, err := converter.ConvertFileChunks(inputPath, cc)
		if err != nil {
			return fmt.Errorf("failed to convert PDF to chunks: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Produced %d chunks\n", len(chunks))

		output, err := json.MarshalIndent(chunks, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal chunks: %w", err)
		}

		if outputPath != "" {
			if err := os.WriteFile(outputPath, output, 0644); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Chunks written to %s\n", outputPath)
		} else {
			fmt.Println(string(output))
		}

		return nil
	}

	var markdown string
	if startPage >= 0 || endPage >= 0 {
		if startPage < 0 {
			startPage = 0
		}
		if endPage < 0 {
			endPage = info.PageCount - 1
		}
		fmt.Fprintf(os.Stderr, "Converting pages %d to %d...\n", startPage+1, endPage+1)
		markdown, err = converter.ConvertPageRange(inputPath, startPage, endPage)
	} else {
		fmt.Fprintf(os.Stderr, "Converting all pages...\n")
		markdown, err = converter.ConvertFile(inputPath)
	}

	if err != nil {
		return fmt.Errorf("failed to convert PDF: %w", err)
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(markdown), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Markdown written to %s\n", outputPath)
	} else {
		fmt.Println(markdown)
	}

	return nil
}
