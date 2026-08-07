// Command colcheck reports which branch derives each table's columns.
//
// The DPBench run with -learned-columns produced identical scores, and an
// identical score cannot distinguish "the model ran and agreed with the
// heuristic" from "the model never ran". This answers that.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

func main() {
	corpus := flag.String("corpus", "benchmarks/dpbench/corpus/pdf", "directory of PDFs")
	flag.Parse()

	ok, err := doctable.ColumnModelAvailable()
	fmt.Printf("column model available: %v (err=%v)\n", ok, err)

	entries, err := os.ReadDir(*corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var paths []string
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			paths = append(paths, filepath.Join(*corpus, entry.Name()))
		}
	}
	sort.Strings(paths)

	doctable.ColumnDerivationCounts() // reset
	for _, path := range paths {
		convert(path)
	}
	anchor, learned, declined, heuristic := doctable.ColumnDerivationCounts()
	total := anchor + learned + declined + heuristic
	fmt.Printf("\ncolumn derivations over %d documents: %d\n", len(paths), total)
	fmt.Printf("  anchor row (unchanged by this work) %d\n", anchor)
	fmt.Printf("  learned model accepted              %d\n", learned)
	fmt.Printf("  learned model declined -> heuristic %d\n", declined)
	fmt.Printf("  heuristic (model off or no gaps)    %d\n", heuristic)
}

func convert(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	backend := parser.NewBackend()
	defer backend.Close()
	doc, err := backend.OpenBytes(context.Background(), data)
	if err != nil {
		return
	}
	defer doc.Close()
	_, _ = docpdf.ExtractMarkdownWithOptions(context.Background(), doc, docpdf.ExtractionOptions{
		DetectTables: true, ReadingOrder: true, DetectStructure: true, DetectHeadings: true,
		ClassifyThenRoute: true, LearnedColumns: true,
	})
}
