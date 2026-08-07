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
	learnedColumns := flags.Bool("learned-columns", false,
		"derive table column boundaries with the FinTabNet-trained model instead of the densest-row heuristic")
	// No stripArgSeparator here: "--" is the flag package's own end-of-flags
	// terminator, so parsing directly keeps `convert -- <path>` working and also
	// lets a path that begins with "-" through.
	if err := flags.Parse(args); err != nil {
		return err
	}

	rest := flags.Args()
	if len(rest) != 1 {
		err := fmt.Errorf("usage: docmill [convert] [-learned-layout] [-learned-columns] <input.pdf>")
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

	markdown, err := docpdf.ExtractMarkdownWithOptions(ctx, doc, convertOptions(*learnedLayout, *learnedColumns))
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
func convertOptions(learnedLayout, learnedColumns bool) docpdf.ExtractionOptions {
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
	if learnedColumns {
		// Independent of -learned-layout: this changes table STRUCTURE (what
		// TEDS scores) rather than which regions are tables, so the two are
		// measurable separately. It still needs the rerouted path, which is
		// where the rulings reach the detector.
		options.ClassifyThenRoute = true
		options.LearnedColumns = true
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
