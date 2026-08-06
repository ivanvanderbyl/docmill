package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
)

// runExplain prints the decision path behind the highest-scoring lines of a
// document — the introspection leaves cannot provide.
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

	rows, err := emitDocument(context.Background(), args[0])
	if err != nil {
		return err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return genPredict(rows[i].Features) > genPredict(rows[j].Features)
	})
	if count > len(rows) {
		count = len(rows)
	}
	for _, row := range rows[:count] {
		fmt.Printf("\n=== page %d line %d: %q\n", row.Page, row.Line, row.Text)
		fmt.Print(genExplain(row.Features, featureNames, 5))
	}
	return nil
}
