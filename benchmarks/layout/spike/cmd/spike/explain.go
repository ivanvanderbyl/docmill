package main

import (
	"context"
	"fmt"
	"strconv"

	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// runExplain prints the decision path behind a document's most interesting
// lines — the introspection AGENTS.md asks for, against the model that ships.
//
//	spike explain <input.pdf> [count]
func runExplain(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: spike explain <input.pdf> [count]")
	}
	count := 3
	if len(args) > 1 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("count: %w", err)
		}
		count = n
	}

	rows, err := emitDocument(context.Background(), args[0], false, false, false)
	if err != nil {
		return err
	}
	if count > len(rows) {
		count = len(rows)
	}
	for _, row := range rows[:count] {
		explanation, err := docpdf.ExplainLineClass(row.Features, 4)
		if err != nil {
			return err
		}
		fmt.Printf("\n=== page %d line %d: %q\n%s", row.Page, row.Line, row.Text, explanation)
	}
	return nil
}
