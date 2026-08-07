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

// runRegions dumps candidate regions for the REGION model's training set.
//
// It requires the line model, because a candidate is a run of same-label lines:
// this stage consumes the previous stage's output, which is why the plan
// sequences it as train line model -> embed -> emit regions -> train region
// model.
func runRegions(args []string) error {
	flags := flag.NewFlagSet("regions", flag.ContinueOnError)
	listPath := flags.String("list", "", "file containing one PDF path per line")
	jobs := flags.Int("jobs", runtime.NumCPU(), "parallel workers")
	features := flags.Bool("features", false, "print the region feature contract and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *features {
		return json.NewEncoder(os.Stdout).Encode(docpdf.RegionFeatureNames)
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
		return fmt.Errorf("usage: spike regions [-list paths.txt] [-jobs N] <input.pdf>...")
	}

	work := make(chan string)
	results := make(chan []docpdf.RegionDebugRow, *jobs)
	var wg sync.WaitGroup
	var failures, done atomic.Int64

	for i := 0; i < *jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				rows, err := regionsFor(path)
				if err != nil {
					failures.Add(1)
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
	total := 0
	for rows := range results {
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
		total += len(rows)
		if n := done.Add(1); n%2000 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d documents, %d regions, %d skipped\n", n, len(paths), total, failures.Load())
		}
	}
	fmt.Fprintf(os.Stderr, "emitted %d regions from %d documents (%d skipped)\n", total, len(paths)-int(failures.Load()), failures.Load())
	return nil
}

func regionsFor(path string) ([]docpdf.RegionDebugRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	backend := parser.NewBackend()
	defer backend.Close()
	doc, err := backend.OpenBytes(context.Background(), data)
	if err != nil {
		return nil, err
	}
	defer doc.Close()
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return docpdf.LayoutRegionRows(context.Background(), doc, name, docpdf.ExtractionOptions{
		DetectTables: true, ReadingOrder: true, DetectStructure: true, DetectHeadings: true,
	})
}
