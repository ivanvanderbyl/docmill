// Command tablegaps emits candidate column boundaries for FinTabNet tables,
// with docmill's own features, for the table-structure trainer.
//
// The features come from pkg/table so training and inference share one
// definition. The labels come from FinTabNet and are derived in Python; this
// tool only carries the geometry across.
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
	"sync"
	"sync/atomic"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

type annotation struct {
	TableID  int       `json:"table_id"`
	Filename string    `json:"filename"`
	Split    string    `json:"split"`
	BBox     []float64 `json:"bbox"`
}

type gapRow struct {
	TableID  int       `json:"table_id"`
	Split    string    `json:"split"`
	Filename string    `json:"filename"`
	Left     float64   `json:"l"`
	Right    float64   `json:"r"`
	Features []float64 `json:"f"`
}

func main() {
	annotations := flag.String("annotations", "", "FinTabNet_1.0.0_table_*.jsonl")
	pdfRoot := flag.String("pdfs", "", "fintabnet/pdf")
	limit := flag.Int("limit", 0, "stop after N tables (0 = all)")
	jobs := flag.Int("jobs", runtime.NumCPU(), "parallel workers")
	features := flag.Bool("features", false, "print the feature contract and exit")
	flag.Parse()

	if *features {
		_ = json.NewEncoder(os.Stdout).Encode(doctable.ColumnGapFeatureNames)
		return
	}

	file, err := os.Open(*annotations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)

	work := make(chan annotation, *jobs*4)
	results := make(chan []gapRow, *jobs*4)
	var wg sync.WaitGroup
	var skipped atomic.Int64

	for i := 0; i < *jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range work {
				rows, err := gapsFor(a, *pdfRoot)
				if err != nil {
					skipped.Add(1)
					continue
				}
				results <- rows
			}
		}()
	}
	go func() {
		count := 0
		for scanner.Scan() {
			var a annotation
			if json.Unmarshal(scanner.Bytes(), &a) != nil || len(a.BBox) != 4 {
				continue
			}
			work <- a
			count++
			if *limit > 0 && count >= *limit {
				break
			}
		}
		close(work)
		wg.Wait()
		close(results)
	}()

	writer := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer writer.Flush()
	encoder := json.NewEncoder(writer)
	tables, gaps := 0, 0
	for rows := range results {
		tables++
		for _, r := range rows {
			if err := encoder.Encode(r); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		gaps += len(rows)
		if tables%2000 == 0 {
			fmt.Fprintf(os.Stderr, "%d tables, %d candidate gaps, %d skipped\n", tables, gaps, skipped.Load())
		}
	}
	fmt.Fprintf(os.Stderr, "done: %d tables, %d candidate gaps, %d skipped\n", tables, gaps, skipped.Load())
}

func gapsFor(a annotation, pdfRoot string) ([]gapRow, error) {
	data, err := os.ReadFile(filepath.Join(pdfRoot, a.Filename))
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
	p, err := doc.Page(context.Background(), 0)
	if err != nil {
		return nil, err
	}
	size, err := p.Size(context.Background())
	if err != nil {
		return nil, err
	}

	var cells []page.TextCell
	if provider, ok := p.(interface {
		WordTextCells(context.Context) ([]page.TextCell, error)
	}); ok {
		cells, err = provider.WordTextCells(context.Background())
	}
	if err != nil || len(cells) == 0 {
		if cells, err = p.TextCells(context.Background()); err != nil {
			return nil, err
		}
	}
	var rulings []page.RulingSegment
	if provider, ok := p.(interface {
		RulingSegments(context.Context) ([]page.RulingSegment, error)
	}); ok {
		rulings, _ = provider.RulingSegments(context.Background())
	}

	// FinTabNet is bottom-left origin (y up); docmill is top-left (y down).
	box := geom.Box{
		L: a.BBox[0], R: a.BBox[2],
		T: size.Height - a.BBox[3], B: size.Height - a.BBox[1],
		Origin: geom.TopLeft,
	}

	candidates := doctable.ColumnGapCandidates(cells, rulings, box)
	out := make([]gapRow, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, gapRow{
			TableID: a.TableID, Split: a.Split, Filename: a.Filename,
			Left: c.Left, Right: c.Right, Features: c.Features,
		})
	}
	return out, nil
}
