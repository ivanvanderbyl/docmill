// Command spike is the throwaway harness for Task 0 of the learned layout
// classifier plan (docs/plans/2026-08-06-learned-layout-classifier.md): prove
// end to end that a gradient-boosted tree over docmill's own geometry can find
// display equations that the hand-tuned heuristics miss.
//
// It is deliberately NOT part of the shipping pipeline. If the spike succeeds,
// Task 2 rebuilds the feature extractor properly inside pkg/pdf; if it fails,
// this directory is deleted along with the rest of the plan.
//
// Subcommands:
//
//	emit     dump one JSONL row per class-agnostically assembled line
//	predict  run the embedded LightGBM model over a PDF and report formula lines
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
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
	"github.com/ivanvanderbyl/docmill/v2/pkg/textline"
)

// lineTolerance matches ParagraphOptions{}.withDefaults().LineTolerance, the
// value the production assembler uses. Hard-coded here because the field is not
// reachable from outside pkg/pdf.
const lineTolerance = 4

// lineRow is one emitted training row: the line's identity and box (for the
// join against the teacher's region boxes) plus its feature vector.
type lineRow struct {
	Doc      string    `json:"doc"`
	Page     int       `json:"page"` // 1-based, matching HURIDOCS page_number
	Line     int       `json:"line"`
	PageW    float64   `json:"page_w"`
	PageH    float64   `json:"page_h"`
	L        float64   `json:"l"`
	T        float64   `json:"t"`
	R        float64   `json:"r"`
	B        float64   `json:"b"`
	Text     string    `json:"text"`
	Features []float64 `json:"f"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: spike <emit|predict|verify|gen|explain|features> [args]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "emit":
		err = runEmit(os.Args[2:])
	case "predict":
		err = runPredict(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "gen":
		err = runGen(os.Args[2:])
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
				rows, err := emitDocument(context.Background(), path)
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

// emitDocument assembles every page CLASS-AGNOSTICALLY — straight from the raw
// text cells into lines, with no figure-region cell drops and no table
// carve-outs — and returns one feature row per line.
//
// This is the point of the exercise and the plan calls it out explicitly: the
// worst formula cases are exactly the lines that today's pipeline swallows into
// a fake table or drops as figure innards, so a dump taken from the default
// path would not contain the rows the model most needs to learn from. Calling
// AssembleLineElements directly on the raw cells is what "DetectTables
// disabled" means here.
func emitDocument(ctx context.Context, path string) ([]lineRow, error) {
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
	pageCount, err := doc.PageCount(ctx)
	if err != nil {
		return nil, err
	}

	var rows []lineRow
	for index := 0; index < pageCount; index++ {
		pageRows, err := emitPage(ctx, doc, index, name)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", index+1, err)
		}
		rows = append(rows, pageRows...)
	}
	return rows, nil
}

func emitPage(ctx context.Context, doc docpdf.Document, index int, name string) ([]lineRow, error) {
	pdfPage, err := doc.Page(ctx, index)
	if err != nil {
		return nil, err
	}
	size, err := pdfPage.Size(ctx)
	if err != nil {
		return nil, err
	}
	cells, err := pdfPage.TextCells(ctx)
	if err != nil {
		return nil, err
	}
	if len(cells) == 0 {
		return nil, nil
	}

	lines := assembleLines(cells)
	ctxPage := newPageContext(size, cells, lines)

	rows := make([]lineRow, 0, len(lines))
	for i := range lines {
		var prev, next *textline.ParagraphTextLine
		if i > 0 {
			prev = &lines[i-1]
		}
		if i+1 < len(lines) {
			next = &lines[i+1]
		}
		box := lines[i].BBox
		rows = append(rows, lineRow{
			Doc:      name,
			Page:     index + 1,
			Line:     i,
			PageW:    size.Width,
			PageH:    size.Height,
			L:        box.L,
			T:        topEdge(box),
			R:        box.R,
			B:        bottomEdge(box),
			Text:     lines[i].Text,
			Features: lineFeatures(lines[i], prev, next, ctxPage),
		})
	}
	return rows, nil
}

// assembleLines runs the shared line assembler over every cell on the page and
// returns the lines sorted top-to-bottom, which is the order the gap features
// assume. AssembleLineElements already emits in that order; the sort is
// defensive and free.
func assembleLines(cells []page.TextCell) []textline.ParagraphTextLine {
	lines := docpdf.AssembleLineElements(cells, lineTolerance)
	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].BBox.CenterY() < lines[j].BBox.CenterY()
	})
	return lines
}

// topEdge and bottomEdge return the box's edges as TOP-LEFT-origin values —
// smaller y is nearer the top of the page — regardless of which origin the box
// records. docmill's text cells are already TopLeft (see TextRectsToCells), and
// HURIDOCS reports left/top/width/height in the same top-down points, so the
// join in join.py can compare them directly. Normalising here rather than at
// the join is the whole of the "reconcile coordinate systems carefully" step.
func topEdge(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.B
	}
	return box.T
}

func bottomEdge(box geom.Box) float64 {
	if box.Origin == geom.BottomLeft {
		return box.T
	}
	return box.B
}
