package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	docpage "github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
)

// The `render` command draws every object the content-stream interpreter says
// the page draws, as a box, into a PNG.
//
// It exists to make the interpreter falsifiable by eye. The interpreter now
// reports images, shadings, filled paths and clip regions that it previously
// discarded, and a bug in any of that — a matrix applied in the wrong order, a
// clip that does not restore at Q, a y-axis left unflipped — produces numbers
// that look entirely plausible in a JSON dump. Drawn over the page they are
// obvious immediately.
//
// It is deliberately naive: outlines only, no fills, no image data, no text.
// Anything more would be a renderer, and a renderer that disagreed with PDFium
// would raise the question of which one was wrong.

type drawnObjectProvider interface {
	DrawnObjects(ctx context.Context) ([]docpage.DrawnObject, error)
}

func runRender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docmill render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outDir := flags.String("out", ".", "directory to write page PNGs into")
	scale := flags.Float64("scale", 2, "pixels per PDF point")
	pageSpec := flags.String("pages", "", "comma-separated 1-based page numbers (default all)")
	kinds := flags.String("kinds", "", "comma-separated kinds to draw: text,path,image,shading,form (default all)")
	asJSON := flags.Bool("json", false, "write one JSON object per page to stdout instead of PNGs")
	regions := flags.Bool("regions", false, "run the learned region stage and draw its decomposition instead of raw objects")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: docmill render [-out dir] [-scale n] [-pages 1,2] [-kinds image,path] <input.pdf>")
		return fmt.Errorf("expected one input file")
	}

	wanted, err := parsePageSpec(*pageSpec)
	if err != nil {
		return err
	}
	kindFilter := parseKindSpec(*kinds)

	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	backend := parser.NewBackend()
	defer func() { _ = backend.Close() }()
	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		return err
	}

	if !*asJSON {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
	}
	stem := strings.TrimSuffix(filepath.Base(flags.Arg(0)), filepath.Ext(flags.Arg(0)))
	encoder := json.NewEncoder(stdout)

	if *regions {
		return renderRegions(ctx, doc, stem, *outDir, *scale, wanted, *asJSON, stdout)
	}

	count, err := doc.PageCount(ctx)
	if err != nil {
		return err
	}
	for index := range count {
		number := index + 1
		if len(wanted) > 0 && !wanted[number] {
			continue
		}
		pg, err := doc.Page(ctx, index)
		if err != nil {
			return err
		}
		provider, ok := pg.(drawnObjectProvider)
		if !ok {
			return fmt.Errorf("backend page does not report drawn objects")
		}
		objects, err := provider.DrawnObjects(ctx)
		if err != nil {
			return err
		}
		size, err := pg.Size(ctx)
		if err != nil {
			return err
		}

		kept := objects[:0:0]
		for _, obj := range objects {
			if kindFilter == nil || kindFilter[obj.Kind] {
				kept = append(kept, obj)
			}
		}

		if *asJSON {
			if err := encoder.Encode(pageObjectsJSON{
				Doc:     stem,
				Page:    number,
				Width:   size.Width,
				Height:  size.Height,
				Objects: kept,
			}); err != nil {
				return err
			}
			continue
		}

		path := filepath.Join(*outDir, fmt.Sprintf("%s.p%03d.png", stem, number))
		if err := writeBoxPNG(path, size, kept, *scale); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s  %d objects (%s)\n", path, len(kept), summariseKinds(kept))
	}
	return nil
}

// pageObjectsJSON is the -json record: one line per page, so a measurement
// script can join it against layout annotations without a second tool.
type pageObjectsJSON struct {
	Doc     string                `json:"doc"`
	Page    int                   `json:"page"`
	Width   float64               `json:"page_w"`
	Height  float64               `json:"page_h"`
	Objects []docpage.DrawnObject `json:"objects"`
}

// kindColour gives each kind a distinct outline colour. Images and shadings are
// the two the interpreter could not see before, so they get the loudest ones.
var kindColour = map[docpage.DrawnKind]color.RGBA{
	docpage.DrawnText:    {0x33, 0x66, 0xCC, 0xFF}, // blue
	docpage.DrawnPath:    {0x88, 0x88, 0x88, 0xFF}, // grey
	docpage.DrawnImage:   {0xE0, 0x30, 0x30, 0xFF}, // red
	docpage.DrawnShading: {0xE0, 0x90, 0x00, 0xFF}, // orange
	docpage.DrawnForm:    {0x22, 0xAA, 0x55, 0xFF}, // green
}

func writeBoxPNG(path string, size geom.Size, objects []docpage.DrawnObject, scale float64) error {
	width := int(size.Width*scale) + 1
	height := int(size.Height*scale) + 1
	if width <= 1 || height <= 1 {
		return fmt.Errorf("page has no area: %.1fx%.1f", size.Width, size.Height)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := range canvas.Pix {
		canvas.Pix[i] = 0xFF
	}

	for _, obj := range objects {
		colour, ok := kindColour[obj.Kind]
		if !ok {
			colour = color.RGBA{0, 0, 0, 0xFF}
		}
		// Forms nest, so their boxes cover their children. Drawing them dashed
		// keeps the outer box readable without hiding what is inside it.
		strokeRect(canvas, obj.Box, scale, colour, obj.Kind == docpage.DrawnForm)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return png.Encode(file, canvas)
}

// strokeRect draws the outline of box. A zero-height or zero-width box — a
// horizontal rule, say — still draws as a line rather than vanishing, which is
// the whole point of looking at the output.
func strokeRect(canvas *image.RGBA, box geom.Box, scale float64, colour color.RGBA, dashed bool) {
	left := int(box.L * scale)
	right := int(box.R * scale)
	top := int(box.T * scale)
	bottom := int(box.B * scale)
	if right < left {
		left, right = right, left
	}
	if bottom < top {
		top, bottom = bottom, top
	}

	for x := left; x <= right; x++ {
		if dashed && (x/4)%2 == 1 {
			continue
		}
		setPixel(canvas, x, top, colour)
		setPixel(canvas, x, bottom, colour)
	}
	for y := top; y <= bottom; y++ {
		if dashed && (y/4)%2 == 1 {
			continue
		}
		setPixel(canvas, left, y, colour)
		setPixel(canvas, right, y, colour)
	}
}

func setPixel(canvas *image.RGBA, x, y int, colour color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(canvas.Rect) {
		return
	}
	canvas.SetRGBA(x, y, colour)
}

func parsePageSpec(spec string) (map[int]bool, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	wanted := map[int]bool{}
	for _, field := range strings.Split(spec, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		var number int
		if _, err := fmt.Sscanf(field, "%d", &number); err != nil || number < 1 {
			return nil, fmt.Errorf("bad page number %q", field)
		}
		wanted[number] = true
	}
	return wanted, nil
}

func parseKindSpec(spec string) map[docpage.DrawnKind]bool {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	kinds := map[docpage.DrawnKind]bool{}
	for _, field := range strings.Split(spec, ",") {
		kinds[docpage.DrawnKind(strings.TrimSpace(field))] = true
	}
	return kinds
}

func summariseKinds(objects []docpage.DrawnObject) string {
	counts := map[docpage.DrawnKind]int{}
	for _, obj := range objects {
		counts[obj.Kind]++
	}
	var parts []string
	for _, kind := range []docpage.DrawnKind{docpage.DrawnText, docpage.DrawnPath, docpage.DrawnImage, docpage.DrawnShading, docpage.DrawnForm} {
		if counts[kind] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", kind, counts[kind]))
		}
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, " ")
}
