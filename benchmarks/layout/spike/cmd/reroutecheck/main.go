// Command reroutecheck is the Task 5 neutrality gate.
//
// The plan is explicit: "DPBench through the rerouted path must match the
// current pipeline within noise. Any real delta is an assembly or routing bug."
// This runs both paths over the same corpus in one process and reports where
// they disagree, so the reroute is proven neutral BEFORE any model decision
// goes live. Comparing rendered Markdown rather than internal state is
// deliberate — it is the only thing a user can observe.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func main() {
	corpus := flag.String("corpus", "benchmarks/dpbench/corpus/pdf", "directory of PDFs")
	jobs := flag.Int("jobs", runtime.NumCPU(), "parallel workers")
	show := flag.Int("show", 5, "how many differing documents to describe")
	formula := flag.Bool("formula", false, "enable LearnedFormulaRouting on the rerouted side (Task 6)")
	flag.Parse()

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

	type result struct {
		name          string
		equal         bool
		defaultLen    int
		routedLen     int
		firstDiffLine int
		defaultLine   string
		routedLine    string
		err           error
	}

	results := make([]result, len(paths))
	var index atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < *jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(index.Add(1)) - 1
				if i >= len(paths) {
					return
				}
				results[i] = compare(paths[i], *formula)
			}
		}()
	}
	wg.Wait()

	same, differ, failed := 0, 0, 0
	var differing []result
	for _, r := range results {
		switch {
		case r.err != nil:
			failed++
		case r.equal:
			same++
		default:
			differ++
			differing = append(differing, r)
		}
	}

	fmt.Printf("documents: %d   identical: %d   differing: %d   errors: %d\n", len(paths), same, differ, failed)
	if differ == 0 && failed == 0 {
		fmt.Println("\nGATE PASSED: the rerouted path is byte-identical to the default path.")
		if !*formula {
			reportStraddleRisk(paths, *jobs)
		}
		return
	}
	for i, r := range differing {
		if i >= *show {
			fmt.Printf("... and %d more\n", len(differing)-*show)
			break
		}
		fmt.Printf("\n%s: %d vs %d bytes, first difference at line %d\n", r.name, r.defaultLen, r.routedLen, r.firstDiffLine)
		fmt.Printf("  default: %q\n", truncate(r.defaultLine))
		fmt.Printf("  routed : %q\n", truncate(r.routedLine))
	}
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("\nERROR %s: %v\n", r.name, r.err)
		}
	}
	os.Exit(1)
}

func truncate(s string) string {
	if len(s) > 110 {
		return s[:110] + "…"
	}
	return s
}

// compare converts one PDF through both paths. Options are identical except for
// ClassifyThenRoute, so any difference is attributable to the reroute alone.
func compare(path string, learnedFormula bool) (out struct {
	name          string
	equal         bool
	defaultLen    int
	routedLen     int
	firstDiffLine int
	defaultLine   string
	routedLine    string
	err           error
}) {
	out.name = filepath.Base(path)
	ctx := context.Background()

	run := func(routed bool) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		backend := parser.NewBackend()
		defer backend.Close()
		doc, err := backend.OpenBytes(ctx, data)
		if err != nil {
			return "", err
		}
		defer doc.Close()
		return docpdf.ExtractMarkdownWithOptions(ctx, doc, docpdf.ExtractionOptions{
			DetectTables:          true,
			ReadingOrder:          true,
			DetectStructure:       true,
			DetectHeadings:        true,
			ClassifyThenRoute:     routed,
			LearnedFormulaRouting: routed && learnedFormula,
		})
	}

	base, err := run(false)
	if err != nil {
		out.err = err
		return out
	}
	routed, err := run(true)
	if err != nil {
		out.err = err
		return out
	}

	out.defaultLen, out.routedLen = len(base), len(routed)
	if base == routed {
		out.equal = true
		return out
	}
	baseLines := strings.Split(base, "\n")
	routedLines := strings.Split(routed, "\n")
	for i := 0; i < len(baseLines) || i < len(routedLines); i++ {
		var a, b string
		if i < len(baseLines) {
			a = baseLines[i]
		}
		if i < len(routedLines) {
			b = routedLines[i]
		}
		if a != b {
			out.firstDiffLine = i + 1
			out.defaultLine, out.routedLine = a, b
			break
		}
	}
	return out
}

// reportStraddleRisk measures the risk Task 6 inherits.
//
// The gate above passes partly by construction: the rerouted path rebuilds
// prose blocks from the ROUTED CELLS, so the class-agnostic line set is
// computed but not yet used to build output. Task 6 will route those lines
// directly, and the plan flags the hazard — "class-agnostic assembly creates
// lines that never existed before", because cells inside tables and figures
// currently never reach the assembler.
//
// A line is at risk exactly when it straddles a routing boundary: partly inside
// a table or heading region and partly outside. Those are the lines whose
// output would change the moment routing is done on lines rather than cells.
// Counting them now turns an unknown into a number.
func reportStraddleRisk(paths []string, jobs int) {
	var total, straddling atomic.Int64
	var index atomic.Int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	var samples []string

	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(index.Add(1)) - 1
				if i >= len(paths) {
					return
				}
				collect(paths[i], &total, &straddling, &mu, &samples)
			}
		}()
	}
	wg.Wait()

	t, s := total.Load(), straddling.Load()
	fmt.Printf("\nclass-agnostic lines straddling a routing boundary: %d of %d (%.3f%%)\n", s, t, 100*float64(s)/float64(max64(t, 1)))
	fmt.Println("  (these are the lines whose output would change if Task 6 routes lines instead of cells)")
	for i, sample := range samples {
		if i >= 5 {
			break
		}
		fmt.Printf("    %s\n", sample)
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func collect(path string, total, straddling *atomic.Int64, mu *sync.Mutex, samples *[]string) {
	restore := docpdf.SetShadowRouteSink(nil)
	defer restore()

	var local []docpdf.StraddleReport
	docpdf.SetStraddleSink(func(reports []docpdf.StraddleReport) {
		local = append(local, reports...)
	})
	defer docpdf.SetStraddleSink(nil)

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
	if _, err := docpdf.ExtractMarkdownWithOptions(context.Background(), doc, docpdf.ExtractionOptions{
		DetectTables: true, ReadingOrder: true, DetectStructure: true, DetectHeadings: true,
		ClassifyThenRoute: true,
	}); err != nil {
		return
	}

	for _, report := range local {
		total.Add(1)
		if report.Straddles {
			straddling.Add(1)
			mu.Lock()
			if len(*samples) < 5 {
				*samples = append(*samples, fmt.Sprintf("%s: %.2f in %s — %q", filepath.Base(path), report.Containment, report.Destination, truncate(report.Text)))
			}
			mu.Unlock()
		}
	}
}
