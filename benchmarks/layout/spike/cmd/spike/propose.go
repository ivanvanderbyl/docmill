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

// The `propose` subcommand dumps the candidate regions the Go proposer builds.
//
// The ceiling numbers so far came from Python reimplementations of what a
// proposer COULD reach. This dumps what the shipping Go proposer ACTUALLY
// offers, so the two can be compared. A gap between them is a bug in the Go
// side, and finding it any later — after a model has been trained on these
// candidates — would mean retraining.

type proposalRow struct {
	Doc    string    `json:"doc"`
	Page   int       `json:"page"`
	Width  float64   `json:"page_w"`
	Height float64   `json:"page_h"`
	L      float64   `json:"l"`
	T      float64   `json:"t"`
	R      float64   `json:"r"`
	B      float64   `json:"b"`
	Source string    `json:"source"`
	Lines  int       `json:"lines"`
	Span   int       `json:"span"`
	Ink    int       `json:"ink"`
	Images int       `json:"images"`
	Paths  int       `json:"paths"`
	Single bool      `json:"single"`
	Class  string    `json:"class,omitempty"`
	Score  float64   `json:"score,omitempty"`
	Iou    float64   `json:"iou_pred,omitempty"`
	NBCls  string    `json:"nb_class,omitempty"`
	NBScr  float64   `json:"nb_score,omitempty"`
	F      []float64 `json:"f,omitempty"`
}

func runPropose(args []string) error {
	flags := flag.NewFlagSet("propose", flag.ContinueOnError)
	listPath := flags.String("list", "", "file containing one PDF path per line")
	jobs := flags.Int("jobs", runtime.NumCPU(), "parallel workers")
	quiet := flags.Bool("quiet", false, "suppress per-document progress")
	splitColumns := flags.Bool("split-columns", false, "split assembled lines at persistent column gaps first")
	selected := flags.Bool("select", false, "classify and suppress, emitting only the regions that survive")
	noSuppress := flags.Bool("no-suppress", false, "with -select, skip non-max suppression")
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
		return fmt.Errorf("usage: spike propose [-list paths.txt] [-jobs N] [-split-columns] <input.pdf>...")
	}

	work := make(chan string)
	results := make(chan []proposalRow, *jobs)
	var wg sync.WaitGroup
	var failures atomic.Int64
	var done atomic.Int64

	for range *jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				rows, err := proposeDocument(context.Background(), path, *splitColumns, *selected, !*noSuppress)
				if err != nil {
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
	total := 0
	for rows := range results {
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
		total += len(rows)
		if n := done.Add(1); !*quiet && n%500 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d documents, %d proposals, %d skipped\n", n, len(paths), total, failures.Load())
		}
	}
	fmt.Fprintf(os.Stderr, "emitted %d proposals from %d documents (%d skipped)\n",
		total, len(paths)-int(failures.Load()), failures.Load())
	return nil
}

func proposeDocument(ctx context.Context, path string, splitColumns, selected, suppress bool) ([]proposalRow, error) {
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
	pages, err := docpdf.PageRegionProposals(ctx, doc, splitColumns, selected, suppress)
	if err != nil {
		return nil, err
	}

	var out []proposalRow
	for _, page := range pages {
		for i, proposal := range page.Proposals {
			var features []float64
			if i < len(page.Features) {
				features = page.Features[i]
			}
			var class string
			var score, iouPred float64
			if i < len(page.Classes) {
				class, score = page.Classes[i], page.Scores[i]
			}
			if i < len(page.Overlaps) {
				iouPred = page.Overlaps[i]
			}
			var nbClass string
			var nbScore float64
			if i < len(page.RealClasses) {
				nbClass, nbScore = page.RealClasses[i], page.RealScores[i]
			}
			out = append(out, proposalRow{
				F:      features,
				Class:  class,
				Score:  score,
				Iou:    iouPred,
				NBCls:  nbClass,
				NBScr:  nbScore,
				Doc:    name,
				Page:   page.Page,
				Width:  page.Size.Width,
				Height: page.Size.Height,
				L:      proposal.Box.L,
				T:      proposal.Box.T,
				R:      proposal.Box.R,
				B:      proposal.Box.B,
				Source: string(proposal.Source),
				Lines:  len(proposal.Lines),
				Span:   proposal.AtomicSpan,
				Ink:    proposal.Ink.Ink,
				Images: proposal.Ink.Images,
				Paths:  proposal.Ink.Paths,
				Single: proposal.Ink.Single,
			})
		}
	}
	return out, nil
}
