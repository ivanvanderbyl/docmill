package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ivanvanderbyl/docmill/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/pkg/pdf"
)

// runConvert converts a single PDF path (args[0]) to Markdown on stdout using
// the native pure-Go PDFium port.
func runConvert(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = stripArgSeparator(args)
	if len(args) != 1 {
		err := fmt.Errorf("usage: docmill [convert] <input.pdf>")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	data, err := os.ReadFile(args[0])
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

	markdown, err := docpdf.ExtractMarkdown(ctx, doc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "extract Markdown: %v\n", err)
		return err
	}

	_, err = fmt.Fprint(stdout, markdown)
	return err
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
