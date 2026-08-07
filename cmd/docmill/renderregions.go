package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// `render -regions` draws the learned region stage's decomposition of each
// page: propose, classify, suppress, then one labelled box per surviving
// region.
//
// This is the region stage's user-facing surface while nothing routes through
// it. The benchmark says 0.55 weighted F1 against DocLayNet; whether that is
// good enough on the documents someone actually cares about is a judgement the
// benchmark cannot make for them, and a picture can.

// regionColour gives each class a fixed colour, chosen to stay tellable-apart
// on a white page.
var regionColour = map[string]color.RGBA{
	"Text":           {0x33, 0x66, 0xCC, 0xFF}, // blue
	"Section-header": {0x7B, 0x2D, 0xBE, 0xFF}, // purple
	"Title":          {0x7B, 0x2D, 0xBE, 0xFF}, // purple, like headers
	"Table":          {0xE0, 0x30, 0x30, 0xFF}, // red
	"Picture":        {0x1F, 0x9E, 0x55, 0xFF}, // green
	"List-item":      {0x00, 0x87, 0xA8, 0xFF}, // teal
	"Formula":        {0xE0, 0x90, 0x00, 0xFF}, // orange
	"Caption":        {0xB0, 0x6A, 0x00, 0xFF}, // brown
	"Page-header":    {0x88, 0x88, 0x88, 0xFF}, // grey
	"Page-footer":    {0x88, 0x88, 0x88, 0xFF}, // grey
	"Footnote":       {0x88, 0x88, 0x88, 0xFF}, // grey
}

// regionJSON is one page of the -regions -json output.
type regionJSON struct {
	Doc     string       `json:"doc"`
	Page    int          `json:"page"`
	Width   float64      `json:"page_w"`
	Height  float64      `json:"page_h"`
	Regions []regionItem `json:"regions"`
}

type regionItem struct {
	Class string   `json:"class"`
	Score float64  `json:"score"`
	Box   geom.Box `json:"box"`
	Lines int      `json:"lines"`
}

func renderRegions(ctx context.Context, doc docpdf.Document, stem, outDir string, scale float64, wanted map[int]bool, asJSON bool, stdout io.Writer) error {
	pages, err := docpdf.PageRegionProposals(ctx, doc, false, true, true)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)

	for _, pg := range pages {
		if len(wanted) > 0 && !wanted[pg.Page] {
			continue
		}

		if asJSON {
			record := regionJSON{
				Doc: stem, Page: pg.Page,
				Width: pg.Size.Width, Height: pg.Size.Height,
				Regions: make([]regionItem, 0, len(pg.Proposals)),
			}
			for i, proposal := range pg.Proposals {
				record.Regions = append(record.Regions, regionItem{
					Class: pg.Classes[i],
					Score: pg.Scores[i],
					Box:   proposal.Box,
					Lines: len(proposal.Lines),
				})
			}
			if err := encoder.Encode(record); err != nil {
				return err
			}
			continue
		}

		width := int(pg.Size.Width*scale) + 1
		height := int(pg.Size.Height*scale) + 1
		if width <= 1 || height <= 1 {
			return fmt.Errorf("page %d has no area", pg.Page)
		}
		canvas := image.NewRGBA(image.Rect(0, 0, width, height))
		for i := range canvas.Pix {
			canvas.Pix[i] = 0xFF
		}

		counts := map[string]int{}
		for i, proposal := range pg.Proposals {
			class := pg.Classes[i]
			counts[class]++
			colour, ok := regionColour[class]
			if !ok {
				colour = color.RGBA{0, 0, 0, 0xFF}
			}
			// A double outline reads as a region even where boxes are dense;
			// single-pixel outlines vanish next to each other.
			strokeRect(canvas, proposal.Box, scale, colour, false)
			inset := proposal.Box
			inset.L++
			inset.R--
			inset.T++
			inset.B--
			strokeRect(canvas, inset, scale, colour, false)

			labelRegion(canvas, proposal.Box, scale, colour,
				fmt.Sprintf("%s %.2f", class, pg.Scores[i]))
		}

		path := filepath.Join(outDir, fmt.Sprintf("%s.p%03d.regions.png", stem, pg.Page))
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := png.Encode(file, canvas); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s  %d regions (%s)\n", path, len(pg.Proposals), summariseRegions(counts))
	}
	return nil
}

// labelRegion writes the class and score just inside the region's top-left
// corner, on a solid patch so it stays readable over the outline.
func labelRegion(canvas *image.RGBA, box geom.Box, scale float64, colour color.RGBA, text string) {
	face := basicfont.Face7x13
	x := int(box.L*scale) + 3
	y := int(box.T*scale) + face.Ascent + 2

	width := font.MeasureString(face, text).Ceil()
	patch := image.Rect(x-1, y-face.Ascent-1, x+width+1, y+face.Descent+1)
	patch = patch.Intersect(canvas.Rect)
	for py := patch.Min.Y; py < patch.Max.Y; py++ {
		for px := patch.Min.X; px < patch.Max.X; px++ {
			canvas.SetRGBA(px, py, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF})
		}
	}

	drawer := font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(colour),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	drawer.DrawString(text)
}

func summariseRegions(counts map[string]int) string {
	out := ""
	for _, class := range []string{"Text", "Section-header", "List-item", "Table", "Picture", "Formula", "Caption", "Page-header", "Page-footer", "Title", "Footnote"} {
		if counts[class] > 0 {
			if out != "" {
				out += " "
			}
			out += fmt.Sprintf("%s=%d", class, counts[class])
		}
	}
	if out == "" {
		return "empty"
	}
	return out
}
