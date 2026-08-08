// Command fintabcheck verifies that FinTabNet's cell annotations line up with
// docmill's own extraction.
//
// This is the check that decides whether the table-structure work is viable at
// all. FinTabNet gives per-cell boxes in PDF points with a BOTTOM-left origin
// (y up); docmill's cells are TOP-left (y down). Getting that flip wrong would
// silently shift every label by the page height, so it is verified against real
// text rather than assumed — the same mistake DocLayNet's non-uniform COCO
// scaling nearly caused.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
)

type record struct {
	TableID  int       `json:"table_id"`
	Filename string    `json:"filename"`
	BBox     []float64 `json:"bbox"`
	HTML     struct {
		Cells []struct {
			Tokens []string  `json:"tokens"`
			BBox   []float64 `json:"bbox"`
		} `json:"cells"`
	} `json:"html"`
}

func main() {
	annotations := flag.String("annotations", "", "FinTabNet_1.0.0_table_val.jsonl")
	pdfRoot := flag.String("pdfs", "", "fintabnet/pdf")
	limit := flag.Int("limit", 20, "tables to check")
	flag.Parse()

	file, err := os.Open(*annotations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)

	checked, matched, cellsTotal, cellsMatched := 0, 0, 0, 0
	for scanner.Scan() && checked < *limit {
		var r record
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			continue
		}
		path := filepath.Join(*pdfRoot, r.Filename)
		cells, height, err := extract(path)
		if err != nil {
			continue
		}
		checked++

		ok, total := 0, 0
		for _, c := range r.HTML.Cells {
			if len(c.BBox) != 4 {
				continue
			}
			want := strings.Join(c.Tokens, "")
			if strings.TrimSpace(want) == "" {
				continue
			}
			total++
			// Bottom-left (y up) to top-left (y down).
			box := geom.Box{
				L: c.BBox[0], R: c.BBox[2],
				T: height - c.BBox[3], B: height - c.BBox[1],
				Origin: geom.TopLeft,
			}
			if overlaps(cells, box, want) {
				ok++
			}
		}
		cellsTotal += total
		cellsMatched += ok
		if total > 0 && float64(ok)/float64(total) >= 0.8 {
			matched++
		}
		if checked <= 3 {
			fmt.Printf("%s table %d: %d/%d cells found in docmill's text\n", r.Filename, r.TableID, ok, total)
		}
	}
	fmt.Printf("\ntables checked: %d, aligned (>=80%% cells): %d\n", checked, matched)
	fmt.Printf("cells: %d, matched to docmill text: %d (%.1f%%)\n", cellsTotal, cellsMatched, 100*float64(cellsMatched)/float64(max(cellsTotal, 1)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func extract(path string) ([]page.TextCell, float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	backend := parser.NewBackend()
	defer backend.Close()
	doc, err := backend.OpenBytes(context.Background(), data)
	if err != nil {
		return nil, 0, err
	}
	defer doc.Close()
	p, err := doc.Page(context.Background(), 0)
	if err != nil {
		return nil, 0, err
	}
	size, err := p.Size(context.Background())
	if err != nil {
		return nil, 0, err
	}
	// WORD-level cells, not line-level. docmill's TextCells are whole-line
	// rects, so a table row is frequently one cell and would never sit inside a
	// single annotated table cell. Matching at word granularity is what makes
	// this a test of the coordinate transform rather than of cell segmentation.
	if provider, ok := p.(interface {
		WordTextCells(context.Context) ([]page.TextCell, error)
	}); ok {
		if words, err := provider.WordTextCells(context.Background()); err == nil && len(words) > 0 {
			return words, size.Height, nil
		}
	}
	cells, err := p.TextCells(context.Background())
	return cells, size.Height, err
}

// overlaps reports whether docmill extracted the expected text somewhere inside
// the annotated cell box. Text comparison is deliberately loose — whitespace and
// case only — because the point is to confirm the GEOMETRY, not the glyphs.
func overlaps(cells []page.TextCell, box geom.Box, want string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(s), ""))
	}
	target := norm(want)
	if target == "" {
		return true
	}
	var found strings.Builder
	for _, cell := range cells {
		w := min64(cell.Box.R, box.R) - max64(cell.Box.L, box.L)
		h := min64(cell.Box.B, box.B) - max64(cell.Box.T, box.T)
		if w <= 0 || h <= 0 {
			continue
		}
		area := cell.Box.Width() * cell.Box.Height()
		if area > 0 && (w*h)/area < 0.5 {
			continue
		}
		found.WriteString(cell.Text)
	}
	got := norm(found.String())
	return got != "" && (strings.Contains(got, target) || strings.Contains(target, got))
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
