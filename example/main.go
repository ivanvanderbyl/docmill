package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/urfave/cli/v3"

	"github.com/ivanvanderbyl/docmill"
)

func main() {
	cmd := &cli.Command{
		Name:  "docmill",
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

	// Initialise pdfium
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

	// Create converter with metrics enabled if requested
	config := docmill.DefaultConfig()
	config.EnableMetricsLogging = enableMetrics
	converter := docmill.NewConverterWithConfig(instance, config)

	// Get document info
	info, err := converter.GetDocumentInfo(inputPath)
	if err != nil {
		return fmt.Errorf("failed to get document info: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Processing PDF with %d pages...\n", info.PageCount)

	// Convert PDF
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

	// Write output
	if outputPath != "" {
		err = os.WriteFile(outputPath, []byte(markdown), 0644)
		if err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Markdown written to %s\n", outputPath)
	} else {
		fmt.Println(markdown)
	}

	return nil
}
