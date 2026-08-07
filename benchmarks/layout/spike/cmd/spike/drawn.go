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

	docpage "github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
)

// The `drawn` subcommand dumps every object the content-stream interpreter says
// a page draws, with its VISIBLE box.
//
// `emit` dumps assembled text lines, which is the only primitive the cascade has
// ever had. That is exactly the limitation being measured here: 39.8% of
// DocLayNet's Picture regions contain no assembled line at all, so no ceiling
// computed from `emit` can say anything about them. This exists so the ceiling
// can be recomputed over ink rather than over text.

// drawnPage is one emitted row: a page and everything it draws.
type drawnPage struct {
	Doc     string                `json:"doc"`
	Page    int                   `json:"page"`
	Width   float64               `json:"page_w"`
	Height  float64               `json:"page_h"`
	Objects []docpage.DrawnObject `json:"objects"`
}

type drawnObjectProvider interface {
	DrawnObjects(ctx context.Context) ([]docpage.DrawnObject, error)
}

func runDrawn(args []string) error {
	flags := flag.NewFlagSet("drawn", flag.ContinueOnError)
	listPath := flags.String("list", "", "file containing one PDF path per line")
	jobs := flags.Int("jobs", runtime.NumCPU(), "parallel workers")
	quiet := flags.Bool("quiet", false, "suppress per-document progress")
	skipText := flags.Bool("skip-text", false, "omit text objects, which `emit` already covers")
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
		return fmt.Errorf("usage: spike drawn [-list paths.txt] [-jobs N] [-skip-text] <input.pdf>...")
	}

	work := make(chan string)
	results := make(chan []drawnPage, *jobs)
	var wg sync.WaitGroup
	var failures atomic.Int64
	var done atomic.Int64

	for range *jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				pages, err := drawnDocument(context.Background(), path, *skipText)
				if err != nil {
					failures.Add(1)
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", filepath.Base(path), err)
					continue
				}
				results <- pages
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
	objects := 0
	for pages := range results {
		for _, page := range pages {
			if err := encoder.Encode(page); err != nil {
				return err
			}
			objects += len(page.Objects)
		}
		if n := done.Add(1); !*quiet && n%500 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d documents, %d objects, %d skipped\n", n, len(paths), objects, failures.Load())
		}
	}
	fmt.Fprintf(os.Stderr, "emitted %d objects from %d documents (%d skipped)\n",
		objects, len(paths)-int(failures.Load()), failures.Load())
	return nil
}

func drawnDocument(ctx context.Context, path string, skipText bool) ([]drawnPage, error) {
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
	count, err := doc.PageCount(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]drawnPage, 0, count)
	for index := range count {
		pg, err := doc.Page(ctx, index)
		if err != nil {
			return nil, err
		}
		provider, ok := pg.(drawnObjectProvider)
		if !ok {
			return nil, fmt.Errorf("backend page does not report drawn objects")
		}
		objects, err := provider.DrawnObjects(ctx)
		if err != nil {
			return nil, err
		}
		size, err := pg.Size(ctx)
		if err != nil {
			return nil, err
		}
		if skipText {
			kept := objects[:0]
			for _, obj := range objects {
				if obj.Kind != docpage.DrawnText {
					kept = append(kept, obj)
				}
			}
			objects = kept
		}
		out = append(out, drawnPage{
			Doc:     name,
			Page:    index + 1,
			Width:   size.Width,
			Height:  size.Height,
			Objects: objects,
		})
	}
	return out, nil
}
