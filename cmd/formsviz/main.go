//go:build pdfium_cgo && cgo

// Command formsviz validates the native parser's AcroForm widget boxes
// visually: it renders each page to an image with real PDFium (cgo) and
// overlays a translucent, type-coloured rectangle for every widget the
// native pkg/parser reports. Divergence between the render and the overlay
// exposes box/coordinate bugs at a glance.
//
// Build and run via the Taskfile (which wires PKG_CONFIG_PATH and
// DYLD_LIBRARY_PATH to a PDFium install):
//
//	task formsviz -- [-dpi 144] [-out bin] input.pdf
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/single_threaded"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("formsviz", flag.ContinueOnError)
	dpi := flags.Int("dpi", 144, "render resolution")
	outDir := flags.String("out", "bin", "output directory for the overlay PNGs")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: formsviz [-dpi 144] [-out bin] <input.pdf>")
		return 2
	}
	input := flags.Arg(0)

	data, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Native side: the widget boxes under validation.
	backend := parser.NewBackend()
	defer backend.Close()
	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "native open: %v\n", err)
		return 1
	}
	defer doc.Close()
	pageCount, err := doc.PageCount(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Render side: real PDFium through cgo.
	pool := single_threaded.Init(single_threaded.Config{})
	defer pool.Close()
	instance, err := pool.GetInstance(30 * time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pdfium init: %v\n", err)
		return 1
	}
	defer instance.Close()
	opened, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pdfium open: %v\n", err)
		return 1
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: opened.Document})

	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	for i := range pageCount {
		pg, err := doc.Page(ctx, i)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		nativePage, ok := pg.(*parser.Page)
		if !ok {
			fmt.Fprintf(os.Stderr, "page %d: native page unavailable\n", i+1)
			return 1
		}
		fields, err := nativePage.FormFieldBoxes(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		size, err := nativePage.Size(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		rendered, err := instance.RenderPageInDPI(&requests.RenderPageInDPI{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: opened.Document, Index: i}},
			DPI:  *dpi,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "pdfium render page %d: %v\n", i+1, err)
			return 1
		}

		out := overlay(rendered.Result.Image, fields, size.Width, size.Height)
		path := filepath.Join(*outDir, fmt.Sprintf("%s-page%d.png", base, i+1))
		if err := writePNG(path, out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("%s: %d widgets (%s)\n", path, len(fields), countByKind(fields))
	}
	return 0
}

// kindColours maps widget kinds to overlay colours. Kinds refine /FT so the
// two Btn behaviours (checkable vs push button) are distinguishable.
var kindColours = []struct {
	kind string
	c    color.RGBA
}{
	{"text", color.RGBA{R: 0, G: 90, B: 220, A: 255}},       // Tx
	{"checkbox", color.RGBA{R: 0, G: 160, B: 70, A: 255}},   // Btn checkable
	{"button", color.RGBA{R: 245, G: 140, B: 0, A: 255}},    // Btn push button
	{"choice", color.RGBA{R: 150, G: 60, B: 210, A: 255}},   // Ch
	{"signature", color.RGBA{R: 220, G: 40, B: 40, A: 255}}, // Sig
	{"other", color.RGBA{R: 120, G: 120, B: 120, A: 255}},
}

const (
	// Field flag bits, PDF 32000-1 Table 226.
	flagRadio      = 1 << 15
	flagPushbutton = 1 << 16
)

func widgetKind(f parser.FormFieldBox) string {
	switch f.Type {
	case "Tx":
		return "text"
	case "Ch":
		return "choice"
	case "Sig":
		return "signature"
	case "Btn":
		if f.Flags&flagPushbutton != 0 {
			return "button"
		}
		if f.Flags&flagRadio != 0 || (f.Box.Width() < 22 && f.Box.Height() < 22) {
			return "checkbox"
		}
		return "checkbox"
	default:
		return "other"
	}
}

func kindColour(kind string) color.RGBA {
	for _, k := range kindColours {
		if k.kind == kind {
			return k.c
		}
	}
	return kindColours[len(kindColours)-1].c
}

// overlay draws each widget box onto the rendered page and appends a legend
// strip below it. Boxes are page points (top-left origin); the render is
// scaled by the ratio between image and page dimensions.
func overlay(render *image.RGBA, fields []parser.FormFieldBox, pageW, pageH float64) *image.RGBA {
	const legendHeight = 28
	bounds := render.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()+legendHeight))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, bounds, render, bounds.Min, draw.Src)

	sx := float64(bounds.Dx()) / pageW
	sy := float64(bounds.Dy()) / pageH
	for _, f := range fields {
		rect := image.Rect(
			int(f.Box.L*sx), int(f.Box.T*sy),
			int(f.Box.R*sx), int(f.Box.B*sy),
		)
		c := kindColour(widgetKind(f))
		fillRect(out, rect, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 70})
		strokeRect(out, rect, c, 2)
	}

	drawLegend(out, image.Rect(0, bounds.Dy(), bounds.Dx(), bounds.Dy()+legendHeight))
	return out
}

// fillRect alpha-blends c over r. Translucent fills must be color.NRGBA:
// color.RGBA is alpha-premultiplied, so straight channel values with a low
// alpha are invalid and blend to the wrong hue.
func fillRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Over)
}

func strokeRect(img *image.RGBA, r image.Rectangle, c color.RGBA, w int) {
	fillRect(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), c)
	fillRect(img, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), c)
	fillRect(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), c)
	fillRect(img, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), c)
}

func drawLegend(img *image.RGBA, strip image.Rectangle) {
	face := basicfont.Face7x13
	x := strip.Min.X + 8
	y := strip.Min.Y + (strip.Dy()+face.Ascent)/2 - 1
	for _, k := range kindColours {
		if k.kind == "other" {
			continue
		}
		swatch := image.Rect(x, y-face.Ascent+2, x+14, y+2)
		fillRect(img, swatch, color.NRGBA{R: k.c.R, G: k.c.G, B: k.c.B, A: 140})
		strokeRect(img, swatch, k.c, 1)
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.Black),
			Face: face,
			Dot:  fixed.P(x+18, y),
		}
		d.DrawString(k.kind)
		x += 18 + font.MeasureString(face, k.kind).Ceil() + 16
	}
}

func countByKind(fields []parser.FormFieldBox) string {
	counts := map[string]int{}
	for _, f := range fields {
		counts[widgetKind(f)]++
	}
	var parts []string
	for _, k := range kindColours {
		if n := counts[k.kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k.kind))
		}
	}
	return strings.Join(parts, ", ")
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
