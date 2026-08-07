// Command spike is the training and measurement harness for the learned layout
// classifier (docs/plans/2026-08-06-learned-layout-classifier.md).
//
// It is NOT part of the shipping pipeline: the feature vector, the model and
// the routing all live in pkg/pdf. This drives them over a corpus.
//
// Subcommands:
//
//	emit      one JSONL row per class-agnostically assembled line: the feature
//	          vector plus what the pipeline currently calls that line
//	explain   print the model's decision path for a document's first lines
//	regions   one JSONL row per candidate region: the line model's proposed
//	          class plus the region-scoped feature vector
//	features  print the feature contract, for the Python trainer to assert on
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// lineRow is one emitted training row. It is now just docmill's own
// LayoutDebugRow: the feature vector and the current heuristic class both come
// from pkg/pdf, so there is exactly one definition of each.
type lineRow = docpdf.LayoutDebugRow

// featureNames is the feature contract, read from pkg/pdf rather than restated
// here. Task 2 moved the definition into the shipping package precisely so this
// throwaway tool cannot drift from it.
var featureNames = docpdf.LayoutFeatureContract()

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: spike <emit|regions|explain|features> [args]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "emit":
		err = runEmit(os.Args[2:])
	case "regions":
		err = runRegions(os.Args[2:])
	case "explain":
		err = runExplain(os.Args[2:])
	case "features":
		// Print the feature-name contract so train.py can assert against it.
		err = json.NewEncoder(os.Stdout).Encode(featureNames)
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "spike:", err)
		os.Exit(1)
	}
}

// runEmit dumps feature rows for every PDF named on the command line, or — with
// -list <file> — for every path in a file, which is how the DocLayNet corpus is
// driven (80k single-page PDFs overflow ARG_MAX).
//
// Emission is embarrassingly parallel across documents, so it runs a worker
// pool: at corpus scale the difference is hours. Output order is not stable
// across workers, which is fine because every row carries its own doc/page/line
// identity and the join is by key, not by position.
func runEmit(args []string) error {
	flags := flag.NewFlagSet("emit", flag.ContinueOnError)
	listPath := flags.String("list", "", "file containing one PDF path per line")
	learnedFormula := flags.Bool("learned-formula", false, "apply the migrated Formula routing when reporting the current class")
	learnedAll := flags.Bool("learned-all", false, "hand every line-class decision to the model")
	learnedRegions := flags.Bool("learned-regions", false, "gate structural regions with the region model")
	jobs := flags.Int("jobs", runtime.NumCPU(), "parallel workers")
	quiet := flags.Bool("quiet", false, "suppress per-document progress")
	if err := flags.Parse(args); err != nil {
		return err
	}

	paths := flags.Args()
	if *listPath != "" {
		data, err := os.ReadFile(*listPath)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				paths = append(paths, line)
			}
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("usage: spike emit [-list paths.txt] [-jobs N] <input.pdf>...")
	}

	work := make(chan string)
	results := make(chan []lineRow, *jobs)
	var wg sync.WaitGroup
	var failures atomic.Int64
	var done atomic.Int64

	for i := 0; i < *jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				rows, err := emitDocument(context.Background(), path, *learnedFormula, *learnedAll, *learnedRegions)
				if err != nil {
					// One malformed PDF in a 80k-document corpus must not
					// abandon the other 79,999; count it and carry on.
					failures.Add(1)
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", filepath.Base(path), err)
					continue
				}
				results <- rows
			}
		}()
	}
	go func() {
		for _, path := range paths {
			work <- path
		}
		close(work)
		wg.Wait()
		close(results)
	}()

	writer := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer writer.Flush()
	encoder := json.NewEncoder(writer)
	lines := 0
	for rows := range results {
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
		lines += len(rows)
		if n := done.Add(1); !*quiet && n%500 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d documents, %d lines, %d skipped\n", n, len(paths), lines, failures.Load())
		}
	}
	fmt.Fprintf(os.Stderr, "emitted %d lines from %d documents (%d skipped)\n", lines, len(paths)-int(failures.Load()), failures.Load())
	return nil
}

// emitDocument delegates to pkg/pdf.LayoutDebugRows, which assembles every page
// CLASS-AGNOSTICALLY — straight from the raw text cells, no figure drops and no
// table carve-outs — computes the feature vector, and reports what today's
// heuristics call each line.
//
// That last part is Task 1's baseline: the heuristics and the model have to be
// scored on the SAME lines or the comparison means nothing.
func emitDocument(ctx context.Context, path string, learnedFormula, learnedAll, learnedRegions bool) ([]lineRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	backend := parser.NewBackend()
	defer backend.Close()

	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		return nil, err
	}
	defer doc.Close()

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// The same options ExtractMarkdown uses, so the current-class column
	// describes the pipeline users actually run.
	return docpdf.LayoutDebugRows(ctx, doc, name, docpdf.ExtractionOptions{
		DetectTables:          true,
		ReadingOrder:          true,
		DetectStructure:       true,
		DetectHeadings:        true,
		LearnedFormulaRouting: learnedFormula,
		LearnedRouting:        learnedAll,
		LearnedRegions:        learnedRegions,
		ClassifyThenRoute:     learnedAll || learnedFormula,
	})
}
