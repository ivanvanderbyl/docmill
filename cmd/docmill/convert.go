package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// runConvert converts a single PDF path to Markdown on stdout using the native
// pure-Go PDFium port.
func runConvert(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docmill convert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	learnedLayout := flags.Bool("learned-layout", false,
		"classify lines with the embedded layout model instead of the hand-tuned detectors (headings, list items, figure innards, formulas)")
	regionMarkdown := flags.Bool("region-markdown", false,
		"let the learned region stage drive the whole page: regions become Markdown by class (experimental)")
	// No stripArgSeparator here: "--" is the flag package's own end-of-flags
	// terminator, so parsing directly keeps `convert -- <path>` working and also
	// lets a path that begins with "-" through.
	if err := flags.Parse(args); err != nil {
		return err
	}

	rest := flags.Args()
	if len(rest) != 1 {
		err := fmt.Errorf("usage: docmill [convert] [-learned-layout] [-region-markdown] <input.pdf>")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	data, err := os.ReadFile(rest[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read PDF: %v\n", err)
		return err
	}

	backend, err := newConvertBackend()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "initialise PDFium: %v\n", err)
		return err
	}
	defer backend.Close()

	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open PDF: %v\n", err)
		return err
	}
	defer doc.Close()

	markdown, err := docpdf.ExtractMarkdownWithOptions(ctx, doc, convertOptions(*learnedLayout, *regionMarkdown))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "extract Markdown: %v\n", err)
		return err
	}

	_, err = fmt.Fprint(stdout, markdown)
	return err
}

// convertOptions is ExtractMarkdown's default option set, plus the learned
// layout pipeline when -learned-layout asks for it.
//
// Without the flag the options are exactly ExtractMarkdown's, so the default
// conversion stays byte-identical to what it produces today.
func convertOptions(learnedLayout, regionMarkdown bool) docpdf.ExtractionOptions {
	options := docpdf.ExtractionOptions{
		DetectTables:    true,
		ReadingOrder:    true,
		DetectStructure: true,
		DetectHeadings:  true,
	}
	if learnedLayout {
		// The learned decisions only exist on the classify-then-route path, so
		// the flag turns that on too. LearnedFormulaRouting is set alongside
		// LearnedRouting because the Formula veto is a separate gate in
		// reroute.go: without it the model would own headings, lists and
		// figures while formulas kept flowing into tables, which is not what
		// "run the learned classifier" means to anyone typing the flag.
		options.ClassifyThenRoute = true
		options.LearnedRouting = true
		options.LearnedFormulaRouting = true
	}
	if regionMarkdown {
		// The region stage owns the page outright. It builds its own line
		// labels internally, so none of the other learned flags are implied —
		// this path replaces the routed pipeline rather than extending it.
		options.RegionRouting = true
	}
	return options
}

func newConvertBackend() (docpdf.Backend, error) {
	return parser.NewBackend(), nil
}

func stripArgSeparator(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}
