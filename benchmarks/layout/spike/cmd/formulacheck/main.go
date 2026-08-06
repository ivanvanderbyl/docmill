package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func run(path string, formula bool) string {
	data, _ := os.ReadFile(path)
	b := parser.NewBackend()
	defer b.Close()
	doc, err := b.OpenBytes(context.Background(), data)
	if err != nil {
		panic(err)
	}
	defer doc.Close()
	out, err := docpdf.ExtractMarkdownWithOptions(context.Background(), doc, docpdf.ExtractionOptions{
		DetectTables: true, ReadingOrder: true, DetectStructure: true, DetectHeadings: true,
		ClassifyThenRoute: true, LearnedFormulaRouting: formula,
	})
	if err != nil {
		panic(err)
	}
	return out
}

func main() {
	ok, err := docpdf.LayoutModelAvailable()
	fmt.Println("model available:", ok, "err:", err, "classes:", docpdf.LayoutModelClasses())
	for _, path := range os.Args[1:] {
		base := run(path, false)
		learned := run(path, true)
		countRows := func(s string) int {
			n := 0
			for _, l := range strings.Split(s, "\n") {
				if strings.HasPrefix(l, "|") {
					n++
				}
			}
			return n
		}
		fmt.Printf("%s: table rows %d -> %d, bytes %d -> %d, identical=%v\n",
			path, countRows(base), countRows(learned), len(base), len(learned), base == learned)
	}
}
